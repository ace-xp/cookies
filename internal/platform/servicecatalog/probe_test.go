package servicecatalog

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyHTTPSuccess(t *testing.T) {
	result := ClassifyHTTP(200, "")
	if result.Outcome != OutcomeOK {
		t.Fatalf("expected ok, got %q", result.Outcome)
	}
	if result.Message != messageOK {
		t.Fatalf("unexpected message %q", result.Message)
	}
}

func TestClassifyHTTPAuthFailure(t *testing.T) {
	for _, status := range []int{401, 403} {
		result := ClassifyHTTP(status, "")
		if result.Outcome != OutcomeAuthFailed {
			t.Fatalf("status %d: expected auth_failed, got %q", status, result.Outcome)
		}
		if !strings.Contains(result.Message, "重新签发") {
			t.Fatalf("status %d: message must tell the operator what to do, got %q", status, result.Message)
		}
	}
}

// An auth failure that came with an explanation keeps both: our guidance and
// the upstream's own words.
func TestClassifyHTTPAuthFailureKeepsUpstreamMessage(t *testing.T) {
	result := ClassifyHTTP(403, "API key has been revoked")
	if result.UpstreamMessage != "API key has been revoked" {
		t.Fatalf("upstream message was lost: %q", result.UpstreamMessage)
	}
	if !strings.Contains(result.Message, "API key has been revoked") {
		t.Fatalf("display message must carry the upstream words, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "重新签发") {
		t.Fatalf("display message must still carry our guidance, got %q", result.Message)
	}
}

// The upstream's own words must survive. Miyun once returned only the code
// 00:403001, the UI guessed "re-copy your cookie", and the real cause was an
// insufficient subscription tier — see the 20260814100000 insights migration.
func TestClassifyHTTPRejectionKeepsUpstreamMessage(t *testing.T) {
	result := ClassifyHTTP(400, "该链接需要高版本才能查看，请升级套餐。")
	if result.Outcome != OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.UpstreamMessage != "该链接需要高版本才能查看，请升级套餐。" {
		t.Fatalf("upstream message was not preserved verbatim: %q", result.UpstreamMessage)
	}
	if !strings.Contains(result.Message, "该链接需要高版本才能查看，请升级套餐。") {
		t.Fatalf("display message must carry the upstream words, got %q", result.Message)
	}
}

func TestClassifyHTTPRejectionWithoutUpstreamWords(t *testing.T) {
	result := ClassifyHTTP(500, "")
	if result.Outcome != OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.Message == "" {
		t.Fatal("a rejection with no upstream words still needs a message")
	}
}

// 404 is its own trap: the credential may be perfect and the host reachable,
// but the path is wrong. Calling that "rejected" with no explanation sends the
// operator hunting for a key problem that does not exist.
func TestClassifyHTTPNotFoundPointsAtTheAddress(t *testing.T) {
	result := ClassifyHTTP(404, "")
	if result.Outcome != OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if !strings.Contains(result.Message, "地址") {
		t.Fatalf("a 404 must point at the address, got %q", result.Message)
	}
}

// 「查不出来」必须和「查出来是坏的」分开。混成一个结果，操作的人会去修一个
// 不存在的故障，而真正只是没接口可探的服务会被永远拦在保存之外。
func TestUnverifiedIsNotAFailure(t *testing.T) {
	result := Unverified("")
	if result.Outcome != OutcomeUnverified {
		t.Fatalf("expected unverified, got %q", result.Outcome)
	}
	if strings.Contains(result.Message, "地址填错") {
		t.Fatalf("an unchecked service must not be accused of a wrong address: %q", result.Message)
	}
	if !strings.Contains(result.Message, "检查不了") {
		t.Fatalf("the message must say the check could not be made, got %q", result.Message)
	}
}

func TestUnverifiedKeepsUpstreamWords(t *testing.T) {
	result := Unverified("  404 page not found  ")
	if result.UpstreamMessage != "404 page not found" {
		t.Fatalf("upstream words were lost: %q", result.UpstreamMessage)
	}
	if !strings.Contains(result.Message, "404 page not found") {
		t.Fatalf("display message must carry the upstream words, got %q", result.Message)
	}
}

func TestClassifyHTTPTrimsUpstreamWhitespace(t *testing.T) {
	result := ClassifyHTTP(400, "  配额已用尽  \n")
	if result.UpstreamMessage != "配额已用尽" {
		t.Fatalf("expected trimmed message, got %q", result.UpstreamMessage)
	}
}

func TestClassifyTransportFailure(t *testing.T) {
	result := ClassifyTransport(errors.New("dial tcp 10.0.0.1:443: i/o timeout"))
	if result.Outcome != OutcomeUnreachable {
		t.Fatalf("expected unreachable, got %q", result.Outcome)
	}
	if !strings.Contains(result.Message, "检查地址") {
		t.Fatalf("message must point at address and network, got %q", result.Message)
	}
}

// A transport error can carry an internal hostname or a token embedded in a
// URL. Only the classification and the fixed guidance reach the browser.
func TestClassifyTransportDoesNotLeakErrorText(t *testing.T) {
	result := ClassifyTransport(errors.New("dial tcp secret-host.internal:443: refused"))
	if strings.Contains(result.Message, "secret-host.internal") {
		t.Fatalf("transport error detail must not reach the message: %q", result.Message)
	}
	if result.UpstreamMessage != "" {
		t.Fatalf("transport errors have no upstream message, got %q", result.UpstreamMessage)
	}
}

func TestClassifyTransportWithNilError(t *testing.T) {
	result := ClassifyTransport(nil)
	if result.Outcome != OutcomeOK {
		t.Fatalf("a nil transport error is a success, got %q", result.Outcome)
	}
}

// The page renders one guidance sentence per outcome, so no outcome may be
// left without one.
func TestEveryOutcomeHasAMessage(t *testing.T) {
	cases := []Result{
		ClassifyHTTP(200, ""),
		ClassifyHTTP(401, ""),
		ClassifyHTTP(500, ""),
		ClassifyTransport(errors.New("boom")),
		Unverified(""),
	}
	seen := map[Outcome]bool{}
	for _, result := range cases {
		if strings.TrimSpace(result.Message) == "" {
			t.Errorf("%q has no message", result.Outcome)
		}
		seen[result.Outcome] = true
	}
	for _, outcome := range []Outcome{OutcomeOK, OutcomeAuthFailed, OutcomeRejected, OutcomeUnreachable, OutcomeUnverified} {
		if !seen[outcome] {
			t.Errorf("no case produced outcome %q", outcome)
		}
	}
}
