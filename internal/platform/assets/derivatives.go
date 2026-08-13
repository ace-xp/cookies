package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
	"github.com/shikanon/cookies/internal/platform/jobruntime"
)

type DerivativeProfile string

const (
	DerivativeVideoProxy DerivativeProfile = "video_proxy_v1"
	DerivativePoster     DerivativeProfile = "poster_v1"
	DerivativeWaveform   DerivativeProfile = "waveform_v1"
	DerivativeFontWeb    DerivativeProfile = "font_woff2_v1"
)

type DerivativeStatus string

const (
	DerivativeQueued  DerivativeStatus = "queued"
	DerivativeRunning DerivativeStatus = "running"
	DerivativeReady   DerivativeStatus = "ready"
	DerivativeFailed  DerivativeStatus = "failed"
)

type ProcessingStatus string

const (
	ProcessingQueued    ProcessingStatus = "queued"
	ProcessingRunning   ProcessingStatus = "running"
	ProcessingSucceeded ProcessingStatus = "succeeded"
	ProcessingFailed    ProcessingStatus = "failed"
)

type AssetDerivative struct {
	ID             string                    `json:"id"`
	OrganizationID contract.OrganizationID   `json:"organization_id"`
	ProjectID      contract.ProjectID        `json:"project_id"`
	Source         contract.AssetVersionRef  `json:"source"`
	Profile        DerivativeProfile         `json:"profile"`
	Status         DerivativeStatus          `json:"status"`
	Output         *contract.AssetVersionRef `json:"output,omitempty"`
	ErrorCode      string                    `json:"error_code,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

type ProcessingJob struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`
	DerivativeID   string                  `json:"derivative_id"`
	Status         ProcessingStatus        `json:"status"`
	Attempt        int                     `json:"attempt"`
	ErrorCode      string                  `json:"error_code,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
}

type EnsureDerivativeRequest struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	AssetRef       contract.AssetVersionRef
	Profile        DerivativeProfile
}

type DerivativeRepository interface {
	EnsureDerivative(context.Context, AssetDerivative, ProcessingJob) (AssetDerivative, ProcessingJob, bool, error)
	FailDerivativeScheduling(context.Context, string, string, time.Time) (AssetDerivative, ProcessingJob, error)
	RetryDerivative(context.Context, contract.OrganizationID, contract.ProjectID, string, string, time.Time) (AssetDerivative, ProcessingJob, error)
	GetDerivative(context.Context, contract.OrganizationID, contract.ProjectID, contract.AssetVersionRef, DerivativeProfile) (AssetDerivative, error)
	GetDerivativeByID(context.Context, string) (AssetDerivative, error)
	CompleteDerivative(context.Context, string, contract.AssetVersionRef, time.Time) (AssetDerivative, error)
}

type DerivativeScheduler interface {
	ScheduleAssetDerivative(context.Context, ProcessingJob) error
}

type DerivativeService struct {
	Repository DerivativeRepository
	Scheduler  DerivativeScheduler
	NewID      ids.Generator
	Now        func() time.Time
}

func (s DerivativeService) EnsureDerivative(ctx context.Context, request EnsureDerivativeRequest) (AssetDerivative, ProcessingJob, bool, error) {
	if s.Repository == nil || s.Scheduler == nil {
		return AssetDerivative{}, ProcessingJob{}, false, fmt.Errorf("derivative repository and scheduler are required")
	}
	if request.OrganizationID == "" || request.ProjectID == "" || !validDerivativeProfile(request.Profile) {
		return AssetDerivative{}, ProcessingJob{}, false, fmt.Errorf("derivative scope and supported profile are required")
	}
	if err := request.AssetRef.Validate(); err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	newID := s.NewID
	if newID == nil {
		newID = ids.New
	}
	derivativeID, err := newID("derivative")
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	jobID, err := newID("assetjob")
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now()
	}
	value := AssetDerivative{ID: derivativeID, OrganizationID: request.OrganizationID, ProjectID: request.ProjectID, Source: request.AssetRef, Profile: request.Profile, Status: DerivativeQueued, CreatedAt: now, UpdatedAt: now}
	job := ProcessingJob{ID: jobID, OrganizationID: request.OrganizationID, ProjectID: request.ProjectID, DerivativeID: derivativeID, Status: ProcessingQueued, Attempt: 1, CreatedAt: now, UpdatedAt: now}
	value, job, duplicate, err := s.Repository.EnsureDerivative(ctx, value, job)
	if err != nil || duplicate {
		return value, job, duplicate, err
	}
	if err := s.Scheduler.ScheduleAssetDerivative(ctx, job); err != nil {
		value, job, markErr := s.Repository.FailDerivativeScheduling(ctx, value.ID, "DERIVATIVE_QUEUE_FAILED", now)
		if markErr != nil {
			return value, job, false, fmt.Errorf("schedule derivative: %v; mark failed: %w", err, markErr)
		}
		return value, job, false, fmt.Errorf("schedule derivative: %w", err)
	}
	return value, job, false, nil
}

func (s DerivativeService) RetryDerivative(ctx context.Context, org contract.OrganizationID, project contract.ProjectID, id string) (AssetDerivative, ProcessingJob, error) {
	if s.Repository == nil || s.Scheduler == nil || strings.TrimSpace(id) == "" {
		return AssetDerivative{}, ProcessingJob{}, fmt.Errorf("derivative repository, scheduler and id are required")
	}
	newID := s.NewID
	if newID == nil {
		newID = ids.New
	}
	jobID, err := newID("assetjob")
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now()
	}
	value, job, err := s.Repository.RetryDerivative(ctx, org, project, id, jobID, now)
	if err != nil {
		return value, job, err
	}
	if err := s.Scheduler.ScheduleAssetDerivative(ctx, job); err != nil {
		value, job, _ = s.Repository.FailDerivativeScheduling(ctx, value.ID, "DERIVATIVE_QUEUE_FAILED", now)
		return value, job, fmt.Errorf("schedule derivative retry: %w", err)
	}
	return value, job, nil
}

// FindDerivative 查一个素材版本的某种派生物现在什么状态、产物在哪。
//
// 派生物这套东西建好之后一直没有查询口——只能建不能查，所以谁也用不上它。
func (s DerivativeService) FindDerivative(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error) {
	if s.Repository == nil {
		return AssetDerivative{}, fmt.Errorf("derivative repository is required")
	}
	if !validDerivativeProfile(profile) {
		return AssetDerivative{}, fmt.Errorf("unsupported derivative profile")
	}
	if err := source.Validate(); err != nil {
		return AssetDerivative{}, err
	}
	return s.Repository.GetDerivative(ctx, organizationID, projectID, source, profile)
}

// DerivativeJobKind 是派生物任务在共享 runtime 上的类型名。
const DerivativeJobKind = "assets.derivative.run"

// JobRuntimeDerivativeScheduler 把派生物任务投进 jobruntime。
// 形状照着 creative 那边的渲染调度器来，同一套幂等键约定。
type JobRuntimeDerivativeScheduler struct {
	Store jobruntime.Store
	NewID func() (string, error)
	Now   func() time.Time
}

func (s JobRuntimeDerivativeScheduler) ScheduleAssetDerivative(ctx context.Context, job ProcessingJob) error {
	if s.Store == nil || s.NewID == nil {
		return fmt.Errorf("job runtime store and ID generator are required")
	}
	payload, err := json.Marshal(derivativeJobPayload{DerivativeID: job.DerivativeID})
	if err != nil {
		return err
	}
	id, err := s.NewID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	digest := sha256.Sum256([]byte(job.ID))
	_, _, err = s.Store.Enqueue(ctx, jobruntime.CreateRequest{
		Job: contract.Job{
			ID: id, Kind: DerivativeJobKind, OrganizationID: job.OrganizationID, ProjectID: job.ProjectID,
			Status: contract.JobQueued, MaxAttempts: 3, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		// 幂等键用 ProcessingJob 的 ID 而不是派生物 ID：重试会新建一个
		// ProcessingJob，那次是要真的再跑一遍的。
		Payload: payload, IdempotencyKey: contract.IdempotencyKey("assets-derivative-" + job.ID),
		RequestHash: hex.EncodeToString(digest[:]),
	})
	return err
}

type derivativeJobPayload struct {
	DerivativeID string `json:"derivative_id"`
}

// DerivativeRunner 真的把一个派生物做出来。
//
// 目前只做 poster_v1。另外三种规格（video_proxy_v1 / waveform_v1 / font_woff2_v1）
// 的实现不在这一期——遇到它们直接跳过，不标失败，免得在表里留一堆做不出来的红条。
type DerivativeRunner struct {
	Repository DerivativeRepository
	Assets     Repository
	Blobs      BlobStore
	Upload     UploadService
	Poster     PosterExtractor
	// Actor 是系统身份。IngestDerivedImage 要 assets.write，
	// 而这条路上没有人，只有 worker。
	Actor contract.ActorContext
	Now   func() time.Time
}

func (r DerivativeRunner) Run(ctx context.Context, derivativeID string) error {
	if r.Repository == nil || r.Assets == nil || r.Blobs == nil || r.Poster == nil {
		return fmt.Errorf("derivative runner is not configured")
	}
	if strings.TrimSpace(derivativeID) == "" {
		return fmt.Errorf("derivative id is required")
	}
	derivative, err := r.Repository.GetDerivativeByID(ctx, derivativeID)
	if err != nil {
		return err
	}
	if derivative.Profile != DerivativePoster {
		// 别的规格这一期没有实现。跳过，保持 queued，等以后有实现了重试就能跑。
		return nil
	}
	if derivative.Status == DerivativeReady {
		return nil
	}

	actor := r.Actor
	actor.OrganizationID = derivative.OrganizationID
	source, err := r.Assets.GetProjectAsset(ctx, derivative.OrganizationID, derivative.ProjectID, derivative.Source)
	if err != nil {
		return err
	}
	if source.Asset.Kind != contract.AssetVideo {
		// 图片没有「首帧」。排到这儿是上游排错了，不是这条任务失败了。
		return nil
	}
	reader, _, err := r.Blobs.Open(ctx, source.Version.Blob)
	if err != nil {
		return err
	}
	defer reader.Close()
	contents, err := io.ReadAll(io.LimitReader(reader, MaxVideoBytes))
	if err != nil {
		return err
	}
	poster, err := r.Poster.ExtractPoster(ctx, contents)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	// RequestID / TraceID 不能空：IngestDerivedImage 第一件事就是 requestContext.Validate()。
	// 用派生物 ID 当这两个值，日志里一眼能看出这次写入是哪条派生任务干的。
	ref, err := r.Upload.IngestDerivedImage(ctx,
		contract.RequestContext{RequestID: "derivative-" + derivative.ID, TraceID: "derivative-" + derivative.ID, Actor: actor},
		derivative.ProjectID, derivative.ID, derivative.Source,
		bytes.NewReader(poster), int64(len(poster)), PosterMIMEType)
	if err != nil {
		return err
	}
	_, err = r.Repository.CompleteDerivative(ctx, derivative.ID, ref.AssetVersion, now)
	return err
}

func DerivativeRuntimeHandler(runner DerivativeRunner) jobruntime.Handler {
	return func(ctx context.Context, claim jobruntime.Claim) (jobruntime.Result, error) {
		var payload derivativeJobPayload
		if claim.Job.Kind != DerivativeJobKind || json.Unmarshal(claim.Payload, &payload) != nil || strings.TrimSpace(payload.DerivativeID) == "" {
			return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{
				Code: "ASSET_DERIVATIVE_PAYLOAD_INVALID", Message: "Asset derivative payload is invalid", Retryable: false,
			}}
		}
		if err := runner.Run(ctx, payload.DerivativeID); err != nil {
			// 标成可重试：抽帧失败大多是取文件超时或 ffmpeg 一时起不来，
			// 重试三次比第一次就放弃合理。三次之后 jobruntime 自己会收手。
			return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{
				Code: "ASSET_DERIVATIVE_FAILED", Message: "Asset derivative execution failed", Retryable: true,
			}}
		}
		return jobruntime.Result{}, nil
	}
}

func validDerivativeProfile(profile DerivativeProfile) bool {
	switch profile {
	case DerivativeVideoProxy, DerivativePoster, DerivativeWaveform, DerivativeFontWeb:
		return true
	}
	return false
}
