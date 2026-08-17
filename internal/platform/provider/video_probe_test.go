package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeArkVideoCredentialMapsUpstreamResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   VideoProbeOutcome
	}{
		{"unauthorized", http.StatusUnauthorized, VideoProbeUnauthorized},
		{"forbidden", http.StatusForbidden, VideoProbeUnauthorized},
		{"task not found means the key was accepted", http.StatusNotFound, VideoProbeOK},
		{"bad request also means the key was accepted", http.StatusBadRequest, VideoProbeOK},
		{"rate limited still proves the key works", http.StatusTooManyRequests, VideoProbeOK},
		{"upstream failure", http.StatusBadGateway, VideoProbeUpstreamError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var gotPath, gotAuthorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuthorization = r.Header.Get("Authorization")
				w.WriteHeader(testCase.status)
			}))
			defer server.Close()

			result := ProbeArkVideoCredential(t.Context(), server.Client(), server.URL, "probe-key")
			if result.Outcome != testCase.want {
				t.Fatalf("outcome = %q, want %q", result.Outcome, testCase.want)
			}
			if result.Message == "" {
				t.Fatal("probe result must carry a user-facing message")
			}
			if strings.Contains(result.Message, "probe-key") {
				t.Fatal("probe message must not echo the API key")
			}
			if gotAuthorization != "Bearer probe-key" {
				t.Fatalf("authorization = %q", gotAuthorization)
			}
			if !strings.HasSuffix(gotPath, "/contents/generations/tasks/"+videoProbeTaskID) {
				t.Fatalf("probe path = %q", gotPath)
			}
		})
	}
}

func TestProbeArkVideoCredentialReportsUnreachableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	server.Close()

	result := ProbeArkVideoCredential(t.Context(), client, server.URL, "probe-key")
	if result.Outcome != VideoProbeUnreachable {
		t.Fatalf("outcome = %q, want %q", result.Outcome, VideoProbeUnreachable)
	}
}

func TestProbeArkVideoCredentialRejectsEmptyInput(t *testing.T) {
	result := ProbeArkVideoCredential(t.Context(), http.DefaultClient, "", "")
	if result.Outcome != VideoProbeInvalidInput {
		t.Fatalf("outcome = %q, want %q", result.Outcome, VideoProbeInvalidInput)
	}
	if result.OK() {
		t.Fatal("invalid input must not report success")
	}
}
