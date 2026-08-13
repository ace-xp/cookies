# 素材洞察 · 第 1 期地基 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「三档结论」做成全模块唯一的判定口径，并把它、名词表、变量三分类、五个共用前端件一次性铺到位，让后面四个入口的计划都建在同一块地基上。

**Architecture:** 后端已经有一套四值的 `ConfidenceLevel`（充分 / 方向性 / 样本不足 / 存在混杂），它是统计口径，保留不动；新增一层三值的 `Verdict`（✅能归因 / 👁只是观察 / ❓算不出来），它是行动口径，四值往三值的收敛规则只写在一个函数里。所有对外结构体把散落的 `Confidence + Note` 字段对替换成内嵌的 `Judgement`，靠 JSON 契约测试保证「凡是有 confidence 的地方就必须有 verdict」。前端对称地做一个 `verdict.ts` 和五个共用件，投后分析页第一个接上去当样板。

**Tech Stack:** Go 1.22+（`net/http` ServeMux 路由、标准库 `testing`）、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`（node:test）跑前端逻辑测试、OpenAPI 3 契约文件 `api/openapi/insights-v1.yaml`。

## Global Constraints

- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- 后端包路径 `github.com/shikanon/cookies/internal/systems/insights`，前端权威目录是 `src/`（不是 `web/`）。
- 三档收敛规则只允许存在一处实现：`ConfidenceLevel.Verdict()`。任何其他地方写 `switch confidence` 决定给人看什么档位，都算违反本计划。
- 四值 `ConfidenceLevel` 与 `VariantVerdict` 枚举的既有取值和 JSON 值一个都不许改——它们已经进了 OpenAPI 契约和前端。
- 已有常量名 `VerdictAttributable / VerdictDirectional / VerdictConfounded / VerdictLowSample / VerdictNoFeatures` 属于 `VariantVerdict`，新枚举不得复用这些名字。
- 三档的固定文案：`explained` = 「能归因」`✅`，`observed` = 「只是观察」`👁`，`unclear` = 「算不出来」`❓`。两条升级通道：`observed` → `experiment`（做个实验），`unclear` → `similar_assets`（找相似素材）。
- 归因只认 `derived`（客观可测）和 `human`（人工标注）两类变量；`ai`（模型推断）可以展示，不得进入任何归因结论。
- 每个任务结束必须能独立跑通 `go build ./... && go test ./internal/systems/insights/...` 和 `npm run build`。
- 阈值 `sufficientSampleImpressions = 10000` / `directionalSampleImpressions = 1000` / `minTrendDays = 4` / `minAnomalyDays = 5` / `anomalyMADMultiple = 3.5` 本期不改数值，只改「谁能读到它们」。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `internal/systems/insights/verdict.go` | 三档枚举、4→3 收敛、`Judgement`、屏级取最弱 | 新建 |
| `internal/systems/insights/verdict_test.go` | 三档的全部行为约束 | 新建 |
| `internal/systems/insights/glossary.go` | 名词表：每个词「叫什么 / 是什么 / 不要再叫」 | 新建 |
| `internal/systems/insights/glossary_test.go` | 禁用别名不得出现在任何对外 Label | 新建 |
| `internal/systems/insights/connectors.go` | `allConfidenceLevels` 清单；`MetricOverview` / `AssetPerformance` 内嵌 `Judgement` | 改 |
| `internal/systems/insights/group_compare.go` | `GroupComparison` 内嵌 `Judgement` | 改 |
| `internal/systems/insights/performance.go` | 五个视图结构体内嵌 `Judgement`；`assetSlice.features` 带上来源；归因只认 derived+human | 改 |
| `internal/systems/insights/assets.go` | `FeatureSource` 增加 `derived` | 改 |
| `internal/systems/insights/settings.go` | 设置页暴露名词表分组 | 改 |
| `internal/systems/insights/judgement_contract_test.go` | JSON 契约：有 confidence 就必须有 verdict | 新建 |
| `api/openapi/insights-v1.yaml` | 新字段进契约 | 改 |
| `src/data/verdict.ts` | 前端侧三档定义与映射（与后端一一对应） | 新建 |
| `test/insight-verdict.test.ts` | 前端映射与后端枚举对齐 | 新建 |
| `src/components/insight/shared/VerdictBadge.tsx` | 三档徽章 | 新建 |
| `src/components/insight/shared/EvidenceDrawer.tsx` | 证据抽屉 | 新建 |
| `src/components/insight/shared/HowItWasComputed.tsx` | 「怎么算的」弹层 | 新建 |
| `src/components/insight/shared/NotEnoughSample.tsx` | 样本不足占位 | 新建 |
| `src/components/insight/shared/PinFindingButton.tsx` | 「记一笔」按钮（本期只出壳，写入在 P1） | 新建 |
| `src/components/insight/shared/index.ts` | 共用件出口 | 新建 |
| `src/components/PostLaunchAnalysisPage.tsx` | 换掉本地 `confidenceLabels`，改用共用件 | 改 |
| `src/components/PostLaunchOverviewPage.tsx` | `confidence_note` → `note` | 改 |
| `src/data/api.ts` | 前端类型跟上新字段 | 改 |

---

### Task 1: 三档判定类型与 4→3 收敛

**Files:**
- Create: `internal/systems/insights/verdict.go`
- Create: `internal/systems/insights/verdict_test.go`
- Modify: `internal/systems/insights/connectors.go:246`（在 `ConfidenceLevel` const 块之后追加清单变量）

**Interfaces:**
- Consumes: 既有 `ConfidenceLevel` 四值枚举（`connectors.go:240-247`）。
- Produces:
  - `type Verdict string`，取值 `VerdictExplained="explained"` / `VerdictObserved="observed"` / `VerdictUnclear="unclear"`
  - `func (Verdict) Label() string`、`func (Verdict) Icon() string`、`func (Verdict) Upgrade() UpgradePath`
  - `type UpgradePath string`，取值 `UpgradeNone=""` / `UpgradeExperiment="experiment"` / `UpgradeSimilar="similar_assets"`；`func (UpgradePath) Label() string`
  - `func (ConfidenceLevel) Verdict() Verdict` —— 全模块唯一收敛点
  - `type Judgement struct{ Confidence ConfidenceLevel; Verdict Verdict; VerdictLabel string; Upgrade UpgradePath; Note string }`，JSON 键 `confidence` / `verdict` / `verdict_label` / `upgrade,omitempty` / `note`
  - `func judge(ConfidenceLevel, string) Judgement`（包内）
  - `func weakestVerdict(...Verdict) Verdict`（包内）
  - `var allConfidenceLevels []ConfidenceLevel`（包内）

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/verdict_test.go`：

```go
package insights

import "testing"

// 四个统计档位往三个行动档位收敛的规则，是整个模块的中轴。这张表写死在测试里，
// 改代码改不动它——要改必须先改这张表，那时候就得解释为什么。
func TestConfidenceMapsToExactlyThreeVerdicts(t *testing.T) {
	t.Parallel()

	want := map[ConfidenceLevel]Verdict{
		ConfidenceSufficient:  VerdictExplained,
		ConfidenceDirectional: VerdictObserved,
		ConfidenceConfounded:  VerdictObserved,
		ConfidenceLowSample:   VerdictUnclear,
	}
	for level, expected := range want {
		if got := level.Verdict(); got != expected {
			t.Errorf("%s 收敛成 %s，期望 %s", level, got, expected)
		}
	}
	// 新加一个 ConfidenceLevel 却忘了给它三档归属，会静默落到默认分支，
	// 屏幕上就多出一批莫名其妙的「算不出来」。这一条拦住它。
	if len(want) != len(allConfidenceLevels) {
		t.Fatalf("ConfidenceLevel 有 %d 个取值，但只有 %d 个有三档归属", len(allConfidenceLevels), len(want))
	}
}

// 三档不是终点，是「现在还缺什么」。缺的东西不一样，下一步就不一样。
func TestEachVerdictKnowsItsOnlyUpgradePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verdict Verdict
		upgrade UpgradePath
	}{
		{VerdictExplained, UpgradeNone},
		{VerdictObserved, UpgradeExperiment},
		{VerdictUnclear, UpgradeSimilar},
	}
	for _, c := range cases {
		if got := c.verdict.Upgrade(); got != c.upgrade {
			t.Errorf("%s 的升级通道是 %q，期望 %q", c.verdict, got, c.upgrade)
		}
	}
}

func TestVerdictLabelAndIconAreFixed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verdict Verdict
		label   string
		icon    string
	}{
		{VerdictExplained, "能归因", "✅"},
		{VerdictObserved, "只是观察", "👁"},
		{VerdictUnclear, "算不出来", "❓"},
	}
	for _, c := range cases {
		if got := c.verdict.Label(); got != c.label {
			t.Errorf("%s 的文案是 %q，期望 %q", c.verdict, got, c.label)
		}
		if got := c.verdict.Icon(); got != c.icon {
			t.Errorf("%s 的图标是 %q，期望 %q", c.verdict, got, c.icon)
		}
	}
}

// 一屏上有一条算不出来，这屏就不能整体标成能归因。
func TestScreenVerdictTakesTheWeakestItem(t *testing.T) {
	t.Parallel()

	if got := weakestVerdict(VerdictExplained, VerdictExplained); got != VerdictExplained {
		t.Errorf("全是能归因时屏级应为能归因，得到 %s", got)
	}
	if got := weakestVerdict(VerdictExplained, VerdictObserved); got != VerdictObserved {
		t.Errorf("混了只是观察时屏级应为只是观察，得到 %s", got)
	}
	if got := weakestVerdict(VerdictExplained, VerdictUnclear, VerdictObserved); got != VerdictUnclear {
		t.Errorf("混了算不出来时屏级应为算不出来，得到 %s", got)
	}
	// 一条结论都没有，是「算不出来」，不是「能归因」。空屏默认成 ✅ 会让
	// 没有数据的页面显得比有数据的页面更可信。
	if got := weakestVerdict(); got != VerdictUnclear {
		t.Errorf("空输入应为算不出来，得到 %s", got)
	}
}

func TestJudgeFillsEveryFieldFromTheConfidence(t *testing.T) {
	t.Parallel()

	got := judge(ConfidenceConfounded, "两组素材在时长上也整齐不同。")
	if got.Confidence != ConfidenceConfounded {
		t.Errorf("Confidence 被改写成了 %s", got.Confidence)
	}
	if got.Verdict != VerdictObserved {
		t.Errorf("Verdict 是 %s，期望 %s", got.Verdict, VerdictObserved)
	}
	if got.VerdictLabel != "只是观察" {
		t.Errorf("VerdictLabel 是 %q", got.VerdictLabel)
	}
	if got.Upgrade != UpgradeExperiment {
		t.Errorf("Upgrade 是 %q，期望 %q", got.Upgrade, UpgradeExperiment)
	}
	if got.Note != "两组素材在时长上也整齐不同。" {
		t.Errorf("Note 被改写成了 %q", got.Note)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestConfidenceMaps|TestEachVerdict|TestVerdictLabel|TestScreenVerdict|TestJudgeFills' -v
```

Expected: 编译失败，`undefined: VerdictExplained`、`undefined: allConfidenceLevels`、`undefined: judge` 等。

- [ ] **Step 3: 在 connectors.go 追加枚举清单**

在 `internal/systems/insights/connectors.go` 的 `ConfidenceLevel` const 块（结束于 `:247` 的 `)`）正下方插入：

```go
// allConfidenceLevels 是这个枚举的完整清单。verdict.go 把四个档位收敛成三个，
// 收敛表写在测试里；有了这份清单，新增档位却忘了给它三档归属会直接测试失败，
// 而不是静默落进默认分支。
var allConfidenceLevels = []ConfidenceLevel{
	ConfidenceSufficient,
	ConfidenceDirectional,
	ConfidenceLowSample,
	ConfidenceConfounded,
}
```

- [ ] **Step 4: 写 verdict.go**

创建 `internal/systems/insights/verdict.go`：

```go
package insights

// 三档结论是素材洞察的中轴：任何一屏、任何一条结论，最后都要落到这三档之一。
// 这个文件是三档的唯一定义处。
//
// 为什么不直接用 ConfidenceLevel：那是四个值的**统计**口径，「样本不足」和
// 「存在混杂」是两种完全不同的毛病，算法里必须分开。但坐在屏幕前的人只需要
// 知道三件事——这条能拿去用（✅）、只能参考（👁）、还是根本算不出来（❓）。
// 四个统计档位往三个行动档位的收敛，只允许发生在本文件的 Verdict() 里。

// Verdict 是给人看的三档。
type Verdict string

const (
	// VerdictExplained ✅ 能归因：差异是真的，而且能归到那个变量上，可以拿去用。
	VerdictExplained Verdict = "explained"
	// VerdictObserved 👁 只是观察：看见了差异，但归不到变量上——可能是样本还不够稳，
	// 也可能是有别的特征跟着一起变。区别写在 Note 里，不占一个档位。
	VerdictObserved Verdict = "observed"
	// VerdictUnclear ❓ 算不出来：数据太少，连差异存不存在都判断不了。
	VerdictUnclear Verdict = "unclear"
)

func (v Verdict) Label() string {
	switch v {
	case VerdictExplained:
		return "能归因"
	case VerdictObserved:
		return "只是观察"
	case VerdictUnclear:
		return "算不出来"
	}
	return string(v)
}

func (v Verdict) Icon() string {
	switch v {
	case VerdictExplained:
		return "✅"
	case VerdictObserved:
		return "👁"
	case VerdictUnclear:
		return "❓"
	}
	return ""
}

// UpgradePath 是这一档往上走的唯一通道。
type UpgradePath string

const (
	UpgradeNone UpgradePath = ""
	// UpgradeExperiment：👁 缺的是「只改这一个变量」，那就得做实验。
	UpgradeExperiment UpgradePath = "experiment"
	// UpgradeSimilar：❓ 缺的是样本，那就得从库里拉相似素材把样本做厚。
	UpgradeSimilar UpgradePath = "similar_assets"
)

func (u UpgradePath) Label() string {
	switch u {
	case UpgradeExperiment:
		return "做个实验"
	case UpgradeSimilar:
		return "找相似素材"
	}
	return ""
}

func (v Verdict) Upgrade() UpgradePath {
	switch v {
	case VerdictObserved:
		return UpgradeExperiment
	case VerdictUnclear:
		return UpgradeSimilar
	}
	return UpgradeNone
}

// Verdict 是四档统计口径到三档行动口径的**唯一**收敛处。
//
// 未知取值落到 VerdictUnclear 而不是 VerdictObserved：不认识的档位应该表现为
// 「算不出来」，让人去查，而不是伪装成一条能参考的观察。
func (c ConfidenceLevel) Verdict() Verdict {
	switch c {
	case ConfidenceSufficient:
		return VerdictExplained
	case ConfidenceDirectional, ConfidenceConfounded:
		return VerdictObserved
	case ConfidenceLowSample:
		return VerdictUnclear
	}
	return VerdictUnclear
}

// Judgement 是一条结论对外的统一表达。要给人看档位的结构体一律内嵌它，
// 而不是各自摆一对 Confidence + Note 字段——摆散了迟早出现两页对同一份数据
// 给出不同档位，而没人解释得清哪个对。
type Judgement struct {
	Confidence   ConfidenceLevel `json:"confidence"`
	Verdict      Verdict         `json:"verdict"`
	VerdictLabel string          `json:"verdict_label"`
	Upgrade      UpgradePath     `json:"upgrade,omitempty"`
	Note         string          `json:"note"`
}

// judge 是构造 Judgement 的唯一入口。手拼 Judgement 字面量会绕过收敛规则，
// 契约测试会在 JSON 层把它抓出来。
func judge(confidence ConfidenceLevel, note string) Judgement {
	verdict := confidence.Verdict()
	return Judgement{
		Confidence:   confidence,
		Verdict:      verdict,
		VerdictLabel: verdict.Label(),
		Upgrade:      verdict.Upgrade(),
		Note:         note,
	}
}

// verdictRank 越小越弱。
func verdictRank(v Verdict) int {
	switch v {
	case VerdictExplained:
		return 2
	case VerdictObserved:
		return 1
	}
	return 0
}

// weakestVerdict 给一屏定档：取最弱的那一条。
func weakestVerdict(verdicts ...Verdict) Verdict {
	if len(verdicts) == 0 {
		return VerdictUnclear
	}
	weakest := VerdictExplained
	for _, v := range verdicts {
		if verdictRank(v) < verdictRank(weakest) {
			weakest = v
		}
	}
	return weakest
}
```

- [ ] **Step 5: 跑测试确认通过**

```bash
go test ./internal/systems/insights/ -run 'TestConfidenceMaps|TestEachVerdict|TestVerdictLabel|TestScreenVerdict|TestJudgeFills' -v
```

Expected: 五个测试全部 PASS。

- [ ] **Step 6: 跑全包测试确认没弄坏别的**

```bash
go build ./... && go test ./internal/systems/insights/...
```

Expected: ok。

- [ ] **Step 7: 提交**

```bash
git add internal/systems/insights/verdict.go internal/systems/insights/verdict_test.go internal/systems/insights/connectors.go
git commit -m "feat(insights): 三档结论作为全模块唯一判定口径"
```

---

### Task 2: 六视图与总览内嵌 Judgement

**Files:**
- Modify: `internal/systems/insights/group_compare.go:20-36`（`GroupComparison`）、`:56-104`（`compareGroups`）
- Modify: `internal/systems/insights/performance.go:72-96`（`VariantComparison`）、`:98-111`（`AssetTrend`）、`:127-148`（`FatigueSignal`）、`:160-176`（`MetricAnomaly`）、`:180-212`（`FeatureDriver`）、`:24-43`（`PerformanceAnalysis`）
- Modify: `internal/systems/insights/connectors.go:683-708`（`MetricOverview`）、`:718-729`（`AssetPerformance`）、`:1281`、`:1230`
- Create: `internal/systems/insights/judgement_contract_test.go`
- Modify: `api/openapi/insights-v1.yaml:2757`、`:2779` 及 PerformanceAnalysis 相关 schema

**Interfaces:**
- Consumes: Task 1 的 `Judgement`、`judge()`、`weakestVerdict()`。
- Produces:
  - 上述七个结构体不再有独立的 `Confidence ConfidenceLevel` / `Note string` / `ConfidenceNote string` 字段，改为内嵌 `Judgement`（字段通过 Go 提升规则仍可用 `x.Confidence` / `x.Note` 访问）。
  - `PerformanceAnalysis` 新增字段 `Judgement Judgement \`json:"judgement"\``（屏级档位，取六视图里最弱的一条）。
  - JSON 线上变化：`MetricOverview.confidence_note` → `note`；所有带 `confidence` 的对象新增 `verdict` / `verdict_label` / `note` / `upgrade`。

- [ ] **Step 1: 写失败的契约测试**

创建 `internal/systems/insights/judgement_contract_test.go`：

```go
package insights

import (
	"encoding/json"
	"testing"
	"time"
)

// 三档要成为中轴，靠的不是「大家记得填」，而是这一条：JSON 里凡是出现了
// confidence，同一个对象里就必须出现 verdict。有人新加一个带 confidence 的
// 结构体却忘了内嵌 Judgement，这里会直接报出它在 JSON 里的路径。
func TestEveryConfidenceCarriesAVerdict(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	window := MetricWindow{Start: now.AddDate(0, 0, -30), End: now}

	overview := buildMetricOverview(window, nil, []DataSource{{ID: "ds-1"}}, now)
	analysis := buildPerformanceAnalysis(window, nil, nil)

	for name, value := range map[string]any{"MetricOverview": overview, "PerformanceAnalysis": analysis} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s 序列化失败：%v", name, err)
		}
		var tree any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("%s 反序列化失败：%v", name, err)
		}
		assertVerdictAccompaniesConfidence(t, name, tree)
	}
}

func assertVerdictAccompaniesConfidence(t *testing.T, path string, node any) {
	t.Helper()
	switch typed := node.(type) {
	case map[string]any:
		if _, hasConfidence := typed["confidence"]; hasConfidence {
			if _, hasVerdict := typed["verdict"]; !hasVerdict {
				t.Errorf("%s 有 confidence 但没有 verdict——这个结构体没内嵌 Judgement", path)
			}
			if _, hasLabel := typed["verdict_label"]; !hasLabel {
				t.Errorf("%s 有 confidence 但没有 verdict_label", path)
			}
		}
		for key, child := range typed {
			assertVerdictAccompaniesConfidence(t, path+"."+key, child)
		}
	case []any:
		for index, child := range typed {
			assertVerdictAccompaniesConfidence(t, path+"[]", child)
			_ = index
		}
	}
}

// 空窗口是最常见的情况：一条数据都没有。此时屏级档位必须是「算不出来」，
// 不能因为「没有任何一条弱结论」就默认成能归因。
func TestEmptyWindowScreenVerdictIsUnclear(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	window := MetricWindow{Start: now.AddDate(0, 0, -30), End: now}

	analysis := buildPerformanceAnalysis(window, nil, nil)
	if analysis.Judgement.Verdict != VerdictUnclear {
		t.Errorf("空窗口的屏级档位是 %s，期望 %s", analysis.Judgement.Verdict, VerdictUnclear)
	}
}

// MetricOverview 原来用 confidence_note，别的结构体用 note。同一个意思两个键名，
// 前端就得写两套渲染。统一成 note。
func TestMetricOverviewUsesTheSharedNoteKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	window := MetricWindow{Start: now.AddDate(0, 0, -30), End: now}

	raw, err := json.Marshal(buildMetricOverview(window, nil, []DataSource{{ID: "ds-1"}}, now))
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if _, stale := decoded["confidence_note"]; stale {
		t.Error("confidence_note 还在——应该已经并进 Judgement 的 note")
	}
	if _, ok := decoded["note"]; !ok {
		t.Error("缺少 note")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestEveryConfidenceCarriesAVerdict|TestEmptyWindowScreenVerdictIsUnclear|TestMetricOverviewUsesTheSharedNoteKey' -v
```

Expected: 三个测试都 FAIL——「有 confidence 但没有 verdict」、屏级档位为空串、`confidence_note 还在`。

- [ ] **Step 3: GroupComparison 内嵌 Judgement**

在 `internal/systems/insights/group_compare.go`，把 `GroupComparison` 结尾的两行

```go
	CovaryingFeatures []string        `json:"covarying_features"`
	Confidence        ConfidenceLevel `json:"confidence"`
	Note              string          `json:"note"`
}
```

改为

```go
	CovaryingFeatures []string `json:"covarying_features"`
	// Judgement 内嵌而不是摆两个字段：档位和它的理由必须一起产生、一起搬运。
	Judgement `json:",inline"`
}
```

> Go 的 `encoding/json` 对匿名结构体字段本来就是展开的，`json:",inline"` 只是写给人看的标记；如果 `go vet` 抱怨这个 tag，直接去掉 tag 写成 `Judgement`。

再把 `compareGroups` 里五处赋值统一改成通过 `judge()`。把 `:82-102` 的 switch 整体替换为：

```go
	// 判定顺序是有意的：样本 → 混杂 → 区间 → 口径 → 充分度。
	// 先卡样本，因为样本不够时后面几项都算不出有意义的东西；混杂排在区间前面，
	// 因为区间不重叠但存在协变特征时，结论仍然归不到目标变量上。
	switch {
	case minImpressions < directionalSampleImpressions:
		result.Judgement = judge(ConfidenceLowSample,
			fmt.Sprintf("样本较少的一侧只有 %s 次展示，这个分组还比不出东西。", countText(minImpressions)))
	case len(result.CovaryingFeatures) > 0:
		result.Judgement = judge(ConfidenceConfounded,
			fmt.Sprintf("这一组素材在「%s」上也整齐地和其他素材不同，差异不能只算到「%s」头上。",
				strings.Join(result.CovaryingFeatures, "」「"), input.SubjectLabel))
	case result.IntervalsOverlap:
		result.Judgement = judge(ConfidenceDirectional, "两组的点击率置信区间重叠，差异可能只是波动。")
	case !input.Comparable:
		result.Judgement = judge(ConfidenceConfounded, "窗口内口径不一致，两组之间的差异可能来自口径而不是内容。")
	case minImpressions < sufficientSampleImpressions:
		result.Judgement = judge(ConfidenceDirectional, directionalNote(input.PreRegistered))
	default:
		result.Judgement = judge(ConfidenceSufficient, sufficientNote(input.PreRegistered))
	}
	return result
```

- [ ] **Step 4: performance.go 五个结构体内嵌 Judgement**

逐个替换字段对（保留其余字段不动）：

`VariantComparison` 末尾：
```go
	Verdict VariantVerdict `json:"verdict"`
	Judgement
}
```

`AssetTrend` 末尾：
```go
	CTRChange *float64 `json:"ctr_change,omitempty"`
	Judgement
}
```

`FatigueSignal` 末尾：
```go
	Severity FatigueSeverity `json:"severity"`
	// AlternativeExplanations 是这次没能排除的其他解释。为空表示确实没有别的解释，
	// 不是「没检查」——检查项是固定的四类。
	AlternativeExplanations []string `json:"alternative_explanations,omitempty"`
	Judgement
}
```

`MetricAnomaly` 末尾（原来只有 `Note`，现在补上档位）：
```go
	// Deviation 是偏离中位数多少个 MAD。
	Deviation float64 `json:"deviation"`
	// 异常永远只到 👁：这一天不对劲是事实，为什么不对劲这里答不了。
	// 所以档位固定 directional，不随样本量变动。
	Judgement
}
```

`FeatureDriver` 末尾：
```go
	CovaryingFeatures []string `json:"covarying_features,omitempty"`
	Judgement
}
```

`PerformanceAnalysis` 在 `Notes` 之前插入：
```go
	// Judgement 是屏级档位：六个视图里最弱的那一条。一屏上有一条算不出来，
	// 这屏就不能整体标成能归因。
	Judgement Judgement `json:"judgement"`
```

然后把 `performance.go` 里所有 `xxx.Confidence = <level>` + `xxx.Note = <text>` 的成对赋值改成 `xxx.Judgement = judge(<level>, <text>)`。用下面这条找出全部位置：

```bash
grep -n "\.Confidence = \|\.Note = " internal/systems/insights/performance.go
```

`MetricAnomaly` 的构造处改成 `Judgement: judge(ConfidenceDirectional, note)`。

最后在 `buildPerformanceAnalysis` 的 `return analysis` 之前插入屏级汇总：

```go
	// 屏级档位取最弱：六个视图里只要有一个说算不出来，整屏就不能标成能归因。
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
	analysis.Judgement = Judgement{
		Confidence:   worstConfidenceOf(analysis),
		Verdict:      weakest,
		VerdictLabel: weakest.Label(),
		Upgrade:      weakest.Upgrade(),
		Note:         screenNote(weakest, len(verdicts)),
	}
```

并在同文件末尾补两个小函数：

```go
// worstConfidenceOf 给屏级档位配一个统计口径值，让 confidence 和 verdict 不打架。
// 三档是从四档收敛来的，反过来一个 verdict 对应不止一个 confidence，
// 这里取「最能解释为什么是这一档」的那个。
func worstConfidenceOf(analysis PerformanceAnalysis) ConfidenceLevel {
	worst := ConfidenceSufficient
	rank := map[ConfidenceLevel]int{
		ConfidenceLowSample:   0,
		ConfidenceConfounded:  1,
		ConfidenceDirectional: 2,
		ConfidenceSufficient:  3,
	}
	seen := false
	visit := func(level ConfidenceLevel) {
		if !seen || rank[level] < rank[worst] {
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

func screenNote(verdict Verdict, items int) string {
	if items == 0 {
		return "这个窗口里还没有能出结论的数据。"
	}
	switch verdict {
	case VerdictExplained:
		return "这一屏的结论都站得住，可以直接用。"
	case VerdictObserved:
		return "这一屏里有结论归不到具体变量上，只能当观察看。"
	}
	return "这一屏里有结论连差异存不存在都判断不了。"
}
```

- [ ] **Step 5: connectors.go 两个结构体内嵌 Judgement**

`MetricOverview:696-697` 的两行

```go
	Confidence     ConfidenceLevel `json:"confidence"`
	ConfidenceNote string          `json:"confidence_note"`
```

替换为

```go
	// 内嵌而不是摆两个字段：以前这里叫 confidence_note，别处叫 note，
	// 同一个意思两个键名，前端就得写两套渲染。
	Judgement
```

`AssetPerformance:728` 的一行

```go
	Confidence   ConfidenceLevel `json:"confidence"`
```

替换为

```go
	Judgement
```

`connectors.go:1230` 改为：

```go
		row.Judgement = judge(confidenceOf(row.Counts, row.Attributable, row.Objects), assetRowNote(row))
```

`connectors.go:1281` 改为：

```go
	level, note := overallConfidence(overview)
	overview.Judgement = judge(level, note)
```

在 `overallConfidence` 下方补 `assetRowNote`：

```go
// assetRowNote 给素材矩阵的每一行配一句理由。以前这一行只有档位没有理由，
// 前端只能显示一个「置信存在混杂」，人看不出混杂在哪。
func assetRowNote(row AssetPerformance) string {
	if !row.Attributable {
		return "这一行是未匹配对象的汇总，归不到具体素材上。"
	}
	if row.Objects > 1 {
		return fmt.Sprintf("这个素材投在 %d 个平台对象上，预算与受众差异会混进来。", row.Objects)
	}
	if row.Counts.Impressions < directionalSampleImpressions {
		return fmt.Sprintf("只有 %s 次展示，这一行还比不出东西。", countText(row.Counts.Impressions))
	}
	if row.Counts.Impressions < sufficientSampleImpressions {
		return "样本到了方向性门槛，可作参考，但还不够下确定结论。"
	}
	return "样本充分、归因到单一素材，这一行可以当结论用。"
}
```

- [ ] **Step 6: 跑测试确认通过**

```bash
go build ./... && go test ./internal/systems/insights/ -run 'TestEveryConfidenceCarriesAVerdict|TestEmptyWindowScreenVerdictIsUnclear|TestMetricOverviewUsesTheSharedNoteKey' -v
```

Expected: 三个测试 PASS。

- [ ] **Step 7: 跑全包测试并修既有断言**

```bash
go test ./internal/systems/insights/... 2>&1 | head -50
```

既有测试里凡是构造 `GroupComparison{Confidence: X, Note: Y}` 之类字面量的，改成 `Judgement: judge(X, Y)`；读取 `.Confidence` / `.Note` 的不用改（字段被提升了）。反复跑到全绿。

- [ ] **Step 8: 更新 OpenAPI 契约**

在 `api/openapi/insights-v1.yaml` 里：

1. `:2757` 的 `required` 列表把 `confidence_note` 换成 `note`，并追加 `verdict, verdict_label`。
2. `:2779` 的 `confidence_note` 定义替换为：

```yaml
        note: { type: string, description: '一句话解释为什么是这个档位。' }
        verdict:
          type: string
          enum: [explained, observed, unclear]
          description: '三档结论：能归因 / 只是观察 / 算不出来。由 confidence 四档收敛而来，收敛规则见 internal/systems/insights/verdict.go。'
        verdict_label: { type: string, description: '三档的中文名，后端给定，前端不得自行翻译。' }
        upgrade:
          type: string
          enum: [experiment, similar_assets]
          description: '这一档往上走的通道：observed 做个实验，unclear 找相似素材。explained 没有这个字段。'
```

3. 同样的四个字段追加到 `VariantComparison` / `AssetTrend` / `FatigueSignal` / `MetricAnomaly` / `FeatureDriver` / `AssetPerformance` / `GroupComparison` 的 properties，并把 `verdict`、`verdict_label` 加进各自的 `required`。用下面这条定位：

```bash
grep -n "VariantComparison:\|AssetTrend:\|FatigueSignal:\|MetricAnomaly:\|FeatureDriver:\|AssetPerformance:\|GroupComparison:" api/openapi/insights-v1.yaml
```

4. `PerformanceAnalysis` 的 properties 加 `judgement: { $ref: '#/components/schemas/Judgement' }`，并在 `components.schemas` 新增：

```yaml
    Judgement:
      type: object
      required: [confidence, verdict, verdict_label, note]
      properties:
        confidence:
          type: string
          enum: [sufficient, directional, low_sample, confounded]
        verdict:
          type: string
          enum: [explained, observed, unclear]
        verdict_label: { type: string }
        upgrade:
          type: string
          enum: [experiment, similar_assets]
        note: { type: string }
```

- [ ] **Step 9: 提交**

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml
git commit -m "feat(insights): 六视图与总览统一内嵌三档判定"
```

---

### Task 3: 变量分三类，归因只认客观可测与人工标注

**Files:**
- Modify: `internal/systems/insights/assets.go:81-90`（`FeatureSource`）
- Modify: `internal/systems/insights/performance.go:270-279`（`assetSlice`）、`:390-421`（`pickFeatures`）、`:46-52`（`FeatureDiff`）、`:520-540`（特征差异比对）、`:985-1030`、`:1095-1125`（驱动因素读取特征处）
- Create: `internal/systems/insights/feature_source_test.go`
- Modify: `api/openapi/insights-v1.yaml`（`FeatureSource` 枚举、`FeatureDiff`）

**Interfaces:**
- Consumes: Task 1 的 `judge()`。
- Produces:
  - `SourceDerived FeatureSource = "derived"`；`func (FeatureSource) valid() bool` 接受三值；`func (FeatureSource) Label() string` 返回「客观可测 / 人工标注 / 模型推断」；`func (FeatureSource) AdmissibleForAttribution() bool` —— `derived` 与 `human` 为 true，`ai` 为 false。
  - `type featureCell struct{ value string; source FeatureSource }`（包内）；`assetSlice.features` 类型改为 `map[string]featureCell`。
  - `func (a *assetSlice) featureValue(key string) (string, bool)`（任意来源，展示用）与 `func (a *assetSlice) attributableFeature(key string) (string, bool)`（只认 derived+human，归因用）。
  - `FeatureDiff` 的 `HumanOnly bool` 字段替换为 `Source FeatureSource \`json:"source"\`` 与 `Admissible bool \`json:"admissible"\``。

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/feature_source_test.go`：

```go
package insights

import "testing"

// 变量分三类，是因为它们的可信度根本不是一回事：时长是从文件里量出来的，
// 「情绪是否高涨」是模型猜的。把模型猜的东西放进归因结论，等于用一个猜测
// 去解释另一个猜测。
func TestOnlyMeasuredAndHumanFeaturesAreAdmissibleForAttribution(t *testing.T) {
	t.Parallel()

	cases := map[FeatureSource]bool{
		SourceDerived: true,
		SourceHuman:   true,
		SourceAI:      false,
	}
	for source, admissible := range cases {
		if got := source.AdmissibleForAttribution(); got != admissible {
			t.Errorf("%s 的归因准入是 %v，期望 %v", source, got, admissible)
		}
		if !source.valid() {
			t.Errorf("%s 应该是合法来源", source)
		}
	}
	if FeatureSource("guessed").valid() {
		t.Error("未知来源不该通过校验")
	}
}

func TestFeatureSourceLabels(t *testing.T) {
	t.Parallel()

	cases := map[FeatureSource]string{
		SourceDerived: "客观可测",
		SourceHuman:   "人工标注",
		SourceAI:      "模型推断",
	}
	for source, label := range cases {
		if got := source.Label(); got != label {
			t.Errorf("%s 的名字是 %q，期望 %q", source, got, label)
		}
	}
}

// 展示和归因用同一份特征，但准入不同：AI 提取的特征可以摆在页面上，
// 不能进结论。两个读取口分开，才不会有人顺手用错。
func TestAttributableFeatureRejectsModelGuesses(t *testing.T) {
	t.Parallel()

	slice := &assetSlice{features: map[string]featureCell{
		"duration": {value: "15s", source: SourceDerived},
		"tone":     {value: "高涨", source: SourceAI},
		"hook":     {value: "疑问句", source: SourceHuman},
	}}

	for key, want := range map[string]string{"duration": "15s", "tone": "高涨", "hook": "疑问句"} {
		if got, ok := slice.featureValue(key); !ok || got != want {
			t.Errorf("展示口读 %s 得到 (%q,%v)，期望 %q", key, got, ok, want)
		}
	}
	if _, ok := slice.attributableFeature("tone"); ok {
		t.Error("模型推断的特征不该进归因")
	}
	if got, ok := slice.attributableFeature("duration"); !ok || got != "15s" {
		t.Errorf("客观可测的特征应该能进归因，得到 (%q,%v)", got, ok)
	}
	if got, ok := slice.attributableFeature("hook"); !ok || got != "疑问句" {
		t.Errorf("人工标注的特征应该能进归因，得到 (%q,%v)", got, ok)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestOnlyMeasuredAndHuman|TestFeatureSourceLabels|TestAttributableFeature' -v
```

Expected: 编译失败，`undefined: SourceDerived`、`undefined: featureCell`。

- [ ] **Step 3: 扩 FeatureSource**

`internal/systems/insights/assets.go:83-90` 改为：

```go
type FeatureSource string

const (
	// SourceAI 模型推断：模型看着素材猜出来的。可以展示，不能进归因结论——
	// 拿一个猜测去解释另一个猜测，结论看起来有理，其实一层假设都没减少。
	SourceAI FeatureSource = "ai"
	// SourceHuman 人工标注：人填的。人会错，但人为自己填的东西负责。
	SourceHuman FeatureSource = "human"
	// SourceDerived 客观可测：从文件本身算出来的，时长、分辨率、镜头数、语速。
	// 同一个文件算两遍结果一样，这是三类里唯一可复现的。
	SourceDerived FeatureSource = "derived"
)

func (s FeatureSource) valid() bool {
	return s == SourceAI || s == SourceHuman || s == SourceDerived
}

func (s FeatureSource) Label() string {
	switch s {
	case SourceDerived:
		return "客观可测"
	case SourceHuman:
		return "人工标注"
	case SourceAI:
		return "模型推断"
	}
	return string(s)
}

// AdmissibleForAttribution 决定这个来源的特征能不能进归因结论。
func (s FeatureSource) AdmissibleForAttribution() bool {
	return s == SourceDerived || s == SourceHuman
}
```

- [ ] **Step 4: assetSlice 带上特征来源**

`performance.go:270-279`，把 `features map[string]string` 改为：

```go
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
```

`pickFeatures`（`:390-421`）里初始化和写入两处跟着改：初始化 `slice.features = map[string]featureCell{}`，写入改成

```go
		slice.features[feature.Key] = featureCell{value: text, source: feature.Source}
```

- [ ] **Step 5: 把归因路径切到 attributableFeature**

编译会直接指出所有读 `slice.features[key]` 的地方。按用途分派：

- `:530` 素材对比算变量差异 → `attributableFeature`（对比要归因）
- `:994`、`:1022` 驱动因素分组 → `attributableFeature`（驱动因素要归因）
- `:1101`、`:1117` 疲劳里读特征做解释 → `featureValue`（只是描述，不下结论）

`FeatureDiff`（`:46-52`）把 `HumanOnly bool` 换成：

```go
	// Source 是这个变量的来源。它决定这条差异能不能进结论——摆在结构体里
	// 而不是让前端去猜，是因为「哪些变量算数」这件事只能有一个说法。
	Source FeatureSource `json:"source"`
	// Admissible 是 Source.AdmissibleForAttribution() 的结果，冗余一份给前端，
	// 免得前端把准入规则再实现一遍。
	Admissible bool `json:"admissible"`
```

构造 `FeatureDiff` 的地方补上这两个字段（来源取 baseline 侧那一格的 `source`；两侧来源不同时取更弱的那一个——`ai` 弱于 `human` 弱于 `derived`，只要有一侧是 `ai`，这条差异就不能进归因）。

- [ ] **Step 6: 跑测试确认通过**

```bash
go build ./... && go test ./internal/systems/insights/...
```

Expected: 全绿。既有测试里构造 `AssetFeature{Source: ...}` 的不用改（`ai`/`human` 仍然合法）。

- [ ] **Step 7: 更新 OpenAPI**

```bash
grep -n "enum: \[ai, human\]\|ai, human" api/openapi/insights-v1.yaml
```

把找到的 `FeatureSource` 枚举都改成 `[ai, human, derived]`，并在 `FeatureDiff` 的 properties 里把 `human_only` 换成：

```yaml
        source:
          type: string
          enum: [ai, human, derived]
          description: '变量来源。derived 从文件算出、human 人工标注、ai 模型推断。'
        admissible:
          type: boolean
          description: '这条差异能否进入归因结论。ai 来源恒为 false。'
```

- [ ] **Step 8: 提交**

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml
git commit -m "feat(insights): 变量分三类，归因只认客观可测与人工标注"
```

---

### Task 4: 名词表落成代码并从设置页暴露

**Files:**
- Create: `internal/systems/insights/glossary.go`
- Create: `internal/systems/insights/glossary_test.go`
- Modify: `internal/systems/insights/settings.go`（新增一个 `glossary` 分组）

**Interfaces:**
- Consumes: Task 1 的 `Verdict`、`UpgradePath`；Task 3 的 `FeatureSource`；既有 `InsightSettings` / `SettingGroup` / `SettingItem` 结构。
- Produces:
  - `type GlossaryTerm struct{ Term, Means string; Avoid []string }`
  - `var insightGlossary []GlossaryTerm`
  - `func bannedAliases() []string` —— 所有 `Avoid` 的扁平集合
  - 设置页新增分组 key `glossary`

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/glossary_test.go`：

```go
package insights

import (
	"strings"
	"testing"
)

// 名词表不是一张给人读的表，它是一条约束：表上写了「不要再叫」的词，
// 就不许再出现在任何用户能看见的文案里。没有这条，名词表两周后就作废了。
func TestNoBannedAliasAppearsInUserFacingLabels(t *testing.T) {
	t.Parallel()

	labels := map[string]string{}
	for _, v := range []Verdict{VerdictExplained, VerdictObserved, VerdictUnclear} {
		labels["Verdict."+string(v)] = v.Label()
	}
	for _, u := range []UpgradePath{UpgradeExperiment, UpgradeSimilar} {
		labels["UpgradePath."+string(u)] = u.Label()
	}
	for _, s := range []FeatureSource{SourceAI, SourceHuman, SourceDerived} {
		labels["FeatureSource."+string(s)] = s.Label()
	}
	for _, c := range allConfidenceLevels {
		labels["ConfidenceLevel."+string(c)] = c.Label()
	}

	for _, alias := range bannedAliases() {
		for where, label := range labels {
			if strings.Contains(label, alias) {
				t.Errorf("%s 的文案 %q 里还有被废弃的说法「%s」", where, label, alias)
			}
		}
	}
}

// 每个词都得说清楚三件事，缺一件这一行就没用：只有名字是废话，
// 只有解释没有「不要再叫」就拦不住旧说法回流。
func TestEveryGlossaryTermIsComplete(t *testing.T) {
	t.Parallel()

	if len(insightGlossary) == 0 {
		t.Fatal("名词表是空的")
	}
	seen := map[string]bool{}
	for _, term := range insightGlossary {
		if strings.TrimSpace(term.Term) == "" {
			t.Error("有一行没有名字")
			continue
		}
		if seen[term.Term] {
			t.Errorf("「%s」在名词表里出现了两次", term.Term)
		}
		seen[term.Term] = true
		if strings.TrimSpace(term.Means) == "" {
			t.Errorf("「%s」没有解释", term.Term)
		}
	}
}

// 三档、两条升级通道、三类变量必须在名词表上——它们是这个模块的中轴词，
// 中轴词不在表上，表就只是装饰。
func TestSpineTermsAreOnTheGlossary(t *testing.T) {
	t.Parallel()

	required := []string{"能归因", "只是观察", "算不出来", "做个实验", "找相似素材",
		"客观可测", "人工标注", "模型推断", "记一笔", "发现", "经验"}
	present := map[string]bool{}
	for _, term := range insightGlossary {
		present[term.Term] = true
	}
	for _, want := range required {
		if !present[want] {
			t.Errorf("中轴词「%s」不在名词表上", want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestNoBannedAlias|TestEveryGlossaryTerm|TestSpineTerms' -v
```

Expected: 编译失败，`undefined: insightGlossary`、`undefined: bannedAliases`。

- [ ] **Step 3: 写 glossary.go**

创建 `internal/systems/insights/glossary.go`：

```go
package insights

// 名词表。这个模块以前同一件事有三四个叫法——「置信充分」「可归因」「强结论」
// 说的是同一档，「洞察卡」「经验」「结论」混着用。词多了不是丰富，是每读一屏
// 都得先在脑子里做一次翻译。
//
// 这张表是有约束力的：Avoid 里的说法不许再出现在任何用户能看见的文案里，
// glossary_test.go 会拦。

type GlossaryTerm struct {
	// Term 是唯一批准的叫法。
	Term string `json:"term"`
	// Means 用一句人话解释它是什么，不解释它怎么算。
	Means string `json:"means"`
	// Avoid 是被这个词取代的旧说法。
	Avoid []string `json:"avoid,omitempty"`
}

var insightGlossary = []GlossaryTerm{
	{
		Term:  "能归因",
		Means: "差异是真的，而且能归到某一个变量上，可以直接拿去指导下一轮。",
		Avoid: []string{"置信充分", "强结论", "高置信"},
	},
	{
		Term:  "只是观察",
		Means: "看见了差异，但说不清是不是那个变量造成的——可能样本还不够稳，也可能有别的特征跟着一起变。",
		Avoid: []string{"方向性结论", "弱结论", "存在混杂结论"},
	},
	{
		Term:  "算不出来",
		Means: "数据太少，连差异存不存在都判断不了。不是「没差异」，是「不知道」。",
		Avoid: []string{"无结论", "数据不足", "低置信"},
	},
	{
		Term:  "做个实验",
		Means: "把「只是观察」升成「能归因」的唯一办法：事先定好只改哪一个变量、分几组、样本到多少才看结果。",
		Avoid: []string{"AB测", "跑实验验证"},
	},
	{
		Term:  "找相似素材",
		Means: "把「算不出来」升上去的办法：从库里拉内容变量重合的素材，把样本做厚了再判一次。",
		Avoid: []string{"扩样本", "召回相似"},
	},
	{
		Term:  "客观可测",
		Means: "从素材文件本身量出来的变量：时长、分辨率、镜头数、语速。算两遍结果一样。",
		Avoid: []string{"结构化特征", "硬特征"},
	},
	{
		Term:  "人工标注",
		Means: "人填的变量。人会错，但人为自己填的东西负责。",
		Avoid: []string{"人工特征", "手工标签"},
	},
	{
		Term:  "模型推断",
		Means: "模型看着素材猜出来的变量。可以摆在页面上参考，不能进结论。",
		Avoid: []string{"AI特征", "智能标签"},
	},
	{
		Term:  "记一笔",
		Means: "在分析页看到一条值得留下的发现时，把它钉进本轮复盘草稿。这是分析页唯一的写操作。",
		Avoid: []string{"收藏", "加入报告", "标记"},
	},
	{
		Term:  "发现",
		Means: "一条还没被人确认的结论，带着它的三档和理由。发现攒在复盘里，确认之后才变成经验。",
		Avoid: []string{"洞察卡", "候选结论", "分析项"},
	},
	{
		Term:  "复盘",
		Means: "一轮投放结束后，把这一轮的发现收在一起、逐条勾选提交的地方。",
		Avoid: []string{"报告", "报告中心", "任务复盘报告"},
	},
	{
		Term:  "经验",
		Means: "人工确认过的发现。下一轮投前直接查它，它是这个模块最终要交付的东西。",
		Avoid: []string{"知识库", "沉淀", "已确认洞察"},
	},
}

func bannedAliases() []string {
	aliases := make([]string, 0, len(insightGlossary)*2)
	for _, term := range insightGlossary {
		aliases = append(aliases, term.Avoid...)
	}
	return aliases
}
```

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/systems/insights/ -run 'TestNoBannedAlias|TestEveryGlossaryTerm|TestSpineTerms' -v
```

Expected: 三个测试 PASS。若 `TestNoBannedAlias` 报出 `ConfidenceLevel` 的 Label（「充分」「方向性」「样本不足」「存在混杂」）撞上禁用词，说明禁用词写得太宽——`ConfidenceLevel` 是统计口径，本来就该保留它自己的说法。把撞上的那个 alias 从 `Avoid` 里去掉，并在该词条上加一行注释说明为什么留着。

- [ ] **Step 5: 设置页暴露名词表**

先看现有分组是怎么组装的：

```bash
grep -n "SettingGroup{" internal/systems/insights/settings.go | head
```

按同样的形状追加一个分组。名词表每一行做成一个 `SettingItem`：`Key` = 词本身，`Value` = `Means`，`Effect` = 「不要再叫：…」，`Recommended` = 「全模块统一用这个词」，`Source` = `internal/systems/insights/glossary.go`，`Basis` = 「2026-08-04 素材洞察重构设计 §3.6」。分组 `State` 用 `SettingInEffect`。

> 注意 `settings_test.go:TestEveryEffectiveSettingExplainsItsImpactAndRecommendation` 会检查 `Value`/`Effect`/`Recommended`/`Source`/`Basis` 五个字段都非空——上面五个都填了才不会挂。没有 `Avoid` 的词条，`Effect` 写「这个词没有被取代的旧说法」，不要留空。

- [ ] **Step 6: 跑全包测试**

```bash
go build ./... && go test ./internal/systems/insights/...
```

Expected: 全绿，包括既有的 `TestEveryEffectiveSettingExplainsItsImpactAndRecommendation`。

- [ ] **Step 7: 提交**

```bash
git add internal/systems/insights/glossary.go internal/systems/insights/glossary_test.go internal/systems/insights/settings.go
git commit -m "feat(insights): 名词表落成代码并从设置页暴露"
```

---

### Task 5: 前端三档定义与五个共用件

**Files:**
- Create: `src/data/verdict.ts`
- Create: `test/insight-verdict.test.ts`
- Create: `src/components/insight/shared/VerdictBadge.tsx`
- Create: `src/components/insight/shared/EvidenceDrawer.tsx`
- Create: `src/components/insight/shared/HowItWasComputed.tsx`
- Create: `src/components/insight/shared/NotEnoughSample.tsx`
- Create: `src/components/insight/shared/PinFindingButton.tsx`
- Create: `src/components/insight/shared/index.ts`
- Modify: `src/styles.css`（追加共用件样式）

**Interfaces:**
- Consumes: Task 1/2 的线上字段 `verdict` / `verdict_label` / `upgrade` / `note` / `confidence`。
- Produces:
  - `type Verdict = 'explained' | 'observed' | 'unclear'`
  - `type UpgradePath = 'experiment' | 'similar_assets'`
  - `interface Judgement { confidence: ApiConfidenceLevel; verdict: Verdict; verdict_label: string; upgrade?: UpgradePath; note: string }`
  - `const verdictIcon: Record<Verdict, string>`、`const verdictTone: Record<Verdict, 'ok' | 'warning' | 'muted'>`
  - `function verdictOfConfidence(level: ApiConfidenceLevel): Verdict`（只用于旧接口兜底）
  - `function weakestVerdict(items: readonly { verdict: Verdict }[]): Verdict`
  - `function upgradeLabel(path: UpgradePath | undefined): string`
  - 组件：`<VerdictBadge judgement={...} />`、`<EvidenceDrawer title open onClose>{children}</EvidenceDrawer>`、`<HowItWasComputed steps={string[]} />`、`<NotEnoughSample judgement={...} onFindSimilar?={() => void} />`、`<PinFindingButton disabled? onPin?={() => void} />`

- [ ] **Step 1: 写失败的测试**

创建 `test/insight-verdict.test.ts`：

```ts
import assert from "node:assert/strict";
import test from "node:test";
import {
  upgradeLabel,
  verdictIcon,
  verdictOfConfidence,
  weakestVerdict,
  type Verdict,
} from "../src/data/verdict.ts";

// 前端的收敛表必须和后端 internal/systems/insights/verdict.go 一模一样。
// 两边各写一套是这个模块以前最典型的毛病：同一份数据两页显示不同档位。
test("四档收敛成三档，规则与后端一致", () => {
  assert.equal(verdictOfConfidence("sufficient"), "explained");
  assert.equal(verdictOfConfidence("directional"), "observed");
  assert.equal(verdictOfConfidence("confounded"), "observed");
  assert.equal(verdictOfConfidence("low_sample"), "unclear");
});

test("三档的图标是固定的", () => {
  assert.equal(verdictIcon.explained, "✅");
  assert.equal(verdictIcon.observed, "👁");
  assert.equal(verdictIcon.unclear, "❓");
});

test("屏级档位取最弱的一条", () => {
  const items = (verdicts: Verdict[]) => verdicts.map((verdict) => ({ verdict }));
  assert.equal(weakestVerdict(items(["explained", "explained"])), "explained");
  assert.equal(weakestVerdict(items(["explained", "observed"])), "observed");
  assert.equal(weakestVerdict(items(["explained", "unclear", "observed"])), "unclear");
  // 一条都没有是「算不出来」，不是「能归因」——空屏不该比有数据的屏更可信。
  assert.equal(weakestVerdict([]), "unclear");
});

test("升级通道的文案", () => {
  assert.equal(upgradeLabel("experiment"), "做个实验");
  assert.equal(upgradeLabel("similar_assets"), "找相似素材");
  assert.equal(upgradeLabel(undefined), "");
});
```

- [ ] **Step 2: 跑测试确认失败**

```bash
npx tsx --test test/insight-verdict.test.ts
```

Expected: FAIL，`Cannot find module '../src/data/verdict.ts'`。

- [ ] **Step 3: 写 verdict.ts**

创建 `src/data/verdict.ts`：

```ts
// 三档结论的前端侧定义。后端权威定义在 internal/systems/insights/verdict.go，
// 这里的收敛表必须和它逐行对齐——两边各写一套，同一份数据就会在两个页面上
// 显示不同的档位，而没人解释得清哪个对。test/insight-verdict.test.ts 盯着。

import type { ApiConfidenceLevel } from './api'

export type Verdict = 'explained' | 'observed' | 'unclear'
export type UpgradePath = 'experiment' | 'similar_assets'

export interface Judgement {
  confidence: ApiConfidenceLevel
  verdict: Verdict
  verdict_label: string
  upgrade?: UpgradePath
  note: string
}

export const verdictIcon: Record<Verdict, string> = {
  explained: '✅',
  observed: '👁',
  unclear: '❓',
}

export const verdictLabel: Record<Verdict, string> = {
  explained: '能归因',
  observed: '只是观察',
  unclear: '算不出来',
}

// tone 复用 styles.css 里已有的三个语义色，不新造颜色体系。
export const verdictTone: Record<Verdict, 'ok' | 'warning' | 'muted'> = {
  explained: 'ok',
  observed: 'warning',
  unclear: 'muted',
}

// verdictOfConfidence 只给还没升级到新字段的旧接口兜底。新接口一律直接读
// 后端给的 verdict，不要在前端重算。
export function verdictOfConfidence(level: ApiConfidenceLevel): Verdict {
  switch (level) {
    case 'sufficient':
      return 'explained'
    case 'directional':
    case 'confounded':
      return 'observed'
    default:
      return 'unclear'
  }
}

const rank: Record<Verdict, number> = { unclear: 0, observed: 1, explained: 2 }

export function weakestVerdict(items: readonly { verdict: Verdict }[]): Verdict {
  if (items.length === 0) return 'unclear'
  return items.reduce<Verdict>(
    (weakest, item) => (rank[item.verdict] < rank[weakest] ? item.verdict : weakest),
    'explained',
  )
}

export function upgradeLabel(path: UpgradePath | undefined): string {
  if (path === 'experiment') return '做个实验'
  if (path === 'similar_assets') return '找相似素材'
  return ''
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
npx tsx --test test/insight-verdict.test.ts
```

Expected: 四个 test 全部 pass。

- [ ] **Step 5: 写五个共用件**

创建 `src/components/insight/shared/VerdictBadge.tsx`：

```tsx
import { type Judgement, upgradeLabel, verdictIcon, verdictTone } from '../../../data/verdict'

// 三档徽章。整个模块只有这一个地方决定档位长什么样——以前每个页面自己写一段
// confidence → 文案的映射，改一次要改七处，改漏一处就有一页在说另一套话。
export function VerdictBadge({ judgement, onUpgrade }: {
  judgement: Judgement
  // onUpgrade 给出去时才显示升级按钮。不是每一屏都有地方接这个动作。
  onUpgrade?: () => void
}) {
  const upgrade = upgradeLabel(judgement.upgrade)
  return (
    <span className={`verdict-badge verdict-${verdictTone[judgement.verdict]}`} title={judgement.note}>
      <span aria-hidden="true">{verdictIcon[judgement.verdict]}</span>
      <span>{judgement.verdict_label}</span>
      {upgrade && onUpgrade ? (
        <button type="button" className="verdict-upgrade" onClick={onUpgrade}>{upgrade}</button>
      ) : null}
    </span>
  )
}
```

创建 `src/components/insight/shared/EvidenceDrawer.tsx`：

```tsx
import type { ReactNode } from 'react'

// 证据抽屉。任何一条结论都必须能一键翻出它背后的数字——「相信我」不是证据。
export function EvidenceDrawer({ title, open, onClose, children }: {
  title: string
  open: boolean
  onClose: () => void
  children: ReactNode
}) {
  if (!open) return null
  return (
    <aside className="evidence-drawer" role="dialog" aria-label={title}>
      <header className="evidence-drawer-head">
        <h3>{title}</h3>
        <button type="button" onClick={onClose} aria-label="关闭">×</button>
      </header>
      <div className="evidence-drawer-body">{children}</div>
    </aside>
  )
}
```

创建 `src/components/insight/shared/HowItWasComputed.tsx`：

```tsx
import { useState } from 'react'

// 「怎么算的」弹层。每一步都写出用了哪个阈值、哪个口径——这一层是模块可信度的
// 全部来源：人不需要看懂统计，但必须能看到我们没有藏东西。
export function HowItWasComputed({ steps }: { steps: readonly string[] }) {
  const [open, setOpen] = useState(false)
  if (steps.length === 0) return null
  return (
    <span className="how-computed">
      <button type="button" className="how-computed-trigger" onClick={() => setOpen(!open)}>
        怎么算的
      </button>
      {open ? (
        <ol className="how-computed-steps">
          {steps.map((step, index) => <li key={index}>{step}</li>)}
        </ol>
      ) : null}
    </span>
  )
}
```

创建 `src/components/insight/shared/NotEnoughSample.tsx`：

```tsx
import type { Judgement } from '../../../data/verdict'

// 样本不足占位。空状态不能只写「暂无数据」——那句话既没说缺什么，
// 也没说怎么补。这个占位必须回答这两件事。
export function NotEnoughSample({ judgement, onFindSimilar }: {
  judgement: Judgement
  onFindSimilar?: () => void
}) {
  return (
    <div className="not-enough-sample">
      <p className="not-enough-sample-verdict">❓ {judgement.verdict_label}</p>
      <p className="not-enough-sample-note">{judgement.note}</p>
      {onFindSimilar ? (
        <button type="button" onClick={onFindSimilar}>找相似素材，把样本做厚</button>
      ) : null}
    </div>
  )
}
```

创建 `src/components/insight/shared/PinFindingButton.tsx`：

```tsx
// 「记一笔」。这是分析页唯一的写操作：分析页只读结论，把值得留下的那条
// 钉进本轮复盘草稿，复盘页再逐条勾选提交。
//
// 本期只出壳：onPin 不给就是禁用态，真正的写入在 P1 分析页计划里接。
export function PinFindingButton({ onPin, pinned }: {
  onPin?: () => void
  pinned?: boolean
}) {
  return (
    <button
      type="button"
      className={pinned ? 'pin-finding pinned' : 'pin-finding'}
      disabled={!onPin || pinned}
      onClick={onPin}
      title={pinned ? '已经记进本轮复盘' : '把这条发现记进本轮复盘'}
    >
      {pinned ? '已记一笔' : '记一笔'}
    </button>
  )
}
```

创建 `src/components/insight/shared/index.ts`：

```ts
export { VerdictBadge } from './VerdictBadge'
export { EvidenceDrawer } from './EvidenceDrawer'
export { HowItWasComputed } from './HowItWasComputed'
export { NotEnoughSample } from './NotEnoughSample'
export { PinFindingButton } from './PinFindingButton'
```

- [ ] **Step 6: 加样式**

在 `src/styles.css` 末尾追加（沿用文件里已有的 `--ok` / `--warning` / `--muted` 变量名；若变量名不同，先 `grep -n "\-\-ok\|\-\-warning\|\-\-muted" src/styles.css` 对齐）：

```css
/* 素材洞察 · 三档共用件 */
.verdict-badge { display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; border-radius: 999px; font-size: 12px; line-height: 18px; }
.verdict-ok { background: color-mix(in srgb, var(--ok) 12%, transparent); color: var(--ok); }
.verdict-warning { background: color-mix(in srgb, var(--warning) 12%, transparent); color: var(--warning); }
.verdict-muted { background: color-mix(in srgb, var(--muted) 12%, transparent); color: var(--muted); }
.verdict-upgrade { margin-left: 4px; border: none; background: none; color: inherit; text-decoration: underline; cursor: pointer; font-size: 12px; padding: 0; }
.evidence-drawer { position: fixed; top: 0; right: 0; bottom: 0; width: min(480px, 90vw); background: var(--surface, #fff); box-shadow: -8px 0 24px rgba(0,0,0,.12); display: flex; flex-direction: column; z-index: 40; }
.evidence-drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--border, #e5e5e5); }
.evidence-drawer-head h3 { margin: 0; font-size: 14px; }
.evidence-drawer-head button { border: none; background: none; font-size: 20px; line-height: 1; cursor: pointer; }
.evidence-drawer-body { padding: 16px; overflow: auto; }
.how-computed { position: relative; display: inline-block; }
.how-computed-trigger { border: none; background: none; color: var(--muted); text-decoration: underline dotted; cursor: pointer; font-size: 12px; padding: 0; }
.how-computed-steps { margin: 6px 0 0; padding-left: 18px; font-size: 12px; color: var(--muted); }
.not-enough-sample { padding: 24px; text-align: center; color: var(--muted); }
.not-enough-sample-verdict { margin: 0 0 4px; font-size: 14px; }
.not-enough-sample-note { margin: 0 0 12px; font-size: 12px; }
.pin-finding { border: 1px solid var(--border, #e5e5e5); background: none; border-radius: 6px; padding: 2px 10px; font-size: 12px; cursor: pointer; }
.pin-finding:disabled { opacity: .45; cursor: default; }
.pin-finding.pinned { border-color: var(--ok); color: var(--ok); }
```

- [ ] **Step 7: 类型检查**

```bash
npm run build
```

Expected: `tsc --noEmit` 通过，vite build 成功。

- [ ] **Step 8: 提交**

```bash
git add src/data/verdict.ts test/insight-verdict.test.ts src/components/insight/shared/ src/styles.css
git commit -m "feat(insights-web): 三档定义与五个共用件"
```

---

### Task 6: 投后分析页与总览页接上共用件

**Files:**
- Modify: `src/data/api.ts:1653`（`confidence_note` → `note` 及新字段）、`:4374` 附近的返回类型
- Modify: `src/components/PostLaunchAnalysisPage.tsx:87`（删掉本地 `confidenceLabels`）、`:331`、`:364`、`:388-389`、`:409`、`:447-448`
- Modify: `src/components/PostLaunchOverviewPage.tsx:187`

**Interfaces:**
- Consumes: Task 5 的 `VerdictBadge` / `NotEnoughSample` / `verdict.ts` 类型；Task 2 的线上新字段。
- Produces: 无新导出。这一步的产出是「模块里第一个不再自己写档位映射的页面」，后面四个入口照抄它。

- [ ] **Step 1: 前端类型跟上后端**

在 `src/data/api.ts` 找到 `ApiMetricOverview`（`:1653` 附近有 `confidence_note`），把

```ts
  confidence: ApiConfidenceLevel
  confidence_note: string
```

替换为

```ts
  confidence: ApiConfidenceLevel
  verdict: Verdict
  verdict_label: string
  upgrade?: UpgradePath
  note: string
```

并在文件顶部 import：

```ts
import type { UpgradePath, Verdict } from './verdict'
```

> `verdict.ts` 从 `api.ts` import `ApiConfidenceLevel`，`api.ts` 又从 `verdict.ts` import 类型——这是纯类型的循环引用，`import type` 会被 TS 完全擦除，运行时不成环。如果 `npm run build` 仍然报循环，把 `ApiConfidenceLevel` 的定义整体挪到 `verdict.ts` 并从 `api.ts` 重新导出。

对 `ApiVariantComparison` / `ApiAssetTrend` / `ApiFatigueSignal` / `ApiMetricAnomaly` / `ApiFeatureDriver` / `ApiAssetPerformance` 做同样的追加（这几个已经有 `confidence` 和 `note`，只需补 `verdict` / `verdict_label` / `upgrade?`）。`ApiPerformanceAnalysis` 追加 `judgement: Judgement`。`ApiFeatureDiff` 把 `human_only` 换成 `source: 'ai' | 'human' | 'derived'` 和 `admissible: boolean`。

- [ ] **Step 2: 跑类型检查确认失败**

```bash
npm run build
```

Expected: FAIL——`PostLaunchOverviewPage.tsx:187` 用了不存在的 `overview.confidence_note`。

- [ ] **Step 3: 改总览页**

`src/components/PostLaunchOverviewPage.tsx:187` 那一行

```tsx
                {overview.confidence_note || confidenceMeaning[overview.confidence]}
```

改为

```tsx
                {overview.note}
```

并在这一行上方（档位显示处）插入徽章：

```tsx
                <VerdictBadge judgement={overview} />
```

顶部 import：

```tsx
import { VerdictBadge } from './insight/shared'
```

如果 `confidenceMeaning` 至此没有别的引用了，删掉它——后端现在每一档都带理由，前端再存一份就会和后端说的不一样。

- [ ] **Step 4: 改投后分析页**

`src/components/PostLaunchAnalysisPage.tsx`：

1. 删掉 `:87` 的 `const confidenceLabels: Record<ApiConfidenceLevel, string> = {...}` 整块，以及 `:7` 的 `type ApiConfidenceLevel` import（若别处不再用）。
2. 顶部加 `import { NotEnoughSample, VerdictBadge } from './insight/shared'`。
3. `:331`、`:364`、`:409`、`:447-448` 四处 `置信{confidenceLabels[item.confidence]}` 一律换成 `<VerdictBadge judgement={item} />`。
4. `:388-389` 的疲劳特判

```tsx
        <em className={item.severity === 'none' && item.confidence === 'low_sample' ? 'muted' : severityTone[item.severity]}>
          {item.severity === 'none' && item.confidence === 'low_sample' ? '判断不了' : severityLabels[item.severity]}
```

改为按 verdict 判断，不再自己读 confidence：

```tsx
        <em className={item.verdict === 'unclear' ? 'muted' : severityTone[item.severity]}>
          {item.verdict === 'unclear' ? '判断不了' : severityLabels[item.severity]}
```

5. 每个视图的空列表分支，把「暂无数据」换成 `<NotEnoughSample judgement={analysis.judgement} />`。

- [ ] **Step 5: 类型检查与构建**

```bash
npm run build
```

Expected: 通过。

- [ ] **Step 6: 起服务验一眼**

用 `preview_start` 起前端（`.claude/launch.json` 里的 dev 配置），打开投后分析页，确认：档位显示的是「✅ 能归因 / 👁 只是观察 / ❓ 算不出来」而不是「置信充分」；空窗口显示的是带理由的占位而不是「暂无数据」；控制台无报错。

- [ ] **Step 7: 跑全量测试**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

Expected: 全绿。

- [ ] **Step 8: 提交**

```bash
git add src/data/api.ts src/components/PostLaunchAnalysisPage.tsx src/components/PostLaunchOverviewPage.tsx
git commit -m "feat(insights-web): 投后分析与总览改用三档共用件"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档第 5 期次表的「第 1 期：三档统一 + 名词表 + shared/ + 变量分三类」：

| 规格要求 | 落在 |
|---|---|
| 三档判定唯一实现（§3.1） | Task 1（`ConfidenceLevel.Verdict()`）+ Task 2（契约测试保证没有第二处） |
| 四档 → 三档映射 | Task 1 Step 4，测试表在 Step 1 |
| 两条升级通道（§1.4） | Task 1（`Upgrade()`）+ Task 5（`VerdictBadge` 的 `onUpgrade`） |
| 名词表（§3.6） | Task 4 |
| shared/ 五个共用件（§4.1） | Task 5 |
| 变量分三类（§3.4） | Task 3 |
| 归因只认 derived+human | Task 3 Step 5（`attributableFeature`） |
| 屏级「每屏自动标三档」（§1.6） | Task 2 Step 4（`PerformanceAnalysis.Judgement`） |

**未覆盖且是有意的：** `FeatureSource` 加了 `derived` 但没有任何地方**产出** derived 特征——视频探针、时长/镜头数的实际抽取属于第 7 期。本期只把通道开出来并把准入规则定死，现存数据全是 `ai`/`human`，行为不变。这一点必须在执行时向使用者说清楚，否则会误以为「客观可测」已经有数据了。

**同样有意留在本期之外的：** 疲劳曝光按日均而不是按总量的修正（设计文档标为「随时」）没有进 P0——它是一个独立的算法 bug 修复，和三档地基没有依赖关系，混进来会让 Task 2 的测试基线跟着变。它单独走一个小提交。

**2. 占位扫描** —— 全文无 TBD / TODO / 「类似 Task N」/「加上适当的错误处理」。Task 2 Step 4 与 Task 3 Step 5 用 `grep` 定位待改点而不是罗列行号，是因为那两处的行号会被前面的编辑挪动；命令和判断规则都是具体的。

**3. 类型一致性** —— 逐条核过：`Verdict` / `UpgradePath` / `Judgement` 三个名字在 Go 与 TS 两侧同名同值；`judge()` 只在 Go 侧（TS 侧直接读后端给的字段，不重算，只留 `verdictOfConfidence` 兜底旧接口）；`weakestVerdict` 两侧同名同语义（空输入 → `unclear`）；`AdmissibleForAttribution()` 与前端 `admissible` 字段对应；`featureValue` / `attributableFeature` 只在 Go 侧。`VerdictExplained` 等新常量与既有 `VerdictAttributable`（属 `VariantVerdict`）无重名。

---

## 依赖关系

P0 是另外五份计划（P1 分析 / P2 复盘 / P3 素材 / P4 经验 / P5 设置）的共同前置。Task 1→2→3 有严格顺序（2 依赖 1 的 `judge`，3 依赖 2 改完的结构体）；Task 4 只依赖 Task 1 和 Task 3 的枚举，可以和 Task 2 并行；Task 5 依赖 Task 2 定下的线上字段；Task 6 依赖 Task 2 和 Task 5。
