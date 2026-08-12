package insights

import (
	"fmt"
	"sort"
	"strings"
)

// AssetType is the content type an analysable asset belongs to (AM-004).
//
// The PRD names six types to identify (§9 AM-004: 小红书图文、公众号图文、品牌广告，
// 以及数字人、广告前贴和爆款复刻效果广告) but organises the feature systems into
// four groups (§5.1-§5.4), where the last three types share the 效果广告 base and
// each add their own pack. FeatureSchemaFor resolves that: every type gets exactly
// the fields its own content supports, and nothing else.
type AssetType string

const (
	AssetTypeXiaohongshuNote AssetType = "xiaohongshu_note" // 小红书图文　03 §5.1
	AssetTypeWechatArticle   AssetType = "wechat_article"   // 公众号图文　03 §5.2
	AssetTypeBrandAd         AssetType = "brand_ad"         // 品牌广告　　03 §5.3
	AssetTypeDigitalHumanAd  AssetType = "digital_human_ad" // 数字人效果广告　03 §5.4
	AssetTypePrerollAd       AssetType = "preroll_ad"       // 广告前贴　　　03 §5.4
	AssetTypeHitReplicaAd    AssetType = "hit_replica_ad"   // 爆款复刻　　　03 §5.4
)

// AssetTypeUnknown marks an indexed asset whose type has not been identified yet.
// It carries no feature fields on purpose: extraction must not start before
// AM-004 type identification has produced an answer.
const AssetTypeUnknown AssetType = ""

func (t AssetType) valid() bool {
	switch t {
	case AssetTypeXiaohongshuNote, AssetTypeWechatArticle, AssetTypeBrandAd,
		AssetTypeDigitalHumanAd, AssetTypePrerollAd, AssetTypeHitReplicaAd:
		return true
	}
	return false
}

// Label returns the Chinese name used across the PRD and the UI.
func (t AssetType) Label() string {
	switch t {
	case AssetTypeXiaohongshuNote:
		return "小红书图文"
	case AssetTypeWechatArticle:
		return "公众号图文"
	case AssetTypeBrandAd:
		return "品牌广告"
	case AssetTypeDigitalHumanAd:
		return "数字人效果广告"
	case AssetTypePrerollAd:
		return "广告前贴"
	case AssetTypeHitReplicaAd:
		return "爆款复刻"
	}
	return "待识别"
}

// IsVideo 说明这类素材的本体是不是一段视频。
//
// 分这一刀是因为「怎么把素材喂给模型」这件事上，六种类型只有两类：图文的本体
// 就是文字，人复制一段正文过来就是原件；视频的本体是画面和声音，人写的那段
// 「画面描述」只是转述，模型看的是转述而不是素材。视频类因此要走多模态
// （understanding.go），图文类不需要。
func (t AssetType) IsVideo() bool {
	switch t {
	case AssetTypeBrandAd, AssetTypeDigitalHumanAd, AssetTypePrerollAd, AssetTypeHitReplicaAd:
		return true
	}
	return false
}

// AllAssetTypes lists the six identifiable types in PRD order.
func AllAssetTypes() []AssetType {
	return []AssetType{
		AssetTypeXiaohongshuNote,
		AssetTypeWechatArticle,
		AssetTypeBrandAd,
		AssetTypeDigitalHumanAd,
		AssetTypePrerollAd,
		AssetTypeHitReplicaAd,
	}
}

// FeatureValueKind constrains how a feature value is stored and compared.
// Comparability is the point: 内容分析 exists to turn creative content into
// variables that can be put side by side (20 §4.1).
type FeatureValueKind string

const (
	FeatureKindText     FeatureValueKind = "text"       // free text, not comparable across assets
	FeatureKindTags     FeatureValueKind = "tags"       // open-ended multi value (keywords, topics)
	FeatureKindEnum     FeatureValueKind = "enum"       // single choice from the controlled vocabulary
	FeatureKindEnumMul  FeatureValueKind = "enum_multi" // multiple choices from the controlled vocabulary
	FeatureKindNumber   FeatureValueKind = "number"
	FeatureKindBool     FeatureValueKind = "bool"
	FeatureKindDuration FeatureValueKind = "duration_seconds"
)

// FeatureField describes one comparable variable of one asset type.
//
// Vocabulary holds the controlled term list. An enum field with an empty
// Vocabulary is deliberate, not an oversight: the PRD (§5 末) assigns vocabulary
// maintenance to an administrator via 能力运营 → 特征体系, so the field ships with
// the term list open and the governance surface fills it. Extraction may still
// write a value; ValidateFeatureValue only rejects off-vocabulary terms once a
// vocabulary exists.
type FeatureField struct {
	Key        string           `json:"key"`
	Label      string           `json:"label"`
	Group      string           `json:"group"`
	Kind       FeatureValueKind `json:"kind"`
	Vocabulary []string         `json:"vocabulary,omitempty"`
	Unit       string           `json:"unit,omitempty"`
	Note       string           `json:"note,omitempty"`
}

// FeatureSchema is the complete, type-specific feature system for one asset type.
type FeatureSchema struct {
	AssetType AssetType      `json:"asset_type"`
	Label     string         `json:"label"`
	Source    string         `json:"source"`
	Fields    []FeatureField `json:"fields"`
	// IsVideo 让前端知道这类素材的提取要不要人填正文：视频类由多模态去看画面
	// （understanding.go），图文类的本体就是那段字，只能人贴进来。
	//
	// 从后端带过去而不是让前端自己列一份类型清单：这不是文案，是「输入框必不必填」
	// 的开关，两边各存一份的话，将来加第七类素材时前端那份不会有人想起来改。
	// 它由 AssetType.IsVideo() 派生，不单独存——见 FeatureSchemaFor。
	IsVideo bool `json:"is_video"`
}

// Groups returns the field groups in declaration order, which follows the PRD's
// own sub-headings so the UI can render a section per group.
func (s FeatureSchema) Groups() []string {
	seen := make(map[string]struct{}, len(s.Fields))
	groups := make([]string, 0, 8)
	for _, field := range s.Fields {
		if _, ok := seen[field.Group]; ok {
			continue
		}
		seen[field.Group] = struct{}{}
		groups = append(groups, field.Group)
	}
	return groups
}

// Field looks up a field by key.
func (s FeatureSchema) Field(key string) (FeatureField, bool) {
	for _, field := range s.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return FeatureField{}, false
}

// 03 §5.1 小红书图文
var xiaohongshuNoteFields = []FeatureField{
	// 标题：利益、问题、数字、反差、场景、身份、情绪、关键词。
	// The seven angles are modelled as one controlled vocabulary rather than seven
	// booleans because the analytical question is "数字型标题 vs 反差型标题 哪个好",
	// which needs them on one axis. 关键词 is separated out: it is not an angle.
	{Key: "title_angles", Label: "标题角度", Group: "标题", Kind: FeatureKindEnumMul,
		Vocabulary: []string{"利益", "问题", "数字", "反差", "场景", "身份", "情绪"}},
	{Key: "title_keywords", Label: "标题关键词", Group: "标题", Kind: FeatureKindTags},

	// 正文：开场方式、内容结构、产品露出位置、体验/证明、CTA。
	{Key: "opening_style", Label: "开场方式", Group: "正文", Kind: FeatureKindEnum},
	{Key: "body_structure", Label: "内容结构", Group: "正文", Kind: FeatureKindEnum},
	{Key: "product_placement", Label: "产品露出位置", Group: "正文", Kind: FeatureKindEnum},
	{Key: "proof_type", Label: "体验与证明", Group: "正文", Kind: FeatureKindEnumMul},
	{Key: "cta", Label: "CTA", Group: "正文", Kind: FeatureKindEnum},

	// 图组：封面构图、主体、文字密度、图片数量、叙事顺序、产品出现频率。
	{Key: "cover_composition", Label: "封面构图", Group: "图组", Kind: FeatureKindEnum},
	{Key: "cover_subject", Label: "封面主体", Group: "图组", Kind: FeatureKindText},
	{Key: "text_density", Label: "文字密度", Group: "图组", Kind: FeatureKindEnum},
	{Key: "image_count", Label: "图片数量", Group: "图组", Kind: FeatureKindNumber, Unit: "张"},
	{Key: "narrative_order", Label: "叙事顺序", Group: "图组", Kind: FeatureKindEnum},
	{Key: "product_frequency", Label: "产品出现频率", Group: "图组", Kind: FeatureKindNumber, Unit: "次"},

	// 表达：生活化程度、品牌露出、商业感、语气、话题与搜索词。
	{Key: "lifestyle_degree", Label: "生活化程度", Group: "表达", Kind: FeatureKindEnum},
	{Key: "brand_exposure", Label: "品牌露出", Group: "表达", Kind: FeatureKindEnum},
	{Key: "commercial_tone", Label: "商业感", Group: "表达", Kind: FeatureKindEnum},
	{Key: "tone", Label: "语气", Group: "表达", Kind: FeatureKindEnum},
	{Key: "topics_and_search_terms", Label: "话题与搜索词", Group: "表达", Kind: FeatureKindTags},
}

// 03 §5.2 公众号图文
var wechatArticleFields = []FeatureField{
	// 标题与摘要：主题、承诺、信息密度和一致性。
	{Key: "headline_theme", Label: "主题", Group: "标题与摘要", Kind: FeatureKindEnum},
	{Key: "headline_promise", Label: "承诺", Group: "标题与摘要", Kind: FeatureKindEnum},
	{Key: "info_density", Label: "信息密度", Group: "标题与摘要", Kind: FeatureKindEnum},
	{Key: "headline_consistency", Label: "标题与正文一致性", Group: "标题与摘要", Kind: FeatureKindEnum},

	// 文章结构：章节、篇幅、叙事/知识/案例类型、产品出现位置。
	{Key: "section_count", Label: "章节数", Group: "文章结构", Kind: FeatureKindNumber, Unit: "节"},
	{Key: "word_count", Label: "篇幅", Group: "文章结构", Kind: FeatureKindNumber, Unit: "字"},
	{Key: "article_type", Label: "文章类型", Group: "文章结构", Kind: FeatureKindEnum,
		Vocabulary: []string{"叙事", "知识", "案例"}},
	{Key: "product_position", Label: "产品出现位置", Group: "文章结构", Kind: FeatureKindEnum},

	// 视觉：头图、内文图、引用和重点样式。
	{Key: "hero_image", Label: "头图", Group: "视觉", Kind: FeatureKindEnum},
	{Key: "inline_image_count", Label: "内文图数量", Group: "视觉", Kind: FeatureKindNumber, Unit: "张"},
	{Key: "quote_style", Label: "引用样式", Group: "视觉", Kind: FeatureKindEnum},
	{Key: "emphasis_style", Label: "重点样式", Group: "视觉", Kind: FeatureKindEnum},

	// 承接：CTA 类型、位置、链接、关注/私域/转化路径。
	{Key: "cta_type", Label: "CTA 类型", Group: "承接", Kind: FeatureKindEnum},
	{Key: "cta_position", Label: "CTA 位置", Group: "承接", Kind: FeatureKindEnum},
	{Key: "cta_link", Label: "CTA 链接", Group: "承接", Kind: FeatureKindText},
	{Key: "conversion_path", Label: "承接路径", Group: "承接", Kind: FeatureKindEnum,
		Vocabulary: []string{"关注", "私域", "转化"}},
}

// 03 §5.3 品牌广告
var brandAdFields = []FeatureField{
	// 叙事：主题、冲突、情绪曲线、角色、场景和品牌角色。
	{Key: "theme", Label: "主题", Group: "叙事", Kind: FeatureKindText},
	{Key: "conflict", Label: "冲突", Group: "叙事", Kind: FeatureKindText},
	{Key: "emotion_curve", Label: "情绪曲线", Group: "叙事", Kind: FeatureKindEnum},
	{Key: "characters", Label: "角色", Group: "叙事", Kind: FeatureKindTags},
	{Key: "scenes", Label: "场景", Group: "叙事", Kind: FeatureKindTags},
	{Key: "brand_role", Label: "品牌角色", Group: "叙事", Kind: FeatureKindEnum},

	// 品牌资产：Logo、品牌色、口号、声音、产品露出和记忆符号。
	{Key: "logo_presence", Label: "Logo 出现", Group: "品牌资产", Kind: FeatureKindBool},
	{Key: "brand_color", Label: "品牌色", Group: "品牌资产", Kind: FeatureKindTags},
	{Key: "slogan", Label: "口号", Group: "品牌资产", Kind: FeatureKindText},
	{Key: "brand_sound", Label: "品牌声音", Group: "品牌资产", Kind: FeatureKindText},
	{Key: "product_exposure", Label: "产品露出", Group: "品牌资产", Kind: FeatureKindEnum},
	{Key: "memory_symbol", Label: "记忆符号", Group: "品牌资产", Kind: FeatureKindTags},

	// 电影感：剧本结构、表演、镜头数、景别、机位、运镜、光线、色彩、声音、节奏和转场。
	{Key: "script_structure", Label: "剧本结构", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "performance", Label: "表演", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "shot_count", Label: "镜头数", Group: "电影感", Kind: FeatureKindNumber, Unit: "个"},
	{Key: "shot_size", Label: "景别", Group: "电影感", Kind: FeatureKindEnumMul},
	{Key: "camera_position", Label: "机位", Group: "电影感", Kind: FeatureKindEnumMul},
	{Key: "camera_movement", Label: "运镜", Group: "电影感", Kind: FeatureKindEnumMul},
	{Key: "lighting", Label: "光线", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "color", Label: "色彩", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "sound", Label: "声音", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "rhythm", Label: "节奏", Group: "电影感", Kind: FeatureKindEnum},
	{Key: "transition", Label: "转场", Group: "电影感", Kind: FeatureKindEnumMul},

	// 一致性：角色、产品、服装、道具、场景、光线和美术风格的跨镜头连续性。
	// This group is what separates a brand film from a pile of clips, so each
	// dimension is scored on its own instead of collapsing into one "一致性" number.
	{Key: "consistency_character", Label: "角色一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_product", Label: "产品一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_costume", Label: "服装一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_prop", Label: "道具一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_scene", Label: "场景一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_lighting", Label: "光线一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},
	{Key: "consistency_art_style", Label: "美术风格一致性", Group: "跨镜头一致性", Kind: FeatureKindEnum},

	// 渠道适配：开场、时长、画幅、封面和发布文案。
	{Key: "channel_opening", Label: "开场适配", Group: "渠道适配", Kind: FeatureKindEnum},
	{Key: "duration", Label: "时长", Group: "渠道适配", Kind: FeatureKindDuration, Unit: "秒"},
	{Key: "aspect_ratio", Label: "画幅", Group: "渠道适配", Kind: FeatureKindEnum},
	{Key: "cover", Label: "封面", Group: "渠道适配", Kind: FeatureKindText},
	{Key: "publish_copy", Label: "发布文案", Group: "渠道适配", Kind: FeatureKindText},
}

// performanceAdBaseFields covers 03 §5.4 效果广告 钩子/转化结构/制作特征/实验变量.
// All three performance-ad types share it; each then adds its own pack.
var performanceAdBaseFields = []FeatureField{
	// 钩子：问题、利益、结果、反差、猎奇、身份、场景和表现形式。
	{Key: "hook_type", Label: "钩子类型", Group: "钩子", Kind: FeatureKindEnumMul,
		Vocabulary: []string{"问题", "利益", "结果", "反差", "猎奇", "身份", "场景"}},
	{Key: "hook_form", Label: "钩子表现形式", Group: "钩子", Kind: FeatureKindEnum},

	// 转化结构：痛点、卖点、机制、演示、证明、优惠、CTA。
	{Key: "pain_point", Label: "痛点", Group: "转化结构", Kind: FeatureKindText},
	{Key: "selling_points", Label: "卖点", Group: "转化结构", Kind: FeatureKindTags},
	{Key: "mechanism", Label: "机制", Group: "转化结构", Kind: FeatureKindText},
	{Key: "demonstration", Label: "演示", Group: "转化结构", Kind: FeatureKindEnum},
	{Key: "proof", Label: "证明", Group: "转化结构", Kind: FeatureKindEnumMul},
	{Key: "offer", Label: "优惠", Group: "转化结构", Kind: FeatureKindText},
	{Key: "cta", Label: "CTA", Group: "转化结构", Kind: FeatureKindEnum},

	// 制作特征：真人/数字人/商品、口播、字幕密度、镜头节奏和素材来源。
	{Key: "presenter", Label: "出镜形式", Group: "制作特征", Kind: FeatureKindEnum,
		Vocabulary: []string{"真人", "数字人", "商品"}},
	{Key: "voiceover", Label: "口播", Group: "制作特征", Kind: FeatureKindBool},
	{Key: "subtitle_density", Label: "字幕密度", Group: "制作特征", Kind: FeatureKindEnum},
	{Key: "shot_rhythm", Label: "镜头节奏", Group: "制作特征", Kind: FeatureKindEnum},
	{Key: "material_source", Label: "素材来源", Group: "制作特征", Kind: FeatureKindEnum},

	// 实验变量：受众、钩子、卖点、证明、CTA、人物、场景、时长。
	// Modelled as one multi-select "本素材相对对照组改变了哪些变量" because AM-009
	// requires knowing whether exactly one main variable moved before any variable
	// attribution is allowed; eight separate fields cannot answer that in one read.
	{Key: "experiment_variables", Label: "改变的实验变量", Group: "实验变量", Kind: FeatureKindEnumMul,
		Vocabulary: []string{"受众", "钩子", "卖点", "证明", "CTA", "人物", "场景", "时长"},
		Note:       "相对对照组改变了哪些变量；仅当只改变一个主变量且样本达标时才做变量归因（AM-009）"},
}

// 03 §5.4 数字人特征
var digitalHumanFields = []FeatureField{
	{Key: "avatar", Label: "数字人", Group: "数字人特征", Kind: FeatureKindText},
	{Key: "voice", Label: "声音", Group: "数字人特征", Kind: FeatureKindText},
	{Key: "lip_sync", Label: "口型", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "expression", Label: "表情", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "gesture", Label: "手势", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "speech_rate", Label: "语速", Group: "数字人特征", Kind: FeatureKindNumber, Unit: "字/分"},
	{Key: "pause", Label: "停顿", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "background", Label: "背景", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "product_layer", Label: "商品层", Group: "数字人特征", Kind: FeatureKindEnum},
	{Key: "ai_disclosure", Label: "AI 标识", Group: "数字人特征", Kind: FeatureKindBool,
		Note: "合规项：数字人素材是否带 AI 生成标识"},
}

// 03 §5.4 广告前贴特征
var prerollFields = []FeatureField{
	{Key: "target_duration", Label: "目标时长", Group: "前贴特征", Kind: FeatureKindDuration, Unit: "秒"},
	{Key: "first_frame", Label: "首帧", Group: "前贴特征", Kind: FeatureKindText},
	{Key: "opening_structure", Label: "开场结构", Group: "前贴特征", Kind: FeatureKindEnum},
	{Key: "info_per_second", Label: "单位时间信息量", Group: "前贴特征", Kind: FeatureKindNumber, Unit: "信息点/秒"},
	{Key: "mute_comprehensible", Label: "静音可理解性", Group: "前贴特征", Kind: FeatureKindEnum,
		Note: "前贴常在静音状态下播放，无字幕或无画面叙事的素材在此失分"},
	{Key: "bumper_time", Label: "贴片出现时间", Group: "前贴特征", Kind: FeatureKindDuration, Unit: "秒"},
	{Key: "cta_time", Label: "CTA 出现时间", Group: "前贴特征", Kind: FeatureKindDuration, Unit: "秒"},
}

// 03 §5.4 爆款复刻特征
var hitReplicaFields = []FeatureField{
	{Key: "reference_source", Label: "参考来源", Group: "复刻特征", Kind: FeatureKindText},
	{Key: "structure_mapping", Label: "结构映射", Group: "复刻特征", Kind: FeatureKindText},
	{Key: "rhythm_similarity", Label: "节奏相似度", Group: "复刻特征", Kind: FeatureKindNumber},
	{Key: "rewrite_degree", Label: "原创改写程度", Group: "复刻特征", Kind: FeatureKindNumber},
	{Key: "own_material_ratio", Label: "自有素材占比", Group: "复刻特征", Kind: FeatureKindNumber},
	{Key: "copyright_risk", Label: "版权风险", Group: "复刻特征", Kind: FeatureKindEnum,
		Vocabulary: []string{"低", "中", "高"},
		Note:       "合规项：复刻程度过高时应在此显式登记风险"},
}

// channelFitMeasuredFields 是效果广告的渠道适配：时长和画幅。
//
// 这两项 PRD 只在 §5.3 品牌广告下列过（「渠道适配：开场、时长、画幅、封面和发布
// 文案」），§5.4 效果广告没有对应的组——那里的「时长」只作为实验变量的一个可选项
// 出现，说的是「这次改没改时长」，不是「这条素材多长」。
//
// 补到效果广告上，是因为这两项是**能从文件本身量出来的**，而效果广告恰恰是最需要
// 它们的一类：15 秒和 45 秒的前贴不是同一个东西，竖版和横版的数字人也不是。缺了
// 这两项，同类素材对比就只能拿模型猜的口播、节奏去比，而那些进不了归因。
//
// 键名、类型、单位与 brandAdFields 里的同名字段完全一致，跨类型比较才对得上。
// 词表照样留空——枚举词表归 能力运营 → 特征体系 管（§5 末），不在这里写死。
var channelFitMeasuredFields = []FeatureField{
	{Key: "duration", Label: "时长", Group: "渠道适配", Kind: FeatureKindDuration, Unit: "秒"},
	{Key: "aspect_ratio", Label: "画幅", Group: "渠道适配", Kind: FeatureKindEnum},
}

func performanceAdSchema(assetType AssetType, extra []FeatureField) FeatureSchema {
	fields := make([]FeatureField, 0, len(performanceAdBaseFields)+len(channelFitMeasuredFields)+len(extra))
	fields = append(fields, performanceAdBaseFields...)
	fields = append(fields, channelFitMeasuredFields...)
	fields = append(fields, extra...)
	return FeatureSchema{AssetType: assetType, Label: assetType.Label(), Source: "03 §5.4", Fields: fields}
}

var featureSchemas = map[AssetType]FeatureSchema{
	AssetTypeXiaohongshuNote: {
		AssetType: AssetTypeXiaohongshuNote, Label: AssetTypeXiaohongshuNote.Label(),
		Source: "03 §5.1", Fields: xiaohongshuNoteFields,
	},
	AssetTypeWechatArticle: {
		AssetType: AssetTypeWechatArticle, Label: AssetTypeWechatArticle.Label(),
		Source: "03 §5.2", Fields: wechatArticleFields,
	},
	AssetTypeBrandAd: {
		AssetType: AssetTypeBrandAd, Label: AssetTypeBrandAd.Label(),
		Source: "03 §5.3", Fields: brandAdFields,
	},
	AssetTypeDigitalHumanAd: performanceAdSchema(AssetTypeDigitalHumanAd, digitalHumanFields),
	AssetTypePrerollAd:      performanceAdSchema(AssetTypePrerollAd, prerollFields),
	AssetTypeHitReplicaAd:   performanceAdSchema(AssetTypeHitReplicaAd, hitReplicaFields),
}

// FeatureSchemaFor returns the feature system of one asset type.
//
// IsVideo 在这里补上，不在上面那张表里写死：写死的话六个条目里漏标一个，
// 那类素材就会莫名其妙要求人填正文，而且不会有任何报错。
func FeatureSchemaFor(assetType AssetType) (FeatureSchema, bool) {
	schema, ok := featureSchemas[assetType]
	schema.IsVideo = assetType.IsVideo()
	return schema, ok
}

// AllFeatureSchemas returns every schema in PRD order, for the 能力运营 →
// 特征体系 governance surface and for the frontend to render type-specific forms.
func AllFeatureSchemas() []FeatureSchema {
	schemas := make([]FeatureSchema, 0, len(featureSchemas))
	for _, assetType := range AllAssetTypes() {
		schema, _ := FeatureSchemaFor(assetType)
		schemas = append(schemas, schema)
	}
	return schemas
}

// ValidateFeatureValue enforces the rule that makes 内容分析 trustworthy:
// a feature key only exists for the asset types whose schema declares it.
//
// This is the code-level guard for the MVP acceptance criterion in 03 §15②
// (「不把视频钩子字段套到公众号文章」). Writing hook_type onto a 公众号图文 asset
// fails here rather than quietly producing a comparison that means nothing.
func ValidateFeatureValue(assetType AssetType, key string, terms []string) error {
	schema, ok := FeatureSchemaFor(assetType)
	if !ok {
		return fmt.Errorf("%w: 素材类型 %q 没有特征体系，请先完成类型识别", ErrInvalidRequest, string(assetType))
	}
	field, ok := schema.Field(key)
	if !ok {
		return fmt.Errorf("%w: 特征 %q 不属于%s的特征体系，不同类型的特征不可混用", ErrInvalidRequest, key, schema.Label)
	}
	if field.Kind != FeatureKindEnum && field.Kind != FeatureKindEnumMul {
		return nil
	}
	if field.Kind == FeatureKindEnum && len(terms) > 1 {
		return fmt.Errorf("%w: 特征 %q 是单选，收到 %d 个取值", ErrInvalidRequest, key, len(terms))
	}
	// An empty vocabulary means the administrator has not published the term list
	// yet (03 §5 末). Accept the value rather than block extraction on governance.
	if len(field.Vocabulary) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(field.Vocabulary))
	for _, term := range field.Vocabulary {
		allowed[term] = struct{}{}
	}
	for _, term := range terms {
		if _, ok := allowed[strings.TrimSpace(term)]; !ok {
			return fmt.Errorf("%w: 特征 %q 的取值 %q 不在受控词表内（允许：%s）",
				ErrInvalidRequest, key, term, strings.Join(field.Vocabulary, "、"))
		}
	}
	return nil
}

// FeatureCoverage reports how much of an asset type's feature system is filled,
// which is what the 待提取 queue and the 特征矩阵 completeness column need.
func FeatureCoverage(assetType AssetType, filledKeys []string) (filled int, total int) {
	schema, ok := FeatureSchemaFor(assetType)
	if !ok {
		return 0, 0
	}
	present := make(map[string]struct{}, len(filledKeys))
	for _, key := range filledKeys {
		present[key] = struct{}{}
	}
	for _, field := range schema.Fields {
		if _, ok := present[field.Key]; ok {
			filled++
		}
	}
	return filled, len(schema.Fields)
}

// SharedFeatureKeys returns the feature keys common to every given asset type,
// sorted. A 特征矩阵 that mixes types may only pivot on these; anything else
// would put unrelated variables in the same column.
func SharedFeatureKeys(assetTypes ...AssetType) []string {
	if len(assetTypes) == 0 {
		return nil
	}
	counts := make(map[string]int)
	distinct := make(map[AssetType]struct{}, len(assetTypes))
	for _, assetType := range assetTypes {
		if _, seen := distinct[assetType]; seen {
			continue
		}
		distinct[assetType] = struct{}{}
		schema, ok := FeatureSchemaFor(assetType)
		if !ok {
			return nil
		}
		for _, field := range schema.Fields {
			counts[field.Key]++
		}
	}
	shared := make([]string, 0, 16)
	for key, count := range counts {
		if count == len(distinct) {
			shared = append(shared, key)
		}
	}
	sort.Strings(shared)
	return shared
}
