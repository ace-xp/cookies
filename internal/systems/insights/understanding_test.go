package insights

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 这批测试守的是「视频类不用人转述」这条线，以及它的退路：
// 多模态哪一步不通，都不该让一次本来做得成的提取变成做不成。

type stubUnderstander struct {
	result MediaUnderstanding
	err    error
	calls  int
}

func (u *stubUnderstander) UnderstandMedia(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (MediaUnderstanding, error) {
	u.calls++
	return u.result, u.err
}

func readyUnderstanding() MediaUnderstanding {
	return MediaUnderstanding{
		Ready: true, ArtifactID: "mediaunderstanding_1",
		Summary:       "一条 15 秒的竖版前贴，开头三秒是价格特写。",
		VisibleText:   []string{"限时 199"},
		Observations:  []string{"第 0 帧是产品特写", "第 3 帧出现价格数字"},
		Inferences:    []string{"这条主打性价比"},
		Transcript:    []string{"今天下单立减一百"},
		KeyframeCount: 5, ModelAlias: "cookies.vision.standard", ProviderCode: "ark",
	}
}

// AM-005 要的是从素材本身提特征。视频类的「素材本身」是画面，
// 不是某个人对画面的转述。
func TestVideoExtractionFeedsTheModelWhatTheVisionModelSawNotAHumanRetelling(t *testing.T) {
	t.Parallel()
	service, generator, runs := testExtractionService(t, extractionAnswer(map[string]any{
		"hook_type": map[string]any{"terms": []string{"利益"}, "confidence": "high", "evidence": "开场就报价格"},
	}))
	understander := &stubUnderstander{result: readyUnderstanding()}
	service.Understanding = understander
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	// 关键：Content 空着。在这条路修好之前，这个请求会被直接拒掉。
	result, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version,
	})
	if err != nil {
		t.Fatalf("视频类不填正文也应当提得出来：%v", err)
	}
	if understander.calls != 1 {
		t.Fatalf("应当问过一次多模态：%d", understander.calls)
	}
	if len(result.Features) != 1 {
		t.Fatalf("features=%#v", result.Features)
	}

	payload := generator.requests[0].Messages[1].Content
	for _, want := range []string{"第 3 帧出现价格数字", "限时 199", "今天下单立减一百", "这条主打性价比"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("视觉证据没进模型：%q 不在\n%s", want, payload)
		}
	}
	// 观察和推断必须分段标注。混成一段的话，上一个模型的猜测会被下一个模型
	// 当成画面里的事实，两层推断叠起来，落库的特征说不清是从哪来的。
	if !strings.Contains(payload, "观察") || !strings.Contains(payload, "推断") {
		t.Fatalf("观察和推断没有分开标注：\n%s", payload)
	}

	// 走了哪条路要看得出来。共用一个提示词版本的话，「换了 prompt 之后效果
	// 变没变」这个问题永远算不清楚。
	if result.Run.PromptVersion != extractionVisionPromptVersion {
		t.Fatalf("视频那条路应当有自己的提示词版本：%s", result.Run.PromptVersion)
	}
	if stored := runs.finished(); len(stored) != 1 || stored[0].Status != AnalysisRunSucceeded {
		t.Fatalf("runs=%#v", stored)
	}

	// 多模态产出仍然只进 AI 层、仍然要人复核。它比人的转述可信，但仍然是推断。
	for _, feature := range result.Features {
		if feature.Source != SourceAI || feature.ReviewState != ReviewPending {
			t.Fatalf("多模态提出来的也只能落在待确认的 AI 层：%#v", feature)
		}
		if feature.Source.AdmissibleForAttribution() {
			t.Fatalf("多模态产出不该能直接进归因：%#v", feature)
		}
	}
}

// 09 §7：留痕记规模不记正文。视频这条路上还要记清楚「看的是哪一次理解」，
// 否则这批特征回头说不清是照着哪一版画面提的。
func TestVideoRunRecordsWhichUnderstandingItReadWithoutTheContent(t *testing.T) {
	t.Parallel()
	service, _, _ := testExtractionService(t, extractionAnswer(map[string]any{
		"hook_type": map[string]any{"terms": []string{"利益"}, "confidence": "high", "evidence": "开场报价"},
	}))
	service.Understanding = &stubUnderstander{result: readyUnderstanding()}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	result, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Run.InputSummary), "限时 199") {
		t.Fatalf("留痕里出现了素材内容：%s", result.Run.InputSummary)
	}
	var summary struct {
		Vision     bool   `json:"vision"`
		ArtifactID string `json:"vision_artifact_id"`
		Keyframes  int    `json:"vision_keyframes"`
		ModelAlias string `json:"vision_model_alias"`
	}
	if err := json.Unmarshal(result.Run.InputSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Vision || summary.ArtifactID != "mediaunderstanding_1" || summary.Keyframes != 5 || summary.ModelAlias == "" {
		t.Fatalf("留痕应当记下看的是哪一次理解：%#v", summary)
	}
}

// 「正在看」不是失败。记成失败的话，运营面上会凭空多出一堆提取失败，
// 而真实情况是人来早了。
func TestStillWatchingIsAWaitNotAFailure(t *testing.T) {
	t.Parallel()
	service, generator, runs := testExtractionService(t, extractionAnswer(map[string]any{}))
	service.Understanding = &stubUnderstander{result: MediaUnderstanding{Pending: true, ArtifactID: "mediaunderstanding_1"}}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version,
	})
	if !errors.Is(err, ErrUnderstandingPending) {
		t.Fatalf("error=%v", err)
	}
	if len(generator.requests) != 0 {
		t.Fatal("还没看完就不该调文本模型花钱")
	}
	if stored := runs.finished(); len(stored) != 0 {
		t.Fatalf("等待不该留下一条失败的分析任务：%#v", stored)
	}
}

// 人已经填了正文就别让他干等。这条守的是「只放宽、不收紧」：
// 改造之前能做成的请求，改造之后一个都不能变成做不成。
func TestAHumanWhoAlreadyTypedSomethingIsNotMadeToWait(t *testing.T) {
	t.Parallel()
	service, generator, _ := testExtractionService(t, extractionAnswer(map[string]any{
		"hook_type": map[string]any{"terms": []string{"问题"}, "confidence": "high", "evidence": "开场提问"},
	}))
	service.Understanding = &stubUnderstander{result: MediaUnderstanding{Pending: true}}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	result, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version, Content: "开场问「你还在为续费发愁吗」。",
	})
	if err != nil {
		t.Fatalf("多模态还在跑，但人填了描述，老路应当照走：%v", err)
	}
	if result.Run.PromptVersion != extractionPromptVersion {
		t.Fatalf("回落之后走的是老路，版本号也该是老的：%s", result.Run.PromptVersion)
	}
	if !strings.Contains(generator.requests[0].Messages[1].Content, "续费发愁") {
		t.Fatalf("回落时应当用人填的那段：%s", generator.requests[0].Messages[1].Content)
	}
}

// 视觉链路没接通时，媒体理解会落一条只有技术校验的产出。那种东西喂给特征模型
// 只会让它照着编——当成没看成，让人填。
func TestAnUnderstandingWithNothingInItIsTreatedAsNotWatched(t *testing.T) {
	t.Parallel()
	service, generator, _ := testExtractionService(t, extractionAnswer(map[string]any{}))
	service.Understanding = &stubUnderstander{result: MediaUnderstanding{Ready: true, ArtifactID: "mediaunderstanding_1"}}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error=%v", err)
	}
	if len(generator.requests) != 0 {
		t.Fatal("没拿到画面就不该调文本模型")
	}
}

// 说不出为什么要人填，人对着一条明明有视频的素材会以为系统坏了。
func TestTheReasonContentIsStillRequiredSaysWhichCaseThisIs(t *testing.T) {
	t.Parallel()
	actor := testActor()

	t.Run("图文类：本体就是那段字", func(t *testing.T) {
		service, _, _ := testExtractionService(t, extractionAnswer(map[string]any{}))
		service.Understanding = &stubUnderstander{result: readyUnderstanding()}
		article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
		_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", article.ID, AnalyzeAssetRequest{
			ExpectedVersion: article.Version,
		})
		if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "正文") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("视频类但没指向素材库里的文件", func(t *testing.T) {
		service, _, _ := testExtractionService(t, extractionAnswer(map[string]any{}))
		understander := &stubUnderstander{result: readyUnderstanding()}
		service.Understanding = understander
		// indexedAsset 不带 PlatformAssetID，正是手工登记的样子。
		asset := indexedAsset(t, service, actor, AssetTypePrerollAd)
		_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
			ExpectedVersion: asset.Version,
		})
		if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "素材库") {
			t.Fatalf("error=%v", err)
		}
		// 没有文件引用时压根不该去问多模态——问了也只能得到「找不到这个文件」。
		if understander.calls != 0 {
			t.Fatalf("不该问多模态：%d", understander.calls)
		}
	})

	t.Run("环境没接多模态", func(t *testing.T) {
		service, _, _ := testExtractionService(t, extractionAnswer(map[string]any{}))
		service.Understanding = nil
		asset := derivedAsset(t, service, actor, AssetTypePrerollAd)
		_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
			ExpectedVersion: asset.Version,
		})
		if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "多模态") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("这条视频不在多模态能看的范围内", func(t *testing.T) {
		service, _, _ := testExtractionService(t, extractionAnswer(map[string]any{}))
		service.Understanding = &stubUnderstander{result: MediaUnderstanding{Unavailable: "这条视频不在多模态能看的范围内（只看 15–90 秒的 mp4）"}}
		asset := derivedAsset(t, service, actor, AssetTypePrerollAd)
		_, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
			ExpectedVersion: asset.Version,
		})
		if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "15–90 秒") {
			t.Fatalf("error=%v", err)
		}
	})
}

// 人知道的东西模型不一定看得出来（「这条是给老客户的续费提醒」）。
// 两边都给，不互相覆盖。
func TestWhatThePersonTypedIsKeptAlongsideWhatTheModelSaw(t *testing.T) {
	t.Parallel()
	service, generator, _ := testExtractionService(t, extractionAnswer(map[string]any{
		"hook_type": map[string]any{"terms": []string{"利益"}, "confidence": "high", "evidence": "开场报价"},
	}))
	service.Understanding = &stubUnderstander{result: readyUnderstanding()}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	if _, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: asset.Version,
		Content:         "这条是给老客户的续费提醒。",
		Note:            "竖版重剪，画面同 A 版。",
	}); err != nil {
		t.Fatal(err)
	}
	payload := generator.requests[0].Messages[1].Content
	for _, want := range []string{"第 3 帧出现价格数字", "老客户的续费提醒", "竖版重剪"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("%q 不在发给模型的内容里：\n%s", want, payload)
		}
	}
}

// 只按素材 ID 去重会走过头：先按人填的正文提一次、再走多模态提一次，
// 输入完全不同，却会命中同一个幂等键拿回上一次的答案。
func TestTwoDifferentInputsOnTheSameAssetDoNotShareAnIdempotencyKey(t *testing.T) {
	t.Parallel()
	service, generator, _ := testExtractionService(t, extractionAnswer(map[string]any{
		"hook_type": map[string]any{"terms": []string{"利益"}, "confidence": "high", "evidence": "开场报价"},
	}))
	service.Understanding = &stubUnderstander{result: readyUnderstanding()}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	current := mustGetAsset(t, service, actor, asset.ID)
	if _, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: current.Version, Content: "人手写的一段画面描述。",
	}); err != nil {
		t.Fatal(err)
	}
	current = mustGetAsset(t, service, actor, asset.ID)
	if _, err := service.AnalyzeAsset(context.Background(), actor, "project_1", asset.ID, AnalyzeAssetRequest{
		ExpectedVersion: current.Version,
	}); err != nil {
		t.Fatal(err)
	}
	if first, second := generator.requests[0].InvocationKey, generator.requests[1].InvocationKey; first == second {
		t.Fatalf("输入变了幂等键就该变，否则第二次拿回的是上一次的答案：%q", first)
	}
}
