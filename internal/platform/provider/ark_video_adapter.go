package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const (
	arkVideoProviderCode         = "ark-video"
	arkVideoDefaultBaseURL       = "https://ark.cn-beijing.volces.com/api/v3"
	arkVideoTaskPath             = "/contents/generations/tasks"
	arkVideoOutputTTL            = 15 * time.Minute
	arkVideoMaxBytes       int64 = 200 << 20
	arkVideoMaxImageBytes  int64 = 30 << 20
)

type ArkVideoConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// ArkVideoAdapter implements Ark's asynchronous Seedance task API. Result
// URLs are downloaded immediately into Provider's private output store and
// never cross the Provider/Assets boundary.
type ArkVideoAdapter struct {
	apiKey      string
	model       string
	baseURL     string
	client      *http.Client
	handles     OutputHandleStore
	credentials GatewayCredentialResolver
	now         func() time.Time
}

func NewRoutedArkVideoAdapter(credentials GatewayCredentialResolver, handles OutputHandleStore) (*ArkVideoAdapter, error) {
	if credentials == nil || handles == nil {
		return nil, fmt.Errorf("Ark video credential resolver and output handle store are required")
	}
	return &ArkVideoAdapter{
		client: &http.Client{Timeout: 3 * time.Minute}, handles: handles, credentials: credentials, now: time.Now,
	}, nil
}

func NewArkVideoAdapter(config ArkVideoConfig, handles OutputHandleStore) (*ArkVideoAdapter, error) {
	if strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" || handles == nil {
		return nil, fmt.Errorf("Ark video API key, model, and output handle store are required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = arkVideoDefaultBaseURL
	}
	return &ArkVideoAdapter{
		apiKey: strings.TrimSpace(config.APIKey), model: strings.TrimSpace(config.Model), baseURL: baseURL,
		client: &http.Client{Timeout: 3 * time.Minute}, handles: handles, now: time.Now,
	}, nil
}

// NewArkVideoAdapterWithRoutes builds an adapter that prefers the route stored
// by the Settings page and falls back to the environment credential when a job
// carries no route. Both sources stay available for the process lifetime, so a
// deployment can be configured from the UI without a restart.
func NewArkVideoAdapterWithRoutes(config ArkVideoConfig, credentials GatewayCredentialResolver, handles OutputHandleStore) (*ArkVideoAdapter, error) {
	if credentials == nil || handles == nil {
		return nil, fmt.Errorf("Ark video credential resolver and output handle store are required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = arkVideoDefaultBaseURL
	}
	return &ArkVideoAdapter{
		apiKey: strings.TrimSpace(config.APIKey), model: strings.TrimSpace(config.Model), baseURL: baseURL,
		client: &http.Client{Timeout: 3 * time.Minute}, handles: handles, credentials: credentials, now: time.Now,
	}, nil
}

func (*ArkVideoAdapter) ProviderCode() string { return arkVideoProviderCode }

func (a *ArkVideoAdapter) Submit(ctx context.Context, request VideoGenerationRequest) (VideoSubmission, error) {
	if err := request.Validate(); err != nil {
		return VideoSubmission{}, err
	}
	if request.Input.Resolution == "1080p" {
		return VideoSubmission{}, ExecutionError{JobError: contract.JobError{Code: "MODEL_INPUT_UNSUPPORTED", Message: "Seedance phase 1 supports 480p or 720p", Retryable: false}}
	}
	apiKey, model, baseURL, err := a.resolveInvocation(ctx, request.Route)
	if err != nil {
		return VideoSubmission{}, err
	}
	if err := validateArkVideoCapabilities(request, model); err != nil {
		return VideoSubmission{}, err
	}
	content, err := encodeArkVideoContent(request)
	if err != nil {
		return VideoSubmission{}, err
	}
	var generateAudio *bool
	switch request.Input.AudioPolicy {
	case VideoAudioSilent:
		value := false
		generateAudio = &value
	case VideoAudioGenerated:
		value := true
		generateAudio = &value
	}
	payload, err := json.Marshal(struct {
		Model         string            `json:"model"`
		Content       []arkVideoContent `json:"content"`
		Duration      int               `json:"duration"`
		Ratio         string            `json:"ratio"`
		Resolution    string            `json:"resolution"`
		GenerateAudio *bool             `json:"generate_audio,omitempty"`
		Watermark     bool              `json:"watermark"`
	}{
		Model:   model,
		Content: content, Duration: request.Input.DurationSeconds, Ratio: request.Input.AspectRatio,
		Resolution: request.Input.Resolution, GenerateAudio: generateAudio, Watermark: false,
	})
	if err != nil {
		return VideoSubmission{}, fmt.Errorf("encode Ark video request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+arkVideoTaskPath, bytes.NewReader(payload))
	if err != nil {
		return VideoSubmission{}, fmt.Errorf("build Ark video request: %w", err)
	}
	a.authorize(httpRequest, apiKey)
	response, err := a.client.Do(httpRequest)
	if err != nil {
		return VideoSubmission{}, ExecutionError{JobError: contract.JobError{Code: "MODEL_SUBMISSION_UNKNOWN", Message: "Ark video submission outcome is unknown and will not be retried automatically", Retryable: false}}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return VideoSubmission{}, arkVideoHTTPError("submission", response.StatusCode, body)
	}
	var decoded arkVideoTaskResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&decoded); err != nil || strings.TrimSpace(decoded.ID) == "" {
		return VideoSubmission{}, ExecutionError{JobError: contract.JobError{Code: "MODEL_RESPONSE_INVALID", Message: "Ark video submission response is invalid", Retryable: false}}
	}
	return VideoSubmission{
		Status: VideoSubmissionAccepted, ProviderCode: arkVideoProviderCode,
		ModelVersion: model, ExternalTaskID: decoded.ID,
	}, nil
}

type arkVideoContent struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *arkVideoImageURL `json:"image_url,omitempty"`
	Role     string            `json:"role,omitempty"`
}

type arkVideoImageURL struct {
	URL string `json:"url"`
}

func validateArkVideoCapabilities(request VideoGenerationRequest, model string) error {
	model = strings.ToLower(strings.TrimSpace(model))
	isSeedance20 := strings.Contains(model, "seedance-2-0")
	isSeedance15 := strings.Contains(model, "seedance-1-5")
	mode := request.Input.InputMode
	if mode == "" {
		mode = VideoInputTextOnly
	}
	if request.Route != nil {
		allowedModes := request.Route.VideoInputModes
		if len(allowedModes) == 0 {
			allowedModes = []VideoInputMode{VideoInputTextOnly}
		}
		if !containsVideoInputMode(allowedModes, mode) {
			return ExecutionError{JobError: contract.JobError{
				Code: "MODEL_INPUT_UNSUPPORTED", Message: "Ark route has not declared the requested video input mode", Retryable: false,
			}}
		}
		if request.Input.AudioPolicy != "" && !containsVideoAudioPolicy(request.Route.VideoAudioPolicies, request.Input.AudioPolicy) {
			return ExecutionError{JobError: contract.JobError{
				Code: "MODEL_INPUT_UNSUPPORTED", Message: "Ark route has not declared the requested video audio policy", Retryable: false,
			}}
		}
	}
	if (mode == VideoInputReferenceImage || mode == VideoInputFirstLastFrame) && !isSeedance20 {
		return ExecutionError{JobError: contract.JobError{
			Code: "MODEL_INPUT_UNSUPPORTED", Message: "configured Ark model does not support the requested Seedance 2.0 image input mode", Retryable: false,
		}}
	}
	if request.Input.AudioPolicy != "" && !isSeedance20 && !isSeedance15 {
		return ExecutionError{JobError: contract.JobError{
			Code: "MODEL_INPUT_UNSUPPORTED", Message: "configured Ark model does not support explicit video audio policy", Retryable: false,
		}}
	}
	return nil
}

func containsVideoInputMode(values []VideoInputMode, target VideoInputMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsVideoAudioPolicy(values []VideoAudioPolicy, target VideoAudioPolicy) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func encodeArkVideoContent(request VideoGenerationRequest) ([]arkVideoContent, error) {
	content := make([]arkVideoContent, 0, 1+len(request.Sources))
	content = append(content, arkVideoContent{Type: "text", Text: request.Input.Prompt})
	for index, source := range request.Sources {
		if source.AuthorizedAsset != nil {
			if source.AuthorizedAsset.ProviderCode != arkVideoProviderCode {
				return nil, ExecutionError{JobError: contract.JobError{
					Code: "MODEL_INPUT_UNSUPPORTED", Message: fmt.Sprintf("authorized video asset at index %d targets another provider", index), Retryable: false,
				}}
			}
			content = append(content, arkVideoContent{
				Type: "image_url", ImageURL: &arkVideoImageURL{URL: "asset://" + source.AuthorizedAsset.AssetID}, Role: string(source.Role),
			})
			continue
		}
		mimeType := strings.ToLower(strings.TrimSpace(source.MIMEType))
		switch mimeType {
		case "image/png", "image/jpeg", "image/webp":
		default:
			return nil, ExecutionError{JobError: contract.JobError{
				Code: "MODEL_INPUT_UNSUPPORTED", Message: fmt.Sprintf("video conditioning image at index %d has an unsupported MIME type", index), Retryable: false,
			}}
		}
		contents, err := io.ReadAll(io.LimitReader(source.Content, arkVideoMaxImageBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read video conditioning image at index %d: %w", index, err)
		}
		if len(contents) == 0 || int64(len(contents)) > arkVideoMaxImageBytes {
			return nil, ExecutionError{JobError: contract.JobError{
				Code: "MODEL_INPUT_UNSUPPORTED", Message: fmt.Sprintf("video conditioning image at index %d must be between 1 byte and 30 MB", index), Retryable: false,
			}}
		}
		content = append(content, arkVideoContent{
			Type: "image_url",
			ImageURL: &arkVideoImageURL{
				URL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(contents),
			},
			Role: string(source.Role),
		})
	}
	return content, nil
}

func (a *ArkVideoAdapter) Poll(ctx context.Context, reference VideoTaskReference) (VideoTaskResult, error) {
	if err := reference.Validate(); err != nil {
		return VideoTaskResult{}, err
	}
	if reference.ProviderCode != arkVideoProviderCode {
		return VideoTaskResult{}, fmt.Errorf("Ark video task reference targets another provider")
	}
	apiKey, _, baseURL, err := a.resolveInvocation(ctx, reference.Route)
	if err != nil {
		return VideoTaskResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+arkVideoTaskPath+"/"+reference.ExternalTaskID, nil)
	if err != nil {
		return VideoTaskResult{}, fmt.Errorf("build Ark video poll request: %w", err)
	}
	a.authorize(httpRequest, apiKey)
	response, err := a.client.Do(httpRequest)
	if err != nil {
		return VideoTaskResult{}, fmt.Errorf("poll Ark video task: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		executionErr := arkVideoHTTPError("poll", response.StatusCode, body)
		if executionErr.JobError.Retryable {
			return VideoTaskResult{}, fmt.Errorf("%s", executionErr.JobError.Message)
		}
		return VideoTaskResult{}, executionErr
	}
	var decoded arkVideoTaskResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&decoded); err != nil {
		return VideoTaskResult{}, fmt.Errorf("decode Ark video task response: %w", err)
	}
	switch strings.ToLower(decoded.Status) {
	case "queued":
		return VideoTaskResult{Status: VideoTaskRunning, Progress: 25}, nil
	case "running":
		return VideoTaskResult{Status: VideoTaskRunning, Progress: 50}, nil
	case "succeeded":
		if strings.TrimSpace(decoded.Content.VideoURL) == "" {
			return VideoTaskResult{}, ExecutionError{JobError: contract.JobError{Code: "MODEL_RESPONSE_INVALID", Message: "Ark video task succeeded without a video URL", Retryable: false}}
		}
		contents, err := a.downloadVideo(ctx, decoded.Content.VideoURL)
		if err != nil {
			return VideoTaskResult{}, err
		}
		ref, err := newVideoOutputRef(reference.ProviderJobID, contents, a.now().UTC().Add(arkVideoOutputTTL))
		if err != nil {
			return VideoTaskResult{}, err
		}
		project := contract.ProjectRef{OrganizationID: reference.OrganizationID, ProjectID: reference.ProjectID, ProjectContextVersion: 1}
		if err := a.handles.Put(ctx, project, ref, contents); err != nil {
			return VideoTaskResult{}, fmt.Errorf("retain Ark video output: %w", err)
		}
		return VideoTaskResult{Status: VideoTaskSucceeded, Outputs: []contract.ProviderOutputRef{ref}}, nil
	case "failed", "cancelled", "canceled":
		message := strings.TrimSpace(decoded.Error.Message)
		if message == "" {
			message = "Ark video generation failed"
		}
		return VideoTaskResult{Status: VideoTaskFailed, Error: &contract.JobError{Code: "MODEL_GENERATION_FAILED", Message: message, Retryable: false}}, nil
	default:
		return VideoTaskResult{}, ExecutionError{JobError: contract.JobError{Code: "MODEL_RESPONSE_INVALID", Message: "Ark video task status is invalid", Retryable: false}}
	}
}

func (a *ArkVideoAdapter) Open(ctx context.Context, project contract.ProjectRef, ref contract.ProviderOutputRef) (io.ReadCloser, contract.OutputMetadata, error) {
	if ref.ProviderCode != arkVideoProviderCode {
		return nil, contract.OutputMetadata{}, ErrOutputHandleNotFound
	}
	return a.handles.Open(ctx, project, ref)
}

func (a *ArkVideoAdapter) downloadVideo(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Ark video output: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Ark video output returned HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, arkVideoMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Ark video output: %w", err)
	}
	if len(contents) < 12 || int64(len(contents)) > arkVideoMaxBytes || string(contents[4:8]) != "ftyp" {
		return nil, ExecutionError{JobError: contract.JobError{Code: "MODEL_OUTPUT_UNSUPPORTED", Message: "Ark video output is not a supported MP4", Retryable: false}}
	}
	return contents, nil
}

func (a *ArkVideoAdapter) authorize(request *http.Request, apiKey string) {
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	if request.Method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
}

func (a *ArkVideoAdapter) resolveInvocation(ctx context.Context, route *VideoRouteSnapshot) (apiKey, model, baseURL string, err error) {
	if route == nil {
		if strings.TrimSpace(a.apiKey) == "" || strings.TrimSpace(a.model) == "" || strings.TrimSpace(a.baseURL) == "" {
			return "", "", "", fmt.Errorf("Ark video route is required")
		}
		return a.apiKey, a.model, a.baseURL, nil
	}
	if a.credentials == nil {
		return "", "", "", fmt.Errorf("Ark video credential resolver is required")
	}
	if err := route.ValidateVideoWithPolicy(false); err != nil {
		return "", "", "", fmt.Errorf("invalid Ark video route: %w", err)
	}
	credential, err := a.credentials.ResolveGatewayCredential(ctx, route.CredentialID, route.CredentialVersion)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve Ark video credential: %w", err)
	}
	return credential, route.UpstreamModel, strings.TrimRight(route.BaseURL, "/"), nil
}

func newVideoOutputRef(providerJobID string, contents []byte, expiresAt time.Time) (contract.ProviderOutputRef, error) {
	return newOutputRef(arkVideoProviderCode, providerJobID, "output_1", "video/mp4", contents, expiresAt)
}

func arkVideoHTTPError(operation string, status int, body []byte) ExecutionError {
	problem := contract.JobError{Code: "MODEL_REQUEST_REJECTED", Message: fmt.Sprintf("Ark video %s returned HTTP %d", operation, status), Retryable: false}
	var upstream struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &upstream) == nil {
		if value := strings.TrimSpace(upstream.Error.Code); value != "" {
			problem.Code = value
		}
		if value := strings.TrimSpace(upstream.Error.Message); value != "" {
			problem.Message = value
		}
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		if problem.Code == "MODEL_REQUEST_REJECTED" {
			problem.Code = "MODEL_TEMPORARILY_UNAVAILABLE"
		}
		problem.Retryable = true
	}
	return ExecutionError{JobError: problem}
}

type arkVideoTaskResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
