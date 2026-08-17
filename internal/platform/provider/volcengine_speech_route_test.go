package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type stubSpeechRouteResolver struct {
	snapshot GatewayRouteSnapshot
	err      error
	calls    int
}

func (r *stubSpeechRouteResolver) ResolveSpeechRoute(context.Context, contract.OrganizationID, string) (GatewayRouteSnapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

type stubSpeechCredentialResolver struct {
	key string
	err error
}

func (r stubSpeechCredentialResolver) ResolveGatewayCredential(context.Context, string, int64) (string, error) {
	return r.key, r.err
}

// speechUpstreamStub answers with one audio chunk and records what the adapter
// actually sent, which is where the resolved address, resource ID, and key show
// up.
type speechUpstreamCapture struct {
	path      string
	apiKey    string
	resource  string
	requested bool
}

func newSpeechUpstream(t *testing.T, capture *speechUpstreamCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.requested = true
		capture.path = r.URL.Path
		capture.apiKey = r.Header.Get("X-Api-Key")
		capture.resource = r.Header.Get("X-Api-Resource-Id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": base64.StdEncoding.EncodeToString([]byte("audio")),
		})
	}))
}

func speechTestInput() SpeechSynthesisInput {
	return SpeechSynthesisInput{
		OrganizationID: "org_local", Text: "你好", VoiceAlias: "douyin-female-01",
		Format: "mp3", SampleRate: 24000, SpeakingRate: 1, Language: "zh-cn",
	}
}

// The adapter must ask the resolver on every call. Reading the address once at
// construction time is exactly the behaviour this task removes — it is why a
// save in the settings page used to need a backend restart.
func TestVolcengineSpeechAdapterResolvesRoutePerCall(t *testing.T) {
	capture := &speechUpstreamCapture{}
	upstream := newSpeechUpstream(t, capture)
	defer upstream.Close()

	resolver := &stubSpeechRouteResolver{snapshot: GatewayRouteSnapshot{
		BaseURL: upstream.URL, UpstreamModel: "resource-from-route",
	}}
	adapter, err := NewRoutedVolcengineSpeechAdapter(resolver, stubSpeechCredentialResolver{key: "sk-from-route"},
		VolcengineSpeechConfig{DefaultVoice: "zh_female_test"})
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := adapter.Synthesize(context.Background(), speechTestInput()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if resolver.calls != 2 {
		t.Fatalf("expected one resolve per call, got %d", resolver.calls)
	}
	if capture.apiKey != "sk-from-route" {
		t.Fatalf("the stored credential was not used: %q", capture.apiKey)
	}
	if capture.resource != "resource-from-route" {
		t.Fatalf("the stored resource ID was not used: %q", capture.resource)
	}
}

// Deployments that never open the settings page keep working: no route plus an
// environment key means the environment is used.
func TestVolcengineSpeechAdapterFallsBackToEnvironment(t *testing.T) {
	capture := &speechUpstreamCapture{}
	upstream := newSpeechUpstream(t, capture)
	defer upstream.Close()

	resolver := &stubSpeechRouteResolver{err: ErrGatewayRouteNotFound}
	adapter, err := NewRoutedVolcengineSpeechAdapter(resolver, stubSpeechCredentialResolver{},
		VolcengineSpeechConfig{
			Endpoint: upstream.URL, APIKey: "sk-from-env",
			ResourceID: "resource-from-env", DefaultVoice: "zh_female_test",
		})
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	if _, err := adapter.Synthesize(context.Background(), speechTestInput()); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if capture.apiKey != "sk-from-env" || capture.resource != "resource-from-env" {
		t.Fatalf("the environment fallback did not apply: key=%q resource=%q", capture.apiKey, capture.resource)
	}
}

// With neither a route nor an environment key there is nothing to call. Saying
// so is better than sending an unauthenticated request and reporting whatever
// the upstream says about it.
func TestVolcengineSpeechAdapterReportsUnavailableWithoutRouteOrEnvironment(t *testing.T) {
	resolver := &stubSpeechRouteResolver{err: ErrGatewayRouteNotFound}
	adapter, err := NewRoutedVolcengineSpeechAdapter(resolver, stubSpeechCredentialResolver{},
		VolcengineSpeechConfig{DefaultVoice: "zh_female_test"})
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	_, err = adapter.Synthesize(context.Background(), speechTestInput())
	if err == nil {
		t.Fatal("expected the capability to report itself unavailable")
	}
	var providerErr SpeechProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "capability_unavailable" {
		t.Fatalf("expected capability_unavailable, got %v", err)
	}
}

// The catalog asks the operator for a host, but the upstream call goes to a
// specific path. Storing the host alone must still produce a working call.
func TestVolcengineSpeechAdapterAppendsSynthesisPathToStoredHost(t *testing.T) {
	capture := &speechUpstreamCapture{}
	upstream := newSpeechUpstream(t, capture)
	defer upstream.Close()

	resolver := &stubSpeechRouteResolver{snapshot: GatewayRouteSnapshot{
		BaseURL: upstream.URL, UpstreamModel: "resource-1",
	}}
	adapter, err := NewRoutedVolcengineSpeechAdapter(resolver, stubSpeechCredentialResolver{key: "sk"},
		VolcengineSpeechConfig{DefaultVoice: "zh_female_test"})
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	if _, err := adapter.Synthesize(context.Background(), speechTestInput()); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if capture.path != volcengineSpeechSynthesisPath {
		t.Fatalf("expected the synthesis path to be appended, got %q", capture.path)
	}
}
