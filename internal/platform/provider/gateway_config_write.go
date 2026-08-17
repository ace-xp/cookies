package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// The Settings page owns exactly one video connection and one video route.
// These identifiers match scripts/configure-ark-video.ps1 so either path can
// overwrite the other's configuration instead of creating a duplicate.
const (
	VideoConnectionID   = "connection_ark_seedance"
	VideoConnectionCode = "ark-seedance"
	VideoRouteID        = "route_ark_seedance_video"
	VideoModelAlias     = "cookies.video.standard"
	videoCapability     = "video.generate"

	videoConnectionTimeoutSeconds = 900
	videoConnectionMaxBytes       = 209715200
)

// videoRouteConstraints mirrors the generation allowlist the PowerShell script
// writes. The Settings page does not expose these; they exist so a stored route
// still rejects unsupported durations and resolutions.
const videoRouteConstraints = `{"duration_seconds_min":4,"duration_seconds_max":15,` +
	`"aspect_ratios":["9:16","16:9","1:1"],"resolutions":["480p","720p"],` +
	`"video_input_modes":["text_only","reference_image","first_last_frame"],` +
	`"video_audio_policies":["silent","generated_audio"]}`

var (
	ErrVideoConfigurationConflict = errors.New("video configuration version conflict")
	// ErrVideoConfigurationCredentialMissing means no usable credential is
	// stored: either nothing was ever saved, or the master key changed and the
	// stored ciphertext no longer decrypts.
	ErrVideoConfigurationCredentialMissing = errors.New("video configuration has no stored credential")
	// ErrVideoConfigurationManagedExternally tells the HTTP layer that this
	// deployment deliberately exposes a read-only route managed by Adapter
	// Gateway, rather than a broken self-managed Ark configuration surface.
	ErrVideoConfigurationManagedExternally = errors.New("video configuration is managed by the adapter gateway")
)

// GatewayManagedVideoConfigurationStore makes the Settings read endpoint
// useful in Adapter Gateway deployments without ever reading or overwriting
// credentials owned by the upstream gateway.
type GatewayManagedVideoConfigurationStore struct {
	Routes VideoRouteResolver
}

func (s GatewayManagedVideoConfigurationStore) GetVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID) (VideoConfiguration, error) {
	if s.Routes == nil {
		return VideoConfiguration{}, fmt.Errorf("adapter gateway video route resolver is required")
	}
	route, err := s.Routes.ResolveVideoRoute(ctx, organizationID, VideoModelAlias)
	if err != nil {
		return VideoConfiguration{}, err
	}
	return VideoConfiguration{
		Configured: true, BaseURL: route.BaseURL, UpstreamModel: route.UpstreamModel,
		CredentialReadable: false, Version: route.CredentialVersion,
		LastVerificationMessage: "由 Adapter Gateway 管理；请在网关控制面维护凭据和路由。",
	}, nil
}

func (GatewayManagedVideoConfigurationStore) VerifyVideoConfiguration(context.Context, contract.OrganizationID, VideoConfigurationInput) (VideoProbeResult, error) {
	return VideoProbeResult{}, ErrVideoConfigurationManagedExternally
}

func (GatewayManagedVideoConfigurationStore) SaveVideoConfiguration(context.Context, contract.OrganizationID, VideoConfigurationInput) (VideoConfiguration, error) {
	return VideoConfiguration{}, ErrVideoConfigurationManagedExternally
}

// VideoConfigurationInputError carries a message written for the operator
// filling in the Settings page, so the HTTP layer can pass it through instead
// of flattening it into a generic failure.
type VideoConfigurationInputError struct{ Message string }

func (e VideoConfigurationInputError) Error() string { return e.Message }

// VideoConfiguration is what the Settings page reads back. MaskedAPIKey never
// contains the credential itself.
type VideoConfiguration struct {
	Configured              bool
	BaseURL                 string
	UpstreamModel           string
	MaskedAPIKey            string
	CredentialReadable      bool
	Version                 int64
	UpdatedAt               time.Time
	LastVerifiedAt          *time.Time
	LastVerificationOK      *bool
	LastVerificationMessage string
}

// VideoConfigurationInput is what the Settings page writes. An empty APIKey
// means "keep the stored one", so changing only the base URL does not force the
// operator to paste the credential again.
type VideoConfigurationInput struct {
	BaseURL         string
	Model           string
	APIKey          string
	Verification    VideoProbeResult
	ExpectedVersion *int64
}

// MaskAPIKey renders a credential for display. Short values are fully masked.
func MaskAPIKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return "****"
	}
	return "****" + trimmed[len(trimmed)-4:]
}

func normalizeVideoConfigurationInput(input VideoConfigurationInput, allowInsecureHTTP bool) (VideoConfigurationInput, error) {
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.BaseURL == "" {
		return VideoConfigurationInput{}, VideoConfigurationInputError{Message: "服务地址不能为空"}
	}
	if input.Model == "" {
		return VideoConfigurationInput{}, VideoConfigurationInputError{Message: "模型名不能为空"}
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || parsed.Host == "" {
		return VideoConfigurationInput{}, VideoConfigurationInputError{Message: "服务地址不是合法的 URL"}
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && allowInsecureHTTP) {
		return VideoConfigurationInput{}, VideoConfigurationInputError{Message: "服务地址必须是 HTTPS"}
	}
	return input, nil
}

// GetVideoConfiguration reads the stored configuration. A stored credential
// that no longer decrypts reports CredentialReadable false rather than failing,
// so the page can ask for a re-entry instead of showing an outage.
func (s MySQLGatewayConfigStore) GetVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID) (VideoConfiguration, error) {
	if s.DB == nil {
		return VideoConfiguration{}, fmt.Errorf("provider database is required")
	}
	var (
		config       VideoConfiguration
		ciphertext   []byte
		nonce        []byte
		keyVersion   sql.NullString
		verifiedAt   sql.NullTime
		verifiedOK   sql.NullBool
		verifiedText sql.NullString
	)
	err := s.DB.QueryRowContext(ctx, `SELECT cr.base_url, rr.upstream_model, c.version, r.updated_at,
			c.last_verified_at, c.last_verification_ok, c.last_verification_message,
			pc.ciphertext, pc.nonce, pc.key_version
		FROM provider_model_routes r
		JOIN provider_model_route_revisions rr ON rr.id = r.current_revision_id
		JOIN provider_connections c ON c.id = rr.connection_id
		JOIN provider_connection_revisions cr ON cr.id = rr.connection_revision_id
		LEFT JOIN provider_credentials pc ON pc.connection_id = c.id AND pc.status = 'active'
			AND pc.active_from <= UTC_TIMESTAMP(6)
			AND (pc.active_until IS NULL OR pc.active_until > UTC_TIMESTAMP(6))
		WHERE r.id = ? AND r.status = 'enabled' AND c.status = 'enabled'
		ORDER BY pc.credential_version DESC
		LIMIT 1`, VideoRouteID).Scan(
		&config.BaseURL, &config.UpstreamModel, &config.Version, &config.UpdatedAt,
		&verifiedAt, &verifiedOK, &verifiedText, &ciphertext, &nonce, &keyVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VideoConfiguration{}, nil
	}
	if err != nil {
		return VideoConfiguration{}, err
	}
	config.Configured = true
	if verifiedAt.Valid {
		instant := verifiedAt.Time
		config.LastVerifiedAt = &instant
	}
	if verifiedOK.Valid {
		outcome := verifiedOK.Bool
		config.LastVerificationOK = &outcome
	}
	config.LastVerificationMessage = verifiedText.String
	if len(ciphertext) > 0 && s.Cipher != nil {
		plaintext, decryptErr := s.Cipher.Decrypt(ciphertext, nonce, keyVersion.String)
		if decryptErr == nil {
			config.CredentialReadable = true
			config.MaskedAPIKey = MaskAPIKey(string(plaintext))
		}
	}
	return config, nil
}

// ResolveVideoAPIKey returns the stored plaintext credential. It exists only so
// a save that omits the API key can still be probed; the value must never leave
// the process.
func (s MySQLGatewayConfigStore) ResolveVideoAPIKey(ctx context.Context, organizationID contract.OrganizationID) (string, error) {
	if s.DB == nil {
		return "", fmt.Errorf("provider database is required")
	}
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
		ORDER BY credential_version DESC LIMIT 1`, VideoConnectionID).Scan(&ciphertext, &nonce, &keyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVideoConfigurationCredentialMissing
	}
	if err != nil {
		return "", err
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, nonce, keyVersion)
	if err != nil {
		return "", ErrVideoConfigurationCredentialMissing
	}
	return string(plaintext), nil
}

// VerifyVideoConfiguration probes a candidate configuration without writing
// anything. An empty APIKey falls back to the stored credential, so an operator
// can re-test an existing setup without pasting the key again.
func (s MySQLGatewayConfigStore) VerifyVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID, input VideoConfigurationInput) (VideoProbeResult, error) {
	normalized, err := normalizeVideoConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return VideoProbeResult{}, err
	}
	apiKey := normalized.APIKey
	if apiKey == "" {
		stored, resolveErr := s.ResolveVideoAPIKey(ctx, organizationID)
		if resolveErr != nil {
			return VideoProbeResult{}, resolveErr
		}
		apiKey = stored
	}
	return ProbeArkVideoCredential(ctx, nil, normalized.BaseURL, apiKey), nil
}

// SaveVideoConfiguration writes one revision of everything the video route
// needs. Every table is append-only: revisions accumulate and a replaced
// credential is retired rather than deleted.
func (s MySQLGatewayConfigStore) SaveVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID, input VideoConfigurationInput) (VideoConfiguration, error) {
	if s.DB == nil {
		return VideoConfiguration{}, fmt.Errorf("provider database is required")
	}
	if s.Cipher == nil {
		return VideoConfiguration{}, fmt.Errorf("provider credential cipher is required")
	}
	normalized, err := normalizeVideoConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return VideoConfiguration{}, err
	}
	transaction, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return VideoConfiguration{}, err
	}
	defer func() { _ = transaction.Rollback() }()

	var currentVersion int64
	err = transaction.QueryRowContext(ctx, `SELECT version FROM provider_connections WHERE id = ? FOR UPDATE`, VideoConnectionID).Scan(&currentVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connections (id, connection_code, connection_type, current_revision_id, status, version)
			VALUES (?, ?, 'ark', NULL, 'enabled', 1)`, VideoConnectionID, VideoConnectionCode); err != nil {
			return VideoConfiguration{}, err
		}
	case err != nil:
		return VideoConfiguration{}, err
	default:
		if normalized.ExpectedVersion != nil && *normalized.ExpectedVersion != currentVersion {
			return VideoConfiguration{}, ErrVideoConfigurationConflict
		}
	}

	connectionRevision, err := nextRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_connection_revisions WHERE connection_id = ?`,
		VideoConnectionID, "connection_ark_seedance_r")
	if err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connection_revisions
		(id, connection_id, revision_number, base_url, timeout_seconds, max_response_bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		connectionRevision.id, VideoConnectionID, connectionRevision.number, normalized.BaseURL,
		videoConnectionTimeoutSeconds, videoConnectionMaxBytes); err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_connections
		SET current_revision_id = ?, status = 'enabled', version = version + 1,
			last_verified_at = UTC_TIMESTAMP(6), last_verification_ok = ?, last_verification_message = ?
		WHERE id = ?`, connectionRevision.id, normalized.Verification.OK(),
		normalized.Verification.Message, VideoConnectionID); err != nil {
		return VideoConfiguration{}, err
	}

	if normalized.APIKey != "" {
		ciphertext, nonce, keyVersion, encryptErr := s.Cipher.Encrypt([]byte(normalized.APIKey))
		if encryptErr != nil {
			return VideoConfiguration{}, encryptErr
		}
		if _, err = transaction.ExecContext(ctx, `UPDATE provider_credentials
			SET status = 'retired', active_until = UTC_TIMESTAMP(6)
			WHERE connection_id = ? AND status = 'active'`, VideoConnectionID); err != nil {
			return VideoConfiguration{}, err
		}
		credential, credentialErr := nextRevision(ctx, transaction,
			`SELECT COALESCE(MAX(credential_version), 0) FROM provider_credentials WHERE connection_id = ?`,
			VideoConnectionID, "credential_ark_seedance_v")
		if credentialErr != nil {
			return VideoConfiguration{}, credentialErr
		}
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_credentials
			(id, connection_id, credential_version, ciphertext, nonce, key_version, status, active_from)
			VALUES (?, ?, ?, ?, ?, ?, 'active', UTC_TIMESTAMP(6))`,
			credential.id, VideoConnectionID, credential.number, ciphertext, nonce, keyVersion); err != nil {
			return VideoConfiguration{}, err
		}
	}

	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_routes
		(id, organization_id, capability, model_alias, current_revision_id, status)
		VALUES (?, NULL, ?, ?, NULL, 'enabled')
		ON DUPLICATE KEY UPDATE status = 'enabled', version = version + 1`,
		VideoRouteID, videoCapability, VideoModelAlias); err != nil {
		return VideoConfiguration{}, err
	}
	routeRevision, err := nextRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_model_route_revisions WHERE route_id = ?`,
		VideoRouteID, "route_ark_seedance_video_r")
	if err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_route_revisions
		(id, route_id, revision_number, connection_id, connection_revision_id, upstream_model, constraints_json)
		VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`,
		routeRevision.id, VideoRouteID, routeRevision.number, VideoConnectionID,
		connectionRevision.id, normalized.Model, videoRouteConstraints); err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_model_routes SET current_revision_id = ? WHERE id = ?`,
		routeRevision.id, VideoRouteID); err != nil {
		return VideoConfiguration{}, err
	}
	if err = transaction.Commit(); err != nil {
		return VideoConfiguration{}, err
	}
	return s.GetVideoConfiguration(ctx, organizationID)
}

type revisionID struct {
	id     string
	number int64
}

// nextRevision allocates the next revision number for an append-only table.
// Shared by the video configuration and the general service configuration, so
// both number their revisions the same way.
func nextRevision(ctx context.Context, transaction *sql.Tx, query, parent, prefix string) (revisionID, error) {
	var current int64
	if err := transaction.QueryRowContext(ctx, query, parent).Scan(&current); err != nil {
		return revisionID{}, err
	}
	next := current + 1
	return revisionID{id: fmt.Sprintf("%s%d", prefix, next), number: next}, nil
}
