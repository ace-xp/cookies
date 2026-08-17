package creative

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
)

func TestCompileBrandAudioMixUsesActiveTrackStateAndImmutableAssets(t *testing.T) {
	t.Parallel()
	visual := contract.AssetVersionRef{AssetID: "visual", Version: 2}
	voice := contract.AssetVersionRef{AssetID: "voice", Version: 3}
	music := contract.AssetVersionRef{AssetID: "music", Version: 4}
	mix := AudioMixVersion{VisualPreview: visual, MasterDurationMS: 15000, SampleRate: 48000, ChannelLayout: "stereo", Tracks: []AudioTrack{
		{Type: BrandAudioTrackVoiceover, GainDB: 2, Clips: []AudioClip{{ID: "voice_1", AssetRef: &voice, TimelineStartMS: 1000, TimelineEndMS: 4000, SourceOutMS: 3000, GainDB: -1, PlaybackRate: 1}}},
		{Type: BrandAudioTrackMusic, Muted: true, Clips: []AudioClip{{ID: "music_1", AssetRef: &music, TimelineEndMS: 15000, SourceOutMS: 15000, PlaybackRate: 1}}},
	}}
	request, err := CompileBrandAudioMix("org_1", "project_1", mix)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Clips) != 1 || request.Clips[0].GainDB != 1 || request.Clips[0].Asset != voice {
		t.Fatalf("unexpected compiled clips: %#v", request.Clips)
	}
}

func TestCompileBrandAudioMixRejectsUnmaterializedClip(t *testing.T) {
	t.Parallel()
	mix := AudioMixVersion{VisualPreview: contract.AssetVersionRef{AssetID: "visual", Version: 1}, MasterDurationMS: 15000, SampleRate: 48000, ChannelLayout: "stereo", Tracks: []AudioTrack{{Type: BrandAudioTrackVoiceover, Clips: []AudioClip{{ID: "voice_1", TimelineEndMS: 3000, SourceOutMS: 3000, PlaybackRate: 1}}}}}
	if _, err := CompileBrandAudioMix("org_1", "project_1", mix); err == nil {
		t.Fatal("unmaterialized clip was accepted")
	}
}

func TestApplyGeneratedBrandVoiceReplacesOnlyOneClipAndAppendsMixRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	workspace, err := PrepareBrandAudioFixture(brandAudioTestPlan(now), contract.AssetVersionRef{AssetID: "visual", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	before := workspace.CurrentMix()
	other := before.Track(BrandAudioTrackVoiceover).Clips[1]
	ref := contract.AssetVersionRef{AssetID: "tts_voice_1", Version: 1}
	result := provider.SpeechSynthesisResult{DurationMS: 1240, ProviderRequestID: "trace_1", ModelAndVoiceSnapshot: "minimax/speech-2.8-turbo/warm", WordTimings: []provider.SpeechWordTiming{{Text: "娇兰", BeginMS: 0, EndMS: 300}}}
	updated, err := applyGeneratedBrandVoice(workspace, "voice_clip_01", "cookies.voice.brand.warm_female", ref, result, "user_1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	after := updated.CurrentMix()
	voice := after.Track(BrandAudioTrackVoiceover)
	if after.Revision != before.Revision+1 || voice.Clips[0].AssetRef == nil || *voice.Clips[0].AssetRef != ref || voice.Clips[0].SourceOutMS != 1240 || len(voice.Clips[0].WordTimings) != 1 {
		t.Fatalf("generated voice was not adopted: %#v", voice.Clips[0])
	}
	if voice.Clips[1].AssetRef != other.AssetRef || voice.Clips[1].FixtureURI != other.FixtureURI {
		t.Fatalf("unrelated voice clip changed: before=%#v after=%#v", other, voice.Clips[1])
	}
	attempt := updated.Attempts[len(updated.Attempts)-1]
	if attempt.Provider != "minimax" || attempt.FixtureMode || attempt.ProviderJobID != "trace_1" || updated.MixedPreview != nil {
		t.Fatalf("unexpected attempt/workspace: %#v %#v", attempt, updated)
	}
}

func TestRecordBrandVoiceFailureKeepsFixtureAndExposesProviderClassification(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	workspace, err := PrepareBrandAudioFixture(brandAudioTestPlan(now), contract.AssetVersionRef{AssetID: "visual", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	before := workspace.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0]
	updated := recordBrandVoiceFailure(workspace, before.ID, provider.SpeechProviderError{Code: "quota_exceeded", Message: "balance unavailable"}, now.Add(time.Minute))
	after := updated.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0]
	if after.FixtureURI != before.FixtureURI || updated.Status != "tts_fallback" {
		t.Fatalf("fixture fallback was not retained: %#v", updated)
	}
	attempt := updated.Attempts[len(updated.Attempts)-1]
	if attempt.Status != "failed" || attempt.ErrorCode != "quota_exceeded" || attempt.ErrorMessage != "balance unavailable" || attempt.FixtureMode {
		t.Fatalf("provider failure is not visible: %#v", attempt)
	}
}

func TestBrandFilmDurationProfilesFixShotAndUnitCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		duration  int
		profileID string
		shotCount int
	}{
		{name: "standard brand film", duration: 15, profileID: BrandFilmDurationStandard15, shotCount: 3},
		{name: "brand story", duration: 30, profileID: BrandFilmDurationStory30, shotCount: 6},
		{name: "custom rounds up by five", duration: 18, profileID: BrandFilmDurationCustom, shotCount: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, err := ResolveBrandFilmDurationProfile(test.duration)
			if err != nil {
				t.Fatal(err)
			}
			if profile.ID != test.profileID || profile.ShotCount != test.shotCount || profile.MasterDurationMS != test.duration*1000 {
				t.Fatalf("profile = %#v", profile)
			}
		})
	}

	for _, duration := range []int{0, 6, 11} {
		if _, err := ResolveBrandFilmDurationProfile(duration); err == nil {
			t.Fatalf("duration %d unexpectedly accepted", duration)
		}
	}
}

func TestPlanBrandFilmGenerationUnitsKeepsOneShotPerUnit(t *testing.T) {
	t.Parallel()
	shots := []BrandFilmShot{
		{ID: "shot_01", Order: 1, StartSecond: 0, EndSecond: 5},
		{ID: "shot_02", Order: 2, StartSecond: 5, EndSecond: 10},
		{ID: "shot_03", Order: 3, StartSecond: 10, EndSecond: 15},
	}

	plan, err := PlanBrandFilmGenerationUnits(15000, shots)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("unit count = %d", len(plan))
	}
	for index, unit := range plan {
		if unit.Order != index+1 || len(unit.ShotIDs) != 1 || unit.ShotIDs[0] != shots[index].ID || unit.ProviderDurationSeconds != 5 {
			t.Fatalf("unit %d = %#v", index+1, unit)
		}
	}

	invalid := append([]BrandFilmShot{}, shots...)
	invalid[2].StartSecond, invalid[2].EndSecond = 10, 13
	if _, err := PlanBrandFilmGenerationUnits(13000, invalid); err == nil {
		t.Fatal("three-second shot unexpectedly accepted")
	}
}

func TestPrepareBrandAudioFixtureBuildsEditablePersistentDraft(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	plan := BrandFilmPlanVersion{
		Revision: 2, ConceptID: "concept_01", Title: "黄金复原蜜", StorySummary: "自然能量进入产品并完成品牌定格。",
		VoiceDirection: "高级、温润、克制", MusicDirection: "克制弦乐与水感氛围", CreatedAt: now,
		Shots: []BrandFilmShot{
			{ID: "shot_01", Order: 1, StartSecond: 0, EndSecond: 5, Purpose: "建立世界", Visual: "蜂巢与水滴", Voiceover: "当自然的修护能量被唤醒。"},
			{ID: "shot_02", Order: 2, StartSecond: 5, EndSecond: 10, Purpose: "产品体验", Visual: "产品与水感质地", Voiceover: "轻盈补水，温润修护。"},
			{ID: "shot_03", Order: 3, StartSecond: 10, EndSecond: 15, Purpose: "品牌定格", Visual: "产品正面定格", Voiceover: "法国娇兰。"},
		},
	}
	visual := contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}

	workspace, err := PrepareBrandAudioFixture(plan, visual, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.Validate(); err != nil {
		t.Fatal(err)
	}
	if workspace.PlanRevision != 2 || workspace.MasterDurationMS != 15000 || len(workspace.BlueprintVersions) != 1 || len(workspace.Variants) != 2 {
		t.Fatalf("workspace = %#v", workspace)
	}
	blueprint := workspace.BlueprintVersions[0]
	if blueprint.PlannerVersion != "brand-audio-director/v1" || len(blueprint.Pronunciations) == 0 || len(blueprint.Decisions) == 0 || len(blueprint.SemanticChecks) == 0 {
		t.Fatalf("audio director did not explain its plan: %#v", blueprint)
	}
	if blueprint.NarrationCues[0].EstimatedDurationMS < 1 || blueprint.NarrationCues[0].AvailableDurationMS < 1 || blueprint.NarrationCues[0].FitStatus == "" {
		t.Fatalf("narration fit was not calculated: %#v", blueprint.NarrationCues[0])
	}
	mix := workspace.CurrentMix()
	if mix == nil || len(mix.Tracks) != 4 || mix.ContentHash == "" {
		t.Fatalf("mix = %#v", mix)
	}
	voice := mix.Track(BrandAudioTrackVoiceover)
	if voice == nil || len(voice.Clips) != 3 || voice.Clips[0].NarrationSource == nil || voice.Clips[0].NarrationSource.ShotID != "shot_01" {
		t.Fatalf("voice track = %#v", voice)
	}
}

func TestPrepareBrandAudioFixtureAcceptsLegacyFourShotTimeline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	plan.Shots = []BrandFilmShot{
		{ID: "shot_01", Order: 1, StartSecond: 0, EndSecond: 3, Purpose: "痛点钩子", Visual: "妆前特写", Voiceover: "轻叹气。"},
		{ID: "shot_02", Order: 2, StartSecond: 3, EndSecond: 8, Purpose: "产品进入", Visual: "蜂皇水产品", Voiceover: "娇兰二十五倍蜂皇水。"},
		{ID: "shot_03", Order: 3, StartSecond: 8, EndSecond: 12, Purpose: "效果展示", Visual: "湿敷后肌肤", Voiceover: "轻盈修护。"},
		{ID: "shot_04", Order: 4, StartSecond: 12, EndSecond: 15, Purpose: "品牌定格", Visual: "产品与 Logo", Voiceover: "了解娇兰。"},
	}
	if err := plan.Validate(); err == nil {
		t.Fatal("legacy four-shot plan should remain invalid for new video generation")
	}
	workspace, err := PrepareBrandAudioFixture(plan, contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	voice := workspace.CurrentMix().Track(BrandAudioTrackVoiceover)
	if voice == nil || len(voice.Clips) != 4 || workspace.MasterDurationMS != 15000 {
		t.Fatalf("legacy timeline was not preserved: %#v", workspace)
	}
}

func TestPrepareBrandSoundDesignFixtureDoesNotRequireNarration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	plan.VoiceDirection = ""
	for index := range plan.Shots {
		plan.Shots[index].Voiceover = ""
	}
	plan.SoundDesignIntent = SoundDesignIntent{
		MusicDirection:    "克制的水感电子氛围，结尾清晰收束",
		SoundEffectFocus:  []string{"精华液流动", "产品定格"},
		SourceAudioPolicy: "mute",
		Avoid:             []string{"人声"},
	}
	workspace, err := PrepareBrandSoundDesignFixture(plan, contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.ContractVersion != BrandAudioWorkspaceV2 || workspace.CurrentMix().Track(BrandAudioTrackVoiceover) != nil || workspace.CurrentMix().Track(BrandAudioTrackAmbience) == nil {
		t.Fatalf("unexpected v2 sound design workspace: %#v", workspace)
	}
	for _, variant := range workspace.Variants {
		if variant.MixVersions[0].VariantID != variant.ID {
			t.Fatalf("variant mix identity is inconsistent: %#v", variant)
		}
	}
}

func TestSelectBrandAudioVariantKeepsIndependentMixHistory(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	workspace, err := PrepareBrandAudioFixture(brandAudioTestPlan(now), contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	before := workspace.CurrentMix().ContentHash
	selected, err := SelectBrandAudioMixVariant(workspace, "audio_variant_immersive_water_zh_cn", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if selected.ActiveVariantID != "audio_variant_immersive_water_zh_cn" || selected.CurrentMix() == nil || selected.CurrentMix().VariantID != selected.ActiveVariantID {
		t.Fatalf("variant was not selected: %#v", selected)
	}
	if workspace.ActiveVariantID == selected.ActiveVariantID || workspace.CurrentMix().ContentHash != before {
		t.Fatal("selecting a variant mutated the source workspace")
	}
	if selected.CurrentMix().Track(BrandAudioTrackMusic).GainDB == workspace.CurrentMix().Track(BrandAudioTrackMusic).GainDB {
		t.Fatal("A/B variants do not contain a meaningful mix difference")
	}
}

func TestUpgradeBrandAudioDirectorPreservesExistingAssetsAndAddsABVariant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	workspace, err := PrepareBrandAudioFixture(plan, contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	workspace.BlueprintVersions[0].PlannerVersion = "brand-audio-fixture-planner/v1"
	workspace.BlueprintVersions[0].Pronunciations = nil
	workspace.BlueprintVersions[0].Decisions = nil
	workspace.BlueprintVersions[0].SemanticChecks = nil
	workspace.Variants = workspace.Variants[:1]
	before := workspace.CurrentMix().ContentHash
	upgraded, err := UpgradeBrandAudioDirector(workspace, plan, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(upgraded.BlueprintVersions) != 2 || len(upgraded.Variants) != 2 || upgraded.CurrentMix().ContentHash != before {
		t.Fatalf("legacy workspace was not upgraded safely: %#v", upgraded)
	}
}

func TestAudioDirectorFlagsNarrationOverrunAndMissingVisualEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	plan.Shots[0].Voiceover = "法国娇兰二十五倍蜂皇水以黑蜂修护科技带来轻盈水感体验，现在点击了解更多产品信息"
	plan.Shots[0].Visual = "纯粹的抽象水波与留白"
	workspace, err := PrepareBrandAudioFixture(plan, contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	blueprint := workspace.BlueprintVersions[0]
	if blueprint.NarrationCues[0].FitStatus != "overrun" || blueprint.NarrationCues[0].SuggestedText == "" {
		t.Fatalf("overrun was not explained: %#v", blueprint.NarrationCues[0])
	}
	if blueprint.SemanticChecks[0].Status != "warning" || blueprint.SemanticChecks[0].Suggestion == "" {
		t.Fatalf("missing visual evidence was not explained: %#v", blueprint.SemanticChecks[0])
	}
}

func TestReviseBrandAudioMixCanAdjustNarrationTiming(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	workspace, err := PrepareBrandAudioFixture(brandAudioTestPlan(now), contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	startMS, endMS := 300, 4300
	revised, err := ReviseBrandAudioMix(workspace, []AudioMixOperation{{Op: AudioMixOperationSetClipTiming, ClipID: "voice_clip_01", TimelineStartMS: &startMS, TimelineEndMS: &endMS}}, "user_1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	clip := revised.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0]
	if clip.TimelineStartMS != startMS || clip.TimelineEndMS != endMS {
		t.Fatalf("narration timing was not revised: %#v", clip)
	}
	if workspace.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0].TimelineStartMS == startMS {
		t.Fatal("prior mix timing was mutated")
	}
}

func TestReviseBrandAudioMixAppendsImmutableVersion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	workspace, err := PrepareBrandAudioFixture(plan, contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	muted := true
	gain := -21.0
	revised, err := ReviseBrandAudioMix(workspace, []AudioMixOperation{
		{Op: AudioMixOperationSetTrackGain, TrackID: "track_music", GainDB: &gain},
		{Op: AudioMixOperationSetTrackMuted, TrackID: "track_sfx", Muted: &muted},
	}, "user_1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revised.ActiveRevision != 2 || len(revised.Variants[0].MixVersions) != 2 {
		t.Fatalf("revised workspace = %#v", revised)
	}
	if revised.Variants[0].MixVersions[0].Track(BrandAudioTrackMusic).GainDB != -18 {
		t.Fatal("prior mix revision was mutated")
	}
	oldMusic := workspace.CurrentMix().Track(BrandAudioTrackMusic)
	newMusic := revised.CurrentMix().Track(BrandAudioTrackMusic)
	if oldMusic.GainDB != -18 || newMusic.GainDB != -21 || revised.CurrentMix().ContentHash == workspace.CurrentMix().ContentHash {
		t.Fatalf("old=%#v new=%#v", oldMusic, newMusic)
	}
	if !revised.CurrentMix().Track(BrandAudioTrackSFX).Muted {
		t.Fatal("sfx mute operation was not applied")
	}
}

func TestReviseBrandAudioMixCanReplaceOneClipWithProjectAsset(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	workspace, err := PrepareBrandAudioFixture(brandAudioTestPlan(now), contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}, "user_1", now)
	if err != nil {
		t.Fatal(err)
	}
	replacement := contract.AssetVersionRef{AssetID: "asset_voice_manual", Version: 3}
	revised, err := ReviseBrandAudioMix(workspace, []AudioMixOperation{{Op: AudioMixOperationReplaceClip, ClipID: "voice_clip_01", AssetRef: &replacement}}, "user_1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	oldClip := workspace.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0]
	newClip := revised.CurrentMix().Track(BrandAudioTrackVoiceover).Clips[0]
	if oldClip.FixtureURI == "" || oldClip.AssetRef != nil || newClip.FixtureURI != "" || newClip.AssetRef == nil || *newClip.AssetRef != replacement {
		t.Fatalf("old=%#v new=%#v", oldClip, newClip)
	}
}

func TestBrandAudioFixturePersistsAndRestoresThroughWorkspace(t *testing.T) {
	t.Parallel()
	service := testService()
	repository := service.Repository.(*memoryRepository)
	service.ViralRemakes = repository
	service.BrandFilmPlanner = DeterministicBrandFilmPlanner{}
	service.BrandFilmComposer = &brandSegmentComposer{}
	service.RenderedAssets = &testRenderedAssetWriter{ref: contract.ProjectAssetRef{ProjectID: "project_1", AssetVersion: contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}}}
	ctx, rc := context.Background(), testRequestContext()
	workspace := completeBrandFilmPlanForTest(t, service, ctx, rc)
	var err error
	workspace, err = service.PrepareBrandFilmGeneration(ctx, rc.Actor, "project_1", workspace.Task.ID, PrepareBrandFilmGenerationRequest{ExpectedRevision: workspace.VideoDraft.Revision, ReferenceAsset: contract.AssetVersionRef{AssetID: "asset_reference", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for unitIndex := range workspace.VideoDraft.BrandFilm.Generation.Units {
		unitID := workspace.VideoDraft.BrandFilm.Generation.Units[unitIndex].ID
		jobID := "provider_job_" + unitID
		workspace, err = service.RegisterBrandFilmGenerationAttempt(ctx, rc.Actor, "project_1", workspace.Task.ID, unitID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		workspace, err = service.ReconcileBrandFilmGenerationAttempt(ctx, rc.Actor, "project_1", workspace.Task.ID, unitID, contract.ProviderJob{ID: jobID, ProjectID: "project_1", ProviderStatus: contract.ProviderJobSucceeded, ProjectAssetRefs: []contract.ProjectAssetRef{{ProjectID: "project_1", AssetVersion: contract.AssetVersionRef{AssetID: contract.AssetID("asset_" + unitID), Version: 1}}}})
		if err != nil {
			t.Fatal(err)
		}
		attempt := workspace.VideoDraft.BrandFilm.Generation.Units[unitIndex].Attempts[0]
		workspace, err = service.LockBrandFilmGenerationUnit(ctx, rc.Actor, "project_1", workspace.Task.ID, LockBrandFilmUnitRequest{ExpectedRevision: workspace.VideoDraft.Revision, UnitID: unitID, AttemptID: attempt.ID})
		if err != nil {
			t.Fatal(err)
		}
	}
	workspace, err = service.ComposeBrandFilmPreview(ctx, rc, "project_1", workspace.Task.ID, ComposeBrandFilmPreviewRequest{ExpectedRevision: workspace.VideoDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.PrepareBrandFilmAudio(ctx, rc.Actor, "project_1", workspace.Task.ID, BrandFilmRevisionRequest{ExpectedRevision: workspace.VideoDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.VideoDraft.BrandFilm.Audio == nil || workspace.VideoDraft.BrandFilm.Audio.CurrentMix() == nil {
		t.Fatalf("audio workspace = %#v", workspace.VideoDraft.BrandFilm.Audio)
	}
	gain := -20.0
	workspace, err = service.UpdateBrandFilmAudioMix(ctx, rc.Actor, "project_1", workspace.Task.ID, UpdateBrandAudioMixRequest{ExpectedRevision: workspace.VideoDraft.Revision, Operations: []AudioMixOperation{{Op: AudioMixOperationSetTrackGain, TrackID: "track_music", GainDB: &gain}}})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.GetBrandFilmWorkspace(ctx, rc.Actor, "project_1", workspace.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.VideoDraft.BrandFilm.Audio.ActiveRevision != 2 || restored.VideoDraft.BrandFilm.Audio.CurrentMix().Track(BrandAudioTrackMusic).GainDB != -20 {
		t.Fatalf("restored audio = %#v", restored.VideoDraft.BrandFilm.Audio)
	}
}

func TestMaterializeBrandAudioFixtureAssetsCreatesImmutableAssetBackedMix(t *testing.T) {
	t.Parallel()
	service := testService()
	repository := service.Repository.(*memoryRepository)
	service.ViralRemakes = repository
	service.BrandFilmPlanner = DeterministicBrandFilmPlanner{}
	service.BrandFilmComposer = &brandSegmentComposer{}
	service.RenderedAssets = &testRenderedAssetWriter{ref: contract.ProjectAssetRef{ProjectID: "project_1", AssetVersion: contract.AssetVersionRef{AssetID: "asset_brand_preview", Version: 1}}}
	fixtureAssets := &testBrandAudioAssetWriter{}
	service.AudioAssets = fixtureAssets
	ctx, rc := context.Background(), testRequestContext()
	workspace := completeBrandFilmPlanForTest(t, service, ctx, rc)
	var err error
	workspace, err = service.PrepareBrandFilmGeneration(ctx, rc.Actor, "project_1", workspace.Task.ID, PrepareBrandFilmGenerationRequest{ExpectedRevision: workspace.VideoDraft.Revision, ReferenceAsset: contract.AssetVersionRef{AssetID: "asset_reference", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for unitIndex := range workspace.VideoDraft.BrandFilm.Generation.Units {
		unitID := workspace.VideoDraft.BrandFilm.Generation.Units[unitIndex].ID
		jobID := "provider_job_" + unitID
		workspace, err = service.RegisterBrandFilmGenerationAttempt(ctx, rc.Actor, "project_1", workspace.Task.ID, unitID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		workspace, err = service.ReconcileBrandFilmGenerationAttempt(ctx, rc.Actor, "project_1", workspace.Task.ID, unitID, contract.ProviderJob{ID: jobID, ProjectID: "project_1", ProviderStatus: contract.ProviderJobSucceeded, ProjectAssetRefs: []contract.ProjectAssetRef{{ProjectID: "project_1", AssetVersion: contract.AssetVersionRef{AssetID: contract.AssetID("asset_" + unitID), Version: 1}}}})
		if err != nil {
			t.Fatal(err)
		}
		attempt := workspace.VideoDraft.BrandFilm.Generation.Units[unitIndex].Attempts[0]
		workspace, err = service.LockBrandFilmGenerationUnit(ctx, rc.Actor, "project_1", workspace.Task.ID, LockBrandFilmUnitRequest{ExpectedRevision: workspace.VideoDraft.Revision, UnitID: unitID, AttemptID: attempt.ID})
		if err != nil {
			t.Fatal(err)
		}
	}
	workspace, err = service.ComposeBrandFilmPreview(ctx, rc, "project_1", workspace.Task.ID, ComposeBrandFilmPreviewRequest{ExpectedRevision: workspace.VideoDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.PrepareBrandFilmAudio(ctx, rc.Actor, "project_1", workspace.Task.ID, BrandFilmRevisionRequest{ExpectedRevision: workspace.VideoDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	a0Revision := workspace.VideoDraft.Revision
	workspace, err = service.MaterializeBrandFilmAudioAssets(ctx, rc, "project_1", workspace.Task.ID, BrandFilmRevisionRequest{ExpectedRevision: a0Revision})
	if err != nil {
		t.Fatal(err)
	}
	audio := workspace.VideoDraft.BrandFilm.Audio
	if audio == nil || audio.ContractVersion != BrandAudioWorkspaceV2 || audio.ActiveRevision != 2 || len(audio.Attempts) != 5 || len(fixtureAssets.calls) != 5 {
		t.Fatalf("materialized audio = %#v calls=%d", audio, len(fixtureAssets.calls))
	}
	if audio.Variants[0].MixVersions[0].Track(BrandAudioTrackAmbience).Clips[0].FixtureURI == "" {
		t.Fatal("A0 mix revision was mutated")
	}
	for _, track := range audio.CurrentMix().Tracks {
		for _, clip := range track.Clips {
			if clip.AssetRef == nil || clip.FixtureURI != "" || len(clip.WaveformPeaks) == 0 || clip.GenerationAttemptID == "" {
				t.Fatalf("asset-backed clip = %#v", clip)
			}
		}
	}
	restored, err := service.GetBrandFilmWorkspace(ctx, rc.Actor, "project_1", workspace.Task.ID)
	if err != nil || restored.VideoDraft.BrandFilm.Audio.CurrentMix().Track(BrandAudioTrackMusic).Clips[0].AssetRef == nil {
		t.Fatalf("restored materialized audio = %#v err=%v", restored.VideoDraft.BrandFilm.Audio, err)
	}
}

type testBrandAudioAssetWriter struct {
	calls []string
}

type testBrandSoundAssetGenerator struct {
	calls []provider.SoundAssetGenerationInput
}

func (g *testBrandSoundAssetGenerator) GenerateSoundAsset(_ context.Context, input provider.SoundAssetGenerationInput) (provider.SoundAssetGenerationResult, error) {
	if err := input.Validate(); err != nil {
		return provider.SoundAssetGenerationResult{}, err
	}
	g.calls = append(g.calls, input)
	audio, _, err := RenderBrandAudioFixtureWAV(input.TrackType, input.Prompt, input.DurationMS)
	if err != nil {
		return provider.SoundAssetGenerationResult{}, err
	}
	return provider.SoundAssetGenerationResult{Audio: audio, Codec: "wav", DurationMS: input.DurationMS, ProviderRequestID: "sound-request-" + input.TrackType, ProviderSnapshot: "test/sound-model/v1"}, nil
}

func TestGenerateBrandSoundAssetsPersistsRealProviderAttempts(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	plan.VoiceDirection = ""
	plan.MusicDirection = ""
	plan.SoundDesignIntent = SoundDesignIntent{MusicDirection: "柔和的金色氛围音乐", SoundEffectFocus: []string{"蜜滴", "玻璃质地"}, SourceAudioPolicy: "mute", Avoid: []string{"人声"}}
	workspace, err := PrepareBrandSoundDesignFixture(plan, contract.AssetVersionRef{AssetID: "visual", Version: 1}, "tester", now)
	if err != nil {
		t.Fatal(err)
	}
	writer := &testBrandAudioAssetWriter{}
	generator := &testBrandSoundAssetGenerator{}
	rc := contract.RequestContext{Actor: contract.ActorContext{OrganizationID: "org_1", Principal: contract.Principal{ID: "tester"}}}
	generated, err := generateBrandSoundAssets(context.Background(), rc, "project_1", "task_1", workspace, writer, generator, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(generator.calls) != 5 || len(generated.Attempts) != 5 || len(writer.calls) != 5 {
		t.Fatalf("sound generation calls=%d attempts=%d ingests=%d", len(generator.calls), len(generated.Attempts), len(writer.calls))
	}
	if generated.CurrentMix().Track(BrandAudioTrackMusic).RightsStatus != "ai_generated" {
		t.Fatalf("music rights status = %#v", generated.CurrentMix().Track(BrandAudioTrackMusic))
	}
	for _, attempt := range generated.Attempts {
		if attempt.FixtureMode || attempt.ProviderSnapshot != "test/sound-model/v1" || attempt.OutputAssetRef == nil {
			t.Fatalf("AI attempt = %#v", attempt)
		}
	}
}

func TestPrepareBrandSoundDesignFixtureProvidesThreeSoundTreatments(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	plan := brandAudioTestPlan(now)
	plan.VoiceDirection, plan.MusicDirection = "", ""
	plan.SoundDesignIntent = SoundDesignIntent{MusicDirection: "高级克制的金色氛围音乐", SoundEffectFocus: []string{"蜜滴", "玻璃质地"}, SourceAudioPolicy: "mute", Avoid: []string{"人声"}}
	workspace, err := PrepareBrandSoundDesignFixture(plan, contract.AssetVersionRef{AssetID: "visual", Version: 1}, "tester", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Variants) != 3 {
		t.Fatalf("variant count = %d", len(workspace.Variants))
	}
	for _, id := range []string{"sound_treatment_restrained_luxury", "sound_treatment_immersive_water", "sound_treatment_youthful_light"} {
		if !hasBrandAudioVariant(workspace.Variants, id) {
			t.Fatalf("missing sound treatment %s", id)
		}
	}
}

func (w *testBrandAudioAssetWriter) IngestDerivedAudio(_ context.Context, _ contract.RequestContext, projectID contract.ProjectID, derivationID string, content io.Reader, sizeBytes int64, mimeType string, _ []contract.ResourceRef) (contract.ProjectAssetRef, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return contract.ProjectAssetRef{}, err
	}
	if len(data) != int(sizeBytes) || !bytes.HasPrefix(data, []byte("RIFF")) || mimeType != "audio/wav" {
		return contract.ProjectAssetRef{}, fmt.Errorf("invalid fixture audio")
	}
	w.calls = append(w.calls, derivationID)
	return contract.ProjectAssetRef{ProjectID: projectID, AssetVersion: contract.AssetVersionRef{AssetID: contract.AssetID(fmt.Sprintf("asset_audio_%02d", len(w.calls))), Version: 1}}, nil
}

func brandAudioTestPlan(now time.Time) BrandFilmPlanVersion {
	return BrandFilmPlanVersion{
		Revision: 2, MasterDurationMS: 15000, ConceptID: "concept_01", Title: "黄金复原蜜", StorySummary: "自然能量进入产品并完成品牌定格。",
		VoiceDirection: "高级、温润、克制", MusicDirection: "克制弦乐与水感氛围", CreatedAt: now,
		Shots: []BrandFilmShot{
			{ID: "shot_01", Order: 1, StartSecond: 0, EndSecond: 5, Purpose: "建立世界", Visual: "蜂巢与水滴", Voiceover: "当自然的修护能量被唤醒。"},
			{ID: "shot_02", Order: 2, StartSecond: 5, EndSecond: 10, Purpose: "产品体验", Visual: "产品与水感质地", Voiceover: "轻盈补水，温润修护。"},
			{ID: "shot_03", Order: 3, StartSecond: 10, EndSecond: 15, Purpose: "品牌定格", Visual: "产品正面定格", Voiceover: "法国娇兰。"},
		},
	}
}
