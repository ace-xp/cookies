package creative

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
)

const (
	BrandAudioWorkspaceV1 = "creative-brand-audio-workspace/v1"
	BrandAudioWorkspaceV2 = "creative-brand-sound-design-workspace/v2"

	BrandFilmDurationStandard15 = "brand_standard_15"
	BrandFilmDurationStory30    = "brand_story_30"
	BrandFilmDurationCustom     = "custom"

	BrandAudioTrackSource    = "source_audio"
	BrandAudioTrackVoiceover = "voiceover"
	BrandAudioTrackAmbience  = "ambience"
	BrandAudioTrackMusic     = "music"
	BrandAudioTrackSFX       = "sfx"

	AudioMixOperationSetTrackGain   = "set_track_gain"
	AudioMixOperationSetTrackMuted  = "set_track_muted"
	AudioMixOperationReplaceClip    = "replace_clip_asset"
	AudioMixOperationSetClipTiming  = "set_clip_timing"
	AudioMixOperationSetClipLabel   = "set_clip_label"
	AudioMixOperationSetClipGain    = "set_clip_gain"
	AudioMixOperationSetClipFade    = "set_clip_fade"
	AudioMixOperationRemoveClip     = "remove_clip"
	AudioMixOperationAddFixtureClip = "add_fixture_clip"
)

type AudioMixOperation struct {
	Op              string                    `json:"op"`
	TrackID         string                    `json:"track_id,omitempty"`
	GainDB          *float64                  `json:"gain_db,omitempty"`
	Muted           *bool                     `json:"muted,omitempty"`
	ClipID          string                    `json:"clip_id,omitempty"`
	AssetRef        *contract.AssetVersionRef `json:"asset_ref,omitempty"`
	TimelineStartMS *int                      `json:"timeline_start_ms,omitempty"`
	TimelineEndMS   *int                      `json:"timeline_end_ms,omitempty"`
	Label           string                    `json:"label,omitempty"`
	FadeInMS        *int                      `json:"fade_in_ms,omitempty"`
	FadeOutMS       *int                      `json:"fade_out_ms,omitempty"`
}

type UpdateBrandAudioMixRequest struct {
	ExpectedRevision int64               `json:"expected_revision"`
	Operations       []AudioMixOperation `json:"operations"`
}

type BrandFilmDurationProfile struct {
	ID               string `json:"id"`
	MasterDurationMS int    `json:"master_duration_ms"`
	ShotCount        int    `json:"shot_count"`
}

type BrandFilmGenerationUnitPlan struct {
	Order                   int      `json:"order"`
	ShotIDs                 []string `json:"shot_ids"`
	TimelineStartMS         int      `json:"timeline_start_ms"`
	TimelineEndMS           int      `json:"timeline_end_ms"`
	ProviderDurationSeconds int      `json:"provider_duration_seconds"`
	ReasonCodes             []string `json:"reason_codes"`
}

func ResolveBrandFilmDurationProfile(durationSeconds int) (BrandFilmDurationProfile, error) {
	profile := BrandFilmDurationProfile{MasterDurationMS: durationSeconds * 1000}
	switch durationSeconds {
	case 15:
		profile.ID, profile.ShotCount = BrandFilmDurationStandard15, 3
	case 30:
		profile.ID, profile.ShotCount = BrandFilmDurationStory30, 6
	default:
		if durationSeconds < 12 {
			return BrandFilmDurationProfile{}, fmt.Errorf("custom brand film duration must be at least 12 seconds")
		}
		profile.ID = BrandFilmDurationCustom
		profile.ShotCount = (durationSeconds + 4) / 5
	}
	return profile, nil
}

func PlanBrandFilmGenerationUnits(masterDurationMS int, shots []BrandFilmShot) ([]BrandFilmGenerationUnitPlan, error) {
	if masterDurationMS <= 0 || masterDurationMS%1000 != 0 {
		return nil, fmt.Errorf("brand film duration must use whole seconds")
	}
	profile, err := ResolveBrandFilmDurationProfile(masterDurationMS / 1000)
	if err != nil {
		return nil, err
	}
	if len(shots) != profile.ShotCount {
		return nil, fmt.Errorf("brand film profile %s requires %d shots", profile.ID, profile.ShotCount)
	}
	units := make([]BrandFilmGenerationUnitPlan, 0, len(shots))
	endSecond := 0
	for index, shot := range shots {
		duration := shot.EndSecond - shot.StartSecond
		if shot.Order != index+1 || shot.StartSecond != endSecond || shot.ID == "" || duration < 4 || duration > 15 {
			return nil, fmt.Errorf("brand film shot %d cannot map to one provider generation", index+1)
		}
		units = append(units, BrandFilmGenerationUnitPlan{
			Order: index + 1, ShotIDs: []string{shot.ID},
			TimelineStartMS: shot.StartSecond * 1000, TimelineEndMS: shot.EndSecond * 1000,
			ProviderDurationSeconds: duration, ReasonCodes: []string{"one_shot_one_generation_unit"},
		})
		endSecond = shot.EndSecond
	}
	if endSecond*1000 != masterDurationMS {
		return nil, fmt.Errorf("brand film shots must cover master duration")
	}
	return units, nil
}

type BrandAudioWorkspace struct {
	ContractVersion   string                    `json:"contract_version"`
	PlanRevision      int64                     `json:"plan_revision"`
	MasterDurationMS  int                       `json:"master_duration_ms"`
	VisualPreview     contract.AssetVersionRef  `json:"visual_preview_asset_ref"`
	BlueprintVersions []AudioBlueprintVersion   `json:"blueprint_versions"`
	Variants          []AudioMixVariant         `json:"variants"`
	ActiveVariantID   string                    `json:"active_variant_id"`
	ActiveRevision    int64                     `json:"active_mix_revision"`
	MixedPreview      *contract.AssetVersionRef `json:"mixed_preview_asset_ref,omitempty"`
	FinalMixedAsset   *contract.AssetVersionRef `json:"final_mixed_asset_ref,omitempty"`
	Attempts          []AudioGenerationAttempt  `json:"generation_attempts"`
	RenderJobs        []AudioMixRenderJob       `json:"render_jobs"`
	Status            string                    `json:"status"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

type AudioBlueprintVersion struct {
	Revision          int64                   `json:"revision"`
	PlanRevision      int64                   `json:"plan_revision"`
	MasterDurationMS  int                     `json:"master_duration_ms"`
	VoiceProfile      VoiceProfileSnapshot    `json:"voice_profile"`
	SoundDesignIntent SoundDesignIntent       `json:"sound_design_intent,omitempty"`
	NarrationCues     []AudioNarrationCue     `json:"narration_cues"`
	MusicArc          AudioMusicArc           `json:"music_arc"`
	SoundEffectCues   []AudioSoundEffectCue   `json:"sound_effect_cues"`
	Pronunciations    []BrandPronunciation    `json:"pronunciations"`
	Decisions         []AudioDirectorDecision `json:"director_decisions"`
	SemanticChecks    []AudioSemanticCheck    `json:"semantic_checks"`
	PlannerVersion    string                  `json:"planner_version"`
	Status            string                  `json:"status"`
	ContentHash       string                  `json:"content_hash"`
	CreatedAt         time.Time               `json:"created_at"`
}

type VoiceProfileSnapshot struct {
	VoiceAlias string  `json:"voice_alias"`
	Language   string  `json:"language"`
	Direction  string  `json:"direction"`
	Speed      float64 `json:"speed"`
	Volume     float64 `json:"volume"`
	Pitch      int     `json:"pitch"`
	Emotion    string  `json:"emotion"`
}

type AudioNarrationCue struct {
	ID                  string  `json:"id"`
	ShotID              string  `json:"shot_id"`
	StartMS             int     `json:"start_ms"`
	EndMS               int     `json:"end_ms"`
	Text                string  `json:"text"`
	Reason              string  `json:"reason"`
	Confidence          float64 `json:"confidence"`
	EstimatedDurationMS int     `json:"estimated_duration_ms"`
	AvailableDurationMS int     `json:"available_duration_ms"`
	FitStatus           string  `json:"fit_status"`
	SuggestedText       string  `json:"suggested_text,omitempty"`
}

type AudioMusicArc struct {
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	Direction string `json:"direction"`
}

type AudioSoundEffectCue struct {
	ID             string `json:"id"`
	ShotID         string `json:"shot_id"`
	TrackType      string `json:"track_type,omitempty"`
	StartMS        int    `json:"start_ms"`
	EndMS          int    `json:"end_ms"`
	Label          string `json:"label"`
	Purpose        string `json:"purpose,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	Intensity      string `json:"intensity,omitempty"`
	Status         string `json:"status,omitempty"`
	Reason         string `json:"reason"`
}

type AudioMixVariant struct {
	ID                string                   `json:"id"`
	Label             string                   `json:"label"`
	VisualPreview     contract.AssetVersionRef `json:"visual_preview_asset_ref"`
	VariantType       string                   `json:"variant_type"`
	Language          string                   `json:"language"`
	StylePreset       string                   `json:"style_preset"`
	SourceVariantID   string                   `json:"source_variant_id,omitempty"`
	MixVersions       []AudioMixVersion        `json:"mix_versions"`
	ActiveMixRevision int64                    `json:"active_mix_revision"`
	Status            string                   `json:"status"`
}

type AudioMixVersion struct {
	ID               string                   `json:"id"`
	Revision         int64                    `json:"revision"`
	ParentRevision   int64                    `json:"parent_revision,omitempty"`
	VariantID        string                   `json:"variant_id"`
	PlanRevision     int64                    `json:"plan_revision"`
	VisualPreview    contract.AssetVersionRef `json:"visual_preview_asset_ref"`
	MasterDurationMS int                      `json:"master_duration_ms"`
	SampleRate       int                      `json:"sample_rate"`
	ChannelLayout    string                   `json:"channel_layout"`
	Tracks           []AudioTrack             `json:"tracks"`
	ContentHash      string                   `json:"content_hash"`
	CompilerVersion  string                   `json:"compiler_version"`
	Status           string                   `json:"status"`
	ChangeSummary    string                   `json:"change_summary"`
	CreatedBy        string                   `json:"created_by"`
	CreatedAt        time.Time                `json:"created_at"`
}

type AudioTrack struct {
	ID           string      `json:"id"`
	Type         string      `json:"type"`
	Role         string      `json:"role"`
	Muted        bool        `json:"muted"`
	Solo         bool        `json:"solo"`
	GainDB       float64     `json:"gain_db"`
	Locked       bool        `json:"locked"`
	RightsStatus string      `json:"rights_status"`
	Clips        []AudioClip `json:"clips"`
}

type AudioClip struct {
	ID                  string                    `json:"id"`
	TrackID             string                    `json:"track_id"`
	Order               int                       `json:"order"`
	AssetRef            *contract.AssetVersionRef `json:"asset_ref,omitempty"`
	FixtureURI          string                    `json:"fixture_uri,omitempty"`
	Label               string                    `json:"label"`
	TimelineStartMS     int                       `json:"timeline_start_ms"`
	TimelineEndMS       int                       `json:"timeline_end_ms"`
	SourceInMS          int                       `json:"source_in_ms"`
	SourceOutMS         int                       `json:"source_out_ms"`
	GainDB              float64                   `json:"gain_db"`
	FadeInMS            int                       `json:"fade_in_ms"`
	FadeOutMS           int                       `json:"fade_out_ms"`
	PlaybackRate        float64                   `json:"playback_rate"`
	NarrationSource     *NarrationSourceRef       `json:"narration_source_ref,omitempty"`
	CueRef              string                    `json:"cue_ref,omitempty"`
	GenerationAttemptID string                    `json:"generation_attempt_id,omitempty"`
	WaveformPeaks       []float64                 `json:"waveform_peaks,omitempty"`
	WordTimings         []AudioWordTiming         `json:"word_timings,omitempty"`
}

type AudioWordTiming struct {
	Text    string `json:"text"`
	BeginMS int    `json:"begin_ms"`
	EndMS   int    `json:"end_ms"`
}

type NarrationSourceRef struct {
	PlanRevision  int64  `json:"plan_revision"`
	ShotID        string `json:"shot_id"`
	VoiceoverHash string `json:"voiceover_hash"`
}

type AudioGenerationAttempt struct {
	ID               string                    `json:"id"`
	ClipID           string                    `json:"clip_id"`
	Ordinal          int                       `json:"ordinal"`
	RetryOf          string                    `json:"retry_of,omitempty"`
	Status           string                    `json:"status"`
	ProviderJobID    string                    `json:"provider_job_id,omitempty"`
	Provider         string                    `json:"provider,omitempty"`
	ProviderSnapshot string                    `json:"provider_snapshot,omitempty"`
	OutputAssetRef   *contract.AssetVersionRef `json:"output_asset_ref,omitempty"`
	FixtureMode      bool                      `json:"fixture_mode"`
	ErrorCode        string                    `json:"error_code,omitempty"`
	ErrorMessage     string                    `json:"error_message,omitempty"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

type AudioMixRenderJob struct {
	ID              string                    `json:"id"`
	TaskID          string                    `json:"creative_task_id"`
	MixRevision     int64                     `json:"mix_revision"`
	MixContentHash  string                    `json:"mix_content_hash"`
	Kind            string                    `json:"kind"`
	Status          string                    `json:"status"`
	RendererVersion string                    `json:"renderer_version"`
	CreatedBy       contract.Principal        `json:"created_by"`
	OutputAssetRef  *contract.AssetVersionRef `json:"output_asset_ref,omitempty"`
	ErrorCode       string                    `json:"error_code,omitempty"`
	ErrorMessage    string                    `json:"error_message,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

func PrepareBrandAudioFixture(plan BrandFilmPlanVersion, visual contract.AssetVersionRef, createdBy string, now time.Time) (BrandAudioWorkspace, error) {
	if err := validateBrandAudioSourcePlan(plan); err != nil {
		return BrandAudioWorkspace{}, err
	}
	if visual.Validate() != nil || strings.TrimSpace(createdBy) == "" || now.IsZero() {
		return BrandAudioWorkspace{}, fmt.Errorf("brand audio fixture input is incomplete")
	}
	masterDurationMS := plan.Shots[len(plan.Shots)-1].EndSecond * 1000
	profile := VoiceProfileSnapshot{VoiceAlias: "cookies.voice.brand.warm_female", Language: "zh-CN", Direction: plan.VoiceDirection, Speed: 1, Volume: 1, Emotion: "restrained_luxury"}
	blueprint := AudioBlueprintVersion{Revision: 1, PlanRevision: plan.Revision, MasterDurationMS: masterDurationMS, VoiceProfile: profile, MusicArc: AudioMusicArc{StartMS: 0, EndMS: masterDurationMS, Direction: plan.MusicDirection}, CreatedAt: now}
	voiceClips := make([]AudioClip, 0, len(plan.Shots))
	sfxClips := make([]AudioClip, 0, len(plan.Shots))
	for index, shot := range plan.Shots {
		startMS, endMS := shot.StartSecond*1000, shot.EndSecond*1000
		voiceHash, err := contract.CanonicalJSONHash(struct {
			Voiceover string `json:"voiceover"`
		}{Voiceover: shot.Voiceover})
		if err != nil {
			return BrandAudioWorkspace{}, err
		}
		cueID := fmt.Sprintf("narration_cue_%02d", index+1)
		blueprint.NarrationCues = append(blueprint.NarrationCues, AudioNarrationCue{ID: cueID, ShotID: shot.ID, StartMS: startMS, EndMS: endMS, Text: shot.Voiceover, Reason: "来自已确认镜头旁白", Confidence: 1})
		voiceClips = append(voiceClips, AudioClip{ID: fmt.Sprintf("voice_clip_%02d", index+1), TrackID: "track_voiceover", Order: index + 1, FixtureURI: "fixture://brand-film/guerlain/voiceover/" + shot.ID, Label: shot.Voiceover, TimelineStartMS: startMS, TimelineEndMS: endMS, SourceOutMS: endMS - startMS, PlaybackRate: 1, FadeInMS: 80, FadeOutMS: 120, NarrationSource: &NarrationSourceRef{PlanRevision: plan.Revision, ShotID: shot.ID, VoiceoverHash: "sha256:" + voiceHash}})
		sfxEnd := startMS + 600
		if sfxEnd > endMS {
			sfxEnd = endMS
		}
		sfxID := fmt.Sprintf("sfx_cue_%02d", index+1)
		blueprint.SoundEffectCues = append(blueprint.SoundEffectCues, AudioSoundEffectCue{ID: sfxID, ShotID: shot.ID, StartMS: startMS, EndMS: sfxEnd, Label: shot.Purpose + "声音重音", Reason: "镜头起点自动吸附"})
		sfxClips = append(sfxClips, AudioClip{ID: fmt.Sprintf("sfx_clip_%02d", index+1), TrackID: "track_sfx", Order: index + 1, FixtureURI: "fixture://brand-film/guerlain/sfx/" + shot.ID, Label: shot.Purpose + "声音重音", TimelineStartMS: startMS, TimelineEndMS: sfxEnd, SourceOutMS: sfxEnd - startMS, PlaybackRate: 1, FadeOutMS: 100, CueRef: sfxID})
	}
	blueprint = directBrandAudioBlueprint(plan, blueprint)
	blueprintHash, err := contract.CanonicalJSONHash(blueprint)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	blueprint.ContentHash = "sha256:" + blueprintHash
	variantID := "audio_variant_restrained_zh_cn"
	mix := AudioMixVersion{ID: "audio_mix_01", Revision: 1, VariantID: variantID, PlanRevision: plan.Revision, VisualPreview: visual, MasterDurationMS: masterDurationMS, SampleRate: 48000, ChannelLayout: "stereo", CompilerVersion: "brand-audio-mix-compiler/v1", Status: "draft", ChangeSummary: "娇兰固定音轨草稿", CreatedBy: createdBy, CreatedAt: now, Tracks: []AudioTrack{
		{ID: "track_source_audio", Type: BrandAudioTrackSource, Role: "Seedance 原声", Muted: true, GainDB: -96, RightsStatus: "generated", Clips: []AudioClip{}},
		{ID: "track_voiceover", Type: BrandAudioTrackVoiceover, Role: "画外音旁白", GainDB: 0, RightsStatus: "fixture", Clips: voiceClips},
		{ID: "track_music", Type: BrandAudioTrackMusic, Role: "品牌音乐", GainDB: -18, RightsStatus: "fixture", Clips: []AudioClip{{ID: "music_clip_01", TrackID: "track_music", Order: 1, FixtureURI: "fixture://brand-film/guerlain/music/restrained-luxury", Label: plan.MusicDirection, TimelineStartMS: 0, TimelineEndMS: masterDurationMS, SourceOutMS: masterDurationMS, PlaybackRate: 1, FadeInMS: 800, FadeOutMS: 1200}}},
		{ID: "track_sfx", Type: BrandAudioTrackSFX, Role: "镜头音效", GainDB: -8, RightsStatus: "fixture", Clips: sfxClips},
	}}
	mixHash, err := contract.CanonicalJSONHash(mix)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	mix.ContentHash = "sha256:" + mixHash
	immersive, err := buildImmersiveWaterVariant(mix, visual, now)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	workspace := BrandAudioWorkspace{ContractVersion: "creative-brand-audio-workspace/v1", PlanRevision: plan.Revision, MasterDurationMS: masterDurationMS, VisualPreview: visual, BlueprintVersions: []AudioBlueprintVersion{blueprint}, Variants: []AudioMixVariant{{ID: variantID, Label: "高级克制版", VisualPreview: visual, VariantType: "tone", Language: "zh-CN", StylePreset: "restrained_luxury", MixVersions: []AudioMixVersion{mix}, ActiveMixRevision: 1, Status: "draft"}, immersive}, ActiveVariantID: variantID, ActiveRevision: 1, Attempts: []AudioGenerationAttempt{}, RenderJobs: []AudioMixRenderJob{}, Status: "draft", UpdatedAt: now}
	return workspace, workspace.Validate()
}

// PrepareBrandSoundDesignFixture creates the v2, no-narration sound-design
// workspace used by new Brand Film tasks. Fixture media remains explicit until
// a real AI music / sound-effect generator is configured.
func PrepareBrandSoundDesignFixture(plan BrandFilmPlanVersion, visual contract.AssetVersionRef, createdBy string, now time.Time) (BrandAudioWorkspace, error) {
	if err := validateBrandAudioSourcePlan(plan); err != nil {
		return BrandAudioWorkspace{}, err
	}
	if visual.Validate() != nil || strings.TrimSpace(createdBy) == "" || now.IsZero() {
		return BrandAudioWorkspace{}, fmt.Errorf("brand sound design fixture input is incomplete")
	}
	intent := plan.SoundDesignIntent
	if intent.Empty() {
		intent = SoundDesignIntent{MusicDirection: firstNonEmpty(plan.MusicDirection, "克制、有品牌留白感的音乐"), SoundEffectFocus: []string{"镜头动作与产品材质", "转场与品牌定格"}, SourceAudioPolicy: "mute", Avoid: []string{"人声"}}
	}
	if err := intent.Validate(); err != nil {
		return BrandAudioWorkspace{}, err
	}
	masterDurationMS := plan.Shots[len(plan.Shots)-1].EndSecond * 1000
	blueprint := AudioBlueprintVersion{Revision: 1, PlanRevision: plan.Revision, MasterDurationMS: masterDurationMS, SoundDesignIntent: intent, MusicArc: AudioMusicArc{StartMS: 0, EndMS: masterDurationMS, Direction: intent.MusicDirection}, CreatedAt: now}
	ambience := AudioClip{ID: "ambience_clip_01", TrackID: "track_ambience", Order: 1, FixtureURI: "fixture://brand-film/sound-design/ambience", Label: "环境氛围：" + strings.Join(intent.SoundEffectFocus[:1], ""), TimelineStartMS: 0, TimelineEndMS: masterDurationMS, SourceOutMS: masterDurationMS, PlaybackRate: 1, FadeInMS: 700, FadeOutMS: 900}
	music := AudioClip{ID: "music_clip_01", TrackID: "track_music", Order: 1, FixtureURI: "fixture://brand-film/sound-design/music", Label: intent.MusicDirection, TimelineStartMS: 0, TimelineEndMS: masterDurationMS, SourceOutMS: masterDurationMS, PlaybackRate: 1, FadeInMS: 800, FadeOutMS: 1200}
	sfxClips := make([]AudioClip, 0, len(plan.Shots))
	for index, shot := range plan.Shots {
		startMS, endMS := shot.StartSecond*1000, shot.EndSecond*1000
		duration := min(900, endMS-startMS)
		cueID := fmt.Sprintf("sound_cue_%02d", index+1)
		label := shot.Purpose + "镜头音效"
		prompt := fmt.Sprintf("%s；围绕%s，时长约%d毫秒，细腻克制、适合品牌广告、无人声", label, shot.Visual, duration)
		blueprint.SoundEffectCues = append(blueprint.SoundEffectCues, AudioSoundEffectCue{ID: cueID, ShotID: shot.ID, TrackType: BrandAudioTrackSFX, StartMS: startMS, EndMS: startMS + duration, Label: label, Purpose: shot.Purpose, Prompt: prompt, NegativePrompt: strings.Join(intent.Avoid, "；"), Intensity: "subtle", Status: "planned", Reason: "根据已确认镜头目的自动安排"})
		sfxClips = append(sfxClips, AudioClip{ID: fmt.Sprintf("sfx_clip_%02d", index+1), TrackID: "track_sfx", Order: index + 1, FixtureURI: "fixture://brand-film/sound-design/sfx/" + shot.ID, Label: prompt, TimelineStartMS: startMS, TimelineEndMS: startMS + duration, SourceOutMS: duration, PlaybackRate: 1, FadeOutMS: 120, CueRef: cueID})
	}
	blueprint = directBrandSoundDesignBlueprint(plan, blueprint)
	blueprintHash, err := contract.CanonicalJSONHash(blueprint)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	blueprint.ContentHash = "sha256:" + blueprintHash
	variantID := "sound_treatment_restrained_luxury"
	mix := AudioMixVersion{ID: "audio_mix_01", Revision: 1, VariantID: variantID, PlanRevision: plan.Revision, VisualPreview: visual, MasterDurationMS: masterDurationMS, SampleRate: 48000, ChannelLayout: "stereo", CompilerVersion: "brand-sound-design-mix-compiler/v2", Status: "draft", ChangeSummary: "AI 声音设计 Fixture 草稿", CreatedBy: createdBy, CreatedAt: now, Tracks: []AudioTrack{
		{ID: "track_source_audio", Type: BrandAudioTrackSource, Role: "原视频声音", Muted: intent.SourceAudioPolicy == "mute", GainDB: -24, RightsStatus: "generated", Clips: []AudioClip{}},
		{ID: "track_ambience", Type: BrandAudioTrackAmbience, Role: "环境氛围", GainDB: -20, RightsStatus: "fixture", Clips: []AudioClip{ambience}},
		{ID: "track_music", Type: BrandAudioTrackMusic, Role: "品牌音乐", GainDB: -18, RightsStatus: "fixture", Clips: []AudioClip{music}},
		{ID: "track_sfx", Type: BrandAudioTrackSFX, Role: "镜头音效", GainDB: -8, RightsStatus: "fixture", Clips: sfxClips},
	}}
	mixHash, err := contract.CanonicalJSONHash(mix)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	mix.ContentHash = "sha256:" + mixHash
	immersive, err := buildImmersiveWaterVariant(mix, visual, now)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	immersive.ID, immersive.Label, immersive.StylePreset = "sound_treatment_immersive_water", "沉浸水感版", "immersive_water"
	for index := range immersive.MixVersions {
		immersive.MixVersions[index].VariantID = immersive.ID
		immersive.MixVersions[index].ContentHash = ""
		variantHash, err := contract.CanonicalJSONHash(immersive.MixVersions[index])
		if err != nil {
			return BrandAudioWorkspace{}, err
		}
		immersive.MixVersions[index].ContentHash = "sha256:" + variantHash
	}
	youthful, err := buildYouthfulLightVariant(mix, visual, now)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	workspace := BrandAudioWorkspace{ContractVersion: BrandAudioWorkspaceV2, PlanRevision: plan.Revision, MasterDurationMS: masterDurationMS, VisualPreview: visual, BlueprintVersions: []AudioBlueprintVersion{blueprint}, Variants: []AudioMixVariant{{ID: variantID, Label: "高级克制版", VisualPreview: visual, VariantType: "tone", Language: "und", StylePreset: "restrained_luxury", MixVersions: []AudioMixVersion{mix}, ActiveMixRevision: 1, Status: "draft"}, immersive, youthful}, ActiveVariantID: variantID, ActiveRevision: 1, Attempts: []AudioGenerationAttempt{}, RenderJobs: []AudioMixRenderJob{}, Status: "draft", UpdatedAt: now}
	return workspace, workspace.Validate()
}

// Audio planning deliberately validates the confirmed timeline instead of the
// current video-generation profile. Persisted projects can contain a legacy
// four-shot 15-second plan, and audio clips are not coupled one-to-one to the
// provider generation units used to create the visual preview.
func validateBrandAudioSourcePlan(plan BrandFilmPlanVersion) error {
	if plan.Revision < 1 || strings.TrimSpace(plan.ConceptID) == "" || strings.TrimSpace(plan.Title) == "" ||
		strings.TrimSpace(plan.StorySummary) == "" ||
		len(plan.Shots) == 0 || plan.CreatedAt.IsZero() {
		return fmt.Errorf("brand audio source plan is incomplete")
	}
	if plan.SoundDesignIntent.Empty() && strings.TrimSpace(plan.VoiceDirection) == "" {
		return fmt.Errorf("brand audio source plan requires a sound design intent or legacy voice direction")
	}
	if !plan.SoundDesignIntent.Empty() {
		if err := plan.SoundDesignIntent.Validate(); err != nil {
			return fmt.Errorf("brand audio source plan sound design intent is invalid: %w", err)
		}
	}
	endSecond := 0
	for index, shot := range plan.Shots {
		if shot.Order != index+1 || shot.StartSecond != endSecond || shot.EndSecond <= shot.StartSecond ||
			strings.TrimSpace(shot.ID) == "" || strings.TrimSpace(shot.Visual) == "" || strings.TrimSpace(shot.Purpose) == "" {
			return fmt.Errorf("brand audio source shot %d is invalid", index+1)
		}
		endSecond = shot.EndSecond
	}
	masterDurationMS := plan.MasterDurationMS
	if masterDurationMS == 0 {
		masterDurationMS = endSecond * 1000
	}
	if masterDurationMS < 12000 || endSecond*1000 != masterDurationMS {
		return fmt.Errorf("brand audio source plan timeline is invalid")
	}
	return nil
}

func (w BrandAudioWorkspace) CurrentMix() *AudioMixVersion {
	for variantIndex := range w.Variants {
		if w.Variants[variantIndex].ID != w.ActiveVariantID {
			continue
		}
		for mixIndex := range w.Variants[variantIndex].MixVersions {
			if w.Variants[variantIndex].MixVersions[mixIndex].Revision == w.ActiveRevision {
				return &w.Variants[variantIndex].MixVersions[mixIndex]
			}
		}
	}
	return nil
}

func (m AudioMixVersion) Track(trackType string) *AudioTrack {
	for index := range m.Tracks {
		if m.Tracks[index].Type == trackType {
			return &m.Tracks[index]
		}
	}
	return nil
}

func (w BrandAudioWorkspace) Validate() error {
	if (w.ContractVersion != BrandAudioWorkspaceV1 && w.ContractVersion != BrandAudioWorkspaceV2) || w.PlanRevision < 1 || w.MasterDurationMS < 12000 || w.VisualPreview.Validate() != nil || len(w.BlueprintVersions) == 0 || len(w.Variants) == 0 || w.ActiveVariantID == "" || w.ActiveRevision < 1 || w.UpdatedAt.IsZero() {
		return fmt.Errorf("brand audio workspace is incomplete")
	}
	mix := w.CurrentMix()
	if mix == nil {
		return fmt.Errorf("brand audio workspace active mix is missing")
	}
	if err := mix.Validate(); err != nil {
		return err
	}
	if w.ContractVersion == BrandAudioWorkspaceV1 {
		return mix.validateTrackLayout(BrandAudioTrackSource, BrandAudioTrackVoiceover, BrandAudioTrackMusic, BrandAudioTrackSFX)
	}
	return mix.validateTrackLayout(BrandAudioTrackSource, BrandAudioTrackAmbience, BrandAudioTrackMusic, BrandAudioTrackSFX)
}

func (m AudioMixVersion) Validate() error {
	if m.ID == "" || m.Revision < 1 || m.VariantID == "" || m.PlanRevision < 1 || m.VisualPreview.Validate() != nil || m.MasterDurationMS < 12000 || m.SampleRate != 48000 || m.ChannelLayout != "stereo" || len(m.Tracks) == 0 || !validSHA256Ref(m.ContentHash) || m.CreatedBy == "" || m.CreatedAt.IsZero() {
		return fmt.Errorf("brand audio mix is incomplete")
	}
	seen := map[string]bool{}
	for _, track := range m.Tracks {
		if track.ID == "" || seen[track.Type] || !supportedAudioTrack(track.Type) {
			return fmt.Errorf("brand audio track is invalid")
		}
		seen[track.Type] = true
		for index, clip := range track.Clips {
			if clip.ID == "" || clip.TrackID != track.ID || clip.Order != index+1 || clip.TimelineStartMS < 0 || clip.TimelineEndMS <= clip.TimelineStartMS || clip.TimelineEndMS > m.MasterDurationMS || clip.PlaybackRate <= 0 || (clip.AssetRef == nil && clip.FixtureURI == "") {
				return fmt.Errorf("brand audio clip is invalid")
			}
			if clip.AssetRef != nil && clip.AssetRef.Validate() != nil {
				return fmt.Errorf("brand audio clip asset reference is invalid")
			}
			for _, peak := range clip.WaveformPeaks {
				if peak < 0 || peak > 1 {
					return fmt.Errorf("brand audio waveform peak is invalid")
				}
			}
		}
	}
	return nil
}

func (m AudioMixVersion) validateTrackLayout(required ...string) error {
	seen := map[string]bool{}
	for _, track := range m.Tracks {
		seen[track.Type] = true
	}
	for _, trackType := range required {
		if !seen[trackType] {
			return fmt.Errorf("brand audio track %s is missing", trackType)
		}
	}
	if len(seen) != len(required) {
		return fmt.Errorf("brand audio track layout is invalid")
	}
	return nil
}

func ReviseBrandAudioMix(workspace BrandAudioWorkspace, operations []AudioMixOperation, createdBy string, now time.Time) (BrandAudioWorkspace, error) {
	if err := workspace.Validate(); err != nil {
		return BrandAudioWorkspace{}, err
	}
	if len(operations) == 0 || strings.TrimSpace(createdBy) == "" || now.IsZero() {
		return BrandAudioWorkspace{}, fmt.Errorf("brand audio mix revision input is incomplete")
	}
	raw, err := json.Marshal(workspace)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var next BrandAudioWorkspace
	if err := json.Unmarshal(raw, &next); err != nil {
		return BrandAudioWorkspace{}, err
	}
	variantIndex := -1
	for index := range next.Variants {
		if next.Variants[index].ID == next.ActiveVariantID {
			variantIndex = index
			break
		}
	}
	if variantIndex < 0 {
		return BrandAudioWorkspace{}, fmt.Errorf("active audio variant is missing")
	}
	current := next.CurrentMix()
	if current == nil {
		return BrandAudioWorkspace{}, fmt.Errorf("active audio mix is missing")
	}
	currentRaw, err := json.Marshal(current)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var revised AudioMixVersion
	if err := json.Unmarshal(currentRaw, &revised); err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ParentRevision, revised.Revision = current.Revision, current.Revision+1
	revised.ID = fmt.Sprintf("audio_mix_%02d", revised.Revision)
	revised.CreatedBy, revised.CreatedAt, revised.Status = createdBy, now, "draft"
	revised.ChangeSummary = "人工调整音轨草稿"
	for _, operation := range operations {
		if operation.Op == AudioMixOperationAddFixtureClip {
			if strings.TrimSpace(operation.TrackID) == "" || strings.TrimSpace(operation.Label) == "" || operation.TimelineStartMS == nil || operation.TimelineEndMS == nil || *operation.TimelineStartMS < 0 || *operation.TimelineEndMS <= *operation.TimelineStartMS || *operation.TimelineEndMS > revised.MasterDurationMS {
				return BrandAudioWorkspace{}, fmt.Errorf("add_fixture_clip requires track, label and valid timeline interval")
			}
			added := false
			for trackIndex := range revised.Tracks {
				track := &revised.Tracks[trackIndex]
				if track.ID != operation.TrackID {
					continue
				}
				if track.Type == BrandAudioTrackSource {
					return BrandAudioWorkspace{}, fmt.Errorf("source audio clips cannot be created manually")
				}
				clipID := fmt.Sprintf("%s_clip_%02d", track.Type, len(track.Clips)+1)
				track.Clips = append(track.Clips, AudioClip{ID: clipID, TrackID: track.ID, Order: len(track.Clips) + 1, FixtureURI: "fixture://brand-film/sound-design/manual/" + clipID, Label: strings.TrimSpace(operation.Label), TimelineStartMS: *operation.TimelineStartMS, TimelineEndMS: *operation.TimelineEndMS, SourceOutMS: *operation.TimelineEndMS - *operation.TimelineStartMS, PlaybackRate: 1, FadeInMS: 80, FadeOutMS: 120})
				added = true
				break
			}
			if !added {
				return BrandAudioWorkspace{}, fmt.Errorf("audio track %s not found", operation.TrackID)
			}
			continue
		}
		if operation.Op == AudioMixOperationReplaceClip || operation.Op == AudioMixOperationSetClipTiming || operation.Op == AudioMixOperationSetClipLabel || operation.Op == AudioMixOperationSetClipGain || operation.Op == AudioMixOperationSetClipFade || operation.Op == AudioMixOperationRemoveClip {
			if strings.TrimSpace(operation.ClipID) == "" {
				return BrandAudioWorkspace{}, fmt.Errorf("audio clip id is required")
			}
			if operation.Op == AudioMixOperationSetClipTiming && (operation.TimelineStartMS == nil || operation.TimelineEndMS == nil || *operation.TimelineStartMS < 0 || *operation.TimelineEndMS <= *operation.TimelineStartMS || *operation.TimelineEndMS > revised.MasterDurationMS) {
				return BrandAudioWorkspace{}, fmt.Errorf("set_clip_timing requires a valid master timeline interval")
			}
			if operation.Op == AudioMixOperationReplaceClip && (operation.AssetRef == nil || operation.AssetRef.Validate() != nil) {
				return BrandAudioWorkspace{}, fmt.Errorf("replace_clip_asset requires asset_ref")
			}
			if operation.Op == AudioMixOperationSetClipLabel && strings.TrimSpace(operation.Label) == "" {
				return BrandAudioWorkspace{}, fmt.Errorf("set_clip_label requires label")
			}
			if operation.Op == AudioMixOperationSetClipGain && (operation.GainDB == nil || *operation.GainDB < -96 || *operation.GainDB > 24) {
				return BrandAudioWorkspace{}, fmt.Errorf("set_clip_gain requires a valid gain")
			}
			if operation.Op == AudioMixOperationSetClipFade && (operation.FadeInMS == nil || operation.FadeOutMS == nil || *operation.FadeInMS < 0 || *operation.FadeOutMS < 0) {
				return BrandAudioWorkspace{}, fmt.Errorf("set_clip_fade requires non-negative fades")
			}
			found := false
			for trackIndex := range revised.Tracks {
				track := &revised.Tracks[trackIndex]
				for clipIndex := range track.Clips {
					clip := &track.Clips[clipIndex]
					if clip.ID != operation.ClipID {
						continue
					}
					switch operation.Op {
					case AudioMixOperationReplaceClip:
						ref := *operation.AssetRef
						clip.AssetRef, clip.FixtureURI, clip.GenerationAttemptID = &ref, "", ""
					case AudioMixOperationSetClipTiming:
						clip.TimelineStartMS, clip.TimelineEndMS = *operation.TimelineStartMS, *operation.TimelineEndMS
					case AudioMixOperationSetClipLabel:
						clip.Label = strings.TrimSpace(operation.Label)
					case AudioMixOperationSetClipGain:
						clip.GainDB = *operation.GainDB
					case AudioMixOperationSetClipFade:
						clip.FadeInMS, clip.FadeOutMS = *operation.FadeInMS, *operation.FadeOutMS
					case AudioMixOperationRemoveClip:
						track.Clips = append(track.Clips[:clipIndex], track.Clips[clipIndex+1:]...)
						for order := range track.Clips {
							track.Clips[order].Order = order + 1
						}
					}
					found = true
					break
				}
				if found {
					break
				}
			}
			if !found {
				return BrandAudioWorkspace{}, fmt.Errorf("audio clip %s is missing", operation.ClipID)
			}
			continue
		}
		trackIndex := -1
		for index := range revised.Tracks {
			if revised.Tracks[index].ID == operation.TrackID {
				trackIndex = index
				break
			}
		}
		if trackIndex < 0 {
			return BrandAudioWorkspace{}, fmt.Errorf("audio track %s not found", operation.TrackID)
		}
		switch operation.Op {
		case AudioMixOperationSetTrackGain:
			if operation.GainDB == nil || *operation.GainDB < -96 || *operation.GainDB > 24 {
				return BrandAudioWorkspace{}, fmt.Errorf("audio track gain is invalid")
			}
			revised.Tracks[trackIndex].GainDB = *operation.GainDB
		case AudioMixOperationSetTrackMuted:
			if operation.Muted == nil {
				return BrandAudioWorkspace{}, fmt.Errorf("audio track muted value is required")
			}
			revised.Tracks[trackIndex].Muted = *operation.Muted
		default:
			return BrandAudioWorkspace{}, fmt.Errorf("unsupported audio mix operation %s", operation.Op)
		}
	}
	if brandAudioMixAssetsReady(&revised) {
		revised.Status = "assets_ready"
	}
	revised.ContentHash = ""
	hash, err := contract.CanonicalJSONHash(revised)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ContentHash = "sha256:" + hash
	if err := revised.Validate(); err != nil {
		return BrandAudioWorkspace{}, err
	}
	next.Variants[variantIndex].MixVersions = append(next.Variants[variantIndex].MixVersions, revised)
	next.Variants[variantIndex].ActiveMixRevision = revised.Revision
	next.ActiveRevision, next.Status, next.UpdatedAt = revised.Revision, revised.Status, now
	next.MixedPreview, next.FinalMixedAsset = nil, nil
	return next, next.Validate()
}

func (s Service) PrepareBrandFilmAudio(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request BrandFilmRevisionRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	brand := detail.VideoDraft.BrandFilm
	plan := brand.CurrentPlan()
	if request.ExpectedRevision != detail.VideoDraft.Revision {
		return TaskDetail{}, ErrVersionConflict
	}
	if plan == nil || !plan.Confirmed || brand.Generation == nil || brand.Generation.PreviewAsset == nil {
		return TaskDetail{}, ErrInvalidState
	}
	if brand.Audio != nil && brand.Audio.PlanRevision == plan.Revision && brand.Audio.VisualPreview == *brand.Generation.PreviewAsset {
		latest := brand.Audio.BlueprintVersions[len(brand.Audio.BlueprintVersions)-1]
		if brand.Audio.ContractVersion == BrandAudioWorkspaceV2 && latest.PlannerVersion == "brand-sound-design-director/v2" {
			return detail, nil
		}
	}
	now := s.now()
	audio, err := PrepareBrandSoundDesignFixture(*plan, *brand.Generation.PreviewAsset, actor.Principal.ID, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage = next.Revision, BrandFilmAudioDraft
	next.BrandFilm.Audio, next.BrandFilm.UpdatedAt = &audio, now
	next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: false, Blockers: []string{"audio_assets", "audio_mix_preview", "audio_confirmation"}}
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}

func (s Service) UpdateBrandFilmAudioMix(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, taskID string, request UpdateBrandAudioMixRequest) (TaskDetail, error) {
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
	audio, err := ReviseBrandAudioMix(*detail.VideoDraft.BrandFilm.Audio, request.Operations, actor.Principal.ID, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage = next.Revision, BrandFilmAudioDraft
	next.BrandFilm.Audio, next.BrandFilm.UpdatedAt = &audio, now
	return s.persistBrandFilmDraft(ctx, actor, projectID, taskID, *detail.VideoDraft, next)
}

func (s Service) MaterializeBrandFilmAudioAssets(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, taskID string, request BrandFilmRevisionRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, requestContext.Actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	if request.ExpectedRevision != detail.VideoDraft.Revision {
		return TaskDetail{}, ErrVersionConflict
	}
	if detail.VideoDraft.BrandFilm.Audio == nil || s.AudioAssets == nil {
		return TaskDetail{}, ErrInvalidState
	}
	if brandAudioAssetsReady(*detail.VideoDraft.BrandFilm.Audio) {
		return detail, nil
	}
	now := s.now()
	audio, err := materializeBrandAudioFixtureAssets(ctx, requestContext, projectID, taskID, *detail.VideoDraft.BrandFilm.Audio, s.AudioAssets, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage = next.Revision, BrandFilmAudioDraft
	next.BrandFilm.Audio, next.BrandFilm.UpdatedAt = &audio, now
	next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: false, Blockers: []string{"audio_mix_preview", "audio_confirmation"}}
	return s.persistBrandFilmDraft(ctx, requestContext.Actor, projectID, taskID, *detail.VideoDraft, next)
}

// GenerateBrandFilmSoundAssets turns the v2 sound-design cues into real project
// audio through the configured prompt-to-audio provider. It intentionally has no
// Fixture fallback: callers can still invoke MaterializeBrandFilmAudioAssets for
// an explicitly labelled development demonstration, but that path is never
// presented as AI generated media.
func (s Service) GenerateBrandFilmSoundAssets(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, taskID string, request BrandFilmRevisionRequest) (TaskDetail, error) {
	detail, err := s.requireBrandFilmWorkspace(ctx, requestContext.Actor, projectID, taskID, true)
	if err != nil {
		return TaskDetail{}, err
	}
	if request.ExpectedRevision != detail.VideoDraft.Revision {
		return TaskDetail{}, ErrVersionConflict
	}
	if detail.VideoDraft.BrandFilm.Audio == nil || detail.VideoDraft.BrandFilm.Audio.ContractVersion != BrandAudioWorkspaceV2 || s.AudioAssets == nil {
		return TaskDetail{}, ErrInvalidState
	}
	if s.BrandFilmSoundAssets == nil {
		return TaskDetail{}, provider.SoundAssetProviderError{Code: "capability_unavailable", Message: "AI 音乐/音效模型尚未配置；请配置独立的 Sound Asset Provider，不能使用 TTS Key"}
	}
	if brandAudioAssetsReady(*detail.VideoDraft.BrandFilm.Audio) {
		return detail, nil
	}
	now := s.now()
	audio, err := generateBrandSoundAssets(ctx, requestContext, projectID, taskID, *detail.VideoDraft.BrandFilm.Audio, s.AudioAssets, s.BrandFilmSoundAssets, now)
	if err != nil {
		return TaskDetail{}, err
	}
	next := cloneBrandVideoDraft(*detail.VideoDraft)
	next.Revision++
	next.BrandFilm.Revision, next.BrandFilm.Stage = next.Revision, BrandFilmAudioDraft
	next.BrandFilm.Audio, next.BrandFilm.UpdatedAt = &audio, now
	next.BrandFilm.Readiness = CreativeReadiness{PlanningReady: true, GenerationReady: true, ProductionReady: false, Blockers: []string{"audio_mix_preview", "audio_confirmation"}}
	return s.persistBrandFilmDraft(ctx, requestContext.Actor, projectID, taskID, *detail.VideoDraft, next)
}

func generateBrandSoundAssets(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, taskID string, workspace BrandAudioWorkspace, writer AudioAssetWriter, generator provider.SoundAssetGenerator, now time.Time) (BrandAudioWorkspace, error) {
	raw, err := json.Marshal(workspace)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var next BrandAudioWorkspace
	if err := json.Unmarshal(raw, &next); err != nil {
		return BrandAudioWorkspace{}, err
	}
	variantIndex := -1
	for index := range next.Variants {
		if next.Variants[index].ID == next.ActiveVariantID {
			variantIndex = index
			break
		}
	}
	current := next.CurrentMix()
	if variantIndex < 0 || current == nil {
		return BrandAudioWorkspace{}, fmt.Errorf("active sound mix is missing")
	}
	currentRaw, _ := json.Marshal(current)
	var revised AudioMixVersion
	if err := json.Unmarshal(currentRaw, &revised); err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ParentRevision, revised.Revision = current.Revision, current.Revision+1
	revised.ID = fmt.Sprintf("audio_mix_%02d", revised.Revision)
	revised.Status, revised.ChangeSummary = "assets_ready", "AI 生成的音乐、环境氛围与镜头音效已进入项目资产库"
	revised.CreatedBy, revised.CreatedAt, revised.ContentHash = requestContext.Actor.Principal.ID, now, ""
	sourceVersion := workspace.PlanRevision
	sourceResources := []contract.ResourceRef{{Type: "creative_brand_film_plan", ID: taskID, Version: &sourceVersion}}
	blueprint := workspace.BlueprintVersions[len(workspace.BlueprintVersions)-1]
	for trackIndex := range revised.Tracks {
		track := &revised.Tracks[trackIndex]
		if track.Type == BrandAudioTrackSource || len(track.Clips) == 0 {
			continue
		}
		track.RightsStatus = "ai_generated"
		for clipIndex := range track.Clips {
			clip := &track.Clips[clipIndex]
			if clip.AssetRef != nil {
				continue
			}
			durationMS := clip.SourceOutMS - clip.SourceInMS
			if durationMS <= 0 {
				durationMS = clip.TimelineEndMS - clip.TimelineStartMS
			}
			negativePrompt := strings.Join(blueprint.SoundDesignIntent.Avoid, "；")
			for _, cue := range blueprint.SoundEffectCues {
				if cue.ID == clip.CueRef && strings.TrimSpace(cue.NegativePrompt) != "" {
					negativePrompt = cue.NegativePrompt
					break
				}
			}
			input := provider.SoundAssetGenerationInput{OrganizationID: requestContext.Actor.OrganizationID, ModelAlias: "cookies.sound.brand", RequestID: taskID + ":" + clip.ID, TrackType: track.Type, Prompt: clip.Label, NegativePrompt: negativePrompt, DurationMS: durationMS, Format: "wav", SampleRate: 48000}
			result, generateErr := generator.GenerateSoundAsset(ctx, input)
			if generateErr != nil {
				return BrandAudioWorkspace{}, fmt.Errorf("generate %s: %w", clip.ID, generateErr)
			}
			if len(result.Audio) == 0 || result.Codec != "wav" {
				return BrandAudioWorkspace{}, fmt.Errorf("generate %s: provider returned invalid audio", clip.ID)
			}
			identity, hashErr := contract.CanonicalJSONHash(struct {
				PlanRevision int64  `json:"plan_revision"`
				ClipID       string `json:"clip_id"`
				Prompt       string `json:"prompt"`
				Snapshot     string `json:"provider_snapshot"`
			}{workspace.PlanRevision, clip.ID, clip.Label, result.ProviderSnapshot})
			if hashErr != nil {
				return BrandAudioWorkspace{}, hashErr
			}
			ref, ingestErr := writer.IngestDerivedAudio(ctx, requestContext, projectID, "brand-ai-sound-"+identity[:32], bytes.NewReader(result.Audio), int64(len(result.Audio)), "audio/wav", sourceResources)
			if ingestErr != nil {
				return BrandAudioWorkspace{}, ingestErr
			}
			attemptID := "audioattempt_" + identity[:20]
			assetRef := ref.AssetVersion
			clip.AssetRef, clip.FixtureURI, clip.GenerationAttemptID, clip.WaveformPeaks = &assetRef, "", attemptID, []float64{}
			next.Attempts = append(next.Attempts, AudioGenerationAttempt{ID: attemptID, ClipID: clip.ID, Ordinal: 1, Status: "succeeded", ProviderJobID: result.ProviderRequestID, Provider: "sound_asset", ProviderSnapshot: result.ProviderSnapshot, OutputAssetRef: &assetRef, FixtureMode: false, CreatedAt: now, UpdatedAt: now})
		}
	}
	hash, err := contract.CanonicalJSONHash(revised)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ContentHash = "sha256:" + hash
	next.Variants[variantIndex].MixVersions = append(next.Variants[variantIndex].MixVersions, revised)
	next.Variants[variantIndex].ActiveMixRevision = revised.Revision
	next.ActiveRevision, next.Status, next.UpdatedAt = revised.Revision, "assets_ready", now
	return next, next.Validate()
}

func brandAudioAssetsReady(workspace BrandAudioWorkspace) bool {
	return brandAudioMixAssetsReady(workspace.CurrentMix())
}

func brandAudioMixAssetsReady(mix *AudioMixVersion) bool {
	if mix == nil {
		return false
	}
	found := false
	for _, track := range mix.Tracks {
		for _, clip := range track.Clips {
			found = true
			if clip.AssetRef == nil || clip.FixtureURI != "" {
				return false
			}
		}
	}
	return found
}

func materializeBrandAudioFixtureAssets(ctx context.Context, requestContext contract.RequestContext, projectID contract.ProjectID, taskID string, workspace BrandAudioWorkspace, writer AudioAssetWriter, now time.Time) (BrandAudioWorkspace, error) {
	raw, err := json.Marshal(workspace)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	var next BrandAudioWorkspace
	if err := json.Unmarshal(raw, &next); err != nil {
		return BrandAudioWorkspace{}, err
	}
	variantIndex := -1
	for index := range next.Variants {
		if next.Variants[index].ID == next.ActiveVariantID {
			variantIndex = index
			break
		}
	}
	current := next.CurrentMix()
	if variantIndex < 0 || current == nil {
		return BrandAudioWorkspace{}, fmt.Errorf("active audio mix is missing")
	}
	currentRaw, _ := json.Marshal(current)
	var revised AudioMixVersion
	if err := json.Unmarshal(currentRaw, &revised); err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ParentRevision, revised.Revision = current.Revision, current.Revision+1
	revised.ID = fmt.Sprintf("audio_mix_%02d", revised.Revision)
	revised.Status, revised.ChangeSummary = "assets_ready", "Fixture 音频已进入项目资产库"
	revised.CreatedBy, revised.CreatedAt, revised.ContentHash = requestContext.Actor.Principal.ID, now, ""
	sourceVersion := workspace.PlanRevision
	sourceResources := []contract.ResourceRef{{Type: "creative_brand_film_plan", ID: taskID, Version: &sourceVersion}}
	for trackIndex := range revised.Tracks {
		track := &revised.Tracks[trackIndex]
		for clipIndex := range track.Clips {
			clip := &track.Clips[clipIndex]
			if clip.AssetRef != nil {
				continue
			}
			durationMS := clip.SourceOutMS - clip.SourceInMS
			contents, peaks, err := RenderBrandAudioFixtureWAV(track.Type, clip.FixtureURI+"\x00"+clip.Label, durationMS)
			if err != nil {
				return BrandAudioWorkspace{}, err
			}
			identity, err := contract.CanonicalJSONHash(struct {
				PlanRevision int64  `json:"plan_revision"`
				ClipID       string `json:"clip_id"`
				FixtureURI   string `json:"fixture_uri"`
				Generator    string `json:"generator"`
			}{workspace.PlanRevision, clip.ID, clip.FixtureURI, "brand-audio-fixture-wav/v1"})
			if err != nil {
				return BrandAudioWorkspace{}, err
			}
			derivationID := "brand-audio-fixture-" + identity[:32]
			ref, err := writer.IngestDerivedAudio(ctx, requestContext, projectID, derivationID, bytes.NewReader(contents), int64(len(contents)), "audio/wav", sourceResources)
			if err != nil {
				return BrandAudioWorkspace{}, err
			}
			attemptIdentity, err := contract.CanonicalJSONHash(struct {
				VariantID string `json:"variant_id"`
				ClipID    string `json:"clip_id"`
				AssetHash string `json:"asset_hash"`
			}{current.VariantID, clip.ID, identity})
			if err != nil {
				return BrandAudioWorkspace{}, err
			}
			attemptID := "audioattempt_" + attemptIdentity[:20]
			assetRef := ref.AssetVersion
			clip.AssetRef, clip.FixtureURI, clip.GenerationAttemptID, clip.WaveformPeaks = &assetRef, "", attemptID, peaks
			next.Attempts = append(next.Attempts, AudioGenerationAttempt{ID: attemptID, ClipID: clip.ID, Ordinal: 1, Status: "succeeded", OutputAssetRef: &assetRef, FixtureMode: true, CreatedAt: now, UpdatedAt: now})
		}
	}
	hash, err := contract.CanonicalJSONHash(revised)
	if err != nil {
		return BrandAudioWorkspace{}, err
	}
	revised.ContentHash = "sha256:" + hash
	next.Variants[variantIndex].MixVersions = append(next.Variants[variantIndex].MixVersions, revised)
	next.Variants[variantIndex].ActiveMixRevision = revised.Revision
	next.ActiveRevision, next.Status, next.UpdatedAt = revised.Revision, "assets_ready", now
	return next, next.Validate()
}
