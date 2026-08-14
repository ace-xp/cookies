package creative

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStrategyBrandWorkflowReadIsSideEffectFreeAndPrepareIsIdempotent(t *testing.T) {
	service := testService()
	intake := brandBriefTestIntake(t)
	intake.Request.CreativeRoutes[0].RequiresHumanConfirmation = true
	service.Repository.(*memoryRepository).intakes[intake.ID] = intake
	briefs := &brandBriefRepositoryStub{}
	service.BrandBriefs = briefs
	service.StrategyPackages = strategyPackageReader{snapshot: brandBriefStrategyPackageSnapshot()}
	service.Directions = &directionRepositoryStub{}
	actor := testRequestContext().Actor

	read, err := service.GetStrategyBrandWorkflow(context.Background(), actor, intake.ProjectID, intake.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Mode != StrategyBrandBriefReviewRequired || read.NextAction != "prepare_brief" || briefs.review.IntakeID != "" {
		t.Fatalf("read-only restore mutated or misclassified workflow: result=%+v review=%+v", read, briefs.review)
	}

	request := PrepareStrategyBrandWorkflowRequest{
		ExpectedInputIdentityHash: intake.InputIdentityHash,
		SelectedRouteID:           intake.Request.SelectedRouteID,
		AcceptStrategyProjection:  true,
	}
	prepared, err := service.PrepareStrategyBrandWorkflow(context.Background(), actor, intake.ProjectID, intake.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Mode != StrategyBrandDirectionReady || prepared.NextAction != "generate_directions" ||
		prepared.BrandBrief == nil || prepared.BrandBrief.Status != BrandBriefConfirmed {
		t.Fatalf("prepared workflow did not advance to direction gate: %+v", prepared)
	}
	confirmedRevision := prepared.BrandBrief.Revision
	replayed, err := service.PrepareStrategyBrandWorkflow(context.Background(), actor, intake.ProjectID, intake.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.BrandBrief == nil || replayed.BrandBrief.Revision != confirmedRevision {
		t.Fatalf("prepare replay changed confirmed Brief revision: first=%d replay=%+v", confirmedRevision, replayed.BrandBrief)
	}
}

func TestStrategyBrandWorkflowRejectsMismatchedAcceptance(t *testing.T) {
	service := testService()
	intake := brandBriefTestIntake(t)
	service.Repository.(*memoryRepository).intakes[intake.ID] = intake
	service.BrandBriefs = &brandBriefRepositoryStub{}
	service.StrategyPackages = strategyPackageReader{snapshot: brandBriefStrategyPackageSnapshot()}
	service.Directions = &directionRepositoryStub{}

	_, err := service.PrepareStrategyBrandWorkflow(context.Background(), testRequestContext().Actor, intake.ProjectID, intake.ID, PrepareStrategyBrandWorkflowRequest{
		ExpectedInputIdentityHash: "sha256:wrong",
		SelectedRouteID:           intake.Request.SelectedRouteID,
		AcceptStrategyProjection:  true,
	})
	if !errors.Is(err, ErrStrategyBrandLineageMismatch) {
		t.Fatalf("mismatched acceptance error=%v, want ErrStrategyBrandLineageMismatch", err)
	}
	if service.BrandBriefs.(*brandBriefRepositoryStub).review.IntakeID != "" {
		t.Fatal("mismatched acceptance persisted a Brand Brief")
	}
}

func TestConfirmedStrategyDirectionReplacesOnlyAnEmptyLegacyTask(t *testing.T) {
	service, intake, direction := strategyBrandTaskFixture(t)
	repository := service.Repository.(*memoryRepository)
	legacyTask := CreativeTask{
		ID: "legacy_brand_task", OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID, IntakeID: intake.ID,
		Format: FormatVideo, Channel: ChannelDouyin, VideoPurpose: "brand", PerformanceMode: PerformanceModeBrandFilm,
		Status: TaskDraft, Direction: CreativeDirection{InputIdentityHash: intake.InputIdentityHash}, Version: 1,
		CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	legacyBrand, err := newBrandFilmDraft(legacyTask, intake, intake.Request.CreativeRoutes[0], service.now())
	if err != nil {
		t.Fatal(err)
	}
	legacyDraft := VideoDraft{
		ContractVersion: "creative-video-draft/v1", TaskID: legacyTask.ID, Revision: 1,
		Concept: "等待品牌方向", Prompt: "等待品牌方向", DurationSeconds: 30, AspectRatio: "16:9", Resolution: "1080p",
		VideoPurpose: "brand", Mandatory: []string{}, Prohibited: []string{}, BrandFilm: legacyBrand, CreatedAt: service.now(),
	}
	repository.tasks[legacyTask.ID] = TaskDetail{Task: legacyTask, Intake: intake, VideoDraft: &legacyDraft, ProductionJobs: []ProductionJob{}}

	created, err := service.CreateVideoTask(context.Background(), testRequestContext().Actor, intake.ProjectID, intake.ID, CreateVideoTaskRequest{
		SelectedRouteID: intake.Request.SelectedRouteID, DirectionID: direction.ID, Channel: ChannelDouyin,
		Mandatory: []string{}, Prohibited: []string{}, ConfirmRoute: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == legacyTask.ID || repository.tasks[legacyTask.ID].Task.Status != TaskArchived {
		t.Fatalf("empty legacy task was not archived and replaced: old=%+v new=%+v", repository.tasks[legacyTask.ID].Task, created)
	}
	createdDetail := repository.tasks[created.ID]
	if createdDetail.VideoDraft == nil || createdDetail.VideoDraft.BrandFilm == nil ||
		createdDetail.VideoDraft.BrandFilm.Stage != BrandFilmConceptConfirmed || created.Direction.DirectionVersionID != direction.ID {
		t.Fatalf("replacement task did not start from the confirmed direction: %+v", createdDetail)
	}
}

func TestConfirmedStrategyDirectionDoesNotReplaceLegacyTaskWithUserWork(t *testing.T) {
	service, intake, direction := strategyBrandTaskFixture(t)
	repository := service.Repository.(*memoryRepository)
	legacyTask := CreativeTask{
		ID: "legacy_brand_task", OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID, IntakeID: intake.ID,
		Format: FormatVideo, Channel: ChannelDouyin, VideoPurpose: "brand", PerformanceMode: PerformanceModeBrandFilm,
		Status: TaskDraft, Direction: CreativeDirection{InputIdentityHash: intake.InputIdentityHash}, Version: 2,
		CreatedAt: service.now(), UpdatedAt: service.now(),
	}
	legacyBrand, err := newBrandFilmDraft(legacyTask, intake, intake.Request.CreativeRoutes[0], service.now())
	if err != nil {
		t.Fatal(err)
	}
	legacyBrand.BriefAnalyses = []BrandBriefAnalysisVersion{{Revision: 1}}
	legacyDraft := VideoDraft{
		ContractVersion: "creative-video-draft/v1", TaskID: legacyTask.ID, Revision: 2,
		Concept: "用户已编辑", Prompt: "用户已编辑", DurationSeconds: 30, AspectRatio: "16:9", Resolution: "1080p",
		VideoPurpose: "brand", Mandatory: []string{}, Prohibited: []string{}, BrandFilm: legacyBrand, CreatedAt: service.now(),
	}
	repository.tasks[legacyTask.ID] = TaskDetail{Task: legacyTask, Intake: intake, VideoDraft: &legacyDraft, ProductionJobs: []ProductionJob{}}

	_, err = service.CreateVideoTask(context.Background(), testRequestContext().Actor, intake.ProjectID, intake.ID, CreateVideoTaskRequest{
		SelectedRouteID: intake.Request.SelectedRouteID, DirectionID: direction.ID, Channel: ChannelDouyin,
		Mandatory: []string{}, Prohibited: []string{}, ConfirmRoute: true,
	})
	if !errors.Is(err, ErrStrategyBrandLegacyTaskNeedsReview) || repository.tasks[legacyTask.ID].Task.Status == TaskArchived {
		t.Fatalf("non-empty legacy task error/status = %v/%s", err, repository.tasks[legacyTask.ID].Task.Status)
	}
}

func strategyBrandTaskFixture(t *testing.T) (Service, CreativeIntake, CreativeDirectionVersion) {
	t.Helper()
	service := testService()
	intake := brandBriefTestIntake(t)
	intake.Request.CreativeRoutes[0].RequiresHumanConfirmation = true
	service.Repository.(*memoryRepository).intakes[intake.ID] = intake
	briefs := confirmedBrandBriefRepository(intake)
	createdAt := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)
	briefs.review.ContentHash = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	briefs.review.ConfirmedBy = "user_1"
	briefs.review.ConfirmedAt = &createdAt
	briefs.review.CreatedAt = createdAt
	briefs.review.UpdatedAt = createdAt
	briefs.review.Document = BrandBriefDocument{
		Summary: "Brand campaign", Market: "CN", Language: "zh-CN",
		Objective:        BrandBriefObjective{Statement: "Build brand memory"},
		AudienceSegments: []BrandBriefAudience{{SegmentID: "audience_1", Label: "Audience"}},
		Product:          BrandBriefProduct{BrandName: "Kanon", ProductName: "Creative Platform", SellingPoints: []string{"Traceable"}, ProofPoints: []string{"evidence_1"}},
		Communication:    BrandBriefCommunication{SingleMindedProposition: intake.Request.CoreMessage},
		Route:            BrandBriefRoute{RouteID: intake.Request.SelectedRouteID, Spec: BrandBriefRouteSpec{TargetDurationSeconds: 30, AspectRatio: "16:9", Resolution: "1080p"}},
		AudioIntent:      BrandBriefAudioIntent{VoiceDirection: "calm and confident", OverallMood: "restrained"},
	}
	service.BrandBriefs = briefs
	direction := CreativeDirectionVersion{
		ContractVersion: CreativeDirectionVersionV1, ID: "direction_brand", OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID,
		BatchID: "batch_brand", IntakeID: intake.ID, InputIdentityHash: intake.InputIdentityHash, RouteID: intake.Request.SelectedRouteID,
		Concept: "A traceable moment", CreativeRationale: "Turn engineering confidence into brand memory",
		MessagePlan: []string{"Show the human decision"}, ExecutionOutline: []string{"One continuous move"}, GuardrailTrace: []string{"Use approved facts"},
		DirectionMode: "cinematic", EmotionalArc: "uncertainty to confidence", VisualGrammar: "restrained light",
		BrandMemoryDevice: "a returning line of light", HumanMoment: "a team confirms together",
		Status: DirectionStatusConfirmed, Version: 1,
		ContentHash: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", CreatedAt: createdAt,
		BrandBriefRef: &BrandBriefReference{Revision: briefs.review.Revision, ContentHash: briefs.review.ContentHash},
	}
	alternate := direction
	alternate.ID = "direction_brand_alternate"
	alternate.Concept = "A visible decision"
	alternate.ContentHash = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	alternate.Status = DirectionStatusCandidate
	service.Directions = &directionRepositoryStub{batch: CreativeDirectionBatch{
		ContractVersion: CreativeDirectionBatchV1, ID: direction.BatchID, OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID,
		IntakeID: intake.ID, InputIdentityHash: intake.InputIdentityHash,
		BrandBriefRef: direction.BrandBriefRef, Status: DirectionBatchReady, Candidates: []CreativeDirectionVersion{direction, alternate}, CreatedAt: createdAt,
	}}
	return service, intake, direction
}
