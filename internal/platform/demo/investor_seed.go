package demo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"time"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/project"
)

const (
	InvestorDemoProjectID = contract.ProjectID("project_investor_precision_evidence")
	investorDemoBrandID   = contract.BrandID("brand_baiyu_precision")
	investorDemoProductID = contract.ProductID("product_precision_cnc_parts")
	investorDemoActor     = "demo-seeder"
)

var investorDemoProject = project.CanonicalDemoProject{
	ProjectID:   InvestorDemoProjectID,
	BrandID:     investorDemoBrandID,
	ProductID:   investorDemoProductID,
	Name:        "投资人路演：精度证据增长",
	Industry:    project.IndustryAutomotiveBrand,
	BrandName:   "白域精工",
	ProductName: "高精度 CNC 加工零部件",
}

type InvestorDemoProjectStore interface {
	EnsureCanonicalDemoProject(context.Context, contract.ActorContext, project.CanonicalDemoProject) (project.Project, error)
	UpsertProjectRuntime(context.Context, contract.OrganizationID, contract.ProjectID, project.ProjectRuntime) error
	UpsertWorkbench(context.Context, project.Workbench) error
	CreateBusinessTask(context.Context, project.BusinessTask) error
	ListBusinessTasks(context.Context, contract.OrganizationID, contract.ProjectID) ([]project.BusinessTask, error)
	UpdateBusinessTask(context.Context, project.BusinessTask) error
	CreateOperationalRecord(context.Context, project.OperationalRecord) error
	ListOperationalRecords(context.Context, contract.OrganizationID, contract.ProjectID) ([]project.OperationalRecord, error)
	UpdateOperationalRecord(context.Context, project.OperationalRecord) error
	DeleteOperationalRecord(context.Context, contract.OrganizationID, contract.ProjectID, string) error
	CreateChangeSet(context.Context, project.ChangeSet) error
	ListChangeSets(context.Context, contract.OrganizationID, contract.ProjectID) ([]project.ChangeSet, error)
	UpdateChangeSet(context.Context, project.ChangeSet) error
	AppendAuditEvent(context.Context, project.AuditEvent) error
	ListAuditEvents(context.Context, contract.OrganizationID, contract.ProjectID) ([]project.AuditEvent, error)
}

type InvestorDemoAssetStore interface {
	EnsureSeedAsset(context.Context, assets.SeedAsset, time.Time) (contract.ProjectAssetRef, error)
}

type InvestorDemoSeedResult struct {
	ProjectID             contract.ProjectID
	ProjectContextVersion int64
	AssetRefs             map[string]contract.ProjectAssetRef
	TaskCount             int
	RecordCount           int
}

func EnsureCanonicalInvestorDemo(ctx context.Context, actor contract.ActorContext, projects InvestorDemoProjectStore, assetStore InvestorDemoAssetStore) (InvestorDemoSeedResult, error) {
	if projects == nil || assetStore == nil {
		return InvestorDemoSeedResult{}, fmt.Errorf("canonical investor demo seed dependencies are required")
	}
	if err := actor.Validate(); err != nil {
		return InvestorDemoSeedResult{}, err
	}
	seededProject, err := projects.EnsureCanonicalDemoProject(ctx, actor, investorDemoProject)
	if err != nil {
		return InvestorDemoSeedResult{}, err
	}
	now := time.Now().UTC()
	assetRefs, err := ensureInvestorDemoAssets(ctx, actor.OrganizationID, seededProject.ID, seededProject.ProjectContextVersion, assetStore, now)
	if err != nil {
		return InvestorDemoSeedResult{}, err
	}
	if err := ensureInvestorDemoTasks(ctx, actor.OrganizationID, seededProject.ID, projects, assetRefs, now); err != nil {
		return InvestorDemoSeedResult{}, err
	}
	if err := ensureInvestorDemoWorkbench(ctx, actor.OrganizationID, seededProject.ID, projects, assetRefs, now); err != nil {
		return InvestorDemoSeedResult{}, err
	}
	if err := ensureInvestorDemoOperations(ctx, actor.OrganizationID, seededProject.ID, projects, now); err != nil {
		return InvestorDemoSeedResult{}, err
	}
	if err := ensureInvestorDemoChangeSet(ctx, actor.OrganizationID, seededProject.ID, projects, assetRefs, now); err != nil {
		return InvestorDemoSeedResult{}, err
	}
	tasks, err := projects.ListBusinessTasks(ctx, actor.OrganizationID, seededProject.ID)
	if err != nil {
		return InvestorDemoSeedResult{}, err
	}
	records, err := projects.ListOperationalRecords(ctx, actor.OrganizationID, seededProject.ID)
	if err != nil {
		return InvestorDemoSeedResult{}, err
	}
	return InvestorDemoSeedResult{ProjectID: seededProject.ID, ProjectContextVersion: seededProject.ProjectContextVersion, AssetRefs: assetRefs, TaskCount: len(tasks), RecordCount: len(records)}, nil
}

func ensureInvestorDemoWorkbench(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, store InvestorDemoProjectStore, refs map[string]contract.ProjectAssetRef, now time.Time) error {
	runtime := project.ProjectRuntime{Code: "SP", Brand: investorDemoProject.BrandName, Product: investorDemoProject.ProductName, Goal: "以精度证据驱动高意向销售线索增长", Stage: "投放审批", Progress: 82, Status: "active", Owner: "Noah Xu", Budget: 1000000, Currency: "CNY", Timezone: "Asia/Shanghai", KnowledgeCount: 6, UpdatedAt: now}
	if err := store.UpsertProjectRuntime(ctx, organizationID, projectID, runtime); err != nil {
		return fmt.Errorf("upsert demo project runtime: %w", err)
	}
	assetID := string(refs["creative_video"].AssetVersion.AssetID)
	version := 1
	completedAt := now
	workbench := project.Workbench{
		Organization:          project.WorkbenchOrganization{ID: string(organizationID), Code: "COOKIES-DEMO", Name: "Cookies 本地演示组织", Owner: "Local Admin", Currency: "CNY", Timezone: "Asia/Shanghai", UpdatedAt: now},
		Client:                project.WorkbenchClient{ID: "client-demo-precision", OrganizationID: string(organizationID), Code: "PRECISION-DEMO", Name: "白域精工（演示）", Industry: "汽车品牌", Owner: "Noah Xu", HealthStatus: "healthy", UpdatedAt: now},
		Brand:                 project.WorkbenchBrand{ID: "brand-demo-precision", OrganizationID: string(organizationID), ClientID: "client-demo-precision", Code: "PRECISION-DEMO", Name: investorDemoProject.BrandName, Category: "汽车零部件", ProductLines: []string{investorDemoProject.ProductName}, Owner: "Noah Xu", GuidelineStatus: "ready", UpdatedAt: now},
		Project:               project.WorkbenchProject{ProjectID: string(projectID), OrganizationID: string(organizationID), ClientID: "client-demo-precision", BrandID: "brand-demo-precision", Stage: "delivery", StageLabel: "投放预检", StagePercent: 70, TaskPercent: 82, RiskStatus: "watch", Blocker: "仅演示本地模拟预检；真实投放仍需人工审批。", UpdatedAt: now},
		AdAccountBindings:     []project.WorkbenchAdAccountBinding{{ID: "account-demo-precision", OrganizationID: string(organizationID), ClientID: "client-demo-precision", BrandID: "brand-demo-precision", Platform: "巨量引擎", AccountName: "本地模拟广告账户", AccountDisplayID: "JLY-LOCAL-PRECISION-001", Currency: "CNY", Timezone: "Asia/Shanghai", PermissionStatus: "normal", LoginStatus: "normal", TrackingStatus: "normal", Owner: "Noah Xu", BoundAssetIDs: []string{assetID}, LastSyncedAt: now}},
		QualityCheckRuns:      []project.WorkbenchQualityCheckRun{{ID: "qc-demo-precision-video-v1", OrganizationID: string(organizationID), ProjectID: string(projectID), AssetID: assetID, AssetVersion: 1, Status: "passed", Model: "demo-quality-vision-v1", RuleVersion: "agency-material-rules-2026-07", PromptVersion: "material-check-2026-07-15", Summary: "视频素材质检记录", Issues: []project.WorkbenchQualityCheckIssue{}, CreatedAt: now, CompletedAt: &completedAt}},
		MaterialConfirmations: []project.WorkbenchMaterialConfirmation{{ID: "confirm-demo-precision-video-v1", OrganizationID: string(organizationID), ProjectID: string(projectID), QualityCheckRunID: "qc-demo-precision-video-v1", AssetID: assetID, AssetVersion: 1, Status: "confirmed", Scope: investorDemoProject.Name, ConfirmedBy: "Noah Xu", Note: "仅用于本地演示预检；不连接或写入任何真实广告平台。", CreatedAt: now}},
		AssetVersionPointers:  []project.WorkbenchAssetVersionPointer{{ID: "pointer-demo-precision-video", OrganizationID: string(organizationID), ProjectID: string(projectID), AssetID: assetID, WorkingVersion: 1, QualityCheckedVersion: &version, HumanConfirmedVersion: &version, DeliveryVersion: &version, Versions: []project.WorkbenchAssetVersion{{Version: 1, CreatedBy: "demo-seeder", SourceTaskID: "task_demo_imported_brief_to_video", SourceType: "model_generation", SourceLabel: "导入 Brief 到视频演示", CreatedAt: now, ChangeSummary: "视频交付版本指针"}}, Authorization: project.WorkbenchAssetAuthorization{Platforms: []string{"巨量引擎"}, Regions: []string{"中国大陆"}, RightsHolder: investorDemoProject.BrandName, ExpiresAt: time.Date(2026, 12, 31, 15, 59, 59, 0, time.UTC), Note: "本地演示授权范围，仅用于模拟预检，不代表真实媒体投放授权。"}, DeliveryTarget: project.WorkbenchDeliveryTarget{Platform: "巨量引擎", Region: "中国大陆"}, Owner: "Noah Xu", UpdatedAt: now}},
	}
	if err := store.UpsertWorkbench(ctx, workbench); err != nil {
		return fmt.Errorf("upsert demo project workbench: %w", err)
	}
	for _, legacyID := range []string{"RUNTIME-2607-01", "DEMO-WORKBENCH-PROFILE-01", "DEMO-LINK-ACCOUNT-01", "DEMO-LINK-QC-01", "DEMO-LINK-CONFIRM-01", "DEMO-LINK-POINTER-01"} {
		if err := store.DeleteOperationalRecord(ctx, organizationID, projectID, legacyID); err != nil {
			return fmt.Errorf("remove legacy demo workbench record %s: %w", legacyID, err)
		}
	}
	return nil
}

func ensureInvestorDemoAssets(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, contextVersion int64, store InvestorDemoAssetStore, now time.Time) (map[string]contract.ProjectAssetRef, error) {
	result := make(map[string]contract.ProjectAssetRef, len(investorDemoAssets))
	for _, seed := range investorDemoAssets {
		ref, err := store.EnsureSeedAsset(ctx, assets.SeedAsset{
			OrganizationID:        organizationID,
			ProjectID:             projectID,
			AssetID:               contract.AssetID(seed.id),
			BlobID:                "blob_" + seed.id,
			Kind:                  seed.kind,
			SourceType:            seed.sourceType,
			MIMEType:              seed.mimeType,
			SizeBytes:             seed.sizeBytes,
			SHA256:                stableSHA256(seed.content),
			WidthPixels:           seed.width,
			HeightPixels:          seed.height,
			Media:                 seed.media,
			ProviderJobID:         seed.providerJobID,
			ProviderOutputID:      seed.providerOutputID,
			ProjectContextVersion: contextVersion,
			Location:              assets.ObjectLocation{Provider: "tos", Bucket: "cookies-assets", Key: "demo/investor/" + seed.objectKey},
		}, now)
		if err != nil {
			return nil, fmt.Errorf("ensure seed asset %s: %w", seed.key, err)
		}
		result[seed.key] = ref
	}
	return result, nil
}

func ensureInvestorDemoTasks(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, store InvestorDemoProjectStore, refs map[string]contract.ProjectAssetRef, now time.Time) error {
	existing, err := store.ListBusinessTasks(ctx, organizationID, projectID)
	if err != nil {
		return err
	}
	byID := make(map[string]project.BusinessTask, len(existing))
	for _, task := range existing {
		byID[task.ID] = task
	}
	taskIDs := map[project.BusinessTaskType]string{}
	for _, seed := range investorDemoTasks {
		taskIDs[seed.taskType] = seed.id
	}
	for _, seed := range investorDemoTasks {
		sourceTasks := make([]string, 0, len(seed.sourceTaskTypes))
		for _, taskType := range seed.sourceTaskTypes {
			sourceTasks = append(sourceTasks, taskIDs[taskType])
		}
		desired := project.BusinessTask{
			ID:                seed.id,
			OrganizationID:    organizationID,
			ProjectID:         projectID,
			Type:              seed.taskType,
			Name:              seed.name,
			Objective:         seed.objective,
			Status:            seed.status,
			SourceTaskIDs:     sourceTasks,
			SourceArtifactIDs: assetRefIDs(refs, seed.sourceAssets),
			OutputArtifactIDs: assetRefIDs(refs, seed.outputAssets),
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		current, ok := byID[desired.ID]
		if !ok {
			if err := store.CreateBusinessTask(ctx, desired); err != nil {
				return fmt.Errorf("create demo task %s: %w", desired.ID, err)
			}
			continue
		}
		if taskNeedsUpdate(current, desired) {
			desired.Version = current.Version
			desired.CreatedAt = current.CreatedAt
			if desired.CreatedAt.IsZero() {
				desired.CreatedAt = now
			}
			if err := store.UpdateBusinessTask(ctx, desired); err != nil {
				return fmt.Errorf("update demo task %s: %w", desired.ID, err)
			}
		}
	}
	return nil
}

func ensureInvestorDemoOperations(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, store InvestorDemoProjectStore, now time.Time) error {
	existing, err := store.ListOperationalRecords(ctx, organizationID, projectID)
	if err != nil {
		return err
	}
	byID := make(map[string]project.OperationalRecord, len(existing))
	for _, record := range existing {
		byID[record.ID] = record
	}
	for _, seed := range investorDemoOperations(projectID) {
		desired := project.OperationalRecord{
			ID:             seed.id,
			OrganizationID: organizationID,
			ProjectID:      projectID,
			Kind:           seed.kind,
			Title:          seed.title,
			Status:         seed.status,
			OccurredAt:     mustParseTime(seed.occurredAt),
			Fields:         seed.fields,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		current, ok := byID[desired.ID]
		if !ok {
			if err := store.CreateOperationalRecord(ctx, desired); err != nil {
				return fmt.Errorf("create demo operation %s: %w", desired.ID, err)
			}
			continue
		}
		if operationNeedsUpdate(current, desired) {
			desired.CreatedAt = current.CreatedAt
			if desired.CreatedAt.IsZero() {
				desired.CreatedAt = now
			}
			if err := store.UpdateOperationalRecord(ctx, desired); err != nil {
				return fmt.Errorf("update demo operation %s: %w", desired.ID, err)
			}
		}
	}
	return nil
}

func ensureInvestorDemoChangeSet(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, store InvestorDemoProjectStore, refs map[string]contract.ProjectAssetRef, now time.Time) error {
	budget := 8600.0
	desired := project.ChangeSet{
		ID:             "changeset_demo_precision_evidence_budget",
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           "精度证据创意与探索预算模拟",
		Status:         project.ChangeSetPreflightPassed,
		ArtifactRefs:   []contract.ProjectAssetRef{refs["brief"], refs["creative_image"], refs["creative_video"], refs["delivery"]},
		BudgetLimit:    &budget,
		Preflight: &project.ChangeSetPreflight{
			Passed: true,
			Checks: []project.PreflightCheck{
				{Code: "budget_boundary", Passed: true, Message: "预算 8600 CNY 在路演模拟边界内", Repair: ""},
				{Code: "asset_refs_stable", Passed: true, Message: "Brief、图文、视频和投放说明均使用稳定 ProjectAssetRef", Repair: ""},
				{Code: "manual_approval_required", Passed: true, Message: "真实投放前仍需人工审批，本 seed 仅保留模拟证据", Repair: ""},
			},
			CheckedAt: mustParseTime("2026-07-22T09:06:00.000Z"),
		},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	existing, err := store.ListChangeSets(ctx, organizationID, projectID)
	if err != nil {
		return err
	}
	var current *project.ChangeSet
	for index := range existing {
		if existing[index].ID == desired.ID {
			current = &existing[index]
			break
		}
	}
	if current == nil {
		if err := store.CreateChangeSet(ctx, desired); err != nil {
			return fmt.Errorf("create demo change set: %w", err)
		}
	} else if changeSetNeedsUpdate(*current, desired) {
		desired.Version = current.Version
		desired.CreatedAt = current.CreatedAt
		if desired.CreatedAt.IsZero() {
			desired.CreatedAt = now
		}
		if err := store.UpdateChangeSet(ctx, desired); err != nil {
			return fmt.Errorf("update demo change set: %w", err)
		}
	}
	return ensureInvestorDemoAudit(ctx, organizationID, projectID, store, now)
}

func ensureInvestorDemoAudit(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, store InvestorDemoProjectStore, now time.Time) error {
	existing, err := store.ListAuditEvents(ctx, organizationID, projectID)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, event := range existing {
		seen[event.ID] = struct{}{}
	}
	events := []project.AuditEvent{
		{
			ID:             "audit_demo_seed_verified",
			OrganizationID: organizationID,
			ProjectID:      projectID,
			Actor:          investorDemoActor,
			Action:         "demo.seed_verified",
			EntityType:     project.AuditEntityProject,
			EntityID:       string(projectID),
			Metadata:       map[string]any{"source": "go-cookies-api", "project_identity": investorDemoProject.Name},
			CreatedAt:      now,
		},
		{
			ID:             "audit_demo_changeset_preflight",
			OrganizationID: organizationID,
			ProjectID:      projectID,
			Actor:          investorDemoActor,
			Action:         "change_set.preflight_passed",
			EntityType:     project.AuditEntityChangeSet,
			EntityID:       "changeset_demo_precision_evidence_budget",
			Metadata:       map[string]any{"simulated": true, "approval_required": true},
			CreatedAt:      now.Add(time.Microsecond),
		},
	}
	for _, event := range events {
		if _, ok := seen[event.ID]; ok {
			continue
		}
		if err := store.AppendAuditEvent(ctx, event); err != nil {
			return fmt.Errorf("append demo audit event %s: %w", event.ID, err)
		}
	}
	return nil
}

func assetRefIDs(refs map[string]contract.ProjectAssetRef, keys []string) []string {
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		ref := refs[key]
		result = append(result, fmt.Sprintf("%s@%d", ref.AssetVersion.AssetID, ref.AssetVersion.Version))
	}
	return result
}

func taskNeedsUpdate(current, desired project.BusinessTask) bool {
	return current.Type != desired.Type || current.Name != desired.Name || current.Objective != desired.Objective ||
		current.Status != desired.Status || !reflect.DeepEqual(current.SourceTaskIDs, desired.SourceTaskIDs) ||
		!reflect.DeepEqual(current.SourceArtifactIDs, desired.SourceArtifactIDs) ||
		!reflect.DeepEqual(current.OutputArtifactIDs, desired.OutputArtifactIDs)
}

func operationNeedsUpdate(current, desired project.OperationalRecord) bool {
	return current.Kind != desired.Kind || current.Title != desired.Title || current.Status != desired.Status ||
		!current.OccurredAt.Equal(desired.OccurredAt) || !reflect.DeepEqual(current.Fields, desired.Fields)
}

func changeSetNeedsUpdate(current, desired project.ChangeSet) bool {
	return current.Name != desired.Name || current.Status != desired.Status || !reflect.DeepEqual(current.ArtifactRefs, desired.ArtifactRefs) ||
		!sameFloatPtr(current.BudgetLimit, desired.BudgetLimit) || !reflect.DeepEqual(current.Preflight, desired.Preflight)
}

func sameFloatPtr(left, right *float64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func stableSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func mustParseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

type demoAssetSeed struct {
	key              string
	id               string
	objectKey        string
	kind             contract.AssetKind
	sourceType       contract.AssetSourceType
	mimeType         string
	sizeBytes        int64
	width            int
	height           int
	media            assets.MediaMetadata
	providerJobID    string
	providerOutputID string
	content          string
}

var investorDemoAssets = []demoAssetSeed{
	{
		key: "brief", id: "asset_demo_investor_brief", objectKey: "brief.txt", kind: contract.AssetDocument,
		sourceType: contract.AssetSourceImported, mimeType: "text/plain", sizeBytes: 166,
		content: "已确认 Brief：以 ±0.01mm 精度、98%+ 准时交付和真实制造场景为核心证据，面向采购与研发负责人获取销售线索。",
	},
	{
		key: "creative_image", id: "asset_demo_investor_creative_image", objectKey: "creative-image.png", kind: contract.AssetImage,
		sourceType: contract.AssetSourceProviderGenerated, mimeType: "image/png", sizeBytes: 245760, width: 1280, height: 720,
		providerJobID: "providerjob_demo_investor_image", providerOutputID: "output_demo_investor_image",
		content: "预置 AI 图文创意：高精度 CNC 加工画面，展示精度与交期证据；仅作路演资产，不代表真实广告素材。",
	},
	{
		key: "creative_video", id: "asset_demo_investor_creative_video", objectKey: "creative-video.mp4", kind: contract.AssetVideo,
		// 宽高得有。真实上传走 ffprobe，量不到宽高会被判成无效视频
		// （video_probe.go 的 Validate），所以「探测成功但没有宽高」是真机上
		// 出不来的状态。缺了它，洞察那边的客观可测层只能量出时长，画幅永远空着。
		sourceType: contract.AssetSourceProviderGenerated, mimeType: "video/mp4", sizeBytes: 15728640, width: 1920, height: 1080,
		media:         assets.MediaMetadata{DurationSeconds: 15, FPS: 30, Codec: "h264", BitrateBPS: 8400000, AudioCodec: "aac", AudioChannels: 2, AudioSampleRate: 48000, ProbeStatus: assets.MediaProbeSucceeded},
		providerJobID: "providerjob_demo_investor_video", providerOutputID: "output_demo_investor_video",
		content: "预置 AI 视频创意：15 秒精密制造品牌片，按问题、证据、交付承诺和 CTA 串联完整路演故事线。",
	},
	{
		key: "strategy", id: "asset_demo_investor_strategy", objectKey: "strategy.txt", kind: contract.AssetDocument,
		sourceType: contract.AssetSourceImported, mimeType: "text/plain", sizeBytes: 184,
		content: "[strategy] 精度证据增长策略：以研发负责人和采购负责人为核心受众，先用公差、良率和交期证据建立信任，再用真实制造场景引导销售线索。",
	},
	{
		key: "insight", id: "asset_demo_investor_insight", objectKey: "insight.txt", kind: contract.AssetDocument,
		sourceType: contract.AssetSourceImported, mimeType: "text/plain", sizeBytes: 166,
		content: "[insight] 素材洞察报告：证据前置版本在 48 个素材样本中表现稳定，研发负责人素材 CTR 与线索质量均优于纯产品特写对照组。",
	},
	{
		key: "delivery", id: "asset_demo_investor_delivery", objectKey: "delivery.txt", kind: contract.AssetDocument,
		sourceType: contract.AssetSourceImported, mimeType: "text/plain", sizeBytes: 170,
		content: "[delivery] 投放模拟计划：保留人工审批、预算边界、重复广告清理和回滚入口；默认只执行本地模拟，不连接真实广告平台。",
	},
}

type demoTaskSeed struct {
	id              string
	taskType        project.BusinessTaskType
	name            string
	objective       string
	status          project.BusinessTaskStatus
	sourceTaskTypes []project.BusinessTaskType
	sourceAssets    []string
	outputAssets    []string
}

var investorDemoTasks = []demoTaskSeed{
	{
		id: "task_demo_precision_strategy", taskType: project.BusinessTaskStrategy, name: "精度证据增长策略评审",
		objective: "整合行业访谈、历史素材表现和公开洞察样本，形成可向投资人复演的策略 Brief 与证据链。",
		status:    project.BusinessTaskReady, sourceAssets: []string{"brief", "insight"}, outputAssets: []string{"strategy"},
	},
	{
		id: "task_demo_precision_creative", taskType: project.BusinessTaskCreative, name: "精密制造主创意组合",
		objective: "基于已确认策略制作图文与视频主创意，突出 ±0.01mm 精度、交付稳定性和真实制造画面。",
		status:    project.BusinessTaskInProgress, sourceTaskTypes: []project.BusinessTaskType{project.BusinessTaskStrategy},
		sourceAssets: []string{"brief", "strategy", "insight"}, outputAssets: []string{"creative_image", "creative_video"},
	},
	{
		id: "task_demo_precision_preroll", taskType: project.BusinessTaskShortDramaPreroll, name: "短剧前贴：交期冲突钩子",
		objective: "用交付延误冲突切入，生成可人工选择的短剧前贴候选并保留候选证据。",
		status:    project.BusinessTaskReady, sourceTaskTypes: []project.BusinessTaskType{project.BusinessTaskStrategy, project.BusinessTaskCreative},
		sourceAssets: []string{"brief", "strategy", "insight"}, outputAssets: []string{"creative_video"},
	},
	{
		id: "task_demo_precision_remake", taskType: project.BusinessTaskViralRemake, name: "高表现结构复刻验证",
		objective: "拆解公开样本的钩子、证明与 CTA 结构，生成白域精工可用的原创复刻草案。",
		status:    project.BusinessTaskDraft, sourceTaskTypes: []project.BusinessTaskType{project.BusinessTaskStrategy, project.BusinessTaskCreative},
		sourceAssets: []string{"brief", "strategy", "insight"}, outputAssets: []string{"creative_video"},
	},
	{
		id: "task_demo_precision_video_edit", taskType: project.BusinessTaskVideoEdit, name: "品牌片包装与交付版本",
		objective: "把主视频、字幕、CTA 和审计说明整理成可进入投放模拟审批的交付版本。",
		status:    project.BusinessTaskInProgress, sourceTaskTypes: []project.BusinessTaskType{project.BusinessTaskStrategy, project.BusinessTaskCreative},
		sourceAssets: []string{"brief", "strategy", "creative_image", "creative_video"}, outputAssets: []string{"delivery"},
	},
}

type demoOperationSeed struct {
	id         string
	kind       project.OperationalRecordKind
	title      string
	status     string
	occurredAt string
	fields     map[string]any
}

func investorDemoOperations(projectID contract.ProjectID) []demoOperationSeed {
	return []demoOperationSeed{
		record("WORK-2607-01", project.OperationalRecordWorkItem, "春季新品上市", "待评审", "2026-07-22T08:30:00.000Z", map[string]any{"type": "策略工作区", "owner": "Amelia Meng", "progress": 82}),
		record("WORK-2607-02", project.OperationalRecordWorkItem, "精密制造品牌片", "生成中", "2026-07-22T05:48:00.000Z", map[string]any{"type": "创意任务", "owner": "Lin Wei", "progress": 68}),
		record("WORK-2607-03", project.OperationalRecordWorkItem, "证据前置实验分析", "需处理", "2026-07-22T07:06:00.000Z", map[string]any{"type": "分析任务", "owner": "Sofia Chen", "progress": 86}),
		record("WORK-2607-04", project.OperationalRecordWorkItem, "销售线索增长计划 06", "待审批", "2026-07-22T08:30:00.000Z", map[string]any{"type": "投放计划", "owner": "Noah Xu", "progress": 82}),
		record("WORK-2607-05", project.OperationalRecordWorkItem, "华东行业受众研究", "已完成", "2026-07-21T10:40:00.000Z", map[string]any{"type": "研究任务", "owner": "Amelia Meng", "progress": 100}),
		record("EV-021", project.OperationalRecordEvidence, "精密制造行业受众研究", "已确认", "2026-07-20T00:00:00.000Z", map[string]any{"source": "项目研究库", "confidence": "高"}),
		record("EV-024", project.OperationalRecordEvidence, "白域精工近 90 天素材表现", "已确认", "2026-07-22T00:00:00.000Z", map[string]any{"source": "广告数据 Connector", "confidence": "高"}),
		record("INS-014", project.OperationalRecordEvidence, "证据前置创意实验结论", "已确认", "2026-07-22T00:00:00.000Z", map[string]any{"source": "洞察与经验", "confidence": "中"}),
		record("EV-027", project.OperationalRecordEvidence, "10 位采购与研发负责人访谈", "已确认", "2026-07-19T00:00:00.000Z", map[string]any{"source": "飞书文档", "confidence": "中"}),
		record("ACT-2607-01", project.OperationalRecordActivity, "策略 v1.2 已提交评审", "completed", "2026-07-22T02:24:00.000Z", map[string]any{"actor": "Amelia Meng", "detail": "引用 5 条证据"}),
		record("ACT-2607-02", project.OperationalRecordActivity, "创意方向 CR-103 已确认", "completed", "2026-07-22T01:48:00.000Z", map[string]any{"actor": "Lin Wei", "detail": "进入视频制作"}),
		record("ACT-2607-03", project.OperationalRecordActivity, "投放计划完成预算校验", "completed", "2026-07-22T01:12:00.000Z", map[string]any{"actor": "系统", "detail": "无阻断风险"}),
		record("METRIC-2607-01", project.OperationalRecordMetric, "证据前置创意表现趋势", "ready", "2026-07-22T08:30:00.000Z", map[string]any{"points": "32,39,36,49,54,51,63,67,72,78,75,86", "unit": "index", "summary": "证据前置版本，正在形成稳定增量。", "latest": "86%", "comparison": "较基线 +18%", "sample": "48 个有效素材版本"}),
		record("METRIC-2607-02", project.OperationalRecordMetric, "线索质量与表单完成趋势", "ready", "2026-07-22T09:00:00.000Z", map[string]any{"points": "41,44,47,52,58,61,66,69,73,76,80,84", "unit": "score", "summary": "高意向表单占比持续提升，研发负责人样本贡献最高。", "latest": "84", "comparison": "较基线 +21%", "sample": "312 条模拟线索"}),
		record("AD-2607-031", project.OperationalRecordPerformanceAd, "精度证据·研发负责人", "持续放量", "2026-07-22T08:30:00.000Z", map[string]any{"platform": "巨量引擎", "format": "视频", "spend": 28640, "impressions": 682400, "ctr": 4.18, "cpa": 54.2}),
		record("AD-2607-028", project.OperationalRecordPerformanceAd, "真实制造场景·采购线", "稳定", "2026-07-21T08:30:00.000Z", map[string]any{"platform": "腾讯广告", "format": "图文", "spend": 21800, "impressions": 486200, "ctr": 3.74, "cpa": 61.8}),
		record("AD-2607-019", project.OperationalRecordPerformanceAd, "短剧前贴·交期冲突", "优先扩量", "2026-07-18T08:30:00.000Z", map[string]any{"platform": "巨量引擎", "format": "视频", "spend": 18420, "impressions": 438900, "ctr": 4.62, "cpa": 49.6}),
		record("AD-2607-014", project.OperationalRecordPerformanceAd, "游戏前贴·精度挑战", "观察", "2026-07-14T08:30:00.000Z", map[string]any{"platform": "快手磁力", "format": "视频", "spend": 15680, "impressions": 326800, "ctr": 3.26, "cpa": 68.4}),
		record("AD-2607-008", project.OperationalRecordPerformanceAd, "纯产品特写·对照组", "建议降量", "2026-07-04T08:30:00.000Z", map[string]any{"platform": "腾讯广告", "format": "图文", "spend": 13320, "impressions": 312100, "ctr": 2.41, "cpa": 82.7}),
		record("MIX-2607-05", project.OperationalRecordAudienceMix, "工程师证言与工厂实拍", "建议扩充", "2026-07-22T09:00:00.000Z", map[string]any{"supply": 22, "spend": 41.6}),
		record("METHOD-M6", project.OperationalRecordMethod, "工程师证言 + 微距实拍", "ready", "2026-07-22T09:00:00.000Z", map[string]any{"detail": "补足精度、良率和可制造性证据"}),
		record("DIAG-2607-04", project.OperationalRecordDeliveryDiagnostic, "审批证据完整度", "success", "2026-07-22T09:00:00.000Z", map[string]any{"value": "92%", "detail": "Brief、创意、预算和回滚说明均已归档"}),
		record("ACTION-P0", project.OperationalRecordDeliveryAction, "补充 6 条新素材", "P0", "2026-07-22T08:30:00.000Z", map[string]any{"detail": "优先改变核心内容，避免只替换边框或文案", "impact": "+12-18% 探索覆盖"}),
		record("ACTION-P1", project.OperationalRecordDeliveryAction, "合并重复广告", "P1", "2026-07-22T08:30:00.000Z", map[string]any{"detail": "保留有效广告，清理 7 条长期无消耗对象", "impact": "减少预算内耗"}),
		record("ACTION-P2", project.OperationalRecordDeliveryAction, "建立浅层转化实验", "P2", "2026-07-22T08:30:00.000Z", map[string]any{"detail": "5-10% 预算仅作为来源建议，提交前需人工确认", "impact": "扩大探索空间"}),
		record("ACTION-P3", project.OperationalRecordDeliveryAction, "补齐 CRM 回传校验", "P3", "2026-07-22T09:00:00.000Z", map[string]any{"detail": "用线索质量字段校验高意向样本，不把点击率直接等同成交", "impact": "提升演示可信度"}),
		record("REC-BRF-2607-11", project.OperationalRecordUnifiedRecord, "春季新品上市 Brief", "已确认", "2026-07-22T01:12:00.000Z", map[string]any{"kind": "Brief", "owner": "Amelia Meng"}),
		record("REC-STR-2607-08", project.OperationalRecordUnifiedRecord, "精度证据增长策略", "已确认", "2026-07-22T02:24:00.000Z", map[string]any{"kind": "策略", "owner": "Amelia Meng"}),
		record("REC-CR-2607-42", project.OperationalRecordUnifiedRecord, "精密制造图文与视频", "制作中", "2026-07-22T05:48:00.000Z", map[string]any{"kind": "创意", "owner": "Lin Wei"}),
		record("REC-PLAN-2607-06", project.OperationalRecordUnifiedRecord, "销售线索增长计划", "待审批", "2026-07-22T08:30:00.000Z", map[string]any{"kind": "投放", "owner": "Noah Xu"}),
		record("REC-CS-2607-018", project.OperationalRecordUnifiedRecord, "素材组合优化 ChangeSet", "待审批", "2026-07-22T08:30:00.000Z", map[string]any{"kind": "变更", "owner": "Noah Xu"}),
		record("REC-ASSET-2607-31", project.OperationalRecordUnifiedRecord, "品牌片视频 v1.3", "待评审", "2026-07-22T09:00:00.000Z", map[string]any{"kind": "视频资产", "owner": "Lin Wei"}),
		record("REC-AUDIT-2607-04", project.OperationalRecordUnifiedRecord, "投放模拟审计证据包", "已归档", "2026-07-22T09:06:00.000Z", map[string]any{"kind": "审计", "owner": "系统"}),
		record("WORK-2607-06", project.OperationalRecordWorkItem, "部署默认演示数据巡检", "已完成", "2026-07-22T09:10:00.000Z", map[string]any{"type": "上线检查", "owner": "系统", "progress": 100, "project_id": string(projectID)}),
	}
}

func record(id string, kind project.OperationalRecordKind, title, status, occurredAt string, fields map[string]any) demoOperationSeed {
	return demoOperationSeed{id: id, kind: kind, title: title, status: status, occurredAt: occurredAt, fields: fields}
}
