package insights_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/identity"
	"github.com/shikanon/cookies/internal/platform/project"
	"github.com/shikanon/cookies/internal/systems/insights"
)

func TestMiyunFoundationAgainstMySQL(t *testing.T) {
	dsn := os.Getenv("COOKIES_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COOKIES_TEST_MYSQL_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	organizationID := contract.OrganizationID("org_miyun_it_" + suffix)
	projectID := contract.ProjectID("project_miyun_it_" + suffix)
	otherProjectID := contract.ProjectID("project_miyun_other_it_" + suffix)
	userID := "user_miyun_it_" + suffix
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         []contract.Scope{"project.read", "project.write", "assets.write"},
	}
	t.Cleanup(func() {
		cleanupMiyunIntegration(t, db, organizationID)
		cleanupInsightsIntegration(t, db, organizationID, userID)
	})
	if err := (identity.MySQLStore{DB: db}).EnsureLocalActor(ctx, actor); err != nil {
		t.Fatal(err)
	}
	projects := project.MySQLStore{DB: db}
	if err := projects.EnsureLocalProject(ctx, actor, projectID); err != nil {
		t.Fatal(err)
	}
	if err := projects.EnsureLocalProject(ctx, actor, otherProjectID); err != nil {
		t.Fatal(err)
	}

	repository := insights.MySQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	connection := insights.MiyunConnection{
		ID: "miyun_connection_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		Status: insights.MiyunConnectionUnverified, SessionCiphertext: []byte("encrypted-test-envelope"),
		SessionKeyVersion: "key-v1", Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	connection, err = repository.CreateMiyunConnection(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	connection.Status = insights.MiyunConnectionReady
	connection.LastVerifiedAt = &now
	connection.UpdatedAt = now.Add(time.Second)
	connection, err = repository.UpdateMiyunConnection(ctx, connection, 1)
	if err != nil || connection.Version != 2 {
		t.Fatalf("connection=%#v err=%v", connection, err)
	}
	if _, err := repository.UpdateMiyunConnection(ctx, connection, 1); !errors.Is(err, insights.ErrVersionConflict) {
		t.Fatalf("stale connection update should conflict: %v", err)
	}
	if _, err := repository.GetMiyunConnection(ctx, organizationID, otherProjectID, connection.ID); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("cross-project connection read should be hidden: %v", err)
	}

	profile := insights.MiyunProductProfile{
		ID: "miyun_profile_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		ConnectionID: connection.ID, Status: insights.MiyunProfileDraft, ProductID: "product_test", ProductName: "Test product",
		CategoryID: "category-test", CategoryName: "Test category", Keywords: []string{"test keyword"},
		MaterialContentTypes: []string{"product_demo"}, WindowStart: now.AddDate(0, -1, 0), WindowEnd: now,
		ProjectContextVersion: 1, ProductAssetRefs: []contract.AssetVersionRef{}, KnowledgeDocumentIDs: []string{},
		RuleVersion: insights.MiyunProductProfileRuleVersion, AnalysisMethod: "rules",
		InputSnapshot:    []byte(`{"version":"1"}`),
		FieldSources:     []insights.MiyunProfileFieldSource{{Field: "keywords", SourceKind: "deterministic_rules", SourceRefs: []string{"product:product_1"}, Confidence: "medium", ReviewState: "suggested", Explanation: "fixture"}},
		AnalysisWarnings: []string{"model_not_used:deterministic_rules"},
		InputHash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Version:          1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	profile, err = repository.CreateMiyunProductProfileDraft(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	replacement := profile
	replacement.ID = "miyun_profile_replacement_it_" + suffix
	replacement.CreatedAt, replacement.UpdatedAt = now.Add(time.Second), now.Add(time.Second)
	replacement, err = repository.CreateMiyunProductProfileDraft(ctx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if superseded, err := repository.GetMiyunProductProfile(ctx, organizationID, projectID, profile.ID); err != nil || superseded.Status != insights.MiyunProfileSuperseded || superseded.Version != 2 {
		t.Fatalf("superseded profile=%#v err=%v", superseded, err)
	}
	confirmedAt := now.Add(2 * time.Second)
	replacement.Status, replacement.ConfirmedBy, replacement.ConfirmedAt = insights.MiyunProfileConfirmed, userID, &confirmedAt
	replacement.UpdatedAt = confirmedAt
	for index := range replacement.FieldSources {
		replacement.FieldSources[index].ReviewState = "human_confirmed"
	}
	profile, err = repository.ConfirmMiyunProductProfile(ctx, replacement, 1)
	if err != nil || profile.Version != 2 {
		t.Fatalf("confirmed profile=%#v err=%v", profile, err)
	}
	if reloaded, err := repository.GetMiyunProductProfile(ctx, organizationID, projectID, profile.ID); err != nil || reloaded.Keywords[0] != "test keyword" || reloaded.Status != insights.MiyunProfileConfirmed {
		t.Fatalf("profile=%#v err=%v", reloaded, err)
	}

	job := insights.MiyunCrawlJob{
		ID: "miyun_job_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		ConnectionID: connection.ID, ProductProfileID: profile.ID, Status: insights.MiyunCrawlJobQueued,
		Operation: "product", QuerySchemaVersion: "youshu-query-v1",
		QuerySnapshot: []byte(`{"keyword":"test keyword","page":1}`), IdempotencyKey: "crawl_" + suffix,
		RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuntimeJobID: "miyun_job_it_" + suffix, Version: 1,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	job, err = repository.CreateMiyunCrawlJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, duplicate, err := repository.CreateMiyunCrawlJobIdempotent(ctx, job); err != nil || !duplicate || replayed.ID != job.ID {
		t.Fatalf("crawl idempotency replay=%#v duplicate=%v err=%v", replayed, duplicate, err)
	}
	if _, err := repository.GetMiyunCrawlJob(ctx, organizationID, projectID, job.ID); err != nil {
		t.Fatal(err)
	}

	material := insights.MiyunMaterial{
		ID: "miyun_material_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		MiyunMaterialID: "remote-material-test", FirstSeenCrawlJobID: job.ID,
		ImportMethod: insights.MiyunImportCrawler, ResourceURLCiphertext: []byte("encrypted"), ResourceURLKeyVersion: "v1", SourceRefStatus: "unknown",
		SelectionStatus: insights.MiyunMaterialDiscovered, ImportStatus: insights.MiyunMaterialImportPending,
		Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	firstSnapshot := insights.MiyunMaterialSnapshot{
		ID: "miyun_snapshot_0_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		MaterialID: material.ID, CrawlJobID: job.ID, SourcePage: 1, ImportMethod: insights.MiyunImportCrawler,
		SchemaVersion: "miyun-card-v1", CapturedAt: now, CumulativeImpressions: 100,
		CumulativeImpressionsRaw: "100", RelatedAds: 5, RelatedCreatorsRaw: "unknown",
		SanitizedRaw: []byte(`{"schema_version":"miyun-card-v1"}`), CreatedAt: now,
	}
	job, err = repository.ApplyMiyunCrawlPage(ctx, job, 1, []insights.MiyunCrawlPageRecord{{Material: material, Snapshot: firstSnapshot}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != insights.MiyunCrawlJobSucceeded || job.CompletedPages != 1 || job.DiscoveredCount != 1 {
		t.Fatalf("applied crawl page=%#v", job)
	}
	material, err = repository.GetMiyunMaterial(ctx, organizationID, projectID, material.ID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := material
	duplicate.ID = "miyun_material_duplicate_it_" + suffix
	if _, err := repository.CreateMiyunMaterial(ctx, duplicate); !errors.Is(err, insights.ErrInvalidState) {
		t.Fatalf("duplicate remote identity should fail: %v", err)
	}
	if _, err := repository.GetMiyunMaterial(ctx, organizationID, otherProjectID, material.ID); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("cross-project material read should be hidden: %v", err)
	}

	capturedAt := now.Add(time.Hour)
	_, err = repository.AppendMiyunMaterialSnapshot(ctx, insights.MiyunMaterialSnapshot{
		ID:             "miyun_snapshot_1_it_" + suffix,
		OrganizationID: organizationID, ProjectID: projectID, MaterialID: material.ID, CrawlJobID: job.ID, SourcePage: 2, ImportMethod: insights.MiyunImportCrawler,
		SchemaVersion: "miyun-card-v1", CapturedAt: capturedAt,
		CumulativeImpressions: 101, CumulativeImpressionsRaw: "101", RelatedAds: 6,
		RelatedCreatorsRaw: "unknown", SanitizedRaw: []byte(`{"schema_version":"miyun-card-v1"}`), CreatedAt: capturedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repository.ListMiyunMaterialSnapshots(ctx, organizationID, projectID, material.ID)
	if err != nil || len(snapshots) != 2 || snapshots[0].CumulativeImpressions == snapshots[1].CumulativeImpressions {
		t.Fatalf("append-only snapshots=%#v err=%v", snapshots, err)
	}

	decisionAt := now.Add(2 * time.Hour)
	material.SelectionStatus, material.DecisionBy, material.DecisionAt, material.DecisionNote = insights.MiyunMaterialConfirmed, userID, &decisionAt, "confirmed in integration test"
	material.UpdatedAt = decisionAt
	material, err = repository.DecideMiyunMaterial(ctx, material, material.Version)
	if err != nil {
		t.Fatal(err)
	}
	material, err = repository.MarkMiyunMaterialImporting(ctx, material, material.Version, "runtime-test")
	if err != nil {
		t.Fatal(err)
	}
	material.UpdatedAt = decisionAt.Add(time.Second)
	material, err = repository.FailMiyunMaterialImport(ctx, material, material.Version, "download", "EXPIRED_URL")
	if err != nil {
		t.Fatal(err)
	}
	job, err = repository.GetMiyunCrawlJob(ctx, organizationID, projectID, job.ID)
	if err != nil || job.Status != insights.MiyunCrawlJobPartial || job.FailedCount != 1 {
		t.Fatalf("partial crawl job=%#v err=%v", job, err)
	}
	material, err = repository.MarkMiyunMaterialImporting(ctx, material, material.Version, "runtime-retry-test")
	if err != nil {
		t.Fatalf("failed material was not retryable: %v", err)
	}

	blobs := assets.NewMemoryBlobStore()
	assetRepository := assets.MySQLRepository{DB: db}
	assetImports := assets.ExternalImportService{
		Repository: assetRepository,
		Projects:   &project.Service{Store: projects, Authorizer: projects},
		Upload: assets.UploadService{
			Repository: assetRepository, Projects: &project.Service{Store: projects, Authorizer: projects}, Blobs: blobs,
			Scanner: assets.NoopScanner{}, QuarantineBucket: "miyun-it-quarantine", AssetsBucket: "miyun-it-assets", VideoProbe: miyunIntegrationVideoProbe{},
		},
		QuarantineBucket: "miyun-it-quarantine",
		NewID:            func(prefix string) (string, error) { return prefix + "_miyun_it_" + suffix, nil },
	}
	video := append([]byte{0, 0, 0, 16, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0}, 16)...)
	assetRef, err := assetImports.Import(ctx, contract.RequestContext{Actor: actor, RequestID: "request_" + suffix, TraceID: "trace_" + suffix}, projectID,
		contract.IdempotencyKey("miyun_external_"+suffix), assets.ExternalMediaImportRequest{
			SourceProvider: "miyun", SourceObjectID: material.MiyunMaterialID, MIMEType: "video/mp4", SizeBytes: int64(len(video)),
		}, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(video)), nil })
	if err != nil {
		t.Fatalf("external asset import: %v", err)
	}
	ledger, err := assetRepository.GetExternalImportBySource(ctx, organizationID, projectID, "miyun", material.MiyunMaterialID)
	if err != nil {
		t.Fatal(err)
	}
	insightAssetID := "insightasset_miyun_it_" + suffix
	material.UpdatedAt = decisionAt.Add(2 * time.Second)
	material, err = repository.CompleteMiyunMaterialImport(ctx, insights.MiyunMaterialImportCompletion{
		Material: material, ExpectedVersion: material.Version,
		Result: insights.MiyunAuthorizedImportResult{ExternalImportID: ledger.ID, AssetRef: assetRef.AssetVersion},
		InsightAsset: insights.Asset{
			ID: insightAssetID, OrganizationID: organizationID, ProjectID: projectID, LineageID: insightAssetID, Revision: 1,
			Title: "Imported Miyun material", SourceKind: insights.AssetSourceMiyun, SourceRef: "miyun://material/" + material.MiyunMaterialID,
			SourceJobID: job.ID, PlatformAssetID: string(assetRef.AssetVersion.AssetID), PlatformAssetVersion: assetRef.AssetVersion.Version,
			AnalysisStatus: insights.AnalysisAwaitingData, AnalysisStatusReason: "Authorized Miyun import; awaiting analysis.",
			AnalysisStatusChangedAt: &material.UpdatedAt, Version: 1, CreatedBy: userID, CreatedAt: material.UpdatedAt, UpdatedAt: material.UpdatedAt,
		},
	})
	if err != nil || material.ImportStatus != insights.MiyunMaterialImportImported || material.InsightAssetID != insightAssetID {
		t.Fatalf("completed Miyun import=%#v err=%v", material, err)
	}
	job, err = repository.GetMiyunCrawlJob(ctx, organizationID, projectID, job.ID)
	if err != nil || job.Status != insights.MiyunCrawlJobSucceeded || job.DownloadedCount != 1 {
		t.Fatalf("recovered crawl job=%#v err=%v", job, err)
	}
	authAt := decisionAt.Add(3 * time.Second)
	job.Status, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = insights.MiyunCrawlJobAuthRequired, "auth_required", "00:403005", authAt
	connection.Status, connection.LastErrorKind, connection.LastErrorCode, connection.LastErrorAt, connection.UpdatedAt = insights.MiyunConnectionAuthRequired, "auth_required", "00:403005", &authAt, authAt
	job, connection, err = repository.UpdateMiyunCrawlJobAndConnection(ctx, job, job.Version, connection, connection.Version)
	if err != nil || job.Status != insights.MiyunCrawlJobAuthRequired || connection.Status != insights.MiyunConnectionAuthRequired {
		t.Fatalf("atomic auth transition job=%#v connection=%#v err=%v", job, connection, err)
	}
	projectAsset, err := assetRepository.GetProjectAsset(ctx, organizationID, projectID, assetRef.AssetVersion)
	if err != nil || projectAsset.Version.SourceType != contract.AssetSourceImported {
		t.Fatalf("imported AssetVersion=%#v err=%v", projectAsset, err)
	}

	handoff := insights.MiyunHandoff{
		ID: "miyun_handoff_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		SourceMaterialID: material.ID, SourceMaterialIDs: []string{material.ID}, ProductProfileID: profile.ID, Status: insights.MiyunHandoffExporting,
		ManifestVersion: "miyun-manifest-v1", ParameterVersion: "parameters-v1",
		ProductFilesSnapshot: []byte(`[]`), SourceSnapshot: []byte(`{"material_id":"remote-material-test"}`),
		ProfileSnapshot: []byte(`{"profile_id":"miyun_profile"}`),
		InputHash:       fmt.Sprintf("%064x", sha256.Sum256([]byte("handoff-input-"+suffix))),
		Version:         1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := repository.CreateMiyunHandoff(ctx, handoff); err != nil {
		t.Fatal(err)
	}
	if reloaded, err := repository.GetMiyunHandoff(ctx, organizationID, projectID, handoff.ID); err != nil || reloaded.ManifestVersion != handoff.ManifestVersion {
		t.Fatalf("handoff=%#v err=%v", reloaded, err)
	}
	if _, err := repository.GetMiyunHandoff(ctx, organizationID, otherProjectID, handoff.ID); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("cross-project handoff read should be hidden: %v", err)
	}

	handoff.Status, handoff.UpdatedAt = insights.MiyunHandoffExported, now.Add(4*time.Second)
	handoff, err = repository.UpdateMiyunHandoffStatus(ctx, handoff, handoff.Version)
	if err != nil {
		t.Fatalf("mark handoff exported: %v", err)
	}
	handoff.Status, handoff.UpdatedAt = insights.MiyunHandoffDelivered, now.Add(5*time.Second)
	handoff, err = repository.UpdateMiyunHandoffStatus(ctx, handoff, handoff.Version)
	if err != nil {
		t.Fatalf("mark handoff delivered: %v", err)
	}

	// A failed registration must leave the handoff deliverable. The return
	// workflow is responsible for performing this Assets import before it
	// persists a returned state.
	_, err = assetImports.Import(ctx, contract.RequestContext{Actor: actor, RequestID: "return-failed_" + suffix, TraceID: "return-failed_" + suffix}, projectID,
		contract.IdempotencyKey("miyun_return_failed_"+suffix), assets.ExternalMediaImportRequest{
			SourceProvider: "miyun-return", SourceObjectID: handoff.ID + "-failed", MIMEType: "video/mp4", SizeBytes: int64(len(video) + 1),
		}, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(video)), nil })
	if err == nil {
		t.Fatal("return registration with a wrong frozen size should fail")
	}
	if persisted, err := repository.GetMiyunHandoff(ctx, organizationID, projectID, handoff.ID); err != nil || persisted.Status != insights.MiyunHandoffDelivered {
		t.Fatalf("failed return must not mark handoff returned: handoff=%#v err=%v", persisted, err)
	}

	returnedAsset, err := assetImports.Import(ctx, contract.RequestContext{Actor: actor, RequestID: "return-success_" + suffix, TraceID: "return-success_" + suffix}, projectID,
		contract.IdempotencyKey("miyun_return_success_"+suffix), assets.ExternalMediaImportRequest{
			SourceProvider: "miyun-return", SourceObjectID: handoff.ID, MIMEType: "video/mp4", SizeBytes: int64(len(video)),
		}, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(video)), nil })
	if err != nil {
		t.Fatalf("register returned MP4 through Assets: %v", err)
	}
	returnedProjectAsset, err := assetRepository.GetProjectAsset(ctx, organizationID, projectID, returnedAsset.AssetVersion)
	if err != nil {
		t.Fatalf("load registered returned MP4: %v", err)
	}
	returnedHash := fmt.Sprintf("%x", sha256.Sum256(video))
	if returnedProjectAsset.Version.SourceType != contract.AssetSourceImported || returnedProjectAsset.Version.MIMEType != "video/mp4" ||
		returnedProjectAsset.Version.SizeBytes != int64(len(video)) || returnedProjectAsset.Version.SHA256 != returnedHash ||
		returnedProjectAsset.Version.Media.ProbeStatus != assets.MediaProbeSucceeded {
		t.Fatalf("returned MP4 must be scanned and probed before the handoff transition: %#v", returnedProjectAsset.Version)
	}
	returnedAt := now.Add(6 * time.Second)
	returnRecord := insights.MiyunHandoffReturn{ID: "miyun_return_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID, HandoffID: handoff.ID, HandoffVersion: handoff.Version, ManifestVersion: handoff.ManifestVersion, InputHash: handoff.InputHash, ParameterVersion: handoff.ParameterVersion, ProductProfileID: handoff.ProductProfileID, CrawlJobID: handoff.CrawlJobID, AssociationSource: insights.MiyunReturnAssociationCrawlJob, Status: insights.MiyunHandoffReturnCreated, IdempotencyKey: "miyun_return_create_" + suffix, RequestHash: fmt.Sprintf("%064x", sha256.Sum256([]byte("return-create-"+suffix))), UploadedBy: userID, Version: 1, CreatedAt: returnedAt, UpdatedAt: returnedAt}
	returnRecord, _, err = repository.CreateMiyunHandoffReturn(ctx, returnRecord)
	if err != nil {
		t.Fatalf("create persistent return: %v", err)
	}
	returnRecord.Status, returnRecord.UploadIdempotencyKey, returnRecord.UploadRequestHash, returnRecord.Filename, returnRecord.AssetVersion, returnRecord.MIMEType, returnRecord.SHA256, returnRecord.SizeBytes, returnRecord.UploadedAt, returnRecord.UpdatedAt = insights.MiyunHandoffReturnUploaded, "miyun_return_upload_"+suffix, fmt.Sprintf("%064x", sha256.Sum256([]byte("return-upload-"+suffix))), "returned.mp4", returnedAsset.AssetVersion, "video/mp4", returnedHash, int64(len(video)), &returnedAt, returnedAt
	returnRecord, err = repository.MarkMiyunHandoffReturnUploaded(ctx, returnRecord, returnRecord.Version)
	if err != nil {
		t.Fatalf("freeze uploaded return: %v", err)
	}
	returnRecord.MarkIdempotencyKey, returnRecord.MarkRequestHash, returnRecord.ReturnedBy, returnRecord.ReturnedAt, returnRecord.UpdatedAt = "miyun_return_mark_"+suffix, fmt.Sprintf("%064x", sha256.Sum256([]byte("return-mark-"+suffix))), userID, &returnedAt, returnedAt
	returnRecord, handoff, err = repository.CompleteMiyunHandoffReturn(ctx, returnRecord, returnRecord.Version, handoff, handoff.Version)
	if err != nil {
		t.Fatalf("atomically mark returned: %v", err)
	}
	if reloaded, err := repository.GetMiyunHandoffReturn(ctx, organizationID, projectID, handoff.ID, returnRecord.ID); err != nil || reloaded.SHA256 != returnedHash || reloaded.AssetVersion != returnedAsset.AssetVersion || reloaded.UploadIdempotencyKey != returnRecord.UploadIdempotencyKey || reloaded.ReturnedBy != userID {
		t.Fatalf("reloaded return=%#v err=%v", reloaded, err)
	}
	if persisted, err := repository.GetMiyunHandoff(ctx, organizationID, projectID, handoff.ID); err != nil || persisted.Status != insights.MiyunHandoffReturned ||
		persisted.InputHash != returnRecord.InputHash || !bytes.Equal(persisted.ProductFilesSnapshot, handoff.ProductFilesSnapshot) ||
		!bytes.Equal(persisted.SourceSnapshot, handoff.SourceSnapshot) || !bytes.Equal(persisted.ProfileSnapshot, handoff.ProfileSnapshot) {
		t.Fatalf("returned handoff must retain frozen manifest inputs: handoff=%#v err=%v", persisted, err)
	}
}

func cleanupMiyunIntegration(t *testing.T, db *sql.DB, organizationID contract.OrganizationID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, statement := range []string{
		"DELETE FROM insight_miyun_handoff_returns WHERE organization_id=?",
		"DELETE FROM insight_miyun_handoffs WHERE organization_id=?",
		"DELETE FROM insight_miyun_material_snapshots WHERE organization_id=?",
		"DELETE FROM insight_miyun_materials WHERE organization_id=?",
		"DELETE FROM insight_assets WHERE organization_id=?",
		"DELETE FROM insight_miyun_crawl_jobs WHERE organization_id=?",
		"DELETE FROM insight_miyun_product_profiles WHERE organization_id=?",
		"DELETE FROM insight_miyun_connections WHERE organization_id=?",
		"DELETE FROM asset_external_imports WHERE organization_id=?",
		"DELETE FROM assets_outbox WHERE organization_id=?",
		"DELETE FROM project_assets WHERE organization_id=?",
		"DELETE FROM asset_versions WHERE organization_id=?",
		"DELETE FROM assets WHERE organization_id=?",
		"DELETE FROM asset_blobs WHERE organization_id=?",
		"DELETE FROM platform_project_runtimes WHERE organization_id=?",
	} {
		if _, err := db.ExecContext(ctx, statement, organizationID); err != nil {
			t.Errorf("cleanup %q: %v", statement, err)
		}
	}
}

type miyunIntegrationVideoProbe struct{}

func (miyunIntegrationVideoProbe) Probe(context.Context, []byte) (assets.VideoMetadata, error) {
	return assets.VideoMetadata{DurationMS: 1000, WidthPixels: 16, HeightPixels: 16, FrameRate: "25/1", VideoCodec: "h264"}, nil
}
