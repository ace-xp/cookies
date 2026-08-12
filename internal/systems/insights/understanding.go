package insights

import (
	"context"
	"fmt"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 视频语义特征：把视频真的喂进模型，而不是让人写一段画面描述再喂那段字。
//
// 在此之前，视频类素材的提取和图文走的是同一条路：人在输入框里敲一段「开头是
// 产品特写，第 3 秒出现价格」，后端把这段字发给**文本**模型，模型从这段字里
// 提特征。结果是模型分析的是那个人的转述能力，不是这条素材——同一条视频换个人
// 描述，提出来的特征就变了。而 PRD AM-005 要的是从素材本身提。
//
// 现在视频类走这里：平台的「媒体理解」（internal/platform/mediaunderstanding）
// 抽关键帧、把帧喂给视觉模型，产出带帧号和时间戳的证据；洞察拿这批证据当素材
// 内容，再交给特征体系那一步映射成字段。视频进了模型，人不用再转述。
//
// **两段式，不是一步到位**，因为媒体理解是异步作业：第一次点提取只把它排上队，
// 看完了才提得出来。硬等在 HTTP 请求里的话，一条 60 秒的视频抽五帧再逐帧过视觉
// 模型，人对着转圈的按钮不知道是卡住了还是在跑。所以「正在看」是一个明确的回答
// （ErrUnderstandingPending），不是一次失败。
//
// 层次也要说清楚：**多模态产出的全部落 ai 层**。它比人的转述可信，但仍然是推断，
// 归因照样不认（AdmissibleForAttribution），照样进人工复核队列。客观可测层只收
// 从文件里量出来的数（derived.go）——视觉模型说「这条节奏偏快」，那不是量出来的。

// MediaUnderstanding 是洞察从媒体理解那边要的全部东西。
//
// 独立定义、只留几段文本，理由同 MediaFacts：洞察不该 import 平台的 Artifact。
// 那个结构里有合约版本、输入指纹、帧的资产引用、风险项、未知项……洞察这一步只要
// 「模型看到了什么」，其余的留在产出它的那一侧，那边才是它们的权威来源。
type MediaUnderstanding struct {
	// Ready 说明看完了，下面几段文本可用。
	Ready bool
	// Pending 说明排上队了还没看完。这不是失败——调用方应当让人过一会儿再点。
	Pending bool
	// Unavailable 是既没看完也不在看时的人话理由，会原样出现在报错里。
	Unavailable string

	// ArtifactID 是这次理解的编号，落进分析留痕，回头能查到这批特征看的是哪一次。
	ArtifactID string

	// Summary 是整条素材的概述；Observations 是模型直接看到的；Inferences 是它的
	// 推断；VisibleText 是画面里的字；Transcript 是口播转写。
	//
	// 分开传而不是拼成一段，是因为交给特征模型时这四类要分开标注。把推断和观察
	// 混成一段话，特征模型分不出哪句是画面里真有的，哪句是上一个模型猜的。
	Summary      string
	Observations []string
	Inferences   []string
	VisibleText  []string
	Transcript   []string

	// KeyframeCount 是抽了几帧。写进留痕，也写给人看：「看了 5 帧」和「看了 1 帧」
	// 得出的结论，可信度不是一回事。
	KeyframeCount int

	// 这一段是产出它的那次模型调用的来历，原样带进 AnalysisRun。
	ProviderCode string
	ModelAlias   string
	ModelVersion string
	ContentHash  string
}

// hasContent 说明这次理解到底有没有拿回可用的东西。
//
// Ready 却一句话都没有是可能的：视觉链路没配好时，媒体理解会落一条只有技术校验
// 的产出（applyTechnicalFallback）。那种东西喂给特征模型只会让它编，不如当没有。
func (u MediaUnderstanding) hasContent() bool {
	return strings.TrimSpace(u.Summary) != "" ||
		len(u.Observations) > 0 || len(u.Inferences) > 0 ||
		len(u.VisibleText) > 0 || len(u.Transcript) > 0
}

// MediaUnderstander 是洞察对媒体理解的唯一依赖。
//
// 一个方法同时管「排队」和「读结果」：媒体理解自己按输入指纹去重，同一个文件同一
// 套配置重复请求只会拿回同一份产出，不会重复排队也不会重复花钱。所以这里没必要
// 分成 Request/Get 两个方法，那样反而要求调用方自己判断该调哪个。
type MediaUnderstander interface {
	UnderstandMedia(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID,
		platformAssetID string, platformAssetVersion int64) (MediaUnderstanding, error)
}

// understandingFor 给一条素材取视频理解结果，顺带回答「这条素材该不该走这条路」。
//
// 返回 (理解结果, 走没走这条路, 错误)。第二个返回值为 false 且没有错误，表示这条
// 素材不适用多模态——调用方该回落到人填正文那条路，而不是报错。
func (s Service) understandingFor(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, asset Asset) (MediaUnderstanding, bool, error) {
	if s.Understanding == nil || !asset.AssetType.IsVideo() {
		return MediaUnderstanding{}, false, nil
	}
	if asset.PlatformAssetID == "" || asset.PlatformAssetVersion == 0 {
		return MediaUnderstanding{}, false, nil
	}
	understanding, err := s.Understanding.UnderstandMedia(ctx, actor, projectID,
		asset.PlatformAssetID, asset.PlatformAssetVersion)
	if err != nil {
		// 媒体理解挂了不该让提取整个失败——人填了正文的话，老路仍然走得通。
		// 把它当成「这条路不通」，由调用方决定回落还是报错。
		//
		// **不把 err.Error() 塞进 Unavailable**：这句话最终会原样出现在页面上，
		// 而这里的 err 可能是数据库或驱动的原文。真正的失败原因记在媒体理解自己的
		// 产出上（Artifact.ErrorCode / ErrorMessage），那边才是查它的地方。
		return MediaUnderstanding{Unavailable: "多模态这次没能看这条视频"}, false, nil
	}
	return understanding, true, nil
}

// visionContent 把视觉证据拼成交给特征模型的那段素材内容。
//
// 四类分段标注，不合并。特征体系里「口播密度」这种字段只能从转写里得出，
// 「画面节奏」只能从观察里得出——不分段的话，特征模型只能在一锅粥里猜哪句
// 对应哪个字段，而它猜错了没人看得出来。
//
// 推断也给出去，但标明是推断。丢掉的话，「这条像是在讲性价比」这类判断就没了，
// 而那恰恰是特征体系里最多的一类字段；不标明的话，上一个模型的猜测会被下一个
// 模型当成画面里的事实，两层推断叠在一起，最后落库的特征说不清是从哪来的。
func visionContent(understanding MediaUnderstanding) string {
	var builder strings.Builder
	if summary := strings.TrimSpace(understanding.Summary); summary != "" {
		fmt.Fprintf(&builder, "【整体概述】\n%s\n\n", summary)
	}
	writeSection(&builder, "画面里出现的文字", understanding.VisibleText)
	writeSection(&builder, "口播转写", understanding.Transcript)
	writeSection(&builder, "模型直接看到的（观察）", understanding.Observations)
	writeSection(&builder, "模型的推断（不是画面里的事实）", understanding.Inferences)
	return strings.TrimSpace(builder.String())
}

func writeSection(builder *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(builder, "【%s】\n", title)
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			fmt.Fprintf(builder, "- %s\n", value)
		}
	}
	builder.WriteString("\n")
}
