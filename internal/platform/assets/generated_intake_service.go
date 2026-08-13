package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
)

type GeneratedIntakeService struct {
	Repository  Repository
	Projects    ActiveProjectResolver
	Now         func() time.Time
	NewID       ids.Generator
	MaxAttempts int
}

func (s GeneratedIntakeService) Create(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, key contract.IdempotencyKey, request GeneratedAssetIntakeRequest) (GeneratedIntake, error) {
	if s.Repository == nil || s.Projects == nil {
		return GeneratedIntake{}, fmt.Errorf("generated intake dependencies are incomplete")
	}
	if err := requestContext.Validate(); err != nil {
		return GeneratedIntake{}, err
	}
	if err := key.Validate(); err != nil {
		return GeneratedIntake{}, err
	}
	if err := request.Validate(); err != nil {
		return GeneratedIntake{}, err
	}
	_, maxBytes, supported := generatedAssetPolicy(request.Output.DeclaredMIMEType)
	if !supported || request.Output.DeclaredSizeBytes > maxBytes {
		return GeneratedIntake{}, ErrUnsupportedAsset
	}
	now := s.now()
	if !request.Output.RetrievalExpiresAt.After(now) {
		return GeneratedIntake{}, ErrProviderOutputExpired
	}
	projectContext, err := s.Projects.RequireActiveContext(ctx, requestContext.Actor, projectID)
	if err != nil {
		return GeneratedIntake{}, err
	}
	if request.Provenance.ProjectContextVersion != projectContext.ProjectContextVersion {
		return GeneratedIntake{}, ErrProjectContextStale
	}
	hash, err := contract.CanonicalJSONHash(request)
	if err != nil {
		return GeneratedIntake{}, err
	}
	newID := s.idGenerator()
	intakeID, err := newID("intake")
	if err != nil {
		return GeneratedIntake{}, err
	}
	assetID, err := newID("asset")
	if err != nil {
		return GeneratedIntake{}, err
	}
	blobID, err := newID("blob")
	if err != nil {
		return GeneratedIntake{}, err
	}
	value := GeneratedIntake{
		ID: intakeID, OrganizationID: requestContext.Actor.OrganizationID, ProjectID: projectID,
		ProviderJobID: request.ProviderJobID, OutputID: request.Output.OutputID, ProviderCode: request.Output.ProviderCode,
		Status: GeneratedIntakeQueued, Request: request, IdempotencyKey: key, RequestHash: hash,
		TargetAssetID: contract.AssetID(assetID), TargetBlobID: blobID, AttemptCount: 0, MaxAttempts: s.maxAttempts(),
		AvailableAt: now, RequestID: requestContext.RequestID, TraceID: requestContext.TraceID, CreatedAt: now, UpdatedAt: now,
	}
	stored, _, err := s.Repository.CreateIntake(ctx, value)
	return stored, err
}

func (s GeneratedIntakeService) Get(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (GeneratedIntake, error) {
	if s.Repository == nil || s.Projects == nil {
		return GeneratedIntake{}, fmt.Errorf("generated intake dependencies are incomplete")
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return GeneratedIntake{}, err
	}
	return s.Repository.GetIntake(ctx, actor.OrganizationID, projectID, id)
}

func (s GeneratedIntakeService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s GeneratedIntakeService) idGenerator() ids.Generator {
	if s.NewID != nil {
		return s.NewID
	}
	return ids.New
}
func (s GeneratedIntakeService) maxAttempts() int {
	if s.MaxAttempts > 0 {
		return s.MaxAttempts
	}
	return 3
}

type GeneratedIntakeWorker struct {
	Repository Repository
	Projects   ActiveProjectResolver
	Fetcher    GeneratedOutputFetcher
	Upload     UploadService
	Actor      contract.ActorContext
	Now        func() time.Time
}

func (w GeneratedIntakeWorker) ProcessOnce(ctx context.Context, workerID string) (bool, error) {
	if w.Repository == nil || w.Projects == nil || w.Fetcher == nil {
		return false, fmt.Errorf("generated intake worker dependencies are incomplete")
	}
	if err := w.Upload.validateDependencies(); err != nil {
		return false, err
	}
	if err := w.Actor.Validate(); err != nil {
		return false, fmt.Errorf("invalid worker actor: %w", err)
	}
	now := w.now()
	intake, found, err := w.Repository.ClaimIntake(ctx, w.Actor, workerID, now)
	if err != nil || !found {
		return found, err
	}
	if w.Actor.OrganizationID != intake.OrganizationID {
		failure := contract.JobError{Code: "TENANT_SCOPE_MISMATCH", Message: "worker is not authorized for intake organization", Retryable: false}
		return true, w.Repository.FailIntake(ctx, intake, failure, now)
	}
	projectContext, err := w.Projects.RequireActiveContext(ctx, w.Actor, intake.ProjectID)
	if err != nil {
		failure := contract.JobError{Code: "PROJECT_NOT_ACTIVE", Message: "project is not active or authorized", Retryable: false}
		return true, w.Repository.FailIntake(ctx, intake, failure, now)
	}
	if projectContext.ProjectContextVersion != intake.Request.Provenance.ProjectContextVersion {
		failure := contract.JobError{Code: "PROJECT_CONTEXT_STALE", Message: "project context changed after generated intake was accepted", Retryable: false}
		return true, w.Repository.FailIntake(ctx, intake, failure, now)
	}
	if !intake.Request.Output.RetrievalExpiresAt.After(now) {
		failure := contract.JobError{Code: "PROVIDER_OUTPUT_EXPIRED", Message: "provider output retrieval handle has expired", Retryable: false}
		return true, w.Repository.FailIntake(ctx, intake, failure, now)
	}
	commit, failure := w.fetchAndIngest(ctx, intake, projectContext)
	if failure != nil {
		if failure.Retryable && intake.AttemptCount < intake.MaxAttempts {
			return true, w.Repository.RetryIntake(ctx, intake, *failure, now.Add(retryDelay(intake.AttemptCount)))
		}
		return true, w.Repository.FailIntake(ctx, intake, *failure, now)
	}
	_, err = w.Repository.CompleteIntake(ctx, intake, commit, w.now())
	if err != nil {
		if !errors.Is(err, ErrInvalidState) {
			_ = w.Upload.Blobs.Delete(ctx, commit.Location)
		}
		return true, err
	}
	// ActorID 留空：这条入库是后台 worker 干的，GeneratedIntake 上没有记发起人，
	// 台账会把它落成 system。编一个人名比留空更坏。
	w.Upload.recordLedger(ctx, LedgerEntry{
		OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID,
		AssetID: commit.AssetID, Version: commit.Version,
		Kind: commit.Kind, SourceType: commit.SourceType,
		Title: LedgerTitle("", commit.SourceType, w.now()),
	})
	return true, nil
}

func (w GeneratedIntakeWorker) fetchAndIngest(ctx context.Context, intake GeneratedIntake, projectContext contract.ProjectContext) (AssetCommit, *contract.JobError) {
	fetchContext, cancelFetch := context.WithDeadline(ctx, intake.Request.Output.RetrievalExpiresAt)
	reader, metadata, err := w.Fetcher.Open(fetchContext, contract.ProjectRef{OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID, ProjectContextVersion: projectContext.ProjectContextVersion}, intake.Request.Output)
	if err != nil {
		cancelFetch()
		failure := classifyFetchError(err)
		return AssetCommit{}, &failure
	}
	defer reader.Close()
	defer cancelFetch()
	if err := metadata.Validate(); err != nil {
		return AssetCommit{}, &contract.JobError{Code: "OUTPUT_METADATA_INVALID", Message: "provider returned invalid output metadata", Retryable: false}
	}
	_, maxBytes, supported := generatedAssetPolicy(intake.Request.Output.DeclaredMIMEType)
	if !supported {
		return AssetCommit{}, &contract.JobError{Code: "ASSET_TYPE_UNSUPPORTED", Message: "provider output type is unsupported", Retryable: false}
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	cancelFetch()
	if err != nil {
		return AssetCommit{}, &contract.JobError{Code: "PROVIDER_OUTPUT_READ_FAILED", Message: "failed to read provider output", Retryable: true}
	}
	digest := sha256.Sum256(data)
	actualSHA := hex.EncodeToString(digest[:])
	if int64(len(data)) > maxBytes || metadata.SizeBytes != int64(len(data)) || metadata.SHA256 != actualSHA {
		return AssetCommit{}, &contract.JobError{Code: "OUTPUT_METADATA_MISMATCH", Message: "provider output bytes do not match provider metadata", Retryable: false}
	}
	declared := intake.Request.Output
	if metadata.MIMEType != declared.DeclaredMIMEType || metadata.SizeBytes != declared.DeclaredSizeBytes || (declared.DeclaredSHA256 != nil && metadata.SHA256 != *declared.DeclaredSHA256) {
		return AssetCommit{}, &contract.JobError{Code: "OUTPUT_METADATA_MISMATCH", Message: "provider output does not match declared metadata", Retryable: false}
	}
	location, err := w.Upload.Blobs.Put(ctx, w.Upload.QuarantineBucket, quarantineKey(intake.OrganizationID, intake.ProjectID, intake.ID), bytes.NewReader(data), int64(len(data)), metadata.MIMEType)
	if err != nil {
		return AssetCommit{}, &contract.JobError{Code: "ASSET_STORAGE_UNAVAILABLE", Message: "failed to stage provider output", Retryable: true}
	}
	commit, err := w.Upload.ingestStoredObject(ctx, intake.OrganizationID, intake.ProjectID, intake.TargetAssetID, intake.TargetBlobID,
		intake.Request.Provenance.ProjectContextVersion, contract.AssetSourceProviderGenerated, location.ObjectLocation,
		intake.ProviderJobID, intake.OutputID, "", intake.TraceID)
	if err != nil {
		// Keep malware detections isolated in the quarantine bucket for its
		// short lifecycle/forensic policy; discard ordinary failed staging.
		if !errors.Is(err, ErrMalwareDetected) {
			_ = w.Upload.Blobs.Delete(ctx, location.ObjectLocation)
		}
		code := "ASSET_INTAKE_FAILED"
		if errors.Is(err, ErrMalwareDetected) {
			code = contract.ErrorAssetQuarantined
		}
		if errors.Is(err, ErrMalwareDetected) || errors.Is(err, ErrInvalidAssetContent) {
			return AssetCommit{}, &contract.JobError{Code: code, Message: "provider output failed asset validation", Retryable: false}
		}
		return AssetCommit{}, &contract.JobError{Code: "ASSET_INTAKE_UNAVAILABLE", Message: "asset intake dependency is temporarily unavailable", Retryable: true}
	}
	_ = w.Upload.Blobs.Delete(ctx, location.ObjectLocation)
	if commit.MIMEType != metadata.MIMEType || commit.SizeBytes != metadata.SizeBytes || commit.SHA256 != metadata.SHA256 {
		_ = w.Upload.Blobs.Delete(ctx, commit.Location)
		return AssetCommit{}, &contract.JobError{Code: "OUTPUT_METADATA_MISMATCH", Message: "validated asset does not match output metadata", Retryable: false}
	}
	commit.Relations = relationsForGeneratedOutput(intake, commit)
	return commit, nil
}

func relationsForGeneratedOutput(intake GeneratedIntake, commit AssetCommit) []AssetRelation {
	output := contract.AssetVersionRef{AssetID: commit.AssetID, Version: commit.Version}
	relations := make([]AssetRelation, 0, len(intake.Request.Provenance.SourceAssetRefs)+len(intake.Request.Provenance.SourceResourceRefs))
	add := func(source contract.ResourceRef) {
		relations = append(relations, AssetRelation{
			OrganizationID: commit.OrganizationID,
			ProjectID:      commit.ProjectID,
			OutputAsset:    output,
			RelationType:   AssetRelationGeneratedFrom,
			Source:         source,
		})
	}
	for _, ref := range intake.Request.Provenance.SourceAssetRefs {
		version := ref.Version
		add(contract.ResourceRef{Type: "asset_version", ID: string(ref.AssetID), Version: &version})
	}
	for _, ref := range intake.Request.Provenance.SourceResourceRefs {
		add(ref)
	}
	return relations
}

type retryableProviderError interface {
	error
	Retryable() bool
}

func classifyFetchError(err error) contract.JobError {
	retryable := true
	var classified retryableProviderError
	if errors.As(err, &classified) {
		retryable = classified.Retryable()
	}
	return contract.JobError{Code: "PROVIDER_OUTPUT_UNAVAILABLE", Message: "provider output could not be retrieved", Retryable: retryable}
}
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second * time.Duration(1<<(min(attempt, 6)-1))
	return delay
}
func (w GeneratedIntakeWorker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
