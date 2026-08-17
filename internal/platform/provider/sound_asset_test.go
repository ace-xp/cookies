package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSoundAssetGeneratorUsesPromptToAudioContract(t *testing.T) {
	var received struct {
		Model     string `json:"model"`
		TrackType string `json:"track_type"`
		Prompt    string `json:"prompt"`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"audio_base64":   base64.StdEncoding.EncodeToString(append([]byte("RIFF"), make([]byte, 60)...)),
			"codec":          "wav",
			"duration_ms":    800,
			"request_id":     "sound-request-1",
			"model_snapshot": "vendor/music-v1",
		})
	}))
	defer server.Close()
	generator := HTTPSoundAssetGenerator{Endpoint: server.URL, APIKey: "test-key", Model: "music-v1", Client: server.Client()}
	result, err := generator.GenerateSoundAsset(context.Background(), SoundAssetGenerationInput{TrackType: "sfx", Prompt: "细腻的玻璃水滴", DurationMS: 800, Format: "wav", SampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	if received.Model != "music-v1" || received.TrackType != "sfx" || received.Prompt != "细腻的玻璃水滴" {
		t.Fatalf("request = %#v", received)
	}
	if result.ProviderSnapshot != "vendor/music-v1" || result.ProviderRequestID != "sound-request-1" || len(result.Audio) != 64 {
		t.Fatalf("result = %#v", result)
	}
}
