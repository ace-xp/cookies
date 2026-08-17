package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// probeTimeout keeps a hung upstream from holding the settings page open.
const probeTimeout = 15 * time.Second

// probeMaxBodyBytes caps how much of an error body is read before classifying.
const probeMaxBodyBytes = 64 * 1024

// versionSegment matches a trailing API version element such as "/v1" or
// "/api/v3".
var versionSegment = regexp.MustCompile(`/v[0-9]+$`)

// probeEndpoint is the lightest read-only endpoint per service. Probes may cost
// money on metered upstreams, so nothing here generates content. The paths are
// verified against the real upstreams during the acceptance run; a wrong one
// shows up as "地址填错了" on a configuration that is actually fine, so fix it
// here rather than loosening the classifier.
// probesTheModelList reports whether this service's probe runs against the
// OpenAI /models endpoint. That endpoint is a convention rather than an
// obligation, so a 404 from it says nothing about the address — which is why
// the answer decides whether a 404 is a verdict or a shrug. The two services
// listed below probe an endpoint their own vendor documents, so a 404 there
// really is a wrong address.
func probesTheModelList(code string) bool {
	switch code {
	case "miyun", "volcengine_speech":
		return false
	default:
		return true
	}
}

func probeEndpoint(code, baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch code {
	case "miyun":
		return base
	case "volcengine_speech":
		return base + "/api/v1/tts/voices"
	default:
		// Every OpenAI-compatible adapter in this package appends its path to a
		// base that already carries a version element — ark bases end in
		// "/api/v3", gateway bases in "/v1". When the operator leaves the
		// version off, those adapters add "/v1" (see gateway_config.go and
		// minimax_speech_adapter.go); the probe has to add it the same way, or
		// it reports a bad address for a base the platform can actually call.
		if versionSegment.MatchString(base) {
			return base + "/models"
		}
		return base + "/v1/models"
	}
}

// ProbeService performs one lightweight authenticated read against the
// upstream and classifies the answer. It never generates content.
func ProbeService(ctx context.Context, code, baseURL, secret string) servicecatalog.Result {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probeEndpoint(code, baseURL), nil)
	if err != nil {
		return servicecatalog.ClassifyTransport(err)
	}
	if code == "miyun" {
		request.Header.Set("Cookie", secret)
	} else {
		request.Header.Set("Authorization", "Bearer "+secret)
	}

	response, err := (&http.Client{Timeout: probeTimeout}).Do(request)
	if err != nil {
		return servicecatalog.ClassifyTransport(err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, probeMaxBodyBytes))
	return classifyProbeResponse(code, response.StatusCode, extractUpstreamMessage(body))
}

// classifyProbeResponse adds the one thing ClassifyHTTP cannot know: which
// endpoint was tried. A 404 only means "wrong address" when the upstream was
// obliged to answer that path.
func classifyProbeResponse(code string, status int, upstreamMessage string) servicecatalog.Result {
	if status == http.StatusNotFound && probesTheModelList(code) {
		return servicecatalog.Unverified(upstreamMessage)
	}
	return servicecatalog.ClassifyHTTP(status, upstreamMessage)
}

// extractUpstreamMessage digs the upstream's own explanation out of the common
// error envelopes. Returning "" is fine — the classifier has a fallback.
func extractUpstreamMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	for _, candidate := range []string{envelope.Error.Message, envelope.Message, envelope.Msg} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
