package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// probeTimeout keeps a hung upstream from holding the settings page open.
const probeTimeout = 15 * time.Second

// probeMaxBodyBytes caps how much of an error body is read before classifying.
const probeMaxBodyBytes = 64 * 1024

// probePath is the lightest read-only endpoint per service. Probes may cost
// money on metered upstreams, so nothing here generates content. The paths are
// verified against the real upstreams during the acceptance run; a wrong one
// shows up as "地址填错了" on a configuration that is actually fine, so fix it
// here rather than loosening the classifier.
func probePath(code string) string {
	switch code {
	case "miyun":
		return ""
	case "volcengine_speech":
		return "/api/v1/tts/voices"
	default:
		return "/models"
	}
}

// ProbeService performs one lightweight authenticated read against the
// upstream and classifies the answer. It never generates content.
func ProbeService(ctx context.Context, code, baseURL, secret string) servicecatalog.Result {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(baseURL, "/") + probePath(code)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
	return servicecatalog.ClassifyHTTP(response.StatusCode, extractUpstreamMessage(body))
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
