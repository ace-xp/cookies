package insights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func (s Service) miyunReturnRepository() (MiyunReturnRepository, error) {
	r, ok := s.Miyun.(MiyunReturnRepository)
	if !ok || r == nil {
		return nil, fmt.Errorf("Miyun return repository is unavailable")
	}
	return r, nil
}

func (s Service) CreateMiyunHandoffReturn(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, handoffID string, key contract.IdempotencyKey, request CreateMiyunHandoffReturnRequest) (MiyunHandoffReturn, error) {
	if err := s.miyunReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunHandoffReturn{}, err
	}
	if err := key.Validate(); err != nil || request.ExpectedVersion < 1 {
		return MiyunHandoffReturn{}, ErrInvalidRequest
	}
	r, err := s.miyunReturnRepository()
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	handoffID = strings.TrimSpace(handoffID)
	hash, err := contract.CanonicalJSONHash(struct {
		HandoffID       string `json:"handoff_id"`
		ExpectedVersion int64  `json:"expected_version"`
	}{handoffID, request.ExpectedVersion})
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	if existing, lookupErr := r.GetMiyunHandoffReturnByIdempotencyKey(ctx, actor.OrganizationID, projectID, handoffID, string(key)); lookupErr == nil {
		if existing.RequestHash != hash {
			return MiyunHandoffReturn{}, ErrIdempotencyConflict
		}
		return existing, nil
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return MiyunHandoffReturn{}, lookupErr
	}
	handoff, err := s.GetMiyunHandoff(ctx, actor, projectID, strings.TrimSpace(handoffID))
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	if handoff.Version != request.ExpectedVersion {
		return MiyunHandoffReturn{}, ErrVersionConflict
	}
	if handoff.Status != MiyunHandoffExported && handoff.Status != MiyunHandoffDelivered && handoff.Status != MiyunHandoffReturned {
		return MiyunHandoffReturn{}, ErrInvalidState
	}
	id, err := s.idGenerator()("miyunreturn")
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	now := s.now()
	value := MiyunHandoffReturn{ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, HandoffID: handoff.ID, HandoffVersion: handoff.Version, ManifestVersion: handoff.ManifestVersion, InputHash: handoff.InputHash, ParameterVersion: handoff.ParameterVersion, ProductProfileID: handoff.ProductProfileID, CrawlJobID: handoff.CrawlJobID, AssociationSource: MiyunReturnAssociationCrawlJob, Status: MiyunHandoffReturnCreated, IdempotencyKey: string(key), RequestHash: hash, UploadedBy: actor.Principal.ID, Version: 1, CreatedAt: now, UpdatedAt: now}
	stored, _, err := r.CreateMiyunHandoffReturn(ctx, value)
	return stored, err
}

func (s Service) UploadMiyunHandoffReturn(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, handoffID, returnID string, key contract.IdempotencyKey, request UploadMiyunHandoffReturnRequest) (MiyunHandoffReturn, error) {
	if err := s.miyunReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunHandoffReturn{}, err
	}
	if s.MiyunReturns == nil || request.Content == nil || key.Validate() != nil || request.ExpectedVersion < 1 || !strings.EqualFold(filepath.Ext(request.Filename), ".mp4") || request.DeclaredMIMEType != MiyunReturnImportMIMEType || request.DeclaredSizeBytes < 1 {
		return MiyunHandoffReturn{}, ErrInvalidRequest
	}
	r, err := s.miyunReturnRepository()
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	value, err := r.GetMiyunHandoffReturn(ctx, actor.OrganizationID, projectID, strings.TrimSpace(handoffID), strings.TrimSpace(returnID))
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	if value.HandoffVersion != request.ExpectedVersion {
		return MiyunHandoffReturn{}, ErrVersionConflict
	}
	handoff, err := s.GetMiyunHandoff(ctx, actor, projectID, handoffID)
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	if handoff.Version != request.ExpectedVersion {
		return MiyunHandoffReturn{}, ErrVersionConflict
	}
	if handoff.Status != MiyunHandoffExported && handoff.Status != MiyunHandoffDelivered && handoff.Status != MiyunHandoffReturned {
		return MiyunHandoffReturn{}, ErrInvalidState
	}
	association, sourceMaterialID, err := resolveMiyunReturnAssociation(handoff, request.Filename, request.AssociationSource, request.SourceMaterialID)
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	uploadHash, err := contract.CanonicalJSONHash(struct {
		ReturnID          string                       `json:"return_id"`
		ExpectedVersion   int64                        `json:"expected_version"`
		Filename          string                       `json:"filename"`
		MIMEType          string                       `json:"mime_type"`
		SizeBytes         int64                        `json:"size_bytes"`
		DeclaredSHA256    *string                      `json:"declared_sha256,omitempty"`
		SourceMaterialID  string                       `json:"source_material_id,omitempty"`
		AssociationSource MiyunReturnAssociationSource `json:"association_source"`
		ContainerFilename string                       `json:"container_filename,omitempty"`
	}{value.ID, request.ExpectedVersion, request.Filename, request.DeclaredMIMEType, request.DeclaredSizeBytes, request.DeclaredSHA256, sourceMaterialID, association, request.ContainerFilename})
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	if value.Status == MiyunHandoffReturnUploaded || value.Status == MiyunHandoffReturnReturned {
		if value.UploadIdempotencyKey != string(key) || value.UploadRequestHash != uploadHash {
			return MiyunHandoffReturn{}, ErrIdempotencyConflict
		}
		return value, nil
	}
	value.UploadIdempotencyKey, value.UploadRequestHash = string(key), uploadHash
	value.CrawlJobID, value.SourceMaterialID, value.AssociationSource, value.ContainerFilename = handoff.CrawlJobID, sourceMaterialID, association, strings.TrimSpace(request.ContainerFilename)
	sources, err := miyunReturnSources(handoff, sourceMaterialID)
	if err != nil {
		return MiyunHandoffReturn{}, err
	}
	result, err := s.MiyunReturns.ImportMiyunReturnMP4(ctx, contract.RequestContext{RequestID: "miyun-return-" + value.ID, TraceID: "miyun-return-" + value.ID, Actor: actor}, projectID, key, MiyunReturnAssetImportRequest{ReturnID: value.ID, Filename: request.Filename, DeclaredMIMEType: request.DeclaredMIMEType, DeclaredSizeBytes: request.DeclaredSizeBytes, DeclaredSHA256: request.DeclaredSHA256, Content: request.Content, SourceResources: sources})
	if err != nil {
		value.UpdatedAt = s.now()
		_, _ = r.FailMiyunHandoffReturn(context.Background(), value, value.Version, "ASSET_IMPORT_FAILED")
		return MiyunHandoffReturn{}, err
	}
	if err := (MiyunReturnImportInput{HandoffID: value.HandoffID, ManifestInputHash: value.InputHash, Filename: request.Filename, AssetVersion: result.AssetVersion, MIMEType: result.MIMEType, SHA256: result.SHA256, SizeBytes: result.SizeBytes, ScanPassed: true, ProbePassed: true}).Validate(); err != nil {
		value.UpdatedAt = s.now()
		_, _ = r.FailMiyunHandoffReturn(context.Background(), value, value.Version, "ASSET_METADATA_INVALID")
		return MiyunHandoffReturn{}, err
	}
	sourceRef := "miyun_handoff_return:" + handoff.ID
	if sourceMaterialID != "" {
		sourceRef += ":" + sourceMaterialID
	}
	indexed, err := s.IndexAsset(ctx, actor, projectID, IndexAssetRequest{Title: request.Filename, SourceKind: AssetSourceMiyun, SourceRef: sourceRef, SourceJobID: value.ID, PlatformAssetID: string(result.AssetVersion.AssetID), PlatformAssetVersion: result.AssetVersion.Version})
	if err != nil {
		value.UpdatedAt = s.now()
		_, _ = r.FailMiyunHandoffReturn(context.Background(), value, value.Version, "INSIGHT_INDEX_FAILED")
		return MiyunHandoffReturn{}, err
	}
	now := s.now()
	value.Status, value.Filename, value.AssetVersion, value.MIMEType, value.SHA256, value.SizeBytes, value.InsightAssetID, value.UploadedBy, value.UploadedAt, value.UpdatedAt = MiyunHandoffReturnUploaded, request.Filename, result.AssetVersion, result.MIMEType, result.SHA256, result.SizeBytes, indexed.ID, actor.Principal.ID, &now, now
	return r.MarkMiyunHandoffReturnUploaded(ctx, value, value.Version)
}

func (s Service) MarkMiyunHandoffReturned(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, handoffID, returnID string, key contract.IdempotencyKey, expectedVersion int64) (MiyunHandoff, MiyunHandoffReturn, error) {
	if err := s.miyunReady(actor, projectID, ScopeConfirm); err != nil {
		return MiyunHandoff{}, MiyunHandoffReturn{}, err
	}
	if key.Validate() != nil || expectedVersion < 1 {
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrInvalidRequest
	}
	r, err := s.miyunReturnRepository()
	if err != nil {
		return MiyunHandoff{}, MiyunHandoffReturn{}, err
	}
	value, err := r.GetMiyunHandoffReturn(ctx, actor.OrganizationID, projectID, handoffID, returnID)
	if err != nil {
		return MiyunHandoff{}, MiyunHandoffReturn{}, err
	}
	handoff, err := s.GetMiyunHandoff(ctx, actor, projectID, handoffID)
	if err != nil {
		return MiyunHandoff{}, MiyunHandoffReturn{}, err
	}
	if value.HandoffVersion != expectedVersion {
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrVersionConflict
	}
	if value.Status == MiyunHandoffReturnReturned {
		if value.MarkIdempotencyKey == string(key) {
			return handoff, value, nil
		}
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrIdempotencyConflict
	}
	if value.Status != MiyunHandoffReturnUploaded {
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrInvalidState
	}
	if handoff.Version != expectedVersion {
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrVersionConflict
	}
	if handoff.Status != MiyunHandoffExported && handoff.Status != MiyunHandoffDelivered && handoff.Status != MiyunHandoffReturned {
		return MiyunHandoff{}, MiyunHandoffReturn{}, ErrInvalidState
	}
	hash, err := contract.CanonicalJSONHash(struct {
		ReturnID        string `json:"return_id"`
		ExpectedVersion int64  `json:"expected_version"`
	}{value.ID, expectedVersion})
	if err != nil {
		return MiyunHandoff{}, MiyunHandoffReturn{}, err
	}
	now := s.now()
	value.MarkIdempotencyKey, value.MarkRequestHash, value.ReturnedBy, value.ReturnedAt, value.UpdatedAt = string(key), hash, actor.Principal.ID, &now, now
	handoff.UpdatedAt = now
	returned, updated, err := r.CompleteMiyunHandoffReturn(ctx, value, value.Version, handoff, expectedVersion)
	return updated, returned, err
}

func miyunReturnSources(handoff MiyunHandoff, sourceMaterialID string) ([]contract.ResourceRef, error) {
	var snapshots miyunHandoffSourcesSnapshot
	var profile miyunHandoffProfileSnapshot
	if json.Unmarshal(handoff.SourceSnapshot, &snapshots) != nil || json.Unmarshal(handoff.ProfileSnapshot, &profile) != nil {
		return nil, ErrInvalidState
	}
	resources := []contract.ResourceRef{{Type: "miyun_handoff", ID: handoff.ID, Version: &handoff.Version}, {Type: "miyun_product_profile", ID: handoff.ProductProfileID, Version: &profile.ProfileVersion}}
	for _, source := range snapshots.Sources {
		if sourceMaterialID != "" && source.Material.ID != sourceMaterialID {
			continue
		}
		version := source.AssetRef.Version
		resources = append(resources, contract.ResourceRef{Type: "asset_version", ID: string(source.AssetRef.AssetID), Version: &version})
	}
	if sourceMaterialID != "" && len(resources) != 3 {
		return nil, ErrInvalidRequest
	}
	return resources, nil
}

func resolveMiyunReturnAssociation(handoff MiyunHandoff, filename string, explicit MiyunReturnAssociationSource, explicitMaterialID string) (MiyunReturnAssociationSource, string, error) {
	allowed := make(map[string]struct{}, len(handoff.SourceMaterialIDs))
	for _, id := range handoff.SourceMaterialIDs {
		allowed[id] = struct{}{}
	}
	materialID := strings.TrimSpace(explicitMaterialID)
	if explicit != "" {
		if !explicit.valid() {
			return "", "", ErrInvalidRequest
		}
		if explicit == MiyunReturnAssociationCrawlJob {
			return explicit, "", nil
		}
		if _, ok := allowed[materialID]; !ok {
			return "", "", ErrInvalidRequest
		}
		return explicit, materialID, nil
	}
	base := filepath.Base(filename)
	for _, id := range handoff.SourceMaterialIDs {
		if strings.HasPrefix(base, id+"__") {
			return MiyunReturnAssociationFilename, id, nil
		}
	}
	return MiyunReturnAssociationCrawlJob, "", nil
}
