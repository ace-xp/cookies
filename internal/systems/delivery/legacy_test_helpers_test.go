package delivery

import (
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// versionFromDraft exists only in tests that verify frozen historical decoding
// and hashes. Production code has no constructor for a legacy PlanVersion.
func versionFromDraft(plan DeliveryPlan, versionNumber int, draft PlanDraft, actor contract.Principal, now time.Time) (DeliveryPlanVersion, error) {
	scenario := scenarioForHistoricalFixture(draft)
	version := DeliveryPlanVersion{
		PlanID: plan.ID, OrganizationID: plan.OrganizationID, ProjectID: plan.ProjectID,
		VersionNumber: versionNumber, Name: strings.TrimSpace(draft.Name), Objective: strings.TrimSpace(draft.Objective),
		Advertiser: MockAdvertiser{ID: strings.TrimSpace(draft.Advertiser.ID), Name: strings.TrimSpace(draft.Advertiser.Name), Platform: strings.TrimSpace(draft.Advertiser.Platform), Source: SourceMock, Scenario: scenario},
		Budget:     draft.Budget, Schedule: draft.Schedule, Tracking: draft.Tracking,
		CreativeReferences: append([]CreativeReference(nil), draft.CreativeReferences...), StrategyReference: draft.StrategyReference,
		SourceStrategyVersion: draft.SourceStrategyVersion, Platform: plan.Platform, Source: SourceMock, Scenario: scenario,
		CreatedBy: actor, CreatedAt: now,
	}
	hash, err := PlanCanonicalHash(version)
	if err != nil {
		return DeliveryPlanVersion{}, err
	}
	version.CanonicalHash = hash
	return version, nil
}

func scenarioForHistoricalFixture(draft PlanDraft) Scenario {
	if draft.Budget.TotalMinor == 0 {
		return ScenarioBudgetZero
	}
	if strings.TrimSpace(draft.Tracking.LandingPage) == "" || strings.TrimSpace(draft.Tracking.PixelID) == "" || strings.TrimSpace(draft.Tracking.ConversionEvent) == "" {
		return ScenarioTrackingMissing
	}
	for _, reference := range draft.CreativeReferences {
		if !reference.Confirmed {
			return ScenarioCreativeUnconfirmed
		}
	}
	if strings.TrimSpace(draft.Advertiser.ID) == "" || len(draft.CreativeReferences) == 0 {
		return ScenarioIncompleteDraft
	}
	return ScenarioGoldenPath
}
