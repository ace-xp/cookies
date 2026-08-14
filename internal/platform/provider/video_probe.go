package provider

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// VideoProbeOutcome classifies a zero-cost connectivity check against the Ark
// video task API. The probe queries a task ID that cannot exist, so a rejected
// credential and an accepted one are distinguishable without submitting work.
type VideoProbeOutcome string

const (
	VideoProbeOK            VideoProbeOutcome = "ok"
	VideoProbeUnauthorized  VideoProbeOutcome = "unauthorized"
	VideoProbeUnreachable   VideoProbeOutcome = "unreachable"
	VideoProbeUpstreamError VideoProbeOutcome = "upstream_error"
	VideoProbeInvalidInput  VideoProbeOutcome = "invalid_input"
)

// videoProbeTaskID is deliberately not a valid Ark task ID.
const videoProbeTaskID = "cookies-connectivity-probe"

const videoProbeTimeout = 15 * time.Second

// VideoProbeResult carries a user-facing message. It never contains the
// credential under test.
type VideoProbeResult struct {
	Outcome VideoProbeOutcome
	Message string
}

func (r VideoProbeResult) OK() bool { return r.Outcome == VideoProbeOK }

// ProbeArkVideoCredential verifies that the base URL is reachable and the API
// key is accepted. It cannot verify the model name: that would require
// submitting a billable generation task.
func ProbeArkVideoCredential(ctx context.Context, client *http.Client, baseURL, apiKey string) VideoProbeResult {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || strings.TrimSpace(apiKey) == "" {
		return VideoProbeResult{Outcome: VideoProbeInvalidInput, Message: "服务地址和密钥都不能为空"}
	}
	if client == nil {
		client = &http.Client{Timeout: videoProbeTimeout}
	}
	probeCtx, cancel := context.WithTimeout(ctx, videoProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+arkVideoTaskPath+"/"+videoProbeTaskID, nil)
	if err != nil {
		return VideoProbeResult{Outcome: VideoProbeInvalidInput, Message: "服务地址不是合法的 URL"}
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return VideoProbeResult{Outcome: VideoProbeUnreachable, Message: "服务地址连不上，请检查地址和网络"}
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return VideoProbeResult{Outcome: VideoProbeUnauthorized, Message: "密钥被拒绝，请确认填的是完整的 API Key"}
	case response.StatusCode >= 500:
		return VideoProbeResult{Outcome: VideoProbeUpstreamError, Message: "模型服务暂时不可用，请稍后重试"}
	default:
		// A 4xx other than 401/403 means the credential was accepted and only the
		// probe task ID was rejected, which is exactly what we are looking for.
		return VideoProbeResult{Outcome: VideoProbeOK, Message: "连接正常"}
	}
}
