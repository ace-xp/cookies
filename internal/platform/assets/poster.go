package assets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PosterMIMEType 是抽出来的首帧图的类型。JPEG 不是随便选的：
// 缩略图要的是小和快，不是无损。
const PosterMIMEType = "image/jpeg"

// PosterExtractor 从一段视频里抽一帧当封面。
//
// 和 VideoMetadataProbe 一样进字节出字节，不向调用方暴露临时文件路径——
// 调用方拿到的是一张图，不是一个要记得删的文件。
type PosterExtractor interface {
	ExtractPoster(ctx context.Context, contents []byte) ([]byte, error)
}

// FFmpegPosterExtractor 用 ffmpeg 抽帧。形状照着 FFprobeVideoProbe 来，
// 包括 WorkRoot 的默认值约定和临时文件一定删掉这条。
type FFmpegPosterExtractor struct {
	Path     string
	WorkRoot string
	// SeekSeconds 是抽第几秒。默认 1 秒而不是 0：很多片子第一帧是纯黑的
	// 开场，抽出来的封面等于没有。
	SeekSeconds float64
}

func (p FFmpegPosterExtractor) ExtractPoster(ctx context.Context, contents []byte) ([]byte, error) {
	if strings.TrimSpace(p.Path) == "" {
		return nil, fmt.Errorf("ffmpeg path is required for poster extraction")
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("video contents are required for poster extraction")
	}
	workRoot := strings.TrimSpace(p.WorkRoot)
	if workRoot == "" {
		workRoot = filepath.Join(".data", "poster-work")
	}
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create poster work directory: %w", err)
	}
	input, err := os.CreateTemp(workRoot, "poster-in-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("create poster input: %w", err)
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if err := input.Chmod(0o600); err != nil {
		input.Close()
		return nil, err
	}
	if _, err := input.Write(contents); err != nil {
		input.Close()
		return nil, err
	}
	if err := input.Close(); err != nil {
		return nil, err
	}

	outputName := inputName + ".jpg"
	defer os.Remove(outputName)

	seek := p.SeekSeconds
	if seek <= 0 {
		seek = 1
	}
	// -ss 放在 -i 前面是快速定位（关键帧对齐，够用且快得多）。
	// -frames:v 1 只要一帧；-vf scale 把长边压到 640，缩略图不需要更大。
	command := exec.CommandContext(ctx, p.Path,
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", fmt.Sprintf("%.3f", seek), "-i", inputName,
		"-frames:v", "1", "-vf", "scale='min(640,iw)':-2",
		"-q:v", "4", outputName)
	if output, err := command.CombinedOutput(); err != nil {
		// 视频短于 SeekSeconds 时上面那次会抽不到帧，退回第 0 秒再试一次。
		// 六秒的广告前贴在库里不算少见。
		retry := exec.CommandContext(ctx, p.Path,
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", inputName, "-frames:v", "1", "-vf", "scale='min(640,iw)':-2",
			"-q:v", "4", outputName)
		if retryOutput, retryErr := retry.CombinedOutput(); retryErr != nil {
			return nil, fmt.Errorf("ffmpeg 抽帧失败: %w: %s / %s", err,
				strings.TrimSpace(string(output)), strings.TrimSpace(string(retryOutput)))
		}
	}
	poster, err := os.ReadFile(outputName)
	if err != nil {
		return nil, fmt.Errorf("read poster output: %w", err)
	}
	if len(poster) == 0 {
		return nil, fmt.Errorf("ffmpeg 抽出来的封面是空的")
	}
	return poster, nil
}
