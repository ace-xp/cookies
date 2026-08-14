package delivery

import (
	"reflect"
	"testing"
	"time"
)

func TestOutcomeSimulationIsDeterministicAndCausallySensitive(t *testing.T) {
	completedAt := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	input := OutcomeSimulationInput{
		PlanID: "plan_1", PlanVersion: 4, PlanCanonicalHash: "plan-hash", Budget: Budget{TotalMinor: 300000, Currency: "CNY"},
		Schedule:  Schedule{StartAt: completedAt, EndAt: completedAt.Add(10 * 24 * time.Hour), Timezone: "Asia/Shanghai"},
		Objective: "conversion", OptimizationGoal: "purchase", BidMinor: 3000, Audience: "high-intent",
		StrategyReference: StrategyReference{TaskID: "strategy_1", Version: 2, ContentHash: "strategy-hash"},
		CreativeFeatures:  []OutcomeCreativeFeature{{AssetID: "asset_1", Version: 3, ContentHash: "creative-hash", QualityBP: 10200}},
	}
	request := CreateOutcomeSimulationRequest{Scenario: OutcomeScenarioCostPressure, StableSeed: "stable-seed"}

	parametersA, metricsA, eventsA := simulateOutcome(input, request, "1234567890abcdef", completedAt)
	parametersB, metricsB, eventsB := simulateOutcome(input, request, "1234567890abcdef", completedAt)
	if !reflect.DeepEqual(parametersA, parametersB) || !reflect.DeepEqual(metricsA, metricsB) || !reflect.DeepEqual(eventsA, eventsB) {
		t.Fatal("same input and stable seed must produce the same parameters, windows and events")
	}
	if len(metricsA) != 3 || metricsA[2].RawMetrics.Conversions == 0 {
		t.Fatalf("cost-pressure scenario should produce three non-zero-conversion windows: %#v", metricsA)
	}

	higherBudget := input
	higherBudget.Budget.TotalMinor *= 2
	_, budgetMetrics, _ := simulateOutcome(higherBudget, request, "abcdef1234567890", completedAt)
	if budgetMetrics[0].RawMetrics.SpendCents <= metricsA[0].RawMetrics.SpendCents || budgetMetrics[0].RawMetrics.Impressions <= metricsA[0].RawMetrics.Impressions {
		t.Fatalf("higher budget must explainably increase spend and impressions: baseline=%#v higher=%#v", metricsA[0].RawMetrics, budgetMetrics[0].RawMetrics)
	}

	betterCreative := input
	betterCreative.CreativeFeatures = []OutcomeCreativeFeature{{AssetID: "asset_2", Version: 1, ContentHash: "better", QualityBP: 11400}}
	_, creativeMetrics, _ := simulateOutcome(betterCreative, request, "feedface12345678", completedAt)
	if creativeMetrics[0].RawMetrics.Clicks <= metricsA[0].RawMetrics.Clicks {
		t.Fatalf("higher creative feature must increase clicks: baseline=%d higher=%d", metricsA[0].RawMetrics.Clicks, creativeMetrics[0].RawMetrics.Clicks)
	}
}

func TestOutcomeSimulationScenariosEmitMatchingEvents(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	input := OutcomeSimulationInput{
		Budget: Budget{TotalMinor: 300000, Currency: "CNY"}, Schedule: Schedule{StartAt: now, EndAt: now.Add(10 * 24 * time.Hour)},
		BidMinor: 3000, Audience: "audience", CreativeFeatures: []OutcomeCreativeFeature{{QualityBP: 10000}},
	}
	tests := []struct {
		scenario OutcomeSimulationScenario
		event    string
	}{
		{OutcomeScenarioCostPressure, "cost_worsening"},
		{OutcomeScenarioUnderDelivery, "under_delivery"},
		{OutcomeScenarioCreativeFatigue, "creative_fatigue"},
		{OutcomeScenarioTrackingAnomaly, "tracking_anomaly"},
		{OutcomeScenarioReviewRejected, "review_rejected"},
	}
	for _, test := range tests {
		t.Run(string(test.scenario), func(t *testing.T) {
			_, metrics, events := simulateOutcome(input, CreateOutcomeSimulationRequest{Scenario: test.scenario, StableSeed: "seed"}, "1234567890abcdef", now)
			if len(events) != 1 || events[0].Type != test.event {
				t.Fatalf("expected %q event, got %#v", test.event, events)
			}
			if test.scenario == OutcomeScenarioTrackingAnomaly && metrics[2].RawMetrics.Conversions != 0 {
				t.Fatalf("tracking anomaly should suppress tracked conversions, got %d", metrics[2].RawMetrics.Conversions)
			}
		})
	}
}

func TestRecommendationMustMatchCurrentPlanVersionBeforeAcceptance(t *testing.T) {
	plan := DeliveryPlan{ID: "plan_1", CurrentVersionNumber: 4}
	current := DeliveryRecommendation{PlanID: "plan_1", PlanVersion: 4}
	if !recommendationMatchesCurrentPlan(current, plan) {
		t.Fatal("recommendation from the current PlanVersion should remain decidable")
	}
	stale := current
	stale.PlanVersion = 3
	if recommendationMatchesCurrentPlan(stale, plan) {
		t.Fatal("recommendation from an older PlanVersion must not be accepted")
	}
}
