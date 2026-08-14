package delivery

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type Source string
type Scenario string
type CheckSeverity string

const (
	SourceMock Source = "mock"

	DeliveryPlanVersionSchemaV2  = "delivery-plan-version/v2"
	PlanRuntimeActive            = "active"
	PlanRuntimeCapabilityPending = "capability_pending"
	PlanRuntimeLegacyUnsupported = "legacy_unsupported"

	ScenarioGoldenPath            Scenario = "golden_path"
	ScenarioBudgetZero            Scenario = "budget_zero"
	ScenarioCreativeUnconfirmed   Scenario = "creative_unconfirmed"
	ScenarioTrackingMissing       Scenario = "tracking_missing"
	ScenarioIncompleteDraft       Scenario = "incomplete_draft"
	ScenarioPlanList              Scenario = "project_plan_list"
	ScenarioApprovalQueue         Scenario = "approval_queue"
	ScenarioPlatformConfiguration Scenario = "platform_configuration"
	ScenarioCapabilityPending     Scenario = "capability_pending"

	CheckSeverityError   CheckSeverity = "error"
	CheckSeverityWarning CheckSeverity = "warning"
)

type MockAdvertiser struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	Source   Source   `json:"source"`
	Scenario Scenario `json:"scenario"`
}

type AdvertiserInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type Budget struct {
	TotalMinor int64  `json:"total_minor"`
	Currency   string `json:"currency"`
}

type Schedule struct {
	StartAt  time.Time `json:"start_at"`
	EndAt    time.Time `json:"end_at"`
	Timezone string    `json:"timezone"`
}

type Tracking struct {
	LandingPage     string `json:"landing_page"`
	PixelID         string `json:"pixel_id"`
	ConversionEvent string `json:"conversion_event"`
}

type CreativeReference struct {
	AssetID     string `json:"asset_id"`
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash,omitempty"`
	Route       string `json:"route,omitempty"`
	Confirmed   bool   `json:"confirmed"`
}

// StrategyReference is a resolvable forward reference to the immutable
// upstream task version used to author a delivery plan.
type StrategyReference struct {
	TaskID      string `json:"task_id"`
	Version     int64  `json:"version"`
	ContentHash string `json:"content_hash,omitempty"`
	Route       string `json:"route,omitempty"`
}

type PlanDraft struct {
	Name                  string              `json:"name"`
	Objective             string              `json:"objective"`
	Advertiser            AdvertiserInput     `json:"advertiser"`
	Budget                Budget              `json:"budget"`
	Schedule              Schedule            `json:"schedule"`
	Tracking              Tracking            `json:"tracking"`
	CreativeReferences    []CreativeReference `json:"creative_references"`
	StrategyReference     StrategyReference   `json:"strategy_reference"`
	SourceStrategyVersion string              `json:"source_strategy_version"`
}

type DeliveryPlanVersion struct {
	SchemaVersion          string                  `json:"schema_version,omitempty"`
	RuntimeStatus          string                  `json:"runtime_status,omitempty"`
	ReadOnly               bool                    `json:"read_only"`
	PlanID                 string                  `json:"plan_id"`
	OrganizationID         contract.OrganizationID `json:"organization_id"`
	ProjectID              contract.ProjectID      `json:"project_id"`
	VersionNumber          int                     `json:"version_number"`
	CanonicalHash          string                  `json:"canonical_hash"`
	DeliveryIntent         *DeliveryIntent         `json:"intent,omitempty"`
	PlatformConfiguration  *PlatformConfiguration  `json:"platform_configuration,omitempty"`
	Name                   string                  `json:"name,omitempty"`
	Objective              string                  `json:"objective,omitempty"`
	Advertiser             MockAdvertiser          `json:"advertiser,omitempty"`
	Budget                 Budget                  `json:"budget,omitempty"`
	Schedule               Schedule                `json:"schedule,omitempty"`
	Tracking               Tracking                `json:"tracking,omitempty"`
	CreativeReferences     []CreativeReference     `json:"creative_references,omitempty"`
	StrategyReference      StrategyReference       `json:"strategy_reference,omitempty"`
	SourceStrategyVersion  string                  `json:"source_strategy_version,omitempty"`
	Platform               string                  `json:"platform"`
	Source                 Source                  `json:"source"`
	Scenario               Scenario                `json:"scenario"`
	CreatedBy              contract.Principal      `json:"created_by"`
	CreatedAt              time.Time               `json:"created_at"`
	ThreeTierConfiguration *ThreeTierConfiguration `json:"three_tier_configuration,omitempty"`
}

func (v DeliveryPlanVersion) IsPlatformConfigurationV2() bool {
	return v.SchemaVersion == DeliveryPlanVersionSchemaV2 && v.DeliveryIntent != nil && v.PlatformConfiguration != nil
}

func (v DeliveryPlanVersion) IsLegacy() bool { return !v.IsPlatformConfigurationV2() }

type UpdatePlanRequest struct {
	ExpectedVersion       int                    `json:"expected_version"`
	Intent                *DeliveryIntent        `json:"intent,omitempty"`
	PlatformConfiguration *PlatformConfiguration `json:"platform_configuration,omitempty"`
}

type RepairTarget struct {
	Field   string `json:"field"`
	Section string `json:"section"`
	Label   string `json:"label"`
}

type PreflightCheck struct {
	Code     string        `json:"code"`
	Severity CheckSeverity `json:"severity"`
	Passed   bool          `json:"passed"`
	Message  string        `json:"message"`
	Repair   *RepairTarget `json:"repair"`
}

type PreflightResult struct {
	PlanID      string           `json:"plan_id"`
	PlanVersion int              `json:"plan_version"`
	Passed      bool             `json:"passed"`
	Blocked     bool             `json:"blocked"`
	Checks      []PreflightCheck `json:"checks"`
	Source      Source           `json:"source"`
	Scenario    Scenario         `json:"scenario"`
	CheckedAt   time.Time        `json:"checked_at"`
}

type PlanList struct {
	Items    []DeliveryPlan `json:"items"`
	Source   Source         `json:"source"`
	Scenario Scenario       `json:"scenario"`
}

type PlanVersionList struct {
	Items    []DeliveryPlanVersion `json:"items"`
	Source   Source                `json:"source"`
	Scenario Scenario              `json:"scenario"`
}

func (request UpdatePlanRequest) Validate() error {
	if request.ExpectedVersion < 1 {
		return fmt.Errorf("expected_version must be at least 1")
	}
	if request.Intent == nil || request.PlatformConfiguration == nil {
		return fmt.Errorf("intent and platform_configuration are both required")
	}
	return nil
}

func cloneVersion(version DeliveryPlanVersion) DeliveryPlanVersion {
	version.CreativeReferences = append([]CreativeReference(nil), version.CreativeReferences...)
	version.ThreeTierConfiguration = cloneThreeTierConfiguration(version.ThreeTierConfiguration)
	version.DeliveryIntent = cloneJSONPointer(version.DeliveryIntent)
	version.PlatformConfiguration = cloneJSONPointer(version.PlatformConfiguration)
	return version
}

func cloneJSONPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned T
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil
	}
	return &cloned
}
