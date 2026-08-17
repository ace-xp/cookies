package media

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
)

const AudioMixRendererVersion = "ffmpeg-audio-mix/v1"

type AudioSource interface {
	OpenAudio(context.Context, contract.OrganizationID, contract.ProjectID, contract.AssetVersionRef) (assets.AssetVersion, io.ReadCloser, error)
}

type AudioMixClip struct {
	ID              string
	TrackType       string
	Asset           contract.AssetVersionRef
	TimelineStartMS int
	TimelineEndMS   int
	SourceInMS      int
	SourceOutMS     int
	GainDB          float64
	FadeInMS        int
	FadeOutMS       int
	PlaybackRate    float64
}

type AudioMixRequest struct {
	OrganizationID   contract.OrganizationID
	ProjectID        contract.ProjectID
	Visual           contract.AssetVersionRef
	MasterDurationMS int
	SampleRate       int
	ChannelLayout    string
	Clips            []AudioMixClip
}

func (r AudioMixRequest) Validate() error {
	if r.OrganizationID == "" || r.ProjectID == "" || r.MasterDurationMS < 1 {
		return fmt.Errorf("audio mix scope and master duration are required")
	}
	if err := r.Visual.Validate(); err != nil {
		return fmt.Errorf("visual asset: %w", err)
	}
	if r.SampleRate < 8000 || r.ChannelLayout != "stereo" || len(r.Clips) == 0 {
		return fmt.Errorf("audio mix requires clips and a supported output format")
	}
	for index, clip := range r.Clips {
		if strings.TrimSpace(clip.ID) == "" || clip.Asset.Validate() != nil || clip.TimelineStartMS < 0 || clip.TimelineEndMS <= clip.TimelineStartMS || clip.TimelineEndMS > r.MasterDurationMS || clip.SourceInMS < 0 || clip.PlaybackRate <= 0 {
			return fmt.Errorf("audio mix clip %d is invalid", index+1)
		}
	}
	return nil
}

type AudioMixRenderer interface {
	RenderAudioMix(context.Context, AudioMixRequest) (CompositionOutput, error)
}

type FFmpegAudioMixRenderer struct {
	FFmpegPath string
	WorkRoot   string
	Videos     VideoSource
	Audio      AudioSource
	Probe      assets.VideoMetadataProbe
	Runner     CommandRunner
}

func (r FFmpegAudioMixRenderer) RenderAudioMix(ctx context.Context, request AudioMixRequest) (CompositionOutput, error) {
	if err := request.Validate(); err != nil {
		return CompositionOutput{}, err
	}
	if strings.TrimSpace(r.FFmpegPath) == "" || r.Videos == nil || r.Audio == nil || r.Probe == nil {
		return CompositionOutput{}, fmt.Errorf("audio mix rendering capability is unavailable")
	}
	runner := r.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	root := strings.TrimSpace(r.WorkRoot)
	if root == "" {
		root = filepath.Join(".data", "audio-mix-work")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return CompositionOutput{}, fmt.Errorf("create audio mix work root: %w", err)
	}
	dir, err := os.MkdirTemp(root, "mix-*")
	if err != nil {
		return CompositionOutput{}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	visualPath := filepath.Join(dir, "visual.mp4")
	if err := copyVideoAsset(ctx, r.Videos, request, visualPath); err != nil {
		cleanup()
		return CompositionOutput{}, err
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", visualPath}
	for index, clip := range request.Clips {
		path := filepath.Join(dir, fmt.Sprintf("audio-%02d.bin", index+1))
		if err := copyAudioAsset(ctx, r.Audio, request, clip.Asset, path); err != nil {
			cleanup()
			return CompositionOutput{}, err
		}
		args = append(args, "-i", path)
	}
	graph, outputLabel := BuildAudioMixFilter(request)
	outputPath := filepath.Join(dir, "mixed-preview.mp4")
	args = append(args, "-filter_complex", graph, "-map", "0:v:0", "-map", outputLabel, "-c:v", "copy", "-c:a", "aac", "-ar", strconv.Itoa(request.SampleRate), "-ac", "2", "-t", seconds(request.MasterDurationMS), "-movflags", "+faststart", outputPath)
	if err := runner.Run(ctx, r.FFmpegPath, args...); err != nil {
		cleanup()
		return CompositionOutput{}, fmt.Errorf("render audio mix: %w", err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		cleanup()
		return CompositionOutput{}, err
	}
	metadata, err := r.Probe.Probe(ctx, contents)
	if err != nil {
		cleanup()
		return CompositionOutput{}, fmt.Errorf("validate audio mix output: %w", err)
	}
	if delta := math.Abs(float64(metadata.DurationMS - int64(request.MasterDurationMS))); delta > 250 || strings.TrimSpace(metadata.AudioCodec) == "" {
		cleanup()
		return CompositionOutput{}, fmt.Errorf("audio mix output metadata does not match the master timeline")
	}
	file, err := os.Open(outputPath)
	if err != nil {
		cleanup()
		return CompositionOutput{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		cleanup()
		return CompositionOutput{}, err
	}
	return CompositionOutput{Content: &cleanupReadCloser{ReadCloser: file, cleanup: cleanup}, SizeBytes: info.Size(), Metadata: metadata}, nil
}

func BuildAudioMixFilter(request AudioMixRequest) (string, string) {
	parts := make([]string, 0, len(request.Clips)+8)
	groups := map[string][]string{}
	for index, clip := range request.Clips {
		label := fmt.Sprintf("clip%d", index)
		duration := clip.TimelineEndMS - clip.TimelineStartMS
		sourceOut := clip.SourceOutMS
		if sourceOut <= clip.SourceInMS {
			sourceOut = clip.SourceInMS + duration
		}
		filters := []string{fmt.Sprintf("atrim=start=%s:end=%s", seconds(clip.SourceInMS), seconds(sourceOut)), "asetpts=PTS-STARTPTS"}
		if math.Abs(clip.PlaybackRate-1) > 0.0001 {
			filters = append(filters, fmt.Sprintf("atempo=%.4f", clip.PlaybackRate))
		}
		filters = append(filters, "apad", fmt.Sprintf("atrim=duration=%s", seconds(duration)), fmt.Sprintf("volume=%.3fdB", clip.GainDB))
		if clip.FadeInMS > 0 {
			filters = append(filters, fmt.Sprintf("afade=t=in:st=0:d=%s", seconds(min(clip.FadeInMS, duration))))
		}
		if clip.FadeOutMS > 0 {
			fade := min(clip.FadeOutMS, duration)
			filters = append(filters, fmt.Sprintf("afade=t=out:st=%s:d=%s", seconds(duration-fade), seconds(fade)))
		}
		filters = append(filters, fmt.Sprintf("adelay=%d|%d", clip.TimelineStartMS, clip.TimelineStartMS))
		parts = append(parts, fmt.Sprintf("[%d:a]%s[%s]", index+1, strings.Join(filters, ","), label))
		groups[clip.TrackType] = append(groups[clip.TrackType], "["+label+"]")
	}
	bus := func(kind, label string) string {
		inputs := groups[kind]
		if len(inputs) == 0 {
			return ""
		}
		if len(inputs) == 1 {
			parts = append(parts, inputs[0]+fmt.Sprintf("anull[%s]", label))
		} else {
			parts = append(parts, strings.Join(inputs, "")+fmt.Sprintf("amix=inputs=%d:normalize=0[%s]", len(inputs), label))
		}
		return "[" + label + "]"
	}
	voice, ambience, music := bus("voiceover", "voicebus"), bus("ambience", "ambiencebus"), bus("music", "musicbus")
	sfx, source := bus("sfx", "sfxbus"), bus("source_audio", "sourcebus")
	if sfx != "" && music != "" {
		parts = append(parts, sfx+"asplit=2[sfxsidechain][sfxout]")
		parts = append(parts, music+"[sfxsidechain]sidechaincompress=threshold=0.04:ratio=5:attack=12:release=260[duckedmusic]")
		sfx = "[sfxout]"
		music = "[duckedmusic]"
	} else if voice != "" && music != "" {
		parts = append(parts, voice+"asplit=2[voicesidechain][voiceout]")
		parts = append(parts, music+"[voicesidechain]sidechaincompress=threshold=0.03:ratio=8:attack=20:release=300[duckedmusic]")
		voice = "[voiceout]"
		music = "[duckedmusic]"
	}
	finalInputs := []string{}
	for _, input := range []string{voice, ambience, music, sfx, source} {
		if input != "" {
			finalInputs = append(finalInputs, input)
		}
	}
	parts = append(parts, strings.Join(finalInputs, "")+fmt.Sprintf("amix=inputs=%d:normalize=0,alimiter=limit=0.95,loudnorm=I=-16:TP=-1.5:LRA=11,atrim=duration=%s[mixout]", len(finalInputs), seconds(request.MasterDurationMS)))
	return strings.Join(parts, ";"), "[mixout]"
}

func copyVideoAsset(ctx context.Context, source VideoSource, request AudioMixRequest, target string) error {
	_, reader, err := source.OpenVideo(ctx, request.OrganizationID, request.ProjectID, request.Visual)
	if err != nil {
		return err
	}
	defer reader.Close()
	return copyToFile(target, reader)
}

func copyAudioAsset(ctx context.Context, source AudioSource, request AudioMixRequest, ref contract.AssetVersionRef, target string) error {
	_, reader, err := source.OpenAudio(ctx, request.OrganizationID, request.ProjectID, ref)
	if err != nil {
		return err
	}
	defer reader.Close()
	return copyToFile(target, reader)
}

func copyToFile(target string, reader io.Reader) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func seconds(ms int) string { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) }
