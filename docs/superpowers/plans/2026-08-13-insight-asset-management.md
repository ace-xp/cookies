# 素材台账基建 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把洞察模块的素材从「分析前置检查清单」扩成「台账 + 分析对象」两类，让平台里所有素材都被登记在册，而只有真正能投流的成品进分析队列。

**Architecture:** 在 `insight_assets` 上加一个正交维度 `role`（`ledger` 台账 / `analysis` 分析对象），身份与分析进度互不干涉。台账的收录通路走 `internal/integrations/` 下的适配器反向注入到 `internal/platform/assets`（仓里没有任何可用的事件消费方，不能走 `asset.ready.v1`）。台账体量比分析对象大一个量级，所以查询能力从「一次取完」换成游标分页 + 标题搜索。米云从被塞进 `external` 正名为独立的 `miyun` 来源。

**Tech Stack:** Go 1.22+（`internal/systems/insights`、`internal/platform/assets`、`internal/integrations`）、MySQL 8、Vite + React 19 + TypeScript 5.9（`src/`）、node:test（`test/`）

## Global Constraints

- 所有新增注释、错误文案、界面文案一律中文。
- 分层：`internal/platform/*` **不得** import `internal/systems/*`。跨模块一律靠 `internal/integrations/` 下的适配器包反向注入，装配在 `cmd/cookies-api/main.go`。
- 迁移文件成对出现：`migrations/insights/<14位时间戳>_<名字>.up.sql` 与 `.down.sql`。
- 前端 canonical 目录是 `src/`，不是 `web/`。`web/` 一个字都不改。
- 后端全量验证：`go build ./...` 与 `go test ./internal/systems/insights/... ./internal/platform/assets/...`。
- 前端全量验证：`npx tsc --noEmit -p tsconfig.json` 与 `npm test`（基线 300 passed / 0 failed，只许涨不许跌）。
- `insights.AssetType` 是**广告形态**六选一（`xiaohongshu_note` / `wechat_article` / `brand_ad` / `digital_human_ad` / `preroll_ad` / `hit_replica_ad`），**不是** image/video。台账收录时一律留空（`AssetTypeUnknown`），绝不从 `contract.AssetKind` 映射过去。
- 权限三档：`insights.read`（`ScopeRead`）/ `insights.write`（`ScopeWrite`）/ `insights.confirm`（`ScopeConfirm`）。

## 相对 spec 的两处偏离（已决，无需再问）

spec 是 `docs/superpowers/specs/2026-08-13-insight-asset-management-design.md`。以下两处按本计划执行，不按 spec 字面：

1. **标题来源**：spec 写「渲染任务反查创意任务名」。反查要让 `internal/platform/assets` 认识 creative 的任务表，是一条新的跨模块依赖，收益只是一个标题。本期改为：上传有文件名就用文件名；渲染产物用 `渲染成片 · <YYYY-MM-DD>`；模型产物用 `模型产物 · <YYYY-MM-DD>`；都没有用 `未命名素材 · <YYYY-MM-DD>`。
2. **退回台账的前置条件**：spec 写「不得被任何已提交的复盘引用过」。复盘的 `ReportFinding` 结构里没有 `asset_id`，这条在数据层查不出来。改为可执行的等价判据：**该素材不存在 `status='matched'` 的映射**——对上号就意味着它有广告对象、有花费、进过分析，这时候退回台账就是在藏数据。

## 需要先请示的动作

以下动作落在使用者的「必须暂停并请求确认」清单里。**动手前逐条得到确认，中途不要自行放行。**

| # | 动作 | 出现在 | 为什么算 |
|---|---|---|---|
| 1 | 改 `internal/platform/assets/upload_service.go` 等四个 `Complete*` 的成功路径，插入台账登记 | Task 8 | 动的是素材库的核心入库逻辑，且是另一个子系统 |
| 2 | 两条数据库迁移：`insight_assets` 加 `role` 列 + 生成列 + 唯一键；放开 `source_kind` 的 CHECK | Task 1、Task 11 | 数据库结构变更，且唯一键会约束线上存量数据 |
| 3 | `GET .../assets` 的响应体加 `next_cursor`、查询参数加 `role`/`cursor`/`q` | Task 6 | 改的是已发布的 API 契约 |
| 4 | 新增两个动词端点 `:promote` / `:return-to-ledger` | Task 6 | 同上 |
| 5 | `cookies-maintain` 加 `backfill-ledger` 子命令并对线上库跑一次回填 | Task 10 | 批量写生产数据 |

不在此列（可直接做）：新建文件（`ledger.go`、`insightsledger/`、`LedgerView.tsx`）、新增测试、新增前端类型、`main.go` 的装配行。

## 文件结构

| 文件 | 责任 |
|---|---|
| `migrations/insights/20260813110000_insight_asset_role.up.sql` / `.down.sql` | 加 `role` 列、生成列 `ledger_object_key`、唯一键与索引 |
| `migrations/insights/20260813120000_insight_asset_source_miyun.up.sql` / `.down.sql` | `source_kind` 放开 `miyun`，回填存量 |
| `internal/systems/insights/assets.go` | `AssetRole` 类型、`Asset.Role`、`AssetFilter` 扩字段、`AssetPage`、`RecordLedgerAssetRequest`、四个新服务方法 |
| `internal/systems/insights/mysql_asset_repository.go` | `role` 的读写、`ListAssetPage` 的游标分页与标题搜索 |
| `internal/systems/insights/httpapi/assets.go` | `role` / `cursor` / `q` 查询参数、`:promote` 与 `:return-to-ledger` 两个动作 |
| `internal/systems/insights/ledger.go`（新建） | `RecordLedgerAsset`：不走 HTTP、不走人的权限门的收录入口 |
| `internal/platform/assets/ledger.go`（新建） | `LedgerRecorder` 接口 + `LedgerRelay` 指针中转器 + 标题推导 |
| `internal/platform/assets/upload_service.go` | 4 个落库成功点后挂 `recordLedger` |
| `internal/platform/assets/generated_intake_service.go` | 模型产物落库成功后挂 `recordLedger` |
| `internal/platform/assets/external_import.go` | 外部导入落库成功后挂 `recordLedger` |
| `internal/integrations/insightsledger/recorder.go`（新建） | 把 `assets.LedgerEntry` 翻成 `insights.RecordLedgerAssetRequest` |
| `cmd/cookies-api/main.go` | 装 `LedgerRelay`，`insightsService` 造好后回填 |
| `cmd/cookies-maintain/main.go` | 新子命令 `backfill-ledger` |
| `api/openapi/insights-v1.yaml` | `role` / `cursor` / `q` / `next_cursor` / `miyun` 的契约 |
| `src/data/api.ts` | `ApiAssetRole`、`ApiInsightAssetFilter` 扩字段、`listInsightAssets` 返回 `next_cursor` |
| `src/components/insight/assets/OverviewView.tsx` | 四队列只数 `role='analysis'` |
| `src/components/insight/assets/LedgerView.tsx`（新建） | 台账清单：搜索框 + 「加载更多」 + 「拉进分析」 |
| `src/components/insight/assets/AssetDetail.tsx` | 血缘列表接上 `listInsightAssetLineage` |

---

### Task 1: 数据库加 role 维度

**Files:**
- Create: `migrations/insights/20260813110000_insight_asset_role.up.sql`
- Create: `migrations/insights/20260813110000_insight_asset_role.down.sql`
- Modify: `internal/systems/insights/assets.go`（在 `AssetSourceKind` 定义之后插入，约 :76 之后）
- Modify: `internal/systems/insights/mysql_asset_repository.go`（`CreateAsset` :17-35、`insightAssetSelect` :388、`scanAsset`）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Produces: `insights.AssetRole`（string 类型别名）、常量 `insights.AssetRoleLedger = "ledger"` 与 `insights.AssetRoleAnalysis = "analysis"`、方法 `func (r AssetRole) valid() bool`、`func (r AssetRole) Label() string`、字段 `insights.Asset.Role AssetRole`（JSON `role`）。

- [ ] **Step 1: 写迁移的 up**

创建 `migrations/insights/20260813110000_insight_asset_role.up.sql`：

```sql
-- 素材有两种身份：台账（平台里所有素材的账本）和分析对象（真正投流、要跑归因的成品）。
-- 身份和分析进度是两个正交维度，所以不加第九个 analysis_status，而是加一列 role。
-- 存量全部是分析对象，回填 'analysis'，现状行为一个字节都不变。
ALTER TABLE insight_assets
  ADD COLUMN role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'analysis' AFTER project_id;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_role CHECK (role IN ('ledger', 'analysis'));

-- 台账靠后台自动收录，同一个平台素材版本可能被收两次（重试、回填）。
-- MySQL 的唯一键不约束含 NULL 的行，所以用生成列把「台账 + 有平台引用」这一种情况
-- 挑出来做唯一键：手工登记的分析素材（role='analysis'）和没有平台引用的行都算 NULL，
-- 不受约束——它们本来就允许一个平台素材登记多条。
ALTER TABLE insight_assets
  ADD COLUMN ledger_object_key VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS (
      CASE WHEN role = 'ledger' AND platform_asset_id IS NOT NULL
           THEN CONCAT(platform_asset_id, ':', platform_asset_version)
           ELSE NULL END
    ) STORED;

ALTER TABLE insight_assets
  ADD UNIQUE KEY uq_insight_assets_ledger_object (organization_id, ledger_object_key);

-- 台账清单按 (role, updated_at, id) 游标翻页，四个分析队列按 role 过滤后再看状态。
ALTER TABLE insight_assets
  ADD KEY idx_insight_assets_role (organization_id, project_id, role, updated_at, id);
```

- [ ] **Step 2: 写迁移的 down**

创建 `migrations/insights/20260813110000_insight_asset_role.down.sql`：

```sql
ALTER TABLE insight_assets DROP KEY idx_insight_assets_role;
ALTER TABLE insight_assets DROP KEY uq_insight_assets_ledger_object;
ALTER TABLE insight_assets DROP COLUMN ledger_object_key;
ALTER TABLE insight_assets DROP CONSTRAINT chk_insight_assets_role;
ALTER TABLE insight_assets DROP COLUMN role;
```

- [ ] **Step 3: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestAssetRoleValidAndLabel(t *testing.T) {
	if !AssetRoleLedger.valid() || !AssetRoleAnalysis.valid() {
		t.Fatal("台账与分析对象都应是合法身份")
	}
	if AssetRole("archive").valid() {
		t.Fatal("身份只有两种，第三种必须被拒")
	}
	if AssetRoleLedger.Label() != "台账" || AssetRoleAnalysis.Label() != "分析对象" {
		t.Fatalf("身份的中文名不对：%q / %q", AssetRoleLedger.Label(), AssetRoleAnalysis.Label())
	}
}
```

- [ ] **Step 4: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestAssetRoleValidAndLabel
```

预期：编译失败，`undefined: AssetRoleLedger`。

- [ ] **Step 5: 加类型**

在 `internal/systems/insights/assets.go` 的 `func (k AssetSourceKind) valid() bool` 之后插入：

```go
// AssetRole 是素材的**身份**，和 AnalysisStatus 说的**进度**是两回事。
//
// 台账（ledger）是平台里所有素材的账本：创意做的每一张图、每一版剪辑、每一段配音
// 都在里面，绝大多数永远不会拿去投流。分析对象（analysis）是真正投出去、有花费、
// 要跑归因的那些成品。
//
// 不做成第九个 analysis_status 的理由：一条素材从台账被拉进分析时，它走到过哪一步
// 应该原样保留；退回台账再拉回来也不该清零。两个正交维度各管各的，队列一律按
// role 过滤，而不是靠把状态归零来实现。
type AssetRole string

const (
	AssetRoleLedger   AssetRole = "ledger"   // 台账：登记在册，不进分析队列
	AssetRoleAnalysis AssetRole = "analysis" // 分析对象：投过流、要跑归因
)

func (r AssetRole) valid() bool {
	switch r {
	case AssetRoleLedger, AssetRoleAnalysis:
		return true
	}
	return false
}

func (r AssetRole) Label() string {
	switch r {
	case AssetRoleLedger:
		return "台账"
	case AssetRoleAnalysis:
		return "分析对象"
	}
	return string(r)
}
```

- [ ] **Step 6: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestAssetRoleValidAndLabel
```

预期：`ok`。

- [ ] **Step 7: 加 Asset 字段**

在 `internal/systems/insights/assets.go` 的 `Asset` 结构体里，`ProjectID` 之后插入一行：

```go
	Role AssetRole `json:"role"`
```

- [ ] **Step 8: 仓储读写 role**

在 `internal/systems/insights/mysql_asset_repository.go` 中，把 `insightAssetSelect` 改成（新增 `role`，紧跟 `project_id`）：

```go
const insightAssetSelect = `SELECT id, organization_id, project_id, role, lineage_id, revision, title, source_kind, source_ref, source_job_id, platform_asset_id, platform_asset_version, asset_type, asset_type_source, asset_type_confidence, analysis_status, analysis_status_reason, analysis_status_changed_at, version, created_by, created_at, updated_at FROM insight_assets`
```

把 `scanAsset` 的 `row.Scan` 首行改成：

```go
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.Role, &value.LineageID, &value.Revision,
```

把 `CreateAsset` 的 INSERT 语句列表加上 `role`，值列表加上 `value.Role`。改完后的 `CreateAsset` 全文：

```go
func (r MySQLRepository) CreateAsset(ctx context.Context, value Asset) (Asset, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO insight_assets
		(id, organization_id, project_id, role, lineage_id, revision, title, source_kind, source_ref, source_job_id,
		 platform_asset_id, platform_asset_version, asset_type, asset_type_source, asset_type_confidence,
		 analysis_status, analysis_status_reason, analysis_status_changed_at, version, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.Role, value.LineageID, value.Revision,
		value.Title, value.SourceKind, value.SourceRef, nullableString(value.SourceJobID),
		nullableString(value.PlatformAssetID), nullableInt64(value.PlatformAssetVersion),
		value.AssetType, value.AssetTypeSource, value.AssetTypeConfidence,
		value.AnalysisStatus, value.AnalysisStatusReason, value.AnalysisStatusChangedAt,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return Asset{}, err
	}
	return value, nil
}
```

> 注意：`nullableString` / `nullableInt64` 是该文件里已有的辅助函数。若现有 `CreateAsset` 用的是别的写法（直接传 `value.SourceJobID`），照现有写法只加 `role` 这一列一个值，不要顺手改其他列的空值处理。

- [ ] **Step 9: 编译并跑素材相关测试**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/systems/insights/ -run 'Asset'
```

预期：`ok`。

- [ ] **Step 10: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add migrations/insights/20260813110000_insight_asset_role.up.sql migrations/insights/20260813110000_insight_asset_role.down.sql internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/mysql_asset_repository.go && git commit -m "feat(insights): 素材加 role 维度，台账与分析对象分开"
```

---

### Task 2: 登记与列表认 role

**Files:**
- Modify: `internal/systems/insights/assets.go`（`IndexAssetRequest`、`IndexAsset`、`AssetFilter`、`ListAssets`）
- Modify: `internal/systems/insights/mysql_asset_repository.go`（`ListAssets`）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Consumes: Task 1 的 `AssetRole`、`AssetRoleLedger`、`AssetRoleAnalysis`、`Asset.Role`。
- Produces: `IndexAssetRequest.Role AssetRole`（JSON `role`，留空默认 `analysis`）、`AssetFilter.Roles []AssetRole`（JSON `roles`，留空默认只查 `analysis`）。

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestIndexAssetDefaultsToAnalysisRole(t *testing.T) {
	service, actor := testService(), testActor()
	asset, err := service.IndexAsset(context.Background(), actor, "k_project_1", IndexAssetRequest{
		Title: "投放成片 A", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	if asset.Role != AssetRoleAnalysis {
		t.Fatalf("不填 role 时应默认是分析对象，得到 %q", asset.Role)
	}
}

func TestIndexAssetRejectsUnknownRole(t *testing.T) {
	service, actor := testService(), testActor()
	_, err := service.IndexAsset(context.Background(), actor, "k_project_1", IndexAssetRequest{
		Title: "投放成片 B", SourceKind: AssetSourceUpload, Role: AssetRole("archive"),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("未知身份应被拒，得到 %v", err)
	}
}

func TestListAssetsHidesLedgerByDefault(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	if _, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "分析对象", SourceKind: AssetSourceUpload,
	}); err != nil {
		t.Fatalf("登记分析对象失败：%v", err)
	}
	if _, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "台账素材", SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
	}); err != nil {
		t.Fatalf("登记台账素材失败：%v", err)
	}

	// 不给 roles 就只看分析对象：四个队列和红点靠这条默认值，绝不能把几千条台账数进去。
	values, err := service.ListAssets(ctx, actor, "k_project_1", AssetFilter{})
	if err != nil {
		t.Fatalf("列素材失败：%v", err)
	}
	for _, value := range values {
		if value.Role != AssetRoleAnalysis {
			t.Fatalf("默认列表混进了 %q：%s", value.Role, value.Title)
		}
	}

	ledger, err := service.ListAssets(ctx, actor, "k_project_1", AssetFilter{Roles: []AssetRole{AssetRoleLedger}})
	if err != nil {
		t.Fatalf("列台账失败：%v", err)
	}
	if len(ledger) != 1 || ledger[0].Title != "台账素材" {
		t.Fatalf("显式要台账时应只拿到台账，得到 %d 条", len(ledger))
	}
}
```

> `testService()` 与 `testActor()` 是 `internal/systems/insights` 测试包里已有的辅助函数（见 `service_test.go:592` 附近）。如果 `errors` 尚未在 `assets_test.go` 的 import 里，加上 `"errors"`。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestIndexAssetDefaultsToAnalysisRole|TestIndexAssetRejectsUnknownRole|TestListAssetsHidesLedgerByDefault'
```

预期：编译失败，`unknown field Role in struct literal of type IndexAssetRequest`。

- [ ] **Step 3: 请求体加 role 并校验**

在 `internal/systems/insights/assets.go` 的 `IndexAssetRequest` 里，`SourceKind` 之后加一行：

```go
	// Role 留空就是分析对象——手工登记的素材默认是要拿去投的。
	// 后台自动收录的台账素材由 RecordLedgerAsset 显式填 ledger。
	Role AssetRole `json:"role"`
```

在 `func (r IndexAssetRequest) validate() error` 里，`if !r.SourceKind.valid()` 那一段之后插入：

```go
	if r.Role != "" && !r.Role.valid() {
		return fmt.Errorf("%w: 素材身份必须是 ledger 或 analysis", ErrInvalidRequest)
	}
```

- [ ] **Step 4: IndexAsset 填默认值**

在 `internal/systems/insights/assets.go` 的 `IndexAsset` 里，找到构造 `Asset{...}` 的地方（`status, reason` 判定之后），在赋值前插入：

```go
	role := request.Role
	if role == "" {
		role = AssetRoleAnalysis
	}
```

并在 `Asset{` 字面量里 `ProjectID:` 之后加：

```go
		Role: role,
```

- [ ] **Step 5: 筛选器加 roles**

在 `internal/systems/insights/assets.go` 的 `AssetFilter` 里，`SourceKinds` 之后加一行：

```go
	// Roles 留空等于「只看分析对象」。这条默认值是台账不淹没四个队列的唯一保证：
	// 忘了传的调用方拿到的是分析对象，不是几千条台账。
	Roles []AssetRole `json:"roles,omitempty"`
```

在 `ListAssets` 的入参校验里（`for _, kind := range filter.SourceKinds` 那一段之后）插入：

```go
	for _, role := range filter.Roles {
		if !role.valid() {
			return nil, fmt.Errorf("%w: 未知的素材身份 %q", ErrInvalidRequest, string(role))
		}
	}
	if len(filter.Roles) == 0 {
		filter.Roles = []AssetRole{AssetRoleAnalysis}
	}
```

- [ ] **Step 6: 仓储按 role 过滤**

在 `internal/systems/insights/mysql_asset_repository.go` 的 `ListAssets` 里，紧跟 `SourceKinds` 那一段 IN 条件之后插入：

```go
	if len(filter.Roles) > 0 {
		placeholders := make([]string, 0, len(filter.Roles))
		for _, role := range filter.Roles {
			placeholders = append(placeholders, "?")
			args = append(args, string(role))
		}
		query += ` AND role IN (` + strings.Join(placeholders, ", ") + `)`
	}
```

> 照抄该文件里 `Statuses` / `SourceKinds` 已有的拼法即可；如果那边用的是别的辅助函数（例如 `inClause`），跟着用同一个，不要引入第二种写法。

- [ ] **Step 7: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestIndexAssetDefaultsToAnalysisRole|TestIndexAssetRejectsUnknownRole|TestListAssetsHidesLedgerByDefault'
```

预期：`ok`，3 个测试全过。

- [ ] **Step 8: 跑素材全量测试**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/
```

预期：`ok`，没有既有测试被打破。

- [ ] **Step 9: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/mysql_asset_repository.go && git commit -m "feat(insights): 登记与列表认素材身份，默认只看分析对象"
```

---

### Task 3: 拉进分析与退回台账

**Files:**
- Modify: `internal/systems/insights/assets.go`（新增 `UpdateAssetRole` 仓储方法声明与两个服务方法）
- Modify: `internal/systems/insights/mysql_asset_repository.go`（`UpdateAssetRole` 实现）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Consumes: Task 2 的 `AssetFilter.Roles`、`Asset.Role`；已有的 `AssetTransitionRequest{ExpectedVersion int64; Reason string}`、`AssetMappingFilter{AssetID, Statuses}`、`MappingMatched`。
- Produces:
  - `AssetRepository.UpdateAssetRole(ctx context.Context, input UpdateAssetRoleInput) (Asset, error)`
  - `type UpdateAssetRoleInput struct { OrganizationID contract.OrganizationID; ProjectID contract.ProjectID; ID string; ExpectedVersion int64; To AssetRole; Now time.Time }`
  - `func (s Service) PromoteAssetToAnalysis(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error)`
  - `func (s Service) ReturnAssetToLedger(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error)`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestPromoteAssetToAnalysisKeepsProgress(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "台账里的成片", SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
	})
	if err != nil {
		t.Fatalf("登记台账素材失败：%v", err)
	}
	before := asset.AnalysisStatus

	promoted, err := service.PromoteAssetToAnalysis(ctx, actor, "k_project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "这条要投了"})
	if err != nil {
		t.Fatalf("拉进分析失败：%v", err)
	}
	if promoted.Role != AssetRoleAnalysis {
		t.Fatalf("拉进分析后身份应是分析对象，得到 %q", promoted.Role)
	}
	// 身份换了，进度不清零——这是 role 独立于 analysis_status 的全部意义。
	if promoted.AnalysisStatus != before {
		t.Fatalf("拉进分析不该动分析进度：%q -> %q", before, promoted.AnalysisStatus)
	}
}

func TestReturnAssetToLedgerRefusesMatchedAsset(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "已经对上号的成片", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	mapping, err := service.RegisterAssetMapping(ctx, actor, "k_project_1", RegisterAssetMappingRequest{
		Platform: "douyin", PlatformObjectKind: "creative", PlatformObjectID: "cr_1", PlatformObjectName: "计划一",
	})
	if err != nil {
		t.Fatalf("登记映射失败：%v", err)
	}
	if _, err := service.ResolveAssetMapping(ctx, actor, "k_project_1", mapping.ID, ResolveAssetMappingRequest{
		ExpectedVersion: mapping.Version, AssetID: asset.ID, Note: "人工对上",
	}); err != nil {
		t.Fatalf("对号失败：%v", err)
	}

	// 对上号意味着它有广告对象、有花费。这时候退回台账等于把已经产生的数据藏起来。
	_, err = service.ReturnAssetToLedger(ctx, actor, "k_project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "看错了"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("对上号的素材不该能退回台账，得到 %v", err)
	}
}

func TestReturnAssetToLedgerAllowsUnmatchedAsset(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "拉错了的素材", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	returned, err := service.ReturnAssetToLedger(ctx, actor, "k_project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "这条其实没投"})
	if err != nil {
		t.Fatalf("退回台账失败：%v", err)
	}
	if returned.Role != AssetRoleLedger {
		t.Fatalf("退回后身份应是台账，得到 %q", returned.Role)
	}
}
```

> `RegisterAssetMappingRequest` 与 `ResolveAssetMappingRequest` 的字段名以 `internal/systems/insights/assets.go` 里的现有定义为准；若字段名不同，按现有定义改这两处调用，测试意图不变。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestPromoteAssetToAnalysisKeepsProgress|TestReturnAssetToLedger'
```

预期：编译失败，`service.PromoteAssetToAnalysis undefined`。

- [ ] **Step 3: 加仓储接口与输入结构**

在 `internal/systems/insights/assets.go` 的 `AssetRepository` 接口里，`TransitionAsset` 那一行之后加：

```go
	UpdateAssetRole(ctx context.Context, input UpdateAssetRoleInput) (Asset, error)
```

在 `TransitionAssetInput` 定义之后加：

```go
// UpdateAssetRoleInput 只换身份，不碰 analysis_status。
// 两个维度分开写，是为了让「拉进分析 → 退回台账 → 再拉进分析」全程无损。
type UpdateAssetRoleInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	To              AssetRole
	Now             time.Time
}
```

- [ ] **Step 4: 实现仓储方法**

在 `internal/systems/insights/mysql_asset_repository.go` 里，紧跟 `TransitionAsset` 的实现之后加：

```go
// UpdateAssetRole 换身份。乐观锁和 TransitionAsset 用同一套：版本对不上就报冲突，
// 两个人同时把一条素材一个拉进分析一个退回台账时，后到的那个必须知道自己晚了。
func (r MySQLRepository) UpdateAssetRole(ctx context.Context, input UpdateAssetRoleInput) (Asset, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback()

	current, err := getAssetForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Asset{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if current.Role == input.To {
		return current, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE insight_assets SET role = ?, version = version + 1, updated_at = ?
		 WHERE organization_id = ? AND project_id = ? AND id = ?`,
		string(input.To), input.Now, input.OrganizationID, input.ProjectID, input.ID); err != nil {
		return Asset{}, err
	}
	if err := tx.Commit(); err != nil {
		return Asset{}, err
	}
	current.Role = input.To
	current.Version++
	current.UpdatedAt = input.Now
	return current, nil
}
```

> `ErrVersionConflict` 是该包里已有的哨兵错误；若现有 `TransitionAsset` 用的是别的名字（例如 `ErrConflict`），改成同一个，不要新造。

- [ ] **Step 5: 实现两个服务方法**

在 `internal/systems/insights/assets.go` 的 `transitionAsset` 之后加：

```go
// PromoteAssetToAnalysis 把一条台账素材拉进分析。
//
// 这是唯一一条从台账进分析的路，而且必须有人点：台账里绝大多数素材永远不会投流，
// 自动往里拉只会把四个队列重新灌满。
func (s Service) PromoteAssetToAnalysis(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if len(strings.TrimSpace(request.Reason)) > 1000 {
		return Asset{}, fmt.Errorf("%w: 原因超长", ErrInvalidRequest)
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Role == AssetRoleAnalysis {
		return Asset{}, fmt.Errorf("%w: 这条素材已经是分析对象", ErrInvalidState)
	}
	return s.Assets.UpdateAssetRole(ctx, UpdateAssetRoleInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion, To: AssetRoleAnalysis, Now: s.now(),
	})
}

// ReturnAssetToLedger 把拉错的素材退回台账。
//
// 只有从没对上号的素材能退：对上号意味着它有广告对象、有花费、进过归因，
// 这时候退回台账就是把已经产生的数据从队列里藏起来。
func (s Service) ReturnAssetToLedger(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if len(strings.TrimSpace(request.Reason)) > 1000 {
		return Asset{}, fmt.Errorf("%w: 原因超长", ErrInvalidRequest)
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Role == AssetRoleLedger {
		return Asset{}, fmt.Errorf("%w: 这条素材本来就在台账里", ErrInvalidState)
	}
	matched, err := s.Assets.ListAssetMappings(ctx, actor.OrganizationID, projectID, AssetMappingFilter{
		AssetID: assetID, Statuses: []MappingStatus{MappingMatched}, Limit: 1,
	})
	if err != nil {
		return Asset{}, err
	}
	if len(matched) > 0 {
		return Asset{}, fmt.Errorf("%w: 这条素材已经和广告对象对上号，有投放数据，不能退回台账", ErrInvalidState)
	}
	return s.Assets.UpdateAssetRole(ctx, UpdateAssetRoleInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion, To: AssetRoleLedger, Now: s.now(),
	})
}
```

- [ ] **Step 6: 补测试替身**

`internal/systems/insights` 的测试用内存版仓储实现 `AssetRepository`。找到它（在 `service_test.go` 或 `assets_test.go` 里实现了 `TransitionAsset` 的那个结构体），加上：

```go
func (r *memoryAssetRepository) UpdateAssetRole(ctx context.Context, input UpdateAssetRoleInput) (Asset, error) {
	current, ok := r.assets[input.ID]
	if !ok {
		return Asset{}, ErrNotFound
	}
	if current.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if current.Role == input.To {
		return current, nil
	}
	current.Role = input.To
	current.Version++
	current.UpdatedAt = input.Now
	r.assets[input.ID] = current
	return current, nil
}
```

> 结构体名与字段名（`r.assets`）以现有测试替身为准。若替身把素材存在切片里，照它的方式改写，语义不变：找到、比版本、换 role、涨版本、写回。

- [ ] **Step 7: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestPromoteAssetToAnalysisKeepsProgress|TestReturnAssetToLedger'
```

预期：`ok`，3 个测试全过。

- [ ] **Step 8: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/mysql_asset_repository.go internal/systems/insights/service_test.go && git commit -m "feat(insights): 拉进分析与退回台账两个显式动作"
```

---

### Task 4: 台账不得写特征

**Files:**
- Modify: `internal/systems/insights/assets.go`（`PatchFeatures`、`ExtractFeatures` 入口处）
- Modify: `internal/systems/insights/extraction.go`（`AnalyzeAsset` 入口处）
- Modify: `internal/systems/insights/derived.go`（`DeriveFeatures` 入口处）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Consumes: Task 1 的 `Asset.Role`、`AssetRoleLedger`。
- Produces: `func (s Service) requireAnalysisRole(asset Asset) error`（包内私有）。

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestLedgerAssetRefusesFeatureWrite(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "台账里的一张图", SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
		AssetType: AssetTypeBrandAd, AssetTypeSource: SourceHuman,
	})
	if err != nil {
		t.Fatalf("登记台账素材失败：%v", err)
	}
	// 台账是账本，不是分析对象。往台账素材上写特征等于让它悄悄变成分析对象，
	// 而队列、红点、归因全按 role 过滤——那些特征谁也看不到，白花模型的钱。
	_, err = service.PatchFeatures(ctx, actor, "k_project_1", asset.ID, PatchFeaturesRequest{
		ExpectedVersion: asset.Version,
		Features:        []FeatureInput{{Key: "cta", Value: FeatureValue{Kind: FeatureKindText, Text: "立即购买"}}},
	})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("台账素材不该能写特征，得到 %v", err)
	}
}
```

> `PatchFeaturesRequest` / `FeatureInput` / `FeatureValue` / `FeatureKindText` / `SourceHuman` / `AssetTypeBrandAd` 均为现有定义。若 `cta` 不是 `brand_ad` 特征体系里的合法键，换成 `AllFeatureSchemas()` 中 `brand_ad` 的任意一个文本键——这个测试只关心 role 闸门先于键校验生效，所以闸门必须放在 `input.validate()` **之前**。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestLedgerAssetRefusesFeatureWrite
```

预期：FAIL，错误不是 `ErrInvalidState`（现在会因为别的原因通过或报别的错）。

- [ ] **Step 3: 加闸门函数**

在 `internal/systems/insights/assets.go` 的 `assetsReady` 之后加：

```go
// requireAnalysisRole 挡住往台账素材上写特征的一切路径。
//
// 台账是账本：登记在册就够了，它不进队列、不跑归因、不该有任何特征。
// 四条写路径（人工修改、AI 提取、量客观变量、单条分析）各自都要过这道门——
// 少一条，那条路径就成了绕过身份的后门。
func (s Service) requireAnalysisRole(asset Asset) error {
	if asset.Role == AssetRoleLedger {
		return fmt.Errorf("%w: 这条素材在台账里，先「拉进分析」才能提特征", ErrInvalidState)
	}
	return nil
}
```

- [ ] **Step 4: 四条路径各挂一次**

在 `PatchFeatures` 里，`asset, err := s.Assets.GetAsset(...)` 之后、`if !asset.TypeIdentified()` 之前插入：

```go
	if err := s.requireAnalysisRole(asset); err != nil {
		return nil, err
	}
```

在 `ExtractFeatures`、`DeriveFeatures`、`AnalyzeAsset` 三个方法里各找到它们取素材的那一行（`s.Assets.GetAsset(...)` 返回的变量），紧随其后插入同样的三行；返回值类型不同的按各自的零值返回：

- `ExtractFeatures` 返回 `([]AssetFeature, error)`：`return nil, err`
- `DeriveFeatures` 返回 `([]AssetFeature, error)`：`return nil, err`
- `AnalyzeAsset` 返回 `(AnalysisRun, error)`：`return AnalysisRun{}, err`

> 若某个方法当前没有先取素材（直接把 assetID 传给仓储），在它权限门之后补一次 `asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)`，取到就判 role，取不到原样返回错误。

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestLedgerAssetRefusesFeatureWrite
```

预期：`ok`。

- [ ] **Step 6: 跑全量确认没打破别的**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/systems/insights/
```

预期：`ok`。

- [ ] **Step 7: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/extraction.go internal/systems/insights/derived.go && git commit -m "feat(insights): 四条特征写入路径一律挡住台账素材"
```

---

### Task 5: 台账查得动 —— 游标分页与标题搜索

**Files:**
- Modify: `internal/systems/insights/assets.go`（`AssetFilter` 加 `Cursor` / `Query`，新增 `AssetPage` 与 `ListAssetPage`）
- Modify: `internal/systems/insights/mysql_asset_repository.go`（`ListAssetPage` 实现、游标编解码）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Consumes: Task 2 的 `AssetFilter.Roles`。
- Produces:
  - `type AssetPage struct { Items []Asset ` + "`json:\"items\"`" + `; NextCursor string ` + "`json:\"next_cursor,omitempty\"`" + ` }`
  - `AssetFilter.Cursor string`（JSON `cursor`）、`AssetFilter.Query string`（JSON `q`）
  - `AssetRepository.ListAssetPage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error)`
  - `func (s Service) ListAssetPage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error)`
  - `func encodeAssetCursor(updatedAt time.Time, id string) string` / `func decodeAssetCursor(value string) (time.Time, string, error)`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestAssetCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 8, 13, 10, 30, 0, 123456000, time.UTC)
	encoded := encodeAssetCursor(at, "insightasset_7")
	gotAt, gotID, err := decodeAssetCursor(encoded)
	if err != nil {
		t.Fatalf("游标解不开：%v", err)
	}
	if !gotAt.Equal(at) || gotID != "insightasset_7" {
		t.Fatalf("游标来回一趟变了：%v / %q", gotAt, gotID)
	}
}

func TestDecodeAssetCursorRejectsGarbage(t *testing.T) {
	// 游标是我们自己发出去的不透明串。收到别的东西就是有人在手改 URL，
	// 直接报错，不能悄悄退回第一页——那会让「加载更多」变成无限循环。
	if _, _, err := decodeAssetCursor("not-a-cursor"); err == nil {
		t.Fatal("乱七八糟的游标应该报错")
	}
}

func TestListAssetPageSearchesTitle(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	for _, title := range []string{"春节主视觉 KV", "夏季促销短视频", "春节红包封面"} {
		if _, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
			Title: title, SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
		}); err != nil {
			t.Fatalf("登记 %q 失败：%v", title, err)
		}
	}
	page, err := service.ListAssetPage(ctx, actor, "k_project_1", AssetFilter{
		Roles: []AssetRole{AssetRoleLedger}, Query: "春节",
	})
	if err != nil {
		t.Fatalf("搜标题失败：%v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("「春节」应命中 2 条，得到 %d 条", len(page.Items))
	}
}
```

> 若 `assets_test.go` 尚未 import `"time"`，加上。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestAssetCursorRoundTrip|TestDecodeAssetCursorRejectsGarbage|TestListAssetPageSearchesTitle'
```

预期：编译失败，`undefined: encodeAssetCursor`。

- [ ] **Step 3: 加筛选字段与分页结构**

在 `internal/systems/insights/assets.go` 的 `AssetFilter` 里，`Limit` 之前加：

```go
	// Cursor 是上一页最后一条的位置，不透明串，由 ListAssetPage 发出。
	// 台账几千条起，offset 分页翻到第 50 页要数过前面 4900 行；游标只比一次索引。
	Cursor string `json:"cursor,omitempty"`
	// Query 按标题模糊搜。台账里绝大多数素材人只记得个名字，
	// 没有搜索的清单等于让人一页页翻——那就是查不动。
	Query string `json:"q,omitempty"`
```

在 `AssetFilter` 定义之后加：

```go
// AssetPage 是一页素材。NextCursor 为空表示到底了——
// 前端靠这一个信号决定「加载更多」还显不显示，不必自己数条数。
type AssetPage struct {
	Items      []Asset `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}
```

在 `AssetRepository` 接口里，`ListAssets` 那一行之后加：

```go
	ListAssetPage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error)
```

- [ ] **Step 4: 加服务方法**

在 `internal/systems/insights/assets.go` 的 `ListAssets` 之后加：

```go
// ListAssetPage 是台账清单的取数口。ListAssets 一次取完的做法留给分析对象那几十条，
// 台账不能这么取——它和平台素材库一样大。
func (s Service) ListAssetPage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return AssetPage{}, err
	}
	for _, status := range filter.Statuses {
		if !status.valid() {
			return AssetPage{}, fmt.Errorf("%w: 未知的分析状态 %q", ErrInvalidRequest, string(status))
		}
	}
	for _, assetType := range filter.AssetTypes {
		if !assetType.valid() {
			return AssetPage{}, fmt.Errorf("%w: 未知的素材类型 %q", ErrInvalidRequest, string(assetType))
		}
	}
	for _, kind := range filter.SourceKinds {
		if !kind.valid() {
			return AssetPage{}, fmt.Errorf("%w: 未知的素材来源 %q", ErrInvalidRequest, string(kind))
		}
	}
	for _, role := range filter.Roles {
		if !role.valid() {
			return AssetPage{}, fmt.Errorf("%w: 未知的素材身份 %q", ErrInvalidRequest, string(role))
		}
	}
	if len(filter.Roles) == 0 {
		filter.Roles = []AssetRole{AssetRoleAnalysis}
	}
	if len(filter.Query) > 255 {
		return AssetPage{}, fmt.Errorf("%w: 搜索词过长", ErrInvalidRequest)
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 50
	}
	return s.Assets.ListAssetPage(ctx, actor.OrganizationID, projectID, filter)
}
```

- [ ] **Step 5: 实现游标编解码与仓储方法**

在 `internal/systems/insights/mysql_asset_repository.go` 的 `ListAssets` 之后加：

```go
// 游标编的是排序键本身 (updated_at, id)，不是行号。中间插进来一条新素材时，
// 已经翻过的页不会因此错位重复——这是 offset 分页做不到的。
func encodeAssetCursor(updatedAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(updatedAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeAssetCursor(value string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: 游标格式不对", ErrInvalidRequest)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("%w: 游标格式不对", ErrInvalidRequest)
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: 游标里的时间读不出来", ErrInvalidRequest)
	}
	return at, parts[1], nil
}

func (r MySQLRepository) ListAssetPage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error) {
	query := insightAssetSelect + ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}

	if len(filter.Roles) > 0 {
		placeholders := make([]string, 0, len(filter.Roles))
		for _, role := range filter.Roles {
			placeholders = append(placeholders, "?")
			args = append(args, string(role))
		}
		query += ` AND role IN (` + strings.Join(placeholders, ", ") + `)`
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		query += ` AND analysis_status IN (` + strings.Join(placeholders, ", ") + `)`
	}
	if len(filter.AssetTypes) > 0 {
		placeholders := make([]string, 0, len(filter.AssetTypes))
		for _, assetType := range filter.AssetTypes {
			placeholders = append(placeholders, "?")
			args = append(args, string(assetType))
		}
		query += ` AND asset_type IN (` + strings.Join(placeholders, ", ") + `)`
	}
	if len(filter.SourceKinds) > 0 {
		placeholders := make([]string, 0, len(filter.SourceKinds))
		for _, kind := range filter.SourceKinds {
			placeholders = append(placeholders, "?")
			args = append(args, string(kind))
		}
		query += ` AND source_kind IN (` + strings.Join(placeholders, ", ") + `)`
	}
	if trimmed := strings.TrimSpace(filter.Query); trimmed != "" {
		// 只搜标题。全文检索是另一件事，台账要的只是「我记得它叫什么」。
		query += ` AND title LIKE ?`
		args = append(args, "%"+escapeLike(trimmed)+"%")
	}
	if filter.Cursor != "" {
		at, id, err := decodeAssetCursor(filter.Cursor)
		if err != nil {
			return AssetPage{}, err
		}
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, at, at, id)
	}

	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	// 多要一条：拿到了就说明还有下一页，用不着再数一次总数。
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return AssetPage{}, err
	}
	defer rows.Close()

	items := make([]Asset, 0, limit)
	for rows.Next() {
		value, scanErr := scanAsset(rows)
		if scanErr != nil {
			return AssetPage{}, scanErr
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return AssetPage{}, err
	}

	page := AssetPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeAssetCursor(last.UpdatedAt, last.ID)
	}
	return page, nil
}

// escapeLike 把 LIKE 的三个元字符转义掉。不转义的话，搜「100%」会命中所有素材。
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
```

在该文件的 import 块里补上 `"encoding/base64"` 与 `"time"`（若尚未引入）。

- [ ] **Step 6: 补测试替身**

在 Task 3 Step 6 改过的那个内存版仓储上加：

```go
func (r *memoryAssetRepository) ListAssetPage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error) {
	values, err := r.ListAssets(ctx, organizationID, projectID, filter)
	if err != nil {
		return AssetPage{}, err
	}
	matched := make([]Asset, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(filter.Query); trimmed != "" && !strings.Contains(value.Title, trimmed) {
			continue
		}
		matched = append(matched, value)
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if len(matched) > limit {
		return AssetPage{Items: matched[:limit], NextCursor: encodeAssetCursor(matched[limit-1].UpdatedAt, matched[limit-1].ID)}, nil
	}
	return AssetPage{Items: matched}, nil
}
```

> 替身的 `ListAssets` 必须已经按 `filter.Roles` 过滤（Task 2 若没在替身里加，这里补上）。若替身文件尚未 import `"strings"`，加上。

- [ ] **Step 7: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run 'TestAssetCursorRoundTrip|TestDecodeAssetCursorRejectsGarbage|TestListAssetPageSearchesTitle'
```

预期：`ok`，3 个测试全过。

- [ ] **Step 8: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/mysql_asset_repository.go internal/systems/insights/service_test.go && git commit -m "feat(insights): 素材清单支持游标分页与标题搜索"
```

---

### Task 6: HTTP 层与契约

**Files:**
- Modify: `internal/systems/insights/httpapi/assets.go`（`listAssets`、`assetAction`）
- Modify: `api/openapi/insights-v1.yaml`
- Test: `internal/systems/insights/httpapi/server_test.go`

**Interfaces:**
- Consumes: Task 3 的 `PromoteAssetToAnalysis` / `ReturnAssetToLedger`、Task 5 的 `ListAssetPage` / `AssetPage`。
- Produces: `GET /api/insights/v1/projects/{project_id}/assets` 新增查询参数 `role`（可重复）、`cursor`、`q`，响应新增 `next_cursor`；`POST .../assets/{asset_id}:promote` 与 `POST .../assets/{asset_id}:return-to-ledger`。

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/httpapi/server_test.go` 末尾追加：

```go
func TestListAssetsAcceptsRoleAndCursor(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()

	response := server.get(t, "/api/insights/v1/projects/k_project_1/assets?role=ledger&q=%E6%98%A5%E8%8A%82&limit=20")
	if response.Code != http.StatusOK {
		t.Fatalf("台账清单应返回 200，得到 %d：%s", response.Code, response.Body.String())
	}
	var body struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解不开：%v", err)
	}
}

func TestListAssetsRejectsUnknownRole(t *testing.T) {
	server, cleanup := newTestServer(t)
	defer cleanup()

	response := server.get(t, "/api/insights/v1/projects/k_project_1/assets?role=archive")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("未知身份应返回 400，得到 %d：%s", response.Code, response.Body.String())
	}
}
```

> `newTestServer` 与 `server.get` 是该测试文件里已有的辅助；照文件里现有测试的写法调用，参数签名以现有为准。若辅助函数名不同（例如 `newServer(t)` / `doGET(...)`），改成现有的名字，测试意图不变。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/httpapi/ -run 'TestListAssetsAcceptsRoleAndCursor|TestListAssetsRejectsUnknownRole'
```

预期：`TestListAssetsRejectsUnknownRole` FAIL（现在会返回 200，因为 `role` 参数被忽略）。

- [ ] **Step 3: 改 listAssets**

把 `internal/systems/insights/httpapi/assets.go` 的 `listAssets` 整个替换成：

```go
func (s *Server) listAssets(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := insights.AssetFilter{
		LineageID: query.Get("lineage_id"),
		Cursor:    query.Get("cursor"),
		Query:     strings.TrimSpace(query.Get("q")),
		Limit:     queryLimit(request),
	}
	for _, value := range queryList(request, "status") {
		filter.Statuses = append(filter.Statuses, insights.AnalysisStatus(value))
	}
	for _, value := range queryList(request, "asset_type") {
		filter.AssetTypes = append(filter.AssetTypes, insights.AssetType(value))
	}
	for _, value := range queryList(request, "source_kind") {
		filter.SourceKinds = append(filter.SourceKinds, insights.AssetSourceKind(value))
	}
	// 不传 role 就是只看分析对象。这条默认值在服务层，这里只负责把人写的传下去。
	for _, value := range queryList(request, "role") {
		filter.Roles = append(filter.Roles, insights.AssetRole(value))
	}
	page, err := s.app.ListAssetPage(request.Context(), mustActor(request), projectID(request), filter)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	// items 这个键名和以前一样：前端不改也能继续读，next_cursor 是纯增量。
	body := map[string]any{"items": page.Items}
	if page.NextCursor != "" {
		body["next_cursor"] = page.NextCursor
	}
	writeJSON(writer, http.StatusOK, body)
}
```

> `strings` 已在该文件 import 中（`queryList` 用了它）。

- [ ] **Step 4: 加两个动作**

在 `internal/systems/insights/httpapi/assets.go` 的 `assetAction` 的 switch 里，`case strings.HasSuffix(action, ":retire"):` 之前插入：

```go
	case strings.HasSuffix(action, ":promote"):
		s.assetTransition(writer, request, strings.TrimSuffix(action, ":promote"), s.app.PromoteAssetToAnalysis)
	case strings.HasSuffix(action, ":return-to-ledger"):
		s.assetTransition(writer, request, strings.TrimSuffix(action, ":return-to-ledger"), s.app.ReturnAssetToLedger)
```

> 这两个方法的签名和 `assetTransitionFunc` 完全一致（`AssetTransitionRequest` 进、`Asset` 出），直接复用现成的 `assetTransition`。

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/httpapi/
```

预期：`ok`。

- [ ] **Step 6: 更新 OpenAPI**

在 `api/openapi/insights-v1.yaml` 中：

1. 找到 `/projects/{project_id}/assets` 的 `get`，在 `parameters` 里追加三条：

```yaml
        - name: role
          in: query
          description: 素材身份，可重复。不传等于只看分析对象（ledger 是台账，几千条起）。
          schema:
            type: array
            items:
              type: string
              enum: [ledger, analysis]
          explode: true
        - name: cursor
          in: query
          description: 上一页返回的 next_cursor。不透明串，不要自己拼。
          schema:
            type: string
        - name: q
          in: query
          description: 按标题模糊搜，最长 255 字符。
          schema:
            type: string
            maxLength: 255
```

2. 该 `get` 的 200 响应 schema 里，`items` 同级追加：

```yaml
                next_cursor:
                  type: string
                  description: 下一页的游标。字段不存在就是到底了。
```

3. `InsightAsset` 的 schema 的 `properties` 里追加，并把 `role` 加进 `required`：

```yaml
        role:
          type: string
          enum: [ledger, analysis]
          description: 素材身份。ledger 是台账（登记在册，不进分析队列），analysis 是分析对象（投过流、要跑归因）。
```

4. `IndexInsightAssetBody` 的 `properties` 里追加（**不**加进 `required`）：

```yaml
        role:
          type: string
          enum: [ledger, analysis]
          default: analysis
          description: 留空即分析对象。台账由后台自动收录，人手工登记的默认都是要投的。
```

5. 找到罗列素材动作的那一段（`:identify-type` / `:confirm` / `:retire` 所在处），把 `:promote` 与 `:return-to-ledger` 按同样格式补上，描述分别写「把台账素材拉进分析（需要 insights.write）」与「把拉错的素材退回台账；已和广告对象对上号的素材会被拒（需要 insights.write）」。

- [ ] **Step 7: 校验 YAML 能解析**

```bash
cd /d/project/cookies-integration-mvp && npx --yes js-yaml api/openapi/insights-v1.yaml > /dev/null && echo "YAML OK"
```

预期：输出 `YAML OK`。

- [ ] **Step 8: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/httpapi/assets.go internal/systems/insights/httpapi/server_test.go api/openapi/insights-v1.yaml && git commit -m "feat(insights): 素材接口暴露 role/cursor/q 与拉进分析、退回台账"
```

---

### Task 7: 洞察侧的收录入口

**Files:**
- Create: `internal/systems/insights/ledger.go`
- Create: `internal/systems/insights/ledger_test.go`

**Interfaces:**
- Consumes: Task 1-2 的 `AssetRole` / `Asset.Role` / `IndexAssetRequest.Role`、已有的 `AssetSourceKind`、`Service.Assets`、`Service.idGenerator()`、`Service.now()`。
- Produces:
  - `type RecordLedgerAssetRequest struct { OrganizationID contract.OrganizationID; ProjectID contract.ProjectID; ActorID string; Title string; SourceKind AssetSourceKind; SourceRef string; SourceJobID string; PlatformAssetID string; PlatformAssetVersion int64 }`
  - `func (s Service) RecordLedgerAsset(ctx context.Context, request RecordLedgerAssetRequest) (Asset, error)`

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/ledger_test.go`：

```go
package insights

import (
	"context"
	"errors"
	"testing"
)

func TestRecordLedgerAssetWritesLedgerRole(t *testing.T) {
	service := testService()
	asset, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "k_org_1", ProjectID: "k_project_1", ActorID: "user_1",
		Title: "主视觉 KV.png", SourceKind: AssetSourceUpload,
		PlatformAssetID: "asset_1", PlatformAssetVersion: 1,
	})
	if err != nil {
		t.Fatalf("收录失败：%v", err)
	}
	if asset.Role != AssetRoleLedger {
		t.Fatalf("收录进来的必须是台账，得到 %q", asset.Role)
	}
	// 台账素材没有广告形态。AssetType 是六种广告形态之一，不是 image/video，
	// 从平台的 AssetKind 是推不出来的——推出来就是编。
	if asset.AssetType != AssetTypeUnknown {
		t.Fatalf("收录不该猜广告形态，得到 %q", asset.AssetType)
	}
}

func TestRecordLedgerAssetRequiresPlatformPair(t *testing.T) {
	service := testService()
	_, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "k_org_1", ProjectID: "k_project_1", ActorID: "user_1",
		Title: "缺版本号的引用", SourceKind: AssetSourceUpload, PlatformAssetID: "asset_1",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("平台引用给一半应被拒，得到 %v", err)
	}
}

func TestRecordLedgerAssetRequiresTitle(t *testing.T) {
	service := testService()
	_, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "k_org_1", ProjectID: "k_project_1", ActorID: "user_1",
		SourceKind: AssetSourceUpload, PlatformAssetID: "asset_1", PlatformAssetVersion: 1,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("标题为空应被拒——兜底标题由调用方给，服务层不替它编，得到 %v", err)
	}
}
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestRecordLedgerAsset
```

预期：编译失败，`service.RecordLedgerAsset undefined`。

- [ ] **Step 3: 实现收录入口**

创建 `internal/systems/insights/ledger.go`：

```go
package insights

import (
	"context"
	"fmt"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// RecordLedgerAssetRequest 是后台收录一条台账素材要的全部东西。
//
// 它和 IndexAssetRequest 长得像，但来路完全不同：那一条是人在界面上手工登记，
// 走 HTTP、过权限门、能填广告形态；这一条是素材库那边刚落库成功后回调进来的，
// 没有 HTTP 请求、没有登录会话，只有一个「是谁的动作触发的」的 ActorID。
type RecordLedgerAssetRequest struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	// ActorID 是触发这次入库的人。台账要能回答「这条是谁弄进来的」，
	// 而收录发生在请求线程之外，拿不到完整的 ActorContext。
	ActorID string

	Title       string
	SourceKind  AssetSourceKind
	SourceRef   string
	SourceJobID string

	PlatformAssetID      string
	PlatformAssetVersion int64
}

func (r RecordLedgerAssetRequest) validate() error {
	if strings.TrimSpace(string(r.OrganizationID)) == "" || strings.TrimSpace(string(r.ProjectID)) == "" {
		return fmt.Errorf("%w: 收录台账素材必须带组织与项目", ErrInvalidRequest)
	}
	title := strings.TrimSpace(r.Title)
	if title == "" || len(title) > 255 {
		return fmt.Errorf("%w: 台账素材标题为空或超长", ErrInvalidRequest)
	}
	if !r.SourceKind.valid() {
		return fmt.Errorf("%w: 未知的素材来源 %q", ErrInvalidRequest, string(r.SourceKind))
	}
	if len(r.SourceRef) > 512 {
		return fmt.Errorf("%w: 来源引用超长", ErrInvalidRequest)
	}
	if (r.PlatformAssetID == "") != (r.PlatformAssetVersion == 0) {
		return fmt.Errorf("%w: 媒体资产引用要么同时给出 ID 与版本，要么都不给", ErrInvalidRequest)
	}
	return nil
}

// RecordLedgerAsset 把平台素材库刚入库的一个素材版本记进台账。
//
// 它**不过人的权限门**：调用它的不是人，是素材库落库成功后的回调。放权限门在这里
// 只会让所有后台入库路径都需要伪造一个带 insights.write 的身份，那才是真的没有门。
// 真正的门在写这一侧的唯一入口上——只有 internal/integrations/insightsledger 能调它。
//
// 它也**不判广告形态**：AssetType 是六种广告形态之一，从平台的 image/video 推不出来。
// 台账素材一律留空，等人把它「拉进分析」时再识别。
func (s Service) RecordLedgerAsset(ctx context.Context, request RecordLedgerAssetRequest) (Asset, error) {
	if s.Assets == nil {
		return Asset{}, fmt.Errorf("insight asset repository is not configured")
	}
	if err := request.validate(); err != nil {
		return Asset{}, err
	}
	id, err := s.idGenerator()("insightasset")
	if err != nil {
		return Asset{}, err
	}
	now := s.now()
	createdBy := strings.TrimSpace(request.ActorID)
	if createdBy == "" {
		createdBy = "system"
	}
	return s.Assets.CreateAsset(ctx, Asset{
		ID: id, OrganizationID: request.OrganizationID, ProjectID: request.ProjectID,
		Role: AssetRoleLedger,
		// 台账素材各自成一条血缘。血缘说的是「同一条创意改了几版」，
		// 那是分析对象才需要梳的关系；台账只管有没有这个东西。
		LineageID: id, Revision: 1,
		Title: strings.TrimSpace(request.Title), SourceKind: request.SourceKind,
		SourceRef: request.SourceRef, SourceJobID: request.SourceJobID,
		PlatformAssetID: request.PlatformAssetID, PlatformAssetVersion: request.PlatformAssetVersion,
		AssetType:      AssetTypeUnknown,
		AnalysisStatus: AnalysisAwaitingData,
		// 状态原因写清楚它为什么停在这儿，免得有人在库里看到 awaiting_data 就去补数据。
		AnalysisStatusReason:    "台账素材，未进入分析。",
		AnalysisStatusChangedAt: &now,
		Version:                 1, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	})
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestRecordLedgerAsset
```

预期：`ok`，3 个测试全过。

- [ ] **Step 5: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/ledger.go internal/systems/insights/ledger_test.go && git commit -m "feat(insights): 后台收录台账素材的入口"
```

---

### Task 8: 平台侧的收录钩子

**Files:**
- Create: `internal/platform/assets/ledger.go`
- Create: `internal/platform/assets/ledger_test.go`
- Modify: `internal/platform/assets/upload_service.go`（结构体 :29-44；落库点 :198、:365、:642）
- Modify: `internal/platform/assets/generated_intake_service.go`（:158）
- Modify: `internal/platform/assets/external_import.go`（:171）

**Interfaces:**
- Produces:
  - `type LedgerEntry struct { OrganizationID contract.OrganizationID; ProjectID contract.ProjectID; ActorID string; AssetID contract.AssetID; Version int64; Kind contract.AssetKind; SourceType contract.AssetSourceType; Title string }`
  - `type LedgerRecorder interface { Record(ctx context.Context, entry LedgerEntry) error }`
  - `type LedgerRelay struct { Recorder LedgerRecorder }`，`func (r *LedgerRelay) Record(ctx context.Context, entry LedgerEntry) error`
  - `func LedgerTitle(filename string, source contract.AssetSourceType, at time.Time) string`
  - `UploadService.Ledger LedgerRecorder`（新字段）
  - `func (s UploadService) recordLedger(ctx context.Context, entry LedgerEntry)`（包内私有）

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/assets/ledger_test.go`：

```go
package assets

import (
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestLedgerTitlePrefersFilename(t *testing.T) {
	at := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	if got := LedgerTitle("主视觉 KV.png", contract.AssetSourceUpload, at); got != "主视觉 KV.png" {
		t.Fatalf("有文件名就用文件名，得到 %q", got)
	}
}

func TestLedgerTitleFallsBackBySource(t *testing.T) {
	at := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	cases := map[contract.AssetSourceType]string{
		contract.AssetSourceRendered:          "渲染成片 · 2026-08-13",
		contract.AssetSourceProviderGenerated: "模型产物 · 2026-08-13",
		contract.AssetSourceImported:          "外部导入 · 2026-08-13",
		contract.AssetSourceUpload:            "未命名素材 · 2026-08-13",
	}
	for source, want := range cases {
		if got := LedgerTitle("", source, at); got != want {
			t.Fatalf("%q 的兜底标题应是 %q，得到 %q", source, want, got)
		}
	}
}

func TestLedgerTitleTrimsOverlongFilename(t *testing.T) {
	// 台账的 title 列是 VARCHAR(255)。超长文件名不截断的话，
	// 收录会在 INSERT 那一步失败，而失败是被吞掉的——素材就这么悄悄漏登记了。
	long := ""
	for len(long) < 400 {
		long += "长"
	}
	got := LedgerTitle(long, contract.AssetSourceUpload, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if len([]rune(got)) > 255 {
		t.Fatalf("标题应截到 255 个字符以内，得到 %d 个", len([]rune(got)))
	}
}

func TestLedgerRelayIsSafeWhenUnwired(t *testing.T) {
	// 装配顺序决定了 relay 一定先于 recorder 存在。这段时间里入库不能崩。
	relay := &LedgerRelay{}
	if err := relay.Record(t.Context(), LedgerEntry{}); err != nil {
		t.Fatalf("没接上 recorder 时应当什么都不做，得到 %v", err)
	}
}
```

> `t.Context()` 需要 Go 1.24+。若项目是 Go 1.22/1.23，改成 `context.Background()` 并 import `"context"`；用 `go version` 确认。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run TestLedger
```

预期：编译失败，`undefined: LedgerTitle`。

- [ ] **Step 3: 实现接口与标题推导**

创建 `internal/platform/assets/ledger.go`：

```go
package assets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 台账是洞察那边的账本：平台里落库成功的每一个素材版本都该在里面有一条。
//
// 为什么是回调不是事件：仓里的 assets_outbox 和 event_outbox 两张表都在写，
// 但全仓没有任何一处 SELECT 它们，Dispatcher 在生产代码里从没被实例化过。
// 接一条事实上不会被消费的事件，等于台账永远是空的。
//
// 分层不能破：这个包在 internal/platform 下，不许 import internal/systems。
// 所以这里只定义接口，实现放在 internal/integrations/insightsledger，
// 由 cmd/cookies-api 在装配时塞进来。
type LedgerEntry struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	// ActorID 是触发这次入库的人。台账要能回答「这条是谁弄进来的」。
	ActorID string

	AssetID    contract.AssetID
	Version    int64
	Kind       contract.AssetKind
	SourceType contract.AssetSourceType
	Title      string
}

type LedgerRecorder interface {
	Record(ctx context.Context, entry LedgerEntry) error
}

// LedgerRelay 解装配顺序的死结：uploadService 在 main.go 靠前的地方就构造好了，
// 而 insightsService 要到几百行之后才有；中间还有几处按值把 UploadService 拷走。
// 拷的是这个指针，回填的也是这个指针里的字段，两边就对上了。
type LedgerRelay struct {
	Recorder LedgerRecorder
}

func (r *LedgerRelay) Record(ctx context.Context, entry LedgerEntry) error {
	if r == nil || r.Recorder == nil {
		return nil
	}
	return r.Recorder.Record(ctx, entry)
}

const ledgerTitleMaxRunes = 255

// LedgerTitle 给台账里的一条素材起个人看得懂的名字。
//
// 上传有文件名就用文件名——那是人自己起的，比任何生成的名字都准。
// 其余几条路径没有名字可用，按来源给个说得清出处的兜底，至少不是一串 ID。
func LedgerTitle(filename string, source contract.AssetSourceType, at time.Time) string {
	if trimmed := strings.TrimSpace(filename); trimmed != "" {
		runes := []rune(trimmed)
		if len(runes) > ledgerTitleMaxRunes {
			return string(runes[:ledgerTitleMaxRunes])
		}
		return trimmed
	}
	date := at.Format("2006-01-02")
	switch source {
	case contract.AssetSourceRendered:
		return fmt.Sprintf("渲染成片 · %s", date)
	case contract.AssetSourceProviderGenerated:
		return fmt.Sprintf("模型产物 · %s", date)
	case contract.AssetSourceImported:
		return fmt.Sprintf("外部导入 · %s", date)
	case contract.AssetSourceCaptured:
		return fmt.Sprintf("采集素材 · %s", date)
	}
	return fmt.Sprintf("未命名素材 · %s", date)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run TestLedger
```

预期：`ok`，4 个测试全过。

- [ ] **Step 5: 加字段与调用辅助**

在 `internal/platform/assets/upload_service.go` 的 `UploadService` 结构体里，`UsePolicy` 之后加一行：

```go
	Ledger           LedgerRecorder
```

在同文件的 `func (s UploadService) now() time.Time` 之前加：

```go
// recordLedger 把刚落库的素材版本记进洞察的台账。
//
// 失败只记日志不回滚：上传已经成功了，用户的文件在库里躺着，为了一条账目把它退掉
// 是本末倒置。漏掉的那条由 cookies-maintain backfill-ledger 补。
//
// 派生物（derived）不收：缩略图、转码档、抽出来的音轨都是同一个素材的不同形态，
// 收进台账只会让「我有多少素材」这个数字翻好几倍。
func (s UploadService) recordLedger(ctx context.Context, entry LedgerEntry) {
	if s.Ledger == nil || entry.SourceType == contract.AssetSourceDerived {
		return
	}
	if err := s.Ledger.Record(ctx, entry); err != nil {
		log.Printf("记素材台账失败 asset=%s version=%d: %v", entry.AssetID, entry.Version, err)
	}
}
```

在该文件 import 块补 `"log"`（若尚未引入）。

- [ ] **Step 6: 四个落库点各挂一次**

**6a.** `upload_service.go` 的 `Finalize`（约 :198），`ref, err := s.Repository.CompleteUpload(ctx, session.ID, commit, s.now())` 的错误处理之后、函数返回之前插入：

```go
	s.recordLedger(ctx, LedgerEntry{
		OrganizationID: session.OrganizationID, ProjectID: session.ProjectID,
		ActorID: session.Principal.ID,
		AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version,
		Kind: commit.Kind, SourceType: commit.SourceType,
		Title: LedgerTitle(session.Filename, commit.SourceType, s.now()),
	})
```

**6b.** `upload_service.go` 的 `ingestRenderedVideo`（约 :365），`ref, err := s.Repository.CompleteRender(...)` 的错误处理之后插入：

```go
	s.recordLedger(ctx, LedgerEntry{
		OrganizationID: requestContext.Actor.OrganizationID, ProjectID: projectID,
		ActorID: requestContext.Actor.Principal.ID,
		AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version,
		Kind: commit.Kind, SourceType: commit.SourceType,
		Title: LedgerTitle("", commit.SourceType, s.now()),
	})
```

**6c.** `upload_service.go` 的 `IngestRenderedImage`（约 :642），`ref, err := s.Repository.CompleteRender(...)` 的错误处理之后插入与 6b 完全相同的那一段。

**6d.** `generated_intake_service.go`（约 :158），`_, err = w.Repository.CompleteIntake(ctx, intake, commit, w.now())` 的错误处理之后插入：

```go
	w.Upload.recordLedger(ctx, LedgerEntry{
		OrganizationID: intake.OrganizationID, ProjectID: intake.ProjectID,
		ActorID: intake.CreatedBy,
		AssetID: commit.AssetID, Version: commit.Version,
		Kind: commit.Kind, SourceType: commit.SourceType,
		Title: LedgerTitle("", commit.SourceType, w.now()),
	})
```

> `intake.CreatedBy` 若不存在，用 `GeneratedIntake` 上任意一个记录发起人的字段；都没有就传 `""`（`RecordLedgerAsset` 会落成 `system`）。

**6e.** `external_import.go`（约 :171），`ref, err := s.Repository.CompleteExternalImport(ctx, stored.ID, commit, s.now())` 的错误处理之后插入：

```go
	s.Upload.recordLedger(ctx, LedgerEntry{
		OrganizationID: stored.OrganizationID, ProjectID: projectID,
		ActorID: requestContext.Actor.Principal.ID,
		AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version,
		Kind: commit.Kind, SourceType: commit.SourceType,
		Title: LedgerTitle("", commit.SourceType, s.now()),
	})
```

**不改的两处**：`upload_service.go` 的两个 `CompleteDerived`（:461、:539）。派生物不进台账，`recordLedger` 里那道 `AssetSourceDerived` 判断是第二道保险，不是替代。

- [ ] **Step 7: 编译并跑平台素材测试**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/platform/assets/
```

预期：`ok`。

- [ ] **Step 8: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/platform/assets/ledger.go internal/platform/assets/ledger_test.go internal/platform/assets/upload_service.go internal/platform/assets/generated_intake_service.go internal/platform/assets/external_import.go && git commit -m "feat(assets): 素材落库成功后回调台账，派生物除外"
```

---

### Task 9: 适配器与装配

**Files:**
- Create: `internal/integrations/insightsledger/recorder.go`
- Create: `internal/integrations/insightsledger/recorder_test.go`
- Modify: `cmd/cookies-api/main.go`（:132 附近构造 relay；:515 之后回填）

**Interfaces:**
- Consumes: Task 7 的 `insights.RecordLedgerAssetRequest` / `Service.RecordLedgerAsset`、Task 8 的 `assets.LedgerEntry` / `assets.LedgerRecorder` / `assets.LedgerRelay`。
- Produces: `type Recorder struct { Service *insights.Service }`，`func (r Recorder) Record(ctx context.Context, entry assets.LedgerEntry) error`。

- [ ] **Step 1: 写失败的测试**

创建 `internal/integrations/insightsledger/recorder_test.go`：

```go
package insightsledger

import (
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

func TestSourceKindFromPlatformSource(t *testing.T) {
	cases := map[contract.AssetSourceType]insights.AssetSourceKind{
		contract.AssetSourceUpload:            insights.AssetSourceUpload,
		contract.AssetSourceRendered:          insights.AssetSourceCreative,
		contract.AssetSourceProviderGenerated: insights.AssetSourceCreative,
		contract.AssetSourceImported:          insights.AssetSourceExternal,
		contract.AssetSourceCaptured:          insights.AssetSourceExternal,
	}
	for source, want := range cases {
		got, ok := sourceKind(source)
		if !ok {
			t.Fatalf("%q 应该有对应的洞察来源", source)
		}
		if got != want {
			t.Fatalf("%q 应映射成 %q，得到 %q", source, want, got)
		}
	}
}

func TestSourceKindRejectsDerived(t *testing.T) {
	// 派生物是同一个素材的另一种形态，不是另一条素材。
	if _, ok := sourceKind(contract.AssetSourceDerived); ok {
		t.Fatal("派生物不该有洞察来源——它根本不进台账")
	}
}
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/integrations/insightsledger/
```

预期：`no Go files` 或编译失败，`undefined: sourceKind`。

- [ ] **Step 3: 实现适配器**

创建 `internal/integrations/insightsledger/recorder.go`：

```go
// Package insightsledger 把平台素材库的入库事实翻成洞察台账的一条记录。
//
// 它存在的唯一理由是分层：internal/platform/assets 不许 import
// internal/systems/insights，所以那边只定义接口，翻译放在这里，
// 由 cmd/cookies-api 在装配时把这个实现塞回去。
package insightsledger

import (
	"context"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

type Recorder struct {
	Service *insights.Service
}

func (r Recorder) Record(ctx context.Context, entry assets.LedgerEntry) error {
	if r.Service == nil {
		return nil
	}
	kind, ok := sourceKind(entry.SourceType)
	if !ok {
		return nil
	}
	_, err := r.Service.RecordLedgerAsset(ctx, insights.RecordLedgerAssetRequest{
		OrganizationID: entry.OrganizationID, ProjectID: entry.ProjectID,
		ActorID: entry.ActorID, Title: entry.Title, SourceKind: kind,
		PlatformAssetID: string(entry.AssetID), PlatformAssetVersion: entry.Version,
	})
	return err
}

// sourceKind 把平台的六种入库来源折成洞察的三种。
//
// 折叠是有损的，但洞察关心的只有「这东西是我们自己做的、人传的、还是外面来的」——
// 平台那六种说的是入库通道，不是出处。返回 false 表示这一种根本不进台账。
func sourceKind(source contract.AssetSourceType) (insights.AssetSourceKind, bool) {
	switch source {
	case contract.AssetSourceUpload:
		return insights.AssetSourceUpload, true
	case contract.AssetSourceRendered, contract.AssetSourceProviderGenerated:
		// 渲染成片和模型产物都是创意模块做出来的。
		return insights.AssetSourceCreative, true
	case contract.AssetSourceImported, contract.AssetSourceCaptured:
		return insights.AssetSourceExternal, true
	}
	// AssetSourceDerived 落在这里：缩略图、转码档、抽出来的音轨
	// 是同一个素材的不同形态，收进台账只会让素材数翻好几倍。
	return "", false
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/integrations/insightsledger/
```

预期：`ok`，2 个测试全过。

- [ ] **Step 5: 装配**

在 `cmd/cookies-api/main.go` 中：

**5a.** 把 :132 那一行改成两行——先造 relay，再把它塞进 uploadService：

```go
	// 台账的收录钩子。insightsService 要到几百行之后才造得出来，
	// 而中间有几处按值把 UploadService 拷走；拷的是这个指针，回填也回填这个指针。
	ledgerRelay := &assets.LedgerRelay{}
	uploadService := &assets.UploadService{Repository: assetRepository, Projects: projectService, Blobs: blobs, Scanner: scanner, QuarantineBucket: cfg.ObjectStorage.QuarantineBucket, AssetsBucket: cfg.ObjectStorage.AssetsBucket, UsePolicy: assets.AssetUsePolicy{Rights: assetRepository}, Ledger: ledgerRelay}
```

**5b.** 在 `insightsService := &insights.Service{...}` 那个字面量的**闭合大括号之后**（:547 附近，`if textProvider != nil` 之前）插入：

```go
	// 回填台账钩子。必须在 insightsService 构造完之后——
	// 在这之前，素材库那边每一次入库都从 relay 上读到 nil，什么都不做。
	ledgerRelay.Recorder = insightsledger.Recorder{Service: insightsService}
```

**5c.** 在 import 块加：

```go
	"github.com/shikanon/cookies/internal/integrations/insightsledger"
```

- [ ] **Step 6: 编译并跑全量**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/... 2>&1 | grep -v "^ok\|no test files" | head -20
```

预期：无输出（全绿）。

- [ ] **Step 7: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/integrations/insightsledger/ cmd/cookies-api/main.go && git commit -m "feat(integrations): 素材入库回调接进洞察台账"
```

---

### Task 10: 存量回填命令

**Files:**
- Modify: `internal/systems/insights/ledger.go`（新增 `BackfillLedger`）
- Modify: `internal/systems/insights/assets.go`（`AssetRepository` 加两个方法）
- Modify: `internal/systems/insights/mysql_asset_repository.go`（实现）
- Modify: `cmd/cookies-maintain/main.go`
- Test: `internal/systems/insights/ledger_test.go`

**Interfaces:**
- Consumes: Task 7 的 `RecordLedgerAssetRequest`。
- Produces:
  - `AssetRepository.ListUnledgeredPlatformAssets(ctx context.Context, limit int) ([]UnledgeredPlatformAsset, error)`
  - `type UnledgeredPlatformAsset struct { OrganizationID contract.OrganizationID; ProjectID contract.ProjectID; AssetID string; Version int64; SourceType string; CreatedBy string; CreatedAt time.Time }`
  - `func (s Service) BackfillLedger(ctx context.Context, batch int) (int, error)`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/ledger_test.go` 末尾追加：

```go
func TestBackfillLedgerRejectsBadBatch(t *testing.T) {
	service := testService()
	if _, err := service.BackfillLedger(context.Background(), 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("批量必须是正数，得到 %v", err)
	}
	if _, err := service.BackfillLedger(context.Background(), 100000); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("批量过大应被拒——一次拉十万行会把库拖垮，得到 %v", err)
	}
}
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestBackfillLedgerRejectsBadBatch
```

预期：编译失败，`service.BackfillLedger undefined`。

- [ ] **Step 3: 加仓储接口与结构**

在 `internal/systems/insights/assets.go` 的 `AssetRepository` 接口里，`ListAssetPage` 之后加：

```go
	ListUnledgeredPlatformAssets(ctx context.Context, limit int) ([]UnledgeredPlatformAsset, error)
```

在 `AssetPage` 定义之后加：

```go
// UnledgeredPlatformAsset 是平台素材库里有、但台账里还没有的一个素材版本。
// 回填命令按它一条条补，直到查不出来为止。
type UnledgeredPlatformAsset struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	AssetID        string
	Version        int64
	SourceType     string
	CreatedBy      string
	CreatedAt      time.Time
}
```

- [ ] **Step 4: 实现仓储查询**

在 `internal/systems/insights/mysql_asset_repository.go` 的 `ListAssetPage` 之后加：

```go
// ListUnledgeredPlatformAssets 找出台账漏掉的素材版本。
//
// 跨模块直接查 asset_versions 是有意的：这是一次性的回填命令，不是运行时通路。
// 运行时那条走 internal/integrations/insightsledger，分层没有破。
// 派生物排除在外，理由和收录时一样——它们是同一个素材的另一种形态。
func (r MySQLRepository) ListUnledgeredPlatformAssets(ctx context.Context, limit int) ([]UnledgeredPlatformAsset, error) {
	rows, err := r.DB.QueryContext(ctx, `
		SELECT v.organization_id, v.project_id, v.asset_id, v.version, v.source_type,
		       COALESCE(v.created_by, ''), v.created_at
		FROM asset_versions v
		LEFT JOIN insight_assets a
		  ON a.organization_id = v.organization_id
		 AND a.role = 'ledger'
		 AND a.platform_asset_id = v.asset_id
		 AND a.platform_asset_version = v.version
		WHERE a.id IS NULL AND v.source_type <> 'derived'
		ORDER BY v.created_at ASC, v.asset_id ASC, v.version ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]UnledgeredPlatformAsset, 0, limit)
	for rows.Next() {
		var value UnledgeredPlatformAsset
		if err := rows.Scan(&value.OrganizationID, &value.ProjectID, &value.AssetID,
			&value.Version, &value.SourceType, &value.CreatedBy, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
```

> 若 `asset_versions` 里没有 `created_by` 列，把那一行 SELECT 换成 `''` 常量，并去掉 `COALESCE`。用这条确认列名：
> ```bash
> cd /d/project/cookies-integration-mvp && grep -rn "CREATE TABLE asset_versions" -A 25 migrations/
> ```

- [ ] **Step 5: 实现回填**

在 `internal/systems/insights/ledger.go` 末尾加：

```go
// BackfillLedger 把平台素材库里已有、台账里没有的素材版本补进来。
//
// 一次跑一批，跑完回报补了多少条。跑到回报 0 就是补完了——
// 这个命令可以反复跑，重复的那些会被 uq_insight_assets_ledger_object 挡在库里。
func (s Service) BackfillLedger(ctx context.Context, batch int) (int, error) {
	if s.Assets == nil {
		return 0, fmt.Errorf("insight asset repository is not configured")
	}
	if batch < 1 || batch > 5000 {
		return 0, fmt.Errorf("%w: 单批数量应在 1..5000 之间", ErrInvalidRequest)
	}
	pending, err := s.Assets.ListUnledgeredPlatformAssets(ctx, batch)
	if err != nil {
		return 0, err
	}
	recorded := 0
	for _, item := range pending {
		kind := backfillSourceKind(item.SourceType)
		if kind == "" {
			continue
		}
		_, recordErr := s.RecordLedgerAsset(ctx, RecordLedgerAssetRequest{
			OrganizationID: item.OrganizationID, ProjectID: item.ProjectID,
			ActorID: item.CreatedBy,
			// 回填拿不到当初的文件名——那躺在上传会话里，早过期清掉了。
			// 用入库日期，至少说得清它是哪天进来的。
			Title:           backfillTitle(item.SourceType, item.CreatedAt),
			SourceKind:      kind,
			PlatformAssetID: item.AssetID, PlatformAssetVersion: item.Version,
		})
		if recordErr != nil {
			// 一条补不上不该让整批停下：唯一键撞了、项目已归档，都可能。
			// 报出来让人看见，接着补下一条。
			log.Printf("回填台账失败 asset=%s version=%d: %v", item.AssetID, item.Version, recordErr)
			continue
		}
		recorded++
	}
	return recorded, nil
}

func backfillSourceKind(sourceType string) AssetSourceKind {
	switch sourceType {
	case "upload":
		return AssetSourceUpload
	case "rendered", "provider_generated":
		return AssetSourceCreative
	case "imported", "captured":
		return AssetSourceExternal
	}
	return ""
}

func backfillTitle(sourceType string, at time.Time) string {
	date := at.Format("2006-01-02")
	switch sourceType {
	case "rendered":
		return fmt.Sprintf("渲染成片 · %s", date)
	case "provider_generated":
		return fmt.Sprintf("模型产物 · %s", date)
	case "imported":
		return fmt.Sprintf("外部导入 · %s", date)
	case "captured":
		return fmt.Sprintf("采集素材 · %s", date)
	}
	return fmt.Sprintf("未命名素材 · %s", date)
}
```

在 `ledger.go` 的 import 块补 `"log"` 与 `"time"`。

- [ ] **Step 6: 补测试替身**

在内存版仓储上加：

```go
func (r *memoryAssetRepository) ListUnledgeredPlatformAssets(ctx context.Context, limit int) ([]UnledgeredPlatformAsset, error) {
	return nil, nil
}
```

- [ ] **Step 7: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestBackfillLedgerRejectsBadBatch
```

预期：`ok`。

- [ ] **Step 8: 加子命令**

把 `cmd/cookies-maintain/main.go` 的用法行改成：

```go
		fmt.Fprintln(os.Stderr, "用法: cookies-maintain <purge-empty-drafts|backfill-ledger>")
```

在 `switch` 的 `default:` 之前插入：

```go
	case "backfill-ledger":
		// 台账是这次才建的，之前入库的素材一条都没登记。这个命令把它们补上。
		// 一次补 1000 条，反复跑到回报 0 为止；重复的那些由唯一键挡住，跑几次都无害。
		service := insights.Service{Assets: insights.MySQLRepository{DB: db}}
		recorded, err := service.BackfillLedger(ctx, 1000)
		if err != nil {
			log.Fatalf("backfill insight asset ledger: %v", err)
		}
		log.Printf("补了 %d 条台账素材（回报 0 就是补完了）", recorded)
```

- [ ] **Step 9: 编译并试跑用法**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go run ./cmd/cookies-maintain 2>&1 | head -2
```

预期：输出 `用法: cookies-maintain <purge-empty-drafts|backfill-ledger>`。

- [ ] **Step 10: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/ledger.go internal/systems/insights/ledger_test.go internal/systems/insights/assets.go internal/systems/insights/mysql_asset_repository.go internal/systems/insights/service_test.go cmd/cookies-maintain/main.go && git commit -m "feat(insights): 存量素材回填台账的维护命令"
```

---

### Task 11: 米云正名

**Files:**
- Create: `migrations/insights/20260813120000_insight_asset_source_miyun.up.sql`
- Create: `migrations/insights/20260813120000_insight_asset_source_miyun.down.sql`
- Modify: `internal/systems/insights/assets.go`（`AssetSourceKind`）
- Modify: `api/openapi/insights-v1.yaml`
- Modify: `src/data/api.ts`（`ApiAssetSourceKind`）
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Produces: `insights.AssetSourceMiyun AssetSourceKind = "miyun"`；前端 `ApiAssetSourceKind` 增加 `'miyun'`。

- [ ] **Step 1: 写迁移的 up**

创建 `migrations/insights/20260813120000_insight_asset_source_miyun.up.sql`：

```sql
-- 米云素材一直挂在 source_kind='external' 下面，但那个标签在界面上指的是
-- 「平台外的竞品参照证据」——那些永远不能拿去投放。米云的素材有 platform_asset_id、
-- 能投、要跑归因，和竞品证据是两码事。同一个词指两样东西，看的人一定会搞错。
ALTER TABLE insight_assets
  DROP CONSTRAINT chk_insight_assets_source_kind;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_source_kind
  CHECK (source_kind IN ('creative', 'upload', 'external', 'miyun'));

UPDATE insight_assets
   SET source_kind = 'miyun'
 WHERE source_kind = 'external' AND source_ref LIKE 'miyun://%';
```

- [ ] **Step 2: 写迁移的 down**

创建 `migrations/insights/20260813120000_insight_asset_source_miyun.down.sql`：

```sql
UPDATE insight_assets SET source_kind = 'external' WHERE source_kind = 'miyun';

ALTER TABLE insight_assets
  DROP CONSTRAINT chk_insight_assets_source_kind;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_source_kind
  CHECK (source_kind IN ('creative', 'upload', 'external'));
```

- [ ] **Step 3: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestMiyunIsItsOwnSourceKind(t *testing.T) {
	if !AssetSourceMiyun.valid() {
		t.Fatal("米云应该是一档独立来源")
	}
	// external 在界面上指的是「平台外的竞品参照证据」，那些永远不能投。
	// 米云的素材能投、要跑归因，两者不能共用一个标签。
	if AssetSourceMiyun == AssetSourceExternal {
		t.Fatal("米云不是外部证据")
	}
}
```

- [ ] **Step 4: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestMiyunIsItsOwnSourceKind
```

预期：编译失败，`undefined: AssetSourceMiyun`。

- [ ] **Step 5: 加常量**

在 `internal/systems/insights/assets.go` 的 `AssetSourceKind` 常量块里加一行：

```go
	AssetSourceMiyun    AssetSourceKind = "miyun"    // 米云采集回来的素材
```

把 `valid()` 改成：

```go
func (k AssetSourceKind) valid() bool {
	switch k {
	case AssetSourceCreative, AssetSourceUpload, AssetSourceExternal, AssetSourceMiyun:
		return true
	}
	return false
}
```

- [ ] **Step 6: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestMiyunIsItsOwnSourceKind && go test ./internal/systems/insights/
```

预期：两条都 `ok`。

- [ ] **Step 7: 契约与前端类型**

在 `api/openapi/insights-v1.yaml` 中，把所有 `source_kind` 的 `enum: [creative, upload, external]` 改成 `enum: [creative, upload, external, miyun]`（`InsightAsset`、`IndexInsightAssetBody`、以及 `GET /assets` 的 `source_kind` 查询参数都要改）。

在 `src/data/api.ts` 中，把 `ApiAssetSourceKind` 的定义改成：

```ts
export type ApiAssetSourceKind = 'creative' | 'upload' | 'external' | 'miyun'
```

- [ ] **Step 8: 前端类型检查**

```bash
cd /d/project/cookies-integration-mvp && npx tsc --noEmit -p tsconfig.json
```

预期：无输出。若某处 `switch (source_kind)` 因为新增分支报穷尽性错误，把 `'miyun'` 那一支补上，文案写「米云」。

- [ ] **Step 9: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add migrations/insights/20260813120000_insight_asset_source_miyun.up.sql migrations/insights/20260813120000_insight_asset_source_miyun.down.sql internal/systems/insights/assets.go internal/systems/insights/assets_test.go api/openapi/insights-v1.yaml src/data/api.ts && git commit -m "feat(insights): 米云从 external 里拆出来独立成一档来源"
```

---

### Task 12: 前端接上台账

**Files:**
- Modify: `src/data/api.ts`（`ApiInsightAsset`、`ApiInsightAssetFilter`、`listInsightAssets`，新增 `promoteInsightAsset` / `returnInsightAssetToLedger`）
- Modify: `src/components/insight/assets/OverviewView.tsx`（取数明确带 role）
- Modify: `src/components/insight/assets/AssetsPage.tsx`（新增 `ledger` 视图）
- Create: `src/components/insight/assets/LedgerView.tsx`
- Modify: `src/data/navigation.ts`（素材入口的 views 加「台账」）
- Test: `test/insight-ledger.test.ts`

**Interfaces:**
- Consumes: Task 6 的 HTTP 契约、Task 11 的 `ApiAssetSourceKind`。
- Produces:
  - `ApiAssetRole = 'ledger' | 'analysis'`
  - `ApiInsightAsset.role: ApiAssetRole`
  - `ApiInsightAssetFilter.roles?: ApiAssetRole[]`、`.cursor?: string`、`.query?: string`
  - `api.listInsightAssets(projectId, filter) => Promise<{ items: ApiInsightAsset[]; next_cursor?: string }>`
  - `api.promoteInsightAsset(projectId, assetId, body) => Promise<ApiInsightAsset>`
  - `api.returnInsightAssetToLedger(projectId, assetId, body) => Promise<ApiInsightAsset>`
  - `ledgerSourceLabel(kind: ApiAssetSourceKind): string`（导出自 `LedgerView.tsx`）

- [ ] **Step 1: 写失败的测试**

创建 `test/insight-ledger.test.ts`：

```ts
import assert from 'node:assert/strict'
import test from 'node:test'
import { ledgerSourceLabel } from '../src/components/insight/assets/LedgerView.tsx'

/**
 * 台账清单上每一条都要说清出处。「external」和「miyun」在这里必须是两个词——
 * 界面上的「外部素材」指的是竞品参照证据，那些永远不能投；米云的能投。
 * 两者共用一个标签的时候，看的人会以为米云素材也不能投。
 */
test('四种来源各有各的中文名', () => {
  assert.equal(ledgerSourceLabel('creative'), '创意产出')
  assert.equal(ledgerSourceLabel('upload'), '手工上传')
  assert.equal(ledgerSourceLabel('external'), '外部导入')
  assert.equal(ledgerSourceLabel('miyun'), '米云采集')
})

test('来源读不出来时不编一个名字', () => {
  assert.equal(ledgerSourceLabel('brand-new' as never), '来源未知')
})
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && npx tsx --test test/insight-ledger.test.ts
```

预期：FAIL，`Cannot find module '../src/components/insight/assets/LedgerView.tsx'`。

- [ ] **Step 3: 扩前端契约类型**

在 `src/data/api.ts` 中：

**3a.** 在 `ApiInsightAsset` 定义之前加：

```ts
/**
 * 素材身份。ledger 是台账——平台里所有素材的账本，绝大多数永远不会投流；
 * analysis 是分析对象——真投过、有花费、要跑归因的成品。
 * 四个队列和红点一律只数 analysis，否则几千条台账会把它们全部灌满。
 */
export type ApiAssetRole = 'ledger' | 'analysis'
```

**3b.** 在 `ApiInsightAsset` 的 `project_id` 之后加一行：

```ts
  role: ApiAssetRole
```

**3c.** 把 `ApiInsightAssetFilter` 改成：

```ts
export type ApiInsightAssetFilter = {
  statuses?: ApiAnalysisStatus[]
  assetTypes?: ApiInsightAssetType[]
  sourceKinds?: ApiAssetSourceKind[]
  /** 不传等于只看分析对象。台账要显式要，免得谁忘了传就拉回来几千条。 */
  roles?: ApiAssetRole[]
  lineageId?: string
  /** 上一页返回的 next_cursor，不透明串。 */
  cursor?: string
  /** 按标题模糊搜。 */
  query?: string
  limit?: number
}
```

**3d.** 把 `listInsightAssets` 改成：

```ts
  listInsightAssets: (projectId: string, filter: ApiInsightAssetFilter = {}) => {
    const search = new URLSearchParams({ limit: String(filter.limit ?? 100) })
    filter.statuses?.forEach(status => search.append('status', status))
    filter.assetTypes?.forEach(assetType => search.append('asset_type', assetType))
    filter.sourceKinds?.forEach(sourceKind => search.append('source_kind', sourceKind))
    filter.roles?.forEach(role => search.append('role', role))
    if (filter.lineageId) search.set('lineage_id', filter.lineageId)
    if (filter.cursor) search.set('cursor', filter.cursor)
    if (filter.query) search.set('q', filter.query)
    return request<{ items: ApiInsightAsset[]; next_cursor?: string }>(
      `${insightProjectPath(projectId)}/assets?${search.toString()}`,
    )
  },
```

**3e.** 在 `indexInsightAsset` 之后加：

```ts
  // 把一条台账素材拉进分析。台账里绝大多数素材永远不会投流，
  // 所以这一步必须有人点——自动往里拉只会把四个队列重新灌满。
  promoteInsightAsset: (
    projectId: string, assetId: string,
    body: { expected_version: number; reason: string },
  ) => request<ApiInsightAsset>(`${insightAssetPath(projectId, assetId)}:promote`, 'POST', body),
  // 把拉错的素材退回台账。已经和广告对象对上号的会被后端拒掉——
  // 那意味着它有花费，退回去等于把数据藏起来。
  returnInsightAssetToLedger: (
    projectId: string, assetId: string,
    body: { expected_version: number; reason: string },
  ) => request<ApiInsightAsset>(`${insightAssetPath(projectId, assetId)}:return-to-ledger`, 'POST', body),
```

- [ ] **Step 4: 写台账视图**

创建 `src/components/insight/assets/LedgerView.tsx`：

```tsx
import { useCallback, useEffect, useState } from 'react'
import { CircleAlert } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiAssetSourceKind, type ApiInsightAsset } from '../../../data/api'
import { isoDate } from '../analysis/format'

/**
 * 台账。平台里所有素材的账本——创意做的每一张图、每一版剪辑、每一段配音都在这儿，
 * 绝大多数永远不会拿去投流。
 *
 * 它和隔壁「总览」的区别不是筛选条件不同，是**回答的问题不同**：总览问「这几条素材
 * 还差什么才能进复盘」，台账问「我们手上到底有些什么」。所以这一屏没有队列、没有红点，
 * 只有一个搜索框和一条条记录——它不催人做事。
 *
 * 分页是游标不是页码：台账和平台素材库一样大，翻到第 50 页要数过前面 4900 行。
 */
const sourceLabels: Record<ApiAssetSourceKind, string> = {
  creative: '创意产出',
  upload: '手工上传',
  external: '外部导入',
  miyun: '米云采集',
}

export function ledgerSourceLabel(kind: ApiAssetSourceKind): string {
  return sourceLabels[kind] ?? '来源未知'
}

export function LedgerView({ onPromoted }: { onPromoted: () => void }) {
  const { currentProject } = useProject()
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<ApiInsightAsset[]>([])
  const [cursor, setCursor] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  // 每改一次搜索词就加一，让下面那个 effect 从头取一遍而不是接着上一页翻。
  const [searchKey, setSearchKey] = useState(0)

  const load = useCallback(async (nextCursor: string, append: boolean) => {
    if (!currentProject.id) return
    setLoading(true)
    setError('')
    try {
      const page = await api.listInsightAssets(currentProject.id, {
        roles: ['ledger'], query: query.trim() || undefined,
        cursor: nextCursor || undefined, limit: 50,
      })
      setItems(previous => append ? [...previous, ...page.items] : page.items)
      setCursor(page.next_cursor ?? '')
    } catch {
      setError('台账读不出来。刷新一次，还不行就是后端没起来。')
    } finally {
      setLoading(false)
    }
  }, [currentProject.id, query])

  useEffect(() => { void load('', false) }, [currentProject.id, searchKey])

  const promote = async (asset: ApiInsightAsset) => {
    try {
      await api.promoteInsightAsset(currentProject.id, asset.id, {
        expected_version: asset.version, reason: '这条要投了，拉进分析。',
      })
      // 拉进分析之后它就不在台账里了，从当前这一页摘掉，不必重取。
      setItems(previous => previous.filter(item => item.id !== asset.id))
      onPromoted()
    } catch {
      setError('拉进分析没成功。多半是这条素材刚被别人改过，刷新后再试。')
    }
  }

  return <div className="assets-ledger">
    <div className="assets-ledger-search">
      <input
        type="search"
        value={query}
        placeholder="按标题搜，比如「春节」"
        onChange={event => setQuery(event.target.value)}
        onKeyDown={event => { if (event.key === 'Enter') setSearchKey(key => key + 1) }}/>
      <button type="button" onClick={() => setSearchKey(key => key + 1)}>搜索</button>
    </div>

    {error ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>{error}</span></div> : null}

    <ul className="assets-ledger-list">
      {items.map(asset => <li key={asset.id}>
        <div>
          <strong>{asset.title}</strong>
          <small>{ledgerSourceLabel(asset.source_kind)} · {isoDate(new Date(asset.created_at))}</small>
        </div>
        <button type="button" onClick={() => void promote(asset)}>拉进分析</button>
      </li>)}
    </ul>

    {items.length === 0 && !loading
      ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
        <small>台账是空的</small>
        创意做出来的素材会自动记进这里。一条都没有，说明这个 Project 还没产出过素材，
        或者素材是在台账建起来之前入的库——那种要跑一次 cookies-maintain backfill-ledger 补。
      </span></div>
      : null}

    {cursor
      ? <button type="button" disabled={loading} onClick={() => void load(cursor, true)}>
        {loading ? '加载中…' : '加载更多'}
      </button>
      : null}
  </div>
}
```

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && npx tsx --test test/insight-ledger.test.ts
```

预期：2 个测试全过。

- [ ] **Step 6: 总览明确只数分析对象**

在 `src/components/insight/assets/OverviewView.tsx` 的 `api.listInsightAssets(currentProject.id, {})`（约 :51）改成：

```tsx
        api.listInsightAssets(currentProject.id, { roles: ['analysis'] }),
```

在 `src/components/insight/assets/AssetsPage.tsx` 的 `void api.listInsightAssets(currentProject.id, {})`（:88）改成：

```tsx
    void api.listInsightAssets(currentProject.id, { roles: ['analysis'] })
```

> 后端默认值已经是 `analysis`，这里显式写出来是给读代码的人看的：这两处数出来的是队列和红点，绝不能混进台账。

- [ ] **Step 7: 挂上视图**

在 `src/components/insight/assets/AssetsPage.tsx` 中：

**7a.** 把 `AssetsView` 改成：

```tsx
export type AssetsView = 'overview' | 'ledger' | 'intake' | 'features' | 'similar' | 'external'
```

**7b.** 在 `headings` 里 `overview` 之后加：

```tsx
  ledger: {
    label: 'LEDGER',
    title: '平台里一共有哪些素材',
    lead: '创意做的每一张图、每一版剪辑都记在这儿。绝大多数永远不会投流——它们不进队列、不催你做事。真要投的那几条，点「拉进分析」。',
  },
```

**7c.** 在 import 里加：

```tsx
import { LedgerView } from './LedgerView'
```

**7d.** 在 `{view === 'similar' ? <SimilarView/> : null}` 之前加：

```tsx
        {view === 'ledger' ? <LedgerView onPromoted={() => setReloadKey(key => key + 1)}/> : null}
```

**7e.** 在 `src/data/navigation.ts` 里找到素材入口的 `views` 数组，把 `'台账'` 插在 `'总览'` 之后。

**7f.** 找到把中文视图名映射成 `AssetsView` 的地方（`AssetsPage` 的调用方，在 `src/components/Pages.tsx` 里），把 `'台账'` 映到 `'ledger'`。若那里用的是按索引取值的写法，插入位置跟着 `views` 数组走即可，不必改映射代码。

- [ ] **Step 8: 血缘接上**

在 `src/components/insight/assets/AssetDetail.tsx` 中，组件体开头加：

```tsx
  const [lineage, setLineage] = useState<ApiInsightAsset[]>([])

  // listInsightAssetLineage 这个接口一直在，只是从来没人调过。
  // 同一条创意改了三版和三条不同创意，在别处是靠 lineage_id 分的——
  // 详情页不显示的话，人只能靠标题猜。
  useEffect(() => {
    let active = true
    void api.listInsightAssetLineage(asset.project_id, asset.id)
      .then(page => { if (active) setLineage(page.items) })
      .catch(() => { if (active) setLineage([]) })
    return () => { active = false }
  }, [asset.project_id, asset.id])
```

在该组件返回的 JSX 末尾（最后一个 `</...>` 之前）加：

```tsx
    {lineage.length > 1 ? <div className="asset-lineage">
      <span className="section-label">这条创意的版本</span>
      <ol>
        {lineage.map(item => <li key={item.id} className={item.id === asset.id ? 'current' : ''}>
          第 {item.revision} 版 · {item.title}
        </li>)}
      </ol>
    </div> : null}
```

补上 import：`useEffect`、`useState`（从 `react`）、`api` 与 `ApiInsightAsset`（从 `../../../data/api`），已有的不重复加。

> `listInsightAssetLineage` 从来没被调用过，它的返回类型要现场确认一次：
> ```bash
> cd /d/project/cookies-integration-mvp && grep -n "listInsightAssetLineage" -A 3 src/data/api.ts
> ```
> 若它返回的是裸数组而不是 `{ items }`，把上面的 `.then(page => ...setLineage(page.items))` 改成 `.then(items => ...setLineage(items))`。
>
> `AssetDetail` 里表示当前素材的 prop 若不叫 `asset`（可能叫 `value` / `item`），按现有的名字改这两处引用。

- [ ] **Step 9: 全量前端验证**

```bash
cd /d/project/cookies-integration-mvp && npx tsc --noEmit -p tsconfig.json && npm test
```

预期：`tsc` 无输出；`npm test` 至少 302 passed / 0 failed（基线 300 加本任务的 2 个）。

- [ ] **Step 10: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add src/data/api.ts src/data/navigation.ts src/components/Pages.tsx src/components/insight/assets/LedgerView.tsx src/components/insight/assets/AssetsPage.tsx src/components/insight/assets/OverviewView.tsx src/components/insight/assets/AssetDetail.tsx test/insight-ledger.test.ts && git commit -m "feat(web): 素材入口加台账视图，队列只数分析对象"
```

---

### Task 13: 收尾验证

**Files:** 无新增，只跑验证。

- [ ] **Step 1: 后端全量**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go vet ./... && go test ./internal/... ./cmd/...
```

预期：全 `ok`，无 vet 告警。

- [ ] **Step 2: 前端全量**

```bash
cd /d/project/cookies-integration-mvp && npx tsc --noEmit -p tsconfig.json && npm test && npm run build
```

预期：`tsc` 与 `build` 无错，`npm test` 0 failed。

- [ ] **Step 3: 迁移能上能下**

```bash
cd /d/project/cookies-integration-mvp && go run ./cmd/cookies-migrate up && go run ./cmd/cookies-migrate down 2 && go run ./cmd/cookies-migrate up
```

预期：三条都成功。若 `cookies-migrate` 的参数不是 `up` / `down N`，先跑 `go run ./cmd/cookies-migrate` 看用法，按它的写法来。

> 这一步需要能连上的 MySQL。连不上就跳过，并在提交信息里写明「迁移未在本地验证」。

- [ ] **Step 4: 回填一次并核对**

```bash
cd /d/project/cookies-integration-mvp && go run ./cmd/cookies-maintain backfill-ledger
```

预期：输出「补了 N 条台账素材」。再跑一次，N 应该变成 0——重复跑无害是这个命令的设计。

- [ ] **Step 5: 提交**

```bash
cd /d/project/cookies-integration-mvp && git status --short
```

若有未提交的改动，逐个 `git add <路径>` 后提交；**不要用 `git add -A`**（工作区里还有上一个任务的大量改动）。

---

## 验收（对照 spec 第七节）

| # | spec 的验收条件 | 由哪个 Task 满足 |
|---|---|---|
| 1 | 台账与分析对象在数据上可区分 | Task 1、2 |
| 2 | 队列与红点只数分析对象 | Task 2（服务层默认值）、Task 12 Step 6（前端显式） |
| 3 | 台账素材不得有特征 | Task 4 |
| 4 | 拉进分析是显式动作，需要 write 权限 | Task 3、6 |
| 5 | 退回台账有前置条件 | Task 3（判据改为「未对上号」，见「相对 spec 的两处偏离」） |
| 6 | 存量行为不变 | Task 1（`DEFAULT 'analysis'`）、Task 13 Step 1-2 |
| 7 | 平台六条入库路径的产物自动进台账 | Task 8（收录点 4 处，derived 2 处按设计排除） |
| 8 | 收录幂等 | Task 1（`uq_insight_assets_ledger_object`）、Task 10（回填可重复跑） |
| 9 | 台账查得动：游标 + 搜索 | Task 5、6、12 |
| 10 | 米云不再叫「外部」 | Task 11 |

**不在本计划内**：spec 基建四（缩略图）另起一份计划——它要动 `internal/platform/assets` 的派生物流水线，是另一个子系统。台账没有缩略图时按类型显示图标，不阻塞上面任何一条。
