package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/identity"
	"github.com/shikanon/cookies/internal/platform/provider"
)

// recordingVideoConfigurationStore stands in for MySQL. It records what the
// handlers hand down so the tests can assert the probe ran before the write.
type recordingVideoConfigurationStore struct {
	config      provider.VideoConfiguration
	probe       provider.VideoProbeResult
	probeErr    error
	saveErr     error
	verifyCalls []provider.VideoConfigurationInput
	saveCalls   []provider.VideoConfigurationInput
}

func (s *recordingVideoConfigurationStore) GetVideoConfiguration(context.Context, contract.OrganizationID) (provider.VideoConfiguration, error) {
	return s.config, nil
}

func (s *recordingVideoConfigurationStore) VerifyVideoConfiguration(_ context.Context, _ contract.OrganizationID, input provider.VideoConfigurationInput) (provider.VideoProbeResult, error) {
	s.verifyCalls = append(s.verifyCalls, input)
	return s.probe, s.probeErr
}

func (s *recordingVideoConfigurationStore) SaveVideoConfiguration(_ context.Context, _ contract.OrganizationID, input provider.VideoConfigurationInput) (provider.VideoConfiguration, error) {
	s.saveCalls = append(s.saveCalls, input)
	if s.saveErr != nil {
		return provider.VideoConfiguration{}, s.saveErr
	}
	s.config = provider.VideoConfiguration{
		Configured: true, BaseURL: input.BaseURL, UpstreamModel: input.Model,
		MaskedAPIKey: provider.MaskAPIKey(input.APIKey), CredentialReadable: true,
		Version: s.config.Version + 1, UpdatedAt: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC),
	}
	return s.config, nil
}

func newVideoConfigurationServer(t *testing.T, store ProviderVideoConfigurationStore) *Server {
	t.Helper()
	resolver, err := identity.NewStaticResolver(contract.ActorContext{
		OrganizationID: "org_1",
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: "user_1"},
		Scopes:         []contract.Scope{"provider.configuration.write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewWithDependencies(Dependencies{
		Resolver:                   resolver,
		ProviderVideoConfiguration: store,
		ProviderVideoEnvironment: ProviderVideoEnvironment{
			Configured: true, Model: "doubao-seedance-1-0-lite-t2v-250428",
			BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		},
	})
}

func TestReadVideoConfigurationNeverReturnsTheStoredKey(t *testing.T) {
	t.Parallel()
	store := &recordingVideoConfigurationStore{config: provider.VideoConfiguration{
		Configured: true, BaseURL: "https://ark.example/api/v3", UpstreamModel: "seedance",
		MaskedAPIKey: provider.MaskAPIKey("ark-secret-abcd"), CredentialReadable: true, Version: 3,
	}}
	response := httptest.NewRecorder()
	newVideoConfigurationServer(t, store).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform/v1/provider/video-configuration", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "ark-secret") {
		t.Fatalf("read response leaked the credential: %s", body)
	}
	if !strings.Contains(body, `"masked_api_key":"****abcd"`) || !strings.Contains(body, `"version":3`) {
		t.Fatalf("read response is missing the safe fields: %s", body)
	}
	if !strings.Contains(body, `"environment_fallback"`) {
		t.Fatalf("read response should describe the environment fallback: %s", body)
	}
}

func TestSaveVideoConfigurationRefusesToStoreAnUnverifiedCredential(t *testing.T) {
	t.Parallel()
	store := &recordingVideoConfigurationStore{probe: provider.VideoProbeResult{
		Outcome: provider.VideoProbeUnauthorized, Message: "密钥被拒绝，请确认填的是完整的 API Key",
	}}
	request := httptest.NewRequest(http.MethodPut, "/platform/v1/provider/video-configuration",
		bytes.NewBufferString(`{"base_url":"https://ark.example/api/v3","model":"seedance","api_key":"ark-wrong-key"}`))
	response := httptest.NewRecorder()
	newVideoConfigurationServer(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(store.saveCalls) != 0 {
		t.Fatalf("a rejected credential must not be written: %+v", store.saveCalls)
	}
	body := response.Body.String()
	if !strings.Contains(body, "密钥被拒绝") || strings.Contains(body, "ark-wrong-key") {
		t.Fatalf("unexpected failure body: %s", body)
	}
}

func TestSaveVideoConfigurationProbesThenWrites(t *testing.T) {
	t.Parallel()
	store := &recordingVideoConfigurationStore{probe: provider.VideoProbeResult{
		Outcome: provider.VideoProbeOK, Message: "连接正常",
	}}
	request := httptest.NewRequest(http.MethodPut, "/platform/v1/provider/video-configuration",
		bytes.NewBufferString(`{"base_url":"https://ark.example/api/v3","model":"seedance","api_key":"ark-good-key-wxyz"}`))
	response := httptest.NewRecorder()
	newVideoConfigurationServer(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(store.verifyCalls) != 1 || len(store.saveCalls) != 1 {
		t.Fatalf("expected exactly one probe then one write: verify=%d save=%d", len(store.verifyCalls), len(store.saveCalls))
	}
	if !store.saveCalls[0].Verification.OK() {
		t.Fatalf("the write must carry the probe outcome: %+v", store.saveCalls[0].Verification)
	}
	body := response.Body.String()
	if strings.Contains(body, "ark-good-key") {
		t.Fatalf("save response leaked the credential: %s", body)
	}
	if !strings.Contains(body, `"masked_api_key":"****wxyz"`) {
		t.Fatalf("save response is missing the mask: %s", body)
	}
}

func TestVerifyVideoConfigurationReusesTheStoredKeyWhenOmitted(t *testing.T) {
	t.Parallel()
	store := &recordingVideoConfigurationStore{probe: provider.VideoProbeResult{
		Outcome: provider.VideoProbeOK, Message: "连接正常",
	}}
	request := httptest.NewRequest(http.MethodPost, "/platform/v1/provider/video-configuration/verification",
		bytes.NewBufferString(`{"base_url":"https://ark.example/api/v3","model":"seedance"}`))
	response := httptest.NewRecorder()
	newVideoConfigurationServer(t, store).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(store.verifyCalls) != 1 || store.verifyCalls[0].APIKey != "" {
		t.Fatalf("an omitted key must reach the store as empty: %+v", store.verifyCalls)
	}
	if len(store.saveCalls) != 0 {
		t.Fatalf("verification must not write anything: %+v", store.saveCalls)
	}
	if !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected verification body: %s", response.Body.String())
	}
}

func TestSaveVideoConfigurationMapsStoreFailuresToActionableProblems(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		probeErr   error
		saveErr    error
		wantStatus int
		wantCode   string
	}{
		{name: "no stored key", probeErr: provider.ErrVideoConfigurationCredentialMissing, wantStatus: http.StatusBadRequest, wantCode: "PROVIDER_CREDENTIAL_REQUIRED"},
		{name: "plain HTTP", probeErr: provider.VideoConfigurationInputError{Message: "服务地址必须是 HTTPS"}, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "concurrent edit", saveErr: provider.ErrVideoConfigurationConflict, wantStatus: http.StatusConflict, wantCode: "PROVIDER_CONFIGURATION_CONFLICT"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingVideoConfigurationStore{
				probe:    provider.VideoProbeResult{Outcome: provider.VideoProbeOK, Message: "连接正常"},
				probeErr: testCase.probeErr, saveErr: testCase.saveErr,
			}
			request := httptest.NewRequest(http.MethodPut, "/platform/v1/provider/video-configuration",
				bytes.NewBufferString(`{"base_url":"https://ark.example/api/v3","model":"seedance"}`))
			response := httptest.NewRecorder()
			newVideoConfigurationServer(t, store).ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), testCase.wantCode) {
				t.Fatalf("body=%s want code %s", response.Body.String(), testCase.wantCode)
			}
		})
	}
}
