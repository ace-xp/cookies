package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const (
	videoGenerateJobKind      = "provider.video.generate"
	videoGenerateOperation    = "video.generate"
	videoExecutionMaxAttempts = 360
)

// VideoGenerationInput is the stable Provider-owned input for asynchronous
// video creation. Vendor request shapes remain private to video adapters.
type VideoGenerationInput struct {
	Prompt             string                   `json:"prompt"`
	DurationSeconds    int                      `json:"duration_seconds"`
	AspectRatio        string                   `json:"aspect_ratio"`
	Resolution         string                   `json:"resolution"`
	AudioPolicy        VideoAudioPolicy         `json:"audio_policy,omitempty"`
	InputMode          VideoInputMode           `json:"input_mode,omitempty"`
	ConditioningAssets []VideoConditioningAsset `json:"conditioning_assets,omitempty"`
}

type VideoAudioPolicy string

const (
	VideoAudioSilent    VideoAudioPolicy = "silent"
	VideoAudioGenerated VideoAudioPolicy = "generated_audio"
)

type VideoInputMode string

const (
	VideoInputTextOnly       VideoInputMode = "text_only"
	VideoInputReferenceImage VideoInputMode = "reference_image"
	VideoInputFirstLastFrame VideoInputMode = "first_last_frame"
)

type VideoConditioningRole string

const (
	VideoConditioningReferenceImage VideoConditioningRole = "reference_image"
	VideoConditioningFirstFrame     VideoConditioningRole = "first_frame"
	VideoConditioningLastFrame      VideoConditioningRole = "last_frame"
)

// VideoConditioningAsset records only an immutable Assets-owned reference.
// Provider resolves authorized content just before submission, so durable job
// state never contains expiring URLs or object-storage credentials.
type VideoConditioningAsset struct {
	Role            VideoConditioningRole          `json:"role"`
	Reference       contract.ProjectAssetRef       `json:"reference"`
	AuthorizedAsset *VideoAuthorizedAssetReference `json:"authorized_asset,omitempty"`
}

// VideoAuthorizedAssetReference points at a provider-managed, pre-authorized
// asset. The local project asset remains the immutable Creative lineage, while
// adapters may submit this reference instead of re-uploading its image bytes.
type VideoAuthorizedAssetReference struct {
	ProviderCode string `json:"provider_code"`
	AssetID      string `json:"asset_id"`
}

func (r VideoAuthorizedAssetReference) Validate() error {
	if strings.TrimSpace(r.ProviderCode) == "" {
		return fmt.Errorf("authorized video asset provider code is required")
	}
	assetID := strings.TrimSpace(r.AssetID)
	if !strings.HasPrefix(assetID, "asset-") || len(assetID) <= len("asset-") {
		return fmt.Errorf("authorized video asset ID must use the asset- prefix")
	}
	for _, character := range assetID[len("asset-"):] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_') {
			return fmt.Errorf("authorized video asset ID contains unsupported characters")
		}
	}
	return nil
}

func (i VideoGenerationInput) Validate() error {
	if strings.TrimSpace(i.Prompt) == "" {
		return fmt.Errorf("video prompt is required")
	}
	if i.DurationSeconds < 4 || i.DurationSeconds > 15 {
		return fmt.Errorf("video duration must be between 4 and 15 seconds")
	}
	switch i.AspectRatio {
	case "9:16", "16:9", "1:1":
	default:
		return fmt.Errorf("video aspect ratio is not supported")
	}
	switch i.Resolution {
	case "480p", "720p", "1080p":
	default:
		return fmt.Errorf("video resolution is not supported")
	}
	switch i.AudioPolicy {
	case "", VideoAudioSilent, VideoAudioGenerated:
	default:
		return fmt.Errorf("video audio policy is not supported")
	}
	mode := i.InputMode
	if mode == "" {
		mode = VideoInputTextOnly
	}
	seenRoles := make(map[VideoConditioningRole]struct{}, len(i.ConditioningAssets))
	seenReferences := make(map[string]struct{}, len(i.ConditioningAssets))
	for index, asset := range i.ConditioningAssets {
		if err := asset.Reference.Validate(); err != nil {
			return fmt.Errorf("invalid video conditioning asset at index %d: %w", index, err)
		}
		if asset.AuthorizedAsset != nil {
			if err := asset.AuthorizedAsset.Validate(); err != nil {
				return fmt.Errorf("invalid authorized video conditioning asset at index %d: %w", index, err)
			}
		}
		switch asset.Role {
		case VideoConditioningReferenceImage, VideoConditioningFirstFrame, VideoConditioningLastFrame:
		default:
			return fmt.Errorf("video conditioning role at index %d is not supported", index)
		}
		if _, exists := seenRoles[asset.Role]; exists {
			return fmt.Errorf("video conditioning role %q is duplicated", asset.Role)
		}
		seenRoles[asset.Role] = struct{}{}
		key := fmt.Sprintf("%s:%s:%d", asset.Reference.ProjectID, asset.Reference.AssetVersion.AssetID, asset.Reference.AssetVersion.Version)
		if _, exists := seenReferences[key]; exists {
			return fmt.Errorf("video conditioning asset at index %d is duplicated", index)
		}
		seenReferences[key] = struct{}{}
	}
	switch mode {
	case VideoInputTextOnly:
		if len(i.ConditioningAssets) != 0 {
			return fmt.Errorf("text-only video input does not accept conditioning assets")
		}
	case VideoInputReferenceImage:
		if len(i.ConditioningAssets) != 1 || i.ConditioningAssets[0].Role != VideoConditioningReferenceImage {
			return fmt.Errorf("reference-image video input requires exactly one reference_image asset")
		}
	case VideoInputFirstLastFrame:
		if len(i.ConditioningAssets) != 2 {
			return fmt.Errorf("first-last-frame video input requires exactly two conditioning assets")
		}
		if _, ok := seenRoles[VideoConditioningFirstFrame]; !ok {
			return fmt.Errorf("first-last-frame video input requires a first_frame asset")
		}
		if _, ok := seenRoles[VideoConditioningLastFrame]; !ok {
			return fmt.Errorf("first-last-frame video input requires a last_frame asset")
		}
	default:
		return fmt.Errorf("video input mode is not supported")
	}
	return nil
}

// CreateVideoJobRequest is the Provider application seam for video jobs.
// Actor and Project must already have been resolved from trusted context.
type CreateVideoJobRequest struct {
	Actor          contract.ActorContext
	Project        contract.ProjectContext
	IdempotencyKey contract.IdempotencyKey
	RequestHash    string
	ModelAlias     string
	SourceSystem   string
	SourceTaskID   string
	Input          VideoGenerationInput
}

func (r CreateVideoJobRequest) Validate() error {
	if err := r.Actor.Validate(); err != nil {
		return fmt.Errorf("invalid actor: %w", err)
	}
	if !r.Actor.HasScope(ScopeJobCreate) {
		return fmt.Errorf("%s scope is required", ScopeJobCreate)
	}
	if err := r.Project.ValidateBrandBound(); err != nil {
		return fmt.Errorf("invalid project for video generation: %w", err)
	}
	if r.Project.OrganizationID != r.Actor.OrganizationID {
		return fmt.Errorf("project organization does not match actor organization")
	}
	if err := r.IdempotencyKey.Validate(); err != nil {
		return err
	}
	if !validSHA256(r.RequestHash) {
		return fmt.Errorf("request hash must be a lowercase hexadecimal SHA-256 digest")
	}
	if strings.TrimSpace(r.ModelAlias) == "" {
		return fmt.Errorf("model alias is required")
	}
	if err := r.Input.Validate(); err != nil {
		return err
	}
	for index, asset := range r.Input.ConditioningAssets {
		if asset.Reference.ProjectID != r.Project.ProjectID {
			return fmt.Errorf("video conditioning asset at index %d belongs to another project", index)
		}
	}
	return nil
}

func (s Service) CreateVideoJob(ctx context.Context, request CreateVideoJobRequest) (contract.ProviderJob, bool, error) {
	if s.Store == nil {
		return contract.ProviderJob{}, false, fmt.Errorf("provider job store is required")
	}
	if s.NewID == nil {
		return contract.ProviderJob{}, false, fmt.Errorf("provider job ID generator is required")
	}
	if s.Scheduler == nil {
		return contract.ProviderJob{}, false, fmt.Errorf("provider execution scheduler is required")
	}
	if err := request.Validate(); err != nil {
		return contract.ProviderJob{}, false, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	providerJobID, err := s.NewID()
	if err != nil {
		return contract.ProviderJob{}, false, fmt.Errorf("generate provider job ID: %w", err)
	}
	createdAt := now().UTC()
	var route *VideoRouteSnapshot
	if s.VideoRoutes != nil {
		resolved, resolveErr := s.VideoRoutes.ResolveVideoRoute(ctx, request.Actor.OrganizationID, request.ModelAlias)
		switch {
		case resolveErr == nil:
			route = &resolved
		case errors.Is(resolveErr, ErrGatewayRouteNotFound) && s.VideoRouteOptional:
			// Nothing configured in the Settings page yet: leave the route nil so
			// the adapter uses the credential it was built with.
			route = nil
		default:
			return contract.ProviderJob{}, false, fmt.Errorf("resolve provider video route: %w", resolveErr)
		}
	}
	job := contract.ProviderJob{
		ID:               providerJobID,
		Kind:             videoGenerateJobKind,
		OrganizationID:   request.Actor.OrganizationID,
		ProjectID:        request.Project.ProjectID,
		ExecutionStatus:  contract.JobQueued,
		ProviderStatus:   contract.ProviderJobSubmitted,
		Progress:         0,
		ProjectAssetRefs: []contract.ProjectAssetRef{},
		AttemptCount:     0,
		MaxAttempts:      videoExecutionMaxAttempts,
		Version:          1,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	if err := job.Validate(); err != nil {
		return contract.ProviderJob{}, false, fmt.Errorf("create provider video job: %w", err)
	}
	stored, duplicate, err := s.Store.Create(ctx, JobRecord{
		Job:                   job,
		Principal:             request.Actor.Principal,
		Operation:             videoGenerateOperation,
		IdempotencyKey:        request.IdempotencyKey,
		RequestHash:           request.RequestHash,
		ProjectContextVersion: request.Project.ProjectContextVersion,
		ModelAlias:            request.ModelAlias,
		SourceSystem:          request.SourceSystem,
		SourceTaskID:          request.SourceTaskID,
		VideoInput:            request.Input,
		Route:                 route,
		SubmissionState:       SubmissionNotStarted,
	})
	if err != nil {
		return contract.ProviderJob{}, false, err
	}
	if err := s.Scheduler.Schedule(ctx, stored.Job); err != nil {
		return stored.Job, duplicate, fmt.Errorf("schedule provider video job execution: %w", err)
	}
	return stored.Job, duplicate, nil
}
