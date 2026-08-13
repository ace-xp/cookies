package delivery

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestObservatoryServicePersistsIdempotentRunsAndAppendOnlyFeedback(t *testing.T) {
	service, actor := newTestService()
	repository := service.Repository.(*memoryRepository)
	selection := validObservatorySelection(t)
	selection.OrganizationID, selection.ProjectID = actor.OrganizationID, "project_a"
	selection.Workflow.OrganizationID, selection.Workflow.ProjectID = actor.OrganizationID, "project_a"
	repository.selections[repositoryKey(actor.OrganizationID, "project_a", selection.ID)] = selection
	request := validObservatoryRequest(selection, ObservatoryModeObserveExisting)

	first, replay, err := service.RunObservatory(context.Background(), actor, "project_a", selection.ID, request)
	if err != nil || replay {
		t.Fatalf("first run replay=%t err=%v", replay, err)
	}
	second, replay, err := service.RunObservatory(context.Background(), actor, "project_a", selection.ID, request)
	if err != nil || !replay || second.ID != first.ID {
		t.Fatalf("second run=%#v replay=%t err=%v", second, replay, err)
	}

	accepted := SubmitObservatoryFeedbackRequest{Disposition: ObservatoryFeedbackAccepted, Reason: "evidence reviewed", DiffKeys: []string{}}
	feedback, replay, err := service.SubmitObservatoryFeedback(context.Background(), actor, "project_a", first.ID, "feedback-key", accepted)
	if err != nil || replay || feedback.RunCanonicalHash != first.CanonicalHash {
		t.Fatalf("feedback=%#v replay=%t err=%v", feedback, replay, err)
	}
	_, replay, err = service.SubmitObservatoryFeedback(context.Background(), actor, "project_a", first.ID, "feedback-key", accepted)
	if err != nil || !replay {
		t.Fatalf("feedback replay=%t err=%v", replay, err)
	}

	modifiedConfiguration := cloneJSONPointer(&selection.Configuration)
	modifiedConfiguration.Payload.OceanEngine.Project.BudgetAndBidding.DailyBudgetMinor++
	modifiedConfiguration.CanonicalHash = ""
	modified := SubmitObservatoryFeedbackRequest{Disposition: ObservatoryFeedbackModified, Reason: "operator retained reviewed final configuration", DiffKeys: []string{"prepare-project-local-form.daily_budget_minor"}, FinalConfiguration: modifiedConfiguration}
	modifiedFeedback, replay, err := service.SubmitObservatoryFeedback(context.Background(), actor, "project_a", first.ID, "feedback-modified-key", modified)
	if err != nil || replay || modifiedFeedback.FinalConfiguration == nil || modifiedFeedback.FinalConfigurationCanonicalHash == selection.Configuration.CanonicalHash {
		t.Fatalf("modified=%#v replay=%t err=%v", modifiedFeedback, replay, err)
	}
	rejected := SubmitObservatoryFeedbackRequest{Disposition: ObservatoryFeedbackRejected, Reason: "drift requires a new decision", DiffKeys: []string{"prepare-project-local-form.daily_budget_minor"}}
	rejectedFeedback, replay, err := service.SubmitObservatoryFeedback(context.Background(), actor, "project_a", first.ID, "feedback-rejected-key", rejected)
	if err != nil || replay || rejectedFeedback.CreatedBy != actor.Principal.ID || rejectedFeedback.RunOutcome != first.Outcome {
		t.Fatalf("rejected=%#v replay=%t err=%v", rejectedFeedback, replay, err)
	}
	listed, err := service.ListObservatoryFeedback(context.Background(), actor, "project_a", first.ID, 20)
	if err != nil || len(listed) != 3 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestBuildObservatoryRunIsDeterministicAndStopsAtRemoteWriteBoundary(t *testing.T) {
	selection := validObservatorySelection(t)
	request := validObservatoryRequest(selection, ObservatoryModeObserveExisting)
	first, err := BuildObservatoryRun(selection, request, "operator-1", time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildObservatoryRun(selection, request, "operator-2", time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.InputHash != second.InputHash || first.CanonicalHash != second.CanonicalHash || !reflect.DeepEqual(first.Steps, second.Steps) {
		t.Fatal("same frozen selection and fixture must produce one deterministic run identity and result")
	}
	if first.Outcome != "in_sync" || first.RemoteWriteEnabled {
		t.Fatalf("unexpected run outcome: %#v", first)
	}
	last := first.Steps[len(first.Steps)-1]
	if last.ExecutedAction != WorkflowRiskObserve || last.Status != "blocked" || last.BlockReason != PhaseCRemoteWriteProhibited {
		t.Fatalf("run crossed the Phase C write boundary: %#v", last)
	}
	for _, step := range first.Steps {
		if step.ExecutedAction == WorkflowRiskRemoteWrite {
			t.Fatalf("remote write was represented as executable: %#v", step)
		}
	}
}

func TestBuildObservatoryRunDistinguishesDriftDataBlockAndRunnerFailure(t *testing.T) {
	selection := validObservatorySelection(t)
	drift := validObservatoryRequest(selection, ObservatoryModeObserveExisting)
	for key := range drift.Fixture.ObservedValues {
		drift.Fixture.ObservedValues[key] = "platform-drift"
		break
	}
	driftRun, err := BuildObservatoryRun(selection, drift, "operator-1", time.Now())
	if err != nil || driftRun.Status != "completed" || driftRun.Outcome != "drift_detected" {
		t.Fatalf("drift result=%#v err=%v", driftRun, err)
	}

	blocked := validObservatoryRequest(selection, ObservatoryModeObserveExisting)
	blocked.Fixture.DataState = ObservatoryDataStale
	blocked.Fixture.DataStateReason = "fixture is older than the policy window"
	blockedRun, err := BuildObservatoryRun(selection, blocked, "operator-1", time.Now())
	if err != nil || blockedRun.Status != "blocked" || blockedRun.Outcome != "stale_data" {
		t.Fatalf("data gate result=%#v err=%v", blockedRun, err)
	}

	failed := validObservatoryRequest(selection, ObservatoryModePrepareNew)
	failed.Fixture.FailureStepID = selection.Workflow.Steps[1].ID
	failedRun, err := BuildObservatoryRun(selection, failed, "operator-1", time.Now())
	if err != nil || failedRun.Status != "runner_failed" || failedRun.Outcome != "runner_failure" {
		t.Fatalf("runner failure result=%#v err=%v", failedRun, err)
	}
	if blockedRun.CanonicalHash == driftRun.CanonicalHash || failedRun.CanonicalHash == driftRun.CanonicalHash {
		t.Fatal("materially different evidence outcomes must not share a canonical hash")
	}
}

func TestObservatoryAllDecisionDataBlocksStopWithEvidence(t *testing.T) {
	selection := validObservatorySelection(t)
	for _, state := range []ObservatoryDataState{ObservatoryDataInsufficient, ObservatoryDataStale, ObservatoryDataBlockedByAsset, ObservatoryDataPlatformPending} {
		t.Run(string(state), func(t *testing.T) {
			request := validObservatoryRequest(selection, ObservatoryModeObserveExisting)
			request.Fixture.DataState, request.Fixture.DataStateReason = state, "controlled fixture gate"
			run, err := BuildObservatoryRun(selection, request, "operator-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != "blocked" || run.Outcome != string(state) || len(run.EvidenceRefs) == 0 || run.Steps[0].BlockReason != string(state) || run.Steps[len(run.Steps)-1].BlockReason != PhaseCRemoteWriteProhibited {
				t.Fatalf("unsafe block result: %#v", run)
			}
		})
	}
}

func TestObservatoryPrepareNewUsesOnlyLocalFormActions(t *testing.T) {
	selection := validObservatorySelection(t)
	request := validObservatoryRequest(selection, ObservatoryModePrepareNew)
	request.Fixture.ObservedValues = map[string]any{}
	run, err := BuildObservatoryRun(selection, request, "operator-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != "local_form_prepared" {
		t.Fatalf("outcome=%s", run.Outcome)
	}
	for _, step := range run.Steps {
		if step.ExecutedAction != WorkflowRiskObserve && step.ExecutedAction != WorkflowRiskPrepareLocalForm {
			t.Fatalf("unexpected authority: %#v", step)
		}
	}
}

func validObservatorySelection(t *testing.T) DecisionSelection {
	t.Helper()
	decision, err := BuildDeliveryDecision(validDecisionEngineInput(t))
	if err != nil {
		t.Fatal(err)
	}
	candidate := decision.Candidates[1]
	workflow, err := CompileDeliveryWorkflow("workflow-1", decision, candidate, "operator-1", time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	selection := DecisionSelection{
		ID: "selection-1", OrganizationID: decision.OrganizationID, ProjectID: decision.ProjectID, DecisionID: decision.ID, DecisionCanonicalHash: decision.CanonicalHash,
		CandidateID: candidate.ID, Configuration: candidate.TargetConfiguration, Workflow: workflow,
		FinalApprovalBinding: FinalApprovalBinding{Status: "ready_for_final_approval", Action: "remote_write", PlanCanonicalHash: decision.Inputs.PlanCanonicalHash, IntentCanonicalHash: decision.Inputs.IntentCanonicalHash, DecisionCanonicalHash: decision.CanonicalHash, ConfigurationCanonicalHash: candidate.TargetConfiguration.CanonicalHash, WorkflowCanonicalHash: workflow.CanonicalHash},
		CreatedBy:            "operator-1", CreatedAt: time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC),
	}
	if err := selection.Validate(); err != nil {
		t.Fatal(err)
	}
	return selection
}

func validObservatoryRequest(selection DecisionSelection, mode ObservatoryMode) RunObservatoryRequest {
	values := map[string]any{}
	selectors := map[string][]string{}
	for _, step := range selection.Workflow.Steps {
		selectors[step.ID] = []string{"fixture://selector/" + step.ID}
		for _, field := range step.Fields {
			values[step.ID+"."+field.Key] = field.ExpectedReadback
		}
	}
	return RunObservatoryRequest{Source: ObservatorySourceReplay, Mode: mode, Fixture: ObservatoryFixture{
		FixtureID: "fixture-1", DataState: ObservatoryDataReady, ObservedAt: time.Date(2026, 8, 12, 7, 0, 0, 0, time.UTC), DataThrough: time.Date(2026, 8, 12, 6, 55, 0, 0, time.UTC),
		ObservedValues: values, SelectorMatches: selectors, EvidenceRefs: []string{"replay://fixture/fixture-1", "screenshot://fixture/page-1"}, PageRefs: []string{"replay://page/project"},
	}}
}
