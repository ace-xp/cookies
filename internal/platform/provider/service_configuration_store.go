package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// Connections and routes on a live deployment were seeded by the configure-*
// scripts, each with its own primary key. So rows are resolved by their natural
// key — connection_code for a connection, (capability, model_alias) for a route
// — and an id is only generated when nothing exists yet. Deriving an id from
// the catalog code would collide with the seeded connection_code, or write a
// second route the platform never reads.
func generatedConnectionID(code string) string {
	return "connection_" + strings.ReplaceAll(strings.ReplaceAll(code, ".", "_"), "-", "_")
}

func generatedRouteID(alias string) string {
	return "route_" + strings.ReplaceAll(alias, ".", "_")
}

// addressField and secretField name the two fields every editable service has
// under a different label — miyun calls them endpoint/session_cookie, the model
// services call them base_url/api_key. Both land in the same storage column, so
// the mapping lives here rather than being special-cased at each call site.
func addressField(service servicecatalog.Service) string {
	for _, field := range service.Fields {
		if field.IsAddress() {
			return field.Name
		}
	}
	return ""
}

func secretField(service servicecatalog.Service) string {
	for _, field := range service.Fields {
		if field.Kind == servicecatalog.FieldSecret {
			return field.Name
		}
	}
	return ""
}

// GetServiceConfiguration reads the current stored revision. The credential is
// decrypted only to build a mask; the plaintext never leaves this function.
func (s MySQLGatewayConfigStore) GetServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, code string) (ServiceConfiguration, error) {
	service, found := servicecatalog.Find(code)
	if !found {
		return ServiceConfiguration{}, ServiceConfigurationInputError{Message: fmt.Sprintf("未知的服务编码 %q", code)}
	}
	if s.DB == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider database is required")
	}
	config := ServiceConfiguration{
		Code:          code,
		Values:        map[string]string{},
		MaskedSecrets: map[string]string{},
	}

	var (
		connectionID     string
		baseURL          string
		version          int64
		updatedAt        time.Time
		verifiedAt       sql.NullTime
		verificationText sql.NullString
		outcome          sql.NullString
	)
	err := s.DB.QueryRowContext(ctx, `SELECT connection.id, revision.base_url, connection.version, connection.updated_at,
			connection.last_verified_at, connection.last_verification_message, connection.last_verification_outcome
		FROM provider_connections connection
		JOIN provider_connection_revisions revision ON revision.id = connection.current_revision_id
		WHERE connection.connection_code = ?`, service.ConnectionCode).Scan(
		&connectionID, &baseURL, &version, &updatedAt, &verifiedAt, &verificationText, &outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return ServiceConfiguration{}, err
	}

	config.Configured = true
	config.Version = version
	config.UpdatedAt = updatedAt
	if address := addressField(service); address != "" {
		config.Values[address] = baseURL
	}
	if outcome.Valid && outcome.String != "" {
		config.LastProbe = servicecatalog.Result{
			Outcome: servicecatalog.Outcome(outcome.String),
			Message: verificationText.String,
		}
	}
	if verifiedAt.Valid {
		instant := verifiedAt.Time
		config.LastProbedAt = &instant
	}

	if service.ModelAlias != "" {
		var upstreamModel string
		err = s.DB.QueryRowContext(ctx, `SELECT route_revision.upstream_model
			FROM provider_model_routes route
			JOIN provider_model_route_revisions route_revision ON route_revision.id = route.current_revision_id
			WHERE route.model_alias = ? AND route.capability = ?`,
			service.ModelAlias, service.Capability).Scan(&upstreamModel)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// A connection without a route is a half-written state the page
			// should show as-is rather than hide.
		case err != nil:
			return ServiceConfiguration{}, err
		default:
			config.Values["model"] = upstreamModel
		}
	}

	if secret := secretField(service); secret != "" {
		plaintext, credErr := s.resolveServiceCredential(ctx, connectionID)
		if credErr == nil {
			config.CredentialReadable = true
			config.MaskedSecrets[secret] = MaskAPIKey(plaintext)
		}
		// A credential row can exist and still not decrypt after a master key
		// rotation. Reporting CredentialReadable=false lets the page ask for a
		// re-entry instead of showing an outage.
	}
	return config, nil
}

func (s MySQLGatewayConfigStore) resolveServiceCredential(ctx context.Context, connectionID string) (string, error) {
	if s.Cipher == nil {
		return "", fmt.Errorf("provider credential cipher is required")
	}
	var (
		ciphertext []byte
		nonce      []byte
		keyVersion string
	)
	err := s.DB.QueryRowContext(ctx, `SELECT ciphertext, nonce, key_version
		FROM provider_credentials
		WHERE connection_id = ? AND status = 'active'
			AND active_from <= UTC_TIMESTAMP(6)
			AND (active_until IS NULL OR active_until > UTC_TIMESTAMP(6))
		ORDER BY credential_version DESC LIMIT 1`, connectionID).Scan(&ciphertext, &nonce, &keyVersion)
	if err != nil {
		return "", err
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, nonce, keyVersion)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// probeService routes through the injectable prober so tests do not depend on
// the network or on someone's credential still being valid.
func (s MySQLGatewayConfigStore) probeService(ctx context.Context, code, address, secret string) servicecatalog.Result {
	if s.ServiceProber != nil {
		return s.ServiceProber(ctx, code, address, secret)
	}
	return ProbeService(ctx, code, address, secret)
}

// VerifyServiceConfiguration probes without writing. An omitted secret is
// probed with the stored credential, so "test connection" still works after a
// page reload.
func (s MySQLGatewayConfigStore) VerifyServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, input ServiceConfigurationInput) (servicecatalog.Result, error) {
	if s.DB == nil {
		return servicecatalog.Result{}, fmt.Errorf("provider database is required")
	}
	normalized, err := normalizeServiceConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return servicecatalog.Result{}, err
	}
	service, _ := servicecatalog.Find(normalized.Code)

	address := normalized.Values[addressField(service)]
	secret := ""
	if name := secretField(service); name != "" {
		secret = normalized.Values[name]
		if secret == "" {
			stored, resolveErr := s.resolveStoredSecret(ctx, service.ConnectionCode)
			if resolveErr != nil {
				return servicecatalog.Result{}, ServiceConfigurationInputError{Message: "还没有保存过密钥，请先填写"}
			}
			secret = stored
		}
	}
	return s.probeService(ctx, normalized.Code, address, secret), nil
}

func (s MySQLGatewayConfigStore) resolveStoredSecret(ctx context.Context, connectionCode string) (string, error) {
	var connectionID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM provider_connections WHERE connection_code = ?`, connectionCode).Scan(&connectionID); err != nil {
		return "", err
	}
	return s.resolveServiceCredential(ctx, connectionID)
}

// SaveServiceConfiguration probes first and writes only on success, so a typo
// cannot take a capability down. Every table is append-only: revisions
// accumulate and a replaced credential is retired rather than deleted.
func (s MySQLGatewayConfigStore) SaveServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, changedBy string, input ServiceConfigurationInput) (ServiceConfiguration, error) {
	if s.DB == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider database is required")
	}
	if s.Cipher == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider credential cipher is required")
	}
	normalized, err := normalizeServiceConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return ServiceConfiguration{}, err
	}
	probe, err := s.VerifyServiceConfiguration(ctx, organizationID, normalized)
	if err != nil {
		return ServiceConfiguration{}, err
	}
	if probe.Outcome != servicecatalog.OutcomeOK {
		return ServiceConfiguration{}, fmt.Errorf("%w: %s", ErrServiceProbeFailed, probe.Message)
	}
	if err := s.writeServiceRevision(ctx, normalized, changedBy, probe); err != nil {
		return ServiceConfiguration{}, err
	}
	return s.GetServiceConfiguration(ctx, organizationID, normalized.Code)
}

func (s MySQLGatewayConfigStore) writeServiceRevision(ctx context.Context, input ServiceConfigurationInput, changedBy string, probe servicecatalog.Result) error {
	service, _ := servicecatalog.Find(input.Code)
	transaction, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()

	connectionID, err := lockOrCreateConnection(ctx, transaction, service, input.ExpectedVersion)
	if err != nil {
		return err
	}

	connectionRevision, err := nextRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_connection_revisions WHERE connection_id = ?`,
		connectionID, connectionID+"_r")
	if err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connection_revisions
		(id, connection_id, revision_number, base_url, created_by)
		VALUES (?, ?, ?, ?, ?)`,
		connectionRevision.id, connectionID, connectionRevision.number,
		input.Values[addressField(service)], changedBy); err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_connections
		SET current_revision_id = ?, status = 'enabled', version = version + 1,
			last_verified_at = UTC_TIMESTAMP(6), last_verification_ok = ?,
			last_verification_message = ?, last_verification_outcome = ?
		WHERE id = ?`,
		connectionRevision.id, probe.Outcome == servicecatalog.OutcomeOK,
		probe.Message, string(probe.Outcome), connectionID); err != nil {
		return err
	}

	if secret := secretField(service); secret != "" && input.Values[secret] != "" {
		if err = replaceServiceCredential(ctx, transaction, s.Cipher, connectionID, input.Values[secret]); err != nil {
			return err
		}
	}

	if service.ModelAlias != "" {
		if err = pointRouteAtRevision(ctx, transaction, service, connectionID, connectionRevision.id, input.Values["model"]); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

// lockOrCreateConnection resolves the connection by its natural key and takes a
// row lock, so two concurrent saves cannot both pass the version check.
func lockOrCreateConnection(ctx context.Context, transaction *sql.Tx, service servicecatalog.Service, expectedVersion *int64) (string, error) {
	var (
		connectionID   string
		currentVersion int64
	)
	err := transaction.QueryRowContext(ctx,
		`SELECT id, version FROM provider_connections WHERE connection_code = ? FOR UPDATE`,
		service.ConnectionCode).Scan(&connectionID, &currentVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		connectionID = generatedConnectionID(service.ConnectionCode)
		// Version starts at 0 because the update below increments it, so the
		// first stored configuration reads back as version 1.
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connections
			(id, connection_code, connection_type, current_revision_id, status, version)
			VALUES (?, ?, ?, NULL, 'enabled', 0)`,
			connectionID, service.ConnectionCode, service.ConnectionType); err != nil {
			return "", err
		}
		return connectionID, nil
	case err != nil:
		return "", err
	default:
		if expectedVersion != nil && *expectedVersion != currentVersion {
			return "", ErrServiceConfigurationConflict
		}
		return connectionID, nil
	}
}

func replaceServiceCredential(ctx context.Context, transaction *sql.Tx, cipher CredentialCipher, connectionID, secret string) error {
	ciphertext, nonce, keyVersion, err := cipher.Encrypt([]byte(secret))
	if err != nil {
		return err
	}
	// Retire rather than delete: the previous ciphertext stays auditable.
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_credentials
		SET status = 'retired', active_until = UTC_TIMESTAMP(6)
		WHERE connection_id = ? AND status = 'active'`, connectionID); err != nil {
		return err
	}
	credential, err := nextRevision(ctx, transaction,
		`SELECT COALESCE(MAX(credential_version), 0) FROM provider_credentials WHERE connection_id = ?`,
		connectionID, connectionID+"_v")
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO provider_credentials
		(id, connection_id, credential_version, ciphertext, nonce, key_version, status, active_from)
		VALUES (?, ?, ?, ?, ?, ?, 'active', UTC_TIMESTAMP(6))`,
		credential.id, connectionID, credential.number, ciphertext, nonce, keyVersion)
	return err
}

// pointRouteAtRevision is what makes a save take effect: call-time resolution
// reads the route, not the connection. The route is found by (capability,
// model_alias) because that pair is the table's unique key — inserting a fresh
// id would hit it and leave the platform on the old revision.
func pointRouteAtRevision(ctx context.Context, transaction *sql.Tx, service servicecatalog.Service, connectionID, connectionRevisionID, upstreamModel string) error {
	var routeID string
	err := transaction.QueryRowContext(ctx,
		`SELECT id FROM provider_model_routes
		WHERE capability = ? AND model_alias = ? AND organization_scope = '*' FOR UPDATE`,
		service.Capability, service.ModelAlias).Scan(&routeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		routeID = generatedRouteID(service.ModelAlias)
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_routes
			(id, organization_id, capability, model_alias, current_revision_id, status)
			VALUES (?, NULL, ?, ?, NULL, 'enabled')`,
			routeID, service.Capability, service.ModelAlias); err != nil {
			return err
		}
	case err != nil:
		return err
	}

	routeRevision, err := nextRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_model_route_revisions WHERE route_id = ?`,
		routeID, routeID+"_r")
	if err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_route_revisions
		(id, route_id, revision_number, connection_id, connection_revision_id, upstream_model)
		VALUES (?, ?, ?, ?, ?, ?)`,
		routeRevision.id, routeID, routeRevision.number, connectionID,
		connectionRevisionID, upstreamModel); err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx,
		`UPDATE provider_model_routes SET current_revision_id = ?, status = 'enabled', version = version + 1 WHERE id = ?`,
		routeRevision.id, routeID)
	return err
}
