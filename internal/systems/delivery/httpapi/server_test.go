package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/identity"
	"github.com/shikanon/cookies/internal/systems/delivery"
)

func TestDeliveryHTTPExposesPlanAndControlledActions(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		plan:      delivery.DeliveryPlan{ID: "deliveryplan_1", Version: 1},
		changeSet: delivery.ChangeSet{ID: "deliverychangeset_1", PlanID: "deliveryplan_1", Version: 1},
	}
	server := New(app)

	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/plans", `{
		"intent":{},"platform_configuration":{}
	}`)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "deliveryplan_1") {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/plans/deliveryplan_1:create-change-set", `{"expected_version":1}`)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || app.createdPlanID != "deliveryplan_1" {
		t.Fatalf("change-set status=%d body=%s plan=%q", response.Code, response.Body.String(), app.createdPlanID)
	}
}

func TestDeliveryHTTPMapsProjectIsolationDenial(t *testing.T) {
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/api/delivery/v1/projects/project_other/plans/plan_1", "")
	writeError(response, request, identity.ErrProjectAccessDenied)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "PROJECT_ACCESS_DENIED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLegacyConfigurationWriteEndpointsAreStableReadOnlyFailures(t *testing.T) {
	server := New(&applicationStub{})
	for _, path := range []string{
		"/api/delivery/v1/projects/project_1/plans/plan_1/configuration:compile",
		"/api/delivery/v1/projects/project_1/plans/plan_1/configuration:override",
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(http.MethodPost, path, `{}`))
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "LEGACY_CONFIGURATION_UNSUPPORTED") {
			t.Fatalf("path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestDeliveryHTTPMapsContractErrorsWithoutHidingTheStableCode(t *testing.T) {
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/plans", `{}`)
	writeError(response, request, &delivery.DeliveryContractError{Code: delivery.ContractErrorCanonicalHashMismatch, Field: "canonical_hash", Message: "mismatch"})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), delivery.ContractErrorCanonicalHashMismatch) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestApprovalRequestRejectsInjectedIdentityAndScopeFields(t *testing.T) {
	t.Parallel()
	server := New(&applicationStub{
		changeSet: delivery.ChangeSet{ID: "deliverychangeset_1", Version: 2},
	})
	fields := []string{"actor", "role", "approver", "scope"}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := authenticatedRequest(
				http.MethodPost,
				"/api/delivery/v1/projects/project_1/change-sets/deliverychangeset_1:approve",
				`{"expected_version":2,"`+field+`":"forged"}`,
			)
			server.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("injected %s status=%d body=%s", field, response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"source":"mock"`) ||
				!strings.Contains(response.Body.String(), `"scenario":"invalid_request"`) {
				t.Fatalf("injected %s response lacks mock provenance: %s", field, response.Body.String())
			}
		})
	}
}

func TestRejectChangeSetHTTPRequiresReasonAndReturnsDurableDecision(t *testing.T) {
	t.Parallel()
	server := New(&applicationStub{changeSet: delivery.ChangeSet{ID: "deliverychangeset_1", Version: 2}})

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/change-sets/deliverychangeset_1:reject", `{"expected_version":2,"reason":""}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty reason status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/change-sets/deliverychangeset_1:reject", `{"expected_version":2,"reason":"needs revision"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"rejected"`) || !strings.Contains(response.Body.String(), `"rejection_reason":"needs revision"`) {
		t.Fatalf("reject status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeliveryHTTPMapsStableApprovalErrorsWithMockProvenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "required", err: delivery.ErrApprovalRequired, status: http.StatusConflict, code: "APPROVAL_REQUIRED"},
		{name: "expired", err: delivery.ErrApprovalExpired, status: http.StatusConflict, code: "APPROVAL_EXPIRED"},
		{name: "content mismatch", err: delivery.ErrApprovalContentMismatch, status: http.StatusConflict, code: "APPROVAL_CONTENT_MISMATCH"},
		{name: "scope exceeded", err: delivery.ErrApprovalScopeExceeded, status: http.StatusForbidden, code: "APPROVAL_SCOPE_EXCEEDED"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/change-sets/change_1:execute", `{"expected_version":3}`)
			writeError(response, request, testCase.err)
			if response.Code != testCase.status ||
				!strings.Contains(response.Body.String(), `"`+testCase.code+`"`) ||
				!strings.Contains(response.Body.String(), `"source":"mock"`) ||
				!strings.Contains(response.Body.String(), `"scenario":"`+strings.ToLower(testCase.code)+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestExecutionHTTPRequiresIdempotencyKeyAndCreates(t *testing.T) {
	server := New(&applicationStub{changeSet: delivery.ChangeSet{ID: "change_1", Version: 2}})
	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/change-sets/change_1:execute", `{"expected_version":2,"scenario":"success"}`)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d body=%s", response.Code, response.Body.String())
	}
	request = authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/change-sets/change_1:execute", `{"expected_version":2,"scenario":"success"}`)
	request.Header.Set("Idempotency-Key", "key_1")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeliveryTourHTTPUsesStableActionRoutes(t *testing.T) {
	t.Parallel()
	app := &applicationStub{tourRun: delivery.DeliveryTourRun{ID: "investor-tour-01", Status: delivery.TourRunPrepared}}
	server := New(app)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/tour-runs/investor-tour-01:prepare", ""))
	if response.Code != http.StatusCreated || app.tourRunID != "investor-tour-01" || !strings.Contains(response.Body.String(), `"status":"prepared"`) {
		t.Fatalf("prepare status=%d run=%q body=%s", response.Code, app.tourRunID, response.Body.String())
	}

	app.tourReplay = true
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/tour-runs/investor-tour-01:prepare", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/delivery/v1/projects/project_1/tour-runs/investor-tour-01", ""))
	if response.Code != http.StatusOK || app.tourRunID != "investor-tour-01" {
		t.Fatalf("get status=%d run=%q body=%s", response.Code, app.tourRunID, response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/tour-runs/investor-tour-01:reset", ""))
	if response.Code != http.StatusOK || app.tourRunID != "investor-tour-01" {
		t.Fatalf("reset status=%d run=%q body=%s", response.Code, app.tourRunID, response.Body.String())
	}
}

func TestDeliveryTourHTTPMapsOwnerMismatch(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/tour-runs/investor-tour-01:reset", "")
	writeError(response, request, delivery.ErrTourOwnerMismatch)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "TOUR_OWNER_MISMATCH") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDecisionWorkflowHTTPStopsAtReadyForFinalApproval(t *testing.T) {
	app := &applicationStub{
		decision:  delivery.DeliveryDecision{ID: "decision_1", SchemaVersion: delivery.DeliveryDecisionSchemaV1},
		selection: delivery.DecisionSelection{ID: "selection_1", Workflow: delivery.CompiledDeliveryWorkflow{Status: "ready_for_final_approval", RemoteWriteEnabled: false}},
	}
	server := New(app)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/plans/plan_1/decisions:generate", `{"expected_version":1}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"schema_version":"delivery-decision/v1"`) {
		t.Fatalf("generate status=%d body=%s", response.Code, response.Body.String())
	}

	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/decisions/decision_1:select", `{"candidate_id":"decision_1-balanced","expected_plan_version":1}`)
	request.Header.Set("Idempotency-Key", "decision-selection-1")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"status":"ready_for_final_approval"`) || !strings.Contains(response.Body.String(), `"remote_write_enabled":false`) {
		t.Fatalf("select status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestObservatoryHTTPExposesReplayAndAuditableFeedback(t *testing.T) {
	app := &applicationStub{
		observatoryRun:      delivery.DeliveryObservatoryRun{ID: "observatory_1", SchemaVersion: delivery.ObservatoryRunSchemaV1, Source: delivery.ObservatorySourceReplay, Status: "completed", Outcome: "drift_detected", RemoteWriteEnabled: false},
		observatoryFeedback: delivery.DeliveryObservatoryFeedback{ID: "feedback_1", SchemaVersion: delivery.ObservatoryFeedbackSchemaV1, Disposition: delivery.ObservatoryFeedbackAccepted},
	}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/decision-selections/selection_1/observatory-runs", `{"source":"replay","mode":"observe_existing","fixture":{"fixture_id":"fixture_1","data_state":"ready","observed_at":"2026-08-12T08:00:00Z","data_through":"2026-08-12T07:55:00Z","observed_values":{},"selector_matches":{},"evidence_refs":["replay://fixture/1"],"page_refs":[]}}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"outcome":"drift_detected"`) || !strings.Contains(response.Body.String(), `"remote_write_enabled":false`) {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}

	request := authenticatedRequest(http.MethodPost, "/api/delivery/v1/projects/project_1/observatory-runs/observatory_1/feedback", `{"disposition":"accepted","reason":"reviewed evidence","diff_keys":[]}`)
	request.Header.Set("Idempotency-Key", "feedback-1")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"disposition":"accepted"`) {
		t.Fatalf("feedback status=%d body=%s", response.Code, response.Body.String())
	}
}

func authenticatedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := contract.WithRequestContext(request.Context(), contract.RequestContext{
		RequestID: "req_1", TraceID: "trace_1",
		Actor: contract.ActorContext{
			OrganizationID: "org_1", Principal: contract.Principal{Kind: contract.PrincipalUser, ID: "user_1"},
			Scopes: []contract.Scope{delivery.ScopeRead, delivery.ScopeWrite, delivery.ScopeApprove, delivery.ScopeExecute},
		},
	})
	return request.WithContext(ctx)
}

type applicationStub struct {
	plan                delivery.DeliveryPlan
	changeSet           delivery.ChangeSet
	createdPlanID       string
	tourRun             delivery.DeliveryTourRun
	tourRunID           string
	tourReplay          bool
	decision            delivery.DeliveryDecision
	selection           delivery.DecisionSelection
	observatoryRun      delivery.DeliveryObservatoryRun
	observatoryFeedback delivery.DeliveryObservatoryFeedback
}

func (s *applicationStub) GenerateDecision(context.Context, contract.ActorContext, contract.ProjectID, string, int) (delivery.DeliveryDecision, error) {
	return s.decision, nil
}
func (s *applicationStub) ListDecisions(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.DeliveryDecision, error) {
	return []delivery.DeliveryDecision{s.decision}, nil
}
func (s *applicationStub) GetDecision(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.DeliveryDecision, error) {
	return s.decision, nil
}
func (s *applicationStub) SelectDecision(context.Context, contract.ActorContext, contract.ProjectID, string, string, delivery.SelectDecisionRequest) (delivery.DecisionSelection, bool, error) {
	return s.selection, false, nil
}
func (s *applicationStub) GetDecisionSelection(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.DecisionSelection, error) {
	return s.selection, nil
}
func (s *applicationStub) RunObservatory(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.RunObservatoryRequest) (delivery.DeliveryObservatoryRun, bool, error) {
	return s.observatoryRun, false, nil
}
func (s *applicationStub) ListObservatoryRuns(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.DeliveryObservatoryRun, error) {
	return []delivery.DeliveryObservatoryRun{s.observatoryRun}, nil
}
func (s *applicationStub) GetObservatoryRun(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.DeliveryObservatoryRun, error) {
	return s.observatoryRun, nil
}
func (s *applicationStub) SubmitObservatoryFeedback(context.Context, contract.ActorContext, contract.ProjectID, string, string, delivery.SubmitObservatoryFeedbackRequest) (delivery.DeliveryObservatoryFeedback, bool, error) {
	return s.observatoryFeedback, false, nil
}
func (s *applicationStub) ListObservatoryFeedback(context.Context, contract.ActorContext, contract.ProjectID, string, int) ([]delivery.DeliveryObservatoryFeedback, error) {
	return []delivery.DeliveryObservatoryFeedback{s.observatoryFeedback}, nil
}

func (s *applicationStub) CreatePlan(context.Context, contract.ActorContext, contract.ProjectID, delivery.CreatePlanRequest) (delivery.DeliveryPlan, error) {
	return s.plan, nil
}
func (s *applicationStub) UpdatePlan(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.UpdatePlanRequest) (delivery.DeliveryPlan, error) {
	return s.plan, nil
}
func (s *applicationStub) ListPlans(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.DeliveryPlan, error) {
	return []delivery.DeliveryPlan{s.plan}, nil
}
func (s *applicationStub) GetPlan(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.DeliveryPlan, error) {
	return s.plan, nil
}
func (s *applicationStub) ListPlanVersions(context.Context, contract.ActorContext, contract.ProjectID, string) ([]delivery.DeliveryPlanVersion, error) {
	return s.plan.Versions, nil
}
func (s *applicationStub) GetPlanVersion(context.Context, contract.ActorContext, contract.ProjectID, string, int) (delivery.DeliveryPlanVersion, error) {
	return s.plan.CurrentVersion, nil
}
func (s *applicationStub) RunPlanPreflight(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.PreflightResult, error) {
	return delivery.PreflightResult{PlanID: s.plan.ID, Source: delivery.SourceMock}, nil
}
func (s *applicationStub) GetPlanDetail(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.PlanDetail, error) {
	return delivery.PlanDetail{Plan: s.plan}, nil
}
func (s *applicationStub) ListChangeSets(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.ChangeSet, error) {
	return []delivery.ChangeSet{s.changeSet}, nil
}
func (s *applicationStub) GetChangeSet(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.ChangeSet, error) {
	return s.changeSet, nil
}
func (s *applicationStub) CreateChangeSet(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, planID string, _ int64) (delivery.ChangeSet, error) {
	s.createdPlanID = planID
	return s.changeSet, nil
}
func (s *applicationStub) Preflight(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (delivery.ChangeSet, error) {
	return s.changeSet, nil
}
func (s *applicationStub) Approve(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (delivery.ChangeSet, error) {
	return s.changeSet, nil
}

func (s *applicationStub) RejectChangeSet(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.RejectChangeSetRequest) (delivery.ChangeSet, error) {
	return delivery.ChangeSet{ID: "changeset-rejected", Status: delivery.ChangeSetRejected, Version: 2, RejectionReason: "needs revision"}, nil
}
func (s *applicationStub) Execute(context.Context, contract.ActorContext, contract.ProjectID, string, string, delivery.ExecuteRequest) (delivery.ExecutionResult, bool, error) {
	now := time.Now()
	return delivery.ExecutionResult{ChangeSet: s.changeSet, Execution: delivery.Execution{CompletedAt: &now}}, false, nil
}
func (s *applicationStub) Rollback(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (delivery.ChangeSet, error) {
	return s.changeSet, nil
}

func (s *applicationStub) GenerateRecommendation(context.Context, contract.ActorContext, contract.ProjectID, string, int) (delivery.DeliveryRecommendation, error) {
	return delivery.DeliveryRecommendation{}, nil
}
func (s *applicationStub) ListRecommendations(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.DeliveryRecommendation, error) {
	return nil, nil
}
func (s *applicationStub) GetRecommendation(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.DeliveryRecommendation, error) {
	return delivery.DeliveryRecommendation{}, nil
}
func (s *applicationStub) AcceptRecommendation(context.Context, contract.ActorContext, contract.ProjectID, string, string, int64) (delivery.RecommendationAcceptance, bool, error) {
	return delivery.RecommendationAcceptance{}, false, nil
}
func (s *applicationStub) RejectRecommendation(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (delivery.DeliveryRecommendation, error) {
	return delivery.DeliveryRecommendation{}, nil
}
func (s *applicationStub) GetManualActionPackage(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.ManualActionPackage, error) {
	return delivery.ManualActionPackage{}, nil
}
func (s *applicationStub) ListExecutions(context.Context, contract.ActorContext, contract.ProjectID, int) ([]delivery.ExecutionResult, error) {
	return nil, nil
}
func (s *applicationStub) GetExecution(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.ExecutionResult, error) {
	return delivery.ExecutionResult{}, nil
}
func (s *applicationStub) CreateOutcomeSimulation(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.CreateOutcomeSimulationRequest) (delivery.OutcomeSimulationResult, error) {
	return delivery.OutcomeSimulationResult{Run: delivery.OutcomeSimulationRun{ID: "deliverysimulationrun_1"}}, nil
}
func (s *applicationStub) GetLatestOutcomeSimulation(context.Context, contract.ActorContext, contract.ProjectID, string) (delivery.OutcomeSimulationResult, error) {
	return delivery.OutcomeSimulationResult{Run: delivery.OutcomeSimulationRun{ID: "deliverysimulationrun_1"}, Replay: true}, nil
}
func (s *applicationStub) CreateDemoMetricSnapshot(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.CreateMetricSnapshotRequest) (delivery.DeliveryMetricSnapshot, error) {
	return delivery.DeliveryMetricSnapshot{ID: "deliverymetric_1", IsSimulated: true}, nil
}
func (s *applicationStub) ListMetricSnapshots(context.Context, contract.ActorContext, contract.ProjectID, string, int) ([]delivery.DeliveryMetricSnapshot, error) {
	return []delivery.DeliveryMetricSnapshot{{ID: "deliverymetric_1", IsSimulated: true}}, nil
}
func (s *applicationStub) EvaluateAlerts(context.Context, contract.ActorContext, contract.ProjectID, delivery.EvaluateAlertsRequest) (delivery.EvaluateAlertsResponse, error) {
	return delivery.EvaluateAlertsResponse{Items: []delivery.DeliveryAlert{}}, nil
}
func (s *applicationStub) ListAlerts(context.Context, contract.ActorContext, contract.ProjectID, delivery.AlertFilter) ([]delivery.DeliveryAlert, error) {
	return []delivery.DeliveryAlert{}, nil
}
func (s *applicationStub) UpdateAlert(context.Context, contract.ActorContext, contract.ProjectID, string, delivery.UpdateAlertRequest) (delivery.DeliveryAlert, error) {
	return delivery.DeliveryAlert{}, nil
}
func (s *applicationStub) PrepareTourRun(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, runID string) (delivery.DeliveryTourRun, bool, error) {
	s.tourRunID = runID
	return s.tourRun, s.tourReplay, nil
}
func (s *applicationStub) GetTourRun(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, runID string) (delivery.DeliveryTourRun, error) {
	s.tourRunID = runID
	return s.tourRun, nil
}
func (s *applicationStub) ResetTourRun(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, runID string) (delivery.DeliveryTourResetResult, error) {
	s.tourRunID = runID
	return delivery.DeliveryTourResetResult{Run: s.tourRun}, nil
}
