package insights

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 洞察卡（03 §8.1 / 功能基线 §3.1）。九个字段缺一不可，其中「类型」是这个模块
// 区别于普通报表工具的地方：事实、统计观察、假设、建议是四种不同强度的东西，
// 混在一起就会把相关性读成因果（03 §2 目标⑥）。

type InsightCardType string

const (
	// CardFact 事实：数据里直接读得出来的，不含推断。
	CardFact InsightCardType = "fact"
	// CardStatistic 统计观察：算出来的相关性，不等于因果。
	CardStatistic InsightCardType = "statistic"
	// CardHypothesis 假设：还没验证的想法，用来指导下一轮测试。
	CardHypothesis InsightCardType = "hypothesis"
	// CardRecommendation 建议：让人去做某件事。
	CardRecommendation InsightCardType = "recommendation"
)

func (t InsightCardType) valid() bool {
	switch t {
	case CardFact, CardStatistic, CardHypothesis, CardRecommendation:
		return true
	}
	return false
}

func (t InsightCardType) Label() string {
	switch t {
	case CardFact:
		return "事实"
	case CardStatistic:
		return "统计观察"
	case CardHypothesis:
		return "假设"
	case CardRecommendation:
		return "建议"
	}
	return string(t)
}

// EvidenceGrade 报告这张卡能不能当证据用。只有事实和统计观察可以——
// 假设和建议是从证据推出来的东西，拿它们当证据就成了循环论证。
func (t InsightCardType) EvidenceGrade() bool { return t == CardFact || t == CardStatistic }

// 置信提示复用 connectors.go 里已有的 ConfidenceLevel（充分 / 方向性 /
// 样本不足 / 存在混杂）。指标总览和洞察卡说的是同一件事，两套词表会让
// 「方向性」在两个页面上含义不同。

func (c ConfidenceLevel) valid() bool {
	switch c {
	case ConfidenceSufficient, ConfidenceDirectional, ConfidenceLowSample, ConfidenceConfounded:
		return true
	}
	return false
}

// Weak 标出需要警告的两种置信：样本不足和存在混杂。这两种情况下结论可能是
// 随机波动或别的变量造成的，摆到决策桌上必须带着这个提示（03 §10.3）。
// Hint 是「置信提示」字段的内容（03 §8.1 第六项）。它不是 Label() 的同义词：
// 标签只说这条是哪一档，提示要说这一档意味着能拿它做什么、不能做什么——
// 在卡上重复一遍「方向性」等于没提示。
func (c ConfidenceLevel) Hint() string {
	switch c {
	case ConfidenceSufficient:
		return "样本量和口径都够，可以据此做决定。"
	case ConfidenceDirectional:
		return "方向大致可信，具体幅度不可信。可以用来选方向，不要用来定指标。"
	case ConfidenceLowSample:
		return "样本太少，看到的差异很可能是随机波动。适合拿去做实验，不适合直接照做。"
	case ConfidenceConfounded:
		return "这段时间同时变了别的东西，效果分不清是谁的。要分清得先做对照。"
	}
	return ""
}

func (c ConfidenceLevel) Weak() bool {
	return c == ConfidenceLowSample || c == ConfidenceConfounded
}

// Applicability 是适用范围的结构化部分。自由文本的适用条件仍然存在 Conditions 里——
// 这里存的是能用来筛选的维度，那里存的是给人读的说明，两者不重复。
type Applicability struct {
	Brands        []string `json:"brands,omitempty"`
	Products      []string `json:"products,omitempty"`
	Channels      []string `json:"channels,omitempty"`
	CreativeTypes []string `json:"creative_types,omitempty"`
	Objectives    []string `json:"objectives,omitempty"`
	Audiences     []string `json:"audiences,omitempty"`
	TimeRangeNote string   `json:"time_range_note,omitempty"`
}

// Summary 把适用条件摆成一行文本。
//
// 同一格的多个取值用「/」连，格与格之间用「·」隔。都用同一个分隔符的话，
// 「抖音 · 小红书 · 效果广告」读起来像三个并列的限制，实际上是两格
// （渠道两个值 + 广告类型一个值），读的人会以为这条经验只在同时满足
// 三件事时才成立。
//
// 这段规则和前端 ExperienceCard.tsx 的 formatScope 必须一模一样：
// 界面上看到的适用范围，和引用时抄出去的那句话，得是同一句。
func (a Applicability) Summary() string {
	groups := [][]string{a.Brands, a.Products, a.Channels, a.CreativeTypes, a.Objectives, a.Audiences}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		parts = append(parts, strings.Join(group, "/"))
	}
	if len(parts) == 0 {
		return "不限"
	}
	return strings.Join(parts, " · ")
}

func (a Applicability) empty() bool {
	return len(a.Brands) == 0 && len(a.Products) == 0 && len(a.Channels) == 0 &&
		len(a.CreativeTypes) == 0 && len(a.Objectives) == 0 && len(a.Audiences) == 0 &&
		strings.TrimSpace(a.TimeRangeNote) == ""
}

// DataBasis 是「凭什么这么说」的数量那一半。
type DataBasis struct {
	AssetCount  int        `json:"asset_count,omitempty"`
	SampleSize  int64      `json:"sample_size,omitempty"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
	Metrics     []string   `json:"metrics,omitempty"`
	Baseline    string     `json:"baseline,omitempty"`
}

func (d DataBasis) empty() bool {
	return d.AssetCount == 0 && d.SampleSize == 0 && d.WindowStart == nil &&
		d.WindowEnd == nil && len(d.Metrics) == 0 && strings.TrimSpace(d.Baseline) == ""
}

// ContentBasis 是「凭什么这么说」的内容那一半：具体是哪些素材特征，
// 以及可以去看的样例版本。没有它，结论就没法落回到创意上。
type ContentBasis struct {
	Features             []string `json:"features,omitempty"`
	ExampleAssetVersions []string `json:"example_asset_versions,omitempty"`
	Note                 string   `json:"note,omitempty"`
}

func (c ContentBasis) empty() bool {
	return len(c.Features) == 0 && len(c.ExampleAssetVersions) == 0 && strings.TrimSpace(c.Note) == ""
}

// validateCard 是九字段的准入检查。它拦的是几种自相矛盾的卡：
//   - 「建议」没有建议动作 —— 一句空话
//   - 「事实」「统计观察」没有数据依据 —— 那其实是假设
//   - 「假设」声称置信充分 —— 假设还没验证，不可能充分
func validateCard(cardType InsightCardType, confidence ConfidenceLevel, action string, basis DataBasis) error {
	if !cardType.valid() {
		return fmt.Errorf("%w: 洞察卡类型只能是事实、统计观察、假设或建议", ErrInvalidRequest)
	}
	if !confidence.valid() {
		return fmt.Errorf("%w: 置信提示只能是充分、方向性、样本不足或存在混杂", ErrInvalidRequest)
	}
	if len(action) > 1000 {
		return fmt.Errorf("%w: 建议动作过长", ErrInvalidRequest)
	}
	if cardType == CardRecommendation && strings.TrimSpace(action) == "" {
		return fmt.Errorf("%w: 「建议」类洞察卡必须写清建议动作", ErrInvalidRequest)
	}
	if cardType.EvidenceGrade() && basis.empty() {
		return fmt.Errorf("%w: 「%s」必须给出数据依据，否则它只是假设", ErrInvalidRequest, cardType.Label())
	}
	if cardType == CardHypothesis && confidence == ConfidenceSufficient {
		return fmt.Errorf("%w: 假设还没验证，置信不可能是「充分」", ErrInvalidRequest)
	}
	return nil
}

// validateApplicability / validateContentBasis 只拦长度和条数。适用范围里写什么词
// 由受控词表管，不在这里硬编码——投前洞察的筛选是按值精确匹配的，写死一份枚举
// 只会让新渠道进不来。
func validateApplicability(value Applicability) error {
	lists := [][]string{value.Brands, value.Products, value.Channels,
		value.CreativeTypes, value.Objectives, value.Audiences}
	for _, list := range lists {
		if err := validateTagList(list, 20, 96, "适用范围"); err != nil {
			return err
		}
	}
	if len(value.TimeRangeNote) > 200 {
		return fmt.Errorf("%w: 适用时间说明过长", ErrInvalidRequest)
	}
	return nil
}

func validateContentBasis(value ContentBasis) error {
	if err := validateTagList(value.Features, 30, 96, "内容依据特征"); err != nil {
		return err
	}
	if err := validateTagList(value.ExampleAssetVersions, 20, 96, "示例素材版本"); err != nil {
		return err
	}
	if len(value.Note) > 1000 {
		return fmt.Errorf("%w: 内容依据说明过长", ErrInvalidRequest)
	}
	return nil
}

func validateTagList(values []string, maxCount, maxLength int, label string) error {
	if len(values) > maxCount {
		return fmt.Errorf("%w: %s 条目过多", ErrInvalidRequest, label)
	}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed == "" || len(trimmed) > maxLength {
			return fmt.Errorf("%w: %s 条目为空或过长", ErrInvalidRequest, label)
		}
	}
	return nil
}

// --- 投前洞察 ---

// PreLaunchFilter 落 22 §6.4①：按当前 Project 的目标、渠道、创意类型筛选。
type PreLaunchFilter struct {
	Channel      string
	CreativeType string
	Objective    string
	Query        string
	// CrossChannel 对应 03 §10.3②：跨渠道比较默认关闭，用户显式打开后才合并展示。
	CrossChannel bool
}

// InsightCard 是经验在投前场景下的呈现形态。它不是另一张表——同一条经验，
// 在经验库里看的是它的生命周期，在投前洞察里看的是它能不能用在这次投放上。
type InsightCard struct {
	ExperienceID   string           `json:"experience_id"`
	LineageID      string           `json:"lineage_id"`
	Revision       int              `json:"revision"`
	Conclusion     string           `json:"conclusion"`
	Type           InsightCardType  `json:"type"`
	TypeLabel      string           `json:"type_label"`
	Applicability  Applicability    `json:"applicability"`
	Conditions     []string         `json:"conditions"`
	DataBasis      DataBasis        `json:"data_basis"`
	ContentBasis   ContentBasis     `json:"content_basis"`
	Confidence     ConfidenceLevel  `json:"confidence"`
	ConfidenceHint string           `json:"confidence_hint"`
	Counterexample []string         `json:"counterexamples"`
	Action         string           `json:"recommended_action"`
	Status         ExperienceStatus `json:"status"`

	// ThresholdVersion 跟着经验一起投影过来。**缺失表示不知道**（人自己填的档位、
	// 老数据），此时界面上不得出现任何阈值标注——这一页是拿经验去指导下一轮投放的
	// 地方，替一条来历不明的结论盖上「按第 N 版判定」，比不盖更糟。
	ThresholdVersion *int64 `json:"threshold_version,omitempty"`

	// MissingFields 把缺的字段直接摊开。§3.1 说「缺字段即不合格」，
	// 但把不合格的卡藏起来更糟——人会以为经验库里就这么点东西。
	// 所以照样返回，只是标出来哪里不完整。
	MissingFields  []string  `json:"missing_fields"`
	ReferenceCount int       `json:"reference_count"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// FeaturePattern 是「历史模式」视图的数据：同一个素材特征在多少张卡里出现过。
// 它不做排行——只说「这个特征反复出现在被确认的结论里」，把是否采纳留给人。
type FeaturePattern struct {
	// Feature 是分桶用的键。落在特征体系里的是那个字段的 Key（比如 hook_type），
	// 没落上的是原样的文本。**不要拿它显示给人看**，用 Label。
	Feature string `json:"feature"`

	// Label 是人话名。落在特征体系里的取字段的中文名（hook_type → 钩子类型），
	// 没落上的原样显示——原来这里把 hook_type 直接印在屏幕上，读的人得会认键名。
	Label string `json:"label"`

	// Governed 说明这个名字有没有落在特征体系里（03 §5 末：受控词表由管理员维护，
	// 避免同义词碎片化）。false 的那些不算「反复」：名字没进表，就没人担保
	// 「首图卖点数」和「首图卖点个数」已经合成了一个桶，这时候说它反复出现过 N 次
	// 是没有依据的。
	Governed bool `json:"governed"`

	CardCount      int             `json:"card_count"`
	Channels       []string        `json:"channels"`
	BestConfidence ConfidenceLevel `json:"best_confidence"`
	Conclusions    []string        `json:"conclusions"`

	// Repeated 说明它够不够得上「反复」（CardCount >= repeatedPatternMinCards
	// 且 Governed）。整屏的标题写着「哪些内容特征反复有效」、导语写着「比只被提过
	// 一次可信得多」，而以前每一条都是「1 条结论提到」——门槛不设，那两句话就是
	// 屏幕在自己骗自己。
	Repeated bool `json:"repeated"`
}

// repeatedPatternMinCards 是「反复」的门槛：至少两条互相独立的结论提到过。
//
// 它不进判定阈值那一套设置。那套管的是「样本够不够下这个结论」，是统计口径；
// 这里管的是「几条算反复」，是一个词的定义。混进去的话，设置页上会多出一格
// 语义完全不同的数字，而调它的人以为自己在调样本量。
//
// 取 2 而不是 3：这个模块目前的经验总量本来就少，取 3 会让整屏长期为空，而空屏
// 说不出「有 4 个特征各被提到过一次」这个真实情况——那恰恰是人该知道的。
const repeatedPatternMinCards = 2

type PreLaunchFacets struct {
	Channels      []string `json:"channels"`
	CreativeTypes []string `json:"creative_types"`
	Objectives    []string `json:"objectives"`
}

type PreLaunchInsight struct {
	ProjectID contract.ProjectID `json:"project_id"`
	Cards     []InsightCard      `json:"cards"`
	Patterns  []FeaturePattern   `json:"patterns"`
	Facets    PreLaunchFacets    `json:"facets"`

	// UnscopedExcluded 是被筛选条件挡掉的「没写适用范围」的卡数量。
	// 一条没写适用范围的经验没法证明它适用于本次投放，所以筛选时不返回；
	// 但静默丢掉会让人以为经验库是空的，所以把数量报出来。
	UnscopedExcluded int `json:"unscoped_excluded"`

	// MixedChannels 是当前结果里横跨的渠道。超过一个且没显式打开跨渠道时，
	// 前端不应把它们并排比较（03 §10.3②）。
	MixedChannels          []string `json:"mixed_channels"`
	CrossChannelComparison bool     `json:"cross_channel_comparison"`

	// 数据质量闸门（03 §10.3③）。数据有阻断问题时不生成强结论。
	QualityChecked           bool     `json:"quality_checked"`
	StrongConclusionsAllowed bool     `json:"strong_conclusions_allowed"`
	QualityBlockers          []string `json:"quality_blockers"`

	// ExperienceReferences 保留原字段名，仍然是已确认经验的原始列表，
	// 老的调用方（把它当经验列表用的）不会因为这次扩展而挂掉。
	ExperienceReferences []Experience `json:"experience_references"`

	Disclosure string `json:"disclosure"`
}

// preLaunchQualityWindowDays 是体检窗口的**出厂设定**。取值经由
// defaultThresholds() 进入 ResolvedThresholds.QualityWindowDays，
// 人在设置里调过之后以调过的为准。
const preLaunchQualityWindowDays = 30

// GetPreLaunch 把已确认经验投影成可以摆上决策桌的洞察卡。
// 只取已确认且未失效的（03 §8.2 / MVP⑩），并且带着数据质量闸门一起返回——
// 数据本身有阻断问题时，这一页给出的任何结论都不该被当成强结论。
func (s Service) GetPreLaunch(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter PreLaunchFilter) (PreLaunchInsight, error) {
	confirmed, err := s.ListExperiences(ctx, actor, projectID, ExperienceConfirmed, 200)
	if err != nil {
		return PreLaunchInsight{}, err
	}
	// 两道闸：状态在用**且**判定能归因，缺一不可。这一页是下游的默认引用集，
	// 「在用」只说明有人认可它值得记下来，「能归因」才说明这个因果排除过混杂。
	// 只过第一道的话，一条 👁 只是观察的经验会和 ✅ 能归因的并排摆在决策桌上，
	// 看的人分不出哪条能照着做。👁 的经验仍然存在、仍然能在「查」里点开
	// 「连只是观察的也看」找到、仍然能被人工点名引用——只是不进默认集。
	values := make([]Experience, 0, len(confirmed))
	for _, value := range confirmed {
		if value.Reusable() {
			values = append(values, value)
		}
	}
	references, err := s.ListProjectExperienceReferences(ctx, actor, projectID, 500)
	if err != nil {
		return PreLaunchInsight{}, err
	}
	referenceCounts := map[string]int{}
	for _, reference := range references {
		referenceCounts[reference.ExperienceID]++
	}

	insight := PreLaunchInsight{
		ProjectID:              projectID,
		ExperienceReferences:   values,
		CrossChannelComparison: filter.CrossChannel,
		Cards:                  []InsightCard{},
		Patterns:               []FeaturePattern{},
		Disclosure:             "仅引用状态在用、且判定为「✅ 能归因」的经验；是否适用于本次投放仍需人工判断。",
	}

	facetSets := [3]map[string]struct{}{{}, {}, {}}
	channelSet := map[string]struct{}{}
	filtered := make([]Experience, 0, len(values))
	for _, value := range values {
		addAll(facetSets[0], value.Applicability.Channels)
		addAll(facetSets[1], value.Applicability.CreativeTypes)
		addAll(facetSets[2], value.Applicability.Objectives)

		match, unscoped := matchesPreLaunchFilter(value, filter)
		if unscoped {
			insight.UnscopedExcluded++
			continue
		}
		if !match {
			continue
		}
		filtered = append(filtered, value)
		addAll(channelSet, value.Applicability.Channels)
		insight.Cards = append(insight.Cards, buildInsightCard(value, referenceCounts[value.ID]))
	}
	insight.Facets = PreLaunchFacets{
		Channels: sortedKeys(facetSets[0]), CreativeTypes: sortedKeys(facetSets[1]),
		Objectives: sortedKeys(facetSets[2]),
	}
	insight.MixedChannels = sortedKeys(channelSet)
	insight.Patterns = buildFeaturePatterns(filtered)

	// 强结论闸门。质量算不出来时如实说「没查」，而不是假装通过。
	if s.Connectors != nil {
		end := s.now()
		days := s.currentThresholds(ctx, actor.OrganizationID).QualityWindowDays
		window := MetricWindow{Start: end.AddDate(0, 0, -days), End: end}
		report, qualityErr := s.GetDataQuality(ctx, actor, projectID, window)
		if qualityErr == nil {
			insight.QualityChecked = true
			insight.StrongConclusionsAllowed = report.StrongConclusionsAllowed
			for _, issue := range report.Issues {
				if issue.Severity == SeverityBlocking && issue.State != QualityIgnored {
					insight.QualityBlockers = append(insight.QualityBlockers, issue.Title)
				}
			}
		}
	}
	return insight, nil
}

// matchesPreLaunchFilter 返回「是否命中」和「是否因为没写适用范围而被挡下」。
// 分成两个返回值是为了让调用方能把后者单独报出来——没写适用范围不是不匹配，
// 是这条经验根本没说自己适用于哪里。
func matchesPreLaunchFilter(value Experience, filter PreLaunchFilter) (match bool, unscoped bool) {
	scoped := filter.Channel != "" || filter.CreativeType != "" || filter.Objective != ""
	if scoped && value.Applicability.empty() {
		return false, true
	}
	if filter.Channel != "" && !containsFold(value.Applicability.Channels, filter.Channel) {
		return false, false
	}
	if filter.CreativeType != "" && !containsFold(value.Applicability.CreativeTypes, filter.CreativeType) {
		return false, false
	}
	if filter.Objective != "" && !containsFold(value.Applicability.Objectives, filter.Objective) {
		return false, false
	}
	if query := strings.TrimSpace(strings.ToLower(filter.Query)); query != "" {
		haystack := strings.ToLower(strings.Join(append([]string{
			value.Conclusion, value.RecommendedAction, value.ContentBasis.Note,
		}, append(append([]string{}, value.Conditions...), value.ContentBasis.Features...)...), " "))
		if !strings.Contains(haystack, query) {
			return false, false
		}
	}
	return true, false
}

func buildInsightCard(value Experience, referenceCount int) InsightCard {
	card := InsightCard{
		ExperienceID: value.ID, LineageID: value.LineageID, Revision: value.Revision,
		Conclusion: value.Conclusion, Type: value.CardType, TypeLabel: value.CardType.Label(),
		Applicability: value.Applicability, Conditions: value.Conditions,
		DataBasis: value.DataBasis, ContentBasis: value.ContentBasis,
		Confidence: value.Confidence, ConfidenceHint: value.Confidence.Hint(),
		Counterexample: value.Counterexamples, Action: value.RecommendedAction,
		Status: value.Status, ReferenceCount: referenceCount, UpdatedAt: value.UpdatedAt,
		ThresholdVersion: value.ThresholdVersion,
		MissingFields:    []string{},
	}
	if value.Applicability.empty() {
		card.MissingFields = append(card.MissingFields, "适用范围")
	}
	if value.DataBasis.empty() {
		card.MissingFields = append(card.MissingFields, "数据依据")
	}
	if value.ContentBasis.empty() {
		card.MissingFields = append(card.MissingFields, "内容依据")
	}
	if len(value.Counterexamples) == 0 {
		card.MissingFields = append(card.MissingFields, "风险与反例")
	}
	if strings.TrimSpace(value.RecommendedAction) == "" {
		card.MissingFields = append(card.MissingFields, "建议动作")
	}
	if card.Counterexample == nil {
		card.Counterexample = []string{}
	}
	if card.Conditions == nil {
		card.Conditions = []string{}
	}
	return card
}

// buildFeaturePatterns 把内容依据里的特征横过来看：同一个特征出现在几条已确认
// 结论里、涉及哪些渠道、其中最强的置信是什么。只统计出现次数，不排名次——
// 出现得多不代表效果好，可能只是被反复试过。
func buildFeaturePatterns(values []Experience) []FeaturePattern {
	type accumulator struct {
		label       string
		governed    bool
		count       int
		channels    map[string]struct{}
		best        ConfidenceLevel
		conclusions []string
	}
	order := []string{}
	byFeature := map[string]*accumulator{}
	for _, value := range values {
		for _, feature := range value.ContentBasis.Features {
			// 先过一遍特征体系：「hook_type」「钩子类型」「 Hook_Type 」写的是同一件事，
			// 不归一的话它们是三个各自「1 条」的桶，永远攒不到「反复」——而这正是
			// 03 §5 末要受控词表防的事。
			key, label, governed := canonicalFeature(feature)
			if key == "" {
				continue
			}
			entry, ok := byFeature[key]
			if !ok {
				entry = &accumulator{label: label, governed: governed, channels: map[string]struct{}{}}
				byFeature[key] = entry
				order = append(order, key)
			}
			entry.count++
			addAll(entry.channels, value.Applicability.Channels)
			if confidenceRank(value.Confidence) > confidenceRank(entry.best) {
				entry.best = value.Confidence
			}
			if len(entry.conclusions) < 5 {
				entry.conclusions = append(entry.conclusions, value.Conclusion)
			}
		}
	}
	patterns := make([]FeaturePattern, 0, len(order))
	for _, key := range order {
		entry := byFeature[key]
		patterns = append(patterns, FeaturePattern{
			Feature: key, Label: entry.label, Governed: entry.governed,
			CardCount: entry.count, Channels: sortedKeys(entry.channels),
			BestConfidence: entry.best, Conclusions: entry.conclusions,
			Repeated: entry.governed && entry.count >= repeatedPatternMinCards,
		})
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		// 够得上「反复」的一律排在前面，次数相同也是。这一屏问的是「有没有什么
		// 一直有效」，一个没入表的名字凑巧被提了两次，不该排在一个入了表、被两条
		// 独立结论提到的特征前面。
		if patterns[i].Repeated != patterns[j].Repeated {
			return patterns[i].Repeated
		}
		if patterns[i].CardCount != patterns[j].CardCount {
			return patterns[i].CardCount > patterns[j].CardCount
		}
		return patterns[i].Label < patterns[j].Label
	})
	return patterns
}

// canonicalFeature 把一个写法不定的特征名归到特征体系里的那一格。
//
// 返回分桶键、人话名、以及有没有归上。归不上的原样留着（键和名都是那段文本）
// ——丢掉的话，屏幕上会少掉一条真实存在的内容依据，而人不会知道少了什么；
// 留着但标成「没入表」，人才知道该去 能力运营 → 特征体系 把它收进去。
//
// 认两种写法：字段键（hook_type）和字段中文名（钩子类型）。中文名有歧义的不认
// ——「主题」在公众号图文里是 headline_theme、在品牌广告里是 theme，猜一个等于
// 把两类素材的结论混进同一个桶。
func canonicalFeature(feature string) (key string, label string, governed bool) {
	raw := strings.TrimSpace(feature)
	if raw == "" {
		return "", "", false
	}
	index := featureNameIndex()
	if entry, ok := index[strings.ToLower(raw)]; ok {
		return entry.key, entry.label, true
	}
	return raw, raw, false
}

type featureNameEntry struct {
	key   string
	label string
}

var featureNameIndexOnce struct {
	sync.Once
	value map[string]featureNameEntry
}

// featureNameIndex 是「怎么写都认得出来」的索引：键名和中文名都指向同一格。
//
// 只建一次。它完全由 features.go 里那几张静态表派生，跟着 AllAssetTypes 走——
// 手抄一份的话，将来加第七类素材，那类的特征会全部被判成「没入表」。
func featureNameIndex() map[string]featureNameEntry {
	featureNameIndexOnce.Do(func() {
		// 键名和中文名分两张表建，最后才合——合着建的话，某个字段的中文名万一
		// 撞上另一个字段的键名，撤歧义时会把那个键名一起撤掉，那格特征就此认不出来。
		byKey := map[string]featureNameEntry{}
		byLabel := map[string]featureNameEntry{}
		ambiguous := map[string]struct{}{}
		for _, schema := range AllFeatureSchemas() {
			for _, field := range schema.Fields {
				entry := featureNameEntry{key: field.Key, label: field.Label}
				byKey[strings.ToLower(field.Key)] = entry
				label := strings.ToLower(strings.TrimSpace(field.Label))
				if label == "" {
					continue
				}
				if existing, ok := byLabel[label]; ok {
					if existing.key != field.Key {
						ambiguous[label] = struct{}{}
					}
					continue
				}
				byLabel[label] = entry
			}
		}
		index := make(map[string]featureNameEntry, len(byKey)+len(byLabel))
		for label, entry := range byLabel {
			if _, bad := ambiguous[label]; bad {
				continue
			}
			index[label] = entry
		}
		// 键名后放：它没有歧义可言，撞上同名的中文名时以它为准。
		for key, entry := range byKey {
			index[key] = entry
		}
		featureNameIndexOnce.value = index
	})
	return featureNameIndexOnce.value
}

func confidenceRank(value ConfidenceLevel) int {
	switch value {
	case ConfidenceSufficient:
		return 4
	case ConfidenceDirectional:
		return 3
	case ConfidenceConfounded:
		return 2
	case ConfidenceLowSample:
		return 1
	}
	return 0
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func addAll(target map[string]struct{}, values []string) {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			target[trimmed] = struct{}{}
		}
	}
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
