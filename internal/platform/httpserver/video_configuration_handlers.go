package httpserver

import (
	"errors"
	"net/http"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
)

// videoConfigurationBody is what the Settings page sends. An omitted api_key
// means "keep the stored one", so the operator can change the base URL or the
// model without pasting the credential again.
type videoConfigurationBody struct {
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKey          string `json:"api_key"`
	ExpectedVersion *int64 `json:"expected_version"`
}

func (b videoConfigurationBody) toInput() provider.VideoConfigurationInput {
	return provider.VideoConfigurationInput{
		BaseURL: b.BaseURL, Model: b.Model, APIKey: b.APIKey, ExpectedVersion: b.ExpectedVersion,
	}
}

// videoConfigurationView is the only shape the credential ever reaches the
// browser in: a mask, never the key.
func videoConfigurationView(config provider.VideoConfiguration, environment ProviderVideoEnvironment) map[string]any {
	view := map[string]any{
		"configured":           config.Configured,
		"base_url":             config.BaseURL,
		"model":                config.UpstreamModel,
		"masked_api_key":       config.MaskedAPIKey,
		"credential_readable":  config.CredentialReadable,
		"version":              config.Version,
		"model_alias":          provider.VideoModelAlias,
		"environment_fallback": environmentFallbackView(environment),
	}
	if !config.UpdatedAt.IsZero() {
		view["updated_at"] = config.UpdatedAt
	}
	verification := map[string]any{"message": config.LastVerificationMessage}
	if config.LastVerifiedAt != nil {
		verification["verified_at"] = *config.LastVerifiedAt
	}
	if config.LastVerificationOK != nil {
		verification["ok"] = *config.LastVerificationOK
	}
	view["last_verification"] = verification
	return view
}

func environmentFallbackView(environment ProviderVideoEnvironment) map[string]any {
	return map[string]any{
		"configured": environment.Configured,
		"model":      environment.Model,
		"base_url":   environment.BaseURL,
	}
}

func (s *Server) readVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.providerVideo == nil {
		s.notImplemented(w, r)
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	config, err := s.providerVideo.GetVideoConfiguration(r.Context(), rc.Actor.OrganizationID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, videoConfigurationView(config, s.providerVideoEnv))
}

func (s *Server) verifyVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.providerVideo == nil {
		s.notImplemented(w, r)
		return
	}
	var body videoConfigurationBody
	if err := decodeJSON(w, r, &body); err != nil {
		s.badRequest(w, r, err)
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	result, err := s.providerVideo.VerifyVideoConfiguration(r.Context(), rc.Actor.OrganizationID, body.toInput())
	if err != nil {
		s.writeVideoConfigurationError(w, r, err)
		return
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": result.OK(), "outcome": string(result.Outcome), "message": result.Message,
	})
}

func (s *Server) saveVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.providerVideo == nil {
		s.notImplemented(w, r)
		return
	}
	var body videoConfigurationBody
	if err := decodeJSON(w, r, &body); err != nil {
		s.badRequest(w, r, err)
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	input := body.toInput()
	// Probe before writing: a configuration that cannot reach the upstream
	// service would silently break every generation task that follows.
	result, err := s.providerVideo.VerifyVideoConfiguration(r.Context(), rc.Actor.OrganizationID, input)
	if err != nil {
		s.writeVideoConfigurationError(w, r, err)
		return
	}
	if !result.OK() {
		writeProblem(w, http.StatusUnprocessableEntity, contract.Error{
			Code: "PROVIDER_VERIFICATION_FAILED", Message: result.Message,
			RequestID: requestIDFrom(r.Context()), Retryable: result.Outcome == provider.VideoProbeUpstreamError,
		})
		return
	}
	input.Verification = result
	config, err := s.providerVideo.SaveVideoConfiguration(r.Context(), rc.Actor.OrganizationID, input)
	if err != nil {
		s.writeVideoConfigurationError(w, r, err)
		return
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, videoConfigurationView(config, s.providerVideoEnv))
}

func (s *Server) writeVideoConfigurationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, provider.ErrVideoConfigurationManagedExternally):
		writeProblem(w, http.StatusConflict, contract.Error{
			Code: "PROVIDER_CONFIGURATION_MANAGED_EXTERNALLY", Message: "视频路由由 Adapter Gateway 管理，请在网关控制面维护。",
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
	case errors.Is(err, provider.ErrVideoConfigurationConflict):
		writeProblem(w, http.StatusConflict, contract.Error{
			Code: "PROVIDER_CONFIGURATION_CONFLICT", Message: "配置已被其他人改动，请刷新后重试。",
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
	case errors.Is(err, provider.ErrVideoConfigurationCredentialMissing):
		writeProblem(w, http.StatusBadRequest, contract.Error{
			Code: "PROVIDER_CREDENTIAL_REQUIRED", Message: "没有可用的已存密钥，请填写 API Key。",
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
	default:
		// Validation messages are written for the operator, so they reach the
		// page verbatim instead of being flattened into a generic failure.
		var invalid provider.VideoConfigurationInputError
		if errors.As(err, &invalid) {
			writeProblem(w, http.StatusBadRequest, contract.Error{
				Code: "INVALID_REQUEST", Message: invalid.Message,
				RequestID: requestIDFrom(r.Context()), Retryable: false,
			})
			return
		}
		s.writeServiceError(w, r, err)
	}
}
