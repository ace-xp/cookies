package creative

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const StrategyBrandWorkflowV1 = "creative-strategy-brand-workflow/v1"

type StrategyBrandWorkflowMode string

const (
	StrategyBrandBriefReviewRequired   StrategyBrandWorkflowMode = "brief_review_required"
	StrategyBrandDirectionReady        StrategyBrandWorkflowMode = "direction_ready"
	StrategyBrandDirectionSelection    StrategyBrandWorkflowMode = "direction_selection_required"
	StrategyBrandTaskReady             StrategyBrandWorkflowMode = "task_ready"
	StrategyBrandLegacyTaskNeedsReview StrategyBrandWorkflowMode = "legacy_task_upgrade_required"
)

type StrategyBrandWorkflowIssue struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

type StrategyBrandWorkflowResult struct {
	ContractVersion      string                       `json:"contract_version"`
	Mode                 StrategyBrandWorkflowMode    `json:"mode"`
	IntakeID             string                       `json:"intake_id"`
	InputIdentityHash    string                       `json:"input_identity_hash"`
	BrandBrief           *BrandBriefReview            `json:"brand_brief,omitempty"`
	LatestDirectionBatch *CreativeDirectionBatch      `json:"latest_direction_batch,omitempty"`
	ConfirmedDirection   *CreativeDirectionVersion    `json:"confirmed_direction,omitempty"`
	Task                 *CreativeTask                `json:"task,omitempty"`
	Issues               []StrategyBrandWorkflowIssue `json:"issues"`
	NextAction           string                       `json:"next_action"`
}

type PrepareStrategyBrandWorkflowRequest struct {
	ExpectedInputIdentityHash string `json:"expected_input_identity_hash"`
	SelectedRouteID           string `json:"selected_route_id"`
	AcceptStrategyProjection  bool   `json:"accept_strategy_projection"`
}

// GetStrategyBrandWorkflow restores durable state only. It deliberately does
// not project, confirm, generate, upgrade, or create any resource.
func (s Service) GetStrategyBrandWorkflow(
	ctx context.Context,
	actor contract.ActorContext,
	projectID contract.ProjectID,
	intakeID string,
) (StrategyBrandWorkflowResult, error) {
	if s.Repository == nil || s.Projects == nil || s.BrandBriefs == nil {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("Strategy brand workflow is unavailable")
	}
	if !actor.HasScope(ScopeRead) {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("%s scope is required", ScopeRead)
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	intake, route, err := s.strategyBrandWorkflowInput(ctx, actor, projectID, intakeID)
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	return s.strategyBrandWorkflowResult(ctx, actor, projectID, intake, route)
}

// PrepareStrategyBrandWorkflow is the single explicit acceptance command for
// a frozen Strategy projection. It is deterministic and never invokes a model,
// generates directions, or creates a Creative task.
func (s Service) PrepareStrategyBrandWorkflow(
	ctx context.Context,
	actor contract.ActorContext,
	projectID contract.ProjectID,
	intakeID string,
	request PrepareStrategyBrandWorkflowRequest,
) (StrategyBrandWorkflowResult, error) {
	if s.Repository == nil || s.Projects == nil || s.BrandBriefs == nil {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("Strategy brand workflow is unavailable")
	}
	if !actor.HasScope(ScopeWrite) {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("%s scope is required", ScopeWrite)
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	intake, route, err := s.strategyBrandWorkflowInput(ctx, actor, projectID, intakeID)
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	if !request.AcceptStrategyProjection || strings.TrimSpace(request.ExpectedInputIdentityHash) == "" ||
		request.ExpectedInputIdentityHash != intake.InputIdentityHash || request.SelectedRouteID != route.RouteID {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("%w: explicit acceptance, input identity, and selected route are required", ErrStrategyBrandLineageMismatch)
	}
	brief, err := s.PrepareBrandBriefReview(ctx, actor, projectID, intake.ID)
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	if len(brief.Blockers) == 0 && brief.Status != BrandBriefConfirmed {
		_, err = s.ConfirmBrandBriefReview(ctx, actor, projectID, intake.ID, ConfirmBrandBriefReviewRequest{ExpectedRevision: brief.Revision})
		if errors.Is(err, ErrVersionConflict) {
			current, readErr := s.BrandBriefs.GetBrandBrief(ctx, actor.OrganizationID, projectID, intake.ID)
			if readErr != nil || current.Status != BrandBriefConfirmed {
				return StrategyBrandWorkflowResult{}, err
			}
			err = nil
		}
		if err != nil {
			return StrategyBrandWorkflowResult{}, err
		}
	}
	return s.strategyBrandWorkflowResult(ctx, actor, projectID, intake, route)
}

func (s Service) strategyBrandWorkflowInput(
	ctx context.Context,
	actor contract.ActorContext,
	projectID contract.ProjectID,
	intakeID string,
) (CreativeIntake, CreativeRouteSnapshot, error) {
	intake, err := s.Repository.GetIntake(ctx, actor.OrganizationID, projectID, intakeID)
	if err != nil {
		return CreativeIntake{}, CreativeRouteSnapshot{}, err
	}
	if intake.Source != IntakeSourceStrategyPackage || intake.ContractVersion != CreativeIntakeV3ContractVersion ||
		intake.Status != IntakeReady || strings.TrimSpace(intake.InputIdentityHash) == "" {
		return CreativeIntake{}, CreativeRouteSnapshot{}, fmt.Errorf("%w: a ready Strategy CreativeIntake v3 is required", ErrStrategyBrandLineageMismatch)
	}
	route, err := selectedPlanningRoute(intake.Request.CreativeRoutes, intake.Request.SelectedRouteID)
	if err != nil {
		return CreativeIntake{}, CreativeRouteSnapshot{}, fmt.Errorf("%w: %v", ErrStrategyBrandLineageMismatch, err)
	}
	if route.RouteType != CreativeRouteBrandVideo || route.VideoPurpose != "brand" {
		return CreativeIntake{}, CreativeRouteSnapshot{}, fmt.Errorf("%w: selected route is not brand video", ErrStrategyBrandLineageMismatch)
	}
	return intake, route, nil
}

func (s Service) strategyBrandWorkflowResult(
	ctx context.Context,
	actor contract.ActorContext,
	projectID contract.ProjectID,
	intake CreativeIntake,
	route CreativeRouteSnapshot,
) (StrategyBrandWorkflowResult, error) {
	result := StrategyBrandWorkflowResult{
		ContractVersion: StrategyBrandWorkflowV1,
		Mode:            StrategyBrandBriefReviewRequired, IntakeID: intake.ID, InputIdentityHash: intake.InputIdentityHash,
		Issues: []StrategyBrandWorkflowIssue{}, NextAction: "prepare_brief",
	}
	brief, err := s.BrandBriefs.GetBrandBrief(ctx, actor.OrganizationID, projectID, intake.ID)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	result.BrandBrief = &brief
	if brief.InputIdentityHash != intake.InputIdentityHash {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("%w: brand Brief input identity changed", ErrStrategyBrandLineageMismatch)
	}
	if brief.Status != BrandBriefConfirmed || len(brief.Blockers) > 0 {
		result.NextAction = "review_brief"
		for _, blocker := range brief.Blockers {
			result.Issues = append(result.Issues, StrategyBrandWorkflowIssue{
				Code: "strategy_brand_projection_blocked", Stage: "planning", Path: "brand_brief",
				Message: blocker, Source: "creative_brand_brief_validator",
			})
		}
		return result, nil
	}
	result.Mode, result.NextAction = StrategyBrandDirectionReady, "generate_directions"
	reader, ok := s.Directions.(DirectionBatchReader)
	if !ok || s.Directions == nil {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("creative direction history is unavailable")
	}
	batch, err := reader.GetLatestDirectionBatch(ctx, actor.OrganizationID, projectID, intake.ID)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	result.LatestDirectionBatch = &batch
	briefRef := &BrandBriefReference{Revision: brief.Revision, ContentHash: brief.ContentHash}
	if batch.IntakeID != intake.ID || batch.InputIdentityHash != intake.InputIdentityHash || !brandBriefReferencesEqual(batch.BrandBriefRef, briefRef) {
		result.Issues = append(result.Issues, StrategyBrandWorkflowIssue{
			Code: "strategy_brand_direction_batch_stale", Stage: "planning", Path: "latest_direction_batch",
			Message: "The latest direction batch belongs to an earlier Brand Brief lineage.", Source: "creative_lineage_validator",
		})
		return result, nil
	}
	if batch.Status == DirectionBatchGenerating {
		result.NextAction = "wait_for_directions"
		return result, nil
	}
	if batch.Status == DirectionBatchFailed {
		result.NextAction = "retry_directions"
		return result, nil
	}
	if batch.Status != DirectionBatchReady {
		return result, nil
	}
	result.Mode, result.NextAction = StrategyBrandDirectionSelection, "select_direction"
	var confirmed *CreativeDirectionVersion
	for index := range batch.Candidates {
		candidate := &batch.Candidates[index]
		if candidate.Status == DirectionStatusConfirmed {
			confirmed = candidate
			break
		}
	}
	if confirmed == nil {
		return result, nil
	}
	result.ConfirmedDirection = confirmed
	result.NextAction = "create_task"
	strategyTasks, ok := s.Repository.(StrategyBrandTaskRepository)
	if !ok {
		return StrategyBrandWorkflowResult{}, fmt.Errorf("Strategy brand task repository is unavailable")
	}
	tasks, err := strategyTasks.ListActiveTasksForIntake(ctx, actor.OrganizationID, projectID, intake.ID)
	if err != nil {
		return StrategyBrandWorkflowResult{}, err
	}
	legacyNeedsReview := false
	for _, task := range tasks {
		if task.IntakeID != intake.ID || task.Status == TaskArchived {
			continue
		}
		if task.Direction.DirectionVersionID == confirmed.ID {
			detail, detailErr := s.Repository.GetTaskDetail(ctx, actor.OrganizationID, projectID, task.ID)
			if detailErr != nil {
				return StrategyBrandWorkflowResult{}, detailErr
			}
			if !isReadyStrategyBrandTask(detail, intake, brief, *confirmed, route) {
				return StrategyBrandWorkflowResult{}, fmt.Errorf("%w: persisted task does not match its Strategy source", ErrStrategyBrandLineageMismatch)
			}
			value := task
			result.Task = &value
			result.Mode, result.NextAction = StrategyBrandTaskReady, "open_task"
			return result, nil
		}
		if strings.TrimSpace(task.Direction.DirectionVersionID) == "" {
			detail, detailErr := s.Repository.GetTaskDetail(ctx, actor.OrganizationID, projectID, task.ID)
			if detailErr != nil {
				return StrategyBrandWorkflowResult{}, detailErr
			}
			if !isEmptyLegacyStrategyBrandTask(detail) {
				legacyNeedsReview = true
			}
		}
	}
	if legacyNeedsReview {
		result.Mode, result.NextAction = StrategyBrandLegacyTaskNeedsReview, "review_legacy_task"
		result.Issues = append(result.Issues, StrategyBrandWorkflowIssue{
			Code: "strategy_brand_legacy_task_requires_upgrade", Stage: "planning", Path: "creative_task",
			Message: "An earlier task contains user work and cannot be replaced automatically.", Source: "creative_legacy_task_classifier",
		})
	}
	return result, nil
}

func isReadyStrategyBrandTask(detail TaskDetail, intake CreativeIntake, brief BrandBriefReview, direction CreativeDirectionVersion, route CreativeRouteSnapshot) bool {
	brand := (*BrandFilmDraft)(nil)
	if detail.VideoDraft != nil {
		brand = detail.VideoDraft.BrandFilm
	}
	return detail.Task.IntakeID == intake.ID && detail.Task.Status != TaskArchived &&
		detail.Task.Format == FormatVideo && detail.Task.PerformanceMode == PerformanceModeBrandFilm &&
		detail.Task.Direction.DirectionVersionID == direction.ID && detail.Task.Direction.InputIdentityHash == intake.InputIdentityHash &&
		brand != nil && brand.Stage != BrandFilmWaitingBrief && brand.SourceSnapshot.IntakeID == intake.ID &&
		brand.SourceSnapshot.InputIdentityHash == intake.InputIdentityHash && brand.SourceSnapshot.RouteID == route.RouteID &&
		brand.SourceSnapshot.BrandBriefContentHash == brief.ContentHash && brand.SourceSnapshot.DirectionID == direction.ID
}

func isEmptyLegacyStrategyBrandTask(detail TaskDetail) bool {
	if detail.Task.Status != TaskDraft || detail.Task.Version != 1 || detail.Task.Format != FormatVideo ||
		detail.Task.PerformanceMode != PerformanceModeBrandFilm || strings.TrimSpace(detail.Task.Direction.DirectionVersionID) != "" ||
		strings.TrimSpace(detail.Task.LineageKey) != "" || detail.Intake.Source != IntakeSourceStrategyPackage ||
		detail.VideoDraft == nil || detail.VideoDraft.Revision != 1 || len(detail.ProductionJobs) != 0 ||
		len(detail.ShortDramaGenerationAttempts) != 0 || len(detail.GamePrerollGenerationAttempts) != 0 ||
		len(detail.CommerceGenerationAttempts) != 0 {
		return false
	}
	brand := detail.VideoDraft.BrandFilm
	return brand != nil && brand.Revision == 1 && brand.Stage == BrandFilmWaitingBrief &&
		brand.SourceSnapshot.IntakeID == detail.Intake.ID && brand.SourceSnapshot.InputIdentityHash == detail.Intake.InputIdentityHash &&
		len(brand.BriefAnalyses) == 0 && len(brand.ConceptSets) == 0 && brand.SelectedConceptID == "" &&
		len(brand.FilmPlans) == 0 && brand.Generation == nil && brand.Audio == nil && len(brand.QualityRuns) == 0 && brand.Delivery == nil
}
