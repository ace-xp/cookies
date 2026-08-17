# 素材洞察 · 模块五「设置」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「数据质量」「能力运营」「系统设置」合成一个叫「设置」的入口，并把判定阈值从代码常量改成可写、带版本的配置——同时让每一条结论都记下它是用哪一版阈值判出来的。

**Architecture:** 三合一的逻辑是它们本来就是一件事的三面：数据质量是「配置对不对」的体检报告，能力运营是变量字典的维护，系统设置是判定阈值。阈值可写这件事是这一期的分量所在——这些数字直接决定三档结论怎么判，不能改就等于这个模块的判断标准不可调，而不同行业、不同渠道的合理门槛本来就不一样。可写必须配一件事：**阈值集只增版本、从不原地修改**，每条结论盖上它用的那一版号。没有这一条，改完阈值之后所有历史结论都说不清是按什么标准算的，而经验库是「新增版本不覆盖、历史可审计」的——一批说不清依据的经验会永远挂在账上。

**Tech Stack:** Go 1.22+、MySQL 8、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`。

## Global Constraints

- 前置依赖：**P0 地基**（`Judgement`）。Task 3 会往 `Judgement` 里加字段，必须和 P0 的契约测试对齐。
- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- 不引入任何新依赖。
- **阈值集不可原地修改。** 保存 = 新增一个版本并把它设为在用。任何 `UPDATE insight_threshold_sets SET <阈值列> = ?` 都是错的。
- **代码常量仍然是默认值的唯一来源。** 落库的是「有人调过的那些」，没调过的从常量取。这样常量改了，没调过的部署跟着走；调过的部署保持人当初的决定。
- 判定的实现全模块只有一处（`group_compare.go`）。阈值要顺着那一处进去，**不许在第二个地方再读一次阈值**。
- 迁移文件放 `migrations/insights/`，命名 `YYYYMMDDHHMMSS_<描述>.up.sql`，文件头必须有中文注释说明为什么要改。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。

> **需要确认的破坏性动作：** Task 2（判定改成读配置而不是常量）、Task 3（往 `Judgement` 加字段）、Task 5（删导航条目）。这三处修改现有业务逻辑，**开始前必须向使用者确认**。Task 1、Task 4 是新增，可直接做。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `migrations/insights/20260811130000_insight_threshold_sets.up.sql` | 阈值集表 | 新建 |
| `internal/systems/insights/thresholds.go` | 阈值集模型、默认值、校验、解析 | 新建 |
| `internal/systems/insights/thresholds_test.go` | 阈值的全部约束 | 新建 |
| `internal/systems/insights/mysql_threshold_repository.go` | 阈值集落库 | 新建 |
| `internal/systems/insights/group_compare.go` | 判定改为读传进来的阈值 | 改 |
| `internal/systems/insights/connectors.go:267-268` | 两个样本常量降级为默认值 | 改 |
| `internal/systems/insights/performance.go:213,216` | 两个天数常量降级为默认值 | 改 |
| `internal/systems/insights/settings.go` | 判定阈值那一组改为可写；并入体检与字典两组 | 改 |
| `internal/systems/insights/httpapi/server.go` | 两条新路由 | 改 |
| `api/openapi/insights-v1.yaml` | 接口与 schema | 改 |
| `src/components/insight/settings/SettingsPage.tsx` | 设置入口的壳 | 新建 |
| `src/components/insight/settings/ThresholdView.tsx` | ① 判定阈值（可写） | 新建 |
| `src/components/insight/settings/HealthView.tsx` | ② 数据体检（原数据质量） | 新建 |
| `src/components/insight/settings/DictionaryView.tsx` | ③ 变量字典（原能力运营） | 新建 |
| `src/components/insight/settings/PermissionView.tsx` | ④ 确认权限 | 新建 |
| `src/components/insight/settings/index.ts` | 出口 | 新建 |
| `src/components/insight/shared/ThresholdStamp.tsx` | 「本次判定使用的阈值」标注 | 新建 |
| `src/data/api.ts` | `getThresholds`、`saveThresholds` | 改 |
| `src/data/navigation.ts:62,65,66` | 数据质量 + 能力运营 + 系统设置 → 设置 | 改 |

---

### Task 1: 阈值集落库，只增版本

**Files:**
- Create: `migrations/insights/20260811130000_insight_threshold_sets.up.sql`
- Create: `internal/systems/insights/thresholds.go`
- Create: `internal/systems/insights/thresholds_test.go`
- Create: `internal/systems/insights/mysql_threshold_repository.go`

**Interfaces:**
- Consumes: 既有常量 `sufficientSampleImpressions`（`connectors.go:267`）、`directionalSampleImpressions`（`:268`）、`minTrendDays`（`performance.go:213`）、`minAnomalyDays`（`:216`）、`minDriverAssets`（`:962`）、`maxComparisonAssets`（`:206`）、`preLaunchQualityWindowDays`（`prelaunch.go:287`）。
- Produces:
  - `type Thresholds struct{...}`（见 Step 3；每格是 `*int` / `*float64`，nil 表示没人调过）
  - `type ThresholdSet struct{ Version int64; Values Thresholds; Reason string; ChangedBy string; ChangedAt time.Time }`
  - `func defaultThresholds() ResolvedThresholds` —— 从代码常量取
  - `type ResolvedThresholds struct{ Version int64; SufficientImpressions, DirectionalImpressions, MinTrendDays, MinAnomalyDays, MinDriverAssets, MaxComparisonAssets, QualityWindowDays int }`
  - `func resolve(set ThresholdSet) ResolvedThresholds` —— 有值用值，没值用默认
  - `func (Thresholds) Validate() error`
  - `ThresholdRepository` 接口三个方法

- [ ] **Step 1: 写迁移**

创建 `migrations/insights/20260811130000_insight_threshold_sets.up.sql`：

```sql
-- 判定阈值从代码常量改成可写配置。
--
-- 这些数字直接决定三档结论怎么判——多少曝光算「充分」、几天才给趋势、跌多少算
-- 疲劳。不能改，等于这个模块的判断标准不可调；而不同行业、不同渠道的合理门槛
-- 本来就不一样，写死一套是错的。
--
-- **只增版本，从不原地改。** 每次保存插一行新的，旧行永远留着。没有这一条，
-- 改完阈值之后所有历史结论都说不清是按什么标准算出来的——而经验库是「新增版本
-- 不覆盖、历史可审计」的，一批说不清依据的经验会永远挂在账上，说不清是对是错。
--
-- 值列全部可空。**NULL 表示「没人调过这一格，用代码默认值」**，不是 0。
-- 存成 0 的话，将来改了代码默认值，那些从没被调过的部署不会跟着走，
-- 而它们本来应该跟着走。
CREATE TABLE insight_threshold_sets (
  id              VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
  organization_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  -- version 在组织内单调递增。它就是盖在每条结论上的那个号码。
  version         BIGINT NOT NULL,

  sufficient_impressions  INT NULL,
  directional_impressions INT NULL,
  min_trend_days          INT NULL,
  min_anomaly_days        INT NULL,
  min_driver_assets       INT NULL,
  max_comparison_assets   INT NULL,
  quality_window_days     INT NULL,

  -- reason 必填。改判定标准是一件要负责的事，写不出理由的改动多半是试出来的，
  -- 而试出来的阈值会在三个月后没人说得清为什么是这个数。
  reason      VARCHAR(1000) NOT NULL,
  changed_by  VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  changed_at  DATETIME(6) NOT NULL,

  UNIQUE KEY uq_insight_threshold_sets_version (organization_id, version),
  CONSTRAINT chk_insight_threshold_sets_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> 没有 `active` 列：**版本号最大的那一版就是在用的那一版**。加一个 active 标记就多一种「最大版本不是在用版本」的状态，而没有任何场景需要回滚到旧版——要回滚，就照旧值再存一版，理由栏写清为什么回。这样回滚本身也在历史里。

- [ ] **Step 2: 跑迁移**

```bash
go run ./cmd/cookies-migrate
```

Expected: 无错误。

- [ ] **Step 3: 写失败的测试**

创建 `internal/systems/insights/thresholds_test.go`：

```go
package insights

import "testing"

func intPtr(value int) *int { return &value }

// 没人调过的格子用代码默认值。
//
// 存成 0 或者在建表时把默认值写死进去，都会切断这条线：将来改了代码默认值，
// 那些从没被调过的部署不会跟着走，而它们本来应该跟着走。
func TestUnsetThresholdsFallBackToCodeDefaults(t *testing.T) {
	t.Parallel()

	got := resolve(ThresholdSet{Version: 3, Values: Thresholds{
		SufficientImpressions: intPtr(5000),
	}})

	if got.SufficientImpressions != 5000 {
		t.Errorf("调过的那格应该用调过的值，得到 %d", got.SufficientImpressions)
	}
	if got.DirectionalImpressions != directionalSampleImpressions {
		t.Errorf("没调过的那格应该用默认值 %d，得到 %d",
			directionalSampleImpressions, got.DirectionalImpressions)
	}
	if got.Version != 3 {
		t.Errorf("版本号要带上，得到 %d", got.Version)
	}
}

// 一条阈值都没存过时，解析出来的就是全套代码默认值，版本号为 0。
// 0 是有意义的：它表示「谁也没调过，跑的是出厂设定」。
func TestNoThresholdSetMeansVersionZero(t *testing.T) {
	t.Parallel()

	got := resolve(ThresholdSet{})
	if got.Version != 0 {
		t.Errorf("没存过应该是第 0 版，得到 %d", got.Version)
	}
	if got.SufficientImpressions != sufficientSampleImpressions {
		t.Errorf("应该是代码默认值，得到 %d", got.SufficientImpressions)
	}
}

// 「充分」的门槛不能低于「有方向」的门槛。
//
// 反过来设的话，一个样本量会同时满足「充分」和「不够充分只能看方向」，
// 判定顺序说了算——那意味着阈值页上两个看起来独立的数字，实际效果取决于
// 代码里 if 的先后。这种配置必须在保存时就拦下来。
func TestSufficientMustNotBeBelowDirectional(t *testing.T) {
	t.Parallel()

	bad := Thresholds{
		SufficientImpressions:  intPtr(500),
		DirectionalImpressions: intPtr(1000),
	}
	if err := bad.Validate(); err == nil {
		t.Error("充分门槛低于方向门槛应该被拒")
	}

	ok := Thresholds{SufficientImpressions: intPtr(8000), DirectionalImpressions: intPtr(1000)}
	if err := ok.Validate(); err != nil {
		t.Errorf("合法组合被拒了：%v", err)
	}
}

// 天数下限不能设成 1。一天的数据算不出趋势，也判不了异常——
// 允许设成 1，等于允许人把「拒绝下结论」这条规则关掉，而那是这个模块的灵魂。
func TestDayThresholdsHaveAFloor(t *testing.T) {
	t.Parallel()

	for _, value := range []Thresholds{
		{MinTrendDays: intPtr(1)},
		{MinAnomalyDays: intPtr(2)},
	} {
		if err := value.Validate(); err == nil {
			t.Errorf("天数低于下限应该被拒：%+v", value)
		}
	}
}

// 非正数一律拒。0 次曝光算充分等于取消样本门槛。
func TestNonPositiveThresholdsAreRejected(t *testing.T) {
	t.Parallel()

	if err := (Thresholds{SufficientImpressions: intPtr(0)}).Validate(); err == nil {
		t.Error("0 应该被拒")
	}
	if err := (Thresholds{MinDriverAssets: intPtr(-1)}).Validate(); err == nil {
		t.Error("负数应该被拒")
	}
}
```

- [ ] **Step 4: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestUnsetThresholds|TestNoThresholdSet|TestSufficientMust|TestDayThresholds|TestNonPositive' -v
```

Expected: `undefined: resolve`。

- [ ] **Step 5: 实现模型**

创建 `internal/systems/insights/thresholds.go`：

```go
package insights

import (
	"context"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 判定阈值。
//
// 这些数字决定三档结论怎么判：多少曝光算「充分」、几天才给趋势、几条素材才谈
// 驱动因素。它们原来是散在五个文件里的代码常量——看不见，也调不了。
// 一个看不见的阈值和一个错的阈值，在使用者那里是同一种东西。
//
// 改成可写之后有两条硬约束：
//   - **只增版本，从不原地改**；
//   - **每条结论盖上它用的那一版号**（见 Task 3）。
// 少了任何一条，改完阈值之后历史结论就说不清是按什么标准算的。

// Thresholds 是「有人调过的那些」。
//
// 每格都是指针：**nil 表示没人调过这一格，用代码默认值**，不是 0。
// 存成 0 的话，将来改了代码默认值，那些从没被调过的部署不会跟着走，
// 而它们本来应该跟着走。
type Thresholds struct {
	SufficientImpressions  *int `json:"sufficient_impressions,omitempty"`
	DirectionalImpressions *int `json:"directional_impressions,omitempty"`
	MinTrendDays           *int `json:"min_trend_days,omitempty"`
	MinAnomalyDays         *int `json:"min_anomaly_days,omitempty"`
	MinDriverAssets        *int `json:"min_driver_assets,omitempty"`
	MaxComparisonAssets    *int `json:"max_comparison_assets,omitempty"`
	QualityWindowDays      *int `json:"quality_window_days,omitempty"`
}

// 天数的下限。允许设成 1，等于允许人把「拒绝下结论」这条规则关掉，
// 而那是这个模块和一张 BI 报表的全部区别。
const (
	floorTrendDays   = 3
	floorAnomalyDays = 4
)

func (t Thresholds) Validate() error {
	positives := []*int{
		t.SufficientImpressions, t.DirectionalImpressions, t.MinTrendDays,
		t.MinAnomalyDays, t.MinDriverAssets, t.MaxComparisonAssets, t.QualityWindowDays,
	}
	for _, value := range positives {
		if value != nil && *value <= 0 {
			return ErrInvalidRequest
		}
	}
	if t.MinTrendDays != nil && *t.MinTrendDays < floorTrendDays {
		return ErrInvalidRequest
	}
	if t.MinAnomalyDays != nil && *t.MinAnomalyDays < floorAnomalyDays {
		return ErrInvalidRequest
	}
	// 「充分」不能低于「有方向」。反过来设的话，一个样本量会同时满足两档，
	// 谁生效取决于代码里 if 的先后——阈值页上两个看起来独立的数字，
	// 实际效果却由实现顺序决定，这种配置必须在保存时就拦下来。
	sufficient := pickInt(t.SufficientImpressions, sufficientSampleImpressions)
	directional := pickInt(t.DirectionalImpressions, directionalSampleImpressions)
	if sufficient < directional {
		return ErrInvalidRequest
	}
	return nil
}

// ThresholdSet 是落库的一版。
type ThresholdSet struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	Version        int64                   `json:"version"`
	Values         Thresholds              `json:"values"`
	Reason         string                  `json:"reason"`
	ChangedBy      string                  `json:"changed_by"`
	ChangedAt      time.Time               `json:"changed_at"`
}

// ResolvedThresholds 是判定真正拿去用的那份：每格都有值，且带着版本号。
//
// 判定链路上一律传这个，不传 ThresholdSet——传后者的话，每个用到阈值的地方
// 都要自己做一次「nil 就取默认」，迟早有一处漏了，那一处就会拿 0 去比。
type ResolvedThresholds struct {
	Version int64 `json:"version"`

	SufficientImpressions  int `json:"sufficient_impressions"`
	DirectionalImpressions int `json:"directional_impressions"`
	MinTrendDays           int `json:"min_trend_days"`
	MinAnomalyDays         int `json:"min_anomaly_days"`
	MinDriverAssets        int `json:"min_driver_assets"`
	MaxComparisonAssets    int `json:"max_comparison_assets"`
	QualityWindowDays      int `json:"quality_window_days"`
}

func pickInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// defaultThresholds 是出厂设定，全部来自代码常量。
//
// **代码常量仍然是默认值的唯一来源**：落库的只有「有人调过的那些」。
// 这样常量改了，没调过的部署跟着走；调过的部署保持人当初的决定。
func defaultThresholds() ResolvedThresholds {
	return ResolvedThresholds{
		Version:                0,
		SufficientImpressions:  sufficientSampleImpressions,
		DirectionalImpressions: directionalSampleImpressions,
		MinTrendDays:           minTrendDays,
		MinAnomalyDays:         minAnomalyDays,
		MinDriverAssets:        minDriverAssets,
		MaxComparisonAssets:    maxComparisonAssets,
		QualityWindowDays:      preLaunchQualityWindowDays,
	}
}

func resolve(set ThresholdSet) ResolvedThresholds {
	value := defaultThresholds()
	value.Version = set.Version
	value.SufficientImpressions = pickInt(set.Values.SufficientImpressions, value.SufficientImpressions)
	value.DirectionalImpressions = pickInt(set.Values.DirectionalImpressions, value.DirectionalImpressions)
	value.MinTrendDays = pickInt(set.Values.MinTrendDays, value.MinTrendDays)
	value.MinAnomalyDays = pickInt(set.Values.MinAnomalyDays, value.MinAnomalyDays)
	value.MinDriverAssets = pickInt(set.Values.MinDriverAssets, value.MinDriverAssets)
	value.MaxComparisonAssets = pickInt(set.Values.MaxComparisonAssets, value.MaxComparisonAssets)
	value.QualityWindowDays = pickInt(set.Values.QualityWindowDays, value.QualityWindowDays)
	return value
}

// ThresholdRepository 只有读最新一版和追加一版两件事。**没有更新方法**——
// 接口上就不给原地改的口子，比在注释里写「请勿修改」可靠。
type ThresholdRepository interface {
	// LatestThresholdSet 返回版本号最大的那一版；一版都没有时返回零值和 ErrNotFound。
	LatestThresholdSet(context.Context, contract.OrganizationID) (ThresholdSet, error)
	AppendThresholdSet(context.Context, ThresholdSet) (ThresholdSet, error)
	ListThresholdSets(context.Context, contract.OrganizationID, int) ([]ThresholdSet, error)
}

// SaveThresholdsRequest 的理由是必填的。改判定标准是一件要负责的事；
// 写不出理由的改动多半是试出来的，而试出来的阈值三个月后没人说得清为什么是这个数。
type SaveThresholdsRequest struct {
	Values Thresholds `json:"values"`
	Reason string     `json:"reason"`
}

func (r SaveThresholdsRequest) Validate() error {
	if strings.TrimSpace(r.Reason) == "" {
		return ErrInvalidRequest
	}
	if len(r.Reason) > 1000 {
		return ErrInvalidRequest
	}
	return r.Values.Validate()
}
```

- [ ] **Step 6: 实现服务与仓储**

`thresholds.go` 追加：

```go
// currentThresholds 是判定链路的唯一入口。读不到就用出厂设定——
// 阈值读失败不该让整个分析页打不开，跑默认值至少还能出结论，且版本号 0
// 会在页面上如实显示成「出厂设定」。
func (s Service) currentThresholds(ctx context.Context, org contract.OrganizationID) ResolvedThresholds {
	set, err := s.Thresholds.LatestThresholdSet(ctx, org)
	if err != nil {
		return defaultThresholds()
	}
	return resolve(set)
}

func (s Service) GetThresholds(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID) (ResolvedThresholds, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return ResolvedThresholds{}, err
	}
	return s.currentThresholds(ctx, actor.OrganizationID), nil
}

// SaveThresholds 追加一版，不改任何已有的行。
func (s Service) SaveThresholds(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request SaveThresholdsRequest) (ResolvedThresholds, error) {
	// 用 ScopeConfirm 而不是 ScopeWrite：改的是全组织的判定标准，
	// 影响面比写一条数据大得多，应该和「确认经验」同一个门槛。
	if err := s.ready(actor, projectID, ScopeConfirm); err != nil {
		return ResolvedThresholds{}, err
	}
	if err := request.Validate(); err != nil {
		return ResolvedThresholds{}, err
	}
	id, err := s.idGenerator()("thresholdset")
	if err != nil {
		return ResolvedThresholds{}, err
	}
	next := int64(1)
	if previous, findErr := s.Thresholds.LatestThresholdSet(ctx, actor.OrganizationID); findErr == nil {
		next = previous.Version + 1
	}
	saved, err := s.Thresholds.AppendThresholdSet(ctx, ThresholdSet{
		ID: id, OrganizationID: actor.OrganizationID, Version: next,
		Values: request.Values, Reason: strings.TrimSpace(request.Reason),
		ChangedBy: actor.Principal.ID, ChangedAt: s.now(),
	})
	if err != nil {
		return ResolvedThresholds{}, err
	}
	return resolve(saved), nil
}
```

`Service` 加字段 `Thresholds ThresholdRepository`，在装配处传进去（`grep -rn "insights.Service{" cmd/ internal/ --include=*.go`）。

创建 `internal/systems/insights/mysql_threshold_repository.go`：三个方法，照 `mysql_repository.go` 的风格。`AppendThresholdSet` 是纯 INSERT；唯一键冲突（两个人同时保存）映射成 `ErrConflict`，让后保存的人重试一次——两个人同时改判定标准，后来的那个必须看到前一个改成了什么。可空列用 `sql.NullInt64` 读写。

- [ ] **Step 7: 跑测试并提交**

```bash
go test ./internal/systems/insights/ -run 'Threshold' -v && go build ./...
```

```bash
git add internal/systems/insights/ migrations/insights/
git commit -m "feat(insights): 判定阈值落库，只增版本"
```

---

### Task 2: 判定读配置，不读常量

> **修改现有业务逻辑。开始前必须向使用者确认。**

**Files:**
- Modify: `internal/systems/insights/group_compare.go`
- Modify: `internal/systems/insights/performance.go`（调用处传阈值）
- Modify: `internal/systems/insights/connectors.go:1285-1300`（判定分支）
- Modify: `internal/systems/insights/group_compare_test.go`

**Interfaces:**
- Consumes: Task 1 的 `ResolvedThresholds`。
- Produces: `groupCompareInput` 增加字段 `Thresholds ResolvedThresholds`。

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/group_compare_test.go` 追加：

```go
// 判定必须跟着配置走。同一批数据，把充分门槛调低，结论就该从「样本不足」
// 变成能出结论——这正是让阈值可写的全部意义。
//
// 这条测试也是「判定只有一处实现」的守卫：如果哪天有人在别处又读了一次常量，
// 那一处不会跟着配置变，而这里会先发现。
func TestVerdictFollowsTheConfiguredThreshold(t *testing.T) {
	t.Parallel()

	input := groupCompareInput{
		InGroup:      MetricCounts{Impressions: 3000, Clicks: 90},
		Rest:         MetricCounts{Impressions: 3000, Clicks: 60},
		SubjectLabel: "开场类型",
		Comparable:   true,
	}

	strict := input
	strict.Thresholds = defaultThresholds()
	strict.Thresholds.SufficientImpressions = 10000
	if got := compareGroups(strict).Confidence; got == ConfidenceSufficient {
		t.Error("3000 次曝光在 10000 的门槛下不该判成充分")
	}

	loose := input
	loose.Thresholds = defaultThresholds()
	loose.Thresholds.SufficientImpressions = 2000
	if got := compareGroups(loose).Confidence; got != ConfidenceSufficient {
		t.Errorf("门槛降到 2000 之后应该判成充分，得到 %q", got)
	}
}

// 没传阈值时走出厂设定。判定是全模块的公共函数，调用点很多，
// 漏传一处就拿 0 去比的话，任何样本量都会被判成充分——那是最坏的一种错。
func TestZeroThresholdsFallBackToDefaults(t *testing.T) {
	t.Parallel()

	input := groupCompareInput{
		InGroup:      MetricCounts{Impressions: 10, Clicks: 5},
		Rest:         MetricCounts{Impressions: 10, Clicks: 1},
		SubjectLabel: "开场类型",
		Comparable:   true,
	}
	if got := compareGroups(input).Confidence; got == ConfidenceSufficient {
		t.Error("没传阈值时应该退回默认值，10 次曝光绝不该判成充分")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestVerdictFollows|TestZeroThresholds' -v
```

Expected: `groupCompareInput` 没有 `Thresholds` 字段。

- [ ] **Step 3: 改判定**

`group_compare.go`：`groupCompareInput` 加

```go
	// Thresholds 是这次判定用的那一套。零值表示调用方没传，
	// compareGroups 会退回出厂设定——判定是公共函数，调用点很多，
	// 漏传一处就拿 0 去比的话，任何样本量都会被判成充分，那是最坏的一种错。
	Thresholds ResolvedThresholds
```

`compareGroups` 开头加：

```go
	thresholds := input.Thresholds
	if thresholds.SufficientImpressions <= 0 {
		thresholds = defaultThresholds()
	}
```

然后把函数体里所有 `sufficientSampleImpressions` / `directionalSampleImpressions` 换成 `thresholds.SufficientImpressions` / `thresholds.DirectionalImpressions`。

`connectors.go:1285-1300` 那段判定同理——它和 `compareGroups` 判的是同一件事，**若两处逻辑重复，本任务顺手把它改成调用 `compareGroups`**；若它判的是别的东西（比如导入口径），就给它也加一个阈值入参，不要留下第二处读常量的地方。

> 改完必须为真：`grep -n "sufficientSampleImpressions\|directionalSampleImpressions" internal/systems/insights/*.go | grep -v _test | grep -v thresholds.go` 只剩常量定义那两行。同理检查 `minTrendDays` / `minAnomalyDays` / `minDriverAssets` / `maxComparisonAssets` / `preLaunchQualityWindowDays`。

- [ ] **Step 4: 调用处传进去**

`performance.go` 里每个构造 `groupCompareInput` 的地方，填上从服务层传下来的 `ResolvedThresholds`。服务层在 `GetPerformanceAnalysis` 一类的入口处**调一次** `s.currentThresholds(ctx, actor.OrganizationID)`，然后一路传下去。

**一次请求只读一次阈值。** 在每个判定点各读一次的话，一次请求里如果有人正好保存了新阈值，同一份分析结果里会有两套标准判出来的结论，而页面上只会盖一个版本号。

- [ ] **Step 5: 跑全量**

```bash
go test ./internal/systems/insights/... -v 2>&1 | tail -30
```

Expected: 新增两条 PASS，既有测试**一条都不能变红**——阈值默认值等于原常量，行为应当完全不变。若有既有测试变红，说明某处默认值取错了，先修那里，不要改测试的期望值。

- [ ] **Step 6: 提交**

```bash
git add internal/systems/insights/
git commit -m "refactor(insights): 判定改为读阈值配置而非代码常量"
```

---

### Task 3: 每条结论盖上它用的那一版阈值

> **往 `Judgement` 加字段，修改现有数据结构。开始前必须向使用者确认。**

**Files:**
- Modify: `internal/systems/insights/verdict.go`（P0 建的）
- Modify: `internal/systems/insights/metric_overview_contract_test.go`
- Create: `src/components/insight/shared/ThresholdStamp.tsx`
- Modify: `src/components/insight/shared/VerdictBadge.tsx`（或 P0 里承载判定展示的那个组件）

**Interfaces:**
- Consumes: Task 1 的 `ResolvedThresholds`；P0 的 `Judgement`。
- Produces: `Judgement.ThresholdVersion int64`（JSON `threshold_version`）；`export function ThresholdStamp({ version }: { version: number })`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/thresholds_test.go` 追加：

```go
// 每条结论要盖上它用的那一版阈值。
//
// 没有这个号码，改完阈值之后所有历史结论都说不清是按什么标准算出来的。
// 经验库是「新增版本不覆盖、历史可审计」的——一批说不清依据的经验会永远
// 挂在账上，说不清是对是错。
func TestJudgementCarriesTheThresholdVersion(t *testing.T) {
	t.Parallel()

	thresholds := defaultThresholds()
	thresholds.Version = 7

	input := groupCompareInput{
		InGroup:      MetricCounts{Impressions: 20000, Clicks: 600},
		Rest:         MetricCounts{Impressions: 20000, Clicks: 400},
		SubjectLabel: "开场类型",
		Comparable:   true,
		Thresholds:   thresholds,
	}
	if got := compareGroups(input).ThresholdVersion; got != 7 {
		t.Errorf("判定应该记下第 7 版阈值，得到 %d", got)
	}
}

// 出厂设定是第 0 版，也要如实盖上——空着的话，页面分不清
// 「跑的是出厂设定」和「这条结论早于阈值功能」这两种情况。
func TestDefaultThresholdsStampVersionZero(t *testing.T) {
	t.Parallel()

	input := groupCompareInput{
		InGroup: MetricCounts{Impressions: 20000, Clicks: 600},
		Rest:    MetricCounts{Impressions: 20000, Clicks: 400},
		SubjectLabel: "开场类型", Comparable: true,
	}
	if got := compareGroups(input).ThresholdVersion; got != 0 {
		t.Errorf("出厂设定应该盖第 0 版，得到 %d", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestJudgementCarries|TestDefaultThresholdsStamp' -v
```

Expected: `ThresholdVersion` 未定义。

- [ ] **Step 3: 加字段**

`verdict.go` 的 `Judgement` 加：

```go
	// ThresholdVersion 是判出这条结论时生效的阈值版本。0 = 出厂设定。
	//
	// 它必须跟着结论走到最远的地方——存进复盘、沉淀成经验之后仍然读得出来。
	// 改完阈值之后回头看一条老结论，这个号码是唯一能说清「它是按什么标准算的」
	// 的东西。
	ThresholdVersion int64 `json:"threshold_version"`
```

`compareGroups` 返回时填上 `thresholds.Version`。`GroupComparison` 若不是内嵌 `Judgement` 而是平铺字段，按 P0 定的形状对齐——**不要新造第二个装判定的结构**。

- [ ] **Step 4: 更新契约测试**

P0 的 `metric_overview_contract_test.go` 走 JSON 检查判定字段。加上 `threshold_version` 的期望，并在注释里写清它为什么必须出现在每一处判定上。若那个测试是「枚举允许的键」的形式，漏加会直接失败——这是好事，说明契约在起作用。

- [ ] **Step 5: 前端标注**

创建 `src/components/insight/shared/ThresholdStamp.tsx`：

```tsx
/**
 * 「本次判定使用的阈值」。
 *
 * 阈值可写之后，这个标注就是必需的：不标的话，一条三个月前的结论和一条今天的
 * 结论看起来完全一样，但可能是按两套标准算出来的。
 */
export function ThresholdStamp({ version }: { version: number }) {
  return <span className="threshold-stamp" title="判定标准可以在「设置 · 判定阈值」里调整">
    {version === 0 ? '按出厂阈值判定' : `按第 ${version} 版阈值判定`}
  </span>
}
```

挂在结论展示组件上：分析页每屏顶部一处，复盘报告每条发现一处（因为一份报告里的发现可能来自不同时间的分析），经验卡的「凭什么」展开层里一处。

**不要每一行都挂。**满屏都是「按第 3 版阈值判定」会把真正要读的结论淹掉；只在人可能问「这是按什么标准算的」的地方出现。

- [ ] **Step 6: 跑全量并提交**

```bash
go test ./internal/systems/insights/... && npm run build
```

```bash
git add internal/systems/insights/ src/components/insight/shared/
git commit -m "feat(insights): 每条结论记下它用的阈值版本"
```

---

### Task 4: 设置入口的四组

**Files:**
- Create: `src/components/insight/settings/SettingsPage.tsx`
- Create: `src/components/insight/settings/ThresholdView.tsx`
- Create: `src/components/insight/settings/HealthView.tsx`
- Create: `src/components/insight/settings/DictionaryView.tsx`
- Create: `src/components/insight/settings/PermissionView.tsx`
- Create: `src/components/insight/settings/index.ts`
- Modify: `internal/systems/insights/settings.go`
- Modify: `src/data/api.ts`
- Reference: `src/components/InsightSettingsPage.tsx`、`src/components/DataQualityPage.tsx`、`src/components/CapabilityOperationsPage.tsx`

**Interfaces:**
- Consumes: Task 1 的 `GetThresholds` / `SaveThresholds`。
- Produces:
  - `export function SettingsPage({ view }: { view: SettingsView })`
  - `export type SettingsView = 'thresholds' | 'health' | 'dictionary' | 'permission'`

- [ ] **Step 1: 后端把只读那一页改成两半**

`settings.go` 现在整页 `Editable: false`。改成按组给：判定阈值那一组 `Editable: true`，其余三组保持 `false`。

`SettingGroup` 加 `Editable bool`；`InsightSettings.Editable` 保留但改成「是否有任何一组可写」，并把 `EditableNote` 的内容改掉——原来那句「03 §17.3 把最低样本由全局规则还是行业模板配置列为待确认，在那条定下来之前做成可配置等于先替它选了答案」现在**不再成立**，因为已经选了：全局规则（按组织，不按行业模板）。新的说明要写清这个决定：

```go
	EditableNote: "判定阈值现在可以改，改动按组织生效、只增版本，每条结论都记下它用的是哪一版。" +
		"行业模板级的阈值（同一组织下不同行业用不同门槛）没有做——" +
		"需要先有一份行业分类口径，那是变量字典的事。",
```

`sampleThresholdSettings()` 与 `windowSettings()` 的 `Value` 改成从 `ResolvedThresholds` 取，不再直接引用常量；`Recommended` 保持字面量不变（它是「出厂推荐」，不该跟着人调的值走），`Deviates` 于是真正开始有意义。

- [ ] **Step 2: 写 ThresholdView**

创建 `src/components/insight/settings/ThresholdView.tsx`。要点：

- 单列分组表单，不用仪表盘布局。
- 每格三样东西缺一不可：**当前值、调它会发生什么、出厂推荐**。「会影响置信度」不算说明，要写清哪一句话会从「有结论」变成「没结论」——这些文案后端 `SettingItem.Effect` 里已经有了，直接渲染，不要在前端另写一套。
- 被调过的格子（`deviates`）标出来，旁边显示出厂推荐值和一个「改回推荐值」。
- **理由是必填的**，保存按钮在理由为空时禁用：

```tsx
      <label>
        为什么改
        <textarea value={reason} onChange={event => setReason(event.target.value)}
          placeholder="例：本项目单条素材曝光量普遍在 3000 上下，按 10000 判的话整轮都出不了结论。"/>
      </label>
      <small className="form-hint">
        理由会跟着这一版阈值一起存下来。三个月后有人问「为什么门槛是这个数」，
        这一栏就是答案。
      </small>
```

- 保存成功后显示新版本号和一句提醒：

```tsx
    setNotice(`已存为第 ${saved.version} 版。从现在起判出来的结论都按这一版算，之前的结论保持原样。`)
```

最后半句很重要：人改完阈值第一个担心的就是「历史结论会不会全变」。

- [ ] **Step 3: 写另外三组**

- `HealthView.tsx`：从 `DataQualityPage.tsx` 搬（新鲜度、缺失、异常、口径、对账、修复队列）。六个原视图降级成小标题分段。它进「设置」的理由要写在页面顶部一句话里：

```tsx
    <p className="settings-intro">
      数据体检是「配置对不对」的报告。上面判定阈值定的是标准，这里看的是
      按这个标准量出来的数据够不够干净——两件事看的是同一个东西的两头。
    </p>
```

- `DictionaryView.tsx`：从 `CapabilityOperationsPage.tsx` 搬（特征体系、指标字典、分析 Skills、评测集、质量看板）。**每个变量要显示它的来源类别**（量出来的 / 人标的 / 模型猜的）——变量字典是这个分类的权威登记处，P0 定的准入规则最终落在这里。
- `PermissionView.tsx`：从 `InsightSettingsPage.tsx` 的确认权限那一组搬，保持只读。

- [ ] **Step 4: 写壳并接 api**

`SettingsPage.tsx` 按 `view` 分发；`index.ts` 出口。`src/data/api.ts` 加：

```ts
  getThresholds: (projectId: string) =>
    request<ApiThresholds>(`${insightProjectPath(projectId)}/thresholds`),

  saveThresholds: (projectId: string, body: {
    values: Record<string, number | null>
    reason: string
  }) => request<ApiThresholds>(`${insightProjectPath(projectId)}/thresholds`, 'PUT', body),
```

后端两条路由 `GET` / `PUT /api/insights/v1/projects/{project_id}/thresholds`，并补 OpenAPI。

> 用 `PUT` 而不是 `POST`：从调用方看这是「把阈值设成这样」，幂等语义对得上。服务端内部是追加一版，这是实现细节。

- [ ] **Step 5: 在浏览器里走一遍**

用 `preview_start` 起服务，确认：

1. 「设置 · 判定阈值」每格都有当前值、影响说明、出厂推荐。
2. 理由留空时保存按钮是禁用的。
3. 把充分门槛从 10000 改成 2000、填理由、保存，提示里出现新版本号和「之前的结论保持原样」。
4. 回到「分析 · 素材对比」，原来判成「样本不足」的那一行现在能出结论了，页面上出现「按第 1 版阈值判定」。**这一条是整期的验收判据**——它同时证明了阈值可写、判定跟着走、版本号盖上了。
5. 把充分门槛设成比方向门槛低，保存被拒且给出了看得懂的提示。
6. 「设置 · 数据体检」「变量字典」「确认权限」三组内容都在，原三页的功能没丢。
7. `preview_console_logs` 无报错。

- [ ] **Step 6: 提交**

```bash
git add src/components/insight/settings/ src/data/api.ts internal/systems/insights/settings.go api/openapi/insights-v1.yaml
git commit -m "feat(insights-web): 设置入口四组，判定阈值改为可写"
```

---

### Task 5: 导航从「数据质量 + 能力运营 + 系统设置」收敛成「设置」

> **这一步修改现有导航结构。开始前必须向使用者确认。**

**Files:**
- Modify: `src/data/navigation.ts:62`（`quality`）、`:65`（`operations`）、`:66`（`settings`）
- Modify: `src/components/Pages.tsx:1501-1502` 附近

- [ ] **Step 1: 改导航条目**

删掉 `quality`（:62）、`operations`（:65）、`settings`（:66）三条，以及 :63-64 那两行只服务于 `operations` 的注释，换成一条：

```ts
      // 「设置」= 原数据质量 + 原能力运营 + 原系统设置。
      //
      // 合并的理由：这三页是一件事的三面。判定阈值定标准，数据体检看按这个标准
      // 量出来的数据干不干净，变量字典管那些数据里的变量叫什么、算不算数。
      // 分成三个入口，人调完阈值不会想到去看体检，看完体检也不知道该回哪里改。
      {
        id: 'settings', label: '设置', icon: Settings2, group: '治理', layout: 'settings',
        description: '判定标准、数据体检、变量字典、确认权限。',
        views: ['判定阈值', '数据体检', '变量字典', '确认权限'],
      },
```

`ShieldCheck`、`SlidersHorizontal` 图标若不再被引用，从 import 里删掉。

- [ ] **Step 2: 改渲染分发**

```bash
grep -rn "'quality'\|'operations'" src/components/Pages.tsx src/App.tsx
```

```ts
const settingsViews: Record<string, SettingsView> = {
  判定阈值: 'thresholds',
  数据体检: 'health',
  变量字典: 'dictionary',
  确认权限: 'permission',
}
```

- [ ] **Step 3: 跑全量并核对整个侧栏**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

用 `preview_snapshot` 核对：洞察下面**只剩五个入口**——分析、复盘、经验、素材、设置。原来的十一个一个不剩，且每一个的功能都能在这五个里找到。这是整套重构的最终验收点。

- [ ] **Step 4: 提交**

```bash
git add src/data/navigation.ts src/components/
git commit -m "feat(insights-web): 数据质量、能力运营与系统设置收敛成「设置」入口"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档「模块五 · 设置」与「3.1 判定只有一处实现」：

| 规格要求 | 落在 |
|---|---|
| 数据质量 + 能力运营 + 系统设置三合一 | Task 4 + Task 5 |
| 四组：判定阈值 / 数据体检 / 变量字典 / 确认权限 | Task 4 的四个视图 |
| 19 个阈值可读可写 | Task 1 的 `Thresholds` + Task 4 的表单 |
| 阈值落库带版本 | Task 1（只增版本，接口上没有更新方法） |
| **每条结论记录它用的阈值版本** | Task 3 的 `Judgement.ThresholdVersion` + `ThresholdStamp` |
| 页面标出「本次判定使用的阈值」 | Task 3 Step 5 |
| 判定全模块只有一处实现 | Task 2 Step 3 的 grep 判据：常量只剩定义那一行 |
| 改阈值后判定跟着变 | Task 2 的 `TestVerdictFollowsTheConfiguredThreshold` + Task 4 Step 5 第 4 条 |

**未覆盖且是有意的：**

- **19 个阈值里只落了 7 个。** 落的是决定三档结论的那些（两个样本门槛、两个天数下限、驱动因素与对比的条数上下限、质量窗口）。其余十几个是导入上限、实验天数上限一类的**保护性上限**（`maxImportRows`、`maxWindowDays`、`maxExperimentDays`、`maxPerCategory`），它们防的是系统被撑爆，不是判定标准——把它们做成可配置，等于把「防呆」做成了「可关闭」。设置页仍然把它们列出来并标明只读，人看得见它们是多少。**这一条与设计文档「19 个阈值可读可写」有出入，需要向使用者说明。**
- **行业模板级阈值**（同一组织下不同行业不同门槛）—— 需要先有一份行业分类口径，那是变量字典的事。Task 4 Step 1 的 `EditableNote` 里明写了。
- **回滚到旧版本的按钮** —— 照旧值再存一版即可，理由栏写清为什么回。这样回滚本身也进历史；给一个「回滚」按钮反而会绕过理由必填。
- **阈值变更的通知** —— 改判定标准影响所有人，理应通知。通知那一组现在整体是 `not_built` 状态，不在本期单独补。
- **原「系统设置」五组里的「通知」和「报告模板」两组** —— 四组的新结构里没有它们的位置。两组当前都是 `not_built`（一张表都没建、一个开关都不生效），做成两个空抽屉只会让人以为设得上。它们的 `SettingGroup` 定义在 `settings.go` 里原样保留、状态照旧是「没做」，但**不在设置页上单开一组**；等真做了再进来。「观察窗口」那一组不是删，是并进了判定阈值——窗口天数本来就是判定标准的一部分。

**2. 占位扫描** —— 无 TBD / TODO / 「同 Task N」。Task 1 Step 6 的仓储、Task 4 Step 1/3 的搬运给的是「改哪几处、每处改成什么、为什么」：前者必须和迁移的列名逐字对齐，后者是从三个现存页面搬既有逻辑，抄一段可能对不上的代码比说清约束更容易出错。Task 2 Step 3 没有给完整的 `compareGroups` 函数体，而是给了替换规则和一条 grep 判据——这个函数有五个判定分支，逐字重抄一遍出错的概率比按规则替换高。

**3. 类型一致性** —— `Thresholds` 七个字段的名字在 Go 结构体、迁移列名（蛇形）、`ResolvedThresholds`、`resolve()` 四处一一对应。`ThresholdVersion` 在 `Judgement`、契约测试、`ThresholdStamp` 的 `version` prop 三处一致。`SettingsView` 四个取值与 `settingsViews` 映射表四个键一致。`ResolvedThresholds` 全部是 `int`（非指针），`Thresholds` 全部是 `*int`——两者刻意不同名不同形，避免有人把「没人调过」的 nil 传进判定；Task 1 的注释里写明了这个区分的理由。

---

## 依赖关系

前置：P0 全部（`Judgement`、契约测试）。Task 3 会动 `Judgement`，所以 **P0 必须先完成**。
与其他期的关系：Task 3 的 `ThresholdStamp` 要挂到 P1 的分析页、P2 的复盘页、P4 的经验卡上——**这一期应当排在最后**，否则那三处还不存在。
后继：第 7 期视频探测产出 `derived` 变量后，变量字典那一组会多一批条目；阈值这一套不用动。

---

## 五份计划的总览

| 计划 | 入口 | 依赖 | 一句话 |
|---|---|---|---|
| [P0 地基](2026-08-11-insight-p0-foundation.md) | — | 无 | 三档结论、名词表、共享组件 |
| [P1 分析](2026-08-11-insight-p1-analysis.md) | 分析 | P0 | 六个视图合一屏，可「记一笔」 |
| [P2 复盘](2026-08-11-insight-p2-review.md) | 复盘 | P1 | 收记的笔 + 系统补的发现，提交成待定经验 |
| [P3 素材](2026-08-11-insight-p3-assets.md) | 素材 | P0（Task 6 需 P1） | 备料三样齐，找相似，外部素材证据区 |
| [P4 经验](2026-08-11-insight-p4-experience.md) | 经验 | P0、P2 | 查 / 管两个模式，三态加复审标记 |
| [P5 设置](2026-08-11-insight-p5-settings.md) | 设置 | P0，且排在最后 | 阈值可写带版本，三合一 |

建议顺序：**P0 → P1 → P2 → P3 → P4 → P5**。P3 和 P4 之间没有耦合，人手够的话可以并行，但 P3 的 Task 6 要改 P1 建的文件。
