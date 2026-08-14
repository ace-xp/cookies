package provider

import "testing"

func TestMaskAPIKeyKeepsOnlyTheTail(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"abcd":                "****",
		"abcdefgh":            "****efgh",
		"sk-1234567890abcdef": "****cdef",
	}
	for input, want := range cases {
		if got := MaskAPIKey(input); got != want {
			t.Fatalf("MaskAPIKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeVideoConfigurationInputRejectsBadValues(t *testing.T) {
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "", Model: "m"}, false); err == nil {
		t.Fatal("empty base URL must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "https://ark.example/api/v3", Model: ""}, false); err == nil {
		t.Fatal("empty model must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "http://ark.example/api/v3", Model: "m"}, false); err == nil {
		t.Fatal("plain HTTP must be rejected unless explicitly allowed")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "http://ark.example/api/v3", Model: "m"}, true); err != nil {
		t.Fatalf("plain HTTP must be accepted when the deployment allows it: %v", err)
	}
	normalized, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: " https://ark.example/api/v3/ ", Model: " seedance "}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.BaseURL != "https://ark.example/api/v3" || normalized.Model != "seedance" {
		t.Fatalf("normalized = %+v", normalized)
	}
}
