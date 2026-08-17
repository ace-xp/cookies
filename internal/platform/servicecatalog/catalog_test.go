package servicecatalog

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllServicesHaveRequiredFields(t *testing.T) {
	services := All()
	if len(services) != 16 {
		t.Fatalf("expected 16 catalog entries, got %d", len(services))
	}
	seen := map[string]bool{}
	for _, service := range services {
		if seen[service.Code] {
			t.Fatalf("duplicate service code %q", service.Code)
		}
		seen[service.Code] = true
		if strings.TrimSpace(service.DisplayName) == "" {
			t.Errorf("%s: DisplayName is required", service.Code)
		}
		if strings.TrimSpace(service.Impact) == "" {
			t.Errorf("%s: Impact is required so the page can say what breaks", service.Code)
		}
		if service.Tier != TierEditable && service.Tier != TierReadOnly {
			t.Errorf("%s: Tier must be editable or readonly, got %q", service.Code, service.Tier)
		}
		if service.Tier == TierEditable && len(service.Fields) == 0 {
			t.Errorf("%s: editable services must declare fields", service.Code)
		}
	}
}

// Editable services are written into provider_connections, so each needs its
// own connection row and a route to resolve at call time. Sharing a connection
// code between two entries would make one silently overwrite the other.
func TestEditableServicesDeclareDistinctConnections(t *testing.T) {
	seen := map[string]string{}
	for _, service := range All() {
		if service.Tier != TierEditable {
			continue
		}
		if strings.TrimSpace(service.ConnectionCode) == "" {
			t.Errorf("%s: editable services need a connection code", service.Code)
			continue
		}
		if owner, taken := seen[service.ConnectionCode]; taken {
			t.Errorf("%s and %s both claim connection code %q", owner, service.Code, service.ConnectionCode)
		}
		seen[service.ConnectionCode] = service.Code
	}
}

// Every editable service has exactly one address field and at most one secret.
// The storage layer maps those two onto fixed columns, so a third variant
// would silently lose data.
func TestEditableServicesHaveOneAddressAndAtMostOneSecret(t *testing.T) {
	for _, service := range All() {
		if service.Tier != TierEditable {
			continue
		}
		addresses, secrets := 0, 0
		for _, field := range service.Fields {
			if field.IsAddress() {
				addresses++
			}
			if field.Kind == FieldSecret {
				secrets++
			}
		}
		if addresses != 1 {
			t.Errorf("%s: expected exactly one address field, got %d", service.Code, addresses)
		}
		if secrets > 1 {
			t.Errorf("%s: expected at most one secret field, got %d", service.Code, secrets)
		}
	}
}

func TestFindReturnsDeclaredService(t *testing.T) {
	service, ok := Find("model.text")
	if !ok {
		t.Fatal("model.text must be declared")
	}
	if service.Capability != "text.generate" {
		t.Fatalf("expected capability text.generate, got %q", service.Capability)
	}
}

func TestFindRejectsUnknownCode(t *testing.T) {
	if _, ok := Find("model.nonexistent"); ok {
		t.Fatal("unknown code must not resolve")
	}
}

// TestEveryEnvKeyIsAccountedFor is the guard against adding a new external
// dependency and forgetting to register it. Every key in .env.example must
// either belong to a catalog entry or be listed as exempt (database, admin
// account, master keys, local paths, feature flags).
func TestEveryEnvKeyIsAccountedFor(t *testing.T) {
	registered := map[string]bool{}
	for _, service := range All() {
		for _, key := range service.EnvKeys {
			if owner, taken := registered[key]; taken && owner {
				t.Errorf("%s is claimed by more than one catalog entry", key)
			}
			registered[key] = true
		}
	}
	for _, key := range ExemptEnvKeys() {
		registered[key] = true
	}
	for _, key := range readEnvExampleKeys(t) {
		if !registered[key] {
			t.Errorf("%s is read from the environment but belongs to no catalog entry and is not exempt", key)
		}
	}
}

// The mirror of the test above: a catalog entry must not point at a variable
// that no longer exists, or the page would tell the operator to edit a key
// that does nothing.
func TestCatalogDoesNotReferenceUnknownEnvKeys(t *testing.T) {
	known := map[string]bool{}
	for _, key := range readEnvExampleKeys(t) {
		known[key] = true
	}
	for _, service := range All() {
		for _, key := range service.EnvKeys {
			if !known[key] {
				t.Errorf("%s references %s, which is not in .env.example", service.Code, key)
			}
		}
	}
}

func readEnvExampleKeys(t *testing.T) []string {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("open .env.example: %v", err)
	}
	defer file.Close()
	keys := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		keys = append(keys, strings.TrimSpace(name))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan .env.example: %v", err)
	}
	return keys
}

// A model alias that no caller resolves is worse than a missing one: the page
// would happily save a route, report success, and change nothing. This caught
// two invented aliases (cookies.document.standard, cookies.research.standard)
// that the platform never asks for — the real names carry an extra segment.
func TestEveryModelAliasIsResolvedSomewhere(t *testing.T) {
	sources := goSourcesOutsideCatalog(t)
	for _, service := range All() {
		if service.ModelAlias == "" {
			continue
		}
		quoted := `"` + service.ModelAlias + `"`
		found := false
		for _, source := range sources {
			if strings.Contains(source, quoted) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s declares alias %s, which no code outside the catalog resolves — saving it would change nothing",
				service.Code, service.ModelAlias)
		}
	}
}

func goSourcesOutsideCatalog(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "internal")
	sources := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "servicecatalog" {
			return fs.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sources = append(sources, string(content))
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal tree: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("found no Go sources to check against")
	}
	return sources
}
