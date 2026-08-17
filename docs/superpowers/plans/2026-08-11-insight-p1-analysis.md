# 素材洞察 · 入口一「分析」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「投后分析」和「实验中心」合成一个叫「分析」的入口：六个视图自由探索、每一屏自动标三档、看到值得留下的结论按「记一笔」钉进本轮复盘草稿。

**Architecture:** 分析页对结论只读——它不确认、不沉淀、不改判定，唯一的写操作是「记一笔」。记一笔不收前端传来的判定：后端拿 (窗口 + 视图 + 来源引用) 回头去 `GetPerformanceAnalysis` 里找回那一条，取它当时算出来的 `Judgement` 存进去。判定要是能从请求里传，页面上标的档位就形同虚设。发现落在 (项目 + 窗口) 的复盘草稿上，草稿不存在就建一份；草稿的隐藏与过期清理属于 P2 复盘。

**Tech Stack:** Go 1.22+（`net/http` ServeMux、标准库 `testing`）、MySQL 8（`migrations/insights/` 时间戳命名的 up.sql）、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`。

## Global Constraints

- 前置依赖：**P0 地基必须先完成**（`docs/superpowers/plans/2026-08-11-insight-p0-foundation.md`）。本计划到处用 `Judgement` / `judge()` / `VerdictBadge` / `PinFindingButton`。
- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- 分析页对结论**只读**。除了「记一笔」，这个入口不得新增任何写接口。
- 「记一笔」的判定由后端重算，请求体里不许出现 `confidence` / `verdict` 字段——出现即校验失败。
- 六个视图共用后端一次 `performance-analysis` 返回，不许拆成六次请求。
- 名词按 P0 的名词表：「记一笔」不叫收藏，「发现」不叫洞察卡，「复盘」不叫报告。
- 迁移文件放 `migrations/insights/`，命名 `YYYYMMDDHHMMSS_<描述>.up.sql`，文件头必须有中文注释说明为什么要改。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。
- 本期不动 `buildReportDigest` 的自动带入逻辑——人记的和系统补的合并去重是 P2 复盘的事。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `internal/systems/insights/report_digest.go` | `ReportFinding` 增加来源、维度、变量、三档 | 改 |
| `internal/systems/insights/findings.go` | 记一笔：草稿 find-or-create、判定回查、去重 | 新建 |
| `internal/systems/insights/findings_test.go` | 记一笔的全部行为约束 | 新建 |
| `internal/systems/insights/service.go` | `PinFindingRequest`、`Service.PinFinding`、仓储接口加一个方法 | 改 |
| `internal/systems/insights/mysql_repository.go` | 按 (项目 + 窗口) 找草稿 | 改 |
| `internal/systems/insights/httpapi/server.go` | `POST .../findings` | 改 |
| `migrations/insights/20260811100000_insight_report_project_window_key.up.sql` | 唯一键补上 project_id | 新建 |
| `api/openapi/insights-v1.yaml` | `ReportFinding` 新字段、`PinFinding` 接口 | 改 |
| `src/components/insight/analysis/AnalysisPage.tsx` | 分析入口的壳：窗口选择、一次取数、分发六视图 | 新建 |
| `src/components/insight/analysis/OverviewView.tsx` | 视图一 · 指标总览 | 新建 |
| `src/components/insight/analysis/ComparisonView.tsx` | 视图二 · 素材对比 | 新建 |
| `src/components/insight/analysis/TrendView.tsx` | 视图三 · 趋势 | 新建 |
| `src/components/insight/analysis/FatigueView.tsx` | 视图四 · 疲劳 | 新建 |
| `src/components/insight/analysis/AnomalyView.tsx` | 视图五 · 异常 | 新建 |
| `src/components/insight/analysis/DriverView.tsx` | 视图六 · 驱动因素 | 新建 |
| `src/components/insight/analysis/usePinFinding.ts` | 记一笔的前端状态 | 新建 |
| `src/data/api.ts` | `pinFinding` 客户端方法与类型 | 改 |
| `src/data/navigation.ts` | 「投后分析」+「实验中心」→「分析」 | 改 |
| `src/App.tsx` / `src/lib/router.ts` | 路由指向新页面 | 改 |
| `src/components/PostLaunchAnalysisPage.tsx` | 删除（内容已拆进 analysis/） | 删（需确认） |

> **需要确认的破坏性动作：** 最后一行「删除 `PostLaunchAnalysisPage.tsx`」和 Task 6 里对 `navigation.ts` 的入口删除，属于「删除文件」和「修改现有核心逻辑」。执行到那一步时必须先向使用者确认，不要自行删除。在得到确认之前，保留旧文件不引用即可。

---

### Task 1: ReportFinding 记得住「谁记的、记的是哪个维度上的哪个变量」

**Files:**
- Modify: `internal/systems/insights/report_digest.go:42-58`
- Create: `internal/systems/insights/findings_test.go`（本任务先只放前两个测试）

**Interfaces:**
- Consumes: P0 的 `Judgement` / `judge()` / `ConfidenceLevel.Verdict()`。
- Produces:
  - `type FindingOrigin string`，取值 `OriginSystem="system"` / `OriginPinned="pinned"`；`func (FindingOrigin) Label() string` 返回「系统补的」/「我记的」。
  - `ReportFinding` 去掉独立的 `Confidence ConfidenceLevel` 字段，改为内嵌 `Judgement`；新增 `Origin FindingOrigin`、`Dimension string`、`Variable string`、`PinnedBy string`、`PinnedAt *time.Time`。
  - `func (f *ReportFinding) normalize()` —— 旧数据补齐：`Origin` 空则填 `OriginSystem`，`Verdict` 空则由 `Confidence` 收敛出来。
  - `func (f ReportFinding) dedupeKey() string` —— `Dimension + "\x00" + Variable`，两者都空时返回空串（表示这条不参与去重）。

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/findings_test.go`：

```go
package insights

import "testing"

// 旧报告的 digest 是 JSON 存的，里面只有 confidence 没有 verdict，也没有 origin。
// 读出来直接用会让复盘页上一半的发现没有档位、全部显示成「系统补的」都判断不了。
// 补齐必须发生在读取路径上，而不是靠一次性刷数据——刷完还会有更旧的备份被恢复回来。
func TestOldFindingsGetNormalizedOnRead(t *testing.T) {
	t.Parallel()

	old := ReportFinding{
		Kind:      SectionAssetPerformance,
		Text:      "15 秒版本的点击率比 30 秒版本高。",
		Judgement: Judgement{Confidence: ConfidenceDirectional},
	}
	old.normalize()

	if old.Origin != OriginSystem {
		t.Errorf("没有 origin 的旧发现应该算系统补的，得到 %q", old.Origin)
	}
	if old.Verdict != VerdictObserved {
		t.Errorf("verdict 应该由 confidence 收敛出来，得到 %q", old.Verdict)
	}
	if old.VerdictLabel != "只是观察" {
		t.Errorf("VerdictLabel 没补上，得到 %q", old.VerdictLabel)
	}
}

// 已经完整的发现不该被 normalize 改写——尤其是 Origin：把人记的那条改成系统补的，
// 复盘页上就再也分不清哪条是人自己挑的。
func TestNormalizeLeavesCompleteFindingsAlone(t *testing.T) {
	t.Parallel()

	pinned := ReportFinding{
		Kind:      SectionAssetPerformance,
		Text:      "15 秒版本的点击率比 30 秒版本高。",
		Origin:    OriginPinned,
		Judgement: judge(ConfidenceSufficient, "样本充分、区间不重叠。"),
	}
	before := pinned
	pinned.normalize()

	if pinned.Origin != before.Origin || pinned.Verdict != before.Verdict ||
		pinned.Note != before.Note {
		t.Errorf("完整的发现被 normalize 改写了：%+v -> %+v", before, pinned)
	}
}

// 去重键是「哪个维度上的哪个变量」。人在素材对比里记过「时长」，系统就不该在
// 同一份复盘里再补一条素材对比 · 时长——同一件事在会上被念两遍，第二遍会被当成
// 另一条独立证据。
func TestDedupeKeyIsDimensionPlusVariable(t *testing.T) {
	t.Parallel()

	a := ReportFinding{Dimension: "comparisons", Variable: "duration"}
	b := ReportFinding{Dimension: "comparisons", Variable: "duration"}
	c := ReportFinding{Dimension: "drivers", Variable: "duration"}

	if a.dedupeKey() != b.dedupeKey() {
		t.Error("同维度同变量应该是同一个去重键")
	}
	if a.dedupeKey() == c.dedupeKey() {
		t.Error("不同维度不该撞键——素材对比说的时长和驱动因素说的时长不是一回事")
	}
	// 自由文本类的发现（比如口径警告）没有维度也没有变量，不参与去重：
	// 拿空键去重会把它们全部折成一条。
	if (ReportFinding{}).dedupeKey() != "" {
		t.Error("没有维度和变量的发现不该参与去重")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestOldFindingsGetNormalized|TestNormalizeLeavesComplete|TestDedupeKeyIs' -v
```

Expected: 编译失败，`undefined: OriginSystem`、`ReportFinding has no field normalize`。

- [ ] **Step 3: 改 ReportFinding**

`internal/systems/insights/report_digest.go`，把 `:42-58` 的 `ReportFinding` 整体替换为：

```go
// FindingOrigin 区分这条发现是人自己挑的，还是系统按规则补的。
//
// 复盘页要把两者分开标（● 我记的 / ○ 系统补的）。混在一起显示，人就分不清
// 哪几条是自己看着数据决定要留的——而那几条恰恰是这次复盘真正的产出。
type FindingOrigin string

const (
	OriginSystem FindingOrigin = "system"
	OriginPinned FindingOrigin = "pinned"
)

func (o FindingOrigin) Label() string {
	if o == OriginPinned {
		return "我记的"
	}
	return "系统补的"
}

// ReportFinding 是报告里的一条发现。它是**定格**的：投后分析是活的，今天打开和
// 下周打开数字就不一样；报告要被引用、被追溯，必须固化，不能实时现算
// （基线文档 §7.9.8「定格」）。
type ReportFinding struct {
	Kind Report SectionKind `json:"kind"`
	Text string            `json:"text"`

	// Strength 是投后分析已经算好的强度，报告不重算，只挑。
	Strength VariantVerdict `json:"strength,omitempty"`

	// Judgement 是这条发现的三档与理由，同样是定格的。内嵌而不是摆一个 confidence
	// 字段：档位和它的理由必须一起搬运，分开搬迟早只搬一半。
	Judgement

	// Origin 说明这条是谁放进来的。
	Origin FindingOrigin `json:"origin"`
	// Dimension 是这条出自六个视图里的哪一个（comparisons / trends / fatigue /
	// anomalies / drivers / overview），Variable 是它说的哪个变量。
	// 这两个字段是复盘合并时的去重键——人记过的，系统不再补一条。
	Dimension string `json:"dimension,omitempty"`
	Variable  string `json:"variable,omitempty"`

	PinnedBy string     `json:"pinned_by,omitempty"`
	PinnedAt *time.Time `json:"pinned_at,omitempty"`

	// SourceRef 指回算出这条的东西：素材 ID、实验 ID、经验 ID。可追溯（03 §MVP⑫）。
	SourceRef string `json:"source_ref,omitempty"`

	// Dropped 为 true 表示人把它删掉了。**不物理删除**——留着才知道
	// 「系统带了什么、人不要什么」，这是评估自动带入好不好用的唯一依据。
	Dropped bool `json:"dropped"`
}

// normalize 补齐旧数据。digest 是 JSON 列，老行里没有 origin 也没有 verdict；
// 补齐放在读取路径上而不是刷一次数据，是因为刷完还会有更旧的备份被恢复回来。
func (f *ReportFinding) normalize() {
	if f.Origin == "" {
		f.Origin = OriginSystem
	}
	if f.Verdict == "" {
		f.Judgement = judge(f.Confidence, f.Note)
	}
}

// dedupeKey 是「哪个维度上的哪个变量」。两者都空表示这条是自由文本
// （比如口径警告），不参与去重——拿空键去重会把它们全折成一条。
func (f ReportFinding) dedupeKey() string {
	if f.Dimension == "" && f.Variable == "" {
		return ""
	}
	return f.Dimension + "\x00" + f.Variable
}
```

> 上面 `Kind Report SectionKind` 里的空格是排版意外，实际写 `Kind ReportSectionKind`。
>
> 文件顶部 import 加 `"time"`。

`buildReportDigest` 及其四个子函数里构造 `ReportFinding{...Confidence: X}` 的地方，改成 `Judgement: judge(X, "")`，并补 `Origin: OriginSystem`、`Dimension`、`Variable`。用这条找出全部构造点：

```bash
grep -n "ReportFinding{" internal/systems/insights/report_digest.go
```

各处的 `Dimension` 取值分别是：`performanceFindings` 里对比条目用 `"comparisons"`、驱动条目用 `"drivers"`、疲劳条目用 `"fatigue"`、异常条目用 `"anomalies"`、口径警告留空；`experimentFindings` 用 `"experiments"`；`experienceFindings` 与 `recommendationFindings` 留空。`Variable` 取该条说的那个特征键（对比取 `ChangedFeatures[0].Key`，驱动取 `Key`，疲劳与异常取 `AssetID`），取不到就留空。

- [ ] **Step 4: 读取路径上调用 normalize**

在 `internal/systems/insights/mysql_repository.go` 的 `GetReport` 与 `ListReports` 里，反序列化 digest 之后加一段：

```go
	for index := range value.Digest {
		value.Digest[index].normalize()
	}
```

- [ ] **Step 5: 跑测试**

```bash
go build ./... && go test ./internal/systems/insights/... 2>&1 | head -40
```

既有 `report_digest_test.go` 里读 `.Confidence` 的断言不用改（字段被提升了）；构造字面量 `ReportFinding{Confidence: X}` 的要改成 `Judgement: judge(X, "")`。反复跑到全绿。

- [ ] **Step 6: 提交**

```bash
git add internal/systems/insights/report_digest.go internal/systems/insights/findings_test.go internal/systems/insights/mysql_repository.go
git commit -m "feat(insights): 发现记得住来源、维度与三档"
```

---

### Task 2: 复盘草稿按 (项目 + 窗口) 唯一

**Files:**
- Create: `migrations/insights/20260811100000_insight_report_project_window_key.up.sql`
- Modify: `internal/systems/insights/service.go:397-401`（`Repository` 接口）
- Modify: `internal/systems/insights/mysql_repository.go`（新增 `FindDraftByWindow`）

**Interfaces:**
- Consumes: 既有 `InsightReport` / `ReportDraft` / `MySQLRepository`。
- Produces:
  - `Repository` 接口新增 `FindDraftByWindow(context.Context, contract.OrganizationID, contract.ProjectID, string, string) (InsightReport, error)`，找不到返回 `ErrNotFound`。
  - 数据库唯一键 `uq_insight_reports_project_execution_window (organization_id, project_id, execution_id, window_start, window_end)`。

- [ ] **Step 1: 写迁移**

创建 `migrations/insights/20260811100000_insight_report_project_window_key.up.sql`：

```sql
-- 复盘草稿要按 (项目 + 窗口) 唯一，唯一键必须带上 project_id。
--
-- 现在的键是 uq_insight_reports_execution_window (organization_id, execution_id,
-- window_start, window_end)。它是按「报告一定挂在一次投放执行上」建的，那时候
-- execution_id 非空，而执行本身就是项目独占的，所以不带 project_id 也不会撞。
--
-- 「记一笔」改变了这个前提：人在分析页看到一条值得留的结论就按一下，这时候还没有
-- 选投放执行——草稿的 execution_id 是空串。于是同一个组织里两个项目、同一个窗口，
-- 键都是 (org, '', '2026-07-01', '2026-07-30')，第二个项目的第一次记一笔会直接
-- 撞键失败，而错误信息只会说重复键，看不出是跨项目撞的。
--
-- 加上 project_id 之后，execution_id 非空的老行行为完全不变（执行本来就属于一个
-- 项目），空 execution_id 的草稿按项目分开。
ALTER TABLE insight_reports
  DROP INDEX uq_insight_reports_execution_window,
  ADD UNIQUE KEY uq_insight_reports_project_execution_window
    (organization_id, project_id, execution_id, window_start, window_end);
```

- [ ] **Step 2: 跑迁移确认能过**

```bash
go run ./cmd/cookies-migrate
```

Expected: 无错误。若本机没起 MySQL，跳过这一步，在 Task 3 的集成测试里一起验。

- [ ] **Step 3: 加仓储方法**

`internal/systems/insights/service.go` 的 `Repository` 接口，在 `GetReport` 那一行下面加：

```go
	// FindDraftByWindow 按 (项目 + 窗口) 找那份还没提交的复盘草稿。
	// 「记一笔」不问人要往哪记——问了等于要求人在看数据之前先声明意图。
	FindDraftByWindow(context.Context, contract.OrganizationID, contract.ProjectID, string, string) (InsightReport, error)
```

`internal/systems/insights/mysql_repository.go` 在 `GetReport` 下方实现（列名和 `GetReport` 保持一致，先看一眼它的 SELECT 列表再照抄）：

```go
// FindDraftByWindow 只找 draft，不找已确认的。已确认的复盘是定格的，
// 往里加一条新发现等于事后改结论。
func (r MySQLRepository) FindDraftByWindow(ctx context.Context, organizationID contract.OrganizationID,
	projectID contract.ProjectID, windowStart, windowEnd string) (InsightReport, error) {
	var id string
	err := r.DB.QueryRowContext(ctx, `
		SELECT id FROM insight_reports
		WHERE organization_id = ? AND project_id = ? AND window_start = ? AND window_end = ?
		  AND status = ?
		ORDER BY created_at ASC LIMIT 1`,
		string(organizationID), string(projectID), windowStart, windowEnd, string(ReportDraft),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return InsightReport{}, ErrNotFound
	}
	if err != nil {
		return InsightReport{}, err
	}
	return r.GetReport(ctx, organizationID, projectID, id)
}
```

> `r.DB` 的实际字段名以文件里已有的用法为准（先 `grep -n "func (r MySQLRepository)" -A 3 internal/systems/insights/mysql_repository.go | head -20` 看一眼）。`ErrNotFound` 在 `service.go:20` 附近，确认名字后再用。

同时在内存/伪造仓储上补同名方法——先找出来有哪些：

```bash
grep -rln "GetReport(ctx context.Context" internal/systems/insights/
```

- [ ] **Step 4: 编译**

```bash
go build ./... && go test ./internal/systems/insights/...
```

Expected: 全绿（新方法暂时没人调用）。

- [ ] **Step 5: 提交**

```bash
git add migrations/insights/ internal/systems/insights/service.go internal/systems/insights/mysql_repository.go
git commit -m "feat(insights): 复盘草稿按项目与窗口唯一"
```

---

### Task 3: 记一笔

**Files:**
- Create: `internal/systems/insights/findings.go`
- Modify: `internal/systems/insights/findings_test.go`（追加）
- Modify: `internal/systems/insights/service.go`（`PinFindingRequest`、`Service.PinFinding`）
- Modify: `internal/systems/insights/httpapi/server.go`（路由 + handler + `Application` 接口）
- Modify: `api/openapi/insights-v1.yaml`

**Interfaces:**
- Consumes: Task 1 的 `ReportFinding` / `OriginPinned` / `dedupeKey()`；Task 2 的 `FindDraftByWindow`；P0 的 `Judgement`；既有 `GetPerformanceAnalysis`。
- Produces:
  - `type PinFindingRequest struct{ Window MetricWindow; Dimension string; SourceRef string; Variable string; Text string }`，`func (PinFindingRequest) Validate() error`
  - `func (Service) PinFinding(context.Context, contract.ActorContext, contract.ProjectID, PinFindingRequest) (InsightReport, error)`
  - `func findJudgement(PerformanceAnalysis, dimension, sourceRef, variable string) (Judgement, string, bool)` —— 回查那一条的判定与它自己的措辞，找不到返回 false
  - HTTP：`POST /api/insights/v1/projects/{project_id}/findings`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/findings_test.go` 追加：

```go
// 判定不能从请求里传。能传的话，页面上那个 ❓ 就是装饰——前端改一个字段
// 就能把「算不出来」记成「能归因」，而复盘会上没人会回去核。
func TestPinFindingRecomputesTheVerdictFromTheAnalysis(t *testing.T) {
	t.Parallel()

	analysis := PerformanceAnalysis{
		Drivers: []FeatureDriver{{
			Key:       "duration",
			Label:     "时长",
			Value:     "15s",
			Judgement: judge(ConfidenceLowSample, "样本较少的一侧只有 300 次展示。"),
		}},
	}
	got, text, ok := findJudgement(analysis, "drivers", "", "duration")
	if !ok {
		t.Fatal("应该能在驱动因素里找回这一条")
	}
	if got.Verdict != VerdictUnclear {
		t.Errorf("回查到的档位是 %s，期望 %s", got.Verdict, VerdictUnclear)
	}
	if text == "" {
		t.Error("回查应该同时给出这条自己的措辞，让人不用自己编")
	}
}

// 请求指到一条分析里不存在的结论时，必须拒绝，而不是记一条没有判定的发现。
// 记下去的话，复盘页上会出现一条既没有档位也没有出处的文字，谁也说不清它从哪来。
func TestPinFindingRejectsAConclusionThatIsNotOnTheScreen(t *testing.T) {
	t.Parallel()

	analysis := PerformanceAnalysis{}
	if _, _, ok := findJudgement(analysis, "drivers", "", "duration"); ok {
		t.Error("分析结果里没有这一条，不该回查成功")
	}
}

func TestPinFindingRequestValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	valid := PinFindingRequest{
		Window:    MetricWindow{Start: now.AddDate(0, 0, -30), End: now},
		Dimension: "drivers",
		Variable:  "duration",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法请求被拒了：%v", err)
	}

	noWindow := valid
	noWindow.Window = MetricWindow{}
	if err := noWindow.Validate(); err == nil {
		t.Error("没有窗口的记一笔应该被拒——不知道往哪份复盘草稿记")
	}

	badDimension := valid
	badDimension.Dimension = "whatever"
	if err := badDimension.Validate(); err == nil {
		t.Error("维度必须是六个视图之一")
	}

	noSubject := valid
	noSubject.Variable, noSubject.SourceRef = "", ""
	if err := noSubject.Validate(); err == nil {
		t.Error("变量和来源引用至少要有一个——两个都没有就回查不到任何一条")
	}
}
```

文件顶部 import 补 `"time"`。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestPinFinding' -v
```

Expected: 编译失败，`undefined: findJudgement`、`undefined: PinFindingRequest`。

- [ ] **Step 3: 写 findings.go**

创建 `internal/systems/insights/findings.go`：

```go
package insights

import "strings"

// 「记一笔」是分析页唯一的写操作。
//
// 分析页是自由探索的地方：换窗口、换视图、来回比。这种地方不能承担「确认结论」的
// 职责——人还在看，一个误点就把一条没想清楚的东西沉淀成了经验。所以分析页只能做
// 一件事：把这一条钉进本轮复盘草稿，等复盘的时候再逐条决定要不要提交。
//
// 判定不收前端的。请求只说「哪个窗口、哪个视图、哪条」，档位由后端回头去那次分析
// 结果里找回来。能传判定的话，页面上标的三档就是装饰。

// analysisDimensions 是六个视图的键。记一笔只能记在这六个里面——
// 记在别的地方，复盘页就不知道该把它归到哪一块。
var analysisDimensions = map[string]ReportSectionKind{
	"overview":    SectionAssetPerformance,
	"comparisons": SectionAssetPerformance,
	"trends":      SectionAssetPerformance,
	"fatigue":     SectionAssetPerformance,
	"anomalies":   SectionAssetPerformance,
	"drivers":     SectionAssetPerformance,
}

// findJudgement 在一次分析结果里找回某一条的判定和它自己的措辞。
//
// 返回措辞而不是让前端把屏幕上的文字传上来：屏幕上的文字是前端拼的，
// 传上来就等于把措辞的权威交给了前端，两处措辞迟早不一样。
func findJudgement(analysis PerformanceAnalysis, dimension, sourceRef, variable string) (Judgement, string, bool) {
	switch dimension {
	case "overview":
		if analysis.Judgement.Verdict == "" {
			return Judgement{}, "", false
		}
		return analysis.Judgement, analysis.Judgement.Note, true
	case "comparisons":
		for _, item := range analysis.Comparisons {
			if !matchesSubject(sourceRef, variable, item.VariantAssetID, firstFeatureKey(item.ChangedFeatures)) {
				continue
			}
			return item.Judgement, comparisonText(item), true
		}
	case "trends":
		for _, item := range analysis.Trends {
			if !matchesSubject(sourceRef, variable, item.AssetID, "") {
				continue
			}
			return item.Judgement, item.AssetTitle + "：" + item.Note, true
		}
	case "fatigue":
		for _, item := range analysis.Fatigue {
			if !matchesSubject(sourceRef, variable, item.AssetID, "") {
				continue
			}
			return item.Judgement, item.AssetTitle + "：" + item.Note, true
		}
	case "anomalies":
		for _, item := range analysis.Anomalies {
			if !matchesSubject(sourceRef, variable, item.AssetID, item.Date) {
				continue
			}
			return item.Judgement, item.Date + " " + item.Metric + "：" + item.Note, true
		}
	case "drivers":
		for _, item := range analysis.Drivers {
			if !matchesSubject(sourceRef, variable, "", item.Key) {
				continue
			}
			return item.Judgement, item.Label + " = " + item.Value + "：" + item.Note, true
		}
	}
	return Judgement{}, "", false
}

// matchesSubject：请求给了哪个就按哪个匹配，两个都给就都要对上。
func matchesSubject(sourceRef, variable, itemRef, itemVariable string) bool {
	if sourceRef != "" && sourceRef != itemRef {
		return false
	}
	if variable != "" && variable != itemVariable {
		return false
	}
	return sourceRef != "" || variable != ""
}

func firstFeatureKey(diffs []FeatureDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	return diffs[0].Key
}

func comparisonText(item VariantComparison) string {
	changed := make([]string, 0, len(item.ChangedFeatures))
	for _, diff := range item.ChangedFeatures {
		changed = append(changed, diff.Label)
	}
	subject := "无差异"
	if len(changed) > 0 {
		subject = strings.Join(changed, "、")
	}
	return item.BaselineTitle + " vs " + item.VariantTitle + "（改了：" + subject + "）：" + item.Note
}
```

- [ ] **Step 4: 加请求类型与服务方法**

`internal/systems/insights/service.go`，在 `CreateReportRequest` 下方加：

```go
// PinFindingRequest 是「记一笔」的入参。
//
// 注意这里**没有** confidence 也没有 verdict：判定是后端回查出来的。
// Text 也是可选的——人可以补一句自己的话，但不补的话用系统给的措辞，
// 而不是让前端把屏幕上的字传上来。
type PinFindingRequest struct {
	Window    MetricWindow `json:"window"`
	Dimension string       `json:"dimension"`
	SourceRef string       `json:"source_ref,omitempty"`
	Variable  string       `json:"variable,omitempty"`
	// Text 是人自己补的一句话，最多 500 字。留空则用系统给这一条的措辞。
	Text string `json:"text,omitempty"`
}

func (r PinFindingRequest) Validate() error {
	if r.Window.Start.IsZero() || r.Window.End.IsZero() || r.Window.End.Before(r.Window.Start) {
		return ErrInvalidRequest
	}
	if _, ok := analysisDimensions[r.Dimension]; !ok {
		return ErrInvalidRequest
	}
	// 两个主语都空就回查不到任何一条，只会记下一条没有出处的文字。
	if strings.TrimSpace(r.SourceRef) == "" && strings.TrimSpace(r.Variable) == "" &&
		r.Dimension != "overview" {
		return ErrInvalidRequest
	}
	if len(r.Text) > 500 {
		return ErrInvalidRequest
	}
	return nil
}
```

在 `CreateReport` 下方加服务方法：

```go
// PinFinding 把分析页上的一条结论钉进本轮复盘草稿。
//
// 草稿是自动建的：不问人「要往哪份复盘记」。问了等于要求人在看数据之前先声明意图
// ——而记一笔的价值恰恰在于人是看到了才决定要留的。
func (s Service) PinFinding(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request PinFindingRequest) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return InsightReport{}, err
	}
	if err := request.Validate(); err != nil {
		return InsightReport{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return InsightReport{}, err
	}

	analysis, err := s.GetPerformanceAnalysis(ctx, actor, projectID, request.Window)
	if err != nil {
		return InsightReport{}, err
	}
	judgement, text, ok := findJudgement(analysis, request.Dimension, request.SourceRef, request.Variable)
	if !ok {
		// 屏幕上没有这一条，说明前端传的主语和当前窗口对不上——多半是窗口换了
		// 但按钮没重渲染。宁可报错，也不记一条没有判定的发现。
		return InsightReport{}, ErrInvalidRequest
	}
	if custom := strings.TrimSpace(request.Text); custom != "" {
		text = custom
	}

	now := s.now()
	finding := ReportFinding{
		Kind:      analysisDimensions[request.Dimension],
		Text:      text,
		Judgement: judgement,
		Origin:    OriginPinned,
		Dimension: request.Dimension,
		Variable:  request.Variable,
		SourceRef: request.SourceRef,
		PinnedBy:  actor.Principal.ID,
		PinnedAt:  &now,
	}

	windowStart := request.Window.Start.Format("2006-01-02")
	windowEnd := request.Window.End.Format("2006-01-02")

	draft, err := s.Repository.FindDraftByWindow(ctx, actor.OrganizationID, projectID, windowStart, windowEnd)
	if errors.Is(err, ErrNotFound) {
		return s.createDraftWithFinding(ctx, actor, projectID, windowStart, windowEnd, finding, now)
	}
	if err != nil {
		return InsightReport{}, err
	}

	// 同一条记两次是常见的误操作（换了个视图又看到同一个结论）。
	// 覆盖而不是追加：人第二次记的时候补的那句话，应该是他现在想说的那句。
	digest := make([]ReportFinding, 0, len(draft.Digest)+1)
	replaced := false
	for _, existing := range draft.Digest {
		if existing.Origin == OriginPinned && existing.dedupeKey() != "" &&
			existing.dedupeKey() == finding.dedupeKey() {
			digest, replaced = append(digest, finding), true
			continue
		}
		digest = append(digest, existing)
	}
	if !replaced {
		digest = append(digest, finding)
	}
	return s.Repository.UpdateReportDigest(ctx, actor.OrganizationID, projectID, draft.ID, draft.Version, digest, now)
}

// createDraftWithFinding 建一份只有这一条发现的空草稿。
//
// 它不走 CreateReport：CreateReport 必须挂一次投放执行，而记一笔发生在人还在看
// 数据的时候，那时候还没到「这份复盘算哪次投放」这个问题。执行 ID 在复盘页提交
// 之前补上。
func (s Service) createDraftWithFinding(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, windowStart, windowEnd string,
	finding ReportFinding, now time.Time) (InsightReport, error) {
	id, err := s.idGenerator()("insightreport")
	if err != nil {
		return InsightReport{}, err
	}
	return s.Repository.CreateReport(ctx, InsightReport{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Status: ReportDraft, Digest: []ReportFinding{finding},
		WindowStart: windowStart, WindowEnd: windowEnd,
		Findings:  []string{},
		Version:   1,
		CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
}
```

> `CreateReport` 现在要求 `ExecutionID` 非空并去 Delivery 校验快照——`createDraftWithFinding` 绕过了它，这是**有意的**：两种草稿的来源不同（一种从投放执行来，一种从人记的发现来）。数据库列 `execution_id` 需要允许空串；先确认 `insight_reports.execution_id` 的定义，若是 `NOT NULL` 无默认值，在 Task 2 的迁移里补一句 `MODIFY COLUMN execution_id VARCHAR(64) ... NOT NULL DEFAULT ''`。

`service.go` 顶部 import 确认有 `"errors"`、`"strings"`、`"time"`。

- [ ] **Step 5: 跑测试**

```bash
go build ./... && go test ./internal/systems/insights/ -run 'TestPinFinding' -v
```

Expected: 三个测试 PASS。

- [ ] **Step 6: 挂 HTTP 路由**

`internal/systems/insights/httpapi/server.go`：

1. `Application` 接口在 `CreateReport` 附近加一行：

```go
	// 记一笔（分析页唯一的写操作）。判定不在入参里：能传的话页面上标的三档就是装饰。
	PinFinding(context.Context, contract.ActorContext, contract.ProjectID, insights.PinFindingRequest) (insights.InsightReport, error)
```

2. `New()` 里加路由：

```go
	server.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/findings", server.pinFinding)
```

3. handler（照 `createReport` 的窗口处理写法——线上传的是 `2026-07-01` 这样的日子，不是时间戳）：

```go
func (s *Server) pinFinding(writer http.ResponseWriter, request *http.Request) {
	// 窗口和 createReport 用同一套解码：人看到的是两个日期，记下来的必须是同两个日期。
	// 中间过一道时区换算就会差一天，而两边显示的还是同一个区间。
	var body struct {
		insights.PinFindingRequest
		Window struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"window"`
	}
	if !decode(writer, request, &body) {
		return
	}
	payload := body.PinFindingRequest
	window, ok := parseDayWindow(writer, body.Window.Start, body.Window.End)
	if !ok {
		return
	}
	payload.Window = window

	actor, projectID, ok := actorAndProject(writer, request)
	if !ok {
		return
	}
	report, err := s.app.PinFinding(request.Context(), actor, projectID, payload)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, report)
}
```

> `parseDayWindow` / `actorAndProject` / `writeError` / `writeJSON` / `decode` 的真实名字以 `createReport`（`server.go:122` 起）里的用法为准，先读一遍那个函数再照抄，不要按上面的名字硬写。

- [ ] **Step 7: 契约与端到端验证**

`api/openapi/insights-v1.yaml` 加接口与 schema：

```yaml
  /api/insights/v1/projects/{project_id}/findings:
    post:
      summary: 记一笔——把分析页上的一条结论钉进本轮复盘草稿
      description: |
        分析页唯一的写操作。请求体里**没有** confidence / verdict：判定由后端拿
        (window, dimension, source_ref, variable) 回到那次分析结果里找回来。
        目标复盘草稿按 (项目 + 窗口) 自动 find-or-create，不需要先建复盘。
        屏幕上不存在这一条时返回 400。
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [window, dimension]
              properties:
                window:
                  type: object
                  required: [start, end]
                  properties:
                    start: { type: string, example: '2026-07-01' }
                    end: { type: string, example: '2026-07-30' }
                dimension:
                  type: string
                  enum: [overview, comparisons, trends, fatigue, anomalies, drivers]
                source_ref: { type: string, description: '素材 ID 或异常日期。与 variable 至少给一个。' }
                variable: { type: string, description: '特征键。与 source_ref 至少给一个。' }
                text: { type: string, maxLength: 500, description: '人自己补的一句话。留空则用系统措辞。' }
      responses:
        '200':
          description: 记入后的复盘草稿全文
          content:
            application/json:
              schema: { $ref: '#/components/schemas/InsightReport' }
```

`ReportFinding` schema 补 `origin` / `dimension` / `variable` / `pinned_by` / `pinned_at` / `verdict` / `verdict_label` / `upgrade`。

- [ ] **Step 8: 跑全量并提交**

```bash
go build ./... && go test ./internal/systems/insights/...
```

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml migrations/insights/
git commit -m "feat(insights): 记一笔把分析页结论钉进复盘草稿"
```

---

### Task 4: 分析页拆成壳 + 六个视图

**Files:**
- Create: `src/components/insight/analysis/AnalysisPage.tsx`
- Create: `src/components/insight/analysis/OverviewView.tsx`
- Create: `src/components/insight/analysis/ComparisonView.tsx`
- Create: `src/components/insight/analysis/TrendView.tsx`
- Create: `src/components/insight/analysis/FatigueView.tsx`
- Create: `src/components/insight/analysis/AnomalyView.tsx`
- Create: `src/components/insight/analysis/DriverView.tsx`
- Create: `src/components/insight/analysis/index.ts`
- Reference: `src/components/PostLaunchAnalysisPage.tsx`（整页搬运的来源，本任务不删）
- Reference: `src/components/PostLaunchOverviewPage.tsx`（视图一的来源）

**Interfaces:**
- Consumes: P0 的 `VerdictBadge` / `NotEnoughSample` / `HowItWasComputed` / `PinFindingButton`；`api.getPerformanceAnalysis`。
- Produces:
  - `export function AnalysisPage({ view }: { view: AnalysisView })`
  - `export type AnalysisView = 'overview' | 'comparisons' | 'trends' | 'fatigue' | 'anomalies' | 'drivers'`
  - 六个视图组件统一签名：`({ analysis, onPin }: { analysis: ApiPerformanceAnalysis; onPin: (target: PinTarget) => void })`
  - `export interface PinTarget { dimension: AnalysisView; source_ref?: string; variable?: string }`

- [ ] **Step 1: 建壳**

创建 `src/components/insight/analysis/AnalysisPage.tsx`：

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiPerformanceAnalysis } from '../../../data/api'
import type { DataState } from '../../../types'
import { StateBoundary } from '../../StateBoundary'
import { VerdictBadge } from '../shared'
import { usePinFinding, type PinTarget } from './usePinFinding'
import { AnomalyView } from './AnomalyView'
import { ComparisonView } from './ComparisonView'
import { DriverView } from './DriverView'
import { FatigueView } from './FatigueView'
import { OverviewView } from './OverviewView'
import { TrendView } from './TrendView'

/**
 * 「分析」入口。六个视图自由探索，每一屏顶上标三档。
 *
 * 六个视图共用一次 performance-analysis 请求：拆成六次的话，「趋势里看到的」
 * 和「疲劳里算的」会来自两份数据，对不上的时候没人解释得清哪个对。
 *
 * 这一页对结论**只读**。唯一的写是「记一笔」——把这一条钉进本轮复盘草稿，
 * 要不要沉淀成经验，是复盘页上另一次明确的决定。
 */
export type AnalysisView = 'overview' | 'comparisons' | 'trends' | 'fatigue' | 'anomalies' | 'drivers'

const rangeOptions = [
  { label: '近 7 天', days: 7 },
  { label: '近 30 天', days: 30 },
  { label: '近 90 天', days: 90 },
]

export function AnalysisPage({ view }: { view: AnalysisView }) {
  const { currentProject } = useProject()
  const [days, setDays] = useState(30)
  const [state, setState] = useState<DataState>('loading')
  const [analysis, setAnalysis] = useState<ApiPerformanceAnalysis | null>(null)

  const window = useMemo(() => {
    const end = new Date()
    const start = new Date(end)
    start.setDate(start.getDate() - days + 1)
    return { start: isoDay(start.toISOString()), end: isoDay(end.toISOString()) }
  }, [days])

  const load = useCallback(() => {
    if (!currentProject.id) return
    setState('loading')
    api.getPerformanceAnalysis(currentProject.id, window.start, window.end)
      .then(result => { setAnalysis(result); setState('ready') })
      .catch(() => { setAnalysis(null); setState('error') })
  }, [currentProject.id, window.start, window.end])

  useEffect(load, [load])

  const { pin, pinned, pinning } = usePinFinding(currentProject.id, window, load)

  return <section className="analysis-page">
    <header className="analysis-head">
      <div className="analysis-range">
        {rangeOptions.map(option => (
          <button key={option.days} type="button"
            className={option.days === days ? 'chip active' : 'chip'}
            onClick={() => setDays(option.days)}>{option.label}</button>
        ))}
        <button type="button" className="chip" onClick={load} aria-label="重新读取">
          <RefreshCw size={14}/>
        </button>
      </div>
      {/* 屏级档位放在最上面：先知道这一屏能信到什么程度，再看下面的数字。 */}
      {analysis ? <VerdictBadge judgement={analysis.judgement}/> : null}
    </header>

    <StateBoundary state={state} onRetry={load}>
      {analysis ? renderView(view, analysis, pin, pinned, pinning) : null}
    </StateBoundary>
  </section>
}

function renderView(
  view: AnalysisView,
  analysis: ApiPerformanceAnalysis,
  onPin: (target: PinTarget) => void,
  pinned: ReadonlySet<string>,
  pinning: boolean,
) {
  const props = { analysis, onPin, pinned, pinning }
  switch (view) {
    case 'comparisons': return <ComparisonView {...props}/>
    case 'trends': return <TrendView {...props}/>
    case 'fatigue': return <FatigueView {...props}/>
    case 'anomalies': return <AnomalyView {...props}/>
    case 'drivers': return <DriverView {...props}/>
    default: return <OverviewView {...props}/>
  }
}

/** 后端收的是 2006-01-02，这里把 ISO 时间戳切回同一个口径。 */
function isoDay(value: string): string {
  return value.slice(0, 10)
}
```

- [ ] **Step 2: 写记一笔的前端状态**

创建 `src/components/insight/analysis/usePinFinding.ts`：

```ts
import { useCallback, useState } from 'react'
import { api } from '../../../data/api'
import type { AnalysisView } from './AnalysisPage'

export interface PinTarget {
  dimension: AnalysisView
  source_ref?: string
  variable?: string
}

/** 前端本地的已记标记键，和后端 dedupeKey 的构成一致（维度 + 变量）。 */
export function pinKey(target: PinTarget): string {
  return `${target.dimension} ${target.variable ?? ''} ${target.source_ref ?? ''}`
}

/**
 * 记一笔的状态。乐观标记「已记」，失败了再撤回——这个按钮一天要按几十次，
 * 每次都等一个来回会让人以为页面卡住。
 */
export function usePinFinding(
  projectId: string,
  window: { start: string; end: string },
  onPinned: () => void,
) {
  const [pinned, setPinned] = useState<ReadonlySet<string>>(new Set())
  const [pinning, setPinning] = useState(false)

  const pin = useCallback((target: PinTarget) => {
    if (!projectId) return
    const key = pinKey(target)
    setPinned(previous => new Set(previous).add(key))
    setPinning(true)
    api.pinFinding(projectId, { window, ...target })
      .then(() => { onPinned() })
      .catch(() => {
        // 撤回标记，让人看得出这一下没成。静默失败会让他以为记上了，
        // 复盘的时候才发现那条不在。
        setPinned(previous => {
          const next = new Set(previous)
          next.delete(key)
          return next
        })
      })
      .finally(() => setPinning(false))
  }, [projectId, window, onPinned])

  return { pin, pinned, pinning }
}
```

- [ ] **Step 3: 加 api 客户端方法**

`src/data/api.ts`，在 `createInsightReport`（`:4134` 附近）旁边加：

```ts
  pinFinding: (projectId: string, body: {
    window: { start: string; end: string }
    dimension: string
    source_ref?: string
    variable?: string
    text?: string
  }) => request<ApiInsightReport>(`${insightProjectPath(projectId)}/findings`, 'POST', body),
```

`ApiPerformanceAnalysis` 类型补 `judgement: Judgement`（P0 Task 6 已加，确认在）；`ApiReportFinding` 补 `origin: 'system' | 'pinned'`、`dimension?: string`、`variable?: string`、`pinned_by?: string`、`pinned_at?: string`。

- [ ] **Step 4: 搬六个视图**

把 `src/components/PostLaunchAnalysisPage.tsx` 里五个列表视图的渲染逻辑，逐个搬进对应文件，每个文件只放一个视图。搬运时做三件替换（其余照抄，包括那些解释「为什么这么算」的中文文案——它们是这一页可信度的来源）：

1. 所有 `置信{confidenceLabels[item.confidence]}` → `<VerdictBadge judgement={item}/>`
2. 空列表分支的 `emptyHints[...]` 文案 → `<NotEnoughSample judgement={analysis.judgement}/>`，并把原来的 hint 文案作为 `judgement.note` 的补充显示在下面（这些 hint 说清了「缺什么」，不能丢）
3. 每一行末尾加 `<PinFindingButton onPin={() => onPin({...})} pinned={pinned.has(pinKey({...}))}/>`

各视图的 `PinTarget` 构成：

| 视图 | dimension | source_ref | variable |
|---|---|---|---|
| 指标总览 | `overview` | — | — |
| 素材对比 | `comparisons` | `item.variant_asset_id` | `item.changed_features[0]?.key` |
| 趋势 | `trends` | `item.asset_id` | — |
| 疲劳 | `fatigue` | `item.asset_id` | — |
| 异常 | `anomalies` | `item.asset_id` | `item.date` |
| 驱动因素 | `drivers` | — | `item.key` |

> 注意：**归因不可用的变量不给记一笔**。素材对比里 `item.changed_features.some(diff => !diff.admissible)` 为真时，`PinFindingButton` 不传 `onPin`（变成禁用态），并在 title 里说明「这条差异里有模型推断的变量，不能进结论」。这是 P0 Task 3 定下的准入规则在界面上的落点。

`src/components/insight/analysis/index.ts`：

```ts
export { AnalysisPage, type AnalysisView } from './AnalysisPage'
```

- [ ] **Step 5: 构建**

```bash
npm run build
```

Expected: 通过。

- [ ] **Step 6: 提交**

```bash
git add src/components/insight/analysis/ src/data/api.ts
git commit -m "feat(insights-web): 分析入口拆成壳与六个视图"
```

---

### Task 5: 记一笔在页面上真的能按

**Files:**
- Modify: `src/components/insight/analysis/*.tsx`（接线，Task 4 已埋好）
- Modify: `src/styles.css`（分析页布局）

**Interfaces:**
- Consumes: Task 3 的 `POST .../findings`；Task 4 的 `usePinFinding`。
- Produces: 无新导出。

- [ ] **Step 1: 起服务**

用 `preview_start` 起前端（`.claude/launch.json` 的 dev 配置）。同时确认后端在跑：

```bash
go run ./cmd/cookies-migrate && go run ./cmd/cookies-seed
```

- [ ] **Step 2: 在浏览器里走一遍**

1. 打开「分析 · 驱动因素」，确认每行都有档位徽章和「记一笔」按钮。
2. 按一下某行的「记一笔」，确认按钮变成「已记一笔」且禁用。
3. 用 `preview_network` 确认 `POST /api/insights/v1/projects/.../findings` 返回 200，响应体里 `digest` 有一条 `origin: "pinned"` 且带 `verdict`。
4. 用 `preview_console_logs` 确认没有报错。
5. 换一个窗口（近 7 天）再按一次，确认建的是另一份草稿（响应体的 `window_start` 不同、`id` 不同）。
6. 找一条 `verdict` 是 `unclear` 的记一下，确认存进去的 `verdict` 也是 `unclear` ——不是被前端改成别的。
7. 找一条含 `admissible: false` 变量的素材对比，确认「记一笔」是禁用的。

- [ ] **Step 3: 补样式**

在 `src/styles.css` 追加分析页需要的几条（`.analysis-page` / `.analysis-head` / `.analysis-range` / `.chip`）。若 `.chip` 已存在（`grep -n "\.chip" src/styles.css`）就复用，不要重复定义。

- [ ] **Step 4: 提交**

```bash
git add src/components/insight/analysis/ src/styles.css
git commit -m "feat(insights-web): 记一笔在分析页接通后端"
```

---

### Task 6: 导航从「投后分析 + 实验中心」收敛成「分析」

**Files:**
- Modify: `src/data/navigation.ts:43-46`（`performance` 与 `experiments` 两条）
- Modify: `src/lib/router.ts:55`、`src/App.tsx:40`（落地页）
- Modify: `src/components/Pages.tsx` 或分发处（把 `performance` 的渲染指向 `AnalysisPage`）

**Interfaces:**
- Consumes: Task 4 的 `AnalysisPage`。
- Produces: 导航里出现 id 为 `analysis` 的入口，六个视图；`performance` 与 `experiments` 两条消失。

> **这一步修改现有导航结构，属于「修改现有文件的核心逻辑」。开始前必须向使用者确认。**

- [ ] **Step 1: 改导航条目**

`src/data/navigation.ts` 里把 `performance` 和 `experiments` 两条整体替换为一条：

```ts
      // 「分析」= 原投后分析（六视图）+ 原实验中心（作为 👁 的升级通道）。
      // 合并的理由：实验不是一个独立的地方，它是「只是观察」升成「能归因」的
      // 那一步。摆成独立入口，人就得先意识到自己需要一个实验，才会想到点进去。
      {
        id: 'analysis', label: '分析', icon: TrendingUp, group: '工作', layout: 'analysis',
        description: '一轮投放跑完，为什么是这个结果。六个视图自由看，每一屏标清能不能归因；看到值得留的按「记一笔」。',
        views: ['指标总览', '素材对比', '趋势', '疲劳', '异常', '驱动因素'],
      },
```

`FlaskConical` **保留**——下一步的常驻「实验」按钮要用它。

- [ ] **Step 1b: 分析页顶部工具栏加常驻「实验」按钮**

`AnalysisPage.tsx` 的工具栏（六个视图共用的那一条）末尾加：

```tsx
        {/* 实验中心没有侧栏入口了。只从 👁 徽章的「做个实验」跳进去的话，
            一屏里一个 👁 都没出现时这个页面就完全不可达——功能还在，路断了。
            这个按钮常驻，六个视图下都在。 */}
        <button type="button" className="toolbar-link" onClick={() => onOpenExperiments()}>
          <FlaskConical size={14} aria-hidden />
          实验
        </button>
```

`onOpenExperiments` 由 `AnalysisPage` 的 props 传入，路由到现有的 `ExperimentCenterPage`。**它和徽章上那条通道并存**：徽章解决「此刻这条结论该怎么升级」，工具栏解决「我想去看看实验」。

- [ ] **Step 2: 改落地页**

`src/lib/router.ts:55` 和 `src/App.tsx:40` 里 insight 的落地页从 `'prelaunch'` 改成 `'analysis'`——「投前洞察」在 P4 会并进「经验」，那之前它仍然可达，只是不再是默认落点。分析是日常最常进的那一屏。

- [ ] **Step 3: 改渲染分发**

找出 `performance` 现在是在哪里被渲染的：

```bash
grep -rn "'performance'\|\"performance\"" src/components/ src/App.tsx | grep -v api.ts
```

把那一处指向 `<AnalysisPage view={...}/>`，`view` 由当前二级视图名映射而来：

```ts
const analysisViews: Record<string, AnalysisView> = {
  指标总览: 'overview',
  素材对比: 'comparisons',
  趋势: 'trends',
  疲劳: 'fatigue',
  异常: 'anomalies',
  驱动因素: 'drivers',
}
```

- [ ] **Step 4: 构建并在浏览器里确认**

```bash
npm run build
```

用 `preview_start` 打开，确认：洞察侧栏里「分析」在最上面、六个视图都能切；原「投后分析」和「实验中心」不在了；点洞察系统默认落在「分析 · 指标总览」。用 `preview_snapshot` 核对侧栏文字。

**还要专门验一条：把六个视图挨个切一遍，工具栏上的「实验」按钮每一屏都在，点进去能打开实验中心。** 这一条是防断路的——侧栏入口删了之后，它是实验页唯一稳定可达的路径。

- [ ] **Step 5: 跑全量**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

- [ ] **Step 6: 提交**

```bash
git add src/data/navigation.ts src/lib/router.ts src/App.tsx src/components/
git commit -m "feat(insights-web): 投后分析与实验中心收敛成「分析」入口"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档「模块一 · 分析」与第 2a 期：

| 规格要求 | 落在 |
|---|---|
| 六视图自由探索，共用一次取数 | Task 4 Step 1（`AnalysisPage` 一次请求，六个视图共用） |
| 每屏自动标三档 | Task 4 Step 1（屏级徽章）+ Step 4（每行徽章） |
| 分析页只读，唯一写操作是记一笔 | Global Constraints + Task 3（只加了 `POST /findings` 一个写接口） |
| 记一笔落在 (项目 + 窗口) 的草稿上 | Task 2（唯一键）+ Task 3（`FindDraftByWindow` + `createDraftWithFinding`） |
| 草稿自动创建，不问人 | Task 3 Step 4 的 `createDraftWithFinding` |
| 判定不可由前端指定 | Task 3 Step 1 的两个测试 + `PinFindingRequest` 里没有档位字段 |
| ● 我记的 / ○ 系统补的 | Task 1 的 `FindingOrigin` |
| 去重键 = 维度 + 变量 | Task 1 的 `dedupeKey()` + Task 3 Step 4 的覆盖逻辑 |
| 实验中心并入分析 | Task 6 |
| 归因不认模型推断的变量 | Task 4 Step 4 的禁用规则 |

**未覆盖且是有意的：**

- **草稿在首次被碰之前不出现在复盘列表里**、**空草稿 30 天清理** —— 属于复盘列表的行为，放 P2。本期记一笔建出来的草稿会直接出现在旧的报告中心列表里，这是过渡期的已知现象，P2 修掉。
- **实验中心的功能本身**（创建实验、附挂素材、结论）—— 把实验重做成分析页内嵌的一步，依赖 P2 复盘对「升级通道」的完整定义，不在本期。实验页面沿用现有 `ExperimentCenterPage`。

  > **但入口不能只挂在徽章上。** 只从 👁 徽章的「做个实验」跳进去的话，一屏里一个 👁 都没出现时这个页面就没有任何路径可达——功能还在，路断了。所以 Task 6 除了删导航条目，还要在 `AnalysisPage` 顶部工具栏放一个**常驻**的「实验」按钮（`FlaskConical` 图标 + 文字，六个视图下都在），点开 `ExperimentCenterPage`。徽章上那条通道保留，它解决的是「此刻这条结论该怎么升级」；工具栏这个解决的是「我想去看看实验」。两条路通向同一页，不冲突。
  >
  > 因此 Task 6 Step 1 的 `FlaskConical` **不要**从 import 里删——工具栏还要用。
- **相似素材**（❓ 的升级通道）—— 后端能力在 P3 素材，本期 `NotEnoughSample` 的「找相似素材」按钮不传 `onFindSimilar`，保持不可点，而不是点了没反应。

**2. 占位扫描** —— 无 TBD / TODO / 「类似 Task N」。Task 4 Step 4 用表格给出六个视图各自的 `PinTarget` 构成而不是「照着写」；Task 3 Step 6 明确要求先读 `createReport` 再照抄辅助函数名，而不是假设名字。

**3. 类型一致性** —— `PinTarget.dimension` 的六个取值、后端 `analysisDimensions` 的六个键、OpenAPI `dimension` 枚举、Task 4 表格里的六行，四处逐字核对一致。`pinKey`（前端）与 `dedupeKey`（后端）的构成刻意不同：前端多带 `source_ref`，因为同一个变量在不同素材上的行要分别显示「已记」；后端只按维度 + 变量去重，因为复盘会上同一个变量说两遍就是重复。这个差异是有意的，已在 `usePinFinding.ts` 的注释里写明。`FindingOrigin` 的 `"system"` / `"pinned"` 在 Go、TS、OpenAPI 三处一致。

---

## 依赖关系

前置：P0 全部六个任务。
后继：P2 复盘依赖本计划的 `ReportFinding` 新字段和 `PinFinding`；P3 素材的「找相似素材」会回填 `NotEnoughSample` 的 `onFindSimilar`。
