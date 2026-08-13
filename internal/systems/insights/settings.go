package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 系统设置（03 §78 与 19 §289 的五个视图：样本门槛、观察窗口、通知、确认权限、报告模板；
// 20 §121「**MVP 可采用保守默认值**」「单列分组表单，重要阈值显示影响说明和默认推荐；
// 不使用仪表盘布局」；22 §239 记的问题是「缺少实际阈值影响说明」）。
//
// 这一页首先要做到的不是「让人改阈值」，而是**让人知道现在生效的阈值是多少、
// 调它会动到什么**。它们决定了页面上什么时候说「充分」、什么时候说「样本不足」、
// 什么时候干脆不给结论——一个看不见的阈值和一个错的阈值，在使用者那里是同一种东西。
//
// 在此之上，决定三档结论的那七个现在**可以改**（thresholds.go）：改动按组织生效、
// 只增版本，每条结论都盖着它用的那一版号。其余的值仍然是代码常量，一律只读——
// 它们里面有一批是保护性上限（导入行数、最长窗口），把它们做成可配置等于把
// 「防呆」做成了「可关闭」。
//
// 不管可写不可写，页面上的数字都取自**正在生效的那份**（ResolvedThresholds，
// 没人调过时逐格退回代码常量），而不是另抄一份。抄一份迟早会和代码对不上，
// 那时候这一页就从「说明」变成了「误导」——比不做更糟。

// SettingGroupState 说明这一组设置现在是什么处境。
type SettingGroupState string

const (
	// SettingInEffect 这一组的值现在真的在生效，页面上的判定就是按它们做的。
	SettingInEffect SettingGroupState = "in_effect"
	// SettingNotBuilt 这一组还没有任何东西。列出来是为了不让人以为它在默默生效。
	SettingNotBuilt SettingGroupState = "not_built"
)

// SettingItem 是一条设置。20 §121 要求「重要阈值显示影响说明和默认推荐」，
// 所以 Effect 和 Recommended 不是可选的注释，是这一页存在的理由。
type SettingItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Value 是现在生效的值，已经格式化成人能读的样子（含单位）。
	Value string `json:"value"`
	// Effect 说明调大调小会发生什么。写「会影响置信度」没有意义，要写清楚
	// 哪一句话会从「有结论」变成「没结论」。
	Effect string `json:"effect"`
	// Recommended 是默认推荐值。和 Value 一致时前端不必强调，不一致时要提示。
	//
	// 这里一律写字面量，不从常量算出来。算出来它就永远等于 Value，「推荐」这个字段
	// 也就没有独立含义了；写死之后，谁改了常量而没回来说明理由，测试会当场拦下。
	Recommended string `json:"recommended"`
	// Deviates 表示当前值偏离了推荐值，页面据此提示「有人调过它」。
	//
	// 由各组自己判定，而不是让前端拿 Value 和 Recommended 比字符串：确认权限那组的
	// 「当前值」是这个权限管到哪些操作、「推荐」是该发给谁，本来就是两件事，
	// 字面永远不同——前端一比就会给每一条都打上「被调过」，那是凭空造出来的告警。
	Deviates bool `json:"deviates"`
	// Source 是这个值在代码里的位置，方便直接翻过去看上下文。
	Source string `json:"source"`
	// Basis 是文档依据。没有依据的阈值要显式写「无文档依据」，那本身就是个待办。
	Basis string `json:"basis"`
	// EditableKey 非空表示这一条可以改，值是它在保存请求 values 里的键名。
	//
	// 可写与否标在**条**上而不是标在组上：样本门槛那一组里，五个判定阈值可以改，
	// 「异常判定倍数」不能改（它是统计口径，不是业务标准）；观察窗口那一组里，
	// 对比条数和体检窗口可以改，导入行数上限不能改。标在组上的话，这两组只能
	// 整组可写或整组只读——要么把防呆开关暴露出去，要么把该调的锁死。
	EditableKey string `json:"editable_key,omitempty"`
}

// SettingsView 是「设置」入口下的四组。
//
// 一个 SettingGroup 对应设置页上的一段，而不再是一个二级视图：判定阈值那一屏
// 由样本门槛 + 观察窗口两组拼出来（窗口天数本来就是判定标准的一部分），
// 名词表并进变量字典。
type SettingsView string

const (
	ViewThresholds SettingsView = "thresholds"
	ViewHealth     SettingsView = "health"
	ViewDictionary SettingsView = "dictionary"
	ViewPermission SettingsView = "permission"
	// ViewNone 表示这一组不在设置页上单开一段。
	//
	// 通知和报告模板都是这种：一张表都没建、一个开关都不生效，做成两个空抽屉
	// 只会让人以为设得上。定义原样留着、状态照旧写「没做」，等真做了再进来。
	ViewNone SettingsView = ""
)

// SettingGroup 是设置页上的一段。
type SettingGroup struct {
	Key   string            `json:"key"`
	Label string            `json:"label"`
	State SettingGroupState `json:"state"`
	// View 是这一组落在四组里的哪一组。为空表示不展示。
	View SettingsView `json:"view"`
	// Editable 表示这一组里有格子可以改。由 Items 里的 EditableKey 汇总而来，
	// 不单独维护——两处各写一份，迟早出现「组说可写、每一格都只读」。
	Editable bool `json:"editable"`
	// Summary 一句话说明这一组管什么。
	Summary string `json:"summary"`
	// Missing 只在 State 为 not_built 时有内容：缺的到底是什么、现在怎么替代。
	Missing []string      `json:"missing"`
	Items   []SettingItem `json:"items"`
}

// InsightSettings 是整页的返回值。
type InsightSettings struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Editable 表示这一页上有东西可以改。逐格的可写与否看 SettingItem.EditableKey——
	// 前端只在有 EditableKey 的那些格子上渲染输入框，其余照旧只读。
	// 渲染一个改不动的输入框，比直接说「这里改不了」更让人恼火。
	Editable bool `json:"editable"`
	// EditableNote 说明哪些能改、改了会怎样、哪些不能改以及为什么。
	EditableNote string `json:"editable_note"`
	// ProjectScoped 恒为 false：这些值对整个部署生效，不分 Project。
	// 路径上带 project_id 只是为了沿用同一套鉴权，不代表值会因项目而不同。
	ProjectScoped bool           `json:"project_scoped"`
	Groups        []SettingGroup `json:"groups"`
}

// GetInsightSettings 汇总当前生效的阈值与规则。
//
// 数值取自**正在生效的那份**：有人调过就是调过的值，没人调过就逐格退回代码常量。
// 要的是「判定按什么算，页面上就写什么」——中间隔一层抄写，就会有对不上的那一天。
func (s Service) GetInsightSettings(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID) (InsightSettings, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return InsightSettings{}, err
	}
	thresholds := s.currentThresholds(ctx, actor.OrganizationID)
	groups := []SettingGroup{
		sampleThresholdSettings(thresholds),
		windowSettings(thresholds),
		notificationSettings(),
		confirmationSettings(),
		reportTemplateSettings(),
		glossarySettings(),
	}
	editable := false
	for index := range groups {
		groups[index].Editable = groupIsEditable(groups[index])
		editable = editable || groups[index].Editable
	}
	return InsightSettings{
		GeneratedAt:   s.now(),
		Editable:      editable,
		ProjectScoped: false,
		EditableNote: "判定阈值现在可以改，改动按组织生效、只增版本，每条结论都记下它用的是哪一版；" +
			"改完之后判出来的结论按新的算，之前的结论保持原样。" +
			"没标可改的那些是保护性上限（导入行数、最长窗口这类），它们防的是系统被撑爆，" +
			"不是判定标准——做成可配置等于把防呆做成了可关闭。" +
			"行业模板级的阈值（同一组织下不同行业用不同门槛）没有做——" +
			"需要先有一份行业分类口径，那是变量字典的事。",
		Groups: groups,
	}, nil
}

func groupIsEditable(group SettingGroup) bool {
	for _, item := range group.Items {
		if item.EditableKey != "" {
			return true
		}
	}
	return false
}

// --- 样本门槛 ---

func sampleThresholdSettings(thresholds ResolvedThresholds) SettingGroup {
	return SettingGroup{
		Key: "sample", Label: "样本门槛", State: SettingInEffect, View: ViewThresholds,
		Summary: "决定一个数字什么时候可以被当成结论。低于门槛时系统说「样本不足」，而不是给一个看起来很确定的比率。",
		Missing: []string{},
		Items: markDeviations([]SettingItem{{
			Key: "sufficient_sample_impressions", Label: "充分样本（曝光）",
			EditableKey: "sufficient_impressions",
			Value:       fmt.Sprintf("%d 次曝光", thresholds.SufficientImpressions),
			Effect: "达到这个量才把置信标成「充分」。调低会让小样本的随机波动被当成结论——" +
				"两条素材的 CTR 差一倍，在几百次曝光下是常事，和素材本身没关系。",
			Recommended: "10000 次曝光",
			Source:      "internal/systems/insights/connectors.go:267",
			Basis:       "03 §17.3 列为待确认，现取保守值",
		}, {
			Key: "directional_sample_impressions", Label: "方向性样本（曝光）",
			EditableKey: "directional_impressions",
			Value:       fmt.Sprintf("%d 次曝光", thresholds.DirectionalImpressions),
			Effect: "低于这个量连方向都不给，直接判「样本不足」。介于两者之间时给「方向性」，" +
				"意思是可以据此决定下一步测什么，但不能写进结论。",
			Recommended: "1000 次曝光",
			Source:      "internal/systems/insights/connectors.go:268",
			Basis:       "03 §17.3 列为待确认，现取保守值",
		}, {
			Key: "min_driver_assets", Label: "驱动因素每组最少素材数",
			EditableKey: "min_driver_assets",
			Value:       fmt.Sprintf("%d 条素材", thresholds.MinDriverAssets),
			Effect: "某个特征取值下不足这么多条素材，就不参与驱动因素比较。" +
				"设成 1 会让「某一条素材恰好表现好」被读成「这个取值有效」。",
			Recommended: "2 条素材",
			Source:      "internal/systems/insights/performance.go:952",
			Basis:       "03 §7.5 驱动因素为相关性分析",
		}, {
			Key: "min_trend_days", Label: "趋势与疲劳最少天数",
			EditableKey: "min_trend_days",
			Value:       fmt.Sprintf("%d 天", thresholds.MinTrendDays),
			Effect: "窗口内有数据的天数少于这个，趋势判「看不出走势」、疲劳不给结论。" +
				"疲劳本来就是个趋势判断，两三天看不出趋势，只能看出波动。" +
				fmt.Sprintf("不能低于 %d 天：再低就等于把「拒绝下结论」这条规则关掉。", floorTrendDays),
			Recommended: "4 天",
			Source:      "internal/systems/insights/performance.go:213",
			Basis:       "03 §7.4 疲劳识别",
		}, {
			Key: "min_anomaly_days", Label: "异常检测最少天数",
			EditableKey: "min_anomaly_days",
			Value:       fmt.Sprintf("%d 天", thresholds.MinAnomalyDays),
			Effect: "少于这么多天就没有「常态」可言，此时算出来的异常全是噪声，所以一条都不报。" +
				fmt.Sprintf("不能低于 %d 天，理由同上。", floorAnomalyDays),
			Recommended: "5 天",
			Source:      "internal/systems/insights/performance.go:216",
			Basis:       "03 §7.4 异常识别",
		}, {
			Key: "anomaly_mad_multiple", Label: "异常判定倍数（MAD）",
			Value: fmt.Sprintf("%.1f 倍", anomalyMADMultiple),
			Effect: "偏离中位数超过这么多个 MAD 才算异常。用 MAD 不用标准差，是因为标准差" +
				"会被它想找的那个异常点自己抬高，越异常越不容易被发现。调低会让正常波动天天报警。",
			Recommended: "3.5 倍",
			Source:      "internal/systems/insights/performance.go:219",
			Basis:       "无文档指定值，取统计惯例",
		}, {
			Key: "min_evaluation_samples", Label: "Skill 准确率最少复核数",
			Value: fmt.Sprintf("%d 条已复核特征", minEvaluationSamples),
			Effect: "低于这个数不显示准确率。一次复核算 100%、两次算 50%，这种数字放在治理面上" +
				"比没有更糟：它看上去像个指标，实际只是随机数。",
			Recommended: "10 条已复核特征",
			Source:      "internal/systems/insights/operations.go:354",
			Basis:       "20 §4.1 支持算法升级与回归",
		}, {
			Key: "mapping_rate_blocking_below", Label: "花费映射率 · 阻断线",
			Value: fmt.Sprintf("%.0f%%", mappingRateBlockingBelow*100),
			Effect: "能归到素材上的花费低于这个比例，整个 Project 暂停强结论——" +
				"大半的钱不知道花在哪条素材上时，任何「这条素材更好」都是猜的。",
			Recommended: "80%",
			Source:      "internal/systems/insights/quality.go:264",
			Basis:       "doc10 §7、§12.6 对账要求",
		}, {
			Key: "mapping_rate_warning_below", Label: "花费映射率 · 警告线",
			Value:       fmt.Sprintf("%.0f%%", mappingRateWarningBelow*100),
			Effect:      "低于这个比例仍可给结论，但每次都要带着这条一起看。",
			Recommended: "95%",
			Source:      "internal/systems/insights/quality.go:265",
			Basis:       "doc10 §7、§12.6 对账要求",
		}}),
	}
}

// markDeviations 给数值阈值标出「当前值和推荐对不上」。只对可比的项用：
// 两边都是同一个东西的两种取值时，字面比对才有意义。
func markDeviations(items []SettingItem) []SettingItem {
	for index := range items {
		items[index].Deviates = items[index].Value != items[index].Recommended
	}
	return items
}

// --- 观察窗口 ---

func windowSettings(thresholds ResolvedThresholds) SettingGroup {
	return SettingGroup{
		Key: "window", Label: "观察窗口", State: SettingInEffect, View: ViewThresholds,
		Summary: "一次分析回看多久、一次请求最多处理多少。这些是上限不是默认值：调大不会让结论更准，只会让请求更慢。",
		Missing: []string{},
		Items: markDeviations([]SettingItem{{
			Key: "max_window_days", Label: "单次分析最长窗口",
			Value: fmt.Sprintf("%d 天", maxWindowDays),
			Effect: "超过这个天数的请求直接拒绝。上限存在的理由是保护数据库，不是业务判断——" +
				"但窗口拉得越长，跨口径变更的风险越大（改过归因窗口的两段数据不能直接并排）。",
			Recommended: "400 天",
			Source:      "internal/systems/insights/connectors.go:1110",
			Basis:       "无文档指定值，按一年零余量取",
		}, {
			Key: "prelaunch_quality_window_days", Label: "投前洞察数据质量回看窗口",
			EditableKey: "quality_window_days",
			Value:       fmt.Sprintf("%d 天", thresholds.QualityWindowDays),
			Effect: "投前洞察判断「引用的经验背后有没有数据问题」时回看这么多天。" +
				"调短会漏掉更早发生但还没修的问题，调长会把早已修复的旧问题一直挂在提示里。",
			Recommended: "30 天",
			Source:      "internal/systems/insights/prelaunch.go:287",
			Basis:       "03 §8 投前证据需标注数据基础",
		}, {
			Key: "freshness_delayed_after_days", Label: "数据延迟判定",
			Value: fmt.Sprintf("落后超过 %d 天", freshnessDelayedAfterDays),
			Effect: "数据截止时间落后当天超过这个天数就标「延迟」，并降低整体置信。" +
				"多数平台的报表本身就有 1 天延迟，设成 1 会天天报警。",
			Recommended: "落后超过 2 天",
			Source:      "internal/systems/insights/connectors.go:269",
			Basis:       "doc10 §11 任何洞察必须展示数据截止时间",
		}, {
			Key: "max_comparison_assets", Label: "素材对比一次最多几条",
			EditableKey: "max_comparison_assets",
			Value:       fmt.Sprintf("%d 条素材", thresholds.MaxComparisonAssets),
			Effect: "超出的按花费截断。这条更多是可读性约束：一次并排二十条，人看不出哪两条只差一个变量，" +
				"而「只差一个变量」正是对比能不能归因的前提。",
			Recommended: "8 条素材",
			Source:      "internal/systems/insights/performance.go:206",
			Basis:       "03 §7.3 变量单一时分析相对表现",
		}, {
			Key: "max_import_rows", Label: "单次报表导入行数上限",
			Value: fmt.Sprintf("%d 行", maxImportRows),
			Effect: "超出的整批拒绝，不做部分导入——半批数据入库比不入库更难查，" +
				"因为指标看上去是完整的，只是少了一截。",
			Recommended: "5000 行",
			Source:      "internal/systems/insights/connectors.go:615",
			Basis:       "无文档指定值，按单请求处理时间取",
		}, {
			Key: "max_feature_values_per_field", Label: "治理面每个字段最多列几个取值",
			Value:       fmt.Sprintf("%d 个取值", maxFeatureValuesPerField),
			Effect:      "只影响能力运营页上的展示，不影响任何判定。超出的折进「共 N 种取值」里。",
			Recommended: "12 个取值",
			Source:      "internal/systems/insights/operations.go:356",
			Basis:       "展示约束，无文档依据",
		}}),
	}
}

// --- 通知 ---

func notificationSettings() SettingGroup {
	return SettingGroup{
		Key: "notification", Label: "通知", State: SettingNotBuilt, View: ViewNone,
		Summary: "还没有任何通知机制。所有需要人注意的事，现在都只能靠人主动打开对应页面才看得到。",
		Missing: []string{
			"没有通知对象，也没有触发规则。代码里没有一处会主动往外发消息。",
			"三件本该主动提醒的事，现在都是被动的：数据源延迟到超过阈值、AI 提出来的特征堆在待复核里没人看、已确认的经验到了复审期。",
			"其中最要紧的是待复核堆积——人工复核是这套系统唯一的质量闸门，没人看就等于没有闸门，而它现在不会提醒任何人。",
		},
		Items: []SettingItem{},
	}
}

// --- 确认权限 ---

func confirmationSettings() SettingGroup {
	return SettingGroup{
		Key: "confirmation", Label: "确认权限", State: SettingInEffect, View: ViewPermission,
		Summary: "谁能把一条结论从「机器说的」变成「我们认的」。这三个权限已经在每个接口上生效，" +
			"但目前只能按组织角色授予：角色定了，三个权限就跟着定了，没法单独给某个人多开或少开一档。",
		Missing: []string{},
		Items: []SettingItem{{
			// 角色对照放在最前面。三条权限的说明再清楚，人还是会问「那我到底有没有」——
			// 这一行就是答案，而且它是唯一一处能让人看出「member 没有确认权」的地方。
			Key: "role_mapping", Label: "哪个角色拿到哪几档",
			Value: "owner / admin：读取 + 编辑 + 确认；member：读取 + 编辑；auditor：只有读取",
			Effect: "角色改在组织成员管理里，不在这一页。要让某个人能确认结论，把他的角色提到 admin——" +
				"按人单授权还没有，所以「只给某个 member 开确认权」这件事现在做不到。",
			Recommended: "确认这一档跟着 admin 走，人数宜少",
			Source:      "internal/platform/identity/session.go ScopesForOrganizationRole",
			Basis:       "doc22 §6.2 按角色控制治理入口",
		}, {
			Key: "insights.read", Label: "读取（insights.read）",
			Value:       "所有查询接口",
			Effect:      "没有它连页面都打不开。素材、指标、质量、洞察、经验的全部只读接口都要它。",
			Recommended: "分析相关的所有人",
			Source:      "internal/systems/insights/service.go:17",
			Basis:       "doc22 §6.2 按角色控制治理入口",
		}, {
			Key: "insights.write", Label: "编辑（insights.write）",
			Value: "登记素材、导入报表、填写特征、发起 AI 提取、提请复审",
			Effect: "能改数据，但改不动「已确认」这个状态。发起 AI 提取也在这一档——" +
				"提取只写待确认的那一层，不产生结论，所以不需要确认权限。",
			Recommended: "分析师与运营",
			Source:      "internal/systems/insights/service.go:18",
			Basis:       "03 §11.1 状态机",
		}, {
			Key: "insights.confirm", Label: "确认（insights.confirm）",
			Value: "确认分析结论、确认复盘报告、确认与驳回经验、下线素材与经验",
			Effect: "这是唯一能让一条结论进入「可被引用」状态的权限。分开授予的意义在于：" +
				"提特征的人和认结论的人可以不是同一个人。",
			Recommended: "对结论负责的人，人数宜少",
			Source:      "internal/systems/insights/service.go:19",
			Basis:       "03 §11.1、AM-006",
		}, {
			Key: "confirm_requires_full_review", Label: "确认前必须逐项复核",
			Value: "强制开启，不可关闭",
			Effect: "只要还有一项 AI 特征没被人认可或推翻，确认动作就会被拒绝。" +
				"所以「已确认」永远意味着有人看过每一个字段，而不是有人点过一次按钮。",
			Recommended: "保持强制",
			Source:      "internal/systems/insights/assets.go:927 ConfirmAssetAnalysis",
			Basis:       "AM-006、doc09 §8",
		}},
	}
}

// --- 报告模板 ---

func reportTemplateSettings() SettingGroup {
	return SettingGroup{
		Key: "report_template", Label: "报告模板", State: SettingNotBuilt, View: ViewNone,
		Summary: "还没有模板对象。现在只有一种报告：一轮投放的复盘，它的结构写死在代码里。",
		Missing: []string{
			"没有模板、没有调度、没有导出产物三类对象，所以「周期报告」「自定义报告」「版本与导出」都无从配置。",
			"03 §9 的功能需求表里，复盘只有 AM-015 任务复盘一条有需求编号，其余几项写在导航表里但没有需求支撑（基线文档冲突 10，待你确认是补需求还是删视图）。",
			"复盘报告本身是能用的：投后分析结论 → 复盘 → 确认后沉淀成经验，这条链路已经跑通，只是它的版式不可配置。",
		},
		Items: []SettingItem{},
	}
}

// --- 名词表 ---

// glossarySettings 把名词表摆到设置页上。
//
// 它和这一页其余几组不太一样：那些是阈值，这是词。放在一起是因为它们的性质相同——
// 都是「现在生效的规则，你改不了，但你有权知道」。名词表比阈值更容易悄悄失效：
// 阈值写错了数字会不对，词用错了页面照样能跑，只是每个人读到的意思略有偏差，
// 半年后谁也说不清「强结论」和「能归因」当初是不是一回事。
func glossarySettings() SettingGroup {
	items := make([]SettingItem, 0, len(insightGlossary))
	for _, term := range insightGlossary {
		effect := "这个词没有被取代的旧说法。"
		if len(term.Avoid) > 0 {
			effect = "不要再叫：" + strings.Join(term.Avoid, "、") + "。"
		}
		items = append(items, SettingItem{
			Key: term.Term, Label: term.Term,
			Value:       term.Means,
			Effect:      effect,
			Recommended: "全模块统一用这个词",
			Source:      "internal/systems/insights/glossary.go",
			Basis:       "2026-08-04 素材洞察重构设计 §3.6",
		})
	}
	return SettingGroup{
		Key: "glossary", Label: "名词表", State: SettingInEffect, View: ViewDictionary,
		Summary: "这个模块里每件事只有一个叫法。表上「不要再叫」的说法不许再出现在任何页面文案里，有测试拦着。",
		Missing: []string{},
		Items:   items,
	}
}
