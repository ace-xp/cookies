# Strategy → 品牌广告 Brief 连续性修复：技术实现方案与反方评审

> 状态：P0 已实现，待测试服务器部署验证
> 日期：2026-08-12
> 范围：Strategy 已批准品牌视频策略交接到 Creative 品牌广告工作台
> 不包含：手动 PDF Brief 解析算法、品牌广告生成模型升级、视频与声音生产能力重构

## 1. 结论

采用“**Creative 确定性投影 + 一次显式接受 + 品牌方向门禁**”方案：

1. Strategy 继续只发布不可变 `StrategyPackage + CreativeHandoff + Route`，不写 Creative 状态。
2. 用户点击“确认策略输入并开始创作”后，Creative 从冻结 Intake 确定性生成自己的 `BrandBriefReview`，执行本地校验并记录确认人、时间和精确输入血缘。
3. 投影无 planning blocker 时，不调用 Brief 分析模型，也不要求用户重新填写或重新确认同一批策略事实；直接进入品牌方向生成与选择。
4. Strategy 来源的品牌视频 Task 必须绑定已确认 `BrandBriefReference` 和 `CreativeDirectionVersion`；禁止再创建 `waiting_for_input` 空任务。
5. Logo、商品图、人物、音乐和声音授权，以及生产参数，继续通过 generation/production readiness 独立确认；它们不得触发完整 Brief 重分析。
6. 手动 PDF/DOCX/Markdown 来源保持现有“解析 → 编辑 → 确认 Brief”流程。

本方案不是把 Strategy 的批准状态直接复制成 Creative 的确认状态。Creative 仍拥有本地派生对象、校验、版本、Hash 和确认记录；消除的是重复模型分析和重复填表，而不是 Creative 的领域边界。

## 2. 问题与证据

当前页面宣称“品牌策略输入已就绪，可直接进入创作”，但真实调用链为：

```text
Strategy CTA
  → 创建 ready CreativeIntake
  → 品牌页立即 createBrandFilmTaskFromIntake（无 DirectionID）
  → newBrandFilmDraft
  → stage=waiting_for_input
  → blocker=brief_analysis_confirmation
  → 工作台要求解析/重新解析 Brief
```

直接原因：

- `src/features/strategy/CreativeTaskPlanner.tsx` 交接后把 Intake ID 深链到品牌广告。
- `src/components/SpecializedPages.tsx` 恢复 Intake 后直接调用 `createBrandFilmTaskFromIntake`。
- `internal/systems/creative/service.go` 允许 Strategy 来源的 `brand_video` 在没有 `DirectionID` 时创建 Task。
- `internal/systems/creative/brand_film.go` 对该 Task 一律创建 `BrandFilmWaitingBrief` 草稿。
- `src/components/BrandFilmWorkspace.tsx` 因此显示“Brief 分析与事实确认”和“解析 Brief”。

仓库同时已经存在另一条正确但未被 Strategy 直达入口使用的链路：

```text
CreativeIntake
  → PrepareBrandBriefReview（确定性投影）
  → ConfirmBrandBriefReview
  → GenerateCreativeDirections
  → ConfirmCreativeDirection
  → CreateVideoTask(DirectionID)
  → buildStrategyBrandFilmDraft
  → stage=concept_confirmed
```

因此本次以收敛编排和强化门禁为主，不新建第三套 Brief 或品牌任务模型。

## 3. 目标与非目标

### 3.1 目标

- Strategy 来源无 planning blocker 时，从点击交接到看到品牌方向，不发生 Brief 模型解析。
- 新建 Strategy 品牌 Task 初始状态不再是 `waiting_for_input`，而是绑定已确认 Brief 和 Direction 的 `concept_confirmed`。
- 所有对象可追溯到相同 `input_identity_hash`、Package Hash、Handoff Hash、Route、Brief Revision/Hash 和 Direction Revision/Hash。
- 刷新、重复点击、接口超时重试和跨页面恢复均不会创建重复 Brief、Direction Batch 或 Task。
- 既有空 Task 可安全升级或明确进入人工处理，不丢失用户已经产生的内容。
- 手动 Brief 链路行为不变。

### 3.2 非目标

- 不让 Strategy 生成 Concept、视觉语言、脚本、分镜或 Prompt。
- 不取消品牌方向的人工选择。
- 不因上游批准而跳过素材权利、宣称使用范围、生成或交付确认。
- 不原地修改已冻结 `strategy-creative-handoff/v1`、`creative-intake/v3` 或共享状态机字段语义。
- 不自动采用新发布的 StrategyPackage 覆盖现有 Intake 或 Task。

## 4. 设计原则

1. **一次用户意图，一条领域命令**：点击“确认策略输入并开始创作”是显式写操作；GET、页面恢复和列表读取不得确认 Brief、生成方向或创建 Task。
2. **确定性数据不用模型重新理解**：目标、受众、主张、宣称、约束和 Route 从冻结输入映射；模型只从 Direction 生成开始参与。
3. **Strategy 批准不等于 Creative 盲信**：Creative 重验 Project、Route、双 Hash、`input_identity_hash`、Schema 和本地 planning readiness。
4. **事实确认与生产确认分离**：Brief planning gate、素材 generation gate 和交付 production gate 不互相冒充。
5. **不可变血缘优先**：任何自动恢复均按精确版本和 Hash，禁止按“最新策略”读取。
6. **兼容迁移不覆盖用户工作**：只有能证明为空的旧 Task 才允许归档替换；已产生用户内容的 Task 保持不变。

## 5. 目标流程与状态机

### 5.1 Strategy 来源

```text
已批准 StrategyPackage / Handoff / brand_video Route
  → 用户点击交接
  → ready CreativeIntake（不可变）
  → POST brand-workflow:prepare
      ├─ 校验失败或 planning blocker
      │    → BrandBriefReview.draft
      │    → 显示“补充策略输入”，不创建 Task
      └─ 无 planning blocker
           → BrandBriefReview.confirmed（记录当前用户）
           → 恢复已有 Direction Batch，或允许生成候选
  → 用户确认一个 Direction
  → POST create-video-task（必须携带 DirectionID）
  → BrandFilmDraft.stage=concept_confirmed
  → 剧本与分镜
  → 素材/权利 generation gate
  → 生成、声音、质检与交付
```

### 5.2 手动文档来源

```text
上传 PDF / DOCX / Markdown
  → manual CreativeIntake
  → create-video-task（允许无 DirectionID）
  → BrandFilmDraft.stage=waiting_for_input
  → 模型解析、编辑、素材确认、人工确认 Brief
  → 品牌方向、剧本与后续生产
```

### 5.3 门禁矩阵

| 条件 | 可以查看/编辑 Brief | 可以生成 Direction | 可以创建 Task | 可以调用真实生成 | 可以交付 |
| --- | --- | --- | --- | --- | --- |
| Strategy 投影有 planning blocker | 是 | 否 | 否 | 否 | 否 |
| Strategy Brief confirmed，素材未确认 | 只读查看/显式修订 | 是 | 是 | 按 Route 资产要求决定 | 否 |
| Direction 未确认 | 是 | 是 | 否 | 否 | 否 |
| Direction confirmed，generation blocker 存在 | 是 | 是 | 是 | 否 | 否 |
| generation ready，production blocker 存在 | 是 | 是 | 是 | 是 | 否 |
| production ready | 是 | 是 | 是 | 是 | 是 |

## 6. API 方案

### 6.1 新增统一准备命令

```http
POST /api/creative/v1/projects/{project_id}/creative-intakes/{intake_id}/brand-workflow:prepare
Idempotency-Key: strategy-brand-prepare-sha256-{64位十六进制摘要}
```

请求：

```json
{
  "expected_input_identity_hash": "sha256:...",
  "selected_route_id": "route_brand_video",
  "accept_strategy_projection": true
}
```

约束：

- 只接受 `source=strategy_package`、`status=ready`、`contract_version=creative-intake/v3` 和选中的 `brand_video` Route。
- `accept_strategy_projection=true` 表示当前用户显式接受 Creative 对冻结输入的确定性派生，不表示接受后续模型生成内容。
- 命令不得调用 LLM/VLM，不得生成 Direction，不得创建 Task。
- 如果投影存在 blocker，保存或恢复 `BrandBriefReview.draft`，不得确认。
- 如果无 blocker，幂等确认 `BrandBriefReview`；`confirmed_by/confirmed_at` 记录执行该命令的用户和时间。
- 已确认 Brief、已有 Batch、已有 Direction 或已有 Task 必须返回当前资源，不重复创建。

响应：

```json
{
  "contract_version": "creative-strategy-brand-workflow/v1",
  "mode": "direction_ready",
  "intake_id": "creativeintake_...",
  "input_identity_hash": "sha256:...",
  "brand_brief": {},
  "latest_direction_batch": null,
  "confirmed_direction": null,
  "task": null,
  "issues": [],
  "next_action": "generate_directions"
}
```

`mode`：

- `brief_review_required`：存在 planning blocker，用户需补充或回到 Strategy 创建新版本。
- `direction_ready`：Brief 已确认，可生成或恢复方向。
- `direction_selection_required`：已有 ready Batch，选择方向。
- `task_ready`：已有合法 Task，直接恢复。
- `legacy_task_upgrade_required`：发现不可自动覆盖的旧直达 Task。

`issues` 使用稳定 `code/stage/path/message/source`，不把自由文本当作前端分支条件。现有 `BrandBriefReview.blockers/warnings: string[]` 暂时保留兼容；新响应由 validator 同步生成结构化问题。

### 6.2 新增只读恢复查询

```http
GET /api/creative/v1/projects/{project_id}/creative-intakes/{intake_id}/brand-workflow
```

返回与 prepare 命令相同的 workflow result，但不得创建、确认、升级或生成任何资源。刷新、旧链接恢复和浏览器前进/后退只调用该 GET；如果返回“尚未准备”，页面显示显式“确认策略输入并开始创作”按钮，由用户点击后调用 prepare POST。

### 6.3 保留现有原子 API

以下接口继续保留，供管理页、调试和手动修订使用：

- `brand-brief:prepare`
- `brand-brief:confirm`
- Direction Batch generate/get
- Direction confirm
- `create-video-task`

统一准备命令在服务层编排已有投影与确认能力，不复制映射规则。它是 UI 的 canonical 入口，原子 API 不是前端默认多步编排。

### 6.4 收紧创建 Task 的服务端规则

`POST .../{intake_id}:create-video-task` 增加如下规则：

```text
if intake.source == strategy_package && route == brand_video:
  require direction_id
  require BrandBriefReview.confirmed
  require Direction.confirmed
  require Brief/Direction/Batch/Intake/Route/input_identity_hash 全部匹配
else if manual brand film:
  allow direction_id omitted
```

错误码：

- `strategy_brand_brief_required`
- `strategy_brand_direction_required`
- `strategy_brand_lineage_mismatch`
- `strategy_brand_legacy_task_requires_review`
- `strategy_brand_projection_blocked`

HTTP 使用现有错误映射：非法状态或血缘冲突返回 409，输入格式错误返回 400，权限/Project 错误沿用 403/404。

### 6.5 OpenAPI

更新 `api/openapi/creative-v1.yaml`：

- 新增 prepare result、request、structured issue schema。
- 新增无副作用的 workflow GET，并在测试中证明不会写入资源。
- 明确 Strategy `brand_video` 创建 Task 必须提供 confirmed Direction。
- 明确 manual Brand Film 仍可无 Direction 创建。
- 明确 prepare 命令幂等、无 Provider 副作用、不会静默选择最新 Package。

## 7. 后端领域实现

### 7.1 新增编排服务

已新增 `internal/systems/creative/strategy_brand_workflow.go`：

```go
type PrepareStrategyBrandWorkflowRequest struct {
    ExpectedInputIdentityHash string `json:"expected_input_identity_hash"`
    SelectedRouteID           string `json:"selected_route_id"`
    AcceptStrategyProjection  bool   `json:"accept_strategy_projection"`
}

func (s Service) PrepareStrategyBrandWorkflow(...) (StrategyBrandWorkflowResult, error)
```

执行顺序：

1. 校验 Actor、Project、scope。
2. 读取 Intake 并核对 source/status/contract/input identity/selected Route。
3. 若 Intake 已有 Task，先分类为合法 Task、可安全归档替换的旧 Task或需人工处理的旧 Task。
4. 调用唯一的 `projectBrandBriefDocument` 映射，不复制 JSON 解析。
5. 通过精确 Package ID/version/hash 补充当前 v1 Handoff 未携带的品牌名、产品名、卖点和证明点。
6. 运行 `validateBrandBriefDocument`，同时产出兼容字符串和结构化 issue。
7. 有 blocker：保存/恢复 draft 并返回 `brief_review_required`。
8. 无 blocker且用户显式接受：确认本地 Brand Brief。
9. 查询该 Brief 精确 Revision/Hash 对应的 Direction Batch、confirmed Direction 和 Task，返回下一动作。

`PrepareBrandBriefReview` 当前会在已有 draft 上合并缺失字段。统一命令必须保留用户编辑：只升级仍等于旧投影值的字段，不覆盖真实人工修订。

### 7.2 当前 Handoff v1 的数据缺口

`strategy-creative-handoff/v1.creative_view.product_and_offer` 只有产品引用、活动机制、Offer 和落地页，不携带品牌名、产品名、卖点、证明点和使用场景。当前 `PrepareBrandBriefReview` 通过精确 `StrategyPackageReader` 补齐这些事实。

P0 保留这一只读调用，但必须满足：

- 使用 Intake 中冻结的 Package ID/version/hash，不读“latest”。
- 读取结果再次核对 Package/Handoff Hash。
- 投影成功后事实固化到 Creative 自有 Brand Brief；后续方向、Task 和工作台恢复不再依赖 Strategy 在线。
- Strategy 不可用时返回可重试错误，不回退模型猜测、默认值或空任务。

P1 发布向后兼容的新 Handoff 版本，将品牌和产品事实纳入完整快照，并提供 v1/v2 双读迁移。P1 不阻塞本次断裂修复，但必须单独立项关闭“首次 Creative 投影依赖 Strategy 在线”的历史契约债务。

### 7.3 Brief 确认语义

- `confirmed_by` 记录点击“确认策略输入并开始创作”的用户，而不是 Strategy 审批人或系统账号。
- `confirmed_at` 使用服务端时间。
- `ContentHash` 只对 Brand Brief document 计算；确认元数据不改变内容 Hash。
- 重放返回同一 confirmed revision，不重复递增。
- 用户后续修改目标、受众、主张等策略事实时，必须显式创建新的 draft revision，并使依赖旧 Brief 的 Direction/Task 进入 superseded 或修订流程；不得原地改 confirmed 文档。

### 7.4 Task 创建与物化

新 Task 统一复用现有 `buildStrategyBrandFilmDraft`，初始值必须满足：

- `source_type=strategy_handoff`
- `stage=concept_confirmed`
- `BriefAnalyses[0].confirmed=true`
- `SelectedConceptID=confirmed Direction ID`
- `PlanningReady=true`
- Package/Handoff/Brief/Direction/Route Hash 齐全
- 初始 blocker 只能是后续生产门禁，例如 `production_plan_confirmation`、`prompt_package`，不能是 `brief_analysis_confirmation`

删除 Strategy 新链路对 `newBrandFilmDraft` 的调用；该构造器仅用于 manual/fixture 来源。

### 7.5 事务与并发

无需为新主链增加业务表，但需要保证：

- Brand Brief 创建/确认继续使用 revision CAS 和唯一 `input_identity_hash`。
- Direction Batch 使用现有幂等身份；同一 confirmed Brief revision/hash 同时只能有一个当前生成批次。
- Task 继续使用 Direction lineage key；数据库允许同一 Intake 的多个明确方向，服务端按 lineage key 幂等恢复同一正式 Task。
- `brand-workflow:prepare` 即使在“创建 draft 成功、确认响应超时”时也可安全重放。
- 两个用户同时点击时，一个成功确认，另一个读取并返回相同 confirmed revision，不返回伪冲突。

如现有 repository 无法把“创建后立即确认”包装为单事务，P0 可采用可恢复的两步 CAS；不得用分布式锁掩盖。后续可增加 `ConfirmProjectedBrandBrief` repository 方法优化事务边界。

## 8. 既有错误 Task 的兼容与修复

### 8.1 分类

| 类别 | 判定 | 处理 |
| --- | --- | --- |
| A：合法策略 Task | 已绑定 confirmed Brief + Direction，BrandFilm 已进入 `concept_confirmed` 或更后阶段 | 原样恢复 |
| B：空旧 Task | Strategy 来源；Direction 为空；BrandFilm 为 `waiting_for_input`；无 analysis/concept/plan/generation/audio/quality/delivery | 事务性归档旧 Task，再按 confirmed Direction 创建正式 Task |
| C：用户已修改旧 Task | 存在 Brief analysis、人工编辑、方向、分镜、生成、声音或交付任一产物 | 禁止自动覆盖，提示保留旧 Task 或从新 Intake 开始 |
| D：血缘异常 | Intake/Route/Hash/Project 任一不匹配 | 409，人工排查 |

### 8.2 归档替换命令

新增内部服务能力 `ReplaceEmptyLegacyStrategyBrandTask`，由确认 Direction 后的 Task 创建服务调用，不暴露为普通页面按钮。它必须在一个数据库事务中：

1. 锁定该 Intake 下无 Direction 的 active legacy Task，并 CAS 校验 Task version 和 VideoDraft revision。
2. 再次验证 B 类空 Task 谓词。
3. 将旧 Task 标记为 `archived`，保留原 Task ID、创建人、时间和空 Draft 供审计。
4. 使用 confirmed Direction identity、lineage key 和 `buildStrategyBrandFilmDraft` 创建新的正式 Task。
5. 提交事务后返回新 Task；任一步失败则旧 Task 与新 Task 均不发生变化。
6. 写审计事件 `creative.brand_task.empty_legacy_replaced.v1`，同时记录 old/new Task ID。

仓库已在 `20260723181100_creative_task_directions` 移除“一 Intake 一 Task”唯一约束，以支持同一 Intake 的多个明确方向。归档替换比原位改写身份更符合不可变血缘：旧错误 Task 可审计，新 Task 从创建时就绑定正确 Direction。事务内仍需检查同一 lineage key 是否已有 Task，避免重复创建。

### 8.3 一次性审计

上线前提供只读审计命令或 SQL 报表，统计 A/B/C/D 数量和 Task ID。迁移只处理 B 类；C/D 类输出清单，不做批量覆盖。不得用模糊 JSON 文本匹配直接更新生产数据。

## 9. 前端实现

### 9.1 Strategy 交接页

`src/features/strategy/CreativeTaskPlanner.tsx`：

- CTA 改为“确认策略输入并开始创作”。
- 同一个点击处理器先创建/恢复 Intake，再调用 `prepareStrategyBrandWorkflow`；成功后才深链品牌广告。页面挂载和导航 effect 不得代替用户执行 prepare。
- URL 明确携带 `intake_id`，不再用同一 `activeTaskId` 猜测资源类型。
- 文案说明“不会重新解析已冻结策略；品牌方向仍需选择，素材权利在生产前确认”。

### 9.2 品牌广告入口

`src/components/SpecializedPages.tsx`：

- 删除 Strategy Intake 恢复后的 `createBrandFilmTaskFromIntake` 调用。
- 用户从品牌 Hub 点击某个策略时调用 `prepareStrategyBrandWorkflow`；通过 Strategy CTA 到达或刷新页面时调用只读 `getStrategyBrandWorkflow` 恢复。
- 按 `mode/next_action` 恢复 Brief 补充、Direction 生成中、Direction 选择、Task 或 legacy 处理页。
- 将 `setBrandIntake(value)` 补回正确分支；当前代码定义了 Direction Gate，却没有在 Strategy 恢复路径中写入该状态。
- `startBrandIntake` 与 Strategy 深链共用同一函数，禁止维护两套入口编排。
- Direction 确认后继续调用带 `direction_id` 的 `createBrandVideoTaskFromDirection`，成功后立即 `onOpenBrandTask(task.id)`。

### 9.3 品牌工作台

`src/components/BrandFilmWorkspace.tsx`：

- Strategy 来源的 confirmed Brief 默认只读展示“继承自已批准策略”，提供来源、Revision 和 Hash 查看入口。
- 初始路由直接定位 `concept` 或 `storyboard`；已选 Direction 的 Task 定位 `storyboard`。
- Strategy 来源不显示“解析 Brief”作为主按钮。
- “创建 Brief 修订”必须明确提示会使旧 Direction 和下游产物失效。
- 素材区继续展示 Logo、商品图和授权状态；缺失时只在相应 generation/production gate 显示阻断。
- manual_document/fixture 继续显示现有解析和确认界面。

### 9.4 URL 资源身份

把品牌入口从“一个 ID，先当 Intake 读，失败后再当 Task 读”的异常驱动分支改为显式身份：

```text
/creative/video/brand?intake={intake_id}
/creative/video/brand?task={task_id}&stage={stage_id}
```

旧链接保留一版兼容解析，但不得在一次 404 后自动执行创建动作。

## 10. 数据、事件与可观测性

### 10.1 数据变化

- 主链复用 `creative_brand_brief_reviews`、Direction Batch/Direction、CreativeTask 和 VideoDraft 表。
- 不新增第三套 Brief 表。
- legacy 归档替换增加 repository 事务方法，不新增或恢复“一 Intake 一 Task”唯一约束。
- 如结构化 issue 暂不入库，可先作为 prepare response 的确定性派生字段；后续再以新契约版本持久化。

### 10.2 审计事件

- `creative.brand_brief.strategy_projection_prepared.v1`
- `creative.brand_brief.strategy_projection_confirmed.v1`
- `creative.brand_direction.confirmed.v1`（复用已有则不重复定义）
- `creative.brand_task.empty_legacy_replaced.v1`

事件携带 organization/project/intake/task、Package/Handoff/Brief/Direction refs、actor、request ID 和时间，不携带完整 Brief 正文或 Provider 密钥。

### 10.3 指标

- `creative_brand_workflow_prepare_total{result}`
- `creative_brand_brief_projection_total{status}`
- `creative_brand_brief_analysis_total{source}`
- `creative_brand_legacy_task_total{class}`
- 从 Strategy CTA 到 Direction 可见、到 Task 创建的耗时分布

告警：发布后默认交接链路中 `source=strategy_package` 的 `brand_brief_analysis_total` 应为 0；只有用户显式创建修订并要求重新分析时才允许增加。指标标签不得使用 Project、Intake 或 Task ID。

## 11. 测试方案

### 11.1 Go 单元测试

新增/调整：

1. 完整 Strategy Intake prepare 后生成 confirmed Brand Brief，不调用 Planner。
2. 投影存在 planning blocker 时保持 draft，不确认、不生成 Direction、不创建 Task。
3. 素材缺失只产生 generation/production warning/blocker，不阻断 Direction planning。
4. Strategy brand Task 缺 DirectionID 返回 `strategy_brand_direction_required`。
5. manual brand film 无 DirectionID 仍可创建 `waiting_for_input` Task。
6. Brief、Batch、Direction、Intake 或 Route Hash 任一不匹配时拒绝创建。
7. prepare 重放返回同一 Brief revision/hash。
8. 两个并发 prepare 只产生一个 confirmed revision。
9. B 类 legacy Task 被归档并创建新的 `concept_confirmed` Task；旧 Task 可审计且不被改写。
10. C 类 legacy Task 不被覆盖。
11. Strategy reader 不可用时返回可重试错误，不调用模型和默认值。

重点文件：

- `internal/systems/creative/brand_brief_review_test.go`
- `internal/systems/creative/direction_planning_test.go`
- `internal/systems/creative/service_test.go`
- 新增 `internal/systems/creative/strategy_brand_workflow_test.go`
- `internal/platform/httpserver/server_test.go`

### 11.2 Repository/MySQL 集成测试

- Brand Brief 创建/确认 CAS 和唯一 identity。
- legacy 空 Task 归档 + 新 Task 创建事务回滚。
- 并发 prepare、Direction confirm 和 Task create 不产生重复记录。
- Task lineage key、direction payload 和 BrandFilm source snapshot 同步写入。

### 11.3 前端单元测试

- Strategy CTA 和品牌 Hub 的显式点击调用 prepare，不调用 `createBrandFilmTaskFromIntake` 或 `analyzeBrandFilmBrief`；页面加载只调用 workflow GET。
- workflow GET 不产生 Brand Brief revision、确认、Direction Batch、Task 或审计写事件。
- `brief_review_required`、`direction_ready`、`direction_selection_required`、`task_ready` 和 legacy 模式渲染正确。
- 确认 Direction 后请求体包含 `direction_id/selected_route_id/channel`。
- manual PDF 仍创建 Task 并进入 Brief 分析。
- URL 明确区分 Intake 和 Task；旧链接只读恢复不触发写操作。

更新：

- `test/brand-film-api.test.ts`
- `test/brand-direction-generation.test.ts`
- `test/strategy-handoff-api.test.ts`

### 11.4 E2E

核心用例：

```text
确认 Strategy Brief
→ 批准 StrategyPackage
→ 选择 brand_video Route
→ 点击确认策略输入并开始创作
→ 看到 3 个品牌方向
→ 确认一个方向
→ 创建并打开 BrandFilm Task
→ 工作台 Brief 为 confirmed，阶段不回退
→ 刷新后状态不变
```

断言：

- 网络请求中没有 `brand-film:analyze-brief`。
- Task source snapshot 含完整 Package/Handoff/Brief/Direction/Route Hash。
- 初始 BrandFilm stage 为 `concept_confirmed`。
- Logo 或商品图未确认时可以查看方向和生成剧本，但真实视频生成按 Route 规则被阻断。
- Strategy 新版本发布后旧 Task 保持原血缘。
- 手动 PDF 对照用例仍从 `waiting_for_input` 开始。

## 12. 分批实施

### Batch 0：产品与契约冻结

- 更新 `docs/02-creative-studio-prd.md`。
- 评审本文、API 行为、错误码、legacy 谓词和发布顺序。
- 更新 OpenAPI 后再进入代码实现。

### Batch 1：后端 prepare 编排

- 新增 prepare request/result 和 handler。
- 复用 Brand Brief projection/validation/confirmation。
- 增加结构化 issue 和幂等测试。
- 暂不收紧旧 create-video-task，保证旧前端仍可用。

### Batch 2：legacy 安全升级

- 实现 A/B/C/D 分类。
- 增加 repository 归档替换事务。
- 提供只读审计报告和测试。

### Batch 3：前端切换

- Strategy CTA、品牌 Hub 和深链统一调用 prepare。
- 恢复 Direction Gate；确认方向后才创建 Task。
- 显式区分 Intake/Task URL。
- 工作台按 source kind 差异化显示。

### Batch 4：后端硬门禁

- 观察新前端稳定后，拒绝 Strategy brand 无 DirectionID 创建 Task。
- 保留 manual 无 Direction路径。
- 删除或限制前端 `createBrandFilmTaskFromIntake`：仅 manual 调用。

### Batch 5：清理与契约债务

- 处理 B 类 legacy Task，输出 C/D 类人工清单。
- 监控 Strategy 来源 Brief 分析调用为 0。
- 单独规划完整 Handoff v2，消除首次投影对 Strategy 在线读取的依赖。

## 13. 发布、兼容与回滚

发布顺序必须为“后端兼容 → 前端切换 → 后端收紧”，否则旧前端会被新门禁打断。

1. 上线 prepare API、legacy 分类和观测，保留旧行为但记录无 Direction 直达请求。
2. 开启 `creative_strategy_brand_continuity_v1` 内部灰度，先覆盖测试 Project。
3. 前端切换 10% → 50% → 100%，观察 prepare 成功率、Direction 到达率、重复资源和错误码。
4. 全量稳定后开启 `creative_strategy_brand_direction_required` 后端硬门禁。
5. 执行 B 类 legacy 升级；C/D 类保持不动。

回滚：

- 可关闭前端“一键确定性接受”，退回“显示 Creative Brand Brief 供人工确认”，但不能恢复无 Direction 空 Task 直达。
- 后端 prepare 是幂等增量 API，可保留不回滚数据。
- 如 Direction 服务异常，用户停留在 confirmed Brief/Direction Gate，不能回退到 Brief 模型解析。
- 新建的正式 Task 不降级；回滚版本必须能读取 `concept_confirmed` 和完整 source snapshot，已归档的空 legacy Task 保持可审计。

## 14. 完成标准

- PRD、本文、OpenAPI、错误码和页面文案一致。
- 新 Strategy 链路不调用 Brief 分析模型、不创建空 Task。
- Strategy 和 manual 两类来源的状态机与 UI 测试齐全。
- legacy 审计完成，B 类可恢复，C/D 类无数据覆盖。
- 本地至少通过：
  - `gofmt`（变更 Go 文件）
  - `go test ./internal/systems/creative ./internal/platform/httpserver`
  - `npm test`
  - `npm run build`
  - 目标 Playwright E2E
  - `git diff --check`
- 推送后按仓库要求持续检查 `gh pr checks`，所有 required GitHub Actions checks 通过才算完成。

## 15. 反方评审

以下评审以“不应批准当前方案”为立场，优先寻找会造成错误继承、合规绕过、数据覆盖或发布事故的理由。

### 15.1 反对意见一：这是把 Strategy 的批准偷换成 Creative 的确认

**挑战**：上游批准者可能只批准策略，不具备创意生产确认权限。自动确认会破坏系统所有权和审计。

**成立部分**：不能后台自动、不能在 GET 中确认，也不能把 Strategy 审批人写成 Creative 确认人。

**处置**：保留 Creative 自有 `BrandBriefReview`、本地 validator、revision/hash 和确认记录；只有具有 `creative.write` 权限的当前用户显式点击“确认策略输入并开始创作”才确认。按钮文案和审计事件明确动作含义。

**裁决**：问题已通过显式用户命令和本地门禁缓解，不否决方案。

### 15.2 反对意见二：无 blocker 不代表信息足以产生高质量品牌方向

**挑战**：当前 validator 把人群洞察、张力、卖点、证明点、场景、语调和声音方案多视为 warning；自动通过可能产出空泛方向。

**成立部分**：这是质量风险，尤其对品牌视频。不能用默认值或模型猜测掩盖。

**处置**：

- P0 planning blocker 继续锁定市场、语言、目标、人群、品牌名、产品名、单一主张和完整 Route。
- warning 在 Direction Gate 显著显示，并进入 PlanningContext；模型不得把缺失项写成事实。
- 上线后以 Direction 人工退回率、Brief 修订率和盲评质量验证是否需要将特定 warning 提升为 blocker。
- 不在本次未经数据验证时一次性收紧冻结契约。

**裁决**：条件接受；灰度指标是扩大流量前门禁。

### 15.3 反对意见三：所谓确定性投影仍依赖 Strategy 在线读取，违背 Intake 完整快照承诺

**挑战**：Handoff v1 缺品牌名、产品名和卖点；`PrepareBrandBriefReview` 需要 `StrategyPackageReader`。Strategy 故障时首次进入 Creative 仍失败。

**成立部分**：完全成立，这是当前冻结契约的历史缺口。

**处置**：P0 只按精确 Package ID/version/hash 读取并立即固化 Brand Brief，不回退模型；P1 发布完整 Handoff 新版本并双读迁移。prepare 返回可重试错误，不能创建空 Task。

**裁决**：不阻塞修复当前重复分析，但列为明确契约债务；若业务要求 Strategy 故障时首次交接也必须可用，则 Handoff v2 必须提升为 P0。

### 15.4 反对意见四：拆开素材确认会让没有 Logo/商品图的方向脱离现实

**挑战**：创意方向若不知道实际商品和 Logo 形态，可能不可制作。

**成立部分**：素材可用性应影响执行风险，但不等同于目标/受众/主张需要重新确认。

**处置**：Direction PlanningContext 保留已知资产元数据和缺失 warning；方向卡显示“素材待补充”风险。Route 明确要求 generation 阶段素材时，ProviderJob 仍被阻断。需要资产才能成立的具体方向应被质量门拒绝或标记高风险。

**裁决**：接受拆分门禁，不接受跳过素材风险展示。

### 15.5 反对意见五：新增聚合 prepare API 是不必要的接口膨胀

**挑战**：前端依次调用现有 prepare、confirm、get batch 即可，新 API 增加维护面。

**成立部分**：领域原子能力已经存在。

**处置**：聚合命令不复制领域逻辑，只表达一次用户意图并集中处理恢复、幂等、legacy 分类和 next_action。把这些判断散落在两个前端入口会再次形成双链路。原子 API继续保留。

**裁决**：新增命令合理，但禁止在 handler 中复制 validator 或 projection。

### 15.6 反对意见六：收紧 create-video-task 会造成前后端发布窗口故障

**挑战**：如果后端先拒绝无 Direction 请求，旧前端立即不可用。

**成立部分**：完全成立。

**处置**：严格执行 Batch 1 → 3 → 4；先兼容观测、再切前端、最后硬门禁。硬门禁使用独立开关，确认旧调用量为 0 后启用。

**裁决**：发布顺序是上线硬条件。

### 15.7 反对意见七：legacy 自动处理可能覆盖用户已经分析的 Brief

**挑战**：仅看 stage 可能不可靠，旧 Task 可能有未显式反映在 stage 的编辑或附件。

**成立部分**：完全成立。

**处置**：B 类谓词必须同时检查 analysis/concept/plan/generation/audio/quality/delivery 全为空、Direction 为空、revision/Task version 符合初始模式，并用事务归档旧 Task、创建新 Task。任何一项不满足归入 C 类，不自动处理。旧记录保留，迁移前先做只读审计。

**裁决**：只有严格谓词和事务测试完成后才允许迁移。

### 15.8 反对意见八：同一 URL 参数混用 Intake ID 和 Task ID 会继续制造隐式副作用

**挑战**：当前页面通过“先 GET Intake，失败再 GET Task”猜类型，错误、权限问题或短暂故障可能被误判并触发其他动作。

**成立部分**：完全成立。

**处置**：P0 前端改为显式 `intake`/`task` 参数；旧链接兼容只能读取和导航，不能在 catch 分支创建资源。

**裁决**：URL 身份修复属于本次 P0，不延后。

### 15.9 反对意见九：方向确认后创建 Task 与 legacy 升级存在并发双写

**挑战**：用户双击、两个标签页或 Direction 回调重试可能同时创建正式 Task并重复归档旧 Task。

**成立部分**：成立。

**处置**：在服务端锁定该 Intake 下的 active legacy Task，并先按 lineage key 查找正式 Task；已有则直接返回，没有才在同一事务内归档空 Task并创建正式 Task。并发冲突后重新按 lineage key 读取，不依赖已移除的“一 Intake 一 Task”唯一键。

**裁决**：并发集成测试是合入门禁。

### 15.10 反对意见十：成功标准只验证“少一步”，没有验证质量和合规

**挑战**：不再分析 Brief 可能提升速度，却降低方向质量或放过宣称风险。

**成立部分**：成立。

**处置**：除耗时外同时跟踪 Direction 退回率、Brand Brief 后续修订率、claim/evidence 校验失败率、素材 blocker 命中率和人工盲评。Package/Handoff 中的 claims、guardrails 和 evidence refs必须原样进入 Brand Brief 与 PlanningContext。

**裁决**：灰度评估必须同时包含效率、质量和合规指标。

## 16. 反方评审最终裁决

**有条件通过。**

实施前必须满足四项前置条件：

1. prepare 是显式 POST，不允许 GET、页面加载或后台任务自动确认。
2. Strategy brand 无 Direction 创建 Task 的硬门禁只能在前端全量切换后开启。
3. legacy 自动归档替换必须使用严格空任务谓词、事务和迁移前审计。
4. 对 Handoff v1 产品事实不完整和首次投影依赖 Strategy 在线的债务建立 P1 任务；若可用性目标要求离线首次交接，则升级为 P0。

任何实现如果选择“在 BrandFilmWorkspace 隐藏解析按钮但后端仍创建 `waiting_for_input` Task”，或“继续无 Direction 创建 Task后强行把 stage 改成 confirmed”，均不符合本方案，应在评审中拒绝。
