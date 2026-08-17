package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// SoundAssetGenerationInput is intentionally separate from SpeechSynthesisInput.
// Music and sound effects are prompt-to-audio media, not a different voice preset.
type SoundAssetGenerationInput struct {
	OrganizationID contract.OrganizationID `json:"-"`
	ModelAlias     string                  `json:"-"`
	RequestID      string                  `json:"-"`
	TrackType      string                  `json:"track_type"`
	Prompt         string                  `json:"prompt"`
	NegativePrompt string                  `json:"negative_prompt,omitempty"`
	DurationMS     int                     `json:"duration_ms"`
	Format         string                  `json:"format"`
	SampleRate     int                     `json:"sample_rate"`
}

func (i SoundAssetGenerationInput) Validate() error {
	if strings.TrimSpace(i.Prompt) == "" || len([]rune(i.Prompt)) > 2_000 {
		return fmt.Errorf("sound prompt must contain 1 to 2000 characters")
	}
	switch i.TrackType {
	case "music", "ambience", "sfx":
	default:
		return fmt.Errorf("sound track type is not supported")
	}
	if i.DurationMS < 100 || i.DurationMS > 60_000 {
		return fmt.Errorf("sound duration must be between 100 and 60000 milliseconds")
	}
	if i.Format != "wav" {
		return fmt.Errorf("sound output format must be wav")
	}
	if i.SampleRate != 48_000 {
		return fmt.Errorf("sound sample rate must be 48000")
	}
	return nil
}

type SoundAssetGenerationResult struct {
	Audio             []byte `json:"-"`
	Codec             string `json:"codec"`
	DurationMS        int    `json:"duration_ms"`
	ProviderRequestID string `json:"provider_request_id"`
	ProviderSnapshot  string `json:"provider_snapshot"`
}

type SoundAssetGenerator interface {
	GenerateSoundAsset(context.Context, SoundAssetGenerationInput) (SoundAssetGenerationResult, error)
}

type SoundAssetProviderError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e SoundAssetProviderError) Error() string { return e.Code + ": " + e.Message }

// HTTPSoundAssetGenerator connects a provider-normalizer endpoint. The endpoint
// contract is deliberately small so that an actual music/SFX vendor can be
// changed without leaking vendor payloads into the Brand Film domain:
// request is SoundAssetGenerationInput plus model; response is
// {audio_base64, codec, duration_ms, request_id, model_snapshot}.
type HTTPSoundAssetGenerator struct {
	Endpoint string
	APIKey   string
	Model    string
	Client   *http.Client
}

func (g HTTPSoundAssetGenerator) GenerateSoundAsset(ctx context.Context, input SoundAssetGenerationInput) (SoundAssetGenerationResult, error) {
	if err := input.Validate(); err != nil {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "input_rejected", Message: err.Error()}
	}
	endpoint, err := url.Parse(strings.TrimSpace(g.Endpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "capability_unavailable", Message: "AI sound endpoint is not configured"}
	}
	if strings.TrimSpace(g.APIKey) == "" || strings.TrimSpace(g.Model) == "" {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "capability_unavailable", Message: "AI sound credential or model is not configured"}
	}
	body, err := json.Marshal(struct {
		Model string `json:"model"`
		SoundAssetGenerationInput
	}{Model: g.Model, SoundAssetGenerationInput: input})
	if err != nil {
		return SoundAssetGenerationResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return SoundAssetGenerationResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+g.APIKey)
	request.Header.Set("Content-Type", "application/json")
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "transport_error", Message: err.Error(), Retryable: true}
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 80<<20)
	var value struct {
		AudioBase64   string `json:"audio_base64"`
		Codec         string `json:"codec"`
		DurationMS    int    `json:"duration_ms"`
		RequestID     string `json:"request_id"`
		ModelSnapshot string `json:"model_snapshot"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(limited).Decode(&value); err != nil {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "invalid_response", Message: err.Error(), Retryable: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(value.Error)
		if message == "" {
			message = fmt.Sprintf("AI sound endpoint returned %d", response.StatusCode)
		}
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "provider_unavailable", Message: message, Retryable: response.StatusCode >= 500}
	}
	audio, err := base64.StdEncoding.DecodeString(value.AudioBase64)
	if err != nil || len(audio) < 44 {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "invalid_audio", Message: "AI sound endpoint returned no valid audio", Retryable: true}
	}
	if value.Codec != "wav" || value.DurationMS < 100 {
		return SoundAssetGenerationResult{}, SoundAssetProviderError{Code: "invalid_response", Message: "AI sound endpoint must return WAV audio with a duration", Retryable: true}
	}
	return SoundAssetGenerationResult{Audio: audio, Codec: value.Codec, DurationMS: value.DurationMS, ProviderRequestID: value.RequestID, ProviderSnapshot: value.ModelSnapshot}, nil
}
