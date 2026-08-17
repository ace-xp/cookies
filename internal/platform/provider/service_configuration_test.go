package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestServiceConfigurationInputRejectsUnknownCode(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(
		ServiceConfigurationInput{Code: "model.nonexistent"}, false)
	if err == nil {
		t.Fatal("an unknown catalog code must be rejected before touching the database")
	}
}

// Read-only entries exist in the catalog so the page can show them. Accepting a
// write for one would silently do nothing, which reads to the operator as a
// successful save.
func TestServiceConfigurationInputRejectsReadOnlyService(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(
		ServiceConfigurationInput{Code: "storage.tos"}, false)
	if err == nil {
		t.Fatal("a read-only service must not accept a write")
	}
}

func TestServiceConfigurationInputRequiresDeclaredRequiredFields(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(
		ServiceConfigurationInput{Code: "model.text", Values: map[string]string{"model": "doubao-x"}}, false)
	if err == nil {
		t.Fatal("base_url is declared required and must be enforced")
	}
	if !strings.Contains(err.Error(), "服务地址") {
		t.Fatalf("the error must name the field in Chinese, got %q", err.Error())
	}
}

func TestServiceConfigurationInputRejectsPlainHTTP(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "http://ark.example.com", "model": "doubao-x", "api_key": "k"},
	}, false)
	if err == nil {
		t.Fatal("plain HTTP must be rejected unless explicitly allowed")
	}
}

func TestServiceConfigurationInputAllowsPlainHTTPWhenPolicyAllows(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "http://127.0.0.1:8000", "model": "doubao-x", "api_key": "k"},
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.Values["base_url"] != "http://127.0.0.1:8000" {
		t.Fatalf("base_url was altered: %q", normalized.Values["base_url"])
	}
}

// A pasted address often carries a trailing slash. Storing both forms would
// produce two connections that look identical on the page.
func TestServiceConfigurationInputTrimsTrailingSlash(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "  https://ark.example.com/api/v3/  ", "model": "doubao-x"},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.Values["base_url"] != "https://ark.example.com/api/v3" {
		t.Fatalf("address was not normalized: %q", normalized.Values["base_url"])
	}
}

func TestServiceConfigurationInputRejectsAddressWithoutHost(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "not a url", "model": "doubao-x"},
	}, false)
	if err == nil {
		t.Fatal("an address with no host must be rejected")
	}
}

// An omitted secret means "keep the stored one", so the operator can change a
// base URL without pasting the credential again.
func TestServiceConfigurationInputKeepsOmittedSecretEmpty(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := normalized.Values["api_key"]; present {
		t.Fatal("an omitted secret must stay omitted, not become an empty credential")
	}
}

// A submitted blank is not a request to erase the credential — the form sends
// the field either way, and erasing on blank would break every save that only
// meant to change the address.
func TestServiceConfigurationInputTreatsBlankSecretAsOmitted(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "   "},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := normalized.Values["api_key"]; present {
		t.Fatal("a blank secret must be treated as omitted")
	}
}

// Miyun names its fields endpoint/session_cookie rather than base_url/api_key.
// The same validation has to reach them, or its address would go unchecked.
func TestServiceConfigurationInputValidatesMiyunAddressField(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "miyun",
		Values: map[string]string{"endpoint": "http://miyun.example.com"},
	}, false)
	if err == nil {
		t.Fatal("miyun's endpoint field must go through the same address validation")
	}
}

// Anything the catalog does not declare is dropped rather than stored, so a
// crafted request cannot smuggle an extra key into the connection config.
func TestServiceConfigurationInputDropsUndeclaredFields(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code: "model.text",
		Values: map[string]string{
			"base_url": "https://ark.example.com", "model": "doubao-x",
			"organization_id": "someone-elses-org",
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := normalized.Values["organization_id"]; present {
		t.Fatal("an undeclared field must not survive normalization")
	}
}

func TestServiceConfigurationInputPreservesExpectedVersion(t *testing.T) {
	version := int64(7)
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
		ExpectedVersion: &version,
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.ExpectedVersion == nil || *normalized.ExpectedVersion != 7 {
		t.Fatal("the expected version must survive normalization or the conflict check is lost")
	}
}

func TestServiceConfigurationViewMasksSecrets(t *testing.T) {
	config := ServiceConfiguration{
		Code:          "model.text",
		Values:        map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
		MaskedSecrets: map[string]string{"api_key": MaskAPIKey("sk-1234567890abcd")},
	}
	for _, value := range config.Values {
		if strings.Contains(value, "sk-1234567890abcd") {
			t.Fatal("the plaintext credential must never appear in Values")
		}
	}
	if config.MaskedSecrets["api_key"] == "sk-1234567890abcd" {
		t.Fatal("MaskedSecrets must hold a mask, not the key")
	}
}

func TestServiceConfigurationConflictIsDistinguishable(t *testing.T) {
	if !errors.Is(ErrServiceConfigurationConflict, ErrServiceConfigurationConflict) {
		t.Fatal("sentinel error must be comparable")
	}
	if errors.Is(ErrServiceConfigurationConflict, ErrServiceProbeFailed) {
		t.Fatal("a version conflict and a failed probe need different handling and must not compare equal")
	}
}
