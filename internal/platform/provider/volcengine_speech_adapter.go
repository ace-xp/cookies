package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// volcengineSpeechSynthesisPath is where synthesis lives on the upstream. The
// settings page asks the operator for a host, so the path is appended here
// rather than being something they have to know.
const volcengineSpeechSynthesisPath = "/api/v3/tts/unidirectional"

const defaultVolcengineSpeechEndpoint = "https://openspeech.bytedance.com" + volcengineSpeechSynthesisPath

// VolcengineSpeechModelAlias is the logical name the stored route is filed
// under. It is what the settings page writes and what this adapter resolves.
const VolcengineSpeechModelAlias = "cookies.speech.volcengine"

const volcengineSpeechTerminalCode = 20000000

type VolcengineSpeechConfig struct {
	Endpoint     string
	APIKey       string
	ResourceID   string
	DefaultVoice string
	VoiceAliases map[string]string
}

type VolcengineSpeechAdapter struct {
	// routes and credentials are nil on the environment-only adapter. When set,
	// they are consulted on every call, so a save in the settings page takes
	// effect without a restart.
	routes       SpeechRouteResolver
	credentials  GatewayCredentialResolver
	config       VolcengineSpeechConfig
	client       *http.Client
	newRequestID func() (string, error)
}

func NewVolcengineSpeechAdapter(config VolcengineSpeechConfig) (*VolcengineSpeechAdapter, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		config.Endpoint = defaultVolcengineSpeechEndpoint
	}
	if err := validateVolcengineSpeechEndpoint(config.Endpoint); err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.ResourceID) == "" || strings.TrimSpace(config.DefaultVoice) == "" {
		return nil, fmt.Errorf("Volcengine speech API key, resource ID and default voice are required")
	}
	return &VolcengineSpeechAdapter{config: config, client: &http.Client{Timeout: 2 * time.Minute}, newRequestID: randomSpeechRequestID}, nil
}

// NewRoutedVolcengineSpeechAdapter resolves the address, resource ID, and key
// from the stored route on every call. fallback keeps deployments working that
// never opened the settings page: with no route and an environment key, the
// environment is used. The voice settings always come from fallback — they are
// not part of what the page manages.
func NewRoutedVolcengineSpeechAdapter(routes SpeechRouteResolver, credentials GatewayCredentialResolver, fallback VolcengineSpeechConfig) (*VolcengineSpeechAdapter, error) {
	if routes == nil || credentials == nil {
		return nil, fmt.Errorf("Volcengine speech route and credential resolvers are required")
	}
	if strings.TrimSpace(fallback.DefaultVoice) == "" {
		return nil, fmt.Errorf("Volcengine speech default voice is required")
	}
	if strings.TrimSpace(fallback.Endpoint) != "" {
		if err := validateVolcengineSpeechEndpoint(fallback.Endpoint); err != nil {
			return nil, err
		}
	}
	return &VolcengineSpeechAdapter{
		routes: routes, credentials: credentials, config: fallback,
		client: &http.Client{Timeout: 2 * time.Minute}, newRequestID: randomSpeechRequestID,
	}, nil
}

func validateVolcengineSpeechEndpoint(endpoint string) error {
	if strings.HasPrefix(endpoint, "https://") ||
		strings.HasPrefix(endpoint, "http://127.0.0.1") ||
		strings.HasPrefix(endpoint, "http://localhost") {
		return nil
	}
	return fmt.Errorf("Volcengine speech endpoint must use HTTPS")
}

// volcengineSpeechTarget is the resolved answer to "where do I send this, with
// which key, as which resource".
type volcengineSpeechTarget struct {
	endpoint   string
	apiKey     string
	resourceID string
}

// resolve prefers the stored route and falls back to the environment. A route
// that resolves but cannot produce a credential is not silently replaced by the
// environment: that would mask a broken configuration behind stale values.
func (a *VolcengineSpeechAdapter) resolve(ctx context.Context, organizationID contract.OrganizationID) (volcengineSpeechTarget, error) {
	if a.routes != nil {
		route, err := a.routes.ResolveSpeechRoute(ctx, organizationID, VolcengineSpeechModelAlias)
		switch {
		case err == nil:
			credential, credErr := a.credentials.ResolveGatewayCredential(ctx, route.CredentialID, route.CredentialVersion)
			if credErr != nil {
				return volcengineSpeechTarget{}, SpeechProviderError{
					Code: "capability_unavailable", Message: credErr.Error(), Retryable: false,
				}
			}
			return volcengineSpeechTarget{
				endpoint:   volcengineSpeechEndpoint(route.BaseURL),
				apiKey:     credential,
				resourceID: route.UpstreamModel,
			}, nil
		case !errors.Is(err, ErrGatewayRouteNotFound):
			return volcengineSpeechTarget{}, SpeechProviderError{
				Code: "capability_unavailable", Message: err.Error(), Retryable: false,
			}
		}
	}

	if strings.TrimSpace(a.config.APIKey) == "" || strings.TrimSpace(a.config.ResourceID) == "" {
		return volcengineSpeechTarget{}, SpeechProviderError{
			Code:    "capability_unavailable",
			Message: "火山语音还没有配置，请在系统设置的外部服务里填写地址与密钥",
		}
	}
	endpoint := a.config.Endpoint
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultVolcengineSpeechEndpoint
	}
	return volcengineSpeechTarget{
		endpoint: endpoint, apiKey: a.config.APIKey, resourceID: a.config.ResourceID,
	}, nil
}

// volcengineSpeechEndpoint accepts either a host or a full synthesis URL, so an
// operator who pastes the address from the vendor's docs and one who pastes
// just the host both end up calling the same place.
func volcengineSpeechEndpoint(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		return defaultVolcengineSpeechEndpoint
	}
	if strings.HasSuffix(trimmed, volcengineSpeechSynthesisPath) {
		return trimmed
	}
	return trimmed + volcengineSpeechSynthesisPath
}

func randomSpeechRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

type volcengineSpeechRequest struct {
	Parameters struct {
		Text    string `json:"text"`
		Speaker string `json:"speaker"`
		Audio   struct {
			Format           string `json:"format"`
			SampleRate       int    `json:"sample_rate"`
			SpeechRate       int    `json:"speech_rate"`
			EnableSubtitle   bool   `json:"enable_subtitle"`
			ExplicitLanguage string `json:"explicit_language,omitempty"`
		} `json:"audio_params"`
	} `json:"req_params"`
}

type volcengineSpeechChunk struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Data     string `json:"data"`
	Sentence struct {
		Text  string `json:"text"`
		Words []struct {
			Word      string  `json:"word"`
			StartTime float64 `json:"startTime"`
			EndTime   float64 `json:"endTime"`
		} `json:"words"`
	} `json:"sentence"`
}

func (a *VolcengineSpeechAdapter) Synthesize(ctx context.Context, input SpeechSynthesisInput) (SpeechSynthesisResult, error) {
	if err := input.Validate(); err != nil {
		return SpeechSynthesisResult{}, err
	}
	target, err := a.resolve(ctx, input.OrganizationID)
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		requestID, err = a.newRequestID()
		if err != nil {
			return SpeechSynthesisResult{}, err
		}
	}
	voice := a.config.DefaultVoice
	if mapped := a.config.VoiceAliases[input.VoiceAlias]; strings.TrimSpace(mapped) != "" {
		voice = mapped
	} else if input.VoiceAlias != "douyin-female-01" && !strings.HasPrefix(input.VoiceAlias, "cookies.") {
		voice = input.VoiceAlias
	}
	var payload volcengineSpeechRequest
	payload.Parameters.Text, payload.Parameters.Speaker = strings.TrimSpace(input.Text), voice
	payload.Parameters.Audio.Format, payload.Parameters.Audio.SampleRate = input.Format, input.SampleRate
	payload.Parameters.Audio.SpeechRate = int((input.SpeakingRate - 1) * 100)
	payload.Parameters.Audio.EnableSubtitle = input.NeedTimestamps
	payload.Parameters.Audio.ExplicitLanguage = normalizeVolcengineSpeechLanguage(input.Language)
	body, err := json.Marshal(payload)
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.endpoint, bytes.NewReader(body))
	if err != nil {
		return SpeechSynthesisResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", target.apiKey)
	request.Header.Set("X-Api-Resource-Id", target.resourceID)
	request.Header.Set("X-Api-Request-Id", requestID)
	response, err := a.client.Do(request)
	if err != nil {
		return SpeechSynthesisResult{}, SpeechProviderError{Code: "transport_error", Message: err.Error(), Retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return SpeechSynthesisResult{}, classifyVolcengineSpeechError(response.StatusCode, string(message))
	}
	result := SpeechSynthesisResult{Codec: input.Format, SampleRate: input.SampleRate, OriginalText: input.Text, WordTimings: []SpeechWordTiming{}, ProviderRequestID: response.Header.Get("X-Tt-Logid"), ModelAndVoiceSnapshot: target.resourceID + "/" + voice}
	decoder := json.NewDecoder(response.Body)
	for {
		var chunk volcengineSpeechChunk
		if err := decoder.Decode(&chunk); err == io.EOF {
			break
		} else if err != nil {
			return SpeechSynthesisResult{}, SpeechProviderError{Code: "invalid_response", Message: err.Error(), Retryable: true}
		}
		if chunk.Code == volcengineSpeechTerminalCode && strings.EqualFold(strings.TrimSpace(chunk.Message), "OK") {
			break
		}
		if chunk.Code != 0 {
			return SpeechSynthesisResult{}, classifyVolcengineSpeechError(chunk.Code, chunk.Message)
		}
		if chunk.Data != "" {
			audio, decodeErr := base64.StdEncoding.DecodeString(chunk.Data)
			if decodeErr != nil {
				return SpeechSynthesisResult{}, SpeechProviderError{Code: "invalid_audio", Message: decodeErr.Error(), Retryable: true}
			}
			result.Audio = append(result.Audio, audio...)
		}
		if chunk.Sentence.Text != "" {
			result.NormalizedText = chunk.Sentence.Text
		}
		for _, word := range chunk.Sentence.Words {
			timing := SpeechWordTiming{Text: word.Word, BeginMS: int(word.StartTime * 1000), EndMS: int(word.EndTime * 1000)}
			result.WordTimings = append(result.WordTimings, timing)
			if timing.EndMS > result.DurationMS {
				result.DurationMS = timing.EndMS
			}
		}
	}
	if len(result.Audio) == 0 {
		return SpeechSynthesisResult{}, SpeechProviderError{Code: "empty_audio", Message: "speech provider returned no audio", Retryable: true}
	}
	if result.NormalizedText == "" {
		result.NormalizedText = input.Text
	}
	return result, nil
}

func normalizeVolcengineSpeechLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "zh-cn", "zh":
		return "zh-cn"
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return ""
	}
}

func classifyVolcengineSpeechError(code int, message string) SpeechProviderError {
	lower := strings.ToLower(message)
	switch {
	case code == http.StatusTooManyRequests || strings.Contains(lower, "quota") || strings.Contains(lower, "concurrency"):
		return SpeechProviderError{Code: "quota_exceeded", Message: message, Retryable: true}
	case code >= 500 || strings.Contains(lower, "timeout") || strings.Contains(lower, "internal"):
		return SpeechProviderError{Code: "provider_unavailable", Message: message, Retryable: true}
	case code == http.StatusUnauthorized || code == http.StatusForbidden || strings.Contains(lower, "access denied"):
		return SpeechProviderError{Code: "capability_unavailable", Message: message, Retryable: false}
	default:
		return SpeechProviderError{Code: "input_rejected", Message: message, Retryable: false}
	}
}
