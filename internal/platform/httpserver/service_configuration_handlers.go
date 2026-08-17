package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// serviceConfigurationBody is what the settings page submits. Values holds
// only the fields declared by the catalog entry; an omitted secret means
// "keep the stored one".
type serviceConfigurationBody struct {
	Values          map[string]string `json:"values"`
	ExpectedVersion *int64            `json:"expected_version"`
}

// serviceConfigurationView is the only shape a credential ever reaches the
// browser in: a mask, never the key.
func serviceConfigurationView(service servicecatalog.Service, config provider.ServiceConfiguration) map[string]any {
	view := map[string]any{
		"code":                service.Code,
		"display_name":        service.DisplayName,
		"tier":                string(service.Tier),
		"impact":              service.Impact,
		"fields":              service.Fields,
		"env_keys":            service.EnvKeys,
		"restart_required":    service.RestartRequired,
		"configured":          config.Configured,
		"values":              config.Values,
		"masked_secrets":      config.MaskedSecrets,
		"credential_readable": config.CredentialReadable,
		"version":             config.Version,
	}
	if service.ManagedNote != "" {
		view["managed_note"] = service.ManagedNote
	}
	if !config.UpdatedAt.IsZero() {
		view["updated_at"] = config.UpdatedAt
	}
	probe := map[string]any{"outcome": string(config.LastProbe.Outcome), "message": config.LastProbe.Message}
	if config.LastProbe.UpstreamMessage != "" {
		probe["upstream_message"] = config.LastProbe.UpstreamMessage
	}
	if config.LastProbedAt != nil {
		probe["probed_at"] = *config.LastProbedAt
	}
	view["last_probe"] = probe
	if len(config.EnvironmentFallback) > 0 {
		view["environment_fallback"] = config.EnvironmentFallback
	}
	return view
}

func (s *Server) listServiceConfigurations(w http.ResponseWriter, r *http.Request) {
	if s.serviceConfigurations == nil {
		s.notImplemented(w, r)
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	items := []map[string]any{}
	for _, service := range servicecatalog.All() {
		config := provider.ServiceConfiguration{Code: service.Code}
		if service.Tier == servicecatalog.TierEditable {
			stored, err := s.serviceConfigurations.GetServiceConfiguration(r.Context(), rc.Actor.OrganizationID, service.Code)
			if err == nil {
				config = stored
			}
			// One unreadable service must not blank the whole page; that row
			// simply reports itself as unconfigured.
		}
		items = append(items, serviceConfigurationView(service, config))
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"services": items})
}

func (s *Server) saveServiceConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.serviceConfigurations == nil {
		s.notImplemented(w, r)
		return
	}
	service, input, ok := s.readServiceConfigurationRequest(w, r)
	if !ok {
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	config, err := s.serviceConfigurations.SaveServiceConfiguration(
		r.Context(), rc.Actor.OrganizationID, rc.Actor.Principal.ID, input)
	if err != nil {
		s.writeServiceConfigurationError(w, r, err)
		return
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, serviceConfigurationView(service, config))
}

func (s *Server) verifyServiceConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.serviceConfigurations == nil {
		s.notImplemented(w, r)
		return
	}
	_, input, ok := s.readServiceConfigurationRequest(w, r)
	if !ok {
		return
	}
	rc, _ := contract.RequestContextFrom(r.Context())
	result, err := s.serviceConfigurations.VerifyServiceConfiguration(r.Context(), rc.Actor.OrganizationID, input)
	if err != nil {
		s.writeServiceConfigurationError(w, r, err)
		return
	}
	writerHeaderNoStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"outcome": string(result.Outcome), "message": result.Message,
		"upstream_message": result.UpstreamMessage,
	})
}

// readServiceConfigurationRequest resolves the {code} path segment against the
// catalog before the body is trusted, so an unknown code is refused with a 404
// rather than reaching storage as an input error.
func (s *Server) readServiceConfigurationRequest(w http.ResponseWriter, r *http.Request) (servicecatalog.Service, provider.ServiceConfigurationInput, bool) {
	code := r.PathValue("code")
	service, found := servicecatalog.Find(code)
	if !found {
		writeProblem(w, http.StatusNotFound, contract.Error{
			Code: "SERVICE_NOT_FOUND", Message: "没有这个服务。",
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
		return servicecatalog.Service{}, provider.ServiceConfigurationInput{}, false
	}
	var body serviceConfigurationBody
	if err := decodeJSON(w, r, &body); err != nil {
		s.badRequest(w, r, err)
		return servicecatalog.Service{}, provider.ServiceConfigurationInput{}, false
	}
	return service, provider.ServiceConfigurationInput{
		Code: code, Values: body.Values, ExpectedVersion: body.ExpectedVersion,
	}, true
}

func (s *Server) writeServiceConfigurationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, provider.ErrServiceConfigurationConflict):
		writeProblem(w, http.StatusConflict, contract.Error{
			Code: "PROVIDER_CONFIGURATION_CONFLICT", Message: "配置已被其他人改动，请刷新后重试。",
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
	case errors.Is(err, provider.ErrServiceProbeFailed):
		// The probe's message already carries the upstream's own words, so it
		// reaches the operator verbatim instead of a generic failure.
		writeProblem(w, http.StatusUnprocessableEntity, contract.Error{
			Code: "PROVIDER_VERIFICATION_FAILED", Message: probeFailureMessage(err),
			RequestID: requestIDFrom(r.Context()), Retryable: false,
		})
	default:
		var invalid provider.ServiceConfigurationInputError
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

// probeFailureMessage strips the sentinel prefix the store wraps in, so the
// page shows the diagnosis rather than "service probe failed: 密钥无效".
func probeFailureMessage(err error) string {
	return strings.TrimPrefix(err.Error(), provider.ErrServiceProbeFailed.Error()+": ")
}
