package httpserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shikanon/cookies/internal/platform/provider"
	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

func servicecatalogTestService(t *testing.T) servicecatalog.Service {
	t.Helper()
	service, found := servicecatalog.Find("model.text")
	if !found {
		t.Fatal("model.text must be declared in the catalog")
	}
	return service
}

func servicecatalogReadOnlyTestService(t *testing.T) servicecatalog.Service {
	t.Helper()
	service, found := servicecatalog.Find("storage.tos")
	if !found {
		t.Fatal("storage.tos must be declared in the catalog")
	}
	return service
}

func testServiceConfiguration(secret string) provider.ServiceConfiguration {
	config := provider.ServiceConfiguration{
		Code:          "model.text",
		Configured:    true,
		Values:        map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
		MaskedSecrets: map[string]string{},
		Version:       3,
	}
	if secret != "" {
		config.MaskedSecrets["api_key"] = provider.MaskAPIKey(secret)
		config.CredentialReadable = true
	}
	return config
}

func TestServiceListViewNeverCarriesPlaintextSecret(t *testing.T) {
	view := serviceConfigurationView(
		servicecatalogTestService(t),
		testServiceConfiguration("sk-1234567890abcd"),
	)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	if strings.Contains(string(encoded), "sk-1234567890abcd") {
		t.Fatalf("the plaintext credential leaked into the response: %s", encoded)
	}
}

func TestServiceListViewReportsTierAndImpact(t *testing.T) {
	view := serviceConfigurationView(servicecatalogTestService(t), testServiceConfiguration(""))
	if view["tier"] != "editable" {
		t.Fatalf("expected tier editable, got %v", view["tier"])
	}
	if view["impact"] == "" || view["impact"] == nil {
		t.Fatal("impact is what tells the operator what breaks; it must be present")
	}
}

func TestServiceListViewReportsEnvKeysForReadOnlyTier(t *testing.T) {
	view := serviceConfigurationView(servicecatalogReadOnlyTestService(t), testServiceConfiguration(""))
	keys, ok := view["env_keys"].([]string)
	if !ok || len(keys) == 0 {
		t.Fatal("a read-only service must tell the operator which env keys to edit")
	}
	if view["restart_required"] != true {
		t.Fatal("a read-only service read at boot must say a restart is needed")
	}
}

// The store wraps the diagnosis in a sentinel. Showing err.Error() verbatim
// would put "service probe failed: " in front of the sentence the operator is
// meant to act on.
func TestProbeFailureMessageDropsTheSentinelPrefix(t *testing.T) {
	wrapped := probeFailureMessage(
		fmt.Errorf("%w: %s", provider.ErrServiceProbeFailed, "密钥无效或已过期，请到服务商后台重新签发"))
	if wrapped != "密钥无效或已过期，请到服务商后台重新签发" {
		t.Fatalf("the sentinel prefix reached the operator: %q", wrapped)
	}
}
