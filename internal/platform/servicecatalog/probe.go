package servicecatalog

import (
	"net/http"
	"strings"
)

// Outcome is the four-way classification every probe collapses to. The page
// renders one sentence of guidance per outcome, so the operator always knows
// the next action.
type Outcome string

const (
	OutcomeOK          Outcome = "ok"
	OutcomeAuthFailed  Outcome = "auth_failed"
	OutcomeUnreachable Outcome = "unreachable"
	OutcomeRejected    Outcome = "rejected"
)

// Result is what a probe reports. UpstreamMessage is kept separate from
// Message so callers can log the upstream's own words and the page can show
// them without the platform paraphrasing.
type Result struct {
	Outcome         Outcome `json:"outcome"`
	Message         string  `json:"message"`
	UpstreamMessage string  `json:"upstream_message,omitempty"`
}

const (
	messageOK          = "已连通，可以用"
	messageAuthFailed  = "密钥无效或已过期，请到服务商后台重新签发"
	messageUnreachable = "地址填错，或服务器出不了网。请检查地址、检查服务器网络"
	messageRejected    = "上游拒绝了这次请求，但没有给出说明"
	messageNotFound    = "地址填错了：能连上服务器，但这个路径不存在"
)

// ClassifyHTTP maps an upstream status code to an outcome. upstreamMessage is
// whatever human-readable explanation the upstream gave; it is carried through
// verbatim rather than re-interpreted, because guessing has misled operators
// before (see the miyun 00:403001 incident).
func ClassifyHTTP(status int, upstreamMessage string) Result {
	trimmed := strings.TrimSpace(upstreamMessage)
	switch {
	case status >= 200 && status < 300:
		return Result{Outcome: OutcomeOK, Message: messageOK}

	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// Our guidance leads, because "the key is bad" is the action; the
		// upstream's wording follows as supporting detail.
		message := messageAuthFailed
		if trimmed != "" {
			message += "（上游说明：" + trimmed + "）"
		}
		return Result{Outcome: OutcomeAuthFailed, Message: message, UpstreamMessage: trimmed}

	case status == http.StatusNotFound:
		// The credential may be fine here. Saying only "rejected" would send
		// the operator looking for a key problem that does not exist.
		message := messageNotFound
		if trimmed != "" {
			message += "（上游说明：" + trimmed + "）"
		}
		return Result{Outcome: OutcomeRejected, Message: message, UpstreamMessage: trimmed}

	default:
		// The upstream knows why it refused and we do not, so its words lead.
		if trimmed != "" {
			return Result{Outcome: OutcomeRejected, Message: trimmed, UpstreamMessage: trimmed}
		}
		return Result{Outcome: OutcomeRejected, Message: messageRejected}
	}
}

// ClassifyTransport handles the case where no HTTP response arrived at all.
// The error text may name internal hosts or carry a token from a URL, so it is
// deliberately dropped rather than forwarded.
func ClassifyTransport(err error) Result {
	if err == nil {
		return Result{Outcome: OutcomeOK, Message: messageOK}
	}
	return Result{Outcome: OutcomeUnreachable, Message: messageUnreachable}
}
