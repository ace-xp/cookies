package creative

import (
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const (
	PerformanceModeViralRemake = "viral_remake"
	ManualViralRemakeRouteID   = "route_manual_viral_remake_v1"
)

type ViralRemakeStatus string

const (
	ViralWaitingForAnalysis ViralRemakeStatus = "waiting_for_analysis"
	ViralAnalysisReady      ViralRemakeStatus = "analysis_ready"
	ViralGenerationReady    ViralRemakeStatus = "generation_ready"
	ViralGenerating         ViralRemakeStatus = "generating"
	ViralCandidateReady     ViralRemakeStatus = "candidate_ready"
	ViralProviderFailed     ViralRemakeStatus = "provider_failed"
	ViralReadyForReview     ViralRemakeStatus = "ready_for_review"
)

type RightsStatus string

const (
	RightsPending   RightsStatus = "pending"
	RightsConfirmed RightsStatus = "confirmed"
)

type ManualViralRemakeInput struct {
	ProductName          string                    `json:"product_name"`
	SellingPoints        []string                  `json:"selling_points"`
	UserInstruction      string                    `json:"user_instruction"`
	ReferenceVideo       contract.AssetVersionRef  `json:"reference_video"`
	ReferenceImage       *contract.AssetVersionRef `json:"reference_image,omitempty"`
	ReferenceVideoRights RightsStatus              `json:"reference_video_rights"`
	ReferenceImageRights RightsStatus              `json:"reference_image_rights,omitempty"`
}

func (i ManualViralRemakeInput) Validate() error {
	if strings.TrimSpace(i.ProductName) == "" || len(i.ProductName) > 300 ||
		strings.TrimSpace(i.UserInstruction) == "" || len(i.UserInstruction) > 2000 {
		return fmt.Errorf("manual viral remake product_name and user_instruction are required")
	}
	if err := validateStringList("selling_points", i.SellingPoints, 12, 300); err != nil {
		return err
	}
	if err := i.ReferenceVideo.Validate(); err != nil {
		return fmt.Errorf("reference_video: %w", err)
	}
	if i.ReferenceImage != nil {
		if err := i.ReferenceImage.Validate(); err != nil {
			return fmt.Errorf("reference_image: %w", err)
		}
	}
	if i.ReferenceVideoRights != RightsPending && i.ReferenceVideoRights != RightsConfirmed {
		return fmt.Errorf("reference_video_rights must be pending or confirmed")
	}
	if i.ReferenceImage != nil && i.ReferenceImageRights != RightsPending && i.ReferenceImageRights != RightsConfirmed {
		return fmt.Errorf("reference_image_rights must be pending or confirmed")
	}
	return nil
}

type ViralRemakeInputSnapshot struct {
	Source               IntakeSource              `json:"source"`
	SelectedRouteID      string                    `json:"selected_route_id"`
	ReferenceVideo       contract.AssetVersionRef  `json:"reference_video"`
	ReferenceImage       *contract.AssetVersionRef `json:"reference_image,omitempty"`
	ProductName          string                    `json:"product_name"`
	SellingPoints        []string                  `json:"selling_points"`
	CallToAction         string                    `json:"call_to_action"`
	UserInstruction      string                    `json:"user_instruction"`
	MandatoryElements    []string                  `json:"mandatory_elements"`
	ProhibitedClaims     []string                  `json:"prohibited_claims"`
	ReferenceVideoRights RightsStatus              `json:"reference_video_rights"`
	ReferenceImageRights RightsStatus              `json:"reference_image_rights,omitempty"`
}

type CreativeReadiness struct {
	PlanningReady   bool     `json:"planning_ready"`
	GenerationReady bool     `json:"generation_ready"`
	ProductionReady bool     `json:"production_ready"`
	MissingFields   []string `json:"missing_fields"`
	Blockers        []string `json:"blockers"`
}

type ViralRemakeDraft struct {
	ContractVersion string                   `json:"contract_version"`
	TaskID          string                   `json:"task_id"`
	Revision        int64                    `json:"revision"`
	Status          ViralRemakeStatus        `json:"status"`
	SelectedRouteID string                   `json:"selected_route_id"`
	InputSnapshot   ViralRemakeInputSnapshot `json:"input_snapshot"`
	InputHash       string                   `json:"input_hash"`
	Readiness       CreativeReadiness        `json:"readiness"`
	Analysis        *ViralAnalysisSnapshot   `json:"analysis_snapshot,omitempty"`
	PromptDraft     *ViralPromptDraft        `json:"prompt_draft,omitempty"`
	PromptPackage   *ViralPromptPackage      `json:"prompt_package,omitempty"`
	Candidates      []ViralCandidate         `json:"candidates"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

func (d ViralRemakeDraft) Validate() error {
	if d.ContractVersion != "creative-viral-remake-draft/v1" || strings.TrimSpace(d.TaskID) == "" ||
		d.Revision < 1 || !validViralStatus(d.Status) || d.SelectedRouteID != ManualViralRemakeRouteID ||
		strings.TrimSpace(d.InputHash) == "" || !d.Readiness.PlanningReady || d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() {
		return fmt.Errorf("viral remake draft is incomplete")
	}
	if d.Candidates == nil {
		return fmt.Errorf("viral remake candidates must be an array")
	}
	return nil
}

func validViralStatus(status ViralRemakeStatus) bool {
	switch status {
	case ViralWaitingForAnalysis, ViralAnalysisReady, ViralGenerationReady, ViralGenerating,
		ViralCandidateReady, ViralProviderFailed, ViralReadyForReview:
		return true
	default:
		return false
	}
}

type ViralPromptDimensionID string

const (
	ViralTaskGoalType          ViralPromptDimensionID = "task_goal_type"
	ViralQualityStyleLighting  ViralPromptDimensionID = "quality_style_lighting"
	ViralEnvironmentAtmosphere ViralPromptDimensionID = "environment_atmosphere"
	ViralCameraContent         ViralPromptDimensionID = "camera_content"
	ViralMusicSound            ViralPromptDimensionID = "music_sound"
)

var viralDimensionOrder = []ViralPromptDimensionID{
	ViralTaskGoalType,
	ViralQualityStyleLighting,
	ViralEnvironmentAtmosphere,
	ViralCameraContent,
	ViralMusicSound,
}

type ViralAnalysisDimension struct {
	ID           ViralPromptDimensionID `json:"id"`
	Prompt       string                 `json:"prompt"`
	EvidenceRefs []string               `json:"evidence_refs"`
	Confidence   float64                `json:"confidence"`
	Source       string                 `json:"source"`
}

type ViralModelLineage struct {
	ModelAlias      string `json:"model_alias"`
	RouteRevisionID string `json:"route_revision_id"`
	PromptVersion   string `json:"prompt_version"`
}

type ViralAnalysisSnapshot struct {
	ContractVersion string                   `json:"contract_version"`
	TaskID          string                   `json:"task_id"`
	SourceAssetRef  contract.AssetVersionRef `json:"source_asset_ref"`
	Dimensions      []ViralAnalysisDimension `json:"dimensions"`
	PreserveRules   []string                 `json:"preserve_rules"`
	ReplaceRules    []string                 `json:"replace_rules"`
	Transcript      string                   `json:"transcript,omitempty"`
	Confidence      float64                  `json:"confidence"`
	EvidenceRefs    []string                 `json:"evidence_refs"`
	ModelLineage    ViralModelLineage        `json:"model_lineage"`
	ContentHash     string                   `json:"content_hash"`
	CreatedAt       time.Time                `json:"created_at"`
}

type ViralPromptDraft struct {
	Revision        int64                             `json:"revision"`
	Dimensions      map[ViralPromptDimensionID]string `json:"dimensions"`
	CompositePrompt string                            `json:"composite_prompt"`
	UpdatedAt       time.Time                         `json:"updated_at"`
}

type ViralGenerationSpec struct {
	ModelAlias         string                  `json:"model_alias"`
	DurationSeconds    int                     `json:"duration_seconds"`
	AspectRatio        string                  `json:"aspect_ratio"`
	Resolution         string                  `json:"resolution"`
	CandidateCount     int                     `json:"candidate_count"`
	ReferenceImageMode ViralReferenceImageMode `json:"reference_image_mode"`
}

// ViralReferenceImageMode records whether a frozen generation package sends a
// visual reference to the model. The source asset remains traceable in the
// input snapshot even when a policy-safe retry must omit it from submission.
type ViralReferenceImageMode string

const (
	ViralReferenceImageModeReferenceImage         ViralReferenceImageMode = "reference_image"
	ViralReferenceImageModeTextOnly               ViralReferenceImageMode = "text_only"
	ViralReferenceImageModeTextOnlyOriginalPerson ViralReferenceImageMode = "text_only_original_person"
)

type ViralPromptPackage struct {
	ContractVersion      string                            `json:"contract_version"`
	TaskID               string                            `json:"task_id"`
	PromptVersion        int64                             `json:"prompt_version"`
	AnalysisSnapshotHash string                            `json:"analysis_snapshot_hash"`
	InputSnapshotHash    string                            `json:"input_snapshot_hash"`
	Dimensions           map[ViralPromptDimensionID]string `json:"dimensions"`
	PreserveRules        []string                          `json:"preserve_rules"`
	ReplaceRules         []string                          `json:"replace_rules"`
	ProductFacts         []string                          `json:"product_facts"`
	NegativeConstraints  []string                          `json:"negative_constraints"`
	GenerationSpec       ViralGenerationSpec               `json:"generation_spec"`
	CompositePrompt      string                            `json:"composite_prompt"`
	ContentHash          string                            `json:"content_hash"`
	ConfirmedBy          string                            `json:"confirmed_by"`
	ConfirmedAt          time.Time                         `json:"confirmed_at"`
}

type ViralCandidateStatus string

const (
	ViralCandidateQueued    ViralCandidateStatus = "queued"
	ViralCandidateRunning   ViralCandidateStatus = "running"
	ViralCandidateSucceeded ViralCandidateStatus = "succeeded"
	ViralCandidateFailed    ViralCandidateStatus = "failed"
	ViralCandidateReviewed  ViralCandidateStatus = "reviewed"
)

type ViralCandidateCheck struct {
	Code    string `json:"code"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type ViralCandidate struct {
	ID             string                    `json:"id"`
	ProviderJobID  string                    `json:"provider_job_id"`
	PromptHash     string                    `json:"prompt_hash"`
	Status         ViralCandidateStatus      `json:"status"`
	OutputAssetRef *contract.AssetVersionRef `json:"output_asset_ref,omitempty"`
	Checks         []ViralCandidateCheck     `json:"checks"`
	ErrorCode      string                    `json:"error_code,omitempty"`
	ErrorMessage   string                    `json:"error_message,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}
