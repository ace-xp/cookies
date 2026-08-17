package creative

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const BrandAudioDirectorVersion = "brand-audio-director/v1"

type BrandPronunciation struct {
	Term     string `json:"term"`
	SpokenAs string `json:"spoken_as"`
	Reason   string `json:"reason"`
}

type AudioDirectorDecision struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	TargetID   string  `json:"target_id"`
	Summary    string  `json:"summary"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Editable   bool    `json:"editable"`
}

type AudioSemanticCheck struct {
	ID         string `json:"id"`
	ShotID     string `json:"shot_id"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Evidence   string `json:"evidence"`
	Suggestion string `json:"suggestion,omitempty"`
}

type SelectBrandAudioVariantRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	VariantID        string `json:"variant_id"`
}

func directBrandAudioBlueprint(plan BrandFilmPlanVersion, blueprint AudioBlueprintVersion) AudioBlueprintVersion {
	blueprint.PlannerVersion = BrandAudioDirectorVersion
	blueprint.Status = "suggested"
	allText := plan.Title + "\n" + plan.StorySummary
	for _, shot := range plan.Shots {
		allText += "\n" + shot.Voiceover + "\n" + shot.OnScreenText
	}
	for _, item := range []BrandPronunciation{
		{Term: "娇兰", SpokenAs: "jiāo lán", Reason: "品牌名统一发音"},
		{Term: "25X", SpokenAs: "二十五倍", Reason: "数字与字母按 Brief 口播规范转换"},
		{Term: "蜂皇水", SpokenAs: "fēng huáng shuǐ", Reason: "产品名统一重音"},
	} {
		if strings.Contains(allText, item.Term) || (item.Term == "25X" && strings.Contains(allText, "二十五倍")) {
			blueprint.Pronunciations = append(blueprint.Pronunciations, item)
		}
	}
	for index := range blueprint.NarrationCues {
		cue := &blueprint.NarrationCues[index]
		cue.AvailableDurationMS = max(0, cue.EndMS-cue.StartMS-350)
		cue.EstimatedDurationMS = estimateMandarinNarrationDuration(cue.Text, blueprint.VoiceProfile.Speed)
		cue.FitStatus = "fits"
		if cue.EstimatedDurationMS > cue.AvailableDurationMS {
			cue.FitStatus = "overrun"
			cue.SuggestedText = compactNarrationSuggestion(cue.Text)
		} else if cue.AvailableDurationMS-cue.EstimatedDurationMS > 1800 {
			cue.FitStatus = "spacious"
		}
		blueprint.Decisions = append(blueprint.Decisions, AudioDirectorDecision{
			ID: fmt.Sprintf("decision_voice_%02d", index+1), Kind: "narration_placement", TargetID: cue.ID,
			Summary: fmt.Sprintf("旁白预计 %.1f 秒，安排在 %.1f–%.1f 秒", float64(cue.EstimatedDurationMS)/1000, float64(cue.StartMS)/1000, float64(cue.EndMS)/1000),
			Reason:  "保留镜头起点的视觉呼吸，并在切镜前留出淡出空间", Confidence: .88, Editable: true,
		})
	}
	blueprint.Decisions = append(blueprint.Decisions, AudioDirectorDecision{ID: "decision_music_ducking", Kind: "music_ducking", TargetID: "track_music", Summary: "旁白出现时自动压低 BGM", Reason: "保证品牌口播清晰，旁白结束后平滑恢复；可通过音乐轨总音量间接调整", Confidence: .96, Editable: false})
	for index, cue := range blueprint.SoundEffectCues {
		blueprint.Decisions = append(blueprint.Decisions, AudioDirectorDecision{ID: fmt.Sprintf("decision_sfx_%02d", index+1), Kind: "beat_snap", TargetID: cue.ID, Summary: fmt.Sprintf("%s 吸附到镜头边界 %.1f 秒", cue.Label, float64(cue.StartMS)/1000), Reason: "用声音重音强化切镜和产品露出", Confidence: .84, Editable: false})
	}
	for index, shot := range plan.Shots {
		spokenProduct := containsAnyAudioTerm(shot.Voiceover, "娇兰", "蜂皇水", "25X", "二十五倍")
		visibleProduct := containsAnyAudioTerm(shot.Visual+shot.Purpose+shot.OnScreenText, "产品", "瓶", "娇兰", "蜂皇水", "25X", "Logo", "品牌")
		status, summary, suggestion := "pass", "旁白与当前镜头语义一致", ""
		if spokenProduct && !visibleProduct {
			status, summary, suggestion = "warning", "口播提到品牌或产品，但画面证据不足", "在该镜头补充产品、瓶身或品牌标识露出"
		}
		blueprint.SemanticChecks = append(blueprint.SemanticChecks, AudioSemanticCheck{ID: fmt.Sprintf("semantic_shot_%02d", index+1), ShotID: shot.ID, Status: status, Summary: summary, Evidence: shot.Visual, Suggestion: suggestion})
	}
	return blueprint
}

// directBrandSoundDesignBlueprint explains an AI sound-design plan without
// introducing narration, voice profiles, or TTS-specific constraints.
func directBrandSoundDesignBlueprint(plan BrandFilmPlanVersion, blueprint AudioBlueprintVersion) AudioBlueprintVersion {
	blueprint.PlannerVersion = "brand-sound-design-director/v2"
	blueprint.Status = "suggested"
	blueprint.Decisions = append(blueprint.Decisions,
		AudioDirectorDecision{ID: "decision_source_audio", Kind: "source_audio_policy", TargetID: "track_source_audio", Summary: "原视频声音默认按声音意图处理", Reason: "避免 Seedance 随机原声破坏统一品牌声音设计", Confidence: .95, Editable: true},
		AudioDirectorDecision{ID: "decision_music_arc", Kind: "music_arc", TargetID: "track_music", Summary: "品牌音乐覆盖全片并在结尾收束", Reason: blueprint.MusicArc.Direction, Confidence: .9, Editable: true},
		AudioDirectorDecision{ID: "decision_key_sfx_ducking", Kind: "key_sfx_ducking", TargetID: "track_music", Summary: "关键镜头音效出现时，品牌音乐自动让位", Reason: "保留产品露出、转场和品牌定格的声音重音，避免音乐遮盖关键质感", Confidence: .92, Editable: true},
	)
	for index, cue := range blueprint.SoundEffectCues {
		blueprint.Decisions = append(blueprint.Decisions, AudioDirectorDecision{ID: fmt.Sprintf("decision_sound_%02d", index+1), Kind: "cue_placement", TargetID: cue.ID, Summary: fmt.Sprintf("%s 安排在 %.1f–%.1f 秒", cue.Label, float64(cue.StartMS)/1000, float64(cue.EndMS)/1000), Reason: cue.Reason, Confidence: .84, Editable: true})
	}
	for index, shot := range plan.Shots {
		status, summary, suggestion := "pass", "镜头与声音事件已有语义对应", ""
		if strings.TrimSpace(shot.Visual) == "" || strings.TrimSpace(shot.Purpose) == "" {
			status, summary, suggestion = "warning", "镜头缺少足够的画面或目的信息，音效语义可能不准确", "补充画面与镜头目的后重新生成声音事件"
		}
		blueprint.SemanticChecks = append(blueprint.SemanticChecks, AudioSemanticCheck{ID: fmt.Sprintf("sound_semantic_shot_%02d", index+1), ShotID: shot.ID, Status: status, Summary: summary, Evidence: shot.Visual, Suggestion: suggestion})
	}
	return blueprint
}

func estimateMandarinNarrationDuration(text string, speed float64) int {
	if speed <= 0 {
		speed = 1
	}
	units := 0.0
	for _, char := range []rune(strings.TrimSpace(text)) {
		switch {
		case unicode.Is(unicode.Han, char), unicode.IsLetter(char), unicode.IsDigit(char):
			units++
		case strings.ContainsRune("，、：；", char):
			units += .7
		case strings.ContainsRune("。！？", char):
			units += 1.1
		}
	}
	if units == 0 {
		return 450
	}
	return int((350 + units*245) / speed)
}

func compactNarrationSuggestion(text string) string {
	result := strings.NewReplacer("法国", "", "真正", "", "立即", "", "全新", "").Replace(strings.TrimSpace(text))
	if len([]rune(result)) > 18 {
		result = string([]rune(result)[:18]) + "…"
	}
	return strings.TrimSpace(result)
}

func containsAnyAudioTerm(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func buildImmersiveWaterVariant(source AudioMixVersion, visual contract.AssetVersionRef, now time.Time) (AudioMixVariant, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return AudioMixVariant{}, err
	}
	var mix AudioMixVersion
	if err := json.Unmarshal(raw, &mix); err != nil {
		return AudioMixVariant{}, err
	}
	variantID := "audio_variant_immersive_water_zh_cn"
	mix.ID, mix.VariantID, mix.ChangeSummary = fmt.Sprintf("audio_mix_immersive_%02d", source.Revision), variantID, "AI 声音导演：沉浸水感版"
	for index := range mix.Tracks {
		switch mix.Tracks[index].Type {
		case BrandAudioTrackMusic:
			mix.Tracks[index].GainDB = -14
		case BrandAudioTrackAmbience:
			mix.Tracks[index].GainDB = -16
		case BrandAudioTrackSFX:
			mix.Tracks[index].GainDB = -4
		}
	}
	mix.ContentHash = ""
	hash, err := contract.CanonicalJSONHash(mix)
	if err != nil {
		return AudioMixVariant{}, err
	}
	mix.ContentHash = "sha256:" + hash
	return AudioMixVariant{ID: variantID, Label: "沉浸水感版", VisualPreview: visual, VariantType: "tone", Language: "zh-CN", StylePreset: "immersive_water", SourceVariantID: source.VariantID, MixVersions: []AudioMixVersion{mix}, ActiveMixRevision: mix.Revision, Status: mix.Status}, nil
}

func buildYouthfulLightVariant(source AudioMixVersion, visual contract.AssetVersionRef, now time.Time) (AudioMixVariant, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return AudioMixVariant{}, err
	}
	var mix AudioMixVersion
	if err := json.Unmarshal(raw, &mix); err != nil {
		return AudioMixVariant{}, err
	}
	variantID := "sound_treatment_youthful_light"
	mix.ID, mix.VariantID, mix.ChangeSummary = fmt.Sprintf("audio_mix_youthful_%02d", source.Revision), variantID, "AI 声音导演：年轻轻快版"
	for index := range mix.Tracks {
		switch mix.Tracks[index].Type {
		case BrandAudioTrackMusic:
			mix.Tracks[index].GainDB = -12
		case BrandAudioTrackAmbience:
			mix.Tracks[index].GainDB = -24
		case BrandAudioTrackSFX:
			mix.Tracks[index].GainDB = -3
		}
	}
	mix.ContentHash = ""
	hash, err := contract.CanonicalJSONHash(mix)
	if err != nil {
		return AudioMixVariant{}, err
	}
	mix.ContentHash = "sha256:" + hash
	return AudioMixVariant{ID: variantID, Label: "年轻轻快版", VisualPreview: visual, VariantType: "tone", Language: "und", StylePreset: "youthful_light", SourceVariantID: source.VariantID, MixVersions: []AudioMixVersion{mix}, ActiveMixRevision: mix.Revision, Status: mix.Status}, nil
}

func UpgradeBrandAudioDirector(workspace BrandAudioWorkspace, plan BrandFilmPlanVersion, now time.Time) (BrandAudioWorkspace, error) {
	if err := workspace.Validate(); err != nil {
		return BrandAudioWorkspace{}, err
	}
	if err := validateBrandAudioSourcePlan(plan); err != nil {
		return BrandAudioWorkspace{}, err
	}
	raw, err := json.Marshal(workspace)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var next BrandAudioWorkspace
	if err := json.Unmarshal(raw, &next); err != nil {
		return BrandAudioWorkspace{}, err
	}
	latest := next.BlueprintVersions[len(next.BlueprintVersions)-1]
	if latest.PlannerVersion == BrandAudioDirectorVersion {
		return next, nil
	}
	latest.Revision++
	latest.CreatedAt, latest.ContentHash = now, ""
	latest.Pronunciations, latest.Decisions, latest.SemanticChecks = nil, nil, nil
	latest = directBrandAudioBlueprint(plan, latest)
	hash, err := contract.CanonicalJSONHash(latest)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	latest.ContentHash = "sha256:" + hash
	next.BlueprintVersions = append(next.BlueprintVersions, latest)
	if !hasBrandAudioVariant(next.Variants, "audio_variant_immersive_water_zh_cn") {
		immersive, err := buildImmersiveWaterVariant(*next.CurrentMix(), next.VisualPreview, now)
		if err != nil {
			return BrandAudioWorkspace{}, err
		}
		next.Variants = append(next.Variants, immersive)
	}
	next.UpdatedAt = now
	return next, next.Validate()
}

func hasBrandAudioVariant(variants []AudioMixVariant, variantID string) bool {
	for _, variant := range variants {
		if variant.ID == variantID {
			return true
		}
	}
	return false
}

func SelectBrandAudioMixVariant(workspace BrandAudioWorkspace, variantID string, now time.Time) (BrandAudioWorkspace, error) {
	if err := workspace.Validate(); err != nil {
		return BrandAudioWorkspace{}, err
	}
	if strings.TrimSpace(variantID) == "" || now.IsZero() {
		return BrandAudioWorkspace{}, fmt.Errorf("audio variant selection is incomplete")
	}
	raw, err := json.Marshal(workspace)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var next BrandAudioWorkspace
	if err := json.Unmarshal(raw, &next); err != nil {
		return BrandAudioWorkspace{}, err
	}
	for index := range next.Variants {
		if next.Variants[index].ID != variantID {
			continue
		}
		next.ActiveVariantID = variantID
		next.ActiveRevision = next.Variants[index].ActiveMixRevision
		next.MixedPreview, next.FinalMixedAsset, next.UpdatedAt = nil, nil, now
		if mix := next.CurrentMix(); mix != nil {
			next.Status = mix.Status
		}
		return next, next.Validate()
	}
	return BrandAudioWorkspace{}, fmt.Errorf("audio variant %s not found", variantID)
}

func (s Service) SelectBrandFilmAudioVariant(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request SelectBrandAudioVariantRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	if request.ExpectedRevision != detail.VideoDraft.Revision {
		return TaskDetail{}, ErrVersionConflict
	}
	if detail.VideoDraft.BrandFilm.Audio == nil {
		return TaskDetail{}, ErrInvalidState
	}
	now := s.now()
	audio, err := SelectBrandAudioMixVariant(*detail.VideoDraft.BrandFilm.Audio, request.VariantID, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage = next.Revision, BrandFilmAudioDraft
	next.BrandFilm.Audio, next.BrandFilm.UpdatedAt = &audio, now
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}
