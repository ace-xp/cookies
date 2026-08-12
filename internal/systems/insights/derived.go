package insights

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 客观可测层：从素材文件本身量出来的变量。
//
// 这一层的存在理由是归因准入（AdmissibleForAttribution）。模型推断进不了归因——
// 拿一个猜测去解释另一个猜测，减不掉任何一层假设；人工标注进得了，但人得一项项填。
// 中间缺的这一层是「不用猜也不用填」的那部分：时长和画幅摆在文件里，读一次就有，
// 同一个文件读两遍结果一样。
//
// 在此之前这一层在库里是空的，而且是**写不进去**的空：SourceDerived 在 Go 里从
// P0 就定义了，归因和相似度也都认它，但建表时的 CHECK 只放 ('ai','human') 过。
// 20260812100000_insight_feature_derived.up.sql 放开了那道约束。
//
// 边界要说清楚：这里只读**探测已经落库的元数据**，不重新跑 ffprobe。素材上传时
// UploadService 已经探过一次（VideoProbe/AudioProbe），结果就存在 AssetVersion 上。
// 洞察再探一遍既慢又可能和素材库对不上——两处各自量出的时长不一致，谁对？
//
// 镜头数、语速这类要真正解码画面和音轨才算得出来的，不在这一层。它们要么等素材库
// 那边加对应的探测，要么走多模态（见「视频语义特征」那条线）——在这里用模型猜一个
// 数然后标成「客观可测」，是这一层最坏的死法。

// MediaFacts 是洞察从媒体资产那边要的全部东西。
//
// 独立定义而不是直接用 assets.AssetVersion，理由同 DeliveryExecutionSnapshot：
// 洞察不该 import 素材库。素材库的版本结构里有二十来个字段（blob 位置、编码、
// 渲染任务 ID……），洞察只要三个数，把整个结构拖进来等于把两个系统焊死。
type MediaFacts struct {
	// Measured 说明探测到底成没成功。false 的时候后面三个数一律不可信——
	// 探测失败时它们是零值，而「时长 0 秒」和「不知道多长」是两回事。
	Measured bool
	// Unavailable 是 Measured 为 false 时的人话理由，会原样出现在报错里。
	Unavailable string

	DurationSeconds float64
	WidthPixels     int
	HeightPixels    int
}

// MediaReader 是洞察对素材库的第二个依赖（第一个是导入时读快照）。
// 只读，而且只读元数据——洞察不存文件，也不该有能力去动它。
type MediaReader interface {
	ReadMediaFacts(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID,
		platformAssetID string, platformAssetVersion int64) (MediaFacts, error)
}

// DeriveFeaturesRequest 只要一个版本号。这一层没有任何可配的东西——
// 量出来是多少就是多少，调用方没有可以影响结果的输入。
type DeriveFeaturesRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

// 画幅落成三挡，不落原始像素。
//
// 相似度是按取值字符串精确重叠算的（similar.go），"1080x1920" 这种值永远匹配不上
// 任何人手填的东西，等于白写。而人真正会拿来比较的也就是竖版横版方形这个粒度——
// 「这两条都是竖版」是一句有用的话，「这两条都是 1080x1920」不是。
const (
	aspectPortrait  = "竖版"
	aspectLandscape = "横版"
	aspectSquare    = "方形"
)

// aspectBucket 把宽高比归成三挡。1:1 附近留 3% 的容差——1080x1084 这种
// 编码器凑出来的尺寸，人眼看就是方的，没必要因为差四个像素判成竖版。
func aspectBucket(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	ratio := float64(width) / float64(height)
	if math.Abs(ratio-1) <= 0.03 {
		return aspectSquare
	}
	if ratio < 1 {
		return aspectPortrait
	}
	return aspectLandscape
}

// DeriveFeatures 写客观可测层。
//
// 与 ExtractFeatures / PatchFeatures 的关键区别是**它不推进素材状态**：
// To 就是素材当前的状态。量一次时长不是一个需要人复核的结论，把素材从「已确认」
// 打回「待确认」等于要求人为一个客观事实重新签一次字。
//
// 顺带挡住了一个洞：只量过、没提取过的素材仍然停在「可分析」，而 ConfirmAssetAnalysis
// 只从「待确认」和「待复审」进得来——所以没人能靠两条客观量把一份没做过内容分析的
// 素材确认掉。
func (s Service) DeriveFeatures(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID,
	assetID string, request DeriveFeaturesRequest) ([]AssetFeature, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return nil, err
	}
	if s.Media == nil {
		return nil, fmt.Errorf("%w: 没接素材库，量不到文件", ErrInvalidState)
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return nil, err
	}
	if !asset.TypeIdentified() {
		return nil, fmt.Errorf("%w: 素材类型待识别，无法确定要写哪套特征", ErrInvalidState)
	}
	if asset.PlatformAssetID == "" || asset.PlatformAssetVersion == 0 {
		return nil, fmt.Errorf("%w: 这条素材没有指向素材库里的文件，没有文件就没有客观量。"+
			"从创意导入的素材才带这个引用，手工登记的要先补上", ErrInvalidState)
	}

	facts, err := s.Media.ReadMediaFacts(ctx, actor, projectID, asset.PlatformAssetID, asset.PlatformAssetVersion)
	if err != nil {
		return nil, err
	}
	// 探测没成功就什么都不写。补一个「大概 30 秒」进客观可测层，比这一层空着坏得多:
	// 归因认这一层，认的就是它不猜。
	if !facts.Measured {
		reason := strings.TrimSpace(facts.Unavailable)
		if reason == "" {
			reason = "素材库没有这个文件的探测结果"
		}
		return nil, fmt.Errorf("%w: %s，量不出客观变量", ErrInvalidState, reason)
	}

	inputs := measurableFeatures(asset.AssetType, facts)
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: 这个文件里没有能量出来的变量——"+
			"图文类素材没有时长，探测不出画幅的文件也归到这里", ErrInvalidState)
	}

	now := s.now()
	features := make([]AssetFeature, 0, len(inputs))
	for _, input := range inputs {
		if validateErr := input.validate(asset.AssetType); validateErr != nil {
			return nil, validateErr
		}
		id, idErr := s.idGenerator()("assetfeature")
		if idErr != nil {
			return nil, idErr
		}
		extractedAt := now
		features = append(features, AssetFeature{
			ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
			AssetType: asset.AssetType, Key: input.Key, Value: input.Value,
			// 不带置信度：量出来的数没有「可能是 30 秒」这回事。
			// 不进复核队列：ReviewAuthored 说的是「没有推断，我来填」，
			// 客观量正是这个意思——只不过填的是探测，不是人。
			Source: SourceDerived, ReviewState: ReviewAuthored,
			SkillID: derivedSkillID, SkillVersion: derivedSkillVersion, ExtractedAt: &extractedAt,
			Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
		})
	}

	return s.Assets.UpsertAssetFeatures(ctx, UpsertAssetFeaturesInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
		ExpectedVersion: request.ExpectedVersion, ReplaceLayer: SourceDerived, Features: features,
		// From 只放当前状态，To 也是当前状态：这次写入不移动素材，但仍然要走
		// 同一个事务和同一次乐观锁检查——特征写进去了而素材版本没动，
		// 下一个并发的人会拿着过期版本继续写。
		From: []AnalysisStatus{asset.AnalysisStatus}, To: asset.AnalysisStatus,
		Reason: fmt.Sprintf("从素材文件量出 %d 项客观变量。", len(features)),
		Now:    now,
	})
}

// derivedSkillID 占的是「谁产出的」这一格（PRD §MVP⑫ 可追溯）。这里不是 Skill，
// 是素材库的探测，但那一格问的是「回头能不能查到这个值哪来的」，答案得填。
const (
	derivedSkillID      = "platform.assets.probe"
	derivedSkillVersion = "v1"
)

// measurableFeatures 把量到的数映射成这个素材类型认识的变量。
//
// 按 schema 里有没有这个键来决定写不写，而不是按素材类型硬编码：图文类没有
// duration 字段，这里就自然不写；哪天某个类型加了新的可测字段，加一个 case 就行。
func measurableFeatures(assetType AssetType, facts MediaFacts) []FeatureInput {
	schema, ok := FeatureSchemaFor(assetType)
	if !ok {
		return nil
	}
	inputs := make([]FeatureInput, 0, 2)

	if field, has := schema.Field("duration"); has && field.Kind == FeatureKindDuration && facts.DurationSeconds > 0 {
		// 留一位小数。探测给的是 15.0166667 这种数，原样存进去，
		// 页面上会出现「15.0166667 秒」，而且两条实际一样长的素材永远不相等。
		inputs = append(inputs, FeatureInput{
			Key:   "duration",
			Value: FeatureValue{Kind: FeatureKindDuration, Number: math.Round(facts.DurationSeconds*10) / 10},
		})
	}
	if field, has := schema.Field("aspect_ratio"); has && field.Kind == FeatureKindEnum {
		if bucket := aspectBucket(facts.WidthPixels, facts.HeightPixels); bucket != "" {
			inputs = append(inputs, FeatureInput{
				Key:   "aspect_ratio",
				Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{bucket}},
			})
		}
	}
	return inputs
}
