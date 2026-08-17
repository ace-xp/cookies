package provider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

func newServiceConfigurationTestStore(t *testing.T) MySQLGatewayConfigStore {
	t.Helper()
	dsn := os.Getenv("COOKIES_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COOKIES_TEST_MYSQL_DSN is not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping MySQL: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x35}, 32))
	cipher, err := NewAESGCMCredentialCipher(key, "service-test-v1")
	if err != nil {
		t.Fatal(err)
	}
	cleanServiceConfigurationRows(t, db)
	t.Cleanup(func() { cleanServiceConfigurationRows(t, db) })
	return MySQLGatewayConfigStore{
		DB: db, Cipher: cipher,
		// Probing a real upstream would make these tests depend on the network
		// and on someone's credential still being valid.
		ServiceProber: func(context.Context, string, string, string) servicecatalog.Result {
			return servicecatalog.Result{Outcome: servicecatalog.OutcomeOK, Message: "test probe"}
		},
	}
}

// Only the rows the catalog owns are removed. Deleting everything would wipe a
// developer's locally configured providers.
func cleanServiceConfigurationRows(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, service := range servicecatalog.All() {
		if service.Tier != servicecatalog.TierEditable {
			continue
		}
		if service.ModelAlias != "" {
			_, _ = db.Exec(`UPDATE provider_model_routes SET current_revision_id = NULL WHERE model_alias = ?`, service.ModelAlias)
			_, _ = db.Exec(`DELETE FROM provider_model_route_revisions WHERE route_id IN
				(SELECT id FROM provider_model_routes WHERE model_alias = ?)`, service.ModelAlias)
			_, _ = db.Exec(`DELETE FROM provider_model_routes WHERE model_alias = ?`, service.ModelAlias)
		}
		code := service.ConnectionCode
		_, _ = db.Exec(`DELETE FROM provider_credentials WHERE connection_id IN
			(SELECT id FROM provider_connections WHERE connection_code = ?)`, code)
		_, _ = db.Exec(`UPDATE provider_connections SET current_revision_id = NULL WHERE connection_code = ?`, code)
		_, _ = db.Exec(`DELETE FROM provider_connection_revisions WHERE connection_id IN
			(SELECT id FROM provider_connections WHERE connection_code = ?)`, code)
		_, _ = db.Exec(`DELETE FROM provider_connections WHERE connection_code = ?`, code)
	}
}

func TestSaveServiceConfigurationWritesRevisionAndKeepsOmittedSecret(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	first, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-first-credential"},
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("expected version 1, got %d", first.Version)
	}
	if first.MaskedSecrets["api_key"] == "sk-first-credential" {
		t.Fatal("the response must carry a mask, not the key")
	}
	if !strings.HasSuffix(first.MaskedSecrets["api_key"], "tial") {
		t.Fatalf("mask should keep the last four characters, got %q", first.MaskedSecrets["api_key"])
	}

	expected := first.Version
	second, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-y"},
		ExpectedVersion: &expected,
	})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("expected version 2, got %d", second.Version)
	}
	if !second.CredentialReadable {
		t.Fatal("omitting the secret must keep the stored credential usable")
	}
	if second.Values["model"] != "doubao-y" {
		t.Fatalf("model was not updated: %q", second.Values["model"])
	}
}

func TestSaveServiceConfigurationRejectsStaleVersion(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	if _, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-a"},
	}); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	stale := int64(999)
	_, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-z"},
		ExpectedVersion: &stale,
	})
	if !errors.Is(err, ErrServiceConfigurationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestGetServiceConfigurationReportsUnconfigured(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	config, err := store.GetServiceConfiguration(t.Context(), "org_local", "model.image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Configured {
		t.Fatal("a service that was never saved must report Configured=false")
	}
}

// Configuration that cannot be reached is not written. A typo that takes a
// capability down until someone notices is exactly what this prevents.
func TestSaveServiceConfigurationDoesNotWriteWhenProbeFails(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	store.ServiceProber = func(context.Context, string, string, string) servicecatalog.Result {
		return servicecatalog.Result{Outcome: servicecatalog.OutcomeAuthFailed, Message: "密钥无效"}
	}
	_, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-bad"},
	})
	if !errors.Is(err, ErrServiceProbeFailed) {
		t.Fatalf("expected probe failure, got %v", err)
	}
	config, getErr := store.GetServiceConfiguration(t.Context(), "org_local", "model.text")
	if getErr != nil {
		t.Fatalf("unexpected error: %v", getErr)
	}
	if config.Configured {
		t.Fatal("a failed probe must leave nothing behind")
	}
}

// Spec 4.6: the revision records who made the change. The credential is not
// part of that record.
func TestSaveServiceConfigurationRecordsWhoChangedIt(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	if _, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-audit"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	var createdBy string
	err := store.DB.QueryRowContext(t.Context(), `SELECT revision.created_by
		FROM provider_connection_revisions revision
		JOIN provider_connections connection ON connection.id = revision.connection_id
		WHERE connection.connection_code = 'ark-text'
		ORDER BY revision.revision_number DESC LIMIT 1`).Scan(&createdBy)
	if err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if createdBy != "operator@example.com" {
		t.Fatalf("expected the operator to be recorded, got %q", createdBy)
	}
}

// The connections and routes on a live deployment were seeded by the
// configure-* scripts with their own primary keys. Deriving an id from the
// catalog code instead of resolving the existing row would either collide on
// the connection_code unique key or write a second route the platform never
// reads — a save that reports success and changes nothing.
func TestSaveServiceConfigurationAdoptsRowsSeededByScripts(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	if _, err := store.DB.ExecContext(t.Context(), `INSERT INTO provider_connections
		(id, connection_code, connection_type, current_revision_id, status, version)
		VALUES ('connection_seeded_by_script', 'ark-text', 'ark', NULL, 'enabled', 5)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	expected := int64(5)
	config, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-adopt"},
		ExpectedVersion: &expected,
	})
	if err != nil {
		t.Fatalf("save onto seeded row: %v", err)
	}
	if config.Version != 6 {
		t.Fatalf("expected the seeded row's version to advance to 6, got %d", config.Version)
	}
	var rows int
	if err := store.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM provider_connections WHERE connection_code = 'ark-text'`).Scan(&rows); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected the seeded connection to be adopted, found %d rows for ark-text", rows)
	}
}

// The route is what call-time resolution reads. Writing a connection without
// pointing the route at it would leave the platform on the old configuration —
// the save would look successful and change nothing.
func TestSaveServiceConfigurationPointsRouteAtNewRevision(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	if _, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-route"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	var upstreamModel, baseURL string
	if err := store.DB.QueryRowContext(t.Context(), `SELECT route_revision.upstream_model, connection_revision.base_url
		FROM provider_model_routes route
		JOIN provider_model_route_revisions route_revision ON route_revision.id = route.current_revision_id
		JOIN provider_connection_revisions connection_revision ON connection_revision.id = route_revision.connection_revision_id
		WHERE route.model_alias = 'cookies.text.standard'`).Scan(&upstreamModel, &baseURL); err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	if upstreamModel != "doubao-x" || baseURL != "https://ark.example.com" {
		t.Fatalf("the route does not point at what was just saved: model=%q base_url=%q", upstreamModel, baseURL)
	}
}

// sharedGatewayCode names a connection no catalog entry claims, which is the
// point: it stands for the shared adapter a live deployment routes several
// capabilities through.
const sharedGatewayCode = "shared-gateway-test"

// seedSharedGateway builds the shape the 8091 environment is in — one
// connection serving a capability whose catalog entry names a different
// connection entirely — so the tests below exercise the real arrangement
// rather than the one the catalog would have created on a blank database.
func seedSharedGateway(t *testing.T, db *sql.DB, code, baseURL, upstreamModel string) servicecatalog.Service {
	t.Helper()
	service, found := servicecatalog.Find(code)
	if !found {
		t.Fatalf("unknown service %q", code)
	}
	cleanSharedGateway(t, db, service)
	t.Cleanup(func() { cleanSharedGateway(t, db, service) })

	revisionID := sharedGatewayCode + "_r1"
	routeID := generatedRouteID(service.ModelAlias)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO provider_connections (id, connection_code, connection_type, current_revision_id, status, version)
			VALUES (?, ?, ?, NULL, 'enabled', 1)`, []any{sharedGatewayCode, sharedGatewayCode, service.ConnectionType}},
		{`INSERT INTO provider_connection_revisions (id, connection_id, revision_number, base_url, created_by)
			VALUES (?, ?, 1, ?, 'seed')`, []any{revisionID, sharedGatewayCode, baseURL}},
		{`UPDATE provider_connections SET current_revision_id = ? WHERE id = ?`, []any{revisionID, sharedGatewayCode}},
		{`INSERT INTO provider_model_routes (id, organization_id, capability, model_alias, current_revision_id, status)
			VALUES (?, NULL, ?, ?, NULL, 'enabled')`, []any{routeID, service.Capability, service.ModelAlias}},
		{`INSERT INTO provider_model_route_revisions
			(id, route_id, revision_number, connection_id, connection_revision_id, upstream_model)
			VALUES (?, ?, 1, ?, ?, ?)`, []any{routeID + "_r1", routeID, sharedGatewayCode, revisionID, upstreamModel}},
		{`UPDATE provider_model_routes SET current_revision_id = ? WHERE id = ?`, []any{routeID + "_r1", routeID}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed shared gateway: %v", err)
		}
	}
	return service
}

func cleanSharedGateway(t *testing.T, db *sql.DB, service servicecatalog.Service) {
	t.Helper()
	_, _ = db.Exec(`UPDATE provider_model_routes SET current_revision_id = NULL WHERE model_alias = ?`, service.ModelAlias)
	_, _ = db.Exec(`DELETE FROM provider_model_route_revisions WHERE connection_id = ?`, sharedGatewayCode)
	_, _ = db.Exec(`DELETE FROM provider_model_routes WHERE model_alias = ?`, service.ModelAlias)
	_, _ = db.Exec(`DELETE FROM provider_credentials WHERE connection_id = ?`, sharedGatewayCode)
	_, _ = db.Exec(`UPDATE provider_connections SET current_revision_id = NULL WHERE id = ?`, sharedGatewayCode)
	_, _ = db.Exec(`DELETE FROM provider_connection_revisions WHERE connection_id = ?`, sharedGatewayCode)
	_, _ = db.Exec(`DELETE FROM provider_connections WHERE id = ?`, sharedGatewayCode)
}

// Reading by the catalog's connection code reports a capability that is live
// and serving traffic as 未配置, because its route points somewhere else. The
// page has to follow the route.
func TestGetServiceConfigurationFollowsTheRouteToASharedConnection(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	seedSharedGateway(t, store.DB, "model.text", "https://shared.example.com/v1", "doubao-shared")

	config, err := store.GetServiceConfiguration(t.Context(), "org_local", "model.text")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !config.Configured {
		t.Fatal("a capability routed through a shared gateway is configured, not blank")
	}
	if config.Values["base_url"] != "https://shared.example.com/v1" {
		t.Fatalf("expected the shared gateway's address, got %q", config.Values["base_url"])
	}
	if config.Values["model"] != "doubao-shared" {
		t.Fatalf("expected the routed model, got %q", config.Values["model"])
	}
}

// Saving onto a shared gateway must edit that gateway. Creating the catalog's
// own connection instead would write a row the router never reads: the save
// reports success and the platform keeps calling the old address.
func TestSaveServiceConfigurationEditsTheSharedConnectionRatherThanForkingOne(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	service := seedSharedGateway(t, store.DB, "model.text", "https://shared.example.com/v1", "doubao-shared")

	saved, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://shared.example.com/v2", "model": "doubao-next", "api_key": "sk-shared"},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Values["base_url"] != "https://shared.example.com/v2" {
		t.Fatalf("the save did not take on the shared gateway: %q", saved.Values["base_url"])
	}

	var forked int
	if err := store.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM provider_connections WHERE connection_code = ?`, service.ConnectionCode).Scan(&forked); err != nil {
		t.Fatalf("count forked connections: %v", err)
	}
	if forked != 0 {
		t.Fatalf("the save forked %q, a connection the router never reads", service.ConnectionCode)
	}

	var routedTo string
	if err := store.DB.QueryRowContext(t.Context(), `SELECT connection.connection_code
		FROM provider_model_routes route
		JOIN provider_model_route_revisions route_revision ON route_revision.id = route.current_revision_id
		JOIN provider_connections connection ON connection.id = route_revision.connection_id
		WHERE route.model_alias = ?`, service.ModelAlias).Scan(&routedTo); err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	if routedTo != sharedGatewayCode {
		t.Fatalf("the route moved off the shared gateway to %q", routedTo)
	}
}

// The stored key lives on the connection the capability is actually on. Looking
// it up by the catalog's code finds nothing, and the page then demands a
// re-entry of a key that is already saved and working.
func TestVerifyServiceConfigurationUsesTheSharedConnectionsStoredKey(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	seedSharedGateway(t, store.DB, "model.text", "https://shared.example.com/v1", "doubao-shared")
	if _, err := store.SaveServiceConfiguration(t.Context(), "org_local", "operator@example.com", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://shared.example.com/v1", "model": "doubao-shared", "api_key": "sk-already-stored"},
	}); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	var probedSecret string
	store.ServiceProber = func(_ context.Context, _, _, secret string) servicecatalog.Result {
		probedSecret = secret
		return servicecatalog.Result{Outcome: servicecatalog.OutcomeOK, Message: "test probe"}
	}
	if _, err := store.VerifyServiceConfiguration(t.Context(), "org_local", ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://shared.example.com/v1", "model": "doubao-shared"},
	}); err != nil {
		t.Fatalf("verify with an omitted secret: %v", err)
	}
	if probedSecret != "sk-already-stored" {
		t.Fatalf("expected the shared gateway's stored key, got %q", probedSecret)
	}
}
