package creative

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const strategyBrandFilmSourceType = "strategy_handoff"

// InitializeStrategyBrandFilmWorkspace upgrades a strategy-created brand task
// that predates the embedded BrandFilm draft. It is deliberately explicit and
// idempotent so a GET never mutates durable workflow state.
func (s Service) InitializeStrategyBrandFilmWorkspace(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string) (TaskDetail, error) {
	if s.Repository == nil || s.ViralRemakes == nil || s.Projects == nil || s.Directions == nil || s.BrandBriefs == nil {
		return TaskDetail{}, fmt.Errorf("strategy brand film dependencies are incomplete")
	}
	if !actor.HasScope(ScopeWrite) {
		return TaskDetail{}, fmt.Errorf("%s scope is required", ScopeWrite)
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return TaskDetail{}, err
	}
	detail, err := s.Repository.GetTaskDetail(ctx, actor.OrganizationID, projectID, taskID)
	if err != nil {
		return TaskDetail{}, err
	}
	if detail.Task.Format != FormatVideo || detail.Task.PerformanceMode != PerformanceModeBrandFilm || detail.VideoDraft == nil || detail.Task.Status == TaskArchived {
		return TaskDetail{}, ErrInvalidState
	}
	if detail.VideoDraft.BrandFilm != nil {
		return detail, nil
	}
	intake, err := s.Repository.GetIntake(ctx, actor.OrganizationID, projectID, detail.Task.IntakeID)
	if err != nil || intake.Source != IntakeSourceStrategyPackage {
		return TaskDetail{}, ErrInvalidState
	}
	direction, err := s.Directions.GetDirection(ctx, actor.OrganizationID, projectID, strings.TrimSpace(detail.Task.Direction.DirectionVersionID))
	if err != nil || direction.Status != DirectionStatusConfirmed {
		return TaskDetail{}, ErrInvalidState
	}
	batch, err := s.Directions.GetDirectionBatch(ctx, actor.OrganizationID, projectID, direction.BatchID)
	if err != nil {
		return TaskDetail{}, err
	}
	brief, err := s.BrandBriefs.GetBrandBrief(ctx, actor.OrganizationID, projectID, intake.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	var route CreativeRouteSnapshot
	for _, candidate := range intake.Request.CreativeRoutes {
		if candidate.RouteID == direction.RouteID {
			route = candidate
			break
		}
	}
	if route.RouteID == "" {
		return TaskDetail{}, ErrInvalidState
	}
	now := s.now()
	brand, err := buildStrategyBrandFilmDraft(detail.Task.ID, intake, route, brief, batch, direction, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	brand.Revision = next.Revision
	next.BrandFilm = brand
	if err := next.Validate(); err != nil {
		return TaskDetail{}, err
	}
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}

func buildStrategyBrandFilmDraft(
	taskID string,
	intake CreativeIntake,
	route CreativeRouteSnapshot,
	brief BrandBriefReview,
	batch CreativeDirectionBatch,
	direction CreativeDirectionVersion,
	now time.Time,
) (*BrandFilmDraft, error) {
	if intake.Source != IntakeSourceStrategyPackage || route.RouteType != CreativeRouteBrandVideo ||
		brief.Status != BrandBriefConfirmed || batch.Status != DirectionBatchReady || direction.Status != DirectionStatusConfirmed {
		return nil, fmt.Errorf("strategy brand film requires confirmed Brief, direction batch, and direction")
	}
	if brief.IntakeID != intake.ID || brief.InputIdentityHash != intake.InputIdentityHash ||
		batch.IntakeID != intake.ID || batch.InputIdentityHash != intake.InputIdentityHash ||
		direction.IntakeID != intake.ID || direction.InputIdentityHash != intake.InputIdentityHash ||
		direction.BatchID != batch.ID || direction.RouteID != route.RouteID ||
		!brandBriefReferencesEqual(direction.BrandBriefRef, &BrandBriefReference{Revision: brief.Revision, ContentHash: brief.ContentHash}) ||
		!brandBriefReferencesEqual(batch.BrandBriefRef, &BrandBriefReference{Revision: brief.Revision, ContentHash: brief.ContentHash}) {
		return nil, fmt.Errorf("strategy brand film lineage is inconsistent")
	}
	if direction.Version < 1 || !validSHA256Ref(direction.ContentHash) || !validSHA256Ref(brief.ContentHash) || !validSHA256Ref(intake.InputIdentityHash) {
		return nil, fmt.Errorf("strategy brand film content hashes are invalid")
	}

	briefText, err := json.Marshal(brief.Document)
	if err != nil {
		return nil, fmt.Errorf("encode confirmed brand Brief: %w", err)
	}
	analysis, err := brandFilmAnalysisFromConfirmedBrief(brief, now)
	if err != nil {
		return nil, err
	}
	concepts, err := brandFilmConceptSetFromDirectionBatch(batch, direction, brief.Document, now)
	if err != nil {
		return nil, err
	}

	source := BrandFilmSourceSnapshot{
		SourceType: strategyBrandFilmSourceType,
		IntakeID:   intake.ID, InputIdentityHash: intake.InputIdentityHash,
		BrandBriefRevision: brief.Revision, BrandBriefContentHash: brief.ContentHash,
		DirectionBatchID: batch.ID, DirectionID: direction.ID, DirectionVersion: direction.Version, DirectionContentHash: direction.ContentHash,
		RouteID: route.RouteID, BriefName: brandFilmBriefName(brief.Document), BriefText: string(briefText),
		ProductName: brief.Document.Product.ProductName, Channel: string(routeChannel(route, intake.Request.Channel)),
		Duration: route.TargetDurationSeconds, AspectRatio: route.AspectRatio, Resolution: firstBrandFilmNonEmpty(route.Resolution, brief.Document.Route.Spec.Resolution, "720p"),
		EvidenceRefs: brandFilmEvidenceRefs(brief.Document, route),
	}
	if ref := intake.Request.StrategyPackageRef; ref != nil {
		source.StrategyPackageID, source.StrategyPackageVersion, source.StrategyPackageHash = ref.PackageID, ref.PackageVersion, ref.PackageContentHash
		source.HandoffContractVersion, source.HandoffContentHash = ref.HandoffContractVersion, ref.HandoffContentHash
	} else if ref := intake.Request.StrategyPackage; ref != nil {
		source.StrategyPackageID, source.StrategyPackageVersion, source.StrategyPackageHash = ref.PackageID, ref.PackageVersion, ref.ExpectedContentHash
		source.HandoffContractVersion, source.HandoffContentHash = ref.HandoffContractVersion, ref.ExpectedHandoffHash
	}
	sourceHash, err := contract.CanonicalJSONHash(source)
	if err != nil {
		return nil, fmt.Errorf("hash strategy brand film source: %w", err)
	}
	draft := &BrandFilmDraft{
		ContractVersion: "creative-brand-film-draft/v1", TaskID: taskID, Revision: 1,
		Stage: BrandFilmConceptConfirmed, SourceSnapshot: source, SourceHash: "sha256:" + sourceHash,
		BriefAnalyses: []BrandBriefAnalysisVersion{analysis}, ConceptSets: []BrandCreativeConceptSet{concepts},
		SelectedConceptID: direction.ID, FilmPlans: []BrandFilmPlanVersion{}, QualityRuns: []BrandFilmQualityRun{},
		Readiness: CreativeReadiness{PlanningReady: true, GenerationReady: false, ProductionReady: false, Blockers: []string{"production_plan_confirmation", "prompt_package"}},
		PromptSeam: BrandFilmReservedGenerationSeam{
			ContractVersion: "creative-brand-generation-seam/v1", UnitPolicy: "4_to_15_seconds",
			PromptContract: "brand-shot-prompt-package/v1", AttemptPolicy: "single_default_regenerate_on_feedback",
		},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := draft.Validate(); err != nil {
		return nil, fmt.Errorf("build strategy brand film draft: %w", err)
	}
	return draft, nil
}

func brandFilmAnalysisFromConfirmedBrief(brief BrandBriefReview, now time.Time) (BrandBriefAnalysisVersion, error) {
	document := brief.Document
	facts := make([]BrandBriefFact, 0, len(document.Claims)+len(document.Product.SellingPoints))
	seen := map[string]bool{}
	for _, claim := range document.Claims {
		text := strings.TrimSpace(claim.ApprovedText)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		facts = append(facts, BrandBriefFact{Text: text, Locator: brandFilmLocator(claim.EvidenceRefIDs, brief.ContentHash), Confidence: 1, Status: "brief_fact"})
	}
	for _, sellingPoint := range document.Product.SellingPoints {
		text := strings.TrimSpace(sellingPoint)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		facts = append(facts, BrandBriefFact{Text: text, Locator: brandFilmLocator(document.Product.ProofPoints, brief.ContentHash), Confidence: 1, Status: "brief_fact"})
	}
	mandatory := make([]string, 0, len(document.Route.AssetRequirements)+len(document.Claims))
	for _, requirement := range document.Route.AssetRequirements {
		mandatory = append(mandatory, strings.TrimSpace(requirement.Role)+"@"+strings.TrimSpace(requirement.RequiredStage))
	}
	for _, claim := range document.Claims {
		if disclaimer := strings.TrimSpace(claim.RequiredDisclaimer); disclaimer != "" {
			mandatory = append(mandatory, disclaimer)
		}
	}
	prohibited := make([]string, 0, len(document.Guardrails))
	for _, guardrail := range document.Guardrails {
		prohibited = append(prohibited, guardrail.Text)
	}
	assets := make([]BrandBriefAssetCandidate, 0, len(document.Assets))
	for index, asset := range document.Assets {
		ref := contract.AssetVersionRef{AssetID: contract.AssetID(asset.AssetRef.AssetID), Version: asset.AssetRef.Version}
		role := normalizeBrandFilmAssetRole(asset.Role)
		status := firstNonEmpty(asset.Rights.Status, "unknown")
		assets = append(assets, BrandBriefAssetCandidate{
			ID: fmt.Sprintf("strategy_asset_%02d", index+1), Role: role, Label: firstNonEmpty(role, asset.Role),
			SourceLocator: fmt.Sprintf("brand_brief.assets[%d]", index), AssetRef: &ref, RightsStatus: status,
			UserConfirmed: (status == "confirmed" || status == "approved") && asset.Rights.GenerativeAIAllowed && asset.Rights.DerivativeWorkAllowed,
		})
	}
	uncertainties := make([]string, 0, len(document.OpenQuestions))
	for _, question := range document.OpenQuestions {
		uncertainties = append(uncertainties, question.Message)
	}
	confirmedAt := brief.ConfirmedAt
	if confirmedAt == nil {
		confirmedAt = &now
	}
	analysis := BrandBriefAnalysisVersion{
		Revision: brief.Revision, Summary: document.Summary, Audience: brandFilmAudience(document.AudienceSegments),
		CoreMessage: document.Communication.SingleMindedProposition, SellingPoints: facts,
		Mandatory: compactBrandFilmStrings(mandatory), Prohibited: compactBrandFilmStrings(prohibited),
		ImageRequirements: brandFilmAssetRequirements(document.Route.AssetRequirements, "generation"),
		VideoRequirements: []string{fmt.Sprintf("%ds · %s · %s", document.Route.Spec.TargetDurationSeconds, document.Route.Spec.AspectRatio, document.Route.Spec.Resolution)},
		SoundDesignIntent: SoundDesignIntent{MusicDirection: firstBrandFilmNonEmpty(document.AudioIntent.OverallMood, strings.Join(document.Communication.ToneConstraints, "、"), "克制、有品牌留白感的音乐"), SoundEffectFocus: []string{"镜头动作与产品材质", "转场与品牌定格"}, SourceAudioPolicy: "mute", Avoid: []string{"人声"}},
		AssetCandidates:   assets, Uncertainties: compactBrandFilmStrings(uncertainties), Confirmed: true,
		ConfirmedBy: brief.ConfirmedBy, ConfirmedAt: confirmedAt,
		ModelAlias: "strategy.confirmed-brand-brief", ModelVersion: brief.ContentHash,
		PromptVersion: "brand-brief-adapter/v1", CreatedAt: firstNonZeroTime(brief.UpdatedAt, now),
	}
	if err := analysis.Validate(); err != nil {
		return BrandBriefAnalysisVersion{}, fmt.Errorf("adapt confirmed brand Brief: %w", err)
	}
	return analysis, nil
}

func brandFilmConceptSetFromDirectionBatch(batch CreativeDirectionBatch, selected CreativeDirectionVersion, document BrandBriefDocument, now time.Time) (BrandCreativeConceptSet, error) {
	candidates := make([]BrandCreativeConcept, 0, len(batch.Candidates))
	for _, direction := range batch.Candidates {
		visualLanguage := compactBrandFilmStrings(append([]string{direction.VisualGrammar}, direction.ExecutionOutline...))
		candidate := BrandCreativeConcept{
			ID: direction.ID, Title: direction.Concept, OneLiner: firstBrandFilmString(direction.MessagePlan, direction.Concept),
			StoryMechanism: direction.CreativeRationale, BrandEntrance: firstNonEmpty(direction.BrandMemoryDevice, direction.HumanMoment),
			VisualLanguage: visualLanguage, SoundIdea: firstNonEmpty(document.AudioIntent.OverallMood, document.AudioIntent.VoiceDirection),
			BriefRationale: direction.CreativeRationale, Risk: strings.Join(direction.GuardrailTrace, "；"),
			Selected: direction.ID == selected.ID, Confirmed: direction.ID == selected.ID,
		}
		candidates = append(candidates, candidate)
	}
	value := BrandCreativeConceptSet{
		Revision: 1, AnalysisRevision: selected.BrandBriefRef.Revision, Candidates: candidates,
		ModelAlias: "cookies.text.standard", ModelVersion: firstNonEmpty(batch.Model, "strategy-direction-batch"),
		PromptVersion: firstNonEmpty(batch.PromptVersion, "creative-direction/v1"), CreatedAt: firstNonZeroTime(batch.CreatedAt, now),
	}
	if err := value.Validate(); err != nil {
		return BrandCreativeConceptSet{}, fmt.Errorf("adapt confirmed CreativeDirection batch: %w", err)
	}
	return value, nil
}

func brandFilmBriefName(document BrandBriefDocument) string {
	return strings.TrimSpace(strings.Join(compactBrandFilmStrings([]string{document.Product.BrandName, document.Product.ProductName, "Brand Brief"}), " · "))
}

func brandFilmAudience(values []BrandBriefAudience) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, strings.Join(compactBrandFilmStrings([]string{value.Label, value.Insight, value.Tension}), "："))
	}
	return strings.Join(compactBrandFilmStrings(items), "；")
}

func brandFilmEvidenceRefs(document BrandBriefDocument, route CreativeRouteSnapshot) []string {
	values := append([]string{}, route.EvidenceRefs...)
	for _, ref := range document.SourceRefs {
		values = append(values, firstNonEmpty(ref.ResourceURI, ref.RefID))
	}
	return compactBrandFilmStrings(values)
}

func brandFilmLocator(values []string, fallback string) string {
	if result := strings.Join(compactBrandFilmStrings(values), ","); result != "" {
		return result
	}
	return fallback
}

func brandFilmAssetRequirements(values []BrandBriefAssetRequirement, stage string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value.RequiredStage == "" || value.RequiredStage == stage {
			result = append(result, value.Role)
		}
	}
	return compactBrandFilmStrings(result)
}

func normalizeBrandFilmAssetRole(value string) string {
	switch strings.TrimSpace(value) {
	case "brand_logo":
		return "logo"
	case "product", "product_image", "product_packshot":
		return "product_front"
	default:
		return strings.TrimSpace(value)
	}
}

func routeChannel(route CreativeRouteSnapshot, fallback CreativeChannel) CreativeChannel {
	if fallback != "" {
		return fallback
	}
	if len(route.Channels) > 0 {
		return CreativeChannel(route.Channels[0])
	}
	return ""
}

func firstBrandFilmString(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(fallback)
}

func firstBrandFilmNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func compactBrandFilmStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func firstNonZeroTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}
