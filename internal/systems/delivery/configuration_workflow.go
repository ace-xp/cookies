package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type RecommendationStatus string

const (
	RecommendationProposed RecommendationStatus = "proposed"
	RecommendationAccepted RecommendationStatus = "accepted"
	RecommendationRejected RecommendationStatus = "rejected"
)

// DeliveryRecommendation keeps the frozen legacy snapshot fields solely so
// historical rows can still be decoded. New recommendations always use the
// platform configuration fields.
type DeliveryRecommendation struct {
	ID                  string                  `json:"id"`
	OrganizationID      contract.OrganizationID `json:"organization_id"`
	ProjectID           contract.ProjectID      `json:"project_id"`
	PlanID              string                  `json:"plan_id"`
	PlanVersion         int                     `json:"plan_version"`
	SimulationRunID     string                  `json:"simulation_run_id"`
	Fingerprint         string                  `json:"fingerprint"`
	BaseSnapshotHash    string                  `json:"base_snapshot_hash"`
	BaseSnapshot        *ThreeTierConfiguration `json:"base_snapshot,omitempty"`
	TargetSnapshot      *ThreeTierConfiguration `json:"target_snapshot,omitempty"`
	BaseConfiguration   *PlatformConfiguration  `json:"base_configuration,omitempty"`
	TargetConfiguration *PlatformConfiguration  `json:"target_configuration,omitempty"`
	TargetSnapshotHash  string                  `json:"target_snapshot_hash"`
	RuntimeStatus       string                  `json:"runtime_status,omitempty"`
	ReadOnly            bool                    `json:"read_only,omitempty"`
	Evidence            []string                `json:"evidence"`
	Action              string                  `json:"action"`
	Impact              string                  `json:"impact"`
	Risks               []string                `json:"risks"`
	Observation         string                  `json:"observation"`
	CooldownUntil       *time.Time              `json:"cooldown_until,omitempty"`
	Provenance          string                  `json:"provenance"`
	Status              RecommendationStatus    `json:"status"`
	Version             int64                   `json:"version"`
	IdempotencyKey      string                  `json:"-"`
	RequestHash         string                  `json:"-"`
	AcceptedChangeSetID string                  `json:"accepted_change_set_id,omitempty"`
	CreatedBy           string                  `json:"created_by"`
	CreatedAt           time.Time               `json:"created_at"`
	UpdatedAt           time.Time               `json:"updated_at"`
}

type RecommendationAcceptance struct {
	Recommendation DeliveryRecommendation `json:"recommendation"`
	ChangeSet      ChangeSet              `json:"change_set"`
}

// ManualActionPackage and ManualActionInstruction are frozen audit DTOs. No
// runtime path creates or materializes them.
type ManualActionPackage struct {
	ID                          string                    `json:"id"`
	OrganizationID              contract.OrganizationID   `json:"organization_id"`
	ProjectID                   contract.ProjectID        `json:"project_id"`
	ChangeSetID                 string                    `json:"change_set_id"`
	TargetSnapshotHash          string                    `json:"target_snapshot_hash"`
	ConfigurationSchemaVersion  string                    `json:"configuration_schema_version,omitempty"`
	ConfigurationID             string                    `json:"configuration_id,omitempty"`
	ConfigurationVersion        int                       `json:"configuration_version,omitempty"`
	ConfigurationPlatform       DeliveryPlatform          `json:"configuration_platform,omitempty"`
	ConfigurationProfileVersion string                    `json:"configuration_profile_version,omitempty"`
	ConfigurationCanonicalHash  string                    `json:"configuration_canonical_hash,omitempty"`
	IntentSchemaVersion         string                    `json:"intent_schema_version,omitempty"`
	IntentID                    string                    `json:"intent_id,omitempty"`
	IntentVersion               int                       `json:"intent_version,omitempty"`
	IntentCanonicalHash         string                    `json:"intent_canonical_hash,omitempty"`
	ContentHash                 string                    `json:"content_hash"`
	RuntimeStatus               string                    `json:"runtime_status,omitempty"`
	ReadOnly                    bool                      `json:"read_only,omitempty"`
	Instructions                []ManualActionInstruction `json:"instructions"`
	ForbiddenActions            []string                  `json:"forbidden_actions"`
	Evidence                    []string                  `json:"evidence"`
	Provenance                  string                    `json:"provenance"`
	OptimizedPlanVersion        int                       `json:"optimized_plan_version"`
	OptimizedPlanHash           string                    `json:"optimized_plan_hash"`
	Source                      Source                    `json:"source"`
	Scenario                    string                    `json:"scenario"`
	CreatedAt                   time.Time                 `json:"created_at"`
}

type ManualActionInstruction struct {
	Layer                string         `json:"layer"`
	GroupID              string         `json:"group_id"`
	PlanID               string         `json:"plan_id"`
	CreativeID           string         `json:"creative_id"`
	FieldKey             string         `json:"field_key"`
	Effective            ThreeTierValue `json:"effective"`
	Source               string         `json:"source"`
	ConfirmationRequired bool           `json:"confirmation_required"`
	ExpectedResult       string         `json:"expected_result"`
	EvidenceRefs         []string       `json:"evidence_refs"`
}

type configurationWorkflowRepository interface {
	CreateRecommendation(context.Context, DeliveryRecommendation) (DeliveryRecommendation, error)
	ListRecommendations(context.Context, contract.OrganizationID, contract.ProjectID, int) ([]DeliveryRecommendation, error)
	GetRecommendation(context.Context, contract.OrganizationID, contract.ProjectID, string) (DeliveryRecommendation, error)
	AcceptRecommendation(context.Context, DeliveryRecommendation, string, string, ChangeSet) (RecommendationAcceptance, bool, error)
	RejectRecommendation(context.Context, contract.OrganizationID, contract.ProjectID, string, int64, string, time.Time) (DeliveryRecommendation, error)
}

type legacyManualActionPackageRepository interface {
	GetManualActionPackage(context.Context, contract.OrganizationID, contract.ProjectID, string) (ManualActionPackage, error)
}

func (s Service) configurationWorkflow() (configurationWorkflowRepository, error) {
	repository, ok := s.Repository.(configurationWorkflowRepository)
	if !ok {
		return nil, ErrUnsupportedConfigurationWorkflow
	}
	return repository, nil
}

// legacyThreeTierSnapshotHash is frozen for verifying historical snapshots.
// It must never be used to create a runtime object.
func legacyThreeTierSnapshotHash(configuration *ThreeTierConfiguration) (string, error) {
	if configuration == nil {
		return "", nil
	}
	return contract.CanonicalJSONHash(configuration)
}

func changeSetPreflightVersion(base DeliveryPlanVersion, changeSet ChangeSet) (DeliveryPlanVersion, error) {
	if !base.IsPlatformConfigurationV2() || changeSet.TargetSnapshot == nil || changeSet.LegacyTargetSnapshot != nil {
		return DeliveryPlanVersion{}, ErrLegacyConfigurationUnsupported
	}
	if err := changeSet.TargetSnapshot.validateStructure(); err != nil {
		return DeliveryPlanVersion{}, err
	}
	if changeSet.TargetSnapshot.CanonicalHash != changeSet.TargetSnapshotHash {
		return DeliveryPlanVersion{}, ErrApprovalContentMismatch
	}
	version := cloneVersion(base)
	version.PlatformConfiguration = cloneJSONPointer(changeSet.TargetSnapshot)
	version.CanonicalHash = changeSet.TargetSnapshotHash
	return version, nil
}

func (s Service) GenerateRecommendation(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, planID string, expectedVersion int) (DeliveryRecommendation, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return DeliveryRecommendation{}, err
	}
	repository, err := s.configurationWorkflow()
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	plan, err := s.Repository.GetPlan(ctx, actor.OrganizationID, projectID, planID)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	if plan.Version != int64(expectedVersion) {
		return DeliveryRecommendation{}, ErrVersionConflict
	}
	if strings.TrimSpace(plan.TourRunID) == "" {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	if plan.CurrentVersion.ReadOnly || !plan.CurrentVersion.IsPlatformConfigurationV2() {
		return DeliveryRecommendation{}, ErrLegacyConfigurationUnsupported
	}
	executions, err := s.Repository.ListExecutions(ctx, actor.OrganizationID, projectID, 100)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	var sourceExecution *ExecutionResult
	for index := range executions {
		candidate := &executions[index]
		if candidate.ChangeSet.PlanID == plan.ID && candidate.Execution.Status == ExecutionSucceeded {
			sourceExecution = candidate
			break
		}
	}
	if sourceExecution == nil {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	simulationRepository, err := s.outcomeSimulations()
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	simulationRun, _, err := simulationRepository.GetLatestOutcomeSimulation(ctx, actor.OrganizationID, projectID, sourceExecution.Execution.ID)
	if err != nil {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	metrics, err := s.Repository.ListMetricSnapshots(ctx, actor.OrganizationID, projectID, sourceExecution.Execution.ID, 100)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	simulationMetrics := make([]DeliveryMetricSnapshot, 0, len(metrics))
	for _, metric := range metrics {
		if metric.SimulationRunID == simulationRun.ID {
			simulationMetrics = append(simulationMetrics, metric)
		}
	}
	if len(simulationMetrics) < 2 {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	sort.Slice(simulationMetrics, func(i, j int) bool { return simulationMetrics[i].WindowSequence < simulationMetrics[j].WindowSequence })
	baseline, current := simulationMetrics[0], simulationMetrics[len(simulationMetrics)-1]
	alerts, err := s.Repository.ListAlerts(ctx, actor.OrganizationID, projectID, AlertFilter{PlanID: plan.ID, ExecutionID: sourceExecution.Execution.ID, Limit: 100})
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	relevantAlerts := make([]DeliveryAlert, 0, len(alerts))
	for _, alert := range alerts {
		if alert.PlanID == plan.ID && alert.ExecutionID == sourceExecution.Execution.ID && alert.SimulationRunID == simulationRun.ID {
			relevantAlerts = append(relevantAlerts, alert)
		}
	}
	if len(relevantAlerts) == 0 {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	baselineCPA := baseline.RawMetrics.SpendCents / maxInt64(1, baseline.RawMetrics.Conversions)
	currentCPA := current.RawMetrics.SpendCents / maxInt64(1, current.RawMetrics.Conversions)
	cpaRatioBP := currentCPA * 10000 / maxInt64(1, baselineCPA)
	reductionPercent := clampInt64((cpaRatioBP-10000)/1000, 5, 20)
	evidence := []string{"simulation://execution/" + sourceExecution.Execution.ID, "simulation://run/" + simulationRun.ID, "simulation://metric/" + baseline.ID, "simulation://metric/" + current.ID}
	for _, alert := range relevantAlerts {
		evidence = append(evidence, "simulation://alert/"+alert.ID)
	}
	baseConfiguration := cloneJSONPointer(plan.CurrentVersion.PlatformConfiguration)
	targetConfiguration := cloneJSONPointer(plan.CurrentVersion.PlatformConfiguration)
	targetConfiguration.VersionNumber++
	ocean := targetConfiguration.Payload.OceanEngine
	if ocean == nil || ocean.Project == nil {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	ocean.Project.BudgetAndBidding.DailyBudgetMinor = ocean.Project.BudgetAndBidding.DailyBudgetMinor * (100 - reductionPercent) / 100
	targetConfiguration.CanonicalHash = ""
	targetConfiguration, err = finalizeRecommendationConfiguration(targetConfiguration)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	fingerprint, err := contract.CanonicalJSONHash(struct {
		Plan     string   `json:"plan"`
		Version  int      `json:"version"`
		Hash     string   `json:"hash"`
		Evidence []string `json:"evidence"`
	}{plan.ID, expectedVersion, targetConfiguration.CanonicalHash, evidence})
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	id, err := s.idGenerator()("deliveryrecommendation")
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	now := s.now()
	cooldown := now.Add(24 * time.Hour)
	return repository.CreateRecommendation(ctx, DeliveryRecommendation{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, PlanID: plan.ID, PlanVersion: expectedVersion,
		SimulationRunID: simulationRun.ID, Fingerprint: fingerprint, BaseSnapshotHash: baseConfiguration.CanonicalHash,
		BaseConfiguration: baseConfiguration, TargetConfiguration: targetConfiguration, TargetSnapshotHash: targetConfiguration.CanonicalHash,
		RuntimeStatus: PlanRuntimeActive, Evidence: evidence, Action: fmt.Sprintf("reduce_budget_%d_percent", reductionPercent),
		Impact: fmt.Sprintf("第二次人工批准后，将计划预算下调 %d%%", reductionPercent), Risks: []string{"需验证下调后转化量是否恢复", "不得自动应用"},
		Observation:   fmt.Sprintf("投后情景模拟的 CPA 从 %d 分上升到 %d 分；调整幅度由 %.2f 倍恶化程度推导，建议观察下一完整窗口", baselineCPA, currentCPA, float64(cpaRatioBP)/10000),
		CooldownUntil: &cooldown, Provenance: OutcomeSimulationModelVersion, Status: RecommendationProposed, Version: 1,
		CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
}

func finalizeRecommendationConfiguration(value *PlatformConfiguration) (*PlatformConfiguration, error) {
	finalized, err := FinalizePlatformConfiguration(*value)
	if err != nil {
		return nil, err
	}
	return &finalized, nil
}

func (s Service) ListRecommendations(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]DeliveryRecommendation, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	repository, err := s.configurationWorkflow()
	if err != nil {
		return nil, err
	}
	return repository.ListRecommendations(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
}

func (s Service) GetRecommendation(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (DeliveryRecommendation, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return DeliveryRecommendation{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return DeliveryRecommendation{}, err
	}
	repository, err := s.configurationWorkflow()
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	return repository.GetRecommendation(ctx, actor.OrganizationID, projectID, id)
}

func (s Service) AcceptRecommendation(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id, key string, expected int64) (RecommendationAcceptance, bool, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return RecommendationAcceptance{}, false, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return RecommendationAcceptance{}, false, err
	}
	repository, err := s.configurationWorkflow()
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	if strings.TrimSpace(key) == "" || expected < 1 {
		return RecommendationAcceptance{}, false, ErrInvalidRequest
	}
	recommendation, err := repository.GetRecommendation(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	if recommendation.ReadOnly || recommendation.BaseConfiguration == nil || recommendation.TargetConfiguration == nil || recommendation.BaseSnapshot != nil || recommendation.TargetSnapshot != nil {
		return RecommendationAcceptance{}, false, ErrLegacyConfigurationUnsupported
	}
	if recommendation.Status != RecommendationProposed && recommendation.Status != RecommendationAccepted {
		return RecommendationAcceptance{}, false, ErrVersionConflict
	}
	if recommendation.Status == RecommendationProposed && recommendation.Version != expected {
		return RecommendationAcceptance{}, false, ErrVersionConflict
	}
	if recommendation.Status == RecommendationProposed {
		plan, planErr := s.Repository.GetPlan(ctx, actor.OrganizationID, projectID, recommendation.PlanID)
		if planErr != nil {
			return RecommendationAcceptance{}, false, planErr
		}
		if !recommendationMatchesCurrentPlan(recommendation, plan) || !plan.CurrentVersion.IsPlatformConfigurationV2() {
			return RecommendationAcceptance{}, false, ErrVersionConflict
		}
		if strings.TrimSpace(plan.TourRunID) == "" {
			return RecommendationAcceptance{}, false, ErrInvalidState
		}
	}
	if recommendation.BaseConfiguration.CanonicalHash != recommendation.BaseSnapshotHash || recommendation.TargetConfiguration.CanonicalHash != recommendation.TargetSnapshotHash {
		return RecommendationAcceptance{}, false, ErrApprovalContentMismatch
	}
	if err := recommendation.BaseConfiguration.validateStructure(); err != nil {
		return RecommendationAcceptance{}, false, ErrApprovalContentMismatch
	}
	if err := recommendation.TargetConfiguration.validateStructure(); err != nil {
		return RecommendationAcceptance{}, false, ErrApprovalContentMismatch
	}
	changeSetID, err := s.idGenerator()("deliverychangeset")
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	now := s.now()
	requestHash, err := contract.CanonicalJSONHash(struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}{id, expected})
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	changeSet := ChangeSet{
		ID: changeSetID, OrganizationID: actor.OrganizationID, ProjectID: projectID, PlanID: recommendation.PlanID,
		PlanVersion: int64(recommendation.PlanVersion), Status: ChangeSetDraft, RiskLevel: "low", PreflightNotes: []string{},
		TargetSnapshot: cloneJSONPointer(recommendation.TargetConfiguration), TargetSnapshotHash: recommendation.TargetSnapshotHash,
		RecommendationID: recommendation.ID, RuntimeStatus: PlanRuntimeActive, Source: SourceMock, Scenario: ScenarioPlatformConfiguration,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	accepted, replay, err := repository.AcceptRecommendation(ctx, recommendation, key, requestHash, changeSet)
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	accepted.ChangeSet, err = s.hydrateChangeSet(ctx, actor.OrganizationID, projectID, accepted.ChangeSet)
	if err != nil {
		return RecommendationAcceptance{}, false, err
	}
	return accepted, replay, nil
}

func recommendationMatchesCurrentPlan(recommendation DeliveryRecommendation, plan DeliveryPlan) bool {
	return recommendation.PlanID == plan.ID && recommendation.PlanVersion == plan.CurrentVersionNumber
}

func (s Service) RejectRecommendation(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string, expected int64) (DeliveryRecommendation, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return DeliveryRecommendation{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return DeliveryRecommendation{}, err
	}
	repository, err := s.configurationWorkflow()
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	recommendation, err := repository.GetRecommendation(ctx, actor.OrganizationID, projectID, id)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	if recommendation.ReadOnly || recommendation.BaseConfiguration == nil || recommendation.TargetConfiguration == nil {
		return DeliveryRecommendation{}, ErrLegacyConfigurationUnsupported
	}
	plan, err := s.Repository.GetPlan(ctx, actor.OrganizationID, projectID, recommendation.PlanID)
	if err != nil {
		return DeliveryRecommendation{}, err
	}
	if strings.TrimSpace(plan.TourRunID) == "" {
		return DeliveryRecommendation{}, ErrInvalidState
	}
	return repository.RejectRecommendation(ctx, actor.OrganizationID, projectID, id, expected, actor.Principal.ID, s.now())
}

func (s Service) GetManualActionPackage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, changeSetID string) (ManualActionPackage, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return ManualActionPackage{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return ManualActionPackage{}, err
	}
	repository, ok := s.Repository.(legacyManualActionPackageRepository)
	if !ok {
		return ManualActionPackage{}, ErrUnsupportedConfigurationWorkflow
	}
	value, err := repository.GetManualActionPackage(ctx, actor.OrganizationID, projectID, changeSetID)
	if err != nil {
		return ManualActionPackage{}, err
	}
	value.RuntimeStatus, value.ReadOnly = PlanRuntimeLegacyUnsupported, true
	return value, nil
}
