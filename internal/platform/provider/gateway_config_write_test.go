package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type staticVideoRouteResolver struct {
	route VideoRouteSnapshot
	err   error
}

func (resolver staticVideoRouteResolver) ResolveVideoRoute(context.Context, contract.OrganizationID, string) (VideoRouteSnapshot, error) {
	return resolver.route, resolver.err
}

func TestMaskAPIKeyKeepsOnlyTheTail(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"abcd":                "****",
		"abcdefgh":            "****efgh",
		"sk-1234567890abcdef": "****cdef",
	}
	for input, want := range cases {
		if got := MaskAPIKey(input); got != want {
			t.Fatalf("MaskAPIKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeVideoConfigurationInputRejectsBadValues(t *testing.T) {
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "", Model: "m"}, false); err == nil {
		t.Fatal("empty base URL must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "https://ark.example/api/v3", Model: ""}, false); err == nil {
		t.Fatal("empty model must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "http://ark.example/api/v3", Model: "m"}, false); err == nil {
		t.Fatal("plain HTTP must be rejected unless explicitly allowed")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "http://ark.example/api/v3", Model: "m"}, true); err != nil {
		t.Fatalf("plain HTTP must be accepted when the deployment allows it: %v", err)
	}
	normalized, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: " https://ark.example/api/v3/ ", Model: " seedance "}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.BaseURL != "https://ark.example/api/v3" || normalized.Model != "seedance" {
		t.Fatalf("normalized = %+v", normalized)
	}
}

func TestGatewayManagedVideoConfigurationStoreReportsGatewayRouteWithoutCredential(t *testing.T) {
	t.Parallel()
	store := GatewayManagedVideoConfigurationStore{Routes: staticVideoRouteResolver{route: VideoRouteSnapshot{
		BaseURL:           "https://gateway.example/video",
		UpstreamModel:     "seedance-2.0",
		CredentialVersion: 7,
	}}}

	configuration, err := store.GetVideoConfiguration(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("get gateway-managed configuration: %v", err)
	}
	if !configuration.Configured || configuration.CredentialReadable || configuration.MaskedAPIKey != "" {
		t.Fatalf("unsafe gateway-managed configuration: %+v", configuration)
	}
	if configuration.BaseURL != "https://gateway.example/video" || configuration.UpstreamModel != "seedance-2.0" || configuration.Version != 7 {
		t.Fatalf("unexpected gateway-managed configuration: %+v", configuration)
	}

	if _, err := store.VerifyVideoConfiguration(context.Background(), "org_1", VideoConfigurationInput{}); !errors.Is(err, ErrVideoConfigurationManagedExternally) {
		t.Fatalf("verify error = %v, want managed-externally error", err)
	}
	if _, err := store.SaveVideoConfiguration(context.Background(), "org_1", VideoConfigurationInput{}); !errors.Is(err, ErrVideoConfigurationManagedExternally) {
		t.Fatalf("save error = %v, want managed-externally error", err)
	}
}
