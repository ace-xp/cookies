# 素材洞察 · 入口二「复盘」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「报告中心」变成「复盘」：一轮结束时打开草稿，看见自己一路记的几笔，系统把漏掉的补齐，逐条决定留不留，提交，把值得复用的沉淀成经验。

**Architecture:** 复盘草稿在分析页记第一笔的时候就已经存在了（P1）。这一期做的是它后半段：提交时才把系统发现**定格**进去——不是打开就补，因为草稿是活的，人还在往里记；定格发生在「这一轮到此为止」那一刻。合并按 (维度 + 变量) 去重，人记过的系统不再补一条。提交同时补上「这份复盘算哪次投放」，这是全流程唯一必须选投放执行的地方。

**Tech Stack:** Go 1.22+（`net/http` ServeMux、标准库 `testing`）、MySQL 8、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`。

## Global Constraints

- 前置依赖：**P0 地基**与 **P1 分析**都必须先完成。本计划用 `Judgement`、`ReportFinding.Origin`、`dedupeKey()`、`FindDraftByWindow`。
- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- 已确认的复盘**不可改**。确认之后的任何写路径都必须返回 `ErrInvalidState`——报告是要被引用、被追溯的，改一条等于让引用它的人手上那份变成假的。
- 发现**不物理删除**。人删掉的标 `Dropped=true` 留着，这是评估「系统补得准不准」的唯一依据。
- 系统发现只在提交那一刻定格一次，之后不再重算。
- 名词按 P0 的名词表：这一页叫「复盘」不叫报告中心；「提交复盘」不叫确认报告；「沉淀成经验」保留原词。
- 迁移文件放 `migrations/insights/`，命名 `YYYYMMDDHHMMSS_<描述>.up.sql`，文件头必须有中文注释说明为什么要改。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `internal/systems/insights/report_digest.go` | 合并人记的与系统补的 | 改 |
| `internal/systems/insights/report_digest_test.go` | 合并去重的约束 | 改 |
| `internal/systems/insights/review.go` | 提交复盘：补执行、定格、确认 | 新建 |
| `internal/systems/insights/review_test.go` | 提交复盘的全部行为约束 | 新建 |
| `internal/systems/insights/service.go` | `SubmitReviewRequest`、`Service.SubmitReview`、列表过滤、清理 | 改 |
| `internal/systems/insights/mysql_repository.go` | `SubmitReport`（补执行 + 确认，一个事务）、`PurgeEmptyDrafts` | 改 |
| `internal/systems/insights/httpapi/server.go` | `POST .../reports/{id}/submit` | 改 |
| `api/openapi/insights-v1.yaml` | 提交复盘接口、`ReportFinding` 已在 P1 补齐 | 改 |
| `src/components/insight/review/ReviewPage.tsx` | 复盘入口的壳：草稿在左，正文在右 | 新建 |
| `src/components/insight/review/DraftPanel.tsx` | 一份草稿的三段：我记的 / 系统补的 / 提交 | 新建 |
| `src/components/insight/review/FindingRow.tsx` | 一条发现：来源点、档位、删/恢复 | 新建 |
| `src/components/insight/review/SubmitReviewAction.tsx` | 提交前选投放执行（由 `FreezeReportAction` 改写而来） | 新建 |
| `src/components/insight/review/HarvestPanel.tsx` | 沉淀成经验 | 新建 |
| `src/components/insight/review/index.ts` | 出口 | 新建 |
| `src/data/api.ts` | `submitReview` | 改 |
| `src/data/navigation.ts` | 「报告中心」→「复盘」 | 改 |

> **需要确认的破坏性动作：** Task 2 改 `ConfirmReport` 的调用路径、Task 5 删导航条目、以及最终删除 `ReportCenterPage.tsx` / `FreezeReportAction.tsx`，都属于「修改现有核心逻辑」或「删除文件」。执行到那几步之前必须先向使用者确认。在得到确认之前保留旧文件不引用即可。

---

### Task 1: 人记过的，系统不再补一条

**Files:**
- Modify: `internal/systems/insights/report_digest.go`（在 `buildReportDigest` 下方加 `mergeFindings`）
- Modify: `internal/systems/insights/report_digest_test.go`

**Interfaces:**
- Consumes: P1 的 `ReportFinding.Origin` / `dedupeKey()` / `OriginPinned` / `OriginSystem`；既有 `buildReportDigest`。
- Produces: `func mergeFindings(pinned, system []ReportFinding) []ReportFinding` —— 人记的排在前面，系统补的里凡是撞了去重键的丢弃；顺序稳定。

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/report_digest_test.go` 追加：

```go
// 人在分析页记过「素材对比 · 时长」，系统就不该在同一份复盘里再补一条同样的。
// 复盘会上同一件事被念两遍，第二遍会被当成另一条独立证据——两条相互印证的错觉，
// 比一条孤证更容易让人下决心。
func TestMergeFindingsDropsSystemDuplicatesOfWhatSomeonePinned(t *testing.T) {
	t.Parallel()

	pinned := []ReportFinding{{
		Kind: SectionAssetPerformance, Text: "15 秒版本点击率更高。",
		Origin: OriginPinned, Dimension: "comparisons", Variable: "duration",
		Judgement: judge(ConfidenceSufficient, "样本充分、区间不重叠。"),
	}}
	system := []ReportFinding{
		{Kind: SectionAssetPerformance, Text: "时长 15s 组的点击率高于其余素材。",
			Origin: OriginSystem, Dimension: "comparisons", Variable: "duration",
			Judgement: judge(ConfidenceSufficient, "")},
		{Kind: SectionAssetPerformance, Text: "开场有人脸的一组转化更好。",
			Origin: OriginSystem, Dimension: "drivers", Variable: "opening_face",
			Judgement: judge(ConfidenceDirectional, "")},
	}

	merged := mergeFindings(pinned, system)
	if len(merged) != 2 {
		t.Fatalf("撞键的系统发现应该被丢掉，剩 2 条，得到 %d 条：%+v", len(merged), merged)
	}
	if merged[0].Origin != OriginPinned {
		t.Error("人记的应该排在前面——复盘先看自己留的，再看系统补的")
	}
	if merged[1].Variable != "opening_face" {
		t.Errorf("没撞键的系统发现应该留下，得到 %q", merged[1].Variable)
	}
}

// 没有维度和变量的发现（口径警告、下一轮建议）去重键是空的，不参与去重。
// 拿空键去重会把它们全折成一条。
func TestMergeFindingsKeepsEveryFreeTextFinding(t *testing.T) {
	t.Parallel()

	system := []ReportFinding{
		{Kind: SectionRecommendation, Text: "下一轮把时长压到 15 秒。", Origin: OriginSystem},
		{Kind: SectionRecommendation, Text: "补一组开场有人脸的素材。", Origin: OriginSystem},
	}
	merged := mergeFindings(nil, system)
	if len(merged) != 2 {
		t.Fatalf("自由文本不该被折叠，期望 2 条，得到 %d 条", len(merged))
	}
}

// 系统内部自己也可能重复（同一个变量在对比和驱动里各出现一次）。
// 这两条不该互相消掉——它们的维度不同，说的确实是两件事。
// 但同维度同变量的系统重复要消掉。
func TestMergeFindingsDedupesWithinTheSystemBatch(t *testing.T) {
	t.Parallel()

	system := []ReportFinding{
		{Text: "甲", Origin: OriginSystem, Dimension: "drivers", Variable: "duration"},
		{Text: "乙", Origin: OriginSystem, Dimension: "drivers", Variable: "duration"},
		{Text: "丙", Origin: OriginSystem, Dimension: "comparisons", Variable: "duration"},
	}
	merged := mergeFindings(nil, system)
	if len(merged) != 2 {
		t.Fatalf("同维度同变量的系统重复要消掉，期望 2 条，得到 %d 条：%+v", len(merged), merged)
	}
	if merged[0].Text != "甲" {
		t.Errorf("重复时保留先出现的那条，得到 %q", merged[0].Text)
	}
}

// 人删掉的那条也占着去重键。删掉不等于「这个维度还空着」——人是看过之后
// 决定不要的，系统再补一条一模一样的回来，等于否决他的决定。
func TestMergeFindingsRespectsWhatSomeoneDeleted(t *testing.T) {
	t.Parallel()

	pinned := []ReportFinding{{
		Text: "15 秒版本点击率更高。", Origin: OriginPinned,
		Dimension: "comparisons", Variable: "duration", Dropped: true,
	}}
	system := []ReportFinding{{
		Text: "时长 15s 组的点击率高于其余素材。", Origin: OriginSystem,
		Dimension: "comparisons", Variable: "duration",
	}}
	merged := mergeFindings(pinned, system)
	if len(merged) != 1 {
		t.Fatalf("被删掉的那条仍然占着去重键，期望 1 条，得到 %d 条：%+v", len(merged), merged)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestMergeFindings' -v
```

Expected: 编译失败，`undefined: mergeFindings`。

- [ ] **Step 3: 实现 mergeFindings**

在 `internal/systems/insights/report_digest.go` 的 `buildReportDigest` 下方加：

```go
// mergeFindings 把人记的和系统补的合成一份。
//
// 人记的全部保留并排在前面——包括他删掉的那些：删掉不等于「这个维度还空着」，
// 人是看过之后决定不要的，系统再补一条一模一样的回来，等于否决他的决定。
//
// 系统补的里，凡是去重键（维度 + 变量）已经出现过的一律丢弃。没有去重键的
// （口径警告、下一轮建议这类自由文本）全部保留——拿空键去重会把它们全折成一条。
func mergeFindings(pinned, system []ReportFinding) []ReportFinding {
	merged := make([]ReportFinding, 0, len(pinned)+len(system))
	seen := make(map[string]struct{}, len(pinned)+len(system))

	for _, finding := range pinned {
		merged = append(merged, finding)
		if key := finding.dedupeKey(); key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, finding := range system {
		key := finding.dedupeKey()
		if key != "" {
			if _, taken := seen[key]; taken {
				continue
			}
			seen[key] = struct{}{}
		}
		merged = append(merged, finding)
	}
	return merged
}
```

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/systems/insights/ -run 'TestMergeFindings' -v
```

Expected: 四个测试全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/systems/insights/report_digest.go internal/systems/insights/report_digest_test.go
git commit -m "feat(insights): 合并复盘发现时人记过的不再补"
```

---

### Task 2: 提交复盘 = 补执行 + 定格系统发现 + 确认

**Files:**
- Create: `internal/systems/insights/review.go`
- Create: `internal/systems/insights/review_test.go`
- Modify: `internal/systems/insights/service.go`（`SubmitReviewRequest`、`Repository` 接口加 `SubmitReport`）
- Modify: `internal/systems/insights/mysql_repository.go`（`SubmitReport`）
- Modify: `internal/systems/insights/httpapi/server.go`

**Interfaces:**
- Consumes: Task 1 的 `mergeFindings`；既有 `buildReportDigest`、`ConfirmReport`、`concludedExperimentsInWindow`、`GetPerformanceAnalysis`。
- Produces:
  - `type SubmitReviewRequest struct{ ExecutionID string; ExpectedVersion int64 }`，`Validate()`
  - `func (Service) SubmitReview(context.Context, contract.ActorContext, contract.ProjectID, string, SubmitReviewRequest) (InsightReport, error)`
  - `Repository` 新增 `SubmitReport(ctx, orgID, projectID, reportID string, expectedVersion int64, executionID string, digest []ReportFinding, actorID string, at time.Time) (InsightReport, error)`
  - HTTP：`POST /api/insights/v1/projects/{project_id}/reports/{report_id}/submit`

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/review_test.go`：

```go
package insights

import (
	"errors"
	"testing"
)

// 系统发现在**提交那一刻**才定格进去，不是草稿一建就补。
//
// 草稿是活的：人一边看分析一边往里记，这中间窗口没变但数据每天在变。
// 一建就补的话，人记第一笔时补进来的那批，到提交的时候数字早就不是那个数了，
// 而报告上不会写它是哪天算的。
func TestSubmitReviewFreezesSystemFindingsAtSubmitTime(t *testing.T) {
	t.Parallel()

	pinned := []ReportFinding{{
		Text: "15 秒版本点击率更高。", Origin: OriginPinned,
		Dimension: "comparisons", Variable: "duration",
		Judgement: judge(ConfidenceSufficient, "样本充分、区间不重叠。"),
	}}
	system := []ReportFinding{
		{Text: "时长 15s 组更高。", Origin: OriginSystem, Dimension: "comparisons", Variable: "duration"},
		{Text: "开场有人脸的一组转化更好。", Origin: OriginSystem, Dimension: "drivers", Variable: "opening_face"},
	}

	frozen := mergeFindings(pinned, system)
	if len(frozen) != 2 {
		t.Fatalf("定格结果应该是 2 条，得到 %d 条", len(frozen))
	}
	if frozen[0].Origin != OriginPinned {
		t.Error("人记的排在前面")
	}
}

func TestSubmitReviewRequestValidation(t *testing.T) {
	t.Parallel()

	valid := SubmitReviewRequest{ExecutionID: "exec_1", ExpectedVersion: 3}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法请求被拒了：%v", err)
	}

	noExecution := SubmitReviewRequest{ExpectedVersion: 3}
	if err := noExecution.Validate(); err == nil {
		// 提交是全流程唯一必须回答「这份复盘算哪次投放」的地方。不回答的话，
		// 这份复盘沉淀出的经验就没有来源执行，下一轮引用它的人无从追溯。
		t.Error("没有投放执行的提交应该被拒")
	}

	noVersion := SubmitReviewRequest{ExecutionID: "exec_1"}
	if err := noVersion.Validate(); err == nil {
		t.Error("没有版本号的提交应该被拒——并发编辑会静默覆盖")
	}
}

// 已提交的复盘不能再提交一次。第二次提交会用今天的数据重新定格一遍系统发现，
// 而引用了第一版的人手上那份就变成假的了。
func TestSubmitReviewRejectsAlreadyConfirmedReports(t *testing.T) {
	t.Parallel()

	report := InsightReport{Status: ReportConfirmed, Version: 1}
	if err := checkSubmittable(report, 1); !errors.Is(err, ErrInvalidState) {
		t.Errorf("已确认的复盘应该拒绝提交，得到 %v", err)
	}
}

func TestSubmitReviewChecksVersion(t *testing.T) {
	t.Parallel()

	report := InsightReport{Status: ReportDraft, Version: 5}
	if err := checkSubmittable(report, 3); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("版本对不上应该报冲突，得到 %v", err)
	}
	if err := checkSubmittable(report, 5); err != nil {
		t.Errorf("版本对得上应该放行，得到 %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestSubmitReview' -v
```

Expected: 编译失败，`undefined: SubmitReviewRequest`、`undefined: checkSubmittable`。

- [ ] **Step 3: 写 review.go**

创建 `internal/systems/insights/review.go`：

```go
package insights

import "strings"

// 提交复盘。
//
// 这一步做三件事，缺一件这份复盘都不完整：
//  1. 补上「这份复盘算哪次投放」——全流程唯一必须回答它的地方；
//  2. 把系统发现定格进去，和人一路记的那几笔合并去重；
//  3. 状态改成已确认，从此不可改。
//
// 三件事必须在一个事务里。分开做的话，中间断电会留下一份「已确认但没有系统发现」
// 的报告，而它看起来和正常的一模一样——没人会怀疑那份复盘漏了东西。

// SubmitReviewRequest 是提交复盘的入参。
type SubmitReviewRequest struct {
	// ExecutionID 是这份复盘算哪次投放。草稿是人在分析页记第一笔时自动建的，
	// 那时候还没到这个问题；提交的时候必须回答。
	ExecutionID string `json:"execution_id"`
	// ExpectedVersion 防并发覆盖：两个人同时开着这份草稿，后提交的那个
	// 会把先提交的删改抹掉，而两边都不会看到任何提示。
	ExpectedVersion int64 `json:"expected_version"`
}

func (r SubmitReviewRequest) Validate() error {
	if strings.TrimSpace(r.ExecutionID) == "" {
		return ErrInvalidRequest
	}
	if r.ExpectedVersion <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

// checkSubmittable 单独拆出来，是为了让「什么样的复盘能提交」这条规则
// 能被直接测到，不用先造一个仓储。
func checkSubmittable(report InsightReport, expectedVersion int64) error {
	if report.Status != ReportDraft {
		return ErrInvalidState
	}
	if report.Version != expectedVersion {
		return ErrVersionConflict
	}
	return nil
}
```

- [ ] **Step 4: 加服务方法**

`internal/systems/insights/service.go`，在 `ConfirmReport` 下方加：

```go
// SubmitReview 提交复盘：补执行、定格系统发现、确认，一个事务。
func (s Service) SubmitReview(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, reportID string, request SubmitReviewRequest) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeConfirm); err != nil {
		return InsightReport{}, err
	}
	if err := request.Validate(); err != nil {
		return InsightReport{}, err
	}
	report, err := s.Repository.GetReport(ctx, actor.OrganizationID, projectID, reportID)
	if err != nil {
		return InsightReport{}, err
	}
	if err := checkSubmittable(report, request.ExpectedVersion); err != nil {
		return InsightReport{}, err
	}

	window, ok := reportMetricWindow(report)
	if !ok {
		// 窗口解析不出来就没法算系统发现。宁可只带人记的那几笔提交，
		// 也不能拿一个猜出来的窗口去算——算出来的数字看起来一样可信。
		return s.Repository.SubmitReport(ctx, actor.OrganizationID, projectID, reportID,
			request.ExpectedVersion, request.ExecutionID, report.Digest, actor.Principal.ID, s.now())
	}

	analysis, err := s.GetPerformanceAnalysis(ctx, actor, projectID, window)
	if err != nil {
		return InsightReport{}, err
	}
	experiments, err := s.concludedExperimentsInWindow(ctx, actor, projectID, window)
	if err != nil {
		return InsightReport{}, err
	}
	experiences, err := s.ListExperiences(ctx, actor, projectID,
		ExperienceFilter{Status: ExperienceConfirmed, Limit: 20})
	if err != nil {
		return InsightReport{}, err
	}

	// 人记的那几笔在前，系统补的去重后跟在后面。
	pinned := make([]ReportFinding, 0, len(report.Digest))
	for _, finding := range report.Digest {
		if finding.Origin == OriginPinned {
			pinned = append(pinned, finding)
		}
	}
	digest := mergeFindings(pinned, buildReportDigest(analysis, experiments, experiences))

	return s.Repository.SubmitReport(ctx, actor.OrganizationID, projectID, reportID,
		request.ExpectedVersion, request.ExecutionID, digest, actor.Principal.ID, s.now())
}

// reportMetricWindow 把报告定格的日期串解析回窗口。
func reportMetricWindow(report InsightReport) (MetricWindow, bool) {
	start, end := reportWindow(report)
	if start == nil || end == nil {
		return MetricWindow{}, false
	}
	return MetricWindow{Start: *start, End: *end}, true
}
```

> `ListExperiences` 与 `ExperienceFilter` 的真实签名以 `service.go` 里已有的调用为准（`grep -n "ExperienceFilter{" internal/systems/insights/`），不要按上面硬写。`buildReportDigest` 的三个入参顺序同理，先看一眼 `CreateReport` 里怎么调的。

- [ ] **Step 5: 加仓储方法**

`Repository` 接口在 `ConfirmReport` 那一行下面加：

```go
	// SubmitReport 在一个事务里补执行 ID、写入定格后的 digest、置为已确认。
	// 拆成三次调用会留下「已确认但没有系统发现」的报告，而它看起来和正常的一模一样。
	SubmitReport(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID,
		reportID string, expectedVersion int64, executionID string, digest []ReportFinding,
		actorID string, at time.Time) (InsightReport, error)
```

`mysql_repository.go` 实现（照 `ConfirmReport` 的事务写法，先读一遍它再写）：

```go
func (r MySQLRepository) SubmitReport(ctx context.Context, organizationID contract.OrganizationID,
	projectID contract.ProjectID, reportID string, expectedVersion int64, executionID string,
	digest []ReportFinding, actorID string, at time.Time) (InsightReport, error) {
	encoded, err := json.Marshal(digest)
	if err != nil {
		return InsightReport{}, err
	}
	result, err := r.DB.ExecContext(ctx, `
		UPDATE insight_reports
		SET execution_id = ?, digest = ?, status = ?, confirmed_by = ?, confirmed_at = ?,
		    updated_at = ?, version = version + 1
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ? AND status = ?`,
		executionID, encoded, string(ReportConfirmed), actorID, at, at,
		string(organizationID), string(projectID), reportID, expectedVersion, string(ReportDraft))
	if err != nil {
		// 唯一键是 (organization_id, project_id, execution_id, window_start, window_end)。
		// 补执行 ID 的这一下有可能撞上同执行同窗口的另一份复盘——多半是同一轮
		// 被两个人各记了一份。这时候必须给出能看懂的错，不能扔一个重复键出去。
		if isDuplicateKey(err) {
			return InsightReport{}, ErrConflict
		}
		return InsightReport{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return InsightReport{}, err
	}
	if affected == 0 {
		return InsightReport{}, ErrVersionConflict
	}
	return r.GetReport(ctx, organizationID, projectID, reportID)
}
```

> `isDuplicateKey` 若仓里没有，在 `mysql_repository.go` 底部加一个（`grep -n "1062\|ErrDuplicate\|isDuplicate" internal/ -r` 先找现成的）。`ErrConflict` 若不存在就用 `ErrInvalidState`，并在 handler 里映射成 409。`confirmed_by` / `confirmed_at` 的真实列名以 `ConfirmReport` 里的 UPDATE 为准。

内存/伪造仓储补同名方法。

- [ ] **Step 6: 挂 HTTP 路由**

`httpapi/server.go`：`Application` 接口加

```go
	SubmitReview(context.Context, contract.ActorContext, contract.ProjectID, string, insights.SubmitReviewRequest) (insights.InsightReport, error)
```

`New()` 里加

```go
	server.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/reports/{report_id}/submit", server.submitReview)
```

handler 照 `confirmReport`（`grep -n "func (s \*Server) confirmReport" internal/systems/insights/httpapi/server.go`）改写：取 `report_id` 路径参数、解 body、调 `s.app.SubmitReview`、`writeJSON` 200。

- [ ] **Step 7: 跑测试并提交**

```bash
go build ./... && go test ./internal/systems/insights/...
```

```bash
git add internal/systems/insights/ 
git commit -m "feat(insights): 提交复盘时定格系统发现并补上投放执行"
```

---

### Task 3: 没人碰过的空草稿不出现，也不永远堆着

**Files:**
- Modify: `internal/systems/insights/service.go`（`ListReports` 过滤）
- Create: `internal/systems/insights/mysql_repository.go` 的 `PurgeEmptyDrafts`
- Modify: `internal/systems/insights/review_test.go`（追加）
- Modify: 定时任务注册处（见 Step 4）

**Interfaces:**
- Consumes: `InsightReport.Digest`、`ReportDraft`。
- Produces:
  - `func hasContent(report InsightReport) bool` —— digest 里有任何一条就算被碰过
  - `Repository` 新增 `PurgeEmptyDrafts(ctx, before time.Time) (int64, error)`
  - `func (Service) PurgeEmptyDrafts(ctx context.Context) (int64, error)`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/review_test.go` 追加：

```go
// 空草稿不该出现在复盘列表里。
//
// 草稿是「记一笔」自动建的（P1），人从来不主动建复盘。所以一份没有任何发现的草稿，
// 意味着人只是打开看了看——把它列出来，复盘列表很快就会被一堆空壳填满，
// 而真正有内容的那几份混在里面找不着。
func TestEmptyDraftsAreHiddenFromTheList(t *testing.T) {
	t.Parallel()

	empty := InsightReport{Status: ReportDraft, Digest: []ReportFinding{}}
	if hasContent(empty) {
		t.Error("没有任何发现的草稿应该算没被碰过")
	}

	touched := InsightReport{Status: ReportDraft, Digest: []ReportFinding{{Text: "记了一笔"}}}
	if !hasContent(touched) {
		t.Error("有发现的草稿应该显示")
	}

	// 人把唯一那条删了，草稿仍然算被碰过：他做过一个明确的决定，
	// 这份草稿代表「这一轮我看过，什么都不值得留」。清掉它等于抹掉那个决定。
	emptied := InsightReport{Status: ReportDraft, Digest: []ReportFinding{{Text: "记了一笔", Dropped: true}}}
	if !hasContent(emptied) {
		t.Error("删空了的草稿仍然算被碰过")
	}

	// 已确认的复盘不管有没有发现都要显示——它是这一轮的正式记录。
	confirmed := InsightReport{Status: ReportConfirmed, Digest: []ReportFinding{}}
	if !hasContent(confirmed) {
		t.Error("已确认的复盘永远显示")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestEmptyDrafts' -v
```

Expected: `undefined: hasContent`。

- [ ] **Step 3: 实现过滤**

`internal/systems/insights/review.go` 追加：

```go
// hasContent 判断这份复盘要不要出现在列表里。
//
// 空草稿是「记一笔」自动建了但人什么都没记的残留。它和「人记了又全删了」不一样：
// 后者是一个明确的决定——这一轮我看过，什么都不值得留——清掉它等于抹掉那个决定。
// 所以判据是 digest 长度，不是「有几条没被删」。
func hasContent(report InsightReport) bool {
	if report.Status != ReportDraft {
		return true
	}
	return len(report.Digest) > 0
}
```

`service.go` 的 `ListReports` 在返回前过滤：

```go
	values, err := s.Repository.ListReports(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	visible := make([]InsightReport, 0, len(values))
	for _, value := range values {
		if hasContent(value) {
			visible = append(visible, value)
		}
	}
	return visible, nil
```

- [ ] **Step 4: 清理任务**

`Repository` 接口加：

```go
	// PurgeEmptyDrafts 删掉 before 之前建的、一条发现都没有的草稿。
	// 它们是「记一笔」建了但人什么都没记的残留，不删会一直占着
	// (项目 + 窗口) 的唯一键。
	PurgeEmptyDrafts(ctx context.Context, before time.Time) (int64, error)
```

`mysql_repository.go`：

```go
func (r MySQLRepository) PurgeEmptyDrafts(ctx context.Context, before time.Time) (int64, error) {
	// JSON_LENGTH(digest) = 0 才删。只看 created_at 不看内容会删掉真的复盘草稿。
	result, err := r.DB.ExecContext(ctx, `
		DELETE FROM insight_reports
		WHERE status = ? AND created_at < ? AND JSON_LENGTH(digest) = 0`,
		string(ReportDraft), before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
```

`service.go`：

```go
// emptyDraftRetention 是空草稿保留多久。30 天：一个投放周期通常一个月内结束，
// 超过一个月还没记过任何东西的草稿，人已经不会回来了。
const emptyDraftRetention = 30 * 24 * time.Hour

// PurgeEmptyDrafts 由定时任务调用，不走 actor 鉴权——它删的是没有内容的残留，
// 不属于任何人的数据。
func (s Service) PurgeEmptyDrafts(ctx context.Context) (int64, error) {
	return s.Repository.PurgeEmptyDrafts(ctx, s.now().Add(-emptyDraftRetention))
}
```

找到现有的定时任务注册处并挂上（若仓里没有定时任务框架，就先只留服务方法，在 `cmd/` 下找一个已有的维护命令挂进去）：

```bash
grep -rn "cron\|ticker\|time.NewTicker\|Scheduler" cmd/ internal/platform/ --include=*.go | head -20
```

若确实没有任何定时机制，**不要为此引入调度库**（引入新工具链需要先经使用者同意）。改为在 `cmd/cookies-maintain` 之类已有的一次性命令里加一个子命令，并在本任务的提交信息里注明「清理暂由手工命令触发，自动化待定」。

- [ ] **Step 5: 跑测试并提交**

```bash
go build ./... && go test ./internal/systems/insights/...
```

```bash
git add internal/systems/insights/ cmd/
git commit -m "feat(insights): 空复盘草稿不列出并可清理"
```

---

### Task 4: 复盘页：我记的 / 系统补的 / 提交

**Files:**
- Create: `src/components/insight/review/ReviewPage.tsx`
- Create: `src/components/insight/review/DraftPanel.tsx`
- Create: `src/components/insight/review/FindingRow.tsx`
- Create: `src/components/insight/review/SubmitReviewAction.tsx`
- Create: `src/components/insight/review/HarvestPanel.tsx`
- Create: `src/components/insight/review/index.ts`
- Modify: `src/data/api.ts`
- Reference: `src/components/ReportCenterPage.tsx`（搬运来源，本任务不删）
- Reference: `src/components/FreezeReportAction.tsx`（`SubmitReviewAction` 的改写来源）

**Interfaces:**
- Consumes: P0 的 `VerdictBadge`；Task 2 的 `POST .../submit`。
- Produces:
  - `export function ReviewPage({ view }: { view: ReviewView })`
  - `export type ReviewView = 'current' | 'all' | 'harvest'`
  - `export function FindingRow({ finding, index, editable, onDrop })`

- [ ] **Step 1: 加 api 方法**

`src/data/api.ts`，在 `confirmInsightReport` 旁边加：

```ts
  submitReview: (projectId: string, reportId: string, body: {
    execution_id: string
    expected_version: number
  }) => request<ApiInsightReport>(
    `${insightProjectPath(projectId)}/reports/${reportId}/submit`, 'POST', body),
```

- [ ] **Step 2: 写 FindingRow**

创建 `src/components/insight/review/FindingRow.tsx`：

```tsx
import { RotateCcw, Trash2 } from 'lucide-react'
import type { ApiReportFinding } from '../../../data/api'
import { VerdictBadge } from '../shared'

/**
 * 复盘里的一条发现。
 *
 * 左边那个点区分来源：● 是人在分析页记的，○ 是系统提交时补的。这个区分不是装饰
 * ——复盘会上「我当时特意留了这条」和「系统扫出来的」份量完全不同，混在一起显示，
 * 人自己也想不起来哪条是哪条。
 */
export function FindingRow({ finding, index, editable, onDrop }: {
  finding: ApiReportFinding
  index: number
  editable: boolean
  onDrop: (index: number, dropped: boolean) => void
}) {
  const pinned = finding.origin === 'pinned'
  return <li className={finding.dropped ? 'finding-row dropped' : 'finding-row'}>
    <span className={pinned ? 'finding-origin pinned' : 'finding-origin'}
      title={pinned ? '我在分析页记的' : '提交时系统补的'}>
      {pinned ? '●' : '○'}
    </span>
    <div className="finding-body">
      <p>{finding.text}</p>
      <VerdictBadge judgement={finding}/>
      {finding.note ? <small className="finding-note">{finding.note}</small> : null}
    </div>
    {editable ? <button type="button" className="text-button"
      onClick={() => onDrop(index, !finding.dropped)}
      title={finding.dropped ? '恢复这一条' : '这一条不要'}>
      {finding.dropped ? <RotateCcw size={14}/> : <Trash2 size={14}/>}
    </button> : null}
  </li>
}
```

- [ ] **Step 3: 写 SubmitReviewAction**

创建 `src/components/insight/review/SubmitReviewAction.tsx`——从 `src/components/FreezeReportAction.tsx` 整份复制过来，改四处：

1. 组件名 `FreezeReportAction` → `SubmitReviewAction`；`onFreeze` → `onSubmit`。
2. 按钮文案「定格这一屏为复盘报告」→「提交这一轮复盘」；「定格并去报告中心」→「提交」。
3. 说明文案整段换成：

```tsx
    <p className="prelaunch-disclosure">
      提交会把这一轮的复盘冻起来：你记的那几笔，加上系统这一刻补齐的素材表现、
      实验结论、相关经验和下一轮建议。系统补的只算这一次，之后再换窗口不影响它。
      提交之后这份复盘不能再改——它会被下一轮引用，改一条等于让引用它的人手上那份变成假的。
    </p>
```

4. 删掉导出的 `isoDay`（P1 的 `AnalysisPage` 里已有一份同名函数；两处各留一份比互相 import 干净，但不要重复导出同名符号造成歧义）。

原文件里「投放执行只能从清单里挑，不给手打的口子」那段注释和 `<select>` 逻辑**原样保留**——理由没变，而且现在它是全流程唯一回答「这份复盘算哪次投放」的地方，比原来更重要。

- [ ] **Step 4: 写 DraftPanel**

创建 `src/components/insight/review/DraftPanel.tsx`：

```tsx
import { useState } from 'react'
import { CircleCheck } from 'lucide-react'
import { api, type ApiInsightReport } from '../../../data/api'
import { FindingRow } from './FindingRow'
import { HarvestPanel } from './HarvestPanel'
import { SubmitReviewAction } from './SubmitReviewAction'

/**
 * 一份复盘的正文。
 *
 * 草稿态和已提交态是同一套布局，差别只在能不能改——两套布局会让人在提交前后
 * 对不上号，以为自己看的是另一份东西。
 */
export function DraftPanel({ report, projectId, onChanged }: {
  report: ApiInsightReport
  projectId: string
  onChanged: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const draft = report.status === 'draft'

  const pinned = report.digest.filter(item => item.origin === 'pinned')
  const system = report.digest.filter(item => item.origin !== 'pinned')

  const drop = (index: number, dropped: boolean) => {
    setBusy(true); setError('')
    api.dropInsightReportFinding(projectId, report.id, {
      index, dropped, expected_version: report.version,
    }).then(onChanged)
      .catch(() => setError('这一条没改成，页面可能不是最新的，刷新后再试。'))
      .finally(() => setBusy(false))
  }

  const submit = async (executionId: string) => {
    setBusy(true); setError('')
    try {
      await api.submitReview(projectId, report.id, {
        execution_id: executionId, expected_version: report.version,
      })
      onChanged()
    } catch {
      setError('提交没成。可能是这一轮已经有人提交过同一次投放的复盘了，刷新看看。')
    } finally {
      setBusy(false)
    }
  }

  return <div className="review-body">
    <section>
      <span className="section-label">这一轮我记的（{pinned.length}）</span>
      {pinned.length ? <ul className="finding-list">
        {pinned.map((finding, index) => <FindingRow key={index} finding={finding}
          index={report.digest.indexOf(finding)} editable={draft} onDrop={drop}/>)}
      </ul> : <p className="panel-empty">
        这一轮还没记过。去「分析」里看，看到值得留的按「记一笔」。
      </p>}
    </section>

    <section>
      <span className="section-label">系统补的（{system.length}）</span>
      {draft ? <p className="prelaunch-disclosure">
        系统发现在提交那一刻才算——草稿还在改，现在补进来的数字到提交时就不是这个数了。
      </p> : system.length ? <ul className="finding-list">
        {system.map((finding, index) => <FindingRow key={index} finding={finding}
          index={report.digest.indexOf(finding)} editable={false} onDrop={drop}/>)}
      </ul> : <p className="panel-empty">这一轮系统没有补出新的发现。</p>}
    </section>

    {error ? <p className="form-error">{error}</p> : null}

    {draft
      ? <SubmitReviewAction window={{ start: report.window_start, end: report.window_end }}
          busy={busy} onSubmit={submit}/>
      : <>
          <p className="review-sealed"><CircleCheck size={15}/>这份复盘已提交，不能再改。</p>
          <HarvestPanel report={report} projectId={projectId} onChanged={onChanged}/>
        </>}
  </div>
}
```

> `api.dropInsightReportFinding` 的真实方法名与入参以 `src/data/api.ts` 里已有的为准（`grep -n "dropInsight\|drop.*Finding" src/data/api.ts`）。

- [ ] **Step 5: 写 HarvestPanel 与 ReviewPage**

`HarvestPanel.tsx` 从 `ReportCenterPage.tsx` 里「沉淀成经验」那一段搬过来，只改标题措辞（「沉淀成经验」保留原词），逻辑不动——它调 `api.createExperience(projectId, reportId, ...)`，后端要求报告已确认，这条约束不变。

`ReviewPage.tsx` 是壳：左边草稿列表（`api.listInsightReports`），右边 `DraftPanel`。三个视图：

```ts
export type ReviewView = 'current' | 'all' | 'harvest'
```

- `current`：只列 draft，默认选中最近那份。这是日常最常进的一屏——「我这一轮记了什么，该收尾了」。
- `all`：全部，含已提交。
- `harvest`：按报告看它沉淀出的经验，逻辑从 `ReportCenterPage` 的 `harvest` 视图整段搬过来（含那段「一份已确认却一条经验都没沉淀的报告，说明这次复盘的结论没人愿意为它背书」的说明文案——它是这个视图存在的理由）。

`index.ts`：

```ts
export { ReviewPage, type ReviewView } from './ReviewPage'
```

- [ ] **Step 6: 构建并在浏览器里走一遍**

```bash
npm run build
```

用 `preview_start` 起服务，走这一串：

1. 去「分析 · 驱动因素」记两笔。
2. 切到「复盘 · 本轮」，确认草稿在列表里，右边「这一轮我记的」有 2 条、都是 ● 、都带档位。
3. 确认「系统补的」这一段显示的是那句「提交那一刻才算」的说明，不是空列表。
4. 删掉其中一条，确认它变灰但没消失，且能恢复。
5. 点「提交这一轮复盘」，选一个投放执行，提交。
6. 确认提交后「系统补的」出现了若干 ○ 条目，且**没有**一条和你记的那两笔是同维度同变量的。
7. 确认页面显示「已提交，不能再改」，删除按钮消失，下面出现「沉淀成经验」。
8. `preview_network` 核对 `POST .../submit` 返回 200 且响应里 `status: "confirmed"`、`execution_id` 非空。
9. `preview_console_logs` 确认无报错。

- [ ] **Step 7: 提交**

```bash
git add src/components/insight/review/ src/data/api.ts src/styles.css
git commit -m "feat(insights-web): 复盘页分我记的与系统补的两段"
```

---

### Task 5: 导航从「报告中心」收敛成「复盘」

**Files:**
- Modify: `src/data/navigation.ts`（`reports` 那一条）
- Modify: 渲染分发处

> **这一步修改现有导航结构。开始前必须向使用者确认。**

- [ ] **Step 1: 改导航条目**

把 `reports` 那一条替换为：

```ts
      // 「复盘」= 原报告中心。改名不是换皮：报告中心听起来是个存档的地方，
      // 而这里是一轮投放收尾时人真正要做事的地方——决定这一轮留下什么。
      {
        id: 'review', label: '复盘', icon: BookOpenCheck, group: '工作', layout: 'review',
        description: '一轮投放收尾。看你一路记的那几笔，系统把漏的补齐，逐条决定留不留，提交，把值得复用的沉淀成经验。',
        views: ['本轮', '全部复盘', '已沉淀经验'],
      },
```

- [ ] **Step 2: 改渲染分发**

```bash
grep -rn "'reports'\|\"reports\"" src/components/ src/App.tsx | grep -v api.ts
```

把那一处指向 `<ReviewPage view={...}/>`：

```ts
const reviewViews: Record<string, ReviewView> = {
  本轮: 'current',
  全部复盘: 'all',
  已沉淀经验: 'harvest',
}
```

- [ ] **Step 3: 跑全量**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

用 `preview_snapshot` 核对侧栏：洞察下面是「分析」「复盘」，原「报告中心」不在了。

- [ ] **Step 4: 提交**

```bash
git add src/data/navigation.ts src/components/
git commit -m "feat(insights-web): 报告中心收敛成「复盘」入口"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档「模块二 · 复盘」与第 2b 期：

| 规格要求 | 落在 |
|---|---|
| 打开草稿看见自己记的几笔 | Task 4 Step 4（「这一轮我记的」段） |
| 系统把漏掉的补齐 | Task 2（提交时定格）+ Task 1（合并去重） |
| 人记过的系统不再补 | Task 1 的 `mergeFindings` + 四个测试 |
| 逐条决定留不留 | Task 4 的 `FindingRow`，沿用既有 `DropReportFinding`（不物理删） |
| 提交后不可改 | Task 2 的 `checkSubmittable` + Task 4 的 `editable={false}` |
| 沉淀成经验 | Task 4 Step 5 的 `HarvestPanel`，后端 `CreateExperience` 不改 |
| 草稿在被碰之前不出现 | Task 3 的 `hasContent` |
| 空草稿 30 天清理 | Task 3 的 `PurgeEmptyDrafts` |
| ● 我记的 / ○ 系统补的 | Task 4 Step 2 的 `FindingRow` |
| 报告中心 → 复盘 | Task 5 |

**未覆盖且是有意的：**

- **定时调度**：仓里若没有现成的定时机制，Task 3 只挂手工命令，不引入调度库——引入新工具链要先经使用者同意。这一点在 Task 3 Step 4 里写死了。
- **`CreateReport` 老路径**：从投放执行直接建报告的旧入口保留不动。两种草稿来源并存是过渡期的现实（一种从执行来、一种从记一笔来），强行合并会让还在用旧路径的地方一起坏掉。等「记一笔」用起来之后再单独一期收掉。
- **经验的四态流转**（待确认 / 已确认 / 待复审 / 已退役）属于 P4 经验入口，本期不动。

**2. 占位扫描** —— 无 TBD / TODO / 「同 Task N」。Task 4 Step 3 明确列出从 `FreezeReportAction` 改写的四处，并给出替换后的完整文案；Task 4 Step 5 对搬运段落说明了「哪句文案不能丢以及为什么」。

**3. 类型一致性** —— `SubmitReviewRequest` 的 `execution_id` / `expected_version` 在 Go、`api.ts`、OpenAPI 三处一致。`ReviewView` 的三个取值与 `reviewViews` 映射表的三个键一致。`FindingRow` 的 `index` 传的是**在完整 digest 里的下标**而不是分段后的下标——`DropReportFinding` 按 digest 下标定位，传分段下标会删错条目；Task 4 Step 4 里用 `report.digest.indexOf(finding)` 显式还原，这一点容易写错，已在两处 `FindingRow` 调用中统一。

---

## 依赖关系

前置：P0 全部、P1 全部。
后继：P4 经验依赖本期提交后的复盘作为经验的来源；P4 会把「已沉淀经验」这个视图从复盘挪进经验入口。
