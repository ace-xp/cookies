package delivery

import (
	"testing"
	"time"
)

func TestFrozenHistoricalThreeTierHashRemainsStable(t *testing.T) {
	configuration := &ThreeTierConfiguration{
		Schema: ThreeTierSchema, Source: SourceMock, Scenario: ScenarioGoldenPath, FixtureScenario: ScenarioGoldenPath,
		GeneratedAt: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC), Evidence: []string{"fixture://historical"},
		Groups: []ThreeTierGroup{{ID: "group-1", Name: "Historical group", Fields: []ThreeTierField{{Key: "budget", Label: "Budget", Effective: ThreeTierValue{Type: "integer", Value: float64(300000)}, EffectiveSource: "historical"}}}},
	}
	hash, err := legacyThreeTierSnapshotHash(configuration)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "c2f548fe0c50b8752f5f88171543dc3ea5f04f03430df95c348cb734c1808b07"
	if hash != expected {
		t.Fatalf("frozen historical snapshot hash = %q", hash)
	}
}

func TestFrozenHistoricalPlanHashStillBindsThreeTierSnapshot(t *testing.T) {
	version := canonicalTestVersion(t)
	withoutConfiguration, err := PlanCanonicalHash(version)
	if err != nil {
		t.Fatal(err)
	}
	version.ThreeTierConfiguration = &ThreeTierConfiguration{Schema: ThreeTierSchema, Source: SourceMock, Scenario: ScenarioGoldenPath, FixtureScenario: ScenarioGoldenPath, Groups: []ThreeTierGroup{}}
	withConfiguration, err := PlanCanonicalHash(version)
	if err != nil {
		t.Fatal(err)
	}
	if withoutConfiguration == withConfiguration {
		t.Fatal("historical configuration must remain bound to the frozen plan hash")
	}
}
