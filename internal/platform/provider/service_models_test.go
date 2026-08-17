package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

func TestListUpstreamModelsReturnsSortedIdentifiers(t *testing.T) {
	var seenPath, seenAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath, seenAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		// The duplicate and the "model"-named entry are both real shapes seen
		// from gateways that front several vendors.
		_, _ = w.Write([]byte(`{"data":[
			{"id":"doubao-seedream-5-0-pro-260628"},
			{"id":"doubao-seed-2-1-pro-260628"},
			{"id":"doubao-seed-2-1-pro-260628"},
			{"model":"doubao-seedance-2-0-fast-260128"},
			{"id":"  "}
		]}`))
	}))
	defer upstream.Close()

	models, result := ListUpstreamModels(context.Background(), "model.text", upstream.URL+"/v1", "sk-test")
	if result.Outcome != servicecatalog.OutcomeOK {
		t.Fatalf("expected ok, got %q (%s)", result.Outcome, result.Message)
	}
	if seenPath != "/v1/models" {
		t.Errorf("expected the probe path /v1/models, got %q", seenPath)
	}
	if seenAuth != "Bearer sk-test" {
		t.Errorf("expected the credential to be sent, got %q", seenAuth)
	}
	want := []string{
		"doubao-seed-2-1-pro-260628",
		"doubao-seedance-2-0-fast-260128",
		"doubao-seedream-5-0-pro-260628",
	}
	if len(models) != len(want) {
		t.Fatalf("expected %v, got %v", want, models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, models)
		}
	}
}

// A bad key must read as a bad key, not as "this service has no models". The
// two are fixed by different actions, so they cannot share a message.
func TestListUpstreamModelsReportsAuthFailureRatherThanAnEmptyList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer upstream.Close()

	models, result := ListUpstreamModels(context.Background(), "model.text", upstream.URL+"/v1", "sk-wrong")
	if result.Outcome != servicecatalog.OutcomeAuthFailed {
		t.Fatalf("expected auth_failed, got %q", result.Outcome)
	}
	if result.UpstreamMessage != "invalid api key" {
		t.Errorf("expected the upstream's own words, got %q", result.UpstreamMessage)
	}
	if len(models) != 0 {
		t.Errorf("a failed read must not report models, got %v", models)
	}
}

// A live service that simply does not publish a list is still healthy; the page
// says so and keeps the text box usable.
func TestListUpstreamModelsAcceptsAnUpstreamWithNoList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	models, result := ListUpstreamModels(context.Background(), "model.image", upstream.URL+"/v1", "sk-test")
	if result.Outcome != servicecatalog.OutcomeOK {
		t.Fatalf("expected ok, got %q", result.Outcome)
	}
	if len(models) != 0 {
		t.Errorf("expected no models, got %v", models)
	}
}

// 火山语音's 资源 ID is not a model name, and miyun is an asset source. Asking
// either for a model list would hit an endpoint that answers something else, so
// the page never offers the button.
func TestSupportsModelListingExcludesServicesWithoutAModelList(t *testing.T) {
	for _, code := range []string{"miyun", "volcengine_speech"} {
		if SupportsModelListing(code) {
			t.Errorf("%s has no model list to read", code)
		}
	}
	for _, code := range []string{"model.text", "model.image", "model.video", "model.vision", "model.research"} {
		if !SupportsModelListing(code) {
			t.Errorf("%s is an OpenAI-shaped endpoint and should be listable", code)
		}
	}
}
