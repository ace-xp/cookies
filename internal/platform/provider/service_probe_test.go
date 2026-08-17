package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

func TestProbeServiceReportsOKOnSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("unexpected authorization header %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-test")
	if result.Outcome != servicecatalog.OutcomeOK {
		t.Fatalf("expected ok, got %q (%s)", result.Outcome, result.Message)
	}
}

func TestProbeServiceReportsAuthFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-bad")
	if result.Outcome != servicecatalog.OutcomeAuthFailed {
		t.Fatalf("expected auth_failed, got %q", result.Outcome)
	}
}

func TestProbeServiceCarriesUpstreamRejectionMessage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"该链接需要高版本才能查看，请升级套餐。"}}`))
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-test")
	if result.Outcome != servicecatalog.OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.UpstreamMessage != "该链接需要高版本才能查看，请升级套餐。" {
		t.Fatalf("upstream words were lost: %q", result.UpstreamMessage)
	}
}

func TestProbeServiceReportsUnreachable(t *testing.T) {
	// Port 1 is reserved and refuses connections on every supported platform.
	result := ProbeService(context.Background(), "model.text", "https://127.0.0.1:1", "sk-test")
	if result.Outcome != servicecatalog.OutcomeUnreachable {
		t.Fatalf("expected unreachable, got %q", result.Outcome)
	}
}

// Miyun authenticates with a session cookie rather than a bearer token. Sending
// the cookie in an Authorization header would read as an anonymous request and
// report a credential problem that does not exist.
func TestProbeServiceSendsMiyunSessionAsCookie(t *testing.T) {
	var authorization, cookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		cookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ProbeService(context.Background(), "miyun", upstream.URL, "sessionid=abc")
	if cookie != "sessionid=abc" {
		t.Fatalf("the session cookie was not sent as a cookie: %q", cookie)
	}
	if authorization != "" {
		t.Fatalf("the session cookie must not be sent as a bearer token: %q", authorization)
	}
}

// 上游只认带版本段的路径。地址填成网关根（不带 /v1）时，这里必须像
// gateway_config.go 那样自己补上，否则一份能正常调用的配置会被报成
// 「地址填错了」。
func TestProbeEndpointAddsVersionSegmentWhenBaseHasNone(t *testing.T) {
	for _, testCase := range []struct{ name, base, want string }{
		{"网关根", "https://gateway.example.com", "https://gateway.example.com/v1/models"},
		{"带尾斜杠的网关根", "https://gateway.example.com/", "https://gateway.example.com/v1/models"},
		{"已带 v1", "https://gateway.example.com/v1", "https://gateway.example.com/v1/models"},
		{"火山方舟的 api/v3", "https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/v3/models"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := probeEndpoint("model.text", testCase.base); got != testCase.want {
				t.Fatalf("probeEndpoint(%q) = %q, want %q", testCase.base, got, testCase.want)
			}
		})
	}
}
