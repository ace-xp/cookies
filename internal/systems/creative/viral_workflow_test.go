package creative

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestAnalyzeViralRemakePersistsFiveDimensionsAndSurvivesReload(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: ViralAnalysisResult{
		Dimensions: []ViralAnalysisDimension{
			{ID: ViralTaskGoalType, Prompt: "15 秒转化广告", EvidenceRefs: []string{"timeline:0-15"}, Confidence: .9, Source: "ai_extracted"},
			{ID: ViralQualityStyleLighting, Prompt: "清晰商业光", EvidenceRefs: []string{"frame:1"}, Confidence: .8, Source: "ai_extracted"},
			{ID: ViralEnvironmentAtmosphere, Prompt: "冬日户外", EvidenceRefs: []string{"frame:2"}, Confidence: .8, Source: "ai_extracted"},
			{ID: ViralCameraContent, Prompt: "钩子、证明、CTA", EvidenceRefs: []string{"frame:3"}, Confidence: .9, Source: "ai_extracted"},
			{ID: ViralMusicSound, Prompt: "节奏递进", EvidenceRefs: []string{"asr:transcript"}, Confidence: .7, Source: "ai_extracted"},
		},
		PreserveRules: []string{"保留节奏功能"}, ReplaceRules: []string{"替换人物和品牌"},
		Transcript: "测试对白", Confidence: .82, EvidenceRefs: []string{"frame:1", "asr:transcript"},
		RouteRevisionID: "route_seed2_r1", PromptVersion: "viral.analyze.v1",
	}}
	actor := testRequestContext().Actor
	workspace, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	got := workspace.VideoDraft.ViralRemake
	if got.Status != ViralAnalysisReady || got.Revision != 2 || got.Analysis == nil ||
		len(got.Analysis.Dimensions) != 5 || got.PromptDraft == nil ||
		len(got.PromptDraft.Dimensions) != 5 || got.Readiness.GenerationReady {
		t.Fatalf("unexpected analyzed workspace: %+v", got)
	}
	reloaded, err := service.GetTaskDetail(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VideoDraft.ViralRemake.Analysis.ContentHash != got.Analysis.ContentHash {
		t.Fatal("analysis snapshot did not survive repository reload")
	}
}

func TestConfirmedViralPromptCreatesTraceableCandidateAndReview(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: completeViralAnalysisResult()}
	actor := testRequestContext().Actor
	analyzed, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	dimensions := cloneViralDimensions(analyzed.VideoDraft.ViralRemake.PromptDraft.Dimensions)
	dimensions[ViralEnvironmentAtmosphere] = "清晨城市通勤场景"
	revised, err := service.UpdateViralPrompt(context.Background(), actor, "project_1", taskID, UpdateViralPromptRequest{
		ExpectedRevision: analyzed.VideoDraft.Revision, Dimensions: dimensions,
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := service.ConfirmViralGeneration(context.Background(), actor, "project_1", taskID, ConfirmViralGenerationRequest{
		ExpectedRevision: revised.VideoDraft.Revision, ConfirmReferenceVideoRights: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed.VideoDraft.ViralRemake.Readiness.GenerationReady ||
		confirmed.VideoDraft.ViralRemake.PromptPackage == nil {
		t.Fatalf("prompt was not confirmed: %+v", confirmed.VideoDraft.ViralRemake)
	}
	input, promptHash, err := service.ViralProviderInput(context.Background(), actor, "project_1", taskID)
	if err != nil || input.InputMode != "text_only" || input.DurationSeconds != 15 || promptHash == "" {
		t.Fatalf("provider input = %+v, hash=%q, err=%v", input, promptHash, err)
	}
	registered, err := service.RegisterViralCandidateJob(context.Background(), actor, "project_1", taskID, "providerjob_viral_1")
	if err != nil {
		t.Fatal(err)
	}
	candidate := registered.VideoDraft.ViralRemake.Candidates[0]
	now := time.Date(2026, time.July, 28, 2, 0, 0, 0, time.UTC)
	reconciled, err := service.ReconcileViralCandidate(context.Background(), actor, "project_1", taskID, contract.ProviderJob{
		ID: "providerjob_viral_1", Kind: "provider.video.generate", OrganizationID: "org_1", ProjectID: "project_1",
		ExecutionStatus: contract.JobSucceeded, ProviderStatus: contract.ProviderJobSucceeded, Progress: 100,
		ProjectAssetRefs: []contract.ProjectAssetRef{{
			ProjectID: "project_1", AssetVersion: contract.AssetVersionRef{AssetID: "asset_generated", Version: 1},
		}},
		AttemptCount: 1, MaxAttempts: 360, Version: 3, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.VideoDraft.ViralRemake.Readiness.ProductionReady ||
		reconciled.VideoDraft.ViralRemake.Candidates[0].OutputAssetRef == nil {
		t.Fatalf("candidate did not become production-ready: %+v", reconciled.VideoDraft.ViralRemake)
	}
	reviewed, err := service.SubmitViralCandidateReview(context.Background(), actor, "project_1", taskID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.VideoDraft.ViralRemake.Status != ViralReadyForReview ||
		reviewed.VideoDraft.ViralRemake.Candidates[0].Status != ViralCandidateReviewed {
		t.Fatalf("candidate was not submitted for review: %+v", reviewed.VideoDraft.ViralRemake)
	}
}

func TestConfirmViralGenerationRequiresEveryReferencedAssetRight(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: completeViralAnalysisResult()}
	repository := service.Repository.(*memoryRepository)
	detail := repository.tasks[taskID]
	referenceImage := contract.AssetVersionRef{AssetID: "asset_reference_image", Version: 1}
	detail.VideoDraft.ViralRemake.InputSnapshot.ReferenceImage = &referenceImage
	detail.VideoDraft.ViralRemake.InputSnapshot.ReferenceImageRights = RightsPending
	repository.tasks[taskID] = detail
	actor := testRequestContext().Actor
	analyzed, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ConfirmViralGeneration(context.Background(), actor, "project_1", taskID, ConfirmViralGenerationRequest{
		ExpectedRevision: analyzed.VideoDraft.Revision, ConfirmReferenceVideoRights: true,
	})
	if err == nil || !strings.Contains(err.Error(), "rights") {
		t.Fatalf("error = %v, want missing reference-image authorization", err)
	}
}

func TestRetryViralWithoutReferenceImageCreatesOriginalPersonGeneration(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: completeViralAnalysisResult()}
	repository := service.Repository.(*memoryRepository)
	detail := repository.tasks[taskID]
	referenceImage := contract.AssetVersionRef{AssetID: "asset_real_person", Version: 1}
	detail.VideoDraft.ViralRemake.InputSnapshot.ReferenceImage = &referenceImage
	detail.VideoDraft.ViralRemake.InputSnapshot.ReferenceImageRights = RightsPending
	repository.tasks[taskID] = detail
	actor := testRequestContext().Actor
	analyzed, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := service.ConfirmViralGeneration(context.Background(), actor, "project_1", taskID, ConfirmViralGenerationRequest{
		ExpectedRevision: analyzed.VideoDraft.Revision, ConfirmReferenceVideoRights: true, ConfirmReferenceImageRights: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if input, _, err := service.ViralProviderInput(context.Background(), actor, "project_1", taskID); err != nil || input.InputMode != "reference_image" {
		t.Fatalf("first attempt must retain the explicitly authorized reference image: input=%+v err=%v", input, err)
	}
	registered, err := service.RegisterViralCandidateJob(context.Background(), actor, "project_1", taskID, "providerjob_rejected_reference")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.ReconcileViralCandidate(context.Background(), actor, "project_1", taskID, contract.ProviderJob{
		ID: "providerjob_rejected_reference", OrganizationID: "org_1", ProjectID: "project_1",
		ProviderStatus: contract.ProviderJobFailed, ExecutionStatus: contract.JobFailed,
		Error: &contract.JobError{Code: "REFERENCE_ASSET_CONTENT_REJECTED", Message: "real person reference rejected"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := service.RetryViralWithoutReferenceImage(context.Background(), actor, "project_1", taskID, RetryViralWithoutReferenceImageRequest{
		ExpectedRevision: failed.VideoDraft.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	viral := fallback.VideoDraft.ViralRemake
	if viral.Status != ViralGenerationReady || !viral.Readiness.GenerationReady || viral.PromptPackage == nil ||
		viral.PromptPackage.GenerationSpec.ReferenceImageMode != ViralReferenceImageModeTextOnlyOriginalPerson ||
		viral.PromptPackage.ContentHash == confirmed.VideoDraft.ViralRemake.PromptPackage.ContentHash ||
		!strings.Contains(viral.PromptPackage.CompositePrompt, "不上传视觉参考图") {
		t.Fatalf("fallback must freeze a distinct text-only original-person package: %+v", viral.PromptPackage)
	}
	input, _, err := service.ViralProviderInput(context.Background(), actor, "project_1", taskID)
	if err != nil || input.InputMode != "text_only" || len(input.ConditioningAssets) != 0 {
		t.Fatalf("fallback must not submit the rejected image again: input=%+v err=%v", input, err)
	}
	if len(viral.Candidates) != 1 || viral.Candidates[0].ID != registered.VideoDraft.ViralRemake.Candidates[0].ID {
		t.Fatalf("failed candidate lineage must remain visible after fallback: %+v", viral.Candidates)
	}
}

func TestRegisterViralCandidateJobRecoversARegistrationFailureWithoutNewCandidate(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: completeViralAnalysisResult()}
	actor := testRequestContext().Actor
	analyzed, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := service.ConfirmViralGeneration(context.Background(), actor, "project_1", taskID, ConfirmViralGenerationRequest{
		ExpectedRevision: analyzed.VideoDraft.Revision, ConfirmReferenceVideoRights: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := service.Repository.(*memoryRepository)
	repository.registerProductionJobErr = errors.New("temporary production-job persistence failure")
	if _, err := service.RegisterViralCandidateJob(context.Background(), actor, "project_1", taskID, "providerjob_viral_retry"); err == nil {
		t.Fatal("first registration must surface the injected persistence failure")
	}
	persisted, err := service.GetTaskDetail(context.Background(), actor, "project_1", taskID)
	if err != nil || len(persisted.VideoDraft.ViralRemake.Candidates) != 1 || len(persisted.ProductionJobs) != 0 {
		t.Fatalf("candidate must survive a failed job registration: detail=%+v err=%v", persisted, err)
	}
	repository.registerProductionJobErr = nil
	recovered, err := service.RegisterViralCandidateJob(context.Background(), actor, "project_1", taskID, "providerjob_viral_retry")
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.VideoDraft.ViralRemake.Candidates) != 1 || len(recovered.ProductionJobs) != 1 ||
		recovered.ProductionJobs[0].ProviderJobID != "providerjob_viral_retry" {
		t.Fatalf("retry must complete the same candidate registration: %+v", recovered)
	}
	if confirmed.VideoDraft.ViralRemake.PromptPackage.ContentHash != recovered.VideoDraft.ViralRemake.Candidates[0].PromptHash {
		t.Fatal("recovered candidate lost prompt lineage")
	}
}

func TestUpdateViralInputInvalidatesStaleAnalysisAndPrompt(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.ViralAnalyzer = stubViralAnalyzer{result: completeViralAnalysisResult()}
	actor := testRequestContext().Actor
	analyzed, err := service.AnalyzeViralRemake(context.Background(), actor, "project_1", taskID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateViralInput(context.Background(), actor, "project_1", taskID, UpdateViralInputRequest{
		ExpectedRevision: analyzed.VideoDraft.Revision,
		ProductName:      "修订后的目标产品", SellingPoints: []string{"可信卖点"},
		CallToAction: "立即预约", UserInstruction: "仅复用节奏，重做产品表达",
	})
	if err != nil {
		t.Fatal(err)
	}
	viral := updated.VideoDraft.ViralRemake
	if viral.Status != ViralWaitingForAnalysis || viral.Analysis != nil || viral.PromptDraft != nil ||
		viral.PromptPackage != nil || len(viral.Candidates) != 0 || viral.Readiness.GenerationReady ||
		viral.InputSnapshot.ProductName != "修订后的目标产品" || viral.InputHash == "input-hash" {
		t.Fatalf("stale viral state was not cleared: %+v", viral)
	}
}

func TestUpdateViralInputRejectsProductOutsideCurrentProjectProfile(t *testing.T) {
	t.Parallel()
	service, taskID := viralWorkflowTestService()
	service.Projects = productBoundTestProjects{productName: "Approved Product"}
	actor := testRequestContext().Actor
	_, err := service.UpdateViralInput(context.Background(), actor, "project_1", taskID, UpdateViralInputRequest{
		ExpectedRevision: 1, ProductName: "Unrelated Product", SellingPoints: []string{"Supported fact"},
		CallToAction: "Learn more", UserInstruction: "Create an original ad",
	})
	if err == nil || !strings.Contains(err.Error(), "current project profile") {
		t.Fatalf("error = %v, want project-product mismatch", err)
	}
}

type productBoundTestProjects struct{ productName string }

func (p productBoundTestProjects) RequireActiveContext(_ context.Context, actor contract.ActorContext, projectID contract.ProjectID) (contract.ProjectContext, error) {
	brand := contract.BrandID("brand_1")
	return contract.ProjectContext{OrganizationID: actor.OrganizationID, ProjectID: projectID, BrandID: &brand, ProductIDs: []contract.ProductID{"product_1"}, ProjectContextVersion: 1}, nil
}

func (p productBoundTestProjects) GetBusinessContext(_ context.Context, _ contract.ActorContext, projectID contract.ProjectID) (contract.ProjectBusinessContext, error) {
	return contract.ProjectBusinessContext{ProjectID: projectID, Products: []contract.ProjectBusinessProduct{{ID: "product_1", Name: p.productName}}}, nil
}

func viralWorkflowTestService() (Service, string) {
	service := testService()
	repository := service.Repository.(*memoryRepository)
	service.ViralRemakes = repository
	now := service.now()
	taskID := "creativetask_viral"
	input := ViralRemakeInputSnapshot{
		Source: IntakeSourceManual, SelectedRouteID: ManualViralRemakeRouteID,
		ReferenceVideo: contract.AssetVersionRef{AssetID: "asset_video", Version: 1},
		ProductName:    "测试产品", SellingPoints: []string{"卖点"}, CallToAction: "立即了解",
		UserInstruction: "原创改写", MandatoryElements: []string{}, ProhibitedClaims: []string{},
		ReferenceVideoRights: RightsPending,
	}
	repository.tasks[taskID] = TaskDetail{
		Task: CreativeTask{
			ID: taskID, OrganizationID: "org_1", ProjectID: "project_1", IntakeID: "intake_viral",
			Format: FormatVideo, Channel: ChannelDouyin, PerformanceMode: PerformanceModeViralRemake,
			Status: TaskDraft, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Intake: CreativeIntake{ID: "intake_viral", OrganizationID: "org_1", ProjectID: "project_1"},
		VideoDraft: &VideoDraft{
			ContractVersion: "creative-video-draft/v1", TaskID: taskID, Revision: 1,
			Concept: "爆款原创改写", Prompt: "等待分析", DurationSeconds: 15,
			AspectRatio: "9:16", Resolution: "720p",
			SourceVideo: input.ReferenceVideo, Mandatory: []string{}, Prohibited: []string{},
			CallToAction: input.CallToAction, CreatedAt: now,
			ViralRemake: &ViralRemakeDraft{
				ContractVersion: "creative-viral-remake-draft/v1", TaskID: taskID, Revision: 1,
				Status: ViralWaitingForAnalysis, SelectedRouteID: ManualViralRemakeRouteID,
				InputSnapshot: input, InputHash: "input-hash",
				Readiness:  CreativeReadiness{PlanningReady: true, MissingFields: []string{}, Blockers: []string{"analysis_snapshot", "confirmed_prompt_package", "reference_video_rights"}},
				Candidates: []ViralCandidate{}, CreatedAt: now, UpdatedAt: now,
			},
		},
		ProductionJobs: []ProductionJob{},
	}
	return service, taskID
}

type stubViralAnalyzer struct {
	result ViralAnalysisResult
	err    error
}

func (s stubViralAnalyzer) Analyze(context.Context, contract.ActorContext, contract.ProjectID, ViralAnalysisRequest) (ViralAnalysisResult, error) {
	return s.result, s.err
}

func completeViralAnalysisResult() ViralAnalysisResult {
	return ViralAnalysisResult{
		Dimensions: []ViralAnalysisDimension{
			{ID: ViralTaskGoalType, Prompt: "15 秒转化广告", EvidenceRefs: []string{"timeline:0-15"}, Confidence: .9, Source: "ai_extracted"},
			{ID: ViralQualityStyleLighting, Prompt: "清晰商业光", EvidenceRefs: []string{"frame:1"}, Confidence: .8, Source: "ai_extracted"},
			{ID: ViralEnvironmentAtmosphere, Prompt: "冬日户外", EvidenceRefs: []string{"frame:2"}, Confidence: .8, Source: "ai_extracted"},
			{ID: ViralCameraContent, Prompt: "钩子、证明、CTA", EvidenceRefs: []string{"frame:3"}, Confidence: .9, Source: "ai_extracted"},
			{ID: ViralMusicSound, Prompt: "节奏递进", EvidenceRefs: []string{"asr:transcript"}, Confidence: .7, Source: "ai_extracted"},
		},
		PreserveRules: []string{"保留节奏功能"}, ReplaceRules: []string{"替换人物和品牌"},
		Transcript: "测试对白", Confidence: .82, EvidenceRefs: []string{"frame:1", "asr:transcript"},
		RouteRevisionID: "route_seed2_r1", PromptVersion: "viral.analyze.v1",
	}
}
