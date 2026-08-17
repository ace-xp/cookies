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

// 复现 8091 上的共享网关：它照常提供 /v1/chat/completions，却没有 /v1/models。
// 把这个 404 当成「地址填错了」，图片、视频、图片理解三项就会一律显示成坏的，
// 又因为保存前必须探通，它们连改都改不了。
func TestProbeServiceTreatsAMissingModelListAsUnverifiedRatherThanABadAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found"))
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.video", upstream.URL+"/v1", "sk-test")
	if result.Outcome != servicecatalog.OutcomeUnverified {
		t.Fatalf("expected unverified, got %q (%s)", result.Outcome, result.Message)
	}
}

// 秘云探的是它自家文档写明的地址，这个 404 就是真的填错了，不能跟着放行。
func TestProbeServiceStillCallsA404ABadAddressWhereThePathIsPromised(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	for _, code := range []string{"miyun", "volcengine_speech"} {
		result := ProbeService(context.Background(), code, upstream.URL, "secret")
		if result.Outcome != servicecatalog.OutcomeRejected {
			t.Errorf("%s: expected rejected, got %q", code, result.Outcome)
		}
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
