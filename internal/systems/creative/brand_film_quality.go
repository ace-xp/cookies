package creative

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

var requiredBrandFilmManualChecks = []string{
	"product_fidelity",
	"brand_logo_packaging",
	"subtitle_voiceover",
	"sound_music",
}

type RunBrandFilmQualityRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

type ConfirmBrandFilmQualityRequest struct {
	ExpectedRevision int64                  `json:"expected_revision"`
	ManualChecks     []BrandFilmManualCheck `json:"manual_checks"`
}

type BrandFilmVersionRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

type BrandFilmVersionResult struct {
	Workspace TaskDetail      `json:"workspace"`
	Version   CreativeVersion `json:"creative_version"`
}

type BrandFilmDeliveryResult struct {
	Workspace TaskDetail      `json:"workspace"`
	Package   CreativePackage `json:"creative_package"`
}

func (s Service) RunBrandFilmQuality(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request RunBrandFilmQualityRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	brand := detail.VideoDraft.BrandFilm
	if request.ExpectedRevision != detail.VideoDraft.Revision || brand.Generation == nil || brand.Generation.PreviewAsset == nil || s.Assets == nil {
		return TaskDetail{}, ErrInvalidState
	}
	// A mixed audio preview is the delivery candidate once it exists. The locked
	// visual preview remains the fallback for workspaces that have not entered
	// sound design, preserving legacy tasks while making the quality/version
	// trace point at the actual audible output.
	previewRef := *brand.Generation.PreviewAsset
	if brand.Audio != nil && brand.Audio.MixedPreview != nil {
		previewRef = *brand.Audio.MixedPreview
	}
	preview, err := s.Assets.ReadForCreative(ctx, actor, projectID, previewRef)
	if err != nil {
		return TaskDetail{}, err
	}
	checks := evaluateBrandFilmQuality(*brand, preview)
	passed := true
	for _, check := range checks {
		passed = passed && check.Passed
	}
	id, err := s.idGenerator()("brandquality")
	if err != nil {
		return TaskDetail{}, err
	}
	now := s.now()
	run := BrandFilmQualityRun{
		ID: id, Revision: int64(len(brand.QualityRuns) + 1), PreviewAsset: previewRef,
		Status: "failed", Checks: checks, ManualChecks: []BrandFilmManualCheck{}, Metrics: brandFilmRunMetrics(*brand.Generation),
		AutomaticPassed: passed, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	if passed {
		run.Status = "awaiting_human"
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision = next.Revision
	next.BrandFilm.Stage = BrandFilmQualityReview
	next.BrandFilm.QualityRuns = append(next.BrandFilm.QualityRuns, run)
	next.BrandFilm.Delivery = nil
	next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: false, Blockers: []string{"automatic_quality_check", "human_quality_confirmation"}}
	if passed {
		next.BrandFilm.Readiness.Blockers = []string{"human_quality_confirmation"}
	}
	next.BrandFilm.UpdatedAt = now
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}

func (s Service) ConfirmBrandFilmQuality(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request ConfirmBrandFilmQualityRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	brand := detail.VideoDraft.BrandFilm
	if request.ExpectedRevision != detail.VideoDraft.Revision || len(brand.QualityRuns) == 0 {
		return TaskDetail{}, ErrVersionConflict
	}
	run := brand.QualityRuns[len(brand.QualityRuns)-1]
	if !run.AutomaticPassed || run.Status != "awaiting_human" {
		return TaskDetail{}, ErrInvalidState
	}
	checks, allPassed, err := normalizeBrandFilmManualChecks(request.ManualChecks)
	if err != nil {
		return TaskDetail{}, err
	}
	now := s.now()
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision = next.Revision
	current := &next.BrandFilm.QualityRuns[len(next.BrandFilm.QualityRuns)-1]
	current.ManualChecks, current.UpdatedAt = checks, now
	current.Status = "failed"
	next.BrandFilm.Stage = BrandFilmQualityReview
	next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: false, Blockers: []string{"human_quality_confirmation"}}
	if allPassed {
		current.Status, current.HumanConfirmed = "passed", true
		current.HumanConfirmedBy, current.HumanConfirmedAt = actor.Principal.ID, &now
		next.BrandFilm.Stage = BrandFilmReadyForReview
		next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: true, Blockers: []string{}}
	}
	next.BrandFilm.UpdatedAt = now
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}

func (s Service) FinalizeBrandFilmVersion(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, taskID string, request BrandFilmVersionRequest, key contract.IdempotencyKey) (BrandFilmVersionResult, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, requestContext.Actor, projectID, taskID, true)
	if err != nil {
		return BrandFilmVersionResult{}, err
	}
	if detail.VideoDraft.BrandFilm.Delivery != nil && detail.VideoDraft.BrandFilm.Delivery.CreativeVersionID != "" {
		version, readErr := s.Repository.GetVersion(ctx, requestContext.Actor.OrganizationID, projectID, detail.VideoDraft.BrandFilm.Delivery.CreativeVersionID)
		return BrandFilmVersionResult{Workspace: detail, Version: version}, readErr
	}
	if request.ExpectedRevision != detail.VideoDraft.Revision || !brandFilmQualityConfirmed(*detail.VideoDraft.BrandFilm) {
		return BrandFilmVersionResult{}, ErrInvalidState
	}
	version, _, err := s.FreezeVersion(ctx, requestContext, projectID, taskID, FreezeVersionRequest{DraftVersion: detail.VideoDraft.Revision}, key)
	if err != nil {
		return BrandFilmVersionResult{}, err
	}
	version, err = s.CheckVersion(ctx, requestContext.Actor, projectID, version.ID)
	if err != nil {
		return BrandFilmVersionResult{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	now := s.now()
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage, next.BrandFilm.UpdatedAt = next.Revision, BrandFilmReadyForReview, now
	next.BrandFilm.Delivery = &BrandFilmDeliveryLifecycle{QualityRunID: next.BrandFilm.QualityRuns[len(next.BrandFilm.QualityRuns)-1].ID, CreativeVersionID: version.ID}
	workspace, err := s.persistBrandFilmDraft(ctx, requestContext.Actor, projectID, taskID, *detail.VideoDraft, next)
	return BrandFilmVersionResult{Workspace: workspace, Version: version}, err
}

func (s Service) ApproveBrandFilmVersion(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request BrandFilmVersionRequest) (BrandFilmVersionResult, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return BrandFilmVersionResult{}, err
	}
	delivery := detail.VideoDraft.BrandFilm.Delivery
	if request.ExpectedRevision != detail.VideoDraft.Revision || delivery == nil || delivery.CreativeVersionID == "" {
		return BrandFilmVersionResult{}, ErrInvalidState
	}
	version, err := s.Repository.GetVersion(ctx, actor.OrganizationID, projectID, delivery.CreativeVersionID)
	if err != nil {
		return BrandFilmVersionResult{}, err
	}
	if version.Status != CreativeVersionApproved {
		version, err = s.ApproveVersion(ctx, actor, projectID, version.ID)
		if err != nil {
			return BrandFilmVersionResult{}, err
		}
	}
	now := s.now()
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage, next.BrandFilm.UpdatedAt = next.Revision, BrandFilmApproved, now
	next.BrandFilm.Delivery.ApprovedBy, next.BrandFilm.Delivery.ApprovedAt = actor.Principal.ID, &now
	workspace, err := s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
	return BrandFilmVersionResult{Workspace: workspace, Version: version}, err
}

func (s Service) DeliverBrandFilmVersion(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request BrandFilmVersionRequest) (BrandFilmDeliveryResult, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return BrandFilmDeliveryResult{}, err
	}
	delivery := detail.VideoDraft.BrandFilm.Delivery
	if request.ExpectedRevision != detail.VideoDraft.Revision || delivery == nil || delivery.CreativeVersionID == "" || detail.VideoDraft.BrandFilm.Stage != BrandFilmApproved {
		return BrandFilmDeliveryResult{}, ErrInvalidState
	}
	var pkg CreativePackage
	if delivery.CreativePackageID != "" {
		packages, listErr := s.Repository.ListPackages(ctx, actor.OrganizationID, projectID, 100)
		if listErr != nil {
			return BrandFilmDeliveryResult{}, listErr
		}
		for _, candidate := range packages {
			if candidate.ID == delivery.CreativePackageID {
				pkg = candidate
			}
		}
	} else {
		pkg, err = s.DeliverVersion(ctx, actor, projectID, delivery.CreativeVersionID)
		if err != nil {
			return BrandFilmDeliveryResult{}, err
		}
	}
	if pkg.ID == "" {
		return BrandFilmDeliveryResult{}, ErrNotFound
	}
	now := s.now()
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage, next.BrandFilm.UpdatedAt = next.Revision, BrandFilmDelivered, now
	next.BrandFilm.Delivery.CreativePackageID = pkg.ID
	next.BrandFilm.Delivery.DeliveredBy, next.BrandFilm.Delivery.DeliveredAt = actor.Principal.ID, &now
	workspace, err := s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
	return BrandFilmDeliveryResult{Workspace: workspace, Package: pkg}, err
}

func evaluateBrandFilmQuality(brand BrandFilmDraft, preview CreativeAssetSnapshot) []BrandFilmQualityCheck {
	checks := make([]BrandFilmQualityCheck, 0, 8)
	add := func(code, category, scope, evidence, repair string, passed bool) {
		severity := "blocking"
		if passed {
			severity = "info"
		}
		checks = append(checks, BrandFilmQualityCheck{Code: code, Category: category, Scope: scope, Passed: passed, Severity: severity, Evidence: evidence, RepairAdvice: repair})
	}
	generation := brand.Generation
	allLocked := generation != nil && len(generation.Units) > 0
	if generation != nil {
		for _, unit := range generation.Units {
			lockedOutput := false
			for _, attempt := range unit.Attempts {
				lockedOutput = lockedOutput || (attempt.ID == unit.LockedAttemptID && attempt.OutputAssetRef != nil && attempt.Status == string(contract.ProviderJobSucceeded))
			}
			allLocked = allLocked && lockedOutput
			add("unit_"+unit.ID, "shot", unit.ID, fmt.Sprintf("%ds-%ds locked attempt %s", unit.StartSecond, unit.EndSecond, unit.LockedAttemptID), "重新生成并锁定该片段", lockedOutput)
		}
	}
	add("locked_generation_units", "technical", "film", "all generation units have a selected succeeded asset", "仅返工未通过的生成片段", allLocked)
	add("preview_asset_ready", "technical", "film", fmt.Sprintf("%s; ready=%t", preview.MIMEType, preview.Ready), "重新合成并等待视频素材入库完成", preview.Ready && preview.Kind == contract.AssetVideo && preview.MIMEType == "video/mp4")
	durationPassed := preview.DurationMS >= 14900 && preview.DurationMS <= 15100
	add("duration_15_seconds", "technical", "film", fmt.Sprintf("duration_ms=%d", preview.DurationMS), "按镜头点重新裁切为 15 秒", durationPassed)
	add("vertical_720p", "technical", "film", fmt.Sprintf("%dx%d", preview.WidthPixels, preview.HeightPixels), "按 720×1280 重新渲染", preview.WidthPixels == 720 && preview.HeightPixels == 1280)
	codec := strings.ToLower(strings.TrimSpace(preview.VideoCodec))
	add("mp4_h264", "technical", "film", fmt.Sprintf("mime=%s codec=%s", preview.MIMEType, preview.VideoCodec), "转码为 MP4/H.264", preview.MIMEType == "video/mp4" && (codec == "h264" || codec == "avc1"))
	add("audio_track", "audio", "film", fmt.Sprintf("audio_codec=%s", preview.AudioCodec), "补齐统一口播或音乐音轨后重新合成", strings.TrimSpace(preview.AudioCodec) != "")
	plan := brand.CurrentPlan()
	copyPassed, musicPassed := plan != nil, plan != nil && (strings.TrimSpace(plan.MusicDirection) != "" || !plan.SoundDesignIntent.Empty())
	if plan != nil {
		copyText := strings.ToLower(plan.StorySummary)
		for _, shot := range plan.Shots {
			copyText += " " + strings.ToLower(shot.Voiceover+" "+shot.OnScreenText)
		}
		if analysis := brand.CurrentAnalysis(); analysis != nil {
			for _, prohibited := range analysis.Prohibited {
				copyPassed = copyPassed && !strings.Contains(copyText, strings.ToLower(strings.TrimSpace(prohibited)))
			}
		}
	}
	add("claim_compliance", "copy", "film", "script and on-screen copy checked against prohibited claims", "修改对应旁白或字幕后重新确认剧本", copyPassed)
	add("music_direction", "audio", "film", "confirmed film plan contains a music or sound-design direction", "补充音乐方向与授权说明", musicPassed)
	return checks
}

func normalizeBrandFilmManualChecks(values []BrandFilmManualCheck) ([]BrandFilmManualCheck, bool, error) {
	if len(values) != len(requiredBrandFilmManualChecks) {
		return nil, false, fmt.Errorf("all brand film manual checks are required")
	}
	byCode := make(map[string]BrandFilmManualCheck, len(values))
	for _, value := range values {
		value.Code, value.Note, value.UnitID = strings.TrimSpace(value.Code), strings.TrimSpace(value.Note), strings.TrimSpace(value.UnitID)
		if _, duplicate := byCode[value.Code]; duplicate {
			return nil, false, fmt.Errorf("duplicate brand film manual check %s", value.Code)
		}
		byCode[value.Code] = value
	}
	result, allPassed := make([]BrandFilmManualCheck, 0, len(requiredBrandFilmManualChecks)), true
	for _, code := range requiredBrandFilmManualChecks {
		value, ok := byCode[code]
		if !ok {
			return nil, false, fmt.Errorf("brand film manual check %s is required", code)
		}
		allPassed = allPassed && value.Passed
		result = append(result, value)
	}
	return result, allPassed, nil
}

func brandFilmRunMetrics(generation BrandFilmGeneration) BrandFilmRunMetrics {
	metrics := BrandFilmRunMetrics{UnitCount: len(generation.Units), RegenerationReasons: map[string]int{}}
	available := 0
	for _, unit := range generation.Units {
		if unit.LockedAttemptID != "" {
			available++
		}
		metrics.AttemptCount += len(unit.Attempts)
		if len(unit.Attempts) > 1 {
			metrics.RegenerationCount += len(unit.Attempts) - 1
		}
		for _, attempt := range unit.Attempts {
			if attempt.Status == string(contract.ProviderJobSucceeded) {
				metrics.SucceededAttempts++
			} else if attempt.Status == string(contract.ProviderJobFailed) || attempt.Status == string(contract.ProviderJobCancelled) {
				metrics.FailedAttempts++
			}
			if reason := strings.TrimSpace(attempt.Feedback); reason != "" {
				metrics.RegenerationReasons[reason]++
			}
		}
	}
	if metrics.AttemptCount > 0 {
		metrics.SuccessRate = math.Round(float64(metrics.SucceededAttempts)/float64(metrics.AttemptCount)*1000) / 1000
	}
	if metrics.UnitCount > 0 {
		metrics.AvailabilityRate = math.Round(float64(available)/float64(metrics.UnitCount)*1000) / 1000
	}
	return metrics
}

func brandFilmQualityConfirmed(brand BrandFilmDraft) bool {
	if len(brand.QualityRuns) == 0 {
		return false
	}
	run := brand.QualityRuns[len(brand.QualityRuns)-1]
	return run.Status == "passed" && run.AutomaticPassed && run.HumanConfirmed
}
