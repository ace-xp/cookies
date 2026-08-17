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
