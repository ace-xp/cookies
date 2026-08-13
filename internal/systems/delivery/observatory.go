package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const (
	ObservatoryRunSchemaV1      = "delivery-observatory-run/v1"
	ObservatoryRunnerV1         = "mock-replay-observatory-runner/v1"
	ObservatoryFeedbackSchemaV1 = "delivery-observatory-feedback/v1"
	PhaseCRemoteWriteProhibited = "PHASE_C_REMOTE_WRITE_PROHIBITED"
)

type ObservatorySource string

const (
	ObservatorySourceMock   ObservatorySource = "mock"
	ObservatorySourceReplay ObservatorySource = "replay"
)

type ObservatoryMode string

const (
	ObservatoryModeObserveExisting ObservatoryMode = "observe_existing"
	ObservatoryModePrepareNew      ObservatoryMode = "prepare_new_local_form"
)

type ObservatoryDataState string

const (
	ObservatoryDataReady           ObservatoryDataState = "ready"
	ObservatoryDataInsufficient    ObservatoryDataState = "insufficient_data"
	ObservatoryDataStale           ObservatoryDataState = "stale_data"
	ObservatoryDataBlockedByAsset  ObservatoryDataState = "blocked_by_asset"
	ObservatoryDataPlatformPending ObservatoryDataState = "platform_pending"
)

type ObservatoryFixture struct {
	FixtureID       string               `json:"fixture_id"`
	DataState       ObservatoryDataState `json:"data_state"`
	DataStateReason string               `json:"data_state_reason"`
	ObservedAt      time.Time            `json:"observed_at"`
	DataThrough     time.Time            `json:"data_through"`
	ObservedValues  map[string]any       `json:"observed_values"`
	SelectorMatches map[string][]string  `json:"selector_matches"`
	EvidenceRefs    []string             `json:"evidence_refs"`
	PageRefs        []string             `json:"page_refs"`
	FailureStepID   string               `json:"failure_step_id,omitempty"`
}

type RunObservatoryRequest struct {
	Source  ObservatorySource  `json:"source"`
	Mode    ObservatoryMode    `json:"mode"`
	Fixture ObservatoryFixture `json:"fixture"`
}

func (r RunObservatoryRequest) Validate() error {
	if r.Source != ObservatorySourceMock && r.Source != ObservatorySourceReplay {
		return ErrInvalidRequest
	}
	if r.Mode != ObservatoryModeObserveExisting && r.Mode != ObservatoryModePrepareNew {
		return ErrInvalidRequest
	}
	if strings.TrimSpace(r.Fixture.FixtureID) == "" || r.Fixture.ObservedAt.IsZero() || r.Fixture.DataThrough.IsZero() || len(r.Fixture.EvidenceRefs) == 0 {
		return ErrInvalidRequest
	}
	switch r.Fixture.DataState {
	case ObservatoryDataReady, ObservatoryDataInsufficient, ObservatoryDataStale, ObservatoryDataBlockedByAsset, ObservatoryDataPlatformPending:
	default:
		return ErrInvalidRequest
	}
	if r.Fixture.DataState != ObservatoryDataReady && strings.TrimSpace(r.Fixture.DataStateReason) == "" {
		return ErrInvalidRequest
	}
	for key := range r.Fixture.ObservedValues {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidRequest
		}
	}
	return nil
}

type ObservatoryBinding struct {
	SelectionID                string `json:"selection_id"`
	DecisionID                 string `json:"decision_id"`
	DecisionCanonicalHash      string `json:"decision_canonical_hash"`
	ConfigurationID            string `json:"configuration_id"`
	ConfigurationVersion       int    `json:"configuration_version"`
	ConfigurationCanonicalHash string `json:"configuration_canonical_hash"`
	WorkflowID                 string `json:"workflow_id"`
	WorkflowCanonicalHash      string `json:"workflow_canonical_hash"`
	DecisionSchemaVersion      string `json:"decision_schema_version"`
	ConfigurationSchemaVersion string `json:"configuration_schema_version"`
	WorkflowSchemaVersion      string `json:"workflow_schema_version"`
}

type ObservatoryFieldDiff struct {
	Key           string `json:"key"`
	EvidenceRef   string `json:"evidence_ref"`
	ExpectedValue any    `json:"expected_value"`
	ObservedValue any    `json:"observed_value"`
	Matches       bool   `json:"matches"`
}

type ObservatoryStepObservation struct {
	StepID          string                 `json:"step_id"`
	Sequence        int                    `json:"sequence"`
	Page            string                 `json:"page"`
	WorkflowAction  string                 `json:"workflow_action"`
	ExecutedAction  WorkflowRisk           `json:"executed_action"`
	Status          string                 `json:"status"`
	SelectorMatches []string               `json:"selector_matches"`
	EvidenceRefs    []string               `json:"evidence_refs"`
	PageRefs        []string               `json:"page_refs"`
	Diffs           []ObservatoryFieldDiff `json:"diffs"`
	BlockReason     string                 `json:"block_reason,omitempty"`
}

type DeliveryObservatoryRun struct {
	SchemaVersion      string                       `json:"schema_version"`
	ID                 string                       `json:"id"`
	OrganizationID     contract.OrganizationID      `json:"organization_id"`
	ProjectID          contract.ProjectID           `json:"project_id"`
	RunnerVersion      string                       `json:"runner_version"`
	Source             ObservatorySource            `json:"source"`
	Mode               ObservatoryMode              `json:"mode"`
	InputHash          string                       `json:"input_hash"`
	Binding            ObservatoryBinding           `json:"binding"`
	DataState          ObservatoryDataState         `json:"data_state"`
	DataStateReason    string                       `json:"data_state_reason"`
	ObservedAt         time.Time                    `json:"observed_at"`
	DataThrough        time.Time                    `json:"data_through"`
	Status             string                       `json:"status"`
	Outcome            string                       `json:"outcome"`
	RemoteWriteEnabled bool                         `json:"remote_write_enabled"`
	Steps              []ObservatoryStepObservation `json:"steps"`
	EvidenceRefs       []string                     `json:"evidence_refs"`
	PageRefs           []string                     `json:"page_refs"`
	CanonicalHash      string                       `json:"canonical_hash"`
	CreatedBy          string                       `json:"created_by"`
	CreatedAt          time.Time                    `json:"created_at"`
}

func (r DeliveryObservatoryRun) canonicalPayload() any {
	return struct {
		SchemaVersion      string                       `json:"schema_version"`
		RunnerVersion      string                       `json:"runner_version"`
		Source             ObservatorySource            `json:"source"`
		Mode               ObservatoryMode              `json:"mode"`
		InputHash          string                       `json:"input_hash"`
		Binding            ObservatoryBinding           `json:"binding"`
		DataState          ObservatoryDataState         `json:"data_state"`
		DataStateReason    string                       `json:"data_state_reason"`
		ObservedAt         time.Time                    `json:"observed_at"`
		DataThrough        time.Time                    `json:"data_through"`
		Status             string                       `json:"status"`
		Outcome            string                       `json:"outcome"`
		RemoteWriteEnabled bool                         `json:"remote_write_enabled"`
		Steps              []ObservatoryStepObservation `json:"steps"`
		EvidenceRefs       []string                     `json:"evidence_refs"`
		PageRefs           []string                     `json:"page_refs"`
	}{r.SchemaVersion, r.RunnerVersion, r.Source, r.Mode, r.InputHash, r.Binding, r.DataState, r.DataStateReason, r.ObservedAt, r.DataThrough, r.Status, r.Outcome, r.RemoteWriteEnabled, r.Steps, r.EvidenceRefs, r.PageRefs}
}

func (r DeliveryObservatoryRun) ComputeCanonicalHash() (string, error) {
	return contract.CanonicalJSONHash(r.canonicalPayload())
}

func (r DeliveryObservatoryRun) Validate() error {
	if r.SchemaVersion != ObservatoryRunSchemaV1 || r.RunnerVersion != ObservatoryRunnerV1 || strings.TrimSpace(r.ID) == "" || r.OrganizationID == "" || r.ProjectID == "" || r.RemoteWriteEnabled || !isLowercaseSHA256(r.InputHash) {
		return ErrInvalidState
	}
	if r.Source != ObservatorySourceMock && r.Source != ObservatorySourceReplay || r.Mode != ObservatoryModeObserveExisting && r.Mode != ObservatoryModePrepareNew {
		return ErrInvalidState
	}
	b := r.Binding
	if b.SelectionID == "" || b.DecisionID == "" || b.ConfigurationID == "" || b.ConfigurationVersion < 1 || b.WorkflowID == "" || !isLowercaseSHA256(b.DecisionCanonicalHash) || !isLowercaseSHA256(b.ConfigurationCanonicalHash) || !isLowercaseSHA256(b.WorkflowCanonicalHash) || b.DecisionSchemaVersion != DeliveryDecisionSchemaV1 || b.ConfigurationSchemaVersion != PlatformConfigurationSchemaV2 || b.WorkflowSchemaVersion != CompiledDeliveryWorkflowSchemaV1 {
		return ErrApprovalContentMismatch
	}
	if len(r.EvidenceRefs) == 0 || r.ObservedAt.IsZero() || r.DataThrough.IsZero() {
		return ErrInvalidState
	}
	switch r.DataState {
	case ObservatoryDataReady:
		if r.Status != "completed" && r.Status != "runner_failed" {
			return ErrInvalidState
		}
	case ObservatoryDataInsufficient, ObservatoryDataStale, ObservatoryDataBlockedByAsset, ObservatoryDataPlatformPending:
		if r.Status != "blocked" || r.Outcome != string(r.DataState) || strings.TrimSpace(r.DataStateReason) == "" {
			return ErrInvalidState
		}
	default:
		return ErrInvalidState
	}
	if r.Status == "completed" && r.Outcome != "in_sync" && r.Outcome != "drift_detected" && r.Outcome != "local_form_prepared" || r.Status == "runner_failed" && r.Outcome != "runner_failure" {
		return ErrInvalidState
	}
	remoteBoundaries := 0
	for index, step := range r.Steps {
		if step.Sequence != index+1 {
			return ErrInvalidState
		}
		if step.ExecutedAction != WorkflowRiskObserve && step.ExecutedAction != WorkflowRiskPrepareLocalForm {
			return ErrInvalidState
		}
		if step.BlockReason == PhaseCRemoteWriteProhibited {
			remoteBoundaries++
			if index != len(r.Steps)-1 || step.ExecutedAction != WorkflowRiskObserve || step.Status != "blocked" {
				return ErrInvalidState
			}
		}
	}
	if remoteBoundaries != 1 {
		return ErrInvalidState
	}
	hash, err := r.ComputeCanonicalHash()
	if err != nil || hash != r.CanonicalHash {
		return ErrApprovalContentMismatch
	}
	return nil
}

func BuildObservatoryRun(selection DecisionSelection, request RunObservatoryRequest, actor string, now time.Time) (DeliveryObservatoryRun, error) {
	if err := selection.Validate(); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	if err := request.Validate(); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	requestHash, err := contract.CanonicalJSONHash(struct {
		SelectionID       string             `json:"selection_id"`
		DecisionHash      string             `json:"decision_hash"`
		ConfigurationHash string             `json:"configuration_hash"`
		WorkflowHash      string             `json:"workflow_hash"`
		Source            ObservatorySource  `json:"source"`
		Mode              ObservatoryMode    `json:"mode"`
		Fixture           ObservatoryFixture `json:"fixture"`
	}{selection.ID, selection.DecisionCanonicalHash, selection.Configuration.CanonicalHash, selection.Workflow.CanonicalHash, request.Source, request.Mode, request.Fixture})
	if err != nil {
		return DeliveryObservatoryRun{}, err
	}
	evidence := sortedUnique(request.Fixture.EvidenceRefs)
	pages := sortedUnique(request.Fixture.PageRefs)
	run := DeliveryObservatoryRun{
		SchemaVersion: ObservatoryRunSchemaV1, ID: "observatory_" + requestHash[:32], OrganizationID: selection.OrganizationID, ProjectID: selection.ProjectID,
		RunnerVersion: ObservatoryRunnerV1, Source: request.Source, Mode: request.Mode, InputHash: requestHash,
		Binding:   ObservatoryBinding{SelectionID: selection.ID, DecisionID: selection.DecisionID, DecisionCanonicalHash: selection.DecisionCanonicalHash, ConfigurationID: selection.Configuration.ConfigurationID, ConfigurationVersion: selection.Configuration.VersionNumber, ConfigurationCanonicalHash: selection.Configuration.CanonicalHash, WorkflowID: selection.Workflow.ID, WorkflowCanonicalHash: selection.Workflow.CanonicalHash, DecisionSchemaVersion: DeliveryDecisionSchemaV1, ConfigurationSchemaVersion: selection.Configuration.SchemaVersion, WorkflowSchemaVersion: selection.Workflow.SchemaVersion},
		DataState: request.Fixture.DataState, DataStateReason: request.Fixture.DataStateReason, ObservedAt: request.Fixture.ObservedAt.UTC(), DataThrough: request.Fixture.DataThrough.UTC(), RemoteWriteEnabled: false, EvidenceRefs: evidence, PageRefs: pages, CreatedBy: actor, CreatedAt: now.UTC(),
	}
	if request.Fixture.DataState != ObservatoryDataReady {
		run.Status, run.Outcome = "blocked", string(request.Fixture.DataState)
		run.Steps = []ObservatoryStepObservation{{StepID: "data-quality-gate", Sequence: 1, Page: "local/observatory", WorkflowAction: "validate_fixture_data", ExecutedAction: WorkflowRiskObserve, Status: "blocked", EvidenceRefs: evidence, PageRefs: pages, BlockReason: string(request.Fixture.DataState)}, remoteBoundaryObservation(2)}
	} else {
		run.Status, run.Outcome = "completed", "in_sync"
		for _, step := range selection.Workflow.Steps {
			if step.Risk == WorkflowRiskRemoteWrite {
				run.Steps = append(run.Steps, remoteBoundaryObservation(len(run.Steps)+1))
				continue
			}
			executed := WorkflowRiskObserve
			status := "observed"
			if request.Mode == ObservatoryModePrepareNew && step.Risk == WorkflowRiskPrepareLocalForm {
				executed, status = WorkflowRiskPrepareLocalForm, "prepared_locally"
			}
			observation := ObservatoryStepObservation{StepID: step.ID, Sequence: len(run.Steps) + 1, Page: step.Page, WorkflowAction: step.Action, ExecutedAction: executed, Status: status, SelectorMatches: sortedUnique(request.Fixture.SelectorMatches[step.ID]), EvidenceRefs: evidence, PageRefs: pages, Diffs: []ObservatoryFieldDiff{}}
			if request.Fixture.FailureStepID == step.ID {
				observation.Status, observation.BlockReason = "failed", "MOCK_REPLAY_RUNNER_FAILURE"
				run.Steps = append(run.Steps, observation, remoteBoundaryObservation(len(run.Steps)+2))
				run.Status, run.Outcome = "runner_failed", "runner_failure"
				break
			}
			for _, field := range step.Fields {
				key := step.ID + "." + field.Key
				observed, found := request.Fixture.ObservedValues[key]
				if request.Mode == ObservatoryModePrepareNew && !found {
					observed, found = field.Value, true
				}
				matches := found && fmt.Sprint(observed) == fmt.Sprint(field.ExpectedReadback)
				observation.Diffs = append(observation.Diffs, ObservatoryFieldDiff{Key: key, EvidenceRef: field.EvidenceRef, ExpectedValue: field.ExpectedReadback, ObservedValue: observed, Matches: matches})
				if !matches {
					run.Outcome = "drift_detected"
				}
			}
			run.Steps = append(run.Steps, observation)
		}
		if request.Mode == ObservatoryModePrepareNew && run.Status == "completed" && run.Outcome == "in_sync" {
			run.Outcome = "local_form_prepared"
		}
	}
	hash, err := run.ComputeCanonicalHash()
	if err != nil {
		return DeliveryObservatoryRun{}, err
	}
	run.CanonicalHash = hash
	if err := run.Validate(); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	return run, nil
}

func remoteBoundaryObservation(sequence int) ObservatoryStepObservation {
	return ObservatoryStepObservation{StepID: "remote-write-boundary", Sequence: sequence, Page: "oceanengine/review", WorkflowAction: "submit_platform_configuration", ExecutedAction: WorkflowRiskObserve, Status: "blocked", SelectorMatches: []string{}, EvidenceRefs: []string{}, PageRefs: []string{}, Diffs: []ObservatoryFieldDiff{}, BlockReason: PhaseCRemoteWriteProhibited}
}

func sortedUnique(values []string) []string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	result := make([]string, 0, len(copyValues))
	for _, value := range copyValues {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

type ObservatoryFeedbackDisposition string

const (
	ObservatoryFeedbackAccepted ObservatoryFeedbackDisposition = "accepted"
	ObservatoryFeedbackModified ObservatoryFeedbackDisposition = "modified"
	ObservatoryFeedbackRejected ObservatoryFeedbackDisposition = "rejected"
)

type SubmitObservatoryFeedbackRequest struct {
	Disposition        ObservatoryFeedbackDisposition `json:"disposition"`
	Reason             string                         `json:"reason"`
	DiffKeys           []string                       `json:"diff_keys"`
	FinalConfiguration *PlatformConfiguration         `json:"final_configuration,omitempty"`
}

type DeliveryObservatoryFeedback struct {
	SchemaVersion                   string                         `json:"schema_version"`
	ID                              string                         `json:"id"`
	OrganizationID                  contract.OrganizationID        `json:"organization_id"`
	ProjectID                       contract.ProjectID             `json:"project_id"`
	RunID                           string                         `json:"run_id"`
	RunCanonicalHash                string                         `json:"run_canonical_hash"`
	RunOutcome                      string                         `json:"run_outcome"`
	Disposition                     ObservatoryFeedbackDisposition `json:"disposition"`
	Reason                          string                         `json:"reason"`
	DiffKeys                        []string                       `json:"diff_keys"`
	FinalConfiguration              *PlatformConfiguration         `json:"final_configuration,omitempty"`
	FinalConfigurationCanonicalHash string                         `json:"final_configuration_canonical_hash,omitempty"`
	CanonicalHash                   string                         `json:"canonical_hash"`
	CreatedBy                       string                         `json:"created_by"`
	CreatedAt                       time.Time                      `json:"created_at"`
}

func (f DeliveryObservatoryFeedback) ComputeCanonicalHash() (string, error) {
	return contract.CanonicalJSONHash(struct {
		SchemaVersion                   string                         `json:"schema_version"`
		RunID                           string                         `json:"run_id"`
		RunCanonicalHash                string                         `json:"run_canonical_hash"`
		RunOutcome                      string                         `json:"run_outcome"`
		Disposition                     ObservatoryFeedbackDisposition `json:"disposition"`
		Reason                          string                         `json:"reason"`
		DiffKeys                        []string                       `json:"diff_keys"`
		FinalConfigurationCanonicalHash string                         `json:"final_configuration_canonical_hash,omitempty"`
	}{f.SchemaVersion, f.RunID, f.RunCanonicalHash, f.RunOutcome, f.Disposition, f.Reason, f.DiffKeys, f.FinalConfigurationCanonicalHash})
}

func (f DeliveryObservatoryFeedback) Validate() error {
	if f.SchemaVersion != ObservatoryFeedbackSchemaV1 || f.ID == "" || f.OrganizationID == "" || f.ProjectID == "" || f.RunID == "" || !isLowercaseSHA256(f.RunCanonicalHash) || strings.TrimSpace(f.RunOutcome) == "" || strings.TrimSpace(f.Reason) == "" {
		return ErrInvalidRequest
	}
	switch f.Disposition {
	case ObservatoryFeedbackAccepted, ObservatoryFeedbackRejected:
		if f.FinalConfiguration != nil {
			return ErrInvalidRequest
		}
	case ObservatoryFeedbackModified:
		if f.FinalConfiguration == nil || f.FinalConfiguration.validateStructure() != nil || f.FinalConfiguration.CanonicalHash != f.FinalConfigurationCanonicalHash {
			return ErrInvalidRequest
		}
		computed, err := f.FinalConfiguration.ComputeCanonicalHash()
		if err != nil || computed != f.FinalConfigurationCanonicalHash {
			return ErrApprovalContentMismatch
		}
	default:
		return ErrInvalidRequest
	}
	hash, err := f.ComputeCanonicalHash()
	if err != nil || hash != f.CanonicalHash {
		return ErrApprovalContentMismatch
	}
	return nil
}

type observatoryRepository interface {
	CreateObservatoryRun(context.Context, DeliveryObservatoryRun) (DeliveryObservatoryRun, bool, error)
	ListObservatoryRuns(context.Context, contract.OrganizationID, contract.ProjectID, int) ([]DeliveryObservatoryRun, error)
	GetObservatoryRun(context.Context, contract.OrganizationID, contract.ProjectID, string) (DeliveryObservatoryRun, error)
	CreateObservatoryFeedback(context.Context, DeliveryObservatoryFeedback, string, string) (DeliveryObservatoryFeedback, bool, error)
	ListObservatoryFeedback(context.Context, contract.OrganizationID, contract.ProjectID, string, int) ([]DeliveryObservatoryFeedback, error)
}

func (s Service) observatory() (observatoryRepository, error) {
	r, ok := s.Repository.(observatoryRepository)
	if !ok {
		return nil, ErrUnsupportedConfigurationWorkflow
	}
	return r, nil
}

func (s Service) RunObservatory(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, selectionID string, request RunObservatoryRequest) (DeliveryObservatoryRun, bool, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	decisionRepo, err := s.decisionWorkflows()
	if err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	repo, err := s.observatory()
	if err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	selection, err := decisionRepo.GetDecisionSelection(ctx, actor.OrganizationID, projectID, selectionID)
	if err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	run, err := BuildObservatoryRun(selection, request, actor.Principal.ID, s.now())
	if err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	return repo.CreateObservatoryRun(ctx, run)
}

func (s Service) ListObservatoryRuns(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]DeliveryObservatoryRun, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	repo, err := s.observatory()
	if err != nil {
		return nil, err
	}
	return repo.ListObservatoryRuns(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
}

func (s Service) GetObservatoryRun(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (DeliveryObservatoryRun, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	repo, err := s.observatory()
	if err != nil {
		return DeliveryObservatoryRun{}, err
	}
	return repo.GetObservatoryRun(ctx, actor.OrganizationID, projectID, id)
}

func (s Service) SubmitObservatoryFeedback(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, runID, idempotencyKey string, request SubmitObservatoryFeedbackRequest) (DeliveryObservatoryFeedback, bool, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	if strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(request.Reason) == "" {
		return DeliveryObservatoryFeedback{}, false, ErrInvalidRequest
	}
	repo, err := s.observatory()
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	run, err := repo.GetObservatoryRun(ctx, actor.OrganizationID, projectID, runID)
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	feedback := DeliveryObservatoryFeedback{SchemaVersion: ObservatoryFeedbackSchemaV1, OrganizationID: actor.OrganizationID, ProjectID: projectID, RunID: run.ID, RunCanonicalHash: run.CanonicalHash, RunOutcome: run.Outcome, Disposition: request.Disposition, Reason: strings.TrimSpace(request.Reason), DiffKeys: sortedUnique(request.DiffKeys), CreatedBy: actor.Principal.ID, CreatedAt: s.now().UTC()}
	if request.FinalConfiguration != nil {
		configuration, finalizeErr := FinalizePlatformConfiguration(*request.FinalConfiguration)
		if finalizeErr != nil {
			return DeliveryObservatoryFeedback{}, false, finalizeErr
		}
		feedback.FinalConfiguration, feedback.FinalConfigurationCanonicalHash = &configuration, configuration.CanonicalHash
		if feedback.FinalConfigurationCanonicalHash == run.Binding.ConfigurationCanonicalHash {
			return DeliveryObservatoryFeedback{}, false, ErrInvalidRequest
		}
	}
	requestHash, err := contract.CanonicalJSONHash(struct {
		RunID   string                           `json:"run_id"`
		Request SubmitObservatoryFeedbackRequest `json:"request"`
	}{run.ID, request})
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	identityHash, err := contract.CanonicalJSONHash(struct {
		RunID          string `json:"run_id"`
		Actor          string `json:"actor"`
		IdempotencyKey string `json:"idempotency_key"`
	}{run.ID, actor.Principal.ID, idempotencyKey})
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	feedback.ID = "observatoryfeedback_" + identityHash[:32]
	feedback.CanonicalHash, err = feedback.ComputeCanonicalHash()
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	if err = feedback.Validate(); err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	return repo.CreateObservatoryFeedback(ctx, feedback, idempotencyKey, requestHash)
}

func (s Service) ListObservatoryFeedback(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, runID string, limit int) ([]DeliveryObservatoryFeedback, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	repo, err := s.observatory()
	if err != nil {
		return nil, err
	}
	return repo.ListObservatoryFeedback(ctx, actor.OrganizationID, projectID, runID, normalizeLimit(limit))
}
