package creative

import (
	"fmt"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/media"
)

// CompileBrandAudioMix translates the domain-owned, immutable mix revision
// into the small infrastructure request understood by media renderers.
func CompileBrandAudioMix(organizationID contract.OrganizationID, projectID contract.ProjectID, mix AudioMixVersion) (media.AudioMixRequest, error) {
	request := media.AudioMixRequest{
		OrganizationID: organizationID, ProjectID: projectID, Visual: mix.VisualPreview,
		MasterDurationMS: mix.MasterDurationMS, SampleRate: mix.SampleRate, ChannelLayout: mix.ChannelLayout,
	}
	hasSolo := false
	for _, track := range mix.Tracks {
		hasSolo = hasSolo || track.Solo
	}
	for _, track := range mix.Tracks {
		if track.Muted || (hasSolo && !track.Solo) {
			continue
		}
		if !supportedAudioTrack(track.Type) {
			return media.AudioMixRequest{}, fmt.Errorf("unsupported audio track type %q", track.Type)
		}
		for _, clip := range track.Clips {
			if clip.AssetRef == nil {
				return media.AudioMixRequest{}, fmt.Errorf("audio clip %s has not been materialized", clip.ID)
			}
			request.Clips = append(request.Clips, media.AudioMixClip{
				ID: clip.ID, TrackType: track.Type, Asset: *clip.AssetRef,
				TimelineStartMS: clip.TimelineStartMS, TimelineEndMS: clip.TimelineEndMS,
				SourceInMS: clip.SourceInMS, SourceOutMS: clip.SourceOutMS,
				GainDB: track.GainDB + clip.GainDB, FadeInMS: clip.FadeInMS, FadeOutMS: clip.FadeOutMS,
				PlaybackRate: clip.PlaybackRate,
			})
		}
	}
	if err := request.Validate(); err != nil {
		return media.AudioMixRequest{}, fmt.Errorf("compile brand audio mix: %w", err)
	}
	return request, nil
}

func supportedAudioTrack(value string) bool {
	switch strings.TrimSpace(value) {
	case BrandAudioTrackVoiceover, BrandAudioTrackAmbience, BrandAudioTrackMusic, BrandAudioTrackSFX, BrandAudioTrackSource:
		return true
	default:
		return false
	}
}
