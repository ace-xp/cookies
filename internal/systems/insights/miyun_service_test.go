package insights

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestMiyunAnalyzeIsDeterministicExplainableAndSupersedesDraft(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	request := AnalyzeMiyunProductProfileRequest{
		ConnectionID: "miyun_connection_1", ProductID: "product_1",
		ProductAssetRefs:     []contract.AssetVersionRef{{AssetID: "asset_1", Version: 1}},
		KnowledgeDocumentIDs: []string{"document_1"},
	}
	first, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.InputHash != second.InputHash || first.ProductName != second.ProductName ||
		!reflect.DeepEqual(first.Keywords, second.Keywords) || !reflect.DeepEqual(first.MaterialTypes, second.MaterialTypes) || !reflect.DeepEqual(first.MaterialContentTypes, second.MaterialContentTypes) {
		t.Fatalf("same frozen input produced different drafts:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if repository.profiles[first.ID].Status != MiyunProfileSuperseded || repository.profiles[first.ID].Version != 2 || second.Status != MiyunProfileDraft {
		t.Fatalf("old draft=%s new=%s", repository.profiles[first.ID].Status, second.Status)
	}
	if second.AnalysisMethod != "rules" || second.ModelVersion != "" || !containsString(second.AnalysisWarnings, "model_not_used:deterministic_rules") {
		t.Fatalf("rule lineage is not explicit: %#v", second)
	}
	if len(second.FieldSources) != 6 || second.FieldSources[0].ReviewState != "suggested" || len(second.FieldSources[0].SourceRefs) == 0 {
		t.Fatalf("field sources=%#v", second.FieldSources)
	}
	projectReader := service.MiyunProjects.(fakeMiyunProjectReader)
	projectReader.source.Context.ProjectContextVersion++
	service.MiyunProjects = projectReader
	changedContext, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", request)
	if err != nil {
		t.Fatal(err)
	}
	if changedContext.InputHash == second.InputHash {
		t.Fatal("context version changed without changing the frozen input hash")
	}
}

func TestUpdateMiyunConnectionExplainsConfigurationAndCookieBounds(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	service := newMiyunTestService(newMiyunServiceRepository(now), now)
	request := UpdateMiyunConnectionRequest{Session: "cookie=value", ExpectedVersion: 0}
	if _, err := service.UpdateMiyunConnection(context.Background(), miyunTestActor(), "project_1", request); !errors.Is(err, ErrInvalidState) || !strings.Contains(err.Error(), "会话加密") {
		t.Fatalf("missing server encryption error=%v", err)
	}

	service.MiyunSecrets = miyunCipherTestDouble{}
	request.Session = "short"
	if _, err := service.UpdateMiyunConnection(context.Background(), miyunTestActor(), "project_1", request); !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "Cookie 值不完整") {
		t.Fatalf("short cookie error=%v", err)
	}
	request.Session = strings.Repeat("x", maxMiyunSessionBytes+1)
	if _, err := service.UpdateMiyunConnection(context.Background(), miyunTestActor(), "project_1", request); !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "超过 16 KiB") {
		t.Fatalf("oversized cookie error=%v", err)
	}
}

func TestMiyunAnalyzeCreatesPendingIdentityWhenProjectHasNoRegisteredProduct(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	projectReader := service.MiyunProjects.(fakeMiyunProjectReader)
	projectReader.source.Context.ProductIDs = []contract.ProductID{}
	projectReader.source.Products = []MiyunProjectProduct{}
	service.MiyunProjects = projectReader

	_, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", AnalyzeMiyunProductProfileRequest{
		ConnectionID: "miyun_connection_1", ProductAssetRefs: []contract.AssetVersionRef{}, KnowledgeDocumentIDs: []string{},
	})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "product_name is required") {
		t.Fatalf("missing manual product name error=%v", err)
	}

	draft, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", AnalyzeMiyunProductProfileRequest{
		ConnectionID: "miyun_connection_1", ProductName: "手冲咖啡套装", CategoryName: "咖啡器具",
		ProductAssetRefs: []contract.AssetVersionRef{}, KnowledgeDocumentIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(draft.ProductID), "project_input:") || draft.ProductName != "手冲咖啡套装" || draft.CategoryName != "咖啡器具" || !containsString(draft.AnalysisWarnings, "product_identity_pending_confirmation") {
		t.Fatalf("pending product identity was not explicit: %#v", draft)
	}
}

func TestMiyunConfirmUsesExpectedVersionAndFreezesConfirmedProfile(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	draft, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", AnalyzeMiyunProductProfileRequest{
		ConnectionID: "miyun_connection_1", ProductID: "product_1", ProductAssetRefs: []contract.AssetVersionRef{}, KnowledgeDocumentIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	query := MiyunProfileQuery{
		ProductName: "Operator product", CategoryID: "cid_1", CategoryName: "Drinkware",
		Keywords: []string{"operator keyword"}, MaterialContentTypes: []string{"商品展示"},
		WindowStart: now.AddDate(0, 0, -6), WindowEnd: now,
	}
	if _, err := service.ConfirmMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", draft.ID, ConfirmMiyunProductProfileRequest{ExpectedVersion: draft.Version + 1, Query: query}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale confirmation error=%v", err)
	}
	confirmed, err := service.ConfirmMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", draft.ID, ConfirmMiyunProductProfileRequest{ExpectedVersion: draft.Version, Query: query})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != MiyunProfileConfirmed || confirmed.Version != 2 || confirmed.ProductName != "Operator product" || confirmed.FieldSources[0].ReviewState != "human_confirmed" {
		t.Fatalf("confirmed=%#v", confirmed)
	}
	if _, err := service.ConfirmMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", draft.ID, ConfirmMiyunProductProfileRequest{ExpectedVersion: confirmed.Version, Query: query}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("confirmed profile changed in place: %v", err)
	}
}

func TestMiyunManualImportReferencesExistingMP4AndReplaysIdempotently(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	request := ManualMiyunMaterialRequest{
		AssetRef:        contract.AssetVersionRef{AssetID: "asset_1", Version: 1},
		MiyunMaterialID: "remote_1", SourceRef: "https://example.test/miyun/material/remote_1", Title: "Manual material",
		DataCard: ManualMiyunDataCard{
			SchemaVersion: MiyunDataCardSchemaV1, CapturedAt: now,
			CumulativeImpressionsRaw: "1.2万", CumulativeImpressions: 12000, RelatedAds: 5,
			SourceFields: []byte(`{"fixture_version":"1"}`),
		},
	}
	first, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "manual-key-1", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "manual-key-1", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Material.ID != second.Material.ID || !second.Replayed || first.Material.ImportMethod != MiyunImportManual ||
		first.Material.FirstSeenCrawlJobID != "" || first.Snapshot.CrawlJobID != "" ||
		first.Snapshot.CumulativeImpressionsRaw != "1.2万" || first.InsightAsset.SourceKind != AssetSourceMiyun {
		t.Fatalf("manual replay/result mismatch: first=%#v second=%#v", first, second)
	}
	request.Title = "different request"
	if _, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "manual-key-1", request); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict=%v", err)
	}

	assetReader := service.MiyunAssets.(*fakeMiyunAssetReader)
	assetReader.source.MIMEType = "image/png"
	request.Title = "new request"
	if _, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "manual-key-2", request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("non-MP4 manual import=%v", err)
	}
}

func TestMiyunAnalysisBoundsAndManualSourceRefRejectsSecrets(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	documentIDs := make([]string, maxMiyunAnalysisSources+1)
	for index := range documentIDs {
		documentIDs[index] = fmt.Sprintf("document_%d", index)
	}
	_, err := service.AnalyzeMiyunProductProfile(context.Background(), miyunTestActor(), "project_1", AnalyzeMiyunProductProfileRequest{
		ConnectionID: "miyun_connection_1", ProductID: "product_1",
		ProductAssetRefs: []contract.AssetVersionRef{}, KnowledgeDocumentIDs: documentIDs,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unbounded analysis sources error=%v", err)
	}

	request := ManualMiyunMaterialRequest{
		AssetRef: contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, MiyunMaterialID: "remote_secret",
		SourceRef: "https://example.test/material?id=1&session_token=secret",
		DataCard:  ManualMiyunDataCard{SchemaVersion: MiyunDataCardSchemaV1, CapturedAt: now, CumulativeImpressionsRaw: "0"},
	}
	if _, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "manual-secret", request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("secret-bearing source URL error=%v", err)
	}
}

func TestMiyunManualImportRequiresRemoteIdentityAndSource(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	base := ManualMiyunMaterialRequest{
		AssetRef: contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, MiyunMaterialID: "remote_1",
		SourceRef: "https://example.test/material/1",
		DataCard:  ManualMiyunDataCard{SchemaVersion: MiyunDataCardSchemaV1, CapturedAt: now, CumulativeImpressionsRaw: "0"},
	}
	for _, mutate := range []func(*ManualMiyunMaterialRequest){
		func(request *ManualMiyunMaterialRequest) { request.MiyunMaterialID = "" },
		func(request *ManualMiyunMaterialRequest) { request.SourceRef = "" },
	} {
		request := base
		mutate(&request)
		service := newMiyunTestService(newMiyunServiceRepository(now), now)
		if _, err := service.ManualImportMiyunMaterial(context.Background(), miyunTestActor(), "project_1", "required-fields", request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("missing manual identity/source error=%v", err)
		}
	}
}

type memoryMiyunServiceRepository struct {
	connection MiyunConnection
	profiles   map[string]MiyunProductProfile
	jobs       map[string]MiyunCrawlJob
	manual     map[string]MiyunManualImportResult
	materials  map[string]MiyunMaterial
	snapshots  map[string][]MiyunMaterialSnapshot
	handoffs   map[string]MiyunHandoff
	returns    map[string]MiyunHandoffReturn
}

func newMiyunServiceRepository(now time.Time) *memoryMiyunServiceRepository {
	connection := validMiyunConnection(now)
	connection.Status = MiyunConnectionReady
	return &memoryMiyunServiceRepository{
		connection: connection, profiles: map[string]MiyunProductProfile{}, jobs: map[string]MiyunCrawlJob{"miyun_job_1": validMiyunCrawlJob(now)}, manual: map[string]MiyunManualImportResult{}, materials: map[string]MiyunMaterial{}, snapshots: map[string][]MiyunMaterialSnapshot{}, handoffs: map[string]MiyunHandoff{}, returns: map[string]MiyunHandoffReturn{},
	}
}

func (r *memoryMiyunServiceRepository) CreateMiyunProductProfileDraft(_ context.Context, value MiyunProductProfile) (MiyunProductProfile, error) {
	for id, existing := range r.profiles {
		if existing.ProjectID == value.ProjectID && existing.ProductID == value.ProductID && existing.Status == MiyunProfileDraft {
			existing.Status, existing.Version = MiyunProfileSuperseded, existing.Version+1
			r.profiles[id] = existing
		}
	}
	r.profiles[value.ID] = value
	return value, nil
}

func (r *memoryMiyunServiceRepository) GetMiyunProductProfile(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (MiyunProductProfile, error) {
	value, ok := r.profiles[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return MiyunProductProfile{}, ErrNotFound
	}
	return value, nil
}

func (r *memoryMiyunServiceRepository) ListMiyunProductProfiles(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, _ int) ([]MiyunProductProfile, error) {
	result := []MiyunProductProfile{}
	for _, value := range r.profiles {
		if value.OrganizationID == organizationID && value.ProjectID == projectID {
			result = append(result, value)
		}
	}
	return result, nil
}

func (r *memoryMiyunServiceRepository) ConfirmMiyunProductProfile(_ context.Context, value MiyunProductProfile, expectedVersion int64) (MiyunProductProfile, error) {
	current, ok := r.profiles[value.ID]
	if !ok {
		return MiyunProductProfile{}, ErrNotFound
	}
	if current.Status != MiyunProfileDraft {
		return MiyunProductProfile{}, ErrInvalidState
	}
	if current.Version != expectedVersion {
		return MiyunProductProfile{}, ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	r.profiles[value.ID] = value
	return value, nil
}

func (r *memoryMiyunServiceRepository) CreateManualMiyunMaterial(_ context.Context, record MiyunManualImportRecord) (MiyunManualImportResult, error) {
	key := record.Material.ManualIdempotencyKey
	if existing, ok := r.manual[key]; ok {
		if existing.Material.ManualRequestHash != record.Material.ManualRequestHash {
			return MiyunManualImportResult{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		return existing, nil
	}
	result := MiyunManualImportResult{Material: record.Material, Snapshot: record.Snapshot, InsightAsset: record.InsightAsset}
	r.manual[key] = result
	return result, nil
}

func (r *memoryMiyunServiceRepository) GetMiyunConnection(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (MiyunConnection, error) {
	if r.connection.OrganizationID != organizationID || r.connection.ProjectID != projectID || r.connection.ID != id {
		return MiyunConnection{}, ErrNotFound
	}
	return r.connection, nil
}

func (r *memoryMiyunServiceRepository) GetProjectMiyunConnection(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID) (MiyunConnection, error) {
	if r.connection.OrganizationID != organizationID || r.connection.ProjectID != projectID {
		return MiyunConnection{}, ErrNotFound
	}
	return r.connection, nil
}

func (r *memoryMiyunServiceRepository) CreateMiyunConnection(context.Context, MiyunConnection) (MiyunConnection, error) {
	return MiyunConnection{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) UpdateMiyunConnection(context.Context, MiyunConnection, int64) (MiyunConnection, error) {
	return MiyunConnection{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) CreateMiyunProductProfile(context.Context, MiyunProductProfile) (MiyunProductProfile, error) {
	return MiyunProductProfile{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) CreateMiyunCrawlJob(context.Context, MiyunCrawlJob) (MiyunCrawlJob, error) {
	return MiyunCrawlJob{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) GetMiyunCrawlJob(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (MiyunCrawlJob, error) {
	value, ok := r.jobs[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return MiyunCrawlJob{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryMiyunServiceRepository) CreateMiyunMaterial(context.Context, MiyunMaterial) (MiyunMaterial, error) {
	return MiyunMaterial{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) GetMiyunMaterial(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (MiyunMaterial, error) {
	value, ok := r.materials[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return MiyunMaterial{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryMiyunServiceRepository) AppendMiyunMaterialSnapshot(context.Context, MiyunMaterialSnapshot) (MiyunMaterialSnapshot, error) {
	return MiyunMaterialSnapshot{}, errors.New("unused")
}
func (r *memoryMiyunServiceRepository) ListMiyunMaterialSnapshots(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, materialID string) ([]MiyunMaterialSnapshot, error) {
	material, err := r.GetMiyunMaterial(context.Background(), organizationID, projectID, materialID)
	if err != nil {
		return nil, err
	}
	return append([]MiyunMaterialSnapshot(nil), r.snapshots[material.ID]...), nil
}
func (r *memoryMiyunServiceRepository) CreateMiyunHandoff(_ context.Context, value MiyunHandoff) (MiyunHandoff, error) {
	for _, existing := range r.handoffs {
		if existing.OrganizationID == value.OrganizationID && existing.ProjectID == value.ProjectID && existing.InputHash == value.InputHash {
			return MiyunHandoff{}, ErrIdempotencyConflict
		}
	}
	r.handoffs[value.ID] = value
	return value, nil
}
func (r *memoryMiyunServiceRepository) GetMiyunHandoff(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (MiyunHandoff, error) {
	value, ok := r.handoffs[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return MiyunHandoff{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryMiyunServiceRepository) ListMiyunHandoffs(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, _ int) ([]MiyunHandoff, error) {
	result := make([]MiyunHandoff, 0, len(r.handoffs))
	for _, value := range r.handoffs {
		if value.OrganizationID == organizationID && value.ProjectID == projectID {
			result = append(result, value)
		}
	}
	return result, nil
}
func (r *memoryMiyunServiceRepository) FindMiyunHandoffByInputHash(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, inputHash string) (MiyunHandoff, error) {
	for _, value := range r.handoffs {
		if value.OrganizationID == organizationID && value.ProjectID == projectID && value.InputHash == inputHash {
			return value, nil
		}
	}
	return MiyunHandoff{}, ErrNotFound
}
func (r *memoryMiyunServiceRepository) UpdateMiyunHandoffStatus(_ context.Context, value MiyunHandoff, expectedVersion int64) (MiyunHandoff, error) {
	current, ok := r.handoffs[value.ID]
	if !ok {
		return MiyunHandoff{}, ErrNotFound
	}
	if current.Version != expectedVersion {
		return MiyunHandoff{}, ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	r.handoffs[value.ID] = value
	return value, nil
}

func (r *memoryMiyunServiceRepository) CreateMiyunHandoffReturn(_ context.Context, value MiyunHandoffReturn) (MiyunHandoffReturn, bool, error) {
	for _, existing := range r.returns {
		if existing.OrganizationID == value.OrganizationID && existing.ProjectID == value.ProjectID && existing.HandoffID == value.HandoffID && existing.IdempotencyKey == value.IdempotencyKey {
			if existing.RequestHash != value.RequestHash {
				return MiyunHandoffReturn{}, false, ErrIdempotencyConflict
			}
			return existing, false, nil
		}
	}
	r.returns[value.ID] = value
	return value, true, nil
}
func (r *memoryMiyunServiceRepository) GetMiyunHandoffReturnByIdempotencyKey(_ context.Context, org contract.OrganizationID, project contract.ProjectID, handoffID, key string) (MiyunHandoffReturn, error) {
	for _, value := range r.returns {
		if value.OrganizationID == org && value.ProjectID == project && value.HandoffID == handoffID && value.IdempotencyKey == key {
			return value, nil
		}
	}
	return MiyunHandoffReturn{}, ErrNotFound
}
func (r *memoryMiyunServiceRepository) GetMiyunHandoffReturn(_ context.Context, org contract.OrganizationID, project contract.ProjectID, handoffID, id string) (MiyunHandoffReturn, error) {
	value, ok := r.returns[id]
	if !ok || value.OrganizationID != org || value.ProjectID != project || value.HandoffID != handoffID {
		return MiyunHandoffReturn{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryMiyunServiceRepository) ListMiyunHandoffReturns(_ context.Context, org contract.OrganizationID, project contract.ProjectID, handoffID string) ([]MiyunHandoffReturn, error) {
	values := []MiyunHandoffReturn{}
	for _, value := range r.returns {
		if value.OrganizationID == org && value.ProjectID == project && value.HandoffID == handoffID {
			values = append(values, value)
		}
	}
	return values, nil
}
func (r *memoryMiyunServiceRepository) MarkMiyunHandoffReturnUploaded(_ context.Context, value MiyunHandoffReturn, expected int64) (MiyunHandoffReturn, error) {
	current, ok := r.returns[value.ID]
	if !ok || current.Version != expected {
		return MiyunHandoffReturn{}, ErrVersionConflict
	}
	value.Version = expected + 1
	r.returns[value.ID] = value
	return value, nil
}
func (r *memoryMiyunServiceRepository) FailMiyunHandoffReturn(_ context.Context, value MiyunHandoffReturn, expected int64, code string) (MiyunHandoffReturn, error) {
	current, ok := r.returns[value.ID]
	if !ok || current.Version != expected {
		return MiyunHandoffReturn{}, ErrVersionConflict
	}
	value.Status, value.FailureCode, value.Version = MiyunHandoffReturnFailed, code, expected+1
	r.returns[value.ID] = value
	return value, nil
}
func (r *memoryMiyunServiceRepository) CompleteMiyunHandoffReturn(_ context.Context, value MiyunHandoffReturn, returnVersion int64, handoff MiyunHandoff, handoffVersion int64) (MiyunHandoffReturn, MiyunHandoff, error) {
	current, ok := r.returns[value.ID]
	if !ok || current.Version != returnVersion {
		return MiyunHandoffReturn{}, MiyunHandoff{}, ErrVersionConflict
	}
	currentHandoff, ok := r.handoffs[handoff.ID]
	if !ok || currentHandoff.Version != handoffVersion {
		return MiyunHandoffReturn{}, MiyunHandoff{}, ErrVersionConflict
	}
	value.Status, value.Version = MiyunHandoffReturnReturned, returnVersion+1
	if currentHandoff.Status != MiyunHandoffReturned {
		handoff.Status, handoff.Version = MiyunHandoffReturned, handoffVersion+1
	} else {
		handoff = currentHandoff
	}
	r.returns[value.ID], r.handoffs[handoff.ID] = value, handoff
	return value, handoff, nil
}

type fakeMiyunProjectReader struct{ source MiyunProjectSource }

func (r fakeMiyunProjectReader) ReadMiyunProjectSource(_ context.Context, actor contract.ActorContext, projectID contract.ProjectID) (MiyunProjectSource, error) {
	if actor.OrganizationID != r.source.Context.OrganizationID || projectID != r.source.Context.ProjectID {
		return MiyunProjectSource{}, ErrNotFound
	}
	return r.source, nil
}

type fakeMiyunAssetReader struct{ source MiyunAssetSource }

func (r *fakeMiyunAssetReader) ReadMiyunAssetSource(_ context.Context, _ contract.ActorContext, projectID contract.ProjectID, ref contract.AssetVersionRef) (MiyunAssetSource, error) {
	if projectID != "project_1" || ref != r.source.Ref {
		return MiyunAssetSource{}, ErrNotFound
	}
	return r.source, nil
}

type fakeMiyunKnowledgeReader struct{ source MiyunKnowledgeSource }

func (r fakeMiyunKnowledgeReader) ReadMiyunKnowledgeSource(_ context.Context, _ contract.ActorContext, projectID contract.ProjectID, id string) (MiyunKnowledgeSource, error) {
	if projectID != "project_1" || id != r.source.ID {
		return MiyunKnowledgeSource{}, ErrNotFound
	}
	return r.source, nil
}

type fakeMiyunMediaReader struct{ evidence MiyunMediaEvidence }

func (r fakeMiyunMediaReader) ReadLatestMiyunMediaEvidence(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, _ contract.AssetVersionRef) (MiyunMediaEvidence, bool, error) {
	return r.evidence, true, nil
}

func newMiyunTestService(repository *memoryMiyunServiceRepository, now time.Time) Service {
	sequence := 0
	return Service{
		Miyun: repository,
		MiyunProjects: fakeMiyunProjectReader{source: MiyunProjectSource{
			Context:     contract.ProjectContext{OrganizationID: "org_1", ProjectID: "project_1", BrandID: miyunBrandID("brand_1"), ProductIDs: []contract.ProductID{"product_1"}, ProjectContextVersion: 7},
			ProjectName: "Campaign", BrandName: "Thermos", CategoryName: "Drinkware",
			Products: []MiyunProjectProduct{{ID: "product_1", Name: "Insulated cup"}},
		}},
		MiyunAssets:    &fakeMiyunAssetReader{source: MiyunAssetSource{Ref: contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, Kind: contract.AssetVideo, MIMEType: "video/mp4", SHA256: fmt.Sprintf("%064d", 1), Ready: true}},
		MiyunKnowledge: fakeMiyunKnowledgeReader{source: MiyunKnowledgeSource{ID: "document_1", Filename: "brief.txt", MIMEType: "text/plain", Status: "ready", Text: "商品展示 轻量保温", TextSHA256: fmt.Sprintf("%064d", 2)}},
		MiyunMedia:     fakeMiyunMediaReader{evidence: MiyunMediaEvidence{ArtifactID: "evidence_1", Status: "partial", ContentHash: fmt.Sprintf("%064d", 3), Evidence: []string{"商品展示"}}},
		Now:            func() time.Time { return now },
		NewID:          func(prefix string) (string, error) { sequence++; return fmt.Sprintf("%s_%d", prefix, sequence), nil },
	}
}

func miyunTestActor() contract.ActorContext {
	return contract.ActorContext{OrganizationID: "org_1", Principal: contract.Principal{Kind: contract.PrincipalUser, ID: "operator_1"}, Scopes: []contract.Scope{ScopeRead, ScopeWrite, ScopeConfirm}}
}

func miyunBrandID(value contract.BrandID) *contract.BrandID { return &value }

func TestMiyunHandoffCreateFreezesInputAndReplays(t *testing.T) {
	now := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	asset := service.MiyunAssets.(*fakeMiyunAssetReader)
	asset.source.SHA256 = miyunHandoffTestHash([]byte("video"))
	material := validMiyunMaterial(now)
	material.SelectionStatus, material.ImportStatus = MiyunMaterialConfirmed, MiyunMaterialImportImported
	material.PlatformAssetID, material.PlatformAssetVersion = "asset_1", 1
	repository.materials[material.ID] = material
	snapshot := validMiyunMaterialSnapshot(now)
	snapshot.MaterialID = material.ID
	latestSnapshot := snapshot
	latestSnapshot.ID = "miyun_snapshot_latest"
	latestSnapshot.CapturedAt = now.Add(time.Minute)
	latestSnapshot.MaterialScore = 99
	repository.snapshots[material.ID] = []MiyunMaterialSnapshot{snapshot, latestSnapshot}
	profile := validMiyunProductProfile(now)
	profile.Status, profile.Version = MiyunProfileConfirmed, 2
	profile.ProductAssetRefs = []contract.AssetVersionRef{{AssetID: "asset_1", Version: 1}}
	profile.KnowledgeDocumentIDs = []string{}
	repository.profiles[profile.ID] = profile

	first, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil || first.ID != second.ID || first.Status != MiyunHandoffExporting {
		t.Fatalf("idempotent create = %#v, %#v, %v", first, second, err)
	}
	var frozenSources miyunHandoffSourcesSnapshot
	if err := json.Unmarshal(first.SourceSnapshot, &frozenSources); err != nil || len(frozenSources.Sources) != 1 || frozenSources.Sources[0].DataCard.ID != latestSnapshot.ID {
		t.Fatalf("handoff did not freeze the latest snapshot in its crawl job: %#v, %v", frozenSources, err)
	}
	frozen := append([]byte(nil), first.ProfileSnapshot...)
	profile.ProductName = "Changed after handoff"
	repository.profiles[profile.ID] = profile
	got, err := service.GetMiyunHandoff(context.Background(), miyunTestActor(), "project_1", first.ID)
	if err != nil || !bytes.Equal(got.ProfileSnapshot, frozen) || got.InputHash != first.InputHash {
		t.Fatalf("handoff was not frozen: %#v, %v", got, err)
	}
	material.SelectionStatus = MiyunMaterialDiscovered
	repository.materials[material.ID] = material
	if _, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("unconfirmed material create error=%v", err)
	}
}

func TestMiyunHandoffFreezesMultipleSourcesInStableOrder(t *testing.T) {
	now := time.Date(2026, 8, 11, 1, 30, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	asset := service.MiyunAssets.(*fakeMiyunAssetReader)
	asset.source.SHA256 = miyunHandoffTestHash([]byte("video"))
	profile := validMiyunProductProfile(now)
	profile.Status = MiyunProfileConfirmed
	profile.ProductAssetRefs = []contract.AssetVersionRef{{AssetID: "asset_1", Version: 1}}
	profile.KnowledgeDocumentIDs = []string{}
	repository.profiles[profile.ID] = profile
	first := validMiyunMaterial(now)
	first.ID, first.MiyunMaterialID = "miyunmaterial_b", "remote-b"
	first.SelectionStatus, first.ImportStatus = MiyunMaterialConfirmed, MiyunMaterialImportImported
	first.PlatformAssetID, first.PlatformAssetVersion = "asset_1", 1
	second := first
	second.ID, second.MiyunMaterialID = "miyunmaterial_a", "remote-a"
	for _, material := range []MiyunMaterial{first, second} {
		repository.materials[material.ID] = material
		snapshot := validMiyunMaterialSnapshot(now)
		snapshot.MaterialID = material.ID
		repository.snapshots[material.ID] = []MiyunMaterialSnapshot{snapshot}
	}
	created, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{first.ID, second.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := created.SourceMaterialIDs, []string{second.ID, first.ID}; !reflect.DeepEqual(got, want) || created.SourceMaterialID != second.ID {
		t.Fatalf("frozen sources = %#v, primary=%s", got, created.SourceMaterialID)
	}
	var frozen miyunHandoffSourcesSnapshot
	if err := json.Unmarshal(created.SourceSnapshot, &frozen); err != nil || len(frozen.Sources) != 2 || frozen.Sources[0].Material.ID != second.ID {
		t.Fatalf("frozen source snapshot = %#v, %v", frozen, err)
	}
	replayed, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{second.ID, first.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("unordered replay = %#v, %v", replayed, err)
	}
}

func TestMiyunHandoffExportAndExplicitDeliveryState(t *testing.T) {
	now := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	asset := service.MiyunAssets.(*fakeMiyunAssetReader)
	asset.source.SHA256 = miyunHandoffTestHash([]byte("video"))
	material := validMiyunMaterial(now)
	material.SelectionStatus, material.ImportStatus = MiyunMaterialConfirmed, MiyunMaterialImportDeduplicated
	material.PlatformAssetID, material.PlatformAssetVersion = "asset_1", 1
	repository.materials[material.ID] = material
	snapshot := validMiyunMaterialSnapshot(now)
	snapshot.MaterialID = material.ID
	repository.snapshots[material.ID] = []MiyunMaterialSnapshot{snapshot}
	profile := validMiyunProductProfile(now)
	profile.Status, profile.Version = MiyunProfileConfirmed, 2
	profile.ProductAssetRefs = []contract.AssetVersionRef{{AssetID: "asset_1", Version: 1}}
	profile.KnowledgeDocumentIDs = []string{}
	repository.profiles[profile.ID] = profile
	handoff, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil {
		t.Fatal(err)
	}
	service.MiyunHandoffContent = miyunHandoffContentTestDouble{content: map[string][]byte{"asset:asset_1:1": []byte("video")}}
	if _, err := service.MarkMiyunHandoffDelivered(context.Background(), miyunTestActor(), "project_1", handoff.ID, handoff.Version); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("delivery before export error=%v", err)
	}
	var output bytes.Buffer
	if err := service.ExportMiyunHandoff(context.Background(), miyunTestActor(), "project_1", handoff.ID, MiyunHandoffPackageSources, &output); err != nil || output.Len() == 0 {
		t.Fatalf("export error=%v size=%d", err, output.Len())
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != "miyun_"+material.ID+".mp4" {
		t.Fatalf("source archive identity=%#v err=%v", archive, err)
	}
	exported, _ := service.GetMiyunHandoff(context.Background(), miyunTestActor(), "project_1", handoff.ID)
	if exported.Status != MiyunHandoffExported {
		t.Fatalf("status after export=%s", exported.Status)
	}
	if _, err := service.MarkMiyunHandoffDelivered(context.Background(), miyunTestActor(), "project_1", handoff.ID, handoff.Version); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale delivery version error=%v", err)
	}
	delivered, err := service.MarkMiyunHandoffDelivered(context.Background(), miyunTestActor(), "project_1", handoff.ID, exported.Version)
	if err != nil || delivered.Status != MiyunHandoffDelivered {
		t.Fatalf("delivery=%#v err=%v", delivered, err)
	}
	if repeated, err := service.MarkMiyunHandoffDelivered(context.Background(), miyunTestActor(), "project_1", handoff.ID, delivered.Version); err != nil || repeated.ID != delivered.ID {
		t.Fatalf("delivery replay=%#v err=%v", repeated, err)
	}
	returned := delivered
	returned.Status = MiyunHandoffReturned
	repository.handoffs[returned.ID] = returned
	output.Reset()
	if err := service.ExportMiyunHandoff(context.Background(), miyunTestActor(), "project_1", returned.ID, MiyunHandoffPackageSources, &output); err != nil || output.Len() == 0 {
		t.Fatalf("returned handoff re-export error=%v size=%d", err, output.Len())
	}
	profile.ProductName = "second frozen profile"
	repository.profiles[profile.ID] = profile
	failing, err := service.CreateMiyunHandoff(context.Background(), miyunTestActor(), "project_1", CreateMiyunHandoffRequest{SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, CrawlJobID: "miyun_job_1"})
	if err != nil || failing.ID == handoff.ID {
		t.Fatalf("second handoff=%#v err=%v", failing, err)
	}
	if err := service.ExportMiyunHandoff(context.Background(), miyunTestActor(), "project_1", failing.ID, MiyunHandoffPackageSources, miyunHandoffFailingWriter{}); err == nil {
		t.Fatal("failed response writer exported a handoff")
	}
	failed, _ := service.GetMiyunHandoff(context.Background(), miyunTestActor(), "project_1", failing.ID)
	if failed.Status != MiyunHandoffFailed {
		t.Fatalf("failed export status=%s", failed.Status)
	}
}

func TestMiyunReturnCreateRequiresExportedOrDeliveredAndReplays(t *testing.T) {
	now := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	handoff := MiyunHandoff{ID: "handoff_return_1", OrganizationID: "org_1", ProjectID: "project_1", SourceMaterialID: "material_1", SourceMaterialIDs: []string{"material_1"}, ProductProfileID: "profile_1", Status: MiyunHandoffExported, ManifestVersion: MiyunHandoffManifestVersion, ParameterVersion: MiyunHandoffParameterVersion, ProductFilesSnapshot: []byte(`{"media":[],"documents":[]}`), SourceSnapshot: []byte(`{"sources":[]}`), ProfileSnapshot: []byte(`{"profile":{},"profile_version":1}`), InputHash: strings.Repeat("a", 64), Version: 7, CreatedBy: "user_1", CreatedAt: now, UpdatedAt: now}
	repository.handoffs[handoff.ID] = handoff
	first, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "return-key", CreateMiyunHandoffReturnRequest{ExpectedVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "return-key", CreateMiyunHandoffReturnRequest{ExpectedVersion: 7})
	if err != nil || first.ID != second.ID || first.InputHash != handoff.InputHash {
		t.Fatalf("replay=%#v %#v %v", first, second, err)
	}
	if _, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "different", CreateMiyunHandoffReturnRequest{ExpectedVersion: 6}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale=%v", err)
	}
	handoff.Status = MiyunHandoffFailed
	repository.handoffs[handoff.ID] = handoff
	if _, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "failed", CreateMiyunHandoffReturnRequest{ExpectedVersion: 7}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("failed state=%v", err)
	}
	if _, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "other_project", handoff.ID, "scope", CreateMiyunHandoffReturnRequest{ExpectedVersion: 7}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross project=%v", err)
	}
}

func TestMiyunReturnUploadFreezesAssetThenExplicitlyMarksReturned(t *testing.T) {
	now := time.Date(2026, 8, 11, 3, 30, 0, 0, time.UTC)
	repository := newMiyunServiceRepository(now)
	service := newMiyunTestService(repository, now)
	service.Assets = testAssetService().Assets
	service.Projects = testProjects{}
	returnedHash := strings.Repeat("b", 64)
	importer := &fakeMiyunReturnImporter{result: MiyunReturnAssetImportResult{AssetVersion: contract.AssetVersionRef{AssetID: "returned_asset_1", Version: 1}, MIMEType: MiyunReturnImportMIMEType, SHA256: returnedHash, SizeBytes: 42}}
	service.MiyunReturns = importer
	handoff := MiyunHandoff{ID: "handoff_return_upload", OrganizationID: "org_1", ProjectID: "project_1", SourceMaterialID: "material_1", SourceMaterialIDs: []string{"material_1"}, ProductProfileID: "profile_1", Status: MiyunHandoffDelivered, ManifestVersion: MiyunHandoffManifestVersion, ParameterVersion: MiyunHandoffParameterVersion, ProductFilesSnapshot: []byte(`{"media":[],"documents":[]}`), SourceSnapshot: []byte(`{"sources":[]}`), ProfileSnapshot: []byte(`{"profile":{},"profile_version":1}`), InputHash: strings.Repeat("a", 64), Version: 7, CreatedBy: "user_1", CreatedAt: now, UpdatedAt: now}
	repository.handoffs[handoff.ID] = handoff

	created, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "return-create", CreateMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version})
	if err != nil {
		t.Fatal(err)
	}
	uploaded, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "return-upload", UploadMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, DeclaredSHA256: &returnedHash, Content: bytes.NewReader(make([]byte, 42))})
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != MiyunHandoffReturnUploaded || uploaded.AssetVersion != importer.result.AssetVersion || uploaded.SHA256 != returnedHash || uploaded.InputHash != handoff.InputHash || uploaded.UploadedBy != "operator_1" || importer.calls != 1 {
		t.Fatalf("uploaded return = %#v; importer=%#v", uploaded, importer)
	}
	if len(importer.request.SourceResources) != 2 || importer.request.SourceResources[0].ID != handoff.ID {
		t.Fatalf("return sources = %#v", importer.request.SourceResources)
	}
	if replay, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "return-upload", UploadMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, DeclaredSHA256: &returnedHash, Content: bytes.NewReader(make([]byte, 42))}); err != nil || replay.ID != uploaded.ID || importer.calls != 1 {
		t.Fatalf("uploaded replay = %#v, importer calls=%d, err=%v", replay, importer.calls, err)
	}
	if _, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "other-upload-key", UploadMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, DeclaredSHA256: &returnedHash, Content: bytes.NewReader(make([]byte, 42))}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("upload idempotency conflict = %v", err)
	}
	if _, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "return-upload", UploadMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version - 1, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, Content: bytes.NewReader(make([]byte, 42))}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale uploaded retry = %v", err)
	}
	returnedHandoff, returned, err := service.MarkMiyunHandoffReturned(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "return-mark", handoff.Version)
	if err != nil {
		t.Fatal(err)
	}
	if returned.Status != MiyunHandoffReturnReturned || returned.ReturnedBy != "operator_1" || returned.ReturnedAt == nil || returnedHandoff.Status != MiyunHandoffReturned {
		t.Fatalf("returned state = handoff %#v return %#v", returnedHandoff, returned)
	}
	if _, _, err := service.MarkMiyunHandoffReturned(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "return-mark", handoff.Version-1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale mark replay = %v", err)
	}
	if _, _, err := service.MarkMiyunHandoffReturned(context.Background(), miyunTestActor(), "project_1", handoff.ID, created.ID, "other-mark-key", handoff.Version); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("mark idempotency conflict = %v", err)
	}
	if replay, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", handoff.ID, "return-create", CreateMiyunHandoffReturnRequest{ExpectedVersion: handoff.Version}); err != nil || replay.ID != created.ID || replay.Status != MiyunHandoffReturnReturned {
		t.Fatalf("create replay after returned = %#v, %v", replay, err)
	}

	failing := handoff
	failing.ID, failing.Status, failing.Version = "handoff_return_failure", MiyunHandoffExported, 3
	repository.handoffs[failing.ID] = failing
	failedReturn, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", failing.ID, "failure-create", CreateMiyunHandoffReturnRequest{ExpectedVersion: failing.Version})
	if err != nil {
		t.Fatal(err)
	}
	service.MiyunReturns = &fakeMiyunReturnImporter{err: errors.New("client disconnected")}
	if _, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", failing.ID, failedReturn.ID, "failure-upload", UploadMiyunHandoffReturnRequest{ExpectedVersion: failing.Version, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, Content: bytes.NewReader(make([]byte, 42))}); err == nil {
		t.Fatal("upload failure unexpectedly succeeded")
	}
	failedStored, err := repository.GetMiyunHandoffReturn(context.Background(), "org_1", "project_1", failing.ID, failedReturn.ID)
	if err != nil || failedStored.Status != MiyunHandoffReturnFailed || repository.handoffs[failing.ID].Status != MiyunHandoffExported {
		t.Fatalf("failed upload must preserve exported handoff: return=%#v handoff=%#v err=%v", failedStored, repository.handoffs[failing.ID], err)
	}

	invalid := handoff
	invalid.ID, invalid.Status, invalid.Version = "handoff_return_invalid_asset", MiyunHandoffDelivered, 4
	repository.handoffs[invalid.ID] = invalid
	invalidReturn, err := service.CreateMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", invalid.ID, "invalid-create", CreateMiyunHandoffReturnRequest{ExpectedVersion: invalid.Version})
	if err != nil {
		t.Fatal(err)
	}
	service.MiyunReturns = &fakeMiyunReturnImporter{result: MiyunReturnAssetImportResult{AssetVersion: contract.AssetVersionRef{AssetID: "invalid_asset", Version: 1}, MIMEType: MiyunReturnImportMIMEType, SHA256: "not-a-sha", SizeBytes: 42}}
	if _, err := service.UploadMiyunHandoffReturn(context.Background(), miyunTestActor(), "project_1", invalid.ID, invalidReturn.ID, "invalid-upload", UploadMiyunHandoffReturnRequest{ExpectedVersion: invalid.Version, Filename: "final.mp4", DeclaredMIMEType: MiyunReturnImportMIMEType, DeclaredSizeBytes: 42, Content: bytes.NewReader(make([]byte, 42))}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid Asset result error = %v", err)
	}
	invalidStored, err := repository.GetMiyunHandoffReturn(context.Background(), "org_1", "project_1", invalid.ID, invalidReturn.ID)
	if err != nil || invalidStored.Status != MiyunHandoffReturnFailed || invalidStored.FailureCode != "ASSET_METADATA_INVALID" || repository.handoffs[invalid.ID].Status != MiyunHandoffDelivered {
		t.Fatalf("invalid asset metadata must preserve delivered handoff: return=%#v handoff=%#v err=%v", invalidStored, repository.handoffs[invalid.ID], err)
	}
}

type fakeMiyunReturnImporter struct {
	result  MiyunReturnAssetImportResult
	err     error
	calls   int
	request MiyunReturnAssetImportRequest
}

func (i *fakeMiyunReturnImporter) ImportMiyunReturnMP4(_ context.Context, _ contract.RequestContext, _ contract.ProjectID, _ contract.IdempotencyKey, request MiyunReturnAssetImportRequest) (MiyunReturnAssetImportResult, error) {
	i.calls++
	i.request = request
	if i.err != nil {
		return MiyunReturnAssetImportResult{}, i.err
	}
	return i.result, nil
}

type miyunHandoffContentTestDouble struct{ content map[string][]byte }

type miyunHandoffFailingWriter struct{}

func (miyunHandoffFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (d miyunHandoffContentTestDouble) OpenMiyunHandoffAsset(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, ref contract.AssetVersionRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(d.content["asset:"+string(ref.AssetID)+":"+fmt.Sprint(ref.Version)])), nil
}
func (d miyunHandoffContentTestDouble) OpenMiyunHandoffDocument(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, id string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(d.content["document:"+id])), nil
}
func miyunHandoffTestHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
