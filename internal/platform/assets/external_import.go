package assets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
)

// ExternalMediaImportRequest contains only caller-authorized source identity
// and transport metadata. Provider generated IDs are never used as Assets
// lineage fields.
type ExternalMediaImportRequest struct {
	SourceProvider string                 `json:"source_provider"`
	SourceObjectID string                 `json:"source_object_id"`
	SourceLocator  string                 `json:"source_locator,omitempty"`
	MIMEType       string                 `json:"mime_type"`
	SizeBytes      int64                  `json:"size_bytes"`
	ReturnSources  []contract.ResourceRef `json:"-"`
}

func (r ExternalMediaImportRequest) Validate() error {
	if strings.TrimSpace(r.SourceProvider) == "" || strings.TrimSpace(r.SourceObjectID) == "" {
		return fmt.Errorf("external import source identity is required")
	}
	if r.MIMEType != "video/mp4" || r.SizeBytes < 1 || r.SizeBytes > MaxVideoBytes {
		return fmt.Errorf("external import must be an MP4 within the video size limit")
	}
	for _, source := range r.ReturnSources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("external import return source: %w", err)
		}
	}
	return nil
}

type ExternalImportOpener func(context.Context) (io.ReadCloser, error)

type ExternalImportService struct {
	Repository       ExternalImportRepository
	Projects         ActiveProjectResolver
	Upload           UploadService
	QuarantineBucket string
	Now              func() time.Time
	NewID            ids.Generator
}

// Import stores an authorized external stream in quarantine, validates it via
// the existing Assets intake pipeline, and records the result in the separate
// external-import ledger.
func (s ExternalImportService) Import(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, key contract.IdempotencyKey, request ExternalMediaImportRequest, open ExternalImportOpener) (contract.ProjectAssetRef, error) {
	if err := s.validate(); err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if err := requestContext.Validate(); err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if !requestContext.Actor.HasScope("assets.write") {
		return contract.ProjectAssetRef{}, fmt.Errorf("assets.write scope is required")
	}
	if err := key.Validate(); err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if err := request.Validate(); err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if open == nil {
		return contract.ProjectAssetRef{}, fmt.Errorf("external import opener is required")
	}
	project, err := s.Projects.RequireActiveContext(ctx, requestContext.Actor, projectID)
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	snapshot, err := json.Marshal(request)
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	hash, err := contract.CanonicalJSONHash(request)
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	newID := s.idGenerator()
	id, err := newID("externalimport")
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	now := s.now()
	ledger := ExternalImport{ID: id, OrganizationID: requestContext.Actor.OrganizationID, ProjectID: projectID, SourceProvider: request.SourceProvider, SourceObjectID: request.SourceObjectID, SourceLocator: request.SourceLocator, IdempotencyKey: string(key), RequestSnapshot: snapshot, RequestHash: hash, Status: ExternalImportQueued, Version: 1, CreatedAt: now, UpdatedAt: now}
	stored, _, err := s.Repository.CreateExternalImport(ctx, ledger)
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if stored.RequestHash != hash || stored.IdempotencyKey != string(key) {
		return contract.ProjectAssetRef{}, ErrIdempotencyConflict
	}
	if stored.Status == ExternalImportSucceeded {
		return storedRef(stored)
	}
	// An ambiguous commit is always reconciled from the ledger before another
	// stream is opened or another asset is created.
	if stored.ResultUnknownAt != nil {
		if refreshed, getErr := s.Repository.GetExternalImportBySource(ctx, stored.OrganizationID, stored.ProjectID, stored.SourceProvider, stored.SourceObjectID); getErr == nil && refreshed.Status == ExternalImportSucceeded {
			return storedRef(refreshed)
		}
		return contract.ProjectAssetRef{}, fmt.Errorf("external import result is being reconciled")
	}
	if err := s.Repository.MarkExternalImportRunning(ctx, stored.OrganizationID, stored.ProjectID, stored.ID, s.now()); err != nil {
		return contract.ProjectAssetRef{}, err
	}

	reader, err := open(ctx)
	if err != nil {
		return s.fail(ctx, stored, "SOURCE_UNAVAILABLE", err)
	}
	if reader == nil {
		return s.fail(ctx, stored, "SOURCE_UNAVAILABLE", fmt.Errorf("external import opener returned nil reader"))
	}
	defer reader.Close()
	staged, err := s.Upload.Blobs.Put(ctx, s.QuarantineBucket, quarantineKey(stored.OrganizationID, stored.ProjectID, stored.ID), io.LimitReader(reader, MaxVideoBytes+1), request.SizeBytes, "video/mp4")
	if err != nil {
		return s.fail(ctx, stored, "INVALID_CONTENT", err)
	}
	preserveQuarantine := false
	defer func() {
		if !preserveQuarantine {
			_ = s.Upload.Blobs.Delete(ctx, staged.ObjectLocation)
		}
	}()
	assetID, err := newID("asset")
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	blobID, err := newID("blob")
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	commit, err := s.Upload.ingestStoredObject(ctx, stored.OrganizationID, projectID, contract.AssetID(assetID), blobID, project.ProjectContextVersion, contract.AssetSourceImported, staged.ObjectLocation, "", "", "", requestContext.TraceID)
	if err != nil {
		return s.fail(ctx, stored, "INVALID_CONTENT", err)
	}
	if commit.MIMEType != "video/mp4" || commit.SizeBytes > MaxVideoBytes {
		return s.fail(ctx, stored, "INVALID_CONTENT", ErrInvalidAssetContent)
	}
	for _, source := range request.ReturnSources {
		commit.Relations = append(commit.Relations, AssetRelation{
			OrganizationID: stored.OrganizationID, ProjectID: projectID,
			OutputAsset:  contract.AssetVersionRef{AssetID: commit.AssetID, Version: commit.Version},
			RelationType: AssetRelationReturnedFrom, Source: source,
		})
	}
	if reused, findErr := s.Repository.FindProjectAssetBySHA256(ctx, stored.OrganizationID, projectID, commit.SHA256); findErr == nil {
		_ = s.Upload.Blobs.Delete(ctx, commit.Location)
		preserveQuarantine = true
		ref, completeErr := s.complete(ctx, stored, reused)
		if completeErr == nil {
			preserveQuarantine = false
			return ref, nil
		}
		return s.reconcileUnknown(ctx, stored, completeErr)
	} else if !errors.Is(findErr, ErrNotFound) {
		_ = s.Upload.Blobs.Delete(ctx, commit.Location)
		return contract.ProjectAssetRef{}, findErr
	}
	preserveQuarantine = true
	ref, err := s.Repository.CompleteExternalImport(ctx, stored.ID, commit, s.now())
	if err == nil {
		preserveQuarantine = false
		// 只有真正新落库的这一条进台账。上面那条按 SHA256 复用已有素材的路不记：
		// 复用的是同一个文件，账本里它早就有一行了。
		s.Upload.recordLedger(ctx, LedgerEntry{
			OrganizationID: stored.OrganizationID, ProjectID: projectID,
			ActorID: requestContext.Actor.Principal.ID,
			AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version,
			Kind: commit.Kind, SourceType: commit.SourceType,
			Title: LedgerTitle("", commit.SourceType, s.now()),
		})
		s.Upload.ensurePoster(ctx, stored.OrganizationID, projectID, ref.AssetVersion, commit.Kind)
		return ref, nil
	}
	return s.reconcileUnknown(ctx, stored, err)
}

func (s ExternalImportService) reconcileUnknown(ctx context.Context, stored ExternalImport, cause error) (contract.ProjectAssetRef, error) {
	// Do not delete durable or quarantine objects after a possibly committed
	// transaction. A later replay may only proceed after ledger readback.
	_ = s.Repository.MarkExternalImportResultUnknown(ctx, stored.OrganizationID, stored.ProjectID, stored.ID, "external import commit outcome is unknown", s.now())
	if reconciled, getErr := s.Repository.GetExternalImportBySource(ctx, stored.OrganizationID, stored.ProjectID, stored.SourceProvider, stored.SourceObjectID); getErr == nil && reconciled.Status == ExternalImportSucceeded {
		return storedRef(reconciled)
	}
	return contract.ProjectAssetRef{}, cause
}

func (s ExternalImportService) complete(ctx context.Context, ledger ExternalImport, ref contract.ProjectAssetRef) (contract.ProjectAssetRef, error) {
	// A hash reuse is persisted through the same completion operation; a zero
	// commit tells implementations to attach the existing project asset only.
	return s.Repository.CompleteExternalImport(ctx, ledger.ID, AssetCommit{OrganizationID: ledger.OrganizationID, ProjectID: ledger.ProjectID, AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version}, s.now())
}
func (s ExternalImportService) fail(ctx context.Context, ledger ExternalImport, code string, cause error) (contract.ProjectAssetRef, error) {
	_ = s.Repository.FailExternalImport(ctx, ledger.OrganizationID, ledger.ProjectID, ledger.ID, code, cause.Error(), s.now())
	return contract.ProjectAssetRef{}, cause
}
func (s ExternalImportService) validate() error {
	if s.Repository == nil || s.Projects == nil || s.Upload.Blobs == nil || s.Upload.Scanner == nil || s.QuarantineBucket == "" || s.Upload.AssetsBucket == "" {
		return fmt.Errorf("external import service dependencies are incomplete")
	}
	if s.Upload.VideoProbe == nil && s.Upload.MediaProbe == nil {
		return fmt.Errorf("external import video probe is required")
	}
	return nil
}
func (s ExternalImportService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s ExternalImportService) idGenerator() ids.Generator {
	if s.NewID != nil {
		return s.NewID
	}
	return s.Upload.idGenerator()
}
func storedRef(value ExternalImport) (contract.ProjectAssetRef, error) {
	if value.CommittedAssetID == "" || value.CommittedAssetVersion < 1 {
		return contract.ProjectAssetRef{}, ErrInvalidState
	}
	return contract.ProjectAssetRef{ProjectID: value.ProjectID, AssetVersion: contract.AssetVersionRef{AssetID: value.CommittedAssetID, Version: value.CommittedAssetVersion}}, nil
}
