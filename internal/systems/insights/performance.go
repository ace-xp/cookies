package insights

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 投后分析的五个解释性视图：素材对比、趋势、疲劳、异常、驱动因素
// （22 §6.2「MVP 只保留真实指标总览，随后实现素材对比、趋势、疲劳、异常和驱动因素」）。
//
// 五个视图共用一次事实读取，因为它们回答的是同一批数据的五个不同问题——分五次拉会
// 出现「趋势里看到的和疲劳里算的不是同一份数据」这种没法解释的情况。
//
// 全篇的纪律只有一条：**能不能归因，和差异有多大，是两件事**。差异大而变量混杂时，
// 这里只出方向性观察，不出结论（03 §7.3）。因此每个结构都带 Confidence 和一段说明
// 混杂来源的文字，前端不允许只显示数字。

// PerformanceAnalysis 是 GET /projects/{id}/performance-analysis 的返回。
type PerformanceAnalysis struct {
	Window  MetricWindow  `json:"window"`
	Caliber MetricCaliber `json:"caliber"`
	// Comparable=false 时所有对比类结论都只是方向性的：口径不一致的数据放在
	// 一起比，比出来的差异可能全部来自口径本身。
	Comparable       bool   `json:"comparable"`
	ComparableReason string `json:"comparable_reason,omitempty"`

	Comparisons []VariantComparison `json:"comparisons"`
	Trends      []AssetTrend        `json:"trends"`
	Fatigue     []FatigueSignal     `json:"fatigue"`
	Anomalies   []MetricAnomaly     `json:"anomalies"`
	Drivers     []FeatureDriver     `json:"drivers"`

	// FeatureCoverage 说明有多少参与分析的素材真的有特征数据。
	// 没有特征就没有变量，素材对比会退化成「两个素材谁的数字大」。
	AssetsInWindow  int `json:"assets_in_window"`
	AssetsWithFeats int `json:"assets_with_features"`
	// Judgement 是**跨视图**档位：五个视图里最弱的那一条。它回答的是「这一次分析
	// 整体能信到什么程度」，不回答「我现在看的这一屏能信到什么程度」。
	//
	// 页面上不要拿它当屏级徽章用——人看的是当前这一屏，而这个值里混着他没打开的
	// 那几屏。屏级徽章一律取 ViewJudgements 里对应的那一条。
	Judgement Judgement `json:"judgement"`
	// ViewJudgements 是每个视图**只按自己那一批结论**算出来的档位，键是视图名
	// （comparisons / trends / fatigue / anomalies / drivers）。
	//
	// 为什么要单独发一份而不是让前端自己从行里取最弱：档位怎么收敛是判定规则的一
	// 部分，规则只能有一处实现。前端各算各的，迟早出现「一行是能归因、整屏是算不
	// 出来」这种同屏打架，而没人说得清哪个对。
	ViewJudgements map[string]Judgement `json:"view_judgements"`
	Notes          []string             `json:"notes,omitempty"`
}

// FeatureDiff 是两个素材之间一个特征的取值差异，也就是 AM-009 说的「实验变量」。
type FeatureDiff struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Baseline string `json:"baseline"`
	Variant  string `json:"variant"`
	// Source 是这个变量的来源。它决定这条差异能不能进结论——摆在结构体里
	// 而不是让前端去猜，是因为「哪些变量算数」这件事只能有一个说法。
	Source FeatureSource `json:"source"`
	// Admissible 是 Source.AdmissibleForAttribution() 的结果，冗余一份给前端，
	// 免得前端把准入规则再实现一遍。
	Admissible bool `json:"admissible"`
}

// admissibleDiffs 挑出能进归因的那些差异。展示要给全，结论只能用这一部分。
func admissibleDiffs(diffs []FeatureDiff) []FeatureDiff {
	kept := make([]FeatureDiff, 0, len(diffs))
	for _, diff := range diffs {
		if diff.Admissible {
			kept = append(kept, diff)
		}
	}
	return kept
}

// weakestSource 取两侧里更弱的那个来源：ai 弱于 human 弱于 derived。只要有一侧
// 是模型推断，这条差异就不能进归因——两个取值里有一个是猜的，差异本身就是猜的。
// 只有一侧记了值时（另一侧显示「未记录」），以记了值的那一侧为准。
func weakestSource(left, right featureCell) FeatureSource {
	switch {
	case left.value == "":
		return right.source
	case right.value == "":
		return left.source
	case sourceStrength(left.source) <= sourceStrength(right.source):
		return left.source
	default:
		return right.source
	}
}

func sourceStrength(source FeatureSource) int {
	switch source {
	case SourceDerived:
		return 3
	case SourceHuman:
		return 2
	case SourceAI:
		return 1
	}
	return 0
}

// VariantVerdict 说明这一对素材的差异能不能归到某个变量上。
type VariantVerdict string

const (
	// VerdictAttributable：恰好一个变量不同，且样本足够、区间不重叠。
	VerdictAttributable VariantVerdict = "attributable"
	// VerdictDirectional：变量单一但区间重叠，只能说方向。
	VerdictDirectional VariantVerdict = "directional"
	// VerdictConfounded：不止一个变量不同，差异归不到任何单一变量上。
	VerdictConfounded VariantVerdict = "confounded"
	// VerdictLowSample：样本不够，先不谈差异。
	VerdictLowSample VariantVerdict = "low_sample"
	// VerdictNoFeatures：至少一边没有特征数据，连变量是什么都不知道。
	VerdictNoFeatures VariantVerdict = "no_features"
)

// VariantComparison 是素材对比的一行，同时承载 AM-009 变体分析。
type VariantComparison struct {
	BaselineAssetID string    `json:"baseline_asset_id"`
	BaselineTitle   string    `json:"baseline_title"`
	VariantAssetID  string    `json:"variant_asset_id"`
	VariantTitle    string    `json:"variant_title"`
	AssetType       AssetType `json:"asset_type,omitempty"`

	ChangedFeatures []FeatureDiff `json:"changed_features"`
	// ControlledCount 是两边取值相同的特征数——它是「控制住了多少」的度量。
	ControlledCount int `json:"controlled_count"`

	BaselineCounts MetricCounts `json:"baseline_counts"`
	VariantCounts  MetricCounts `json:"variant_counts"`
	BaselineRates  MetricRates  `json:"baseline_rates"`
	VariantRates   MetricRates  `json:"variant_rates"`

	BaselineCTRInterval *RateInterval `json:"baseline_ctr_interval,omitempty"`
	VariantCTRInterval  *RateInterval `json:"variant_ctr_interval,omitempty"`
	// IntervalsOverlap=true 表示两条置信区间重叠，差异可能只是噪声。
	IntervalsOverlap bool     `json:"intervals_overlap"`
	CTRLift          *float64 `json:"ctr_lift,omitempty"`

	// VariantVerdict 是这一对素材专有的五档，比三档更细：它还回答「归不了因是因为
	// 变量太多，还是因为压根没有特征数据」。字段名不能叫 Verdict——那样会遮蔽内嵌
	// Judgement 的三档 Verdict，JSON 里 verdict 键会静默变成 attributable 这类值。
	VariantVerdict VariantVerdict `json:"variant_verdict"`
	Judgement
}

// AssetTrend 是一个素材在窗口内的逐日走势。
type AssetTrend struct {
	AssetID    string             `json:"asset_id"`
	AssetTitle string             `json:"asset_title"`
	AssetType  AssetType          `json:"asset_type,omitempty"`
	Points     []PerformancePoint `json:"points"`
	// ActiveDays 是真的有数据的天数。窗口 30 天但只投了 3 天，走势图会骗人。
	ActiveDays int    `json:"active_days"`
	Direction  string `json:"direction"`
	// CTRChange 是后半段相对前半段的相对变化。分母为零时为空，不退化成 0。
	CTRChange *float64 `json:"ctr_change,omitempty"`
	Judgement
}

// FatigueSeverity 分三档，`none` 也会返回：知道「查过了，没有」比看不到这一行有用。
type FatigueSeverity string

const (
	FatigueNone   FatigueSeverity = "none"
	FatigueWatch  FatigueSeverity = "watch"
	FatigueLikely FatigueSeverity = "likely"
)

// FatigueSignal 对应 03 §7.4：识别曝光增大但点击/转化下降、成本恶化的趋势，
// 并且**必须区分数据延迟、受众变化、预算变化和真正的素材衰退**。
// 我们手上没有受众数据，区分不了的就明写在 AlternativeExplanations 里，
// 不假装排除过。
type FatigueSignal struct {
	AssetID    string    `json:"asset_id"`
	AssetTitle string    `json:"asset_title"`
	AssetType  AssetType `json:"asset_type,omitempty"`

	FirstHalf  MetricCounts `json:"first_half"`
	SecondHalf MetricCounts `json:"second_half"`
	FirstRates MetricRates  `json:"first_rates"`
	LastRates  MetricRates  `json:"last_rates"`

	CTRChange        *float64 `json:"ctr_change,omitempty"`
	CPAChange        *float64 `json:"cpa_change,omitempty"`
	ImpressionChange *float64 `json:"impression_change,omitempty"`

	Severity FatigueSeverity `json:"severity"`
	// AlternativeExplanations 是这次没能排除的其他解释。为空表示确实没有别的解释，
	// 不是「没检查」——检查项是固定的四类。
	AlternativeExplanations []string `json:"alternative_explanations,omitempty"`
	Judgement
}

// AnomalyKind 说明这一天是怎么不对劲的。
type AnomalyKind string

const (
	AnomalySpike AnomalyKind = "spike"
	AnomalyDrop  AnomalyKind = "drop"
	AnomalyGap   AnomalyKind = "gap"
)

// MetricAnomaly 是窗口内某一天偏离常态的记录。判定用中位数和 MAD 而不是均值和
// 标准差：广告数据里一次大促就能把均值和标准差同时拉走，之后什么都不算异常了。
type MetricAnomaly struct {
	Date       string      `json:"date"`
	Scope      string      `json:"scope"`
	AssetID    string      `json:"asset_id,omitempty"`
	AssetTitle string      `json:"asset_title,omitempty"`
	Metric     string      `json:"metric"`
	Kind       AnomalyKind `json:"kind"`

	Observed float64 `json:"observed"`
	Median   float64 `json:"median"`
	// Deviation 是偏离中位数多少个 MAD。
	Deviation float64 `json:"deviation"`
	// 异常永远只到 👁：这一天不对劲是事实，为什么不对劲这里答不了。
	// 所以档位固定 directional，不随样本量变动。
	Judgement
}

// FeatureDriver 是「哪一类内容特征伴随更好的表现」。
//
// 它是相关，不是因果——同一个特征取值的素材往往还共享别的特征。
// CovaryingFeatures 就是这件事的证据：这一组素材在这些特征上也整齐地
// 和其他素材不同，差异不能只算到 FeatureKey 头上。
type FeatureDriver struct {
	AssetType AssetType `json:"asset_type,omitempty"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	Group     string    `json:"group"`
	Value     string    `json:"value"`

	Assets     int          `json:"assets"`
	RestAssets int          `json:"rest_assets"`
	Counts     MetricCounts `json:"counts"`
	RestCounts MetricCounts `json:"rest_counts"`
	Rates      MetricRates  `json:"rates"`
	RestRates  MetricRates  `json:"rest_rates"`

	CTRInterval      *RateInterval `json:"ctr_interval,omitempty"`
	RestCTRInterval  *RateInterval `json:"rest_ctr_interval,omitempty"`
	IntervalsOverlap bool          `json:"intervals_overlap"`
	CTRLift          *float64      `json:"ctr_lift,omitempty"`

	CovaryingFeatures []string `json:"covarying_features,omitempty"`
	Judgement
}

// maxComparisonAssets 限制两两配对的规模。取花费最高的若干个素材配对，
// 其余的会在 Notes 里说清楚被排除了多少个——静默截断等于谎报覆盖面。
//
// 这是**出厂设定**：判定读的是 ResolvedThresholds.MaxComparisonAssets，
// 这个常量经由 defaultThresholds() 进去，没人调过时才生效。
const maxComparisonAssets = 8

// 趋势与异常的天数门槛的**出厂设定**。这几个数决定页面上什么时候给判定、
// 什么时候说「看不出来」。判定读的是 ResolvedThresholds，不再直接读这里；
// 这几个常量经由 defaultThresholds() 进去，没人在设置里调过时才生效。
//
// anomalyMADMultiple 不可配：它是判定方法本身的一部分（多少个 MAD 算离群），
// 不是「这个行业的合理门槛」。开放它等于让人调统计口径，而不是调业务标准。
const (
	// minTrendDays 少于这么多天就没有走势可言，趋势判 unknown、疲劳不给结论。
	minTrendDays = 4
	// minAnomalyDays 少于这么多天就没有「常态」可言，算出来的异常全是噪声。
	// 项目级和素材级用同一个数——换个阈值只会让两处的「异常」不是同一个意思。
	minAnomalyDays = 5
	// anomalyMADMultiple 偏离中位数超过这么多个 MAD 才算异常。用 MAD 不用标准差，
	// 是因为标准差会被它想找的那个异常点自己抬高，越异常越不容易被发现。
	anomalyMADMultiple = 3.5
)

// GetPerformanceAnalysis 组装五个解释性视图。
func (s Service) GetPerformanceAnalysis(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, window MetricWindow) (PerformanceAnalysis, error) {
	if err := s.connectorsReady(actor, projectID, ScopeRead); err != nil {
		return PerformanceAnalysis{}, err
	}
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return PerformanceAnalysis{}, err
	}
	if window.End.Before(window.Start) {
		return PerformanceAnalysis{}, fmt.Errorf("%w: 数据窗口的结束日期早于开始日期", ErrInvalidRequest)
	}
	if window.Days() > maxWindowDays {
		return PerformanceAnalysis{}, fmt.Errorf("%w: 数据窗口最长 %d 天", ErrInvalidRequest, maxWindowDays)
	}
	facts, err := s.Connectors.ListMetricFacts(ctx, actor.OrganizationID, projectID, window)
	if err != nil {
		return PerformanceAnalysis{}, err
	}

	assetIDs := attributableAssetIDs(facts)
	var features []AssetFeature
	if len(assetIDs) > 0 {
		features, err = s.Assets.ListAssetFeatures(ctx, actor.OrganizationID, projectID, assetIDs, len(assetIDs)*64)
		if err != nil {
			return PerformanceAnalysis{}, err
		}
	}
	return buildPerformanceAnalysis(window, facts, features,
		s.currentThresholds(ctx, actor.OrganizationID)), nil
}

func attributableAssetIDs(facts []MetricFactWithMapping) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, 16)
	for _, fact := range facts {
		if fact.AssetID == "" {
			continue
		}
		if _, ok := seen[fact.AssetID]; ok {
			continue
		}
		seen[fact.AssetID] = struct{}{}
		ids = append(ids, fact.AssetID)
	}
	sort.Strings(ids)
	return ids
}

// assetSlice 是一个素材在窗口内的全部事实，按日聚合后供五个视图复用。
type assetSlice struct {
	assetID string
	title   string
	kind    AssetType
	total   MetricCounts
	byDate  map[string]MetricCounts
	objects map[string]struct{}
	// features 只收「人工确认过的」和「AI 提取但没被拒绝的」，见 pickFeatures。
	// 带着来源一起存：展示可以用全部三类，归因只能用 derived 和 human。
	features map[string]featureCell
}

// featureCell 是一个特征的取值加它的来源。来源不跟着值走，下游就只能假设
// 「所有特征一样可信」——那是这个模块最贵的一个假设。
type featureCell struct {
	value  string
	source FeatureSource
}

// featureValue 是展示口：三类来源都给。
func (a *assetSlice) featureValue(key string) (string, bool) {
	cell, ok := a.features[key]
	if !ok || cell.value == "" {
		return "", false
	}
	return cell.value, true
}

// attributableFeature 是归因口：只给 derived 和 human。
func (a *assetSlice) attributableFeature(key string) (string, bool) {
	cell, ok := a.features[key]
	if !ok || cell.value == "" || !cell.source.AdmissibleForAttribution() {
		return "", false
	}
	return cell.value, true
}

func (a *assetSlice) dates() []string {
	values := make([]string, 0, len(a.byDate))
	for date := range a.byDate {
		values = append(values, date)
	}
	sort.Strings(values)
	return values
}

// thresholds 从服务层传下来，一次请求只读一次，然后一路传给五个视图。
// 每个视图各读一次的话，一次请求里如果有人正好保存了新阈值，同一屏上
// 对比和趋势会按两套标准判，而页面上只会盖一个版本号。
// 零值时逐格退回出厂设定（orDefaults），测试可以直接传 ResolvedThresholds{}。
func buildPerformanceAnalysis(window MetricWindow, facts []MetricFactWithMapping,
	features []AssetFeature, thresholds ResolvedThresholds) PerformanceAnalysis {
	thresholds = thresholds.orDefaults()
	analysis := PerformanceAnalysis{Window: window, Comparable: true}

	slices := map[string]*assetSlice{}
	projectByDate := map[string]MetricCounts{}
	currencies := map[string]struct{}{}
	attributions := map[string]struct{}{}
	schemas := map[string]struct{}{}
	platforms := map[Platform]struct{}{}

	for _, fact := range facts {
		date := fact.StatDate.Format("2006-01-02")
		projectByDate[date] = projectByDate[date].add(fact.Counts)
		currencies[fact.Caliber.Currency] = struct{}{}
		attributions[fact.Caliber.AttributionWindow] = struct{}{}
		schemas[fact.Caliber.MetricSchemaVersion] = struct{}{}
		platforms[fact.Platform] = struct{}{}
		if fact.AssetID == "" {
			continue
		}
		slice, ok := slices[fact.AssetID]
		if !ok {
			slice = &assetSlice{
				assetID: fact.AssetID, title: fact.AssetTitle, kind: fact.AssetType,
				byDate: map[string]MetricCounts{}, objects: map[string]struct{}{},
				features: map[string]featureCell{},
			}
			slices[fact.AssetID] = slice
		}
		slice.total = slice.total.add(fact.Counts)
		slice.byDate[date] = slice.byDate[date].add(fact.Counts)
		slice.objects[string(fact.Platform)+"\x00"+fact.PlatformObjectKind+"\x00"+fact.PlatformObjectID] = struct{}{}
	}

	analysis.Caliber = MetricCaliber{
		Currency:            singleOr(currencies, "多币种"),
		AttributionWindow:   singleOr(attributions, "多归因窗口"),
		MetricSchemaVersion: singleOr(schemas, "多版本"),
		TimeZone:            "按各数据源账户时区聚合",
	}
	var reasons []string
	if len(currencies) > 1 {
		reasons = append(reasons, "窗口内混合了多种币种")
	}
	if len(attributions) > 1 {
		reasons = append(reasons, "窗口内混合了多种归因窗口")
	}
	if len(schemas) > 1 {
		reasons = append(reasons, "窗口内混合了多个指标口径版本")
	}
	if len(platforms) > 1 {
		reasons = append(reasons, "窗口内包含多个平台，平台之间的指标定义不同")
	}
	if len(reasons) > 0 {
		analysis.Comparable = false
		analysis.ComparableReason = strings.Join(reasons, "；")
	}

	assignFeatures(slices, features)

	ordered := make([]*assetSlice, 0, len(slices))
	for _, slice := range slices {
		ordered = append(ordered, slice)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].total.SpendCents != ordered[j].total.SpendCents {
			return ordered[i].total.SpendCents > ordered[j].total.SpendCents
		}
		return ordered[i].assetID < ordered[j].assetID
	})

	analysis.AssetsInWindow = len(ordered)
	for _, slice := range ordered {
		if len(slice.features) > 0 {
			analysis.AssetsWithFeats++
		}
	}

	analysis.Comparisons = buildComparisons(ordered, analysis.Comparable, &analysis.Notes, thresholds)
	analysis.Trends = buildTrends(ordered, thresholds)
	analysis.Fatigue = buildFatigue(ordered, window, thresholds)
	anomalies, anomalyCoverage := buildAnomalies(projectByDate, ordered, thresholds)
	analysis.Anomalies = anomalies
	analysis.Drivers = buildDrivers(ordered, analysis.Comparable, thresholds)

	if analysis.AssetsInWindow == 0 {
		analysis.Notes = append(analysis.Notes, "窗口内没有任何能归到素材上的投放数据，五个视图都无从算起。先去数据接入把平台对象和素材对应起来。")
	} else if analysis.AssetsWithFeats == 0 {
		analysis.Notes = append(analysis.Notes, "窗口内的素材都还没有内容特征，素材对比只能比数字、驱动因素无从谈起。先去内容分析把特征提出来。")
	}
	if !analysis.Comparable {
		analysis.Notes = append(analysis.Notes, "口径不一致："+analysis.ComparableReason+"。这一页所有对比结论都只能当方向性观察。")
	}

	// 先按视图各算各的，再把五份合成跨视图那一份。反过来做（先算总的再拆）会
	// 丢掉「这一屏一条结论都没有」和「这一屏的结论都很弱」的区别。
	analysis.ViewJudgements = map[string]Judgement{
		"comparisons": viewJudgement("comparisons", verdictsOf(analysis.Comparisons, func(item VariantComparison) Judgement { return item.Judgement }), thresholds),
		"trends":      viewJudgement("trends", verdictsOf(analysis.Trends, func(item AssetTrend) Judgement { return item.Judgement }), thresholds),
		"fatigue":     viewJudgement("fatigue", verdictsOf(analysis.Fatigue, func(item FatigueSignal) Judgement { return item.Judgement }), thresholds),
		"anomalies":   anomalyViewJudgement(verdictsOf(analysis.Anomalies, func(item MetricAnomaly) Judgement { return item.Judgement }), anomalyCoverage, thresholds),
		"drivers":     viewJudgement("drivers", verdictsOf(analysis.Drivers, func(item FeatureDriver) Judgement { return item.Judgement }), thresholds),
	}

	// 跨视图档位取最弱：整次分析里只要有一屏说算不出来，这次分析整体就不能说成
	// 能归因。它出现在「这一页怎么读」里，不出现在屏级徽章上。
	verdicts := make([]Verdict, 0, len(analysis.Comparisons)+len(analysis.Trends)+
		len(analysis.Fatigue)+len(analysis.Anomalies)+len(analysis.Drivers))
	for _, item := range analysis.Comparisons {
		verdicts = append(verdicts, item.Verdict)
	}
	for _, item := range analysis.Trends {
		verdicts = append(verdicts, item.Verdict)
	}
	for _, item := range analysis.Fatigue {
		verdicts = append(verdicts, item.Verdict)
	}
	for _, item := range analysis.Anomalies {
		verdicts = append(verdicts, item.Verdict)
	}
	for _, item := range analysis.Drivers {
		verdicts = append(verdicts, item.Verdict)
	}
	weakest := weakestVerdict(verdicts...)
	screenThresholdVersion := thresholds.Version
	analysis.Judgement = Judgement{
		Confidence:   worstConfidenceOf(analysis),
		Verdict:      weakest,
		VerdictLabel: weakest.Label(),
		Upgrade:      weakest.Upgrade(),
		Note:         crossViewNote(weakest, len(verdicts)),
		// 跨视图那一份也要盖号码：它同样是「按某一版标准算出来的」。这里手拼
		// Judgement 而不走 judgeAt，是因为它由五视图取最弱算出来，不是从一个
		// confidence 收敛来的——套 judgeAt 会把已经算好的 weakest 覆盖掉。
		ThresholdVersion: &screenThresholdVersion,
	}
	return analysis
}

// verdictsOf 把一个视图里每一行的判定摘出来。泛型是为了不给五个视图各写一遍
// 同样的 for 循环——那种重复最容易在加第六个视图时漏掉一个。
func verdictsOf[T any](items []T, pick func(T) Judgement) []Judgement {
	out := make([]Judgement, 0, len(items))
	for _, item := range items {
		out = append(out, pick(item))
	}
	return out
}

// viewJudgement 给单个视图定档：只看这一屏自己的结论，取最弱的那一条。
//
// 一条结论都没有时给 unclear，理由用这个视图专属的那句——「这一屏里有结论连差异
// 存不存在都判断不了」安到一条结论都没有的屏上是答非所问。
func viewJudgement(view string, items []Judgement, thresholds ResolvedThresholds) Judgement {
	verdicts := make([]Verdict, 0, len(items))
	worst := ConfidenceSufficient
	seen := false
	for _, item := range items {
		verdicts = append(verdicts, item.Verdict)
		if !seen || confidenceRank(item.Confidence) < confidenceRank(worst) {
			worst, seen = item.Confidence, true
		}
	}
	if !seen {
		worst = ConfidenceLowSample
	}
	weakest := weakestVerdict(verdicts...)
	version := thresholds.Version
	return Judgement{
		Confidence:       worst,
		Verdict:          weakest,
		VerdictLabel:     weakest.Label(),
		Upgrade:          weakest.Upgrade(),
		Note:             viewNote(view, weakest, len(verdicts)),
		ThresholdVersion: &version,
	}
}

// viewNote 是屏级徽章上那句话。主语必须是「这一屏」，而且要说清这一屏自己的情况。
func viewNote(view string, verdict Verdict, items int) string {
	if items == 0 {
		return viewEmptyNotes[view]
	}
	switch verdict {
	case VerdictExplained:
		return "这一屏的结论都站得住，可以直接用。"
	case VerdictObserved:
		return "这一屏里有结论归不到具体变量上，只能当观察看。"
	}
	return "这一屏里有结论连差异存不存在都判断不了。"
}

// anomalyViewJudgement 是异常屏专属的定档。别的四屏「一条都没有」只有一个意思
// ——算不出来；异常屏不是：它零条的常见含义恰恰是**查过了，很干净**，那是一条
// 站得住的结论，不该顶着「❓ 算不出来」发出去（人会以为检测坏了，或者以为
// 这屏还没跑）。所以这里按覆盖情况分开定档，而不是沿用 viewJudgement 的空态。
func anomalyViewJudgement(items []Judgement, scan anomalyScan, thresholds ResolvedThresholds) Judgement {
	if len(items) > 0 {
		return viewJudgement("anomalies", items, thresholds)
	}
	if !scan.covered() {
		// 没查成的原因要说对。天数够了但每天数字一模一样，跟天数不够是两件事，
		// 后者再等几天就有，前者等多久都不会有——这批数据本身没有波动可言。
		if scan.FlatAssets > 0 || scan.ProjectFlat {
			return judgeAt(thresholds, ConfidenceLowSample,
				"窗口内的数字每天一模一样，没有起伏就没有「常态」可言，偏离也就无从算起——这种序列通常是补录或按均值摊出来的。"+
					"这一屏空着不代表没问题，是没查成。")
		}
		return judgeAt(thresholds, ConfidenceLowSample, fmt.Sprintf(
			"窗口内还没有序列跑够 %d 天，这项判断没做成——这一屏是空的不代表没问题，是根本没查。",
			thresholds.MinAnomalyDays))
	}
	if skipped := scan.skipped(); skipped > 0 {
		return judgeAt(thresholds, ConfidenceSufficient, fmt.Sprintf(
			"查过了，这个窗口里没有哪一天偏离常态。另有 %d 个素材没参与这项检查（天数不够 %d 天，或者整段数字没有起伏）。",
			skipped, thresholds.MinAnomalyDays))
	}
	return judgeAt(thresholds, ConfidenceSufficient, "查过了，这个窗口里没有哪一天偏离常态。")
}

// viewEmptyNotes 说的是「这一屏为什么一条都没有」，不是「这一屏的结论很弱」。
// 前端的空态提示和这里保持同一口径。
//
// 每条都带一句「怎么补」：空态最容易被读成「这个功能坏了」，写清缺什么、去哪补，
// 人才有下一步。前端不再另写一份——两份文案迟早不一致。
var viewEmptyNotes = map[string]string{
	"comparisons": "这一屏没有可配对的素材，比不出任何差异。要配对得有同一类型下至少两个素材、且都在这个窗口里有投放数据。",
	"trends":      "这一屏没有素材跑够可比较的时间，算不出走势。先确认这个窗口里有素材在投，数据也回流了。",
	"fatigue":     "这一屏没有素材跑够两段可比较的时间，判断不了跑不跑得动。同一条素材至少要连着投上几天。",
	// 异常屏的空态正常走 anomalyViewJudgement，不落到这里。留一条兜底文案是
	// 防它哪天被别的路径调到——那时也不能说成「没发现」，因为查没查过还不知道。
	"anomalies": "这一屏没有列出偏离常态的日子。",
	"drivers":   "这一屏还没有足够的特征数据，谈不上哪个特征在起作用。要么素材还没记内容特征，要么同一取值下的素材不足 2 个——去「素材 · 变量」补。",
}

// worstConfidenceOf 给跨视图档位配一个统计口径值，让 confidence 和 verdict 不打架。
// 三档是从四档收敛来的，反过来一个 verdict 对应不止一个 confidence，
// 这里取「最能解释为什么是这一档」的那个。
func worstConfidenceOf(analysis PerformanceAnalysis) ConfidenceLevel {
	worst := ConfidenceSufficient
	seen := false
	visit := func(level ConfidenceLevel) {
		if !seen || confidenceRank(level) < confidenceRank(worst) {
			worst, seen = level, true
		}
	}
	for _, item := range analysis.Comparisons {
		visit(item.Confidence)
	}
	for _, item := range analysis.Trends {
		visit(item.Confidence)
	}
	for _, item := range analysis.Fatigue {
		visit(item.Confidence)
	}
	for _, item := range analysis.Anomalies {
		visit(item.Confidence)
	}
	for _, item := range analysis.Drivers {
		visit(item.Confidence)
	}
	if !seen {
		return ConfidenceLowSample
	}
	return worst
}

// crossViewNote 说的是整次分析，不是某一屏。主语必须是「这次分析」——写成
// 「这一屏」的话，它出现在任何一个视图上都在替那一屏说话，而它算的是五屏之和。
func crossViewNote(verdict Verdict, items int) string {
	if items == 0 {
		return "这个窗口里还没有能出结论的数据。"
	}
	switch verdict {
	case VerdictExplained:
		return "这次分析的五个视图里，每一条结论都站得住。"
	case VerdictObserved:
		return "这次分析里有结论归不到具体变量上，它们可能不在你当前这一屏。"
	}
	return "这次分析里有结论连差异存不存在都判断不了，它们可能不在你当前这一屏。"
}

// assignFeatures 把特征贴到素材上。同一个 key 有 AI 行和人工行时以人工为准
// （AM-006「人工结果不被后台覆盖」）；只有被人**明确否掉**的行丢掉——被否掉的
// 推断不该继续参与变量识别。
//
// 注意 authored 不在丢弃之列：那是 AI 没提过、人第一个填的项，是货真价实的
// 特征，不是被推翻的推断。早先这里连它一起丢，导致人在内容分析里手填的特征
// 在素材对比和驱动因素里一条都看不见。
func assignFeatures(slices map[string]*assetSlice, features []AssetFeature) {
	human := map[string]map[string]struct{}{}
	for _, feature := range features {
		slice, ok := slices[feature.AssetID]
		if !ok || !feature.ReviewState.CountsTowardAnalysis() {
			continue
		}
		if !comparableKind(feature.Value.Kind) {
			continue
		}
		text := featureValueText(feature.Value)
		if text == "" {
			continue
		}
		confirmed := feature.Source == SourceHuman || feature.ReviewState == ReviewConfirmed
		if _, taken := human[feature.AssetID][feature.Key]; taken && !confirmed {
			continue
		}
		if confirmed {
			if human[feature.AssetID] == nil {
				human[feature.AssetID] = map[string]struct{}{}
			}
			human[feature.AssetID][feature.Key] = struct{}{}
		}
		// 存的是「有效来源」，不是原始来源：AI 提取但人被拉来看过并认可的行，
		// 从此按人工标注算——有人为它背书了。没人看过的推断仍然是推断。
		// 不这么做的话，「人工复核」这道工序对归因就毫无意义：复核完了还是进不了结论。
		source := feature.Source
		if confirmed {
			source = SourceHuman
		}
		slice.features[feature.Key] = featureCell{value: text, source: source}
	}
}

func featureValueText(value FeatureValue) string {
	switch value.Kind {
	case FeatureKindTags, FeatureKindEnum, FeatureKindEnumMul:
		if len(value.Terms) == 0 {
			return strings.TrimSpace(value.Text)
		}
		terms := append([]string(nil), value.Terms...)
		sort.Strings(terms)
		return strings.Join(terms, "、")
	case FeatureKindBool:
		if value.Bool {
			return "是"
		}
		return "否"
	case FeatureKindNumber, FeatureKindDuration:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value.Number), "0"), ".")
	default:
		return strings.TrimSpace(value.Text)
	}
}

// comparableKind 决定一个特征能不能当变量用。自由文本每个素材都不一样，
// 拿它当变量的话每一对素材都会「改了 N 个变量」，AM-009 的判定就永远是混杂。
// features.go 对 FeatureKindText 的注释写的就是 "not comparable across assets"。
func comparableKind(kind FeatureValueKind) bool {
	return kind != FeatureKindText
}

func fieldOf(kind AssetType, key string) FeatureField {
	if schema, ok := FeatureSchemaFor(kind); ok {
		if field, found := schema.Field(key); found {
			return field
		}
	}
	return FeatureField{Key: key, Label: key, Group: "未登记特征"}
}

// --- 素材对比 / 变体分析（AM-008、AM-009，03 §7.2 §7.3）---

func buildComparisons(ordered []*assetSlice, comparable bool, notes *[]string,
	thresholds ResolvedThresholds) []VariantComparison {
	thresholds = thresholds.orDefaults()
	pool := ordered
	if len(pool) > thresholds.MaxComparisonAssets {
		pool = pool[:thresholds.MaxComparisonAssets]
		*notes = append(*notes, fmt.Sprintf(
			"素材对比只配对了花费最高的 %d 个素材，窗口内另有 %d 个素材没有参与配对。",
			thresholds.MaxComparisonAssets, len(ordered)-thresholds.MaxComparisonAssets))
	}
	comparisons := make([]VariantComparison, 0, len(pool)*(len(pool)-1)/2)
	for i := 0; i < len(pool); i++ {
		for j := i + 1; j < len(pool); j++ {
			left, right := pool[i], pool[j]
			// 类型不同的素材没有共同的特征体系，比出来的差异连变量都对不齐。
			if left.kind != right.kind {
				continue
			}
			comparisons = append(comparisons, compareAssets(left, right, comparable, thresholds))
		}
	}
	sort.Slice(comparisons, func(i, j int) bool {
		return verdictRank(comparisons[i].VariantVerdict) < verdictRank(comparisons[j].VariantVerdict)
	})
	return comparisons
}

// verdictRank 让能归因的排在前面，样本不足和无特征的沉底。
func verdictRank(verdict VariantVerdict) int {
	switch verdict {
	case VerdictAttributable:
		return 0
	case VerdictDirectional:
		return 1
	case VerdictConfounded:
		return 2
	case VerdictLowSample:
		return 3
	default:
		return 4
	}
}

func compareAssets(baseline, variant *assetSlice, comparable bool,
	thresholds ResolvedThresholds) VariantComparison {
	thresholds = thresholds.orDefaults()
	result := VariantComparison{
		BaselineAssetID: baseline.assetID, BaselineTitle: baseline.title,
		VariantAssetID: variant.assetID, VariantTitle: variant.title,
		AssetType:      baseline.kind,
		BaselineCounts: baseline.total, VariantCounts: variant.total,
		BaselineRates: RatesOf(baseline.total), VariantRates: RatesOf(variant.total),
		// 必须初始化成空切片，不能留 nil。两个素材在已记录特征上完全一致时这里
		// 一条都不会 append，nil 会被序列化成 null，前端读 .length 直接抛异常并
		// 崩掉整个投后分析页——六个视图一起打不开。
		ChangedFeatures: make([]FeatureDiff, 0),
	}
	result.BaselineCTRInterval = WilsonInterval(baseline.total.Clicks, baseline.total.Impressions)
	result.VariantCTRInterval = WilsonInterval(variant.total.Clicks, variant.total.Impressions)
	result.IntervalsOverlap = intervalsOverlap(result.BaselineCTRInterval, result.VariantCTRInterval)
	result.CTRLift = relativeChange(result.BaselineRates.CTR, result.VariantRates.CTR)

	keys := map[string]struct{}{}
	for key := range baseline.features {
		keys[key] = struct{}{}
	}
	for key := range variant.features {
		keys[key] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	for _, key := range sorted {
		left, right := baseline.features[key], variant.features[key]
		source := weakestSource(left, right)
		if left.value == right.value {
			// 受控变量只数能进归因的那些：两边都是模型猜的「情绪一致」，
			// 并不能让「只改了一个变量」这句话更站得住脚。
			if left.value != "" && source.AdmissibleForAttribution() {
				result.ControlledCount++
			}
			continue
		}
		field := fieldOf(baseline.kind, key)
		result.ChangedFeatures = append(result.ChangedFeatures, FeatureDiff{
			Key: key, Label: field.Label, Group: field.Group,
			Baseline: orDash(left.value), Variant: orDash(right.value),
			Source: source, Admissible: source.AdmissibleForAttribution(),
		})
	}

	minImpressions := baseline.total.Impressions
	if variant.total.Impressions < minImpressions {
		minImpressions = variant.total.Impressions
	}
	// 判定只看能进归因的那部分变量。模型推断的差异照样列在 ChangedFeatures 里给人看，
	// 但不参与「改了几个变量」的计数——否则一条模型猜出来的差异就能撑起一个归因结论。
	admissible := admissibleDiffs(result.ChangedFeatures)
	switch {
	case len(baseline.features) == 0 || len(variant.features) == 0:
		result.VariantVerdict = VerdictNoFeatures
		result.Judgement = judgeAt(thresholds, ConfidenceConfounded,
			"至少一边没有内容特征，两个素材之间到底改了什么无从判断。数字上的差异不能算到任何变量头上。")
	case minImpressions < int64(thresholds.DirectionalImpressions):
		result.VariantVerdict = VerdictLowSample
		result.Judgement = judgeAt(thresholds, ConfidenceLowSample,
			fmt.Sprintf("样本较少的一边只有 %s 次展示，不到 %s 次的方向性门槛，先不谈差异。",
				countText(minImpressions), countText(int64(thresholds.DirectionalImpressions))))
	case len(result.ChangedFeatures) == 0:
		result.VariantVerdict = VerdictConfounded
		result.Judgement = judgeAt(thresholds, ConfidenceConfounded,
			"两个素材在已记录的特征上完全一致，差异来自特征体系没覆盖到的地方——可能是投放设置、时段或受众，不是内容。")
	case len(admissible) == 0:
		result.VariantVerdict = VerdictNoFeatures
		result.Judgement = judgeAt(thresholds, ConfidenceConfounded,
			fmt.Sprintf("两个素材的差异只出现在模型推断的变量上（%s）。模型推断不进结论——用一个猜测去解释另一个猜测，一层假设都没减少。要归因，先在内容分析里人工确认这几个变量。",
				joinFeatureLabels(result.ChangedFeatures)))
	case len(admissible) > 1:
		result.VariantVerdict = VerdictConfounded
		result.Judgement = judgeAt(thresholds, ConfidenceConfounded,
			fmt.Sprintf("这一对同时改了 %d 个变量（%s），差异归不到其中任何一个上。要归因得再做一组只改一个变量的素材。",
				len(admissible), joinFeatureLabels(admissible)))
	case result.IntervalsOverlap:
		result.VariantVerdict = VerdictDirectional
		result.Judgement = judgeAt(thresholds, ConfidenceDirectional,
			fmt.Sprintf("只改了「%s」，但两边的点击率置信区间重叠，差异可能只是波动。方向可以参考，不能当结论。",
				admissible[0].Label))
	case minImpressions < int64(thresholds.SufficientImpressions):
		result.VariantVerdict = VerdictDirectional
		result.Judgement = judgeAt(thresholds, ConfidenceDirectional,
			fmt.Sprintf("只改了「%s」，区间也不重叠，但样本还没到 %s 次展示的充分门槛。",
				admissible[0].Label, countText(int64(thresholds.SufficientImpressions))))
	case result.ControlledCount == 0:
		// 「只改了一个变量」这句话是靠受控变量撑起来的：两边还有别的特征、而且取值
		// 相同，才谈得上「别的都没动」。一个受控变量都没有，说明两条素材身上各自
		// 只记着这一个能比的特征——差异确实存在，但把它归到这个变量上，等于用
		// 「我们只量了这一个」冒充「只有这一个不一样」。
		//
		// 这一档不是样本问题，补数据也不会变成 ✅：要升上去得先把两边的内容变量
		// 补齐（人工确认过的那种），让「其余特征相同」这句话真的有东西撑着。
		result.VariantVerdict = VerdictDirectional
		result.Judgement = judgeAt(thresholds, ConfidenceDirectional,
			fmt.Sprintf("只改了「%s」，样本也够、区间也不重叠，但两边再没有第二个能对上的特征"+
				"——除了这一个，别的地方是不是也不一样，现在判断不了。先去「素材 · 变量」把两条素材的变量补齐再看。",
				admissible[0].Label))
	default:
		result.VariantVerdict = VerdictAttributable
		result.Judgement = judgeAt(thresholds, ConfidenceSufficient,
			fmt.Sprintf("只改了「%s」，其余 %d 个特征取值相同，样本充分且区间不重叠——这个差异可以归到这个变量上。",
				admissible[0].Label, result.ControlledCount))
	}
	// 口径不一致会把所有归因打回方向性：差异可能全部来自口径本身。
	if !comparable && result.VariantVerdict == VerdictAttributable {
		result.VariantVerdict = VerdictDirectional
		result.Judgement = judgeAt(thresholds, ConfidenceConfounded,
			result.Note+"（但窗口内口径不一致，这一条降级为方向性观察。）")
	}
	return result
}

func joinFeatureLabels(diffs []FeatureDiff) string {
	labels := make([]string, 0, len(diffs))
	for _, diff := range diffs {
		labels = append(labels, diff.Label)
	}
	if len(labels) > 4 {
		return strings.Join(labels[:4], "、") + fmt.Sprintf(" 等 %d 个", len(labels))
	}
	return strings.Join(labels, "、")
}

func orDash(value string) string {
	if value == "" {
		return "（未记录）"
	}
	return value
}

func intervalsOverlap(left, right *RateInterval) bool {
	if left == nil || right == nil {
		// 有一边算不出区间就当作重叠：不知道差异是否显著时，默认它不显著。
		return true
	}
	return left.Low <= right.High && right.Low <= left.High
}

// relativeChange 是 (after-before)/before。before 为空或为 0 时返回 nil，
// 不退化成 0——「没有基线」和「没有变化」是两件事（doc10 §6）。
func relativeChange(before, after *float64) *float64 {
	if before == nil || after == nil || *before == 0 {
		return nil
	}
	value := (*after - *before) / *before
	return &value
}

// --- 趋势（03 §7.4 的前半）---

func buildTrends(ordered []*assetSlice, thresholds ResolvedThresholds) []AssetTrend {
	thresholds = thresholds.orDefaults()
	trends := make([]AssetTrend, 0, len(ordered))
	for _, slice := range ordered {
		dates := slice.dates()
		trend := AssetTrend{
			AssetID: slice.assetID, AssetTitle: slice.title, AssetType: slice.kind,
			ActiveDays: len(dates),
			// 同 ChangedFeatures：空切片要初始化，nil 序列化成 null 会崩掉前端。
			Points: make([]PerformancePoint, 0, len(dates)),
		}
		for _, date := range dates {
			counts := slice.byDate[date]
			trend.Points = append(trend.Points, PerformancePoint{Date: date, Counts: counts, Rates: RatesOf(counts)})
		}
		first, second := splitHalves(slice)
		trend.CTRChange = relativeChange(RatesOf(first).CTR, RatesOf(second).CTR)
		confidence := confidenceOf(slice.total, true, len(slice.objects), thresholds)
		var note string
		switch {
		case len(dates) < thresholds.MinTrendDays:
			trend.Direction, note = "unknown", fmt.Sprintf("窗口内只有 %d 天有数据，看不出走势。", len(dates))
			// 同疲劳那边：天数不够就没有走势可言，曝光量再大也换不来天数。
			// 不压档位的话，页面上会出现「看不出走势 · 置信充分」。
			confidence = ConfidenceLowSample
		case trend.CTRChange == nil:
			trend.Direction, note = "unknown", "前半段没有展示，算不出变化，不能当成持平。"
			confidence = ConfidenceLowSample
		case *trend.CTRChange <= -0.15:
			trend.Direction, note = "declining", "后半段点击率明显低于前半段。"
		case *trend.CTRChange >= 0.15:
			trend.Direction, note = "rising", "后半段点击率明显高于前半段。"
		default:
			trend.Direction, note = "flat", "前后两段点击率变化在 ±15% 以内。"
		}
		trend.Judgement = judgeAt(thresholds, confidence, note)
		trends = append(trends, trend)
	}
	return trends
}

// splitHalves 按有数据的日期数对半切，不按窗口天数——窗口 30 天只投了 6 天时，
// 按窗口切会把全部数据塞进其中一半。
func splitHalves(slice *assetSlice) (MetricCounts, MetricCounts) {
	dates := slice.dates()
	var first, second MetricCounts
	mid := len(dates) / 2
	for index, date := range dates {
		if index < mid {
			first = first.add(slice.byDate[date])
		} else {
			second = second.add(slice.byDate[date])
		}
	}
	return first, second
}

// --- 疲劳（03 §7.4）---

func buildFatigue(ordered []*assetSlice, window MetricWindow,
	thresholds ResolvedThresholds) []FatigueSignal {
	thresholds = thresholds.orDefaults()
	signals := make([]FatigueSignal, 0, len(ordered))
	for _, slice := range ordered {
		first, second := splitHalves(slice)
		firstRates, secondRates := RatesOf(first), RatesOf(second)
		signal := FatigueSignal{
			AssetID: slice.assetID, AssetTitle: slice.title, AssetType: slice.kind,
			FirstHalf: first, SecondHalf: second,
			FirstRates: firstRates, LastRates: secondRates,
			CTRChange:        relativeChange(firstRates.CTR, secondRates.CTR),
			CPAChange:        relativeChange(firstRates.CPACents, secondRates.CPACents),
			ImpressionChange: relativeChange(floatOf(first.Impressions), floatOf(second.Impressions)),
			Severity:         FatigueNone,
		}

		ctrDown := signal.CTRChange != nil && *signal.CTRChange <= -0.2
		cpaUp := signal.CPAChange != nil && *signal.CPAChange >= 0.2
		impressionsUp := signal.ImpressionChange != nil && *signal.ImpressionChange >= 0.1

		confidence := confidenceOf(slice.total, true, len(slice.objects), thresholds)
		var note string
		switch {
		case len(slice.dates()) < thresholds.MinTrendDays:
			note = fmt.Sprintf("只有 %d 天数据，疲劳要看趋势，天数不够就没有趋势可看。", len(slice.dates()))
			// 曝光量再大也换不来天数。这里必须把置信压到 low_sample，否则页面上会
			// 出现「没有疲劳迹象 · 置信充分」——那是在说「查过了，没问题」，
			// 而实际情况是「压根没法查」。这两句话对读者的意义完全相反。
			confidence = ConfidenceLowSample
		case ctrDown && impressionsUp:
			// 03 §7.4 点名的典型形态：曝光继续放大，点击却掉下来。
			signal.Severity = FatigueLikely
			note = "曝光还在放大，点击率却明显下滑——这是素材疲劳最典型的形态。"
		case ctrDown && cpaUp:
			signal.Severity = FatigueLikely
			note = "点击率下滑的同时单次转化成本上升，效率在双向恶化。"
		case ctrDown || cpaUp:
			signal.Severity = FatigueWatch
			note = "有一项指标在恶化，但另一项没有同向变化，还不足以判定为素材衰退。"
		default:
			note = "后半段没有出现点击率下滑或成本上升，这一轮看不到疲劳迹象。"
		}
		signal.Judgement = judgeAt(thresholds, confidence, note)

		if signal.Severity != FatigueNone {
			signal.AlternativeExplanations = fatigueAlternatives(signal, slice, window)
		}
		signals = append(signals, signal)
	}
	sort.Slice(signals, func(i, j int) bool {
		return fatigueRank(signals[i].Severity) < fatigueRank(signals[j].Severity)
	})
	return signals
}

func fatigueRank(severity FatigueSeverity) int {
	switch severity {
	case FatigueLikely:
		return 0
	case FatigueWatch:
		return 1
	default:
		return 2
	}
}

// fatigueAlternatives 列出这次**没能排除**的其他解释（03 §7.4 要求区分
// 数据延迟、受众变化、预算变化和真正的素材衰退）。能排除的就不列，
// 排除不了的必须列出来——把「排除不了」写成「已排除」是最坏的一种假精确。
func fatigueAlternatives(signal FatigueSignal, slice *assetSlice, window MetricWindow) []string {
	var reasons []string
	if signal.SecondHalf.SpendCents > 0 && signal.FirstHalf.SpendCents > 0 {
		change := float64(signal.SecondHalf.SpendCents-signal.FirstHalf.SpendCents) / float64(signal.FirstHalf.SpendCents)
		if math.Abs(change) >= 0.2 {
			reasons = append(reasons, fmt.Sprintf("后半段花费变化了 %.0f%%，预算调整本身就会改变流量结构和竞价环境。", change*100))
		}
	}
	dates := slice.dates()
	if len(dates) > 0 {
		last := dates[len(dates)-1]
		if last < window.End.Format("2006-01-02") {
			reasons = append(reasons, fmt.Sprintf("这个素材最后一天有数据是 %s，晚于它的日期都还没回流，末段下滑可能只是数据没到齐。", last))
		}
	}
	if len(slice.objects) > 1 {
		reasons = append(reasons, fmt.Sprintf("这个素材同时投在 %d 个平台对象上，各自的受众和出价不同，合并看趋势会互相稀释。", len(slice.objects)))
	}
	// 受众变化永远排除不了：我们手上没有受众构成数据。
	reasons = append(reasons, "受众变化排除不了——当前没有接入受众构成数据，人群变了和素材看腻了在数字上长得一样。")
	return reasons
}

func floatOf(value int64) *float64 {
	result := float64(value)
	return &result
}

// --- 异常（20 §4.1「错误与延迟置顶」）---

// anomalyScan 记的是这一轮异常检测**到底查过什么**。
//
// 零条异常有两种截然相反的含义：查过了、这个窗口很干净；和一条序列都没跑够天数、
// 根本没查成。不把它记下来的话，屏级徽章只能一律给「❓ 算不出来」，于是一屏
// 干净数据被说成没查成——人会以为异常检测坏了，而它其实正常跑完了。
type anomalyScan struct {
	// ProjectScanned 项目级花费序列是否真的跑过判定（天数够且有波动可言）。
	ProjectScanned bool
	// ProjectFlat 项目级天数够了，但整段花费一点波动都没有。
	ProjectFlat bool
	// ScannedAssets 跑过判定的素材条数。
	ScannedAssets int
	// ShortAssets 天数不够门槛、没参与判定的素材条数。
	ShortAssets int
	// FlatAssets 天数够了、但整段曝光一点波动都没有的素材条数。
	//
	// 这两种「没查成」要分开记，因为给人的下一步完全不同：天数不够是再等几天，
	// 没有波动是这批数据本身有问题（补录、按均值摊）。混成一句「天数不够」，
	// 会让人对着一条跑满 7 天的素材看到「还没跑够 5 天」。
	FlatAssets int
}

func (s anomalyScan) covered() bool { return s.ProjectScanned || s.ScannedAssets > 0 }

func (s anomalyScan) skipped() int { return s.ShortAssets + s.FlatAssets }

func buildAnomalies(projectByDate map[string]MetricCounts, ordered []*assetSlice,
	thresholds ResolvedThresholds) ([]MetricAnomaly, anomalyScan) {
	thresholds = thresholds.orDefaults()
	scan := anomalyScan{}
	anomalies := make([]MetricAnomaly, 0, 8)
	dates := make([]string, 0, len(projectByDate))
	for date := range projectByDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	spends := make([]float64, 0, len(dates))
	for _, date := range dates {
		spends = append(spends, float64(projectByDate[date].SpendCents))
	}
	median, mad := medianAndMAD(spends)
	// 少于这么多天没有「常态」可言，算出来的异常全是噪声。
	if len(dates) >= thresholds.MinAnomalyDays && mad <= 0 {
		scan.ProjectFlat = true
	}
	if len(dates) >= thresholds.MinAnomalyDays && mad > 0 {
		scan.ProjectScanned = true
		for index, date := range dates {
			deviation := math.Abs(spends[index]-median) / mad
			if deviation < anomalyMADMultiple {
				continue
			}
			kind := AnomalySpike
			word := "高"
			if spends[index] < median {
				kind, word = AnomalyDrop, "低"
			}
			anomalies = append(anomalies, MetricAnomaly{
				Date: date, Scope: "project", Metric: "spend_cents", Kind: kind,
				Observed: spends[index], Median: median, Deviation: deviation,
				Judgement: judgeAt(thresholds, ConfidenceDirectional,
					fmt.Sprintf("这一天全项目花费明显%s于窗口内的常态水平，先确认是投放动作还是数据问题，再解释素材表现。", word)),
			})
		}
	}

	// 素材级曝光突变。项目级花费能抓住「今天整体花超了」，但抓不住「某一条素材
	// 昨天被转了一次、曝光翻了四倍」——后者被其余素材的量摊平在总数里，而它恰恰是
	// 解释单条素材表现时最先要排除的那种事。所以两个尺度都要看。
	//
	// 看曝光而不是花费：素材级的花费还受出价和预算分配影响，曝光更接近「这条素材
	// 那天被推了多少」这件事本身。规则和项目级完全一致（MAD、3.5、至少 5 天），
	// 换个阈值只会让两处的「异常」不是同一个意思。
	for _, slice := range ordered {
		dates := slice.dates()
		if len(dates) < thresholds.MinAnomalyDays {
			scan.ShortAssets++
			continue
		}
		impressions := make([]float64, 0, len(dates))
		for _, date := range dates {
			impressions = append(impressions, float64(slice.byDate[date].Impressions))
		}
		assetMedian, assetMAD := medianAndMAD(impressions)
		if assetMAD <= 0 {
			// 常态一点波动都没有，说明这是被四舍五入或补录填出来的序列，
			// 拿它当基准算偏离只会得到一堆假阳性。这条也算「没查成」，
			// 但原因和天数不够不是一回事，分开记。
			scan.FlatAssets++
			continue
		}
		scan.ScannedAssets++
		// 每条素材每个方向只留偏离最大的那一天，其余的折进备注里。
		//
		// 这不是为了让列表短。整窗中位数对「台阶」不稳健：一条素材中途加了个投放
		// 计划、曝光整体抬了一级，台阶另一侧的每一天都会被判成「偏低」。逐条列出来
		// 会说成十几件事，而真相只有一件——量在中间变了一次。所以同方向多天命中时，
		// 报最极端的那天，并在备注里挑明这更像整体变化而不是当天出事。
		type worst struct {
			index     int
			deviation float64
			count     int
		}
		extremes := map[AnomalyKind]*worst{}
		for index := range dates {
			deviation := math.Abs(impressions[index]-assetMedian) / assetMAD
			if deviation < anomalyMADMultiple {
				continue
			}
			kind := AnomalySpike
			if impressions[index] < assetMedian {
				kind = AnomalyDrop
			}
			current, seen := extremes[kind]
			if !seen {
				extremes[kind] = &worst{index: index, deviation: deviation, count: 1}
				continue
			}
			current.count++
			if deviation > current.deviation {
				current.index, current.deviation = index, deviation
			}
		}
		for _, kind := range []AnomalyKind{AnomalySpike, AnomalyDrop} {
			hit, seen := extremes[kind]
			if !seen {
				continue
			}
			word := "高"
			if kind == AnomalyDrop {
				word = "低"
			}
			note := fmt.Sprintf("这一天这条素材的曝光明显%s于它自己的常态。这一天的表现不要和其他日子放在一起算平均，先弄清楚那天发生了什么。", word)
			if hit.count > 1 {
				note = fmt.Sprintf("窗口内有 %d 天曝光明显%s于常态，这里列的是最极端的一天。这么多天同向偏离，通常说明投放量在窗口中间整体变过一次（加减计划、改预算），而不是某一天出了事——先去核对投放动作，再谈素材表现。",
					hit.count, word)
			}
			anomalies = append(anomalies, MetricAnomaly{
				Date: dates[hit.index], Scope: "asset", AssetID: slice.assetID, AssetTitle: slice.title,
				Metric: "impressions", Kind: kind,
				Observed: impressions[hit.index], Median: assetMedian, Deviation: hit.deviation,
				Judgement: judgeAt(thresholds, ConfidenceDirectional, note),
			})
		}
	}

	// 断档：某个素材中间有连续没有数据的日子。它不一定是异常，但会让趋势和疲劳算错，
	// 所以要在异常里说出来。
	for _, slice := range ordered {
		gaps := missingDates(slice.dates())
		if len(gaps) == 0 {
			continue
		}
		anomalies = append(anomalies, MetricAnomaly{
			Date: gaps[0], Scope: "asset", AssetID: slice.assetID, AssetTitle: slice.title,
			Metric: "impressions", Kind: AnomalyGap,
			Judgement: judgeAt(thresholds, ConfidenceDirectional,
				fmt.Sprintf("这个素材在投放期间有 %d 天没有数据（从 %s 起）。断档期间是停投还是没回流，这里分不出来，但趋势和疲劳都会因此算偏。",
					len(gaps), gaps[0])),
		})
	}

	sort.Slice(anomalies, func(i, j int) bool {
		if anomalies[i].Deviation != anomalies[j].Deviation {
			return anomalies[i].Deviation > anomalies[j].Deviation
		}
		return anomalies[i].Date < anomalies[j].Date
	})
	return anomalies, scan
}

// medianAndMAD 返回中位数和中位数绝对偏差。用 MAD 而不是标准差：
// 一次大促就能把标准差撑大到之后什么都不算异常。
func medianAndMAD(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	median := percentile50(sorted)
	deviations := make([]float64, 0, len(values))
	for _, value := range values {
		deviations = append(deviations, math.Abs(value-median))
	}
	sort.Float64s(deviations)
	// 1.4826 把 MAD 换算成正态分布下的标准差当量，好让 3.5 这个阈值有通常的含义。
	return median, percentile50(deviations) * 1.4826
}

func percentile50(sorted []float64) float64 {
	count := len(sorted)
	if count == 0 {
		return 0
	}
	if count%2 == 1 {
		return sorted[count/2]
	}
	return (sorted[count/2-1] + sorted[count/2]) / 2
}

func missingDates(dates []string) []string {
	if len(dates) < 3 {
		return nil
	}
	start, err := parseDate(dates[0])
	if err != nil {
		return nil
	}
	end, err := parseDate(dates[len(dates)-1])
	if err != nil {
		return nil
	}
	present := map[string]struct{}{}
	for _, date := range dates {
		present[date] = struct{}{}
	}
	var gaps []string
	for cursor := start; !cursor.After(end); cursor = cursor.AddDate(0, 0, 1) {
		key := cursor.Format("2006-01-02")
		if _, ok := present[key]; !ok {
			gaps = append(gaps, key)
		}
	}
	return gaps
}

// --- 驱动因素（03 §7.5 模式归纳）---

// minDriverAssets 是一个特征取值至少要覆盖几个素材才值得比较。两个太少：
// 一个取值只对应一个素材时，比的是那个素材，不是那个特征。
const minDriverAssets = 2

func buildDrivers(ordered []*assetSlice, comparable bool, thresholds ResolvedThresholds) []FeatureDriver {
	thresholds = thresholds.orDefaults()
	byType := map[AssetType][]*assetSlice{}
	for _, slice := range ordered {
		if len(slice.features) == 0 {
			continue
		}
		byType[slice.kind] = append(byType[slice.kind], slice)
	}

	drivers := make([]FeatureDriver, 0, 16)
	for kind, group := range byType {
		if len(group) < thresholds.MinDriverAssets*2 {
			// 同类型素材不足 4 个时，任何分组都会退化成「一个对一个」。
			continue
		}
		keys := map[string]struct{}{}
		for _, slice := range group {
			for key := range slice.features {
				keys[key] = struct{}{}
			}
		}
		sortedKeys := make([]string, 0, len(keys))
		for key := range keys {
			sortedKeys = append(sortedKeys, key)
		}
		sort.Strings(sortedKeys)

		for _, key := range sortedKeys {
			buckets := map[string][]*assetSlice{}
			for _, slice := range group {
				value, ok := slice.attributableFeature(key)
				if !ok {
					continue
				}
				buckets[value] = append(buckets[value], slice)
			}
			if len(buckets) < 2 {
				// 全都一样的特征不是变量，没有对照组。
				continue
			}
			sortedValues := make([]string, 0, len(buckets))
			for value := range buckets {
				sortedValues = append(sortedValues, value)
			}
			sort.Strings(sortedValues)
			// 只有两个取值时，两行是同一个发现的正反两面：「A 比其余高 40%」和
			// 「其余比 A 低 40%」。两行都列出来会被读成两个独立发现，所以只留第一行；
			// 它的对照组就是另一个取值，信息一点没少。三个取值起，每一行才各自成立。
			if len(sortedValues) == 2 {
				sortedValues = sortedValues[:1]
			}
			for _, value := range sortedValues {
				inGroup := buckets[value]
				if len(inGroup) < thresholds.MinDriverAssets {
					continue
				}
				rest := make([]*assetSlice, 0, len(group))
				for _, slice := range group {
					if current, _ := slice.attributableFeature(key); current != value {
						rest = append(rest, slice)
					}
				}
				if len(rest) < thresholds.MinDriverAssets {
					continue
				}
				drivers = append(drivers, buildDriver(kind, key, value, inGroup, rest, comparable, thresholds))
			}
		}
	}
	sort.Slice(drivers, func(i, j int) bool {
		return driverRank(drivers[i]) < driverRank(drivers[j])
	})
	return drivers
}

func driverRank(driver FeatureDriver) int {
	switch {
	case !driver.IntervalsOverlap && len(driver.CovaryingFeatures) == 0:
		return 0
	case !driver.IntervalsOverlap:
		return 1
	default:
		return 2
	}
}

func buildDriver(kind AssetType, key, value string, inGroup, rest []*assetSlice, comparable bool,
	thresholds ResolvedThresholds) FeatureDriver {
	field := fieldOf(kind, key)
	driver := FeatureDriver{
		AssetType: kind, Key: key, Label: field.Label, Group: field.Group, Value: value,
		Assets: len(inGroup), RestAssets: len(rest),
	}
	// 组间判定走 group_compare.go，和实验中心共用一套。驱动因素是事后按特征凑的分组，
	// 所以 PreRegistered 为 false——同样的数字，这里只能说到「相关」。
	comparison := compareGroups(groupCompareInput{
		InGroup:      inGroup,
		Rest:         rest,
		CovaryKey:    key,
		SubjectLabel: field.Label,
		Comparable:   comparable,
		Thresholds:   thresholds,
	})
	driver.Counts, driver.RestCounts = comparison.Counts, comparison.RestCounts
	driver.Rates, driver.RestRates = comparison.Rates, comparison.RestRates
	driver.CTRInterval, driver.RestCTRInterval = comparison.CTRInterval, comparison.RestCTRInterval
	driver.IntervalsOverlap = comparison.IntervalsOverlap
	driver.CTRLift = comparison.CTRLift
	driver.CovaryingFeatures = comparison.CovaryingFeatures
	// 整块搬 Judgement 而不是逐字段拷：档位、理由、升级通道要么一起来自组间判定，
	// 要么就会出现「档位是这次算的、理由是上次的」。
	driver.Judgement = comparison.Judgement
	return driver
}

// covaryingFeatures 找出那些「组内整齐一致、且与组外整齐不同」的其他特征。
// 它们和目标特征绑在一起变化，是这条驱动因素结论最直接的混杂来源。
func covaryingFeatures(target string, inGroup, rest []*assetSlice) []string {
	keys := map[string]struct{}{}
	for _, slice := range inGroup {
		for key := range slice.features {
			keys[key] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		if key != target {
			sorted = append(sorted, key)
		}
	}
	sort.Strings(sorted)

	var found []string
	for _, key := range sorted {
		groupValue, uniform := uniformValue(inGroup, key)
		if !uniform {
			continue
		}
		differs := true
		for _, slice := range rest {
			value, ok := slice.attributableFeature(key)
			if !ok || value == groupValue {
				differs = false
				break
			}
		}
		if differs {
			found = append(found, fieldOf(inGroup[0].kind, key).Label)
		}
	}
	return found
}

func uniformValue(slices []*assetSlice, key string) (string, bool) {
	var value string
	for index, slice := range slices {
		current, ok := slice.attributableFeature(key)
		if !ok {
			return "", false
		}
		if index == 0 {
			value = current
			continue
		}
		if current != value {
			return "", false
		}
	}
	return value, value != ""
}
