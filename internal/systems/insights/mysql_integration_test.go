package insights_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/identity"
	"github.com/shikanon/cookies/internal/platform/project"
	"github.com/shikanon/cookies/internal/systems/insights"
)

// TestExperienceLifecycleAgainstMySQL drives the persistence half of PRD §11.1
// and §7.6 through real DDL: the state machine, the version chain, and the
// audit trail that makes retirement a logical delete rather than a DELETE.
func TestExperienceLifecycleAgainstMySQL(t *testing.T) {
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
	organizationID := contract.OrganizationID("org_insights_it_" + suffix)
	projectID := contract.ProjectID("project_insights_it_" + suffix)
	userID := "user_insights_it_" + suffix
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         []contract.Scope{"project.read", "project.write"},
	}
	t.Cleanup(func() { cleanupInsightsIntegration(t, db, organizationID, userID) })
	if err := (identity.MySQLStore{DB: db}).EnsureLocalActor(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if err := (project.MySQLStore{DB: db}).EnsureLocalProject(ctx, actor, projectID); err != nil {
		t.Fatal(err)
	}

	repository := insights.MySQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	report, err := repository.CreateReport(ctx, insights.InsightReport{
		ID: "insightreport_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		ExecutionID: "deliveryexecution_it_" + suffix, DeliveryMode: "local_simulation",
		EvidenceID: "evidence_it_" + suffix, EvidenceSummary: "本地模拟证据",
		MetricSnapshotID: "metricsnapshot_it_" + suffix, CreativePackageID: "creativepackage_it_" + suffix,
		IsSimulated: true, DatasetVersion: "preroll-demo/v1", Status: insights.ReportConfirmed,
		Summary: "集成测试报告", Findings: []string{"发现"}, Version: 1,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := seedExperience(ctx, t, repository, organizationID, projectID, userID, report.ID, "experience_it_1_"+suffix, "", 1, now)

	// A freshly deposited conclusion is not quotable yet.
	pending, err := repository.ListExperiences(ctx, organizationID, projectID, insights.ExperiencePending, 50)
	if err != nil || len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending list=%d err=%v", len(pending), err)
	}
	confirmedBefore, err := repository.ListExperiences(ctx, organizationID, projectID, insights.ExperienceConfirmed, 50)
	if err != nil || len(confirmedBefore) != 0 {
		t.Fatalf("confirmed before=%d err=%v", len(confirmedBefore), err)
	}

	// A stale expected_version must not move the state machine.
	if _, err := repository.ConfirmExperience(ctx, insights.ConfirmExperienceInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: first.ID,
		ExpectedVersion: 99, ActorID: userID, Now: now,
		AuditID: "experienceaudit_it_stale_" + suffix, SupersedeAuditID: "experienceaudit_it_stale2_" + suffix,
	}); !errors.Is(err, insights.ErrVersionConflict) {
		t.Fatalf("stale confirm error = %v", err)
	}

	confirmed, err := repository.ConfirmExperience(ctx, insights.ConfirmExperienceInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: first.ID,
		ExpectedVersion: first.Version, ActorID: userID, Now: now,
		AuditID: "experienceaudit_it_2_" + suffix, SupersedeAuditID: "experienceaudit_it_3_" + suffix,
	})
	if err != nil || confirmed.Status != insights.ExperienceConfirmed || confirmed.ConfirmedBy != userID || !confirmed.Reusable() {
		t.Fatalf("confirm=%#v err=%v", confirmed, err)
	}

	reference, err := repository.CreateExperienceReference(ctx, insights.ExperienceReference{
		ID: "experienceref_it_1_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		ExperienceID: confirmed.ID, ConsumerKind: "strategy", ConsumerID: "strategy_it_" + suffix,
		Outcome: insights.ReferenceAdopted, Note: "直接采纳", Version: 1,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || reference.Outcome != insights.ReferenceAdopted {
		t.Fatalf("reference=%#v err=%v", reference, err)
	}

	// A revision supersedes rather than overwrites: it starts pending and the
	// predecessor stays quotable until the revision is confirmed.
	revision := seedExperience(ctx, t, repository, organizationID, projectID, userID, report.ID,
		"experience_it_2_"+suffix, confirmed.ID, 2, now)
	revision.LineageID = confirmed.LineageID
	stillConfirmed, err := repository.GetExperience(ctx, organizationID, projectID, confirmed.ID)
	if err != nil || !stillConfirmed.Reusable() {
		t.Fatalf("predecessor before confirm=%#v err=%v", stillConfirmed, err)
	}

	promoted, err := repository.ConfirmExperience(ctx, insights.ConfirmExperienceInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: revision.ID,
		ExpectedVersion: revision.Version, ActorID: userID, Now: now,
		AuditID: "experienceaudit_it_5_" + suffix, SupersedeAuditID: "experienceaudit_it_6_" + suffix,
	})
	if err != nil || promoted.Status != insights.ExperienceConfirmed {
		t.Fatalf("confirm revision=%#v err=%v", promoted, err)
	}
	retired, err := repository.GetExperience(ctx, organizationID, projectID, confirmed.ID)
	if err != nil || retired.Status != insights.ExperienceRetired || retired.SupersededByID != revision.ID {
		t.Fatalf("superseded predecessor=%#v err=%v", retired, err)
	}
	quotable, err := repository.ListExperiences(ctx, organizationID, projectID, insights.ExperienceConfirmed, 50)
	if err != nil || len(quotable) != 1 || quotable[0].ID != revision.ID {
		t.Fatalf("quotable=%d err=%v", len(quotable), err)
	}

	// Retirement is logical: the row and its reference history stay readable.
	references, err := repository.ListExperienceReferences(ctx, organizationID, projectID, confirmed.ID, 50)
	if err != nil || len(references) != 1 {
		t.Fatalf("references after retirement=%d err=%v", len(references), err)
	}
	audits, err := repository.ListExperienceAudits(ctx, organizationID, projectID, confirmed.ID, 50)
	if err != nil || len(audits) != 3 ||
		audits[0].ToStatus != insights.ExperiencePending ||
		audits[1].ToStatus != insights.ExperienceConfirmed ||
		audits[2].ToStatus != insights.ExperienceRetired {
		t.Fatalf("audits=%#v err=%v", audits, err)
	}
	lineage, err := repository.ListExperienceLineage(ctx, organizationID, projectID, confirmed.LineageID)
	if err != nil || len(lineage) != 2 || lineage[0].Revision != 1 || lineage[1].Revision != 2 {
		t.Fatalf("lineage=%d err=%v", len(lineage), err)
	}

	// 质疑一条在用的结论，走的是标记，不是状态。标完它还在用、还能被引用。
	flagged, err := repository.FlagExperienceForReview(ctx, insights.FlagExperienceReviewInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: revision.ID,
		ExpectedVersion: promoted.Version, NeedsReview: true,
		Reason: "新一轮数据与结论冲突", ActorID: userID, Now: now,
		AuditID: "experienceaudit_it_7_" + suffix,
	})
	if err != nil || flagged.Status != insights.ExperienceConfirmed || !flagged.NeedsReview || !flagged.Reusable() {
		t.Fatalf("flag for review=%#v err=%v", flagged, err)
	}
	// 标记要真的落库，不能只活在返回值里——下一个读它的人得看得见。
	stored, err := repository.GetExperience(ctx, organizationID, projectID, revision.ID)
	if err != nil || !stored.NeedsReview || stored.ReviewHint() == "" {
		t.Fatalf("stored flag=%#v err=%v", stored, err)
	}
	// 拿旧版本号再标一次是并发写，必须被版本号挡住。
	if _, err := repository.FlagExperienceForReview(ctx, insights.FlagExperienceReviewInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: revision.ID,
		ExpectedVersion: promoted.Version, NeedsReview: true,
		Reason: "重复请求", ActorID: userID, Now: now,
		AuditID: "experienceaudit_it_8_" + suffix,
	}); !errors.Is(err, insights.ErrVersionConflict) {
		t.Fatalf("stale flag error = %v", err)
	}
}

// TestAssetAnalysisAgainstMySQL drives 分析素材库与内容分析 through the real DDL.
// The point of running it against MySQL rather than the in-memory fake is the
// constraint that the fake can only imitate: uq_insight_asset_features_layer is
// what actually keeps 人工结论 from being overwritten by the next extraction (§14).
func TestAssetAnalysisAgainstMySQL(t *testing.T) {
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
	organizationID := contract.OrganizationID("org_assets_it_" + suffix)
	projectID := contract.ProjectID("project_assets_it_" + suffix)
	userID := "user_assets_it_" + suffix
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         []contract.Scope{"project.read", "project.write"},
	}
	t.Cleanup(func() { cleanupInsightsIntegration(t, db, organizationID, userID) })
	if err := (identity.MySQLStore{DB: db}).EnsureLocalActor(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if err := (project.MySQLStore{DB: db}).EnsureLocalProject(ctx, actor, projectID); err != nil {
		t.Fatal(err)
	}

	repository := insights.MySQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	assetID := "insightasset_it_1_" + suffix
	asset, err := repository.CreateAsset(ctx, insights.Asset{
		ID: assetID, OrganizationID: organizationID, ProjectID: projectID,
		LineageID: assetID, Revision: 1, Title: "集成测试公众号图文",
		SourceKind: insights.AssetSourceUpload, AssetType: insights.AssetTypeWechatArticle,
		AssetTypeSource: insights.SourceHuman, AnalysisStatus: insights.AnalysisAnalysable,
		AnalysisStatusChangedAt: &now, Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || asset.AnalysisStatus != insights.AnalysisAnalysable {
		t.Fatalf("asset=%#v err=%v", asset, err)
	}

	// AI 层：一次提取写入两项特征，素材进入待确认。
	aiFeatures := []insights.AssetFeature{
		assetFeature("assetfeature_it_ai_type_"+suffix, organizationID, projectID, assetID, "article_type",
			insights.FeatureValue{Kind: "enum", Terms: []string{"知识"}}, insights.SourceAI, userID, now),
		assetFeature("assetfeature_it_ai_count_"+suffix, organizationID, projectID, assetID, "section_count",
			insights.FeatureValue{Kind: "number", Number: 5}, insights.SourceAI, userID, now),
	}
	for i := range aiFeatures {
		aiFeatures[i].Confidence = insights.ConfidenceLow
		aiFeatures[i].ReviewState = insights.ReviewPending
		aiFeatures[i].SkillID = "skill_it_" + suffix
	}
	stored, err := repository.UpsertAssetFeatures(ctx, insights.UpsertAssetFeaturesInput{
		OrganizationID: organizationID, ProjectID: projectID, AssetID: assetID,
		ExpectedVersion: asset.Version, ReplaceLayer: insights.SourceAI, Features: aiFeatures,
		From: []insights.AnalysisStatus{insights.AnalysisAnalysable},
		To:   insights.AnalysisPendingConfirmation, Reason: "集成测试提取", Now: now,
	})
	if err != nil || len(stored) != 2 {
		t.Fatalf("stored=%d err=%v", len(stored), err)
	}
	unreviewed, err := repository.CountAssetFeaturesByReviewState(ctx, organizationID, projectID, assetID, insights.ReviewPending)
	if err != nil || unreviewed != 2 {
		t.Fatalf("unreviewed=%d err=%v", unreviewed, err)
	}

	// 人工层：改写其中一项。AI 那一行必须原样留在旁边。
	current, err := repository.GetAsset(ctx, organizationID, projectID, assetID)
	if err != nil {
		t.Fatal(err)
	}
	humanFeature := assetFeature("assetfeature_it_human_type_"+suffix, organizationID, projectID, assetID, "article_type",
		insights.FeatureValue{Kind: "enum", Terms: []string{"案例"}}, insights.SourceHuman, userID, now)
	humanFeature.ReviewState = insights.ReviewRejected
	if _, err = repository.UpsertAssetFeatures(ctx, insights.UpsertAssetFeaturesInput{
		OrganizationID: organizationID, ProjectID: projectID, AssetID: assetID,
		ExpectedVersion: current.Version, ReplaceLayer: insights.SourceHuman,
		Features: []insights.AssetFeature{humanFeature},
		From:     []insights.AnalysisStatus{insights.AnalysisPendingConfirmation},
		To:       insights.AnalysisPendingConfirmation, Reason: "集成测试人工修正", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	// 再提取一次同一个键：唯一键落在 (组织, 素材, 特征, 来源) 上，只能盖掉 AI 那一行。
	current, err = repository.GetAsset(ctx, organizationID, projectID, assetID)
	if err != nil {
		t.Fatal(err)
	}
	reExtracted := assetFeature("assetfeature_it_ai_type2_"+suffix, organizationID, projectID, assetID, "article_type",
		insights.FeatureValue{Kind: "enum", Terms: []string{"叙事"}}, insights.SourceAI, userID, now)
	reExtracted.Confidence = insights.ConfidenceHigh
	reExtracted.ReviewState = insights.ReviewPending
	features, err := repository.UpsertAssetFeatures(ctx, insights.UpsertAssetFeaturesInput{
		OrganizationID: organizationID, ProjectID: projectID, AssetID: assetID,
		ExpectedVersion: current.Version, ReplaceLayer: insights.SourceAI,
		Features: []insights.AssetFeature{reExtracted},
		From:     []insights.AnalysisStatus{insights.AnalysisPendingConfirmation},
		To:       insights.AnalysisPendingConfirmation, Reason: "集成测试重新提取", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	layers := map[string]string{}
	for _, feature := range features {
		if feature.Key == "article_type" {
			layers[string(feature.Source)] = feature.Value.Terms[0]
		}
	}
	if len(features) != 3 || layers["ai"] != "叙事" || layers["human"] != "案例" {
		t.Fatalf("重新提取只应覆盖 AI 层：features=%#v", features)
	}

	// article_type 已有人工结论，只剩 section_count 未复核。
	unreviewed, err = repository.CountAssetFeaturesByReviewState(ctx, organizationID, projectID, assetID, insights.ReviewPending)
	if err != nil || unreviewed != 1 {
		t.Fatalf("unreviewed after human conclusion=%d err=%v", unreviewed, err)
	}

	// 待匹配队列：平台对象先落库，再由人指向素材。
	mappingID := "insightassetmapping_it_1_" + suffix
	mapping, err := repository.CreateAssetMapping(ctx, insights.AssetMapping{
		ID: mappingID, OrganizationID: organizationID, ProjectID: projectID,
		Platform: "demo_platform", PlatformObjectKind: "creative", PlatformObjectID: "cr-it-" + suffix,
		PlatformObjectName: "集成测试创意", Status: insights.MappingUnmatched,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || mapping.Status != insights.MappingUnmatched {
		t.Fatalf("mapping=%#v err=%v", mapping, err)
	}
	queue, err := repository.ListAssetMappings(ctx, organizationID, projectID, insights.AssetMappingFilter{
		Statuses: []insights.MappingStatus{insights.MappingUnmatched},
	})
	if err != nil || len(queue) != 1 {
		t.Fatalf("queue=%d err=%v", len(queue), err)
	}
	mapping.Status = insights.MappingMatched
	mapping.AssetID = assetID
	mapping.MatchSource = "human"
	mapping.MatchedBy = userID
	mapping.MatchedAt = &now
	resolved, err := repository.ResolveAssetMapping(ctx, mapping, 1)
	if err != nil || resolved.AssetID != assetID || resolved.Version != 2 {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	if _, err = repository.ResolveAssetMapping(ctx, mapping, 1); !errors.Is(err, insights.ErrVersionConflict) {
		t.Fatalf("stale resolve error = %v", err)
	}

	// 失效是逻辑删除：行还在，状态机拦住重复操作。
	current, err = repository.GetAsset(ctx, organizationID, projectID, assetID)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := repository.TransitionAsset(ctx, insights.TransitionAssetInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: current.Version,
		From: []insights.AnalysisStatus{
			insights.AnalysisAwaitingData, insights.AnalysisAwaitingMatch, insights.AnalysisAnalysable,
			insights.AnalysisPendingConfirmation, insights.AnalysisConfirmed, insights.AnalysisNeedsReview,
		},
		To: insights.AnalysisRetired, Reason: "集成测试失效", Now: now,
	})
	if err != nil || retired.AnalysisStatus != insights.AnalysisRetired {
		t.Fatalf("retired=%#v err=%v", retired, err)
	}
	if _, err = repository.TransitionAsset(ctx, insights.TransitionAssetInput{
		OrganizationID: organizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: retired.Version,
		From:            []insights.AnalysisStatus{insights.AnalysisConfirmed},
		To:              insights.AnalysisNeedsReview, Reason: "重复请求", Now: now,
	}); !errors.Is(err, insights.ErrInvalidState) {
		t.Fatalf("repeat transition error = %v", err)
	}
	remaining, err := repository.ListAssets(ctx, organizationID, projectID, insights.AssetFilter{
		Statuses: []insights.AnalysisStatus{insights.AnalysisRetired},
	})
	if err != nil || len(remaining) != 1 {
		t.Fatalf("retired asset must stay readable: remaining=%d err=%v", len(remaining), err)
	}
}

func assetFeature(id string, organizationID contract.OrganizationID, projectID contract.ProjectID,
	assetID, key string, value insights.FeatureValue, source insights.FeatureSource,
	userID string, now time.Time) insights.AssetFeature {
	return insights.AssetFeature{
		ID: id, OrganizationID: organizationID, ProjectID: projectID, AssetID: assetID,
		AssetType: insights.AssetTypeWechatArticle, Key: key, Value: value, Source: source,
		ExtractedAt: &now, Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
}

func seedExperience(ctx context.Context, t *testing.T, repository insights.MySQLRepository,
	organizationID contract.OrganizationID, projectID contract.ProjectID, userID, reportID, id, supersedesID string,
	revision int, now time.Time) insights.Experience {
	t.Helper()
	lineageID := id
	if supersedesID != "" {
		source, err := repository.GetExperience(ctx, organizationID, projectID, supersedesID)
		if err != nil {
			t.Fatal(err)
		}
		lineageID = source.LineageID
	}
	value := insights.Experience{
		ID: id, OrganizationID: organizationID, ProjectID: projectID,
		LineageID: lineageID, Revision: revision, SupersedesID: supersedesID,
		ReportID: reportID, SourceExecutionID: "deliveryexecution_it_" + id,
		SourceEvidenceID: "evidence_it_" + id, SourceMetricSnapshotID: "metricsnapshot_it_" + id,
		Conclusion: "本地集成测试结论", Conditions: []string{"小红书图文"}, Counterexamples: []string{"未覆盖视频"},
		// card_type 和 confidence 在库里都有 CHECK，空串过不去。这里给足量证据，
		// 是因为下面要断言「确认之后可默认引用」——两道闸得都开着才验得出来。
		CardType:  insights.CardStatistic,
		Judgement: insights.NewJudgement(insights.ConfidenceSufficient, ""),
		Status:    insights.ExperiencePending, StatusChangedBy: userID, StatusChangedAt: &now,
		Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	stored, err := repository.CreateExperience(ctx, value, insights.ExperienceAudit{
		ID: "experienceaudit_seed_" + id, OrganizationID: organizationID, ProjectID: projectID,
		ExperienceID: id, ToStatus: insights.ExperiencePending, Reason: "集成测试沉淀",
		ActorID: userID, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func cleanupInsightsIntegration(t *testing.T, db *sql.DB, organizationID contract.OrganizationID, userID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	statements := []string{
		"DELETE FROM insight_metric_daily WHERE organization_id=?",
		"DELETE FROM insight_import_batches WHERE organization_id=?",
		"DELETE FROM insight_data_sources WHERE organization_id=?",
		"DELETE FROM insight_asset_features WHERE organization_id=?",
		"DELETE FROM insight_asset_mappings WHERE organization_id=?",
		"DELETE FROM insight_assets WHERE organization_id=?",
		"DELETE FROM insight_experience_references WHERE organization_id=?",
		"DELETE FROM insight_experience_audits WHERE organization_id=?",
		"DELETE FROM insight_experiences WHERE organization_id=?",
		"DELETE FROM insight_reports WHERE organization_id=?",
		// EnsureLocalProject 会顺手建一行运行时，它外键指向 projects。
		// 不先删它，下面每一条清理都会连环失败，测试留一地脏数据。
		"DELETE FROM platform_project_runtimes WHERE organization_id=?",
		"DELETE FROM project_context_versions WHERE organization_id=?",
		"DELETE FROM project_products WHERE organization_id=?",
		"DELETE FROM project_memberships WHERE organization_id=?",
		"DELETE FROM projects WHERE organization_id=?",
		"DELETE FROM brand_guideline_versions WHERE organization_id=?",
		"DELETE FROM products WHERE organization_id=?",
		"DELETE FROM brands WHERE organization_id=?",
		"DELETE FROM organization_memberships WHERE organization_id=?",
		"DELETE FROM organizations WHERE id=?",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement, organizationID); err != nil {
			t.Errorf("cleanup %q: %v", statement, err)
		}
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id=?", userID); err != nil {
		t.Errorf("cleanup user: %v", err)
	}
}

// TestDataIngestionAgainstMySQL drives doc10 §7/§8 through the real DDL: the
// idempotency key that keeps a re-run from doubling spend, the duplicate-batch
// key that blocks the same file twice, and the left join that keeps unmatched
// platform objects in the result set instead of dropping their spend.
func TestDataIngestionAgainstMySQL(t *testing.T) {
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
	organizationID := contract.OrganizationID("org_ingest_it_" + suffix)
	projectID := contract.ProjectID("project_ingest_it_" + suffix)
	userID := "user_ingest_it_" + suffix
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         []contract.Scope{"project.read", "project.write"},
	}
	t.Cleanup(func() { cleanupInsightsIntegration(t, db, organizationID, userID) })
	if err := (identity.MySQLStore{DB: db}).EnsureLocalActor(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if err := (project.MySQLStore{DB: db}).EnsureLocalProject(ctx, actor, projectID); err != nil {
		t.Fatal(err)
	}

	repository := insights.MySQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Second)
	statDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	caliber := insights.MetricCaliber{TimeZone: "Asia/Shanghai", Currency: "CNY",
		AttributionWindow: "click_7d", MetricSchemaVersion: "v1"}

	source, err := repository.CreateDataSource(ctx, insights.DataSource{
		ID: "insightsource_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		Platform: insights.PlatformDouyin, AccountLabel: "集成测试账户", AccountRef: "adv_" + suffix,
		IngestMode: insights.IngestFileImport, CredentialRef: "vault://it/" + suffix,
		Status: insights.DataSourceDraft, QualityStatus: insights.QualityHealthy,
		Caliber: caliber, FieldMapping: map[string]string{"展示数": "impressions", "消耗": "spend_cents"},
		Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 同一项目下同一个平台账户只能接入一次。
	if _, err := repository.CreateDataSource(ctx, source); err == nil {
		t.Fatal("重复接入同一个平台账户应当被唯一键挡回")
	}

	// 字段映射写进 JSON 列后要能原样读回来，否则接入向导第二步会空着。
	activated := source
	activated.Status = insights.DataSourceActive
	activated.DataThrough = &statDate
	activated.LastSyncedAt = &now
	activated.UpdatedAt = now
	activated, err = repository.UpdateDataSource(ctx, activated, source.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateDataSource(ctx, activated, source.Version); !errors.Is(err, insights.ErrVersionConflict) {
		t.Fatalf("过期版本应当冲突：err=%v", err)
	}
	reloaded, err := repository.GetDataSource(ctx, organizationID, projectID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FieldMapping["展示数"] != "impressions" || reloaded.Caliber.AttributionWindow != "click_7d" ||
		reloaded.DataThrough == nil || reloaded.DataThrough.Format("2006-01-02") != "2026-07-20" {
		t.Fatalf("数据源读回来不完整：%#v", reloaded)
	}

	windowStart, windowEnd := statDate, statDate
	batch := insights.ImportBatch{
		ID: "insightbatch_it_1_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		DataSourceID: source.ID, Kind: insights.ImportFile, Status: insights.ImportRunning,
		SourceLabel: "7月投放.csv", WindowStart: &windowStart, WindowEnd: &windowEnd,
		ContentHash: "hash_" + suffix, RequestedRows: 2, StartedAt: &now,
		Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	batch, err = repository.CreateImportBatch(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	// doc10 §8：相同文件哈希 + 相同导入范围默认阻止重复。
	duplicate := batch
	duplicate.ID = "insightbatch_it_2_" + suffix
	if _, err := repository.CreateImportBatch(ctx, duplicate); !errors.Is(err, insights.ErrInvalidState) {
		t.Fatalf("重复批次应当被挡回：err=%v", err)
	}

	fact := insights.MetricFact{
		ID: "insightmetric_it_1_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		DataSourceID: source.ID, ImportBatchID: batch.ID, Platform: insights.PlatformDouyin,
		PlatformObjectKind: "creative", PlatformObjectID: "c1_" + suffix,
		StatDate: statDate, Caliber: caliber,
		Counts:    insights.MetricCounts{Impressions: 20000, Clicks: 400, Conversions: 8, SpendCents: 100_00},
		Raw:       map[string]any{"平台原字段": "show_cnt"},
		CreatedAt: now, UpdatedAt: now,
	}
	unmatched := fact
	unmatched.ID = "insightmetric_it_2_" + suffix
	unmatched.PlatformObjectID = "c2_" + suffix
	unmatched.Counts = insights.MetricCounts{Impressions: 5000, Clicks: 50, SpendCents: 40_00}
	if written, err := repository.UpsertMetricFacts(ctx, []insights.MetricFact{fact, unmatched}); err != nil || written != 2 {
		t.Fatalf("written=%d err=%v", written, err)
	}

	// doc10 §7：同一批重跑不产生重复记录，晚归因覆盖旧值。
	corrected := fact
	corrected.ID = "insightmetric_it_3_" + suffix
	corrected.Counts.Conversions = 11
	if _, err := repository.UpsertMetricFacts(ctx, []insights.MetricFact{corrected}); err != nil {
		t.Fatal(err)
	}

	assetID := "insightasset_it_ingest_" + suffix
	if _, err := repository.CreateAsset(ctx, insights.Asset{
		ID: assetID, OrganizationID: organizationID, ProjectID: projectID,
		LineageID: assetID, Revision: 1, Title: "集成测试前贴片",
		SourceKind: insights.AssetSourceUpload, AssetType: insights.AssetTypePrerollAd,
		AssetTypeSource: insights.SourceHuman, AnalysisStatus: insights.AnalysisAnalysable,
		AnalysisStatusChangedAt: &now, Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateAssetMapping(ctx, insights.AssetMapping{
		ID: "insightmapping_it_" + suffix, OrganizationID: organizationID, ProjectID: projectID,
		Platform: "douyin", PlatformObjectKind: "creative", PlatformObjectID: "c1_" + suffix,
		PlatformObjectName: "夏季前贴", AssetID: assetID, Status: insights.MappingMatched,
		MatchSource: "human", MatchedBy: userID, MatchedAt: &now,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	facts, err := repository.ListMetricFacts(ctx, organizationID, projectID, insights.MetricWindow{
		Start: statDate.AddDate(0, 0, -3), End: statDate.AddDate(0, 0, 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("重跑不应产生第三行：%d 行", len(facts))
	}
	byObject := map[string]insights.MetricFactWithMapping{}
	for _, value := range facts {
		byObject[value.PlatformObjectID] = value
	}
	matched := byObject["c1_"+suffix]
	if matched.AssetID != assetID || matched.AssetTitle != "集成测试前贴片" || matched.Counts.Conversions != 11 {
		t.Fatalf("匹配上的行应当带素材且转化被覆盖：%#v", matched)
	}
	// doc10 §5：未匹配的对象照样要出现在结果里，它的花费仍计入总盘。
	orphan := byObject["c2_"+suffix]
	if orphan.AssetID != "" || orphan.MappingStatus != insights.MappingUnmatched || orphan.Counts.SpendCents != 40_00 {
		t.Fatalf("未匹配的行不该消失：%#v", orphan)
	}
	if !matched.StatDate.Equal(statDate) {
		t.Fatalf("数据日期读回来对不上：%s", matched.StatDate)
	}

	finished := batch
	finished.Status = insights.ImportPartial
	finished.AcceptedRows = 2
	finished.RejectedRows = 1
	finished.ErrorSummary = "1 行未通过校验，其余已写入"
	finished.Errors = []string{"第 3 行：点击数大于展示数，口径有误"}
	finished.FinishedAt = &now
	finished.UpdatedAt = now
	if _, err := repository.FinishImportBatch(ctx, finished, batch.Version); err != nil {
		t.Fatal(err)
	}
	batches, err := repository.ListImportBatches(ctx, organizationID, projectID, insights.ImportBatchFilter{
		DataSourceID: source.ID, Limit: 10,
	})
	if err != nil || len(batches) != 1 {
		t.Fatalf("batches=%d err=%v", len(batches), err)
	}
	if batches[0].Status != insights.ImportPartial || len(batches[0].Errors) != 1 ||
		batches[0].WindowStart == nil || batches[0].WindowStart.Format("2006-01-02") != "2026-07-20" {
		t.Fatalf("批次读回来不完整：%#v", batches[0])
	}
}

// TestReportDraftSlotAgainstMySQL 盯的是「复盘提交之后，同一个窗口还能不能再记一笔」。
//
// 这件事只有真 DDL 能验：内存仓库不带唯一键，怎么写都过。而唯一键正是那条死路的
// 成因——已确认的报告如果还占着 (项目 + 执行 + 窗口)，PinFinding 找不到草稿又建不出
// 草稿，重试三次后只会抛一个刷新也没用的版本冲突（PRD §15.3 要的是开下一份草稿）。
func TestReportDraftSlotAgainstMySQL(t *testing.T) {
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
	organizationID := contract.OrganizationID("org_insights_slot_" + suffix)
	projectID := contract.ProjectID("project_insights_slot_" + suffix)
	userID := "user_insights_slot_" + suffix
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         []contract.Scope{"project.read", "project.write"},
	}
	t.Cleanup(func() { cleanupInsightsIntegration(t, db, organizationID, userID) })
	if err := (identity.MySQLStore{DB: db}).EnsureLocalActor(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if err := (project.MySQLStore{DB: db}).EnsureLocalProject(ctx, actor, projectID); err != nil {
		t.Fatal(err)
	}

	repository := insights.MySQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	// 记一笔建出来的草稿：没有投放执行，只有窗口。
	draft := func(id string) insights.InsightReport {
		return insights.InsightReport{
			ID: id, OrganizationID: organizationID, ProjectID: projectID,
			Status: insights.ReportDraft, Findings: []string{},
			Digest:      []insights.ReportFinding{{Kind: "pinned", Text: "开场三秒", Origin: insights.OriginPinned}},
			WindowStart: "2026-07-01", WindowEnd: "2026-07-31",
			Version: 1, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
		}
	}

	first, err := repository.CreateReport(ctx, draft("insightreport_slot_1_"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	// 同一个窗口第二份草稿仍然要被挡住：并发的两次记一笔各建一份的话，
	// 人记的东西会分散在两份草稿里，而屏幕上只看得见一份。
	if _, err := repository.CreateReport(ctx, draft("insightreport_slot_2_"+suffix)); !errors.Is(err, insights.ErrInvalidState) {
		t.Fatalf("同窗口第二份草稿应该被唯一键挡住，实际 err=%v", err)
	}

	// 提交，且不挂投放执行——执行是选填的，这正是最常见的一种提交。
	submitted, err := repository.SubmitReport(ctx, insights.SubmitReportInput{
		OrganizationID: organizationID, ProjectID: projectID, ReportID: first.ID,
		ExpectedVersion: first.Version, ExecutionID: "", Summary: "七月这一轮",
		Digest: first.Digest, ActorID: userID, At: now,
	})
	if err != nil || submitted.Status != insights.ReportConfirmed {
		t.Fatalf("提交复盘 =%#v err=%v", submitted, err)
	}

	// 提交之后再记一笔：已确认的不算草稿，所以要能建出下一份草稿来。
	if _, err := repository.FindDraftByWindow(ctx, organizationID, projectID, "2026-07-01", "2026-07-31"); !errors.Is(err, insights.ErrNotFound) {
		t.Fatalf("已确认的报告不该被当成草稿，err=%v", err)
	}
	next, err := repository.CreateReport(ctx, draft("insightreport_slot_3_"+suffix))
	if err != nil {
		t.Fatalf("提交之后同窗口再记一笔应该开出新草稿，实际 err=%v", err)
	}
	found, err := repository.FindDraftByWindow(ctx, organizationID, projectID, "2026-07-01", "2026-07-31")
	if err != nil || found.ID != next.ID {
		t.Fatalf("再记一笔应该落在新草稿上：found=%s err=%v", found.ID, err)
	}
	// 老的那份还在，定格没被动过。
	frozen, err := repository.GetReport(ctx, organizationID, projectID, first.ID)
	if err != nil || frozen.Status != insights.ReportConfirmed || frozen.Summary != "七月这一轮" {
		t.Fatalf("已提交的那份被动过了：%#v err=%v", frozen, err)
	}
}
