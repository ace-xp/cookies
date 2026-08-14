package delivery

import (
	"reflect"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestBuildDeliveryDecisionIsDeterministicAndExplainable(t *testing.T) {
	input := validDecisionEngineInput(t)
	first, err := BuildDeliveryDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildDeliveryDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash != second.CanonicalHash || !reflect.DeepEqual(first.Candidates, second.Candidates) {
		t.Fatal("same immutable inputs must produce the same decision")
	}
	if len(first.Candidates) != 3 || first.RecommendedCandidateID != "balanced" {
		t.Fatalf("unexpected candidate set: %#v", first.Candidates)
	}
	for _, candidate := range first.Candidates {
		if candidate.TargetConfiguration.ConfigurationProvenance.Kind != ConfigurationGeneratedByDecisionEngine || candidate.TargetConfiguration.CanonicalHash == "" {
			t.Fatalf("candidate is not an immutable decision-engine configuration: %#v", candidate)
		}
		for _, constraint := range candidate.Constraints {
			if !constraint.Passed {
				t.Fatalf("candidate %s violates %s", candidate.ID, constraint.Code)
			}
		}
	}
	mutated := input
	mutated.Current.RawMetrics.SpendCents++
	changed, err := BuildDeliveryDecision(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CanonicalHash == first.CanonicalHash {
		t.Fatal("decision hash must change when a bound fact changes")
	}
	differentIdentity := input
	differentIdentity.DecisionID = "decision-2"
	replayed, err := BuildDeliveryDecision(differentIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CanonicalHash != first.CanonicalHash {
		t.Fatal("decision identity must not leak into the deterministic business hash")
	}
}

func TestCompileDeliveryWorkflowHardStopsRemoteWrite(t *testing.T) {
	decision, err := BuildDeliveryDecision(validDecisionEngineInput(t))
	if err != nil {
		t.Fatal(err)
	}
	candidate := decision.Candidates[1]
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	workflow, err := CompileDeliveryWorkflow("workflow-1", decision, candidate, "operator-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.Status != "ready_for_final_approval" || workflow.RemoteWriteEnabled {
		t.Fatalf("unexpected Phase C authority boundary: %#v", workflow)
	}
	last := workflow.Steps[len(workflow.Steps)-1]
	if last.Risk != WorkflowRiskRemoteWrite || !last.Blocked || last.BlockReason != "PHASE_C_REMOTE_WRITE_PROHIBITED" {
		t.Fatalf("remote write step is not hard blocked: %#v", last)
	}
	if workflow.CanonicalHash == "" || workflow.ConfigurationCanonicalHash != candidate.TargetConfiguration.CanonicalHash {
		t.Fatal("workflow must bind the selected immutable configuration")
	}
	duplicate, err := CompileDeliveryWorkflow("workflow-1", decision, candidate, "operator-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.CanonicalHash != workflow.CanonicalHash {
		t.Fatal("compiler output must be deterministic")
	}
	differentIdentity, err := CompileDeliveryWorkflow("workflow-2", decision, candidate, "operator-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if differentIdentity.CanonicalHash != workflow.CanonicalHash {
		t.Fatal("workflow identity must not leak into the deterministic compiler hash")
	}
}

func TestBlockedDecisionDoesNotForceCandidates(t *testing.T) {
	input := validDecisionEngineInput(t)
	decision, err := BuildBlockedDeliveryDecision(input.DecisionID, input.OrganizationID, input.ProjectID, input.Plan, "insufficient_data", "two metric windows are required", "collect another metric window", input.CreatedBy, input.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Diagnostic.Code != "insufficient_data" || len(decision.Candidates) != 0 || decision.RecommendedCandidateID != "" || decision.CanonicalHash == "" {
		t.Fatalf("blocked decision forced a recommendation: %#v", decision)
	}
}

func validDecisionEngineInput(t *testing.T) DecisionEngineInput {
	t.Helper()
	intent := validDeliveryIntent(t)
	configuration := validOceanEnginePlatformConfiguration(t, intent, 1)
	plan := DeliveryPlan{
		ID: "plan-1", OrganizationID: contract.OrganizationID("org-1"), ProjectID: contract.ProjectID("project-1"), CurrentVersionNumber: 1,
		CurrentVersion: DeliveryPlanVersion{PlanID: "plan-1", OrganizationID: contract.OrganizationID("org-1"), ProjectID: contract.ProjectID("project-1"), VersionNumber: 1, CanonicalHash: configuration.CanonicalHash, DeliveryIntent: &intent, PlatformConfiguration: &configuration},
	}
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	return DecisionEngineInput{
		DecisionID: "decision-1", OrganizationID: plan.OrganizationID, ProjectID: plan.ProjectID, Plan: plan,
		Simulation: OutcomeSimulationRun{ID: "simulation-1", PlanID: plan.ID, PlanVersion: 1, InputHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Baseline:   DeliveryMetricSnapshot{ID: "metric-1", RawMetrics: RawMetrics{SpendCents: 10000, Conversions: 10}},
		Current:    DeliveryMetricSnapshot{ID: "metric-2", RawMetrics: RawMetrics{SpendCents: 15000, Conversions: 10}},
		Evidence:   []string{"simulation://metric/metric-2", "simulation://metric/metric-1"}, CreatedBy: "operator-1", CreatedAt: now,
	}
}
