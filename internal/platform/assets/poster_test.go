package assets

import (
	"context"
	"errors"
	"testing"
)

func TestFFmpegPosterExtractorRequiresPathAndContent(t *testing.T) {
	// 没配 ffmpeg 路径时要明确报错，不能返回空字节当成「抽出来了一张空图」——
	// 那会让一张 0 字节的图落进素材库，前端显示成裂图，而没人知道为什么。
	extractor := FFmpegPosterExtractor{}
	if _, err := extractor.ExtractPoster(context.Background(), []byte("fake")); err == nil {
		t.Fatal("没有 ffmpeg 路径时应当报错")
	}
	withPath := FFmpegPosterExtractor{Path: "ffmpeg"}
	if _, err := withPath.ExtractPoster(context.Background(), nil); err == nil {
		t.Fatal("没有视频内容时应当报错")
	}
}

func TestFFmpegPosterExtractorReportsMissingBinary(t *testing.T) {
	extractor := FFmpegPosterExtractor{Path: "definitely-not-a-real-ffmpeg-binary"}
	_, err := extractor.ExtractPoster(context.Background(), []byte("fake video bytes"))
	if err == nil {
		t.Fatal("ffmpeg 不存在时应当报错")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("错误应当说清是 ffmpeg 的问题，得到 %v", err)
	}
}

func TestPosterMIMETypeIsAllowedForDerivedImages(t *testing.T) {
	// IngestDerivedImage 会用 allowedDeclaredImageMIME 挡住不认识的类型。
	// 抽出来的图声明成一个它不认的 MIME，落库那一步会失败，而失败发生在
	// worker 里、只留一行日志——所以在这里就把它钉死。
	if !allowedDeclaredImageMIME(PosterMIMEType) {
		t.Fatalf("%q 应当是允许的派生图类型", PosterMIMEType)
	}
}
