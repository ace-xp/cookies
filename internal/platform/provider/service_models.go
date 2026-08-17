package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// modelListLimit caps how many identifiers reach the page. A gateway fronting
// several vendors can answer with hundreds; a picker that long is unusable and
// the response is not worth the bytes.
const modelListLimit = 200

// SupportsModelListing reports whether asking this service for a model list is
// meaningful. Only the OpenAI-shaped endpoints answer /models; miyun is an
// asset source, and 火山语音 takes a resource ID rather than a model name, so
// for those the page keeps the plain text box instead of showing an empty
// picker that looks broken.
func SupportsModelListing(code string) bool {
	switch code {
	case "miyun", "volcengine_speech":
		return false
	default:
		return true
	}
}

// ListUpstreamModels asks the upstream which models this credential may call.
// It reuses the probe's endpoint and classifier, so a wrong address or an
// expired key produces the same diagnosis the operator already knows from
// 测试连接 rather than a second vocabulary for the same failures.
//
// This exists because the model identifier used to be a bare text box: the
// operator had to remember a string like doubao-seedance-2-0-fast-260128, and a
// typo stayed invisible until a real generation failed.
func ListUpstreamModels(ctx context.Context, code, baseURL, secret string) ([]string, servicecatalog.Result) {
	if !SupportsModelListing(code) {
		return nil, servicecatalog.Result{Outcome: servicecatalog.OutcomeOK, Message: "这项没有可读取的模型清单"}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probeEndpoint(code, baseURL), nil)
	if err != nil {
		return nil, servicecatalog.ClassifyTransport(err)
	}
	request.Header.Set("Authorization", "Bearer "+secret)

	response, err := (&http.Client{Timeout: probeTimeout}).Do(request)
	if err != nil {
		return nil, servicecatalog.ClassifyTransport(err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, probeMaxBodyBytes))
	result := servicecatalog.ClassifyHTTP(response.StatusCode, extractUpstreamMessage(body))
	if result.Outcome != servicecatalog.OutcomeOK {
		return nil, result
	}
	return parseModelIdentifiers(body), result
}

// parseModelIdentifiers reads the OpenAI /models envelope. Some gateways name
// the identifier "model" instead of "id", so both are accepted; anything else
// yields an empty list, which the page reports as "上游没有给出模型清单" rather
// than as an error — a working service that simply does not list its models is
// still a working service.
func parseModelIdentifiers(body []byte) []string {
	var envelope struct {
		Data []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	seen := map[string]bool{}
	identifiers := []string{}
	for _, entry := range envelope.Data {
		name := strings.TrimSpace(entry.ID)
		if name == "" {
			name = strings.TrimSpace(entry.Model)
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		identifiers = append(identifiers, name)
	}
	sort.Strings(identifiers)
	if len(identifiers) > modelListLimit {
		identifiers = identifiers[:modelListLimit]
	}
	return identifiers
}
