# 素材洞察 · 模块三「经验」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「经验库」和「投前洞察」合成一个叫「经验」的入口，分「查」「管」两个模式；同时把四态收成三态加一个复审标记，并给下游引用加上「判定必须是 ✅ 能归因」这道闸。

**Architecture:** 这两页本来就是同一批数据的两种读法——投前洞察的五个视图（策略证据、创意建议、历史模式、风险与反例、引用记录）里，前四个只是同一批经验按不同条件筛，第五个是每条经验自己的引用历史。所以不新建数据，只重排读法：卡面三行（结论 / 适用条件 / 凭什么），九个字段全收进展开层。状态那边，「待复审」本质是「还在用但该看一眼了」而不是一个独立状态，拆成状态加标记后行为不变、能力不减，但状态从四个变成三个——三个人记得住。

**Tech Stack:** Go 1.22+、MySQL 8、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`。

## Global Constraints

- 前置依赖：**P0 地基**（三档判定与共享组件）、**P2 复盘**（`SubmitReview` 是经验的上游）。
- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- 不引入任何新依赖。
- **状态改名对照表**（P0 名词表里已定，此处逐字照抄）：`pending` = **待定**、`confirmed` = **在用**、`retired` = **停用**；「待复审」不再是状态，是「在用」上的一个标记，文案固定为**「该看一眼了」**。
- **下游默认引用的两道闸：状态「在用」且判定「✅ 能归因」。** 👁 只是观察的经验可以存、可以查、可以人工点名引用，但不进默认引用集。
- 修订永远新增版本，不覆盖；任何状态变化都进 `insight_experience_audits`，一条都不能少记。
- 迁移文件放 `migrations/insights/`，命名 `YYYYMMDDHHMMSS_<描述>.up.sql`，文件头必须有中文注释说明为什么要改。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。

> **需要确认的破坏性动作：** Task 1（改经验状态机与数据迁移）、Task 6（删导航条目）。这两处修改的是现有业务逻辑与数据库，**开始前必须向使用者确认**。其余任务是新增，可直接做。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `migrations/insights/20260811120000_insight_experience_review_flag.up.sql` | 四态 → 三态 + 标记 | 新建 |
| `internal/systems/insights/service.go:39-56,203-241,656-700` | 状态常量、`Experience.NeedsReview`、`Reusable()`、三个流转方法 | 改 |
| `internal/systems/insights/mysql_repository.go:255-300` | 流转的 from 列表、读写新列 | 改 |
| `internal/systems/insights/experience_query.go` | 「查」模式的适用性筛选 | 新建 |
| `internal/systems/insights/experience_query_test.go` | 筛选与引用闸的全部约束 | 新建 |
| `internal/systems/insights/httpapi/server.go` | 一条新路由、一处 handler 改名 | 改 |
| `api/openapi/insights-v1.yaml` | 新接口与字段 | 改 |
| `src/components/insight/experience/ExperiencePage.tsx` | 入口的壳，查 / 管 切换 | 新建 |
| `src/components/insight/experience/LookupView.tsx` | 「查」：筛选器 + 卡片列表 | 新建 |
| `src/components/insight/experience/ManageView.tsx` | 「管」：待定队列 + 确认 / 驳回 / 修订 | 新建 |
| `src/components/insight/experience/ExperienceCard.tsx` | 三行卡面 + 「凭什么」展开层 | 新建 |
| `src/components/insight/experience/EvidenceTrail.tsx` | 展开层：证据链 + 引用记录 | 新建 |
| `src/components/insight/experience/index.ts` | 出口 | 新建 |
| `src/components/ExperienceReviseForm.tsx` | 复用，只改引入路径 | 改 |
| `src/data/api.ts` | `lookupExperiences`、状态类型收敛 | 改 |
| `src/data/navigation.ts:43,57` | 「投前洞察」+「经验库」→「经验」 | 改 |

---

### Task 1: 四态收成三态加一个复审标记

> **修改现有业务逻辑与数据库。开始前必须向使用者确认。**

**Files:**
- Create: `migrations/insights/20260811120000_insight_experience_review_flag.up.sql`
- Modify: `internal/systems/insights/service.go:39-56`（状态常量）、`:203-241`（`Experience`、`Reusable`）、`:656-700`（三个流转方法）
- Modify: `internal/systems/insights/mysql_repository.go:255-300`
- Modify: `internal/systems/insights/service_test.go`

**Interfaces:**
- Consumes: 既有 `ExperienceStatus` / `Experience` / `TransitionExperienceInput` / `ConfirmExperienceInput`；P0 的 `Judgement` / `VerdictExplained`。
- Produces:
  - `Experience.NeedsReview bool`（JSON `needs_review`）
  - `func (Experience) Reusable() bool` —— 语义改为「在用 **且** 判定为能归因」
  - `func (Experience) StatusLabel() string` —— 待定 / 在用 / 停用
  - `func (Experience) ReviewHint() string` —— 有标记时返回「该看一眼了」，否则空串
  - `Repository.FlagExperienceForReview(context.Context, FlagExperienceReviewInput) (Experience, error)`
  - `ExperienceNeedsReview` 常量**删除**

- [ ] **Step 1: 写迁移**

创建 `migrations/insights/20260811120000_insight_experience_review_flag.up.sql`：

```sql
-- 经验四态收成三态，「待复审」变成「在用」上的一个标记。
--
-- 「待复审」从来就不是一个独立状态：一条被标了待复审的经验，仍然在用、仍然
-- 能被引用，只是有人觉得该重新看一眼。把它做成状态，代价是每个读经验的地方
-- 都要判断「confirmed 或者 needs_review」——漏一处，那条经验就在某个页面上
-- 凭空消失了。做成标记之后，读的地方只认 confirmed，标记只影响怎么显示。
--
-- 数据迁移的方向是「进而不是退」：原来 needs_review 的行变成 confirmed 加标记，
-- 它们仍然在用。反过来把它们降级成 pending 会让一批已经在被引用的经验突然
-- 失去引用资格，而没有人做过这个决定。
ALTER TABLE insight_experiences
  ADD COLUMN needs_review TINYINT(1) NOT NULL DEFAULT 0 AFTER status;

UPDATE insight_experiences SET needs_review = 1, status = 'confirmed'
  WHERE status = 'needs_review';

ALTER TABLE insight_experiences
  DROP CONSTRAINT chk_insight_experiences_status;

ALTER TABLE insight_experiences
  ADD CONSTRAINT chk_insight_experiences_status
  CHECK (status IN ('pending', 'confirmed', 'retired'));

-- 审计表里的历史记录**原样保留**，不改写。
-- 那些 to_status = 'needs_review' 的行是真实发生过的事，改掉它们等于伪造历史；
-- 读审计的地方按「历史状态可能有已经不存在的取值」来渲染即可。
```

> 审计表 `insight_experience_audits` 若也有 status 的 CHECK 约束，**保留 `needs_review`**（`grep -n "chk_insight_experience_audits" migrations/insights/*.sql`）。历史行必须还能读出来。

- [ ] **Step 2: 跑迁移**

```bash
go run ./cmd/cookies-migrate
```

Expected: 无错误。用 `SHOW CREATE TABLE insight_experiences` 确认 CHECK 里只剩三个取值。

- [ ] **Step 3: 写失败的测试**

在 `internal/systems/insights/service_test.go` 追加：

```go
// 下游默认引用有两道闸：状态在用，判定能归因。
//
// 只看状态是不够的。一条「👁 只是观察」的经验也可以被人确认——确认的是
// 「这个观察值得记下来」，不是「这个因果成立」。让它进默认引用集，下一轮就会
// 有人照着一个没排除混杂的观察去做素材，而他不会知道自己在赌。
func TestOnlyExplainedExperiencesAreReusableByDefault(t *testing.T) {
	t.Parallel()

	explained := Experience{Status: ExperienceConfirmed}
	explained.Verdict = VerdictExplained
	if !explained.Reusable() {
		t.Error("在用且能归因的经验应该可默认引用")
	}

	observed := Experience{Status: ExperienceConfirmed}
	observed.Verdict = VerdictObserved
	if observed.Reusable() {
		t.Error("只是观察的经验不该进默认引用集")
	}

	pending := Experience{Status: ExperiencePending}
	pending.Verdict = VerdictExplained
	if pending.Reusable() {
		t.Error("还没人确认的经验不该被引用")
	}
}

// 标了「该看一眼了」的经验仍然在用。这正是把它从状态改成标记的理由：
// 读的地方只认 confirmed，不会因为漏判一个状态就让它凭空消失。
func TestFlaggedExperienceStaysUsable(t *testing.T) {
	t.Parallel()

	value := Experience{Status: ExperienceConfirmed, NeedsReview: true}
	value.Verdict = VerdictExplained
	if !value.Reusable() {
		t.Error("标了复审的经验仍然在用，仍然可引用")
	}
	if value.ReviewHint() == "" {
		t.Error("标了复审就要在界面上说出来，否则这个标记等于没有")
	}
	if value.StatusLabel() != "在用" {
		t.Errorf("状态标签应该是「在用」，得到 %q", value.StatusLabel())
	}
}

func TestStatusLabelsAreTheThreeAgreedWords(t *testing.T) {
	t.Parallel()

	cases := map[ExperienceStatus]string{
		ExperiencePending:   "待定",
		ExperienceConfirmed: "在用",
		ExperienceRetired:   "停用",
	}
	for status, want := range cases {
		if got := (Experience{Status: status}).StatusLabel(); got != want {
			t.Errorf("%s 的标签应该是 %q，得到 %q", status, want, got)
		}
	}
}
```

- [ ] **Step 4: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestOnlyExplained|TestFlagged|TestStatusLabels' -v
```

Expected: 编译失败，`Experience` 没有 `NeedsReview` / `ReviewHint` / `StatusLabel`。

- [ ] **Step 5: 改 Go 侧**

`service.go:39-56` 删掉 `ExperienceNeedsReview` 常量，`valid()` 只留三个：

```go
type ExperienceStatus string

// 三态。「待复审」不在这里——它是「在用」上的一个标记（NeedsReview），
// 不是一个独立状态：被标记的经验仍然在用、仍然能被引用，只是该重新看一眼。
// 做成状态的话，每个读经验的地方都得判断「confirmed 或者 needs_review」，
// 漏一处那条经验就在某个页面上凭空消失了。
const (
	ExperiencePending   ExperienceStatus = "pending"
	ExperienceConfirmed ExperienceStatus = "confirmed"
	ExperienceRetired   ExperienceStatus = "retired"
)

func (s ExperienceStatus) valid() bool {
	switch s {
	case ExperiencePending, ExperienceConfirmed, ExperienceRetired:
		return true
	}
	return false
}
```

`Experience` 结构体：在 `Status` 后面加

```go
	Status       ExperienceStatus `json:"status"`
	// NeedsReview 是「该看一眼了」。它不影响这条经验能不能用，只影响界面怎么显示。
	NeedsReview  bool             `json:"needs_review"`
```

`Reusable()` 重写：

```go
// Reusable 决定下游能不能默认引用这条经验。**两道闸，缺一不可。**
//
// 状态在用只说明有人认可它值得记下来；判定能归因才说明这个因果排除过混杂。
// 一条「👁 只是观察」的经验也可能被确认——确认的是「这个观察值得记」，
// 不是「照着做会有同样的结果」。放它进默认引用集，下一轮就会有人照着一个
// 没排除混杂的观察去做素材，而他不会知道自己在赌。
//
// 要引用 👁 的经验不是不行，但必须由人显式点名，不能是默认发生的。
func (e Experience) Reusable() bool {
	return e.Status == ExperienceConfirmed && e.Verdict == VerdictExplained
}

func (e Experience) StatusLabel() string {
	switch e.Status {
	case ExperiencePending:
		return "待定"
	case ExperienceConfirmed:
		return "在用"
	case ExperienceRetired:
		return "停用"
	}
	return string(e.Status)
}

// ReviewHint 只在界面上用。空串表示不用提示。
func (e Experience) ReviewHint() string {
	if e.NeedsReview {
		return "该看一眼了"
	}
	return ""
}
```

三个流转方法：

```go
// RequestExperienceReview 给在用的经验打上「该看一眼了」。
// **它不改状态**——这条经验还在用，标记只是提醒有人该重新看它的依据。
func (s Service) RequestExperienceReview(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, experienceID string, request ExperienceTransitionRequest) (Experience, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return Experience{}, err
	}
	if err := request.validate(true); err != nil {
		return Experience{}, err
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	return s.Repository.FlagExperienceForReview(ctx, FlagExperienceReviewInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: experienceID,
		ExpectedVersion: request.ExpectedVersion, NeedsReview: true,
		Reason: strings.TrimSpace(request.Reason), ActorID: actor.Principal.ID,
		Now: s.now(), AuditID: auditID,
	})
}
```

`ConfirmExperience` 的准入条件（原 `:656`）改成：

```go
	// 待定的确认，是「这条经验成立」；在用且标了复审的确认，是「重新看过了，还成立」。
	// 后者要顺手把标记摘掉，否则它会一直挂着，下次没人知道到底看过没有。
	if current.Status != ExperiencePending && !(current.Status == ExperienceConfirmed && current.NeedsReview) {
		return Experience{}, ErrInvalidState
	}
```

`RetireExperience` 的 from 列表去掉 `ExperienceNeedsReview`，只留 `ExperienceConfirmed`——标了复审的经验状态本来就是 confirmed，已经被覆盖。

- [ ] **Step 6: 改仓储**

`mysql_repository.go`：

- `:265` 的 `From: []ExperienceStatus{ExperiencePending, ExperienceNeedsReview}` → `{ExperiencePending, ExperienceConfirmed}`，并在 `ConfirmExperience` 的 UPDATE 里加 `needs_review = 0`（确认就等于看过了）。
- `:291` 的 from 列表去掉 `ExperienceNeedsReview`。
- 所有 SELECT 的列表加 `needs_review`，扫描到 `Experience.NeedsReview`；INSERT 加这一列。
- 新增 `FlagExperienceForReview`：单条 UPDATE（`needs_review`、`status_reason`、`status_changed_by`、`status_changed_at`、`updated_at`、`version+1`），guard 在 `version = ? AND status = 'confirmed'`，同事务写一条审计（`from_status` 和 `to_status` 都填 `confirmed`——状态确实没变，审计如实记）。

> `grep -rn "ExperienceNeedsReview" internal/ src/` 必须最终为空（`src/` 里那些改成读 `needs_review` 字段）。

- [ ] **Step 7: 跑全量并提交**

```bash
go build ./... && go test ./internal/systems/insights/...
```

```bash
git add internal/systems/insights/ migrations/insights/
git commit -m "feat(insights): 经验四态收成三态加一个复审标记"
```

---

### Task 2: 「查」模式的适用性筛选

**Files:**
- Create: `internal/systems/insights/experience_query.go`
- Create: `internal/systems/insights/experience_query_test.go`

**Interfaces:**
- Consumes: Task 1 的全部；既有 `Applicability`（`grep -n "type Applicability" -A 12 internal/systems/insights/service.go` 看它有哪些字段）。
- Produces:
  - `type ExperienceLookup struct{ Brand, Channel, AdType, Objective, Audience string; Feature string; IncludeObserved bool; Limit int }`
  - `type ExperienceMatch struct{ Experience Experience; Matched []string; Default bool }`
  - `func matchApplicability(value Experience, lookup ExperienceLookup) (ExperienceMatch, bool)`
  - `func (Service) LookupExperiences(context.Context, contract.ActorContext, contract.ProjectID, ExperienceLookup) ([]ExperienceMatch, error)`

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/experience_query_test.go`：

```go
package insights

import "testing"

func usable(applicability Applicability, verdict Verdict) Experience {
	value := Experience{Status: ExperienceConfirmed, Applicability: applicability}
	value.Verdict = verdict
	return value
}

// 适用条件是「这条经验在什么范围内成立」，不是标签。
// 筛的时候：经验没写这一格 = 不限，能匹配任何取值；写了就必须对上。
//
// 反过来做（没写就不匹配）会把绝大多数经验筛没——大部分经验只写了两三格。
func TestBlankApplicabilityMeansUnrestricted(t *testing.T) {
	t.Parallel()

	value := usable(Applicability{Channel: "抖音"}, VerdictExplained)
	if _, ok := matchApplicability(value, ExperienceLookup{Channel: "抖音", Brand: "某美妆"}); !ok {
		t.Error("经验没写品牌就等于不限品牌，应该匹配上")
	}
	if _, ok := matchApplicability(value, ExperienceLookup{Channel: "小红书"}); ok {
		t.Error("写了抖音就不该匹配小红书")
	}
}

// 匹配上了哪几格要说出来。人看到一条经验被推荐，第一个问题就是
// 「凭什么推给我」——答案是「因为渠道和广告类型都对上了」。
func TestMatchTellsWhichConditionsHit(t *testing.T) {
	t.Parallel()

	value := usable(Applicability{Channel: "抖音", AdType: "效果广告"}, VerdictExplained)
	match, ok := matchApplicability(value, ExperienceLookup{Channel: "抖音", AdType: "效果广告"})
	if !ok {
		t.Fatal("应该匹配上")
	}
	if len(match.Matched) != 2 {
		t.Errorf("应该报出两格匹配，得到 %v", match.Matched)
	}
}

// 默认只给能归因的。只是观察的要显式要（IncludeObserved），
// 而且要在结果里标出来它不是默认集里的。
func TestObservedExperiencesNeedAnExplicitAsk(t *testing.T) {
	t.Parallel()

	observed := usable(Applicability{Channel: "抖音"}, VerdictObserved)

	if _, ok := matchApplicability(observed, ExperienceLookup{Channel: "抖音"}); ok {
		t.Error("默认不该给出只是观察的经验")
	}

	match, ok := matchApplicability(observed, ExperienceLookup{Channel: "抖音", IncludeObserved: true})
	if !ok {
		t.Fatal("显式要了就该给")
	}
	if match.Default {
		t.Error("只是观察的经验即使给出来，也不能标成默认可引用")
	}
}

// 停用的和待定的永远不进「查」。查的人要的是「能照着做的」，
// 把没确认的混进去，他分不出哪条是已经有人背过书的。
func TestLookupExcludesPendingAndRetired(t *testing.T) {
	t.Parallel()

	for _, status := range []ExperienceStatus{ExperiencePending, ExperienceRetired} {
		value := Experience{Status: status, Applicability: Applicability{Channel: "抖音"}}
		value.Verdict = VerdictExplained
		if _, ok := matchApplicability(value, ExperienceLookup{Channel: "抖音"}); ok {
			t.Errorf("%s 的经验不该出现在「查」里", status)
		}
	}
}

// 标了「该看一眼了」的仍然出现在「查」里，但要能被界面认出来。
// 藏起来的话，等于悄悄拿掉了一条正在被引用的经验。
func TestFlaggedExperienceStillShowsUpInLookup(t *testing.T) {
	t.Parallel()

	value := usable(Applicability{Channel: "抖音"}, VerdictExplained)
	value.NeedsReview = true
	match, ok := matchApplicability(value, ExperienceLookup{Channel: "抖音"})
	if !ok {
		t.Fatal("标了复审的经验还在用，应该查得到")
	}
	if !match.Default {
		t.Error("标记不影响它是不是默认可引用")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestBlank|TestMatch|TestObserved|TestLookup|TestFlaggedExperienceStill' -v
```

Expected: `undefined: matchApplicability`。

- [ ] **Step 3: 实现**

创建 `internal/systems/insights/experience_query.go`：

```go
package insights

import (
	"context"
	"strings"

	"github.com/cookies/internal/platform/contract"
)

// 「查」模式：下一轮要做素材，先看以前什么有效。
//
// 这一段替代了原来的「投前洞察」页。那一页的五个视图——策略证据、创意建议、
// 历史模式、风险与反例、引用记录——前四个只是同一批经验按不同条件筛，
// 第五个是每条经验自己的引用历史。它们不是五个功能，是一个功能的五种切法，
// 所以做成筛选器和展开层，不做成五个入口。

// ExperienceLookup 是「查」的条件。每一格空着表示不限。
type ExperienceLookup struct {
	Brand     string `json:"brand,omitempty"`
	Channel   string `json:"channel,omitempty"`
	AdType    string `json:"ad_type,omitempty"`
	Objective string `json:"objective,omitempty"`
	Audience  string `json:"audience,omitempty"`
	// Feature 按内容特征找：「有没有关于开场的经验」。
	Feature string `json:"feature,omitempty"`

	// IncludeObserved 打开之后连「👁 只是观察」的也给。默认关着——
	// 查的人多数时候要的是能照着做的东西，混进观察会让他分不清哪条能信。
	IncludeObserved bool `json:"include_observed,omitempty"`
	Limit           int  `json:"limit,omitempty"`
}

// ExperienceMatch 是一条命中。Matched 说清「凭什么推给你」，
// Default 说清「能不能直接照着做」。
type ExperienceMatch struct {
	Experience Experience `json:"experience"`
	Matched    []string   `json:"matched"`
	Default    bool       `json:"default"`
}

// conditionHit 判断一格。经验没写 = 不限，能匹配任何取值；写了就必须对上。
//
// 反过来做（没写就不匹配）会把绝大多数经验筛没：大部分经验只写了两三格适用条件，
// 而查的人会把当前项目的五格全填上。
func conditionHit(experienceValue, lookupValue string) (hit bool, restricted bool) {
	experienceValue = strings.TrimSpace(experienceValue)
	if experienceValue == "" {
		return true, false
	}
	if strings.TrimSpace(lookupValue) == "" {
		return true, false // 查的人没限定这一格，就不用它来卡
	}
	return strings.EqualFold(experienceValue, lookupValue), true
}

func matchApplicability(value Experience, lookup ExperienceLookup) (ExperienceMatch, bool) {
	// 「查」只给在用的。待定的还没人背书，停用的已经被人撤下——
	// 混进去的话，看的人分不出哪条是能照着做的。
	if value.Status != ExperienceConfirmed {
		return ExperienceMatch{}, false
	}
	reusable := value.Reusable()
	if !reusable && !lookup.IncludeObserved {
		return ExperienceMatch{}, false
	}

	conditions := []struct {
		label      string
		experience string
		lookup     string
	}{
		{"品牌", value.Applicability.Brand, lookup.Brand},
		{"渠道", value.Applicability.Channel, lookup.Channel},
		{"广告类型", value.Applicability.AdType, lookup.AdType},
		{"目标", value.Applicability.Objective, lookup.Objective},
		{"受众", value.Applicability.Audience, lookup.Audience},
	}
	matched := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		hit, restricted := conditionHit(condition.experience, condition.lookup)
		if !hit {
			return ExperienceMatch{}, false
		}
		if restricted {
			matched = append(matched, condition.label)
		}
	}
	if feature := strings.TrimSpace(lookup.Feature); feature != "" {
		if !strings.Contains(value.Conclusion, feature) &&
			!strings.Contains(value.RecommendedAction, feature) {
			return ExperienceMatch{}, false
		}
		matched = append(matched, "内容特征")
	}
	return ExperienceMatch{Experience: value, Matched: matched, Default: reusable}, true
}

func (s Service) LookupExperiences(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, lookup ExperienceLookup) ([]ExperienceMatch, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	// 先按状态取在用的，再在内存里过适用条件。适用条件有六格、还有「空着等于不限」
	// 这条规则，写成 SQL 是六个 (col = '' OR col = ?)，改一格条件就要改一次 SQL；
	// 在用的经验一个项目也就几十上百条，内存里过一遍更清楚也更好改。
	values, err := s.Repository.ListExperiences(ctx, actor.OrganizationID, projectID,
		[]ExperienceStatus{ExperienceConfirmed}, normalizeLimit(0))
	if err != nil {
		return nil, err
	}
	matches := make([]ExperienceMatch, 0, len(values))
	for _, value := range values {
		match, ok := matchApplicability(value, lookup)
		if !ok {
			continue
		}
		matches = append(matches, match)
		if lookup.Limit > 0 && len(matches) >= lookup.Limit {
			break
		}
	}
	return matches, nil
}
```

> `Repository.ListExperiences` 的实际签名以 `grep -n "ListExperiences" internal/systems/insights/service.go` 为准；若它不收状态列表，就取全量后在循环里靠 `matchApplicability` 自己滤——它第一件事就是查状态，结果一样。
>
> `Applicability` 的字段名以 `service.go` 里的定义为准。若它没有 `Audience`/`Objective` 这两格，就删掉对应的两行，不要新加字段——那是另一件事。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/systems/insights/ -run 'TestBlank|TestMatch|TestObserved|TestLookup|TestFlaggedExperienceStill' -v
```

Expected: 五个测试全部 PASS。

- [ ] **Step 5: 挂路由并提交**

`Application` 接口加 `LookupExperiences`；`registerExperienceRoutes()` 加：

```go
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/experiences/lookup", s.lookupExperiences)
```

用 POST 而不是 GET：条件有七个，塞 query string 里既难读也容易漏转义。OpenAPI 里补接口与 `ExperienceMatch` schema。

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml
git commit -m "feat(insights): 按适用条件查经验"
```

---

### Task 3: 三行卡面与「凭什么」展开层

**Files:**
- Create: `src/components/insight/experience/ExperienceCard.tsx`
- Create: `src/components/insight/experience/EvidenceTrail.tsx`
- Modify: `src/data/api.ts`

**Interfaces:**
- Consumes: Task 1、Task 2 的接口；P0 的 `VerdictBadge`。
- Produces:
  - `export function ExperienceCard({ match, actions }: { match: ApiExperienceMatch; actions?: ReactNode })`
  - `export function EvidenceTrail({ experience }: { experience: ApiExperience })`

- [ ] **Step 1: 补 api 类型与方法**

`src/data/api.ts`：`ApiExperienceStatus` 收成三个取值 `'pending' | 'confirmed' | 'retired'`，`ApiExperience` 加 `needs_review: boolean`，并加：

```ts
export interface ApiExperienceMatch {
  experience: ApiExperience
  matched: string[]
  default: boolean
}

  lookupExperiences: (projectId: string, body: {
    brand?: string
    channel?: string
    ad_type?: string
    objective?: string
    audience?: string
    feature?: string
    include_observed?: boolean
    limit?: number
  }) => request<ApiExperienceMatch[]>(`${insightProjectPath(projectId)}/experiences/lookup`, 'POST', body),
```

- [ ] **Step 2: 写卡片**

创建 `src/components/insight/experience/ExperienceCard.tsx`：

```tsx
import { useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import type { ApiExperienceMatch } from '../../../data/api'
import { VerdictBadge } from '../shared/VerdictBadge'
import { EvidenceTrail } from './EvidenceTrail'

/**
 * 经验卡。
 *
 * **卡面只露三行：结论 / 适用条件 / 凭什么。** 原来的九个字段一个没删，
 * 全收进展开层——列表页上摊开九个字段，一屏放不下三条经验，而查经验的人
 * 是来扫一遍找出哪条跟自己有关的，不是来逐条精读的。
 */
export function ExperienceCard({ match, actions }: {
  match: ApiExperienceMatch
  actions?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const { experience } = match

  return <article className="experience-card">
    <header>
      <VerdictBadge verdict={experience.verdict} label={experience.verdict_label}/>
      <h4>{experience.conclusion}</h4>
      {experience.needs_review
        // 标记要看得见但不能吓人：这条经验还在用，只是该重新看一眼它的依据了。
        ? <span className="experience-review-flag">该看一眼了</span>
        : null}
    </header>

    <p className="experience-scope">
      适用：{formatScope(experience)}
      {match.matched.length
        ? <small>（{match.matched.join('、')}对上了）</small>
        : <small>（没设限，任何情况都适用）</small>}
    </p>

    <button type="button" className="text-button" onClick={() => setOpen(!open)}>
      <ChevronRight size={14} className={open ? 'rotated' : ''}/>
      凭什么{open ? '' : ' ▸'}
    </button>

    {!match.default ? <p className="experience-caveat">
      这条只是观察，没排除掉别的变量。可以参考，但别当成「照着做就会这样」。
    </p> : null}

    {open ? <EvidenceTrail experience={experience}/> : null}
    {actions ? <footer className="experience-actions">{actions}</footer> : null}
  </article>
}

function formatScope(experience: ApiExperienceMatch['experience']): string {
  const parts = [
    experience.applicability?.brand,
    experience.applicability?.channel,
    experience.applicability?.ad_type,
    experience.applicability?.objective,
    experience.applicability?.audience,
  ].filter(Boolean)
  return parts.length ? parts.join(' · ') : '不限'
}
```

- [ ] **Step 3: 写展开层**

创建 `src/components/insight/experience/EvidenceTrail.tsx`。它要能一路点回原始数据：结论 → 证据摘要 → 具体素材 → 来源复盘 → 原始指标。分四段：

1. **数据依据** —— 窗口、样本量、来源执行；来自 `data_basis`。
2. **内容依据** —— 涉及哪些变量；来自 `content_basis`。
3. **来源** —— 「来自 {报告 ID} 的复盘」，链接到复盘页那份报告。这一跳是整条证据链上最要紧的一环：人看完结论问的第一个问题是「这是哪次投放得出来的」。
4. **引用记录** —— 谁引用过、采纳还是改了还是拒了。原「投前洞察」的第五个视图并到这里：引用记录本来就是每条经验自己的属性，做成一个独立视图，人得先在那一页找到这条经验才能看它的历史。

反例（`counterexamples`）单独一段，标题写「什么情况下不成立」——这一格填了内容的经验才是真被人推敲过的，要显眼。

- [ ] **Step 4: 构建并提交**

```bash
npm run build
```

```bash
git add src/components/insight/experience/ src/data/api.ts src/styles.css
git commit -m "feat(insights-web): 经验卡三行卡面与证据链展开层"
```

---

### Task 4: 「查」和「管」两个模式

**Files:**
- Create: `src/components/insight/experience/LookupView.tsx`
- Create: `src/components/insight/experience/ManageView.tsx`
- Create: `src/components/insight/experience/ExperiencePage.tsx`
- Create: `src/components/insight/experience/index.ts`
- Modify: `src/components/ExperienceReviseForm.tsx`（只改引入路径）
- Reference: `src/components/ExperienceLibraryPage.tsx`、`src/components/PreLaunchInsightPage.tsx`

**Interfaces:**
- Consumes: Task 3 的 `ExperienceCard`；既有 `ExperienceReviseForm`。
- Produces:
  - `export function ExperiencePage({ view }: { view: ExperienceView })`
  - `export type ExperienceView = 'lookup' | 'manage'`

- [ ] **Step 1: 写「查」**

创建 `src/components/insight/experience/LookupView.tsx`。要点：

- 进来**先自动按当前项目的条件筛一遍**，不是给一张空表单等人填。查经验的人手上就是当前这个项目，让他把品牌渠道再敲一遍是白费功夫。条件预填成当前项目的值，可改可清。
- 筛选器是五个下拉 + 一个内容特征输入框 + 一个「连只是观察的也看」开关。
- 顶部一行说清系统按什么筛的：

```tsx
    <p className="lookup-context">
      当前项目：{[project.brand, project.channel, project.adType].filter(Boolean).join(' · ') || '未设置条件'}
      　按这些条件筛出 {matches.length} 条能用的经验。
    </p>
```

- 空结果不能只写「暂无数据」。要分清两种空：

```tsx
  const emptyHint = hasAnyConfirmed
    ? '这些条件下还没有能用的经验。放宽条件再看看，或者去复盘里沉淀一条。'
    : '这个项目还没有确认过的经验。经验来自复盘——投完一轮、提交复盘、有人确认，它才会出现在这里。'
```

第二种情况在项目刚起步时是常态，说成「暂无数据」的话，人会以为是系统坏了。

- [ ] **Step 2: 写「管」**

创建 `src/components/insight/experience/ManageView.tsx`。从 `ExperienceLibraryPage.tsx` 搬确认 / 驳回 / 复审 / 停用 / 修订这五个动作和 `ExperienceReviseForm` 的接线，改三处：

1. 列表按 `待定` → `在用且标了复审` → `在用` → `停用` 排。待定和标了复审的是**要人做事的**，排前面；其余是存档。
2. 卡片换成 Task 3 的 `ExperienceCard`，动作按钮通过 `actions` 传进去。
3. 状态标签用 Task 1 的三个词，`needs_review` 单独渲染成标记，不再当成第四种状态。

顶部一行给出待办数量——「管」是低频模式，人进来第一件事是看有没有事要做：

```tsx
    <p className="manage-context">
      {pendingCount} 条待定、{flaggedCount} 条该看一眼了。
    </p>
```

- [ ] **Step 3: 写壳**

创建 `src/components/insight/experience/ExperiencePage.tsx`，按 `view` 分发；`index.ts` 出口 `ExperiencePage` 与 `ExperienceView`。

- [ ] **Step 4: 在浏览器里走一遍**

用 `preview_start` 起服务，确认：

1. 「经验 · 查」自动按当前项目筛出结果，顶部说清了按什么筛的。
2. 点「凭什么」展开，四段证据都在，能看到来源复盘的链接。
3. 打开「连只是观察的也看」，多出来的那些卡片上有「这条只是观察」那句提醒。
4. 「经验 · 管」里待定的排在最前；确认一条待定的，它移到在用。
5. 给一条在用的点「该看一眼了」，它**仍然出现在「查」里**，卡上多了标记。这一条要专门确认——它是这次状态改造成不成立的判据。
6. `preview_console_logs` 无报错。

- [ ] **Step 5: 提交**

```bash
git add src/components/insight/experience/ src/components/ExperienceReviseForm.tsx
git commit -m "feat(insights-web): 经验入口的查与管两个模式"
```

---

### Task 5: 下游引用带上适用条件与来源

**Files:**
- Modify: `internal/systems/insights/experience_query.go`
- Modify: `internal/systems/insights/experience_query_test.go`
- Modify: `src/components/insight/experience/EvidenceTrail.tsx`

**Interfaces:**
- Consumes: Task 2 的 `ExperienceMatch`；既有 `RecordExperienceReference`。
- Produces: `func (ExperienceMatch) CitationText() string`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/experience_query_test.go` 追加：

```go
// 引用一条经验时必须带上适用条件和来源。
//
// 只把结论抄走，下游拿到的是一句没有边界的断言：「痛点开场点击率高 38%」
// ——在什么渠道？什么广告类型？基于哪次投放？答不上来的话，这句话会被用到
// 它根本不成立的地方去。
func TestCitationCarriesScopeAndSource(t *testing.T) {
	t.Parallel()

	value := usable(Applicability{Channel: "抖音", AdType: "效果广告"}, VerdictExplained)
	value.Conclusion = "痛点开场比产品开场点击率高 38%"
	value.ReportID = "report_1"

	text := ExperienceMatch{Experience: value, Default: true}.CitationText()
	for _, want := range []string{"痛点开场", "抖音", "效果广告", "report_1"} {
		if !strings.Contains(text, want) {
			t.Errorf("引用文本里少了 %q：%s", want, text)
		}
	}
}

// 只是观察的经验被引用时，那句提醒必须跟着走。
// 它留在界面上而没进引用文本的话，抄到下游就变成了一条看起来同样可靠的结论。
func TestCitationOfObservedCarriesTheCaveat(t *testing.T) {
	t.Parallel()

	value := usable(Applicability{Channel: "抖音"}, VerdictObserved)
	value.Conclusion = "15 秒整体好过 30 秒"
	text := ExperienceMatch{Experience: value, Default: false}.CitationText()
	if !strings.Contains(text, "只是观察") {
		t.Errorf("提醒没跟着引用走：%s", text)
	}
}
```

文件顶部补 `"strings"` 引入。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestCitation' -v
```

Expected: `match.CitationText undefined`。

- [ ] **Step 3: 实现**

`experience_query.go` 追加：

```go
// CitationText 是这条经验被抄到下游时的完整说法。
//
// 只抄结论的话，下游拿到的是一句没有边界的断言——「痛点开场点击率高 38%」，
// 在什么渠道、什么广告类型、基于哪次投放，全丢了。这句话于是会被用到它根本
// 不成立的地方去，而用它的人没有任何线索能发现这一点。
func (m ExperienceMatch) CitationText() string {
	parts := []string{m.Experience.Conclusion}
	if scope := m.Experience.Applicability.Summary(); scope != "" {
		parts = append(parts, "适用："+scope)
	}
	if !m.Default {
		parts = append(parts, "（只是观察，没排除掉别的变量，别当成照着做就会这样）")
	}
	if m.Experience.ReportID != "" {
		parts = append(parts, "来源："+m.Experience.ReportID)
	}
	return strings.Join(parts, "　")
}
```

`Applicability.Summary()` 若不存在就在同一个文件里加一个——把非空的几格用 ` · ` 连起来，和前端 `formatScope` 同一个规则。**两处规则必须一致**，否则界面上看到的适用范围和抄出去的不是一句话。

- [ ] **Step 4: 前端接上**

`EvidenceTrail.tsx` 的引用记录那一段加一个「复制引用」按钮，复制的内容就是后端这段文本的同构实现。**不要在前端另写一套拼法**——调 `match.citation_text`（把它作为 `ExperienceMatch` 的一个 JSON 字段返回）。

- [ ] **Step 5: 跑测试并提交**

```bash
go test ./internal/systems/insights/... && npm run build
```

```bash
git add internal/systems/insights/ src/components/insight/experience/
git commit -m "feat(insights): 引用经验时带上适用条件与来源"
```

---

### Task 6: 导航从「投前洞察 + 经验库」收敛成「经验」

> **这一步修改现有导航结构。开始前必须向使用者确认。**

**Files:**
- Modify: `src/data/navigation.ts:43,57`
- Modify: 渲染分发处

- [ ] **Step 1: 改导航条目**

删掉 `prelaunch`（第 43 行）和 `knowledge`（第 57 行，label 是「经验库」）两条，换成一条。

> 注意 id 是 `knowledge` 不是 `experience`——按 label 猜 id 会删错条目；第 65 行是「能力运营」`operations`，那是 P5 的事。

```ts
      // 「经验」= 原经验库 + 原投前洞察。
      // 合并的理由：这两页是同一批数据的两种读法。投前洞察的五个视图里，
      // 前四个只是同一批经验按不同条件筛，第五个（引用记录）是每条经验自己的
      // 属性——做成独立视图，人得先在那一页找到这条经验才能看它的历史。
      {
        id: 'experience', label: '经验', icon: Lightbulb, group: '工作', layout: 'analysis',
        description: '以前什么有效、在什么条件下成立、凭什么这么说。',
        views: ['查经验', '管经验'],
      },
```

`SearchCheck` 图标若不再被引用，从 import 里删掉。

- [ ] **Step 2: 改渲染分发**

```bash
grep -rn "'prelaunch'\|\"prelaunch\"" src/ | grep -v api.ts
```

```ts
const experienceViews: Record<string, ExperienceView> = {
  查经验: 'lookup',
  管经验: 'manage',
}
```

`src/App.tsx` 里若把 `'prelaunch'` 作为落地页（P1 已改成 `'analysis'`，确认一遍没有残留）。

- [ ] **Step 3: 跑全量**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

用 `preview_snapshot` 核对侧栏：洞察下面是「分析」「复盘」「经验」「素材」，原「投前洞察」「经验库」不在了。

- [ ] **Step 4: 提交**

```bash
git add src/data/navigation.ts src/components/
git commit -m "feat(insights-web): 投前洞察与经验库收敛成「经验」入口"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档「模块三 · 经验」：

| 规格要求 | 落在 |
|---|---|
| 经验库 + 投前洞察合并 | Task 4 + Task 6 |
| 查 / 管两个模式共用一套卡片 | Task 3 的 `ExperienceCard`，两个视图都用它 |
| 卡面只露三行，九字段收进展开层 | Task 3 Step 2 + Step 3 |
| 按品牌 / 渠道 / 广告类型 / 目标 / 受众 / 内容特征检索（AM-013） | Task 2 的 `ExperienceLookup` 六格 |
| 一路点回原始数据 | Task 3 Step 3 的四段证据链 |
| 确认 / 驳回 / 修订，修订新增版本 | Task 4 Step 2（沿用既有服务，没动血缘逻辑） |
| 下游引用带适用条件与来源链接，可回传采纳/修改/拒绝（AM-014） | Task 5 + 既有 `RecordExperienceReference` |
| 三档判定继承自来源结论 | P0 已给 `Experience` 内嵌 `Judgement`；Task 1 的 `Reusable()` 用它 |
| **下游默认只引用「在用」且「✅ 能归因」** | Task 1 的 `Reusable()` 两道闸 + Task 2 的 `IncludeObserved` 默认关 + Task 5 的引用提醒 |
| 四态 → 三态 + 复审标记 | Task 1 |
| 状态改名：待定 / 在用 / 停用 | Task 1 的 `StatusLabel()` + 一条盯着这三个词的测试 |
| 引用记录并进展开层 | Task 3 Step 3 第四段 |

**未覆盖且是有意的：**

- **不自动发现反证**（AM-018）—— 设计文档明确后置。新数据和老经验冲突，现在靠人在复盘里自己看出来。Task 1 的「该看一眼了」标记是给这件事留的位置：将来自动检测出冲突时，它打的就是这个标记。
- **不自动编排多条经验成实验计划**（AM-020）—— 设计文档标了 P2 不做。
- **原「投前洞察」里的创意建议生成** —— 那是把经验喂给模型产出建议，属于创意侧的能力，不在洞察这边实现。本期只把经验按条件筛出来交给人。
- **跨项目查经验** —— 现在只查当前 Project。跨项目要先解决适用条件在不同项目间是不是同一套词的问题，那是变量字典（第 6 期设置）的事。

**2. 占位扫描** —— 无 TBD / TODO / 「同 Task N」。Task 1 Step 6（改仓储）、Task 3 Step 3（展开层）、Task 4 Step 1/2（两个视图）给的是「改哪几处、每处改成什么、为什么」而不是整段代码：这几处要么必须和现有 SQL 列名逐字对齐，要么是从两个现存页面搬运既有逻辑——抄一段可能对不上的代码比说清约束更容易出错。每一处都点了名要改的行为和判据。

**3. 类型一致性** —— `ExperienceStatus` 三个取值在 Go 常量、迁移的 CHECK、`ApiExperienceStatus`、`StatusLabel()` 四处一致。`needs_review` 在迁移列名、Go 字段 JSON tag、`ApiExperience` 三处一致。`ExperienceView` 的两个取值与 `experienceViews` 映射表的两个键一致。`Applicability.Summary()`（Go）与 `formatScope()`（TS）用同一个连接规则，这一点在 Task 5 Step 3 里写明了。`ExperienceMatch` 的 `Default` 字段在 JSON 里是 `default`——这是 TS 的保留字，所以前端只能写 `match.default`（属性访问合法，不能当变量名解构），Task 3 Step 2 里用的正是属性访问。

---

## 依赖关系

前置：P0 全部（`Judgement`、`VerdictBadge`）、P2 复盘（经验的唯一上游是提交后的复盘）。
后继：P5 设置里的「确认权限」决定谁能点 Task 4 里的确认按钮。
与 P3 的关系：无耦合，可并行。
