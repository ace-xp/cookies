# 爆款复刻／裂变闭环整改技术调研（2026-08-17）

## 实施状态（2026-08-17）

本轮已经落地并通过构建/测试验证的整改项：

- Adapter Gateway 运行模式为视频配置读取端点注入只读配置源；设置页明确展示“由 Gateway 管理”，写入或探测返回可行动的冲突说明，避免 503 与错误的自管理表单。
- 前端只以 `video.generate + cookies.video.standard` 的精确能力作为生成门禁，并区分 Gateway 管理模式。
- 工作区恢复源视频、参考图和候选成片的项目资产预览；任一已引用素材不能预览时，不允许完成授权或发起生成。候选视频可播放、下载并提交审核。
- 产品名在项目存在产品档案时必须属于当前项目；修改产品事实会作废分析和 PromptPackage，必须重新分析。
- 时长在页面明确固定为 15 秒；分析、手工 intake 和视频 job 使用稳定幂等键。确认后的同一 PromptPackage 重试会复用原 job；候选先落库、生产作业注册失败后可由同一 job 重试补齐，不会丢失候选血缘或重复计费。
- 视频和参考图采用独立授权勾选，服务端仍逐项强制校验。
- 当视频模型以 `REFERENCE_ASSET_CONTENT_REJECTED` 拒绝清晰真实人物参考图时，页面会**自动**冻结一个新的、可追溯的文本生成 PromptPackage 并继续提交生成：保留场景、服装、镜头、节奏和氛围等文字约束，但不再把该图片提交给模型，并明确要求生成原创、非特定身份的写实成年人；失败候选和原图授权记录仍保留在任务血缘中。

尚未在本变更中引入新的 saga/outbox 表、授权证据合同字段或跨分页的“最近工作区”专用 API；前述候选优先持久化与同一 job 补偿已覆盖当前作业丢失风险。这些进一步演进需要数据库迁移和完整的运维 reconciler，保留为下面的 P2/P1 演进项，而不是伪装成已经存在的能力。

## 结论

当前“爆款复刻”已具备任务持久化、五维分析、人工确认、Provider 视频任务、候选血缘和提交评审的主干能力，但尚未达到稳定可运营的闭环标准。最需要优先处理的是：视频路由真实可用性与前端门禁不一致、Adapter Gateway 部署下设置页不可运维、恢复后无法核验已授权素材、生成成功后无法在当前页面预览成片。

本调研仅使用仓库源码、OpenAPI、测试和既有设计文档作为一手资料；没有调用外部来源，也没有读取或记录任何凭据。

## 范围、预期闭环与事实基线

预期用户路径为：选择项目内源视频和参考图 → 生成五维分析 → 编辑提示词 → 明确确认每份素材的授权 → 以已验证的视频路由创建任务 → 轮询并入库 → 当前页面预览成片与检查结果 → 提交评审／进入交付。

后端的病毒复刻状态机已定义 `waiting_for_analysis`、`analysis_ready`、`generation_ready`、`generating`、`candidate_ready`、`provider_failed`、`ready_for_review`，并在候选成功后保存输出项目资产、授权、提示词血缘和原创性护栏四项检查。[`internal/systems/creative/viral_remake.go:16`](../../internal/systems/creative/viral_remake.go#L16) [`internal/systems/creative/viral_workflow.go:290`](../../internal/systems/creative/viral_workflow.go#L290) [`internal/systems/creative/viral_workflow.go:406`](../../internal/systems/creative/viral_workflow.go#L406)

OpenAPI 也声明了恢复、分析、提示词修订、生成确认、候选提交评审及 Provider 视频任务等端点，故以下问题主要是“契约虽在、端到端体验或运行保障不完整”，而不是缺少基本 API。[`api/openapi/creative-v1.yaml:1353`](../../api/openapi/creative-v1.yaml#L1353) [`api/openapi/creative-v1.yaml:1460`](../../api/openapi/creative-v1.yaml#L1460)

优先级约定：P0 会让正常用户无法生成或无法恢复生产能力；P1 会造成错误成片、不可核验授权、重复成本或无法完成评审；P2 是可靠性、可观测性或回归覆盖缺口。

## 问题与推荐整改

### P0：Adapter Gateway 下视频配置接口未装配，运营人员无法修复路由

**证据。** `GET/PUT/POST /platform/v1/provider/video-configuration*` 在 `providerVideo == nil` 时直接返回未实现（HTTP 503）。[`internal/platform/httpserver/video_configuration_handlers.go:62`](../../internal/platform/httpserver/video_configuration_handlers.go#L62) [`internal/platform/httpserver/video_configuration_handlers.go:77`](../../internal/platform/httpserver/video_configuration_handlers.go#L77)

启动装配中，`adapter_gateway` 分支只把 `MySQLGatewayConfigStore` 赋给 Provider job 的 `VideoRoutes`；`dependencies.ProviderVideoConfiguration` 与环境回退信息只在非 Gateway 分支设置。因此运行中的网关路由可以被解析，却没有设置页服务可读写或诊断。[`cmd/cookies-api/main.go:765`](../../cmd/cookies-api/main.go#L765) [`cmd/cookies-api/main.go:769`](../../cmd/cookies-api/main.go#L769) [`cmd/cookies-api/main.go:777`](../../cmd/cookies-api/main.go#L777)

**影响。** 视频路由失效后，业务页面只能报“未配置”或创建任务失败；操作者没有受支持的自助恢复入口。既有设置设计把这一组接口定义为“保存前探测、保存即生效”的运维通道，当前部署分支违背该设计。[`docs/superpowers/specs/2026-08-15-video-model-settings-design.md:113`](../superpowers/specs/2026-08-15-video-model-settings-design.md#L113)

**修复。** 将 Gateway 场景的 `videoConfigStore` 同时注入 `ProviderVideoConfiguration`；设置 API 应识别连接类型并展示“由 Adapter Gateway 管理”的只读路由详情或受策略允许的编辑入口。若 Gateway 禁止本应用改凭据，应返回可行动的“由哪个控制面维护”的状态，而不是 503。保留现有 API Key 掩码与保存前探测行为。

**验证。** 增加启动装配集成测试：Gateway 模式下配置 API 至少 GET 200；缺路由时页面可得到明确 `not_configured` 原因；允许管理时 PUT/verify 成功并在下一次视频任务解析到新路由。回归 `video_configuration_handlers_test.go` 的不泄露凭据断言。[`internal/platform/httpserver/video_configuration_handlers_test.go:70`](../../internal/platform/httpserver/video_configuration_handlers_test.go#L70)

### P0：生成按钮采用“任意网关已配置”而非视频路由就绪作为门禁

**证据。** `ModelConfigContext` 虽计算了 `videoGenerationAvailable`，却同时把 provider 的整体 `status` 由 capabilities 的总状态决定；网络异常时还把所有状态压扁为“未配置”。[`src/context/ModelConfigContext.tsx:67`](../../src/context/ModelConfigContext.tsx#L67) [`src/context/ModelConfigContext.tsx:71`](../../src/context/ModelConfigContext.tsx#L71) [`src/context/ModelConfigContext.tsx:76`](../../src/context/ModelConfigContext.tsx#L76)

爆款复刻页面没有使用 `videoGenerationAvailable`，而是从 `providers` 中寻找任一 `status === '已配置'` 的 provider，再据此放行生成。该 provider 可仅有文本或图像能力；反之，能力接口瞬断也会误判为未配置。[`src/components/SpecializedPages.tsx:651`](../../src/components/SpecializedPages.tsx#L651) [`src/components/SpecializedPages.tsx:679`](../../src/components/SpecializedPages.tsx#L679) [`src/components/SpecializedPages.tsx:859`](../../src/components/SpecializedPages.tsx#L859) [`src/components/SpecializedPages.tsx:936`](../../src/components/SpecializedPages.tsx#L936)

**影响。** 用户可通过 UI 门禁后才在 Provider 创建阶段失败；或正常的视频路由因能力探测失败而被错误禁用。后端在路由缺失且不可回退时确实拒绝创建任务，证实前端不能将“网关已配置”代替“此模型路由可创建”。[`internal/platform/provider/video_route_fallback_test.go:78`](../../internal/platform/provider/video_route_fallback_test.go#L78)

**修复。** 将页面门禁改为精确的 `video.generate + cookies.video.standard + available`，并同时展示 `loading / unavailable / degraded / available`。最好由后端提供任务可执行性预检（模型别名、输入模式、时长、比例、分辨率、路由版本、可重试原因），页面使用该预检，而非从总能力列表自行推断。

**验证。** 前端测试覆盖：只有文本模型时按钮禁用；视频路由可用时按钮可用；能力 API 失败显示“状态暂不可确认，可重试”而非“未配置”。后端集成测试覆盖预检与实际 `:video-job` 对同一配置给出一致结果。

### P1：恢复任务后素材预览丢失，授权确认无法核验对象

**证据。** 初次上传时页面使用 `URL.createObjectURL(file)` 作为预览；组件卸载时立即回收该 URL。[`src/components/SpecializedPages.tsx:747`](../../src/components/SpecializedPages.tsx#L747) [`src/components/SpecializedPages.tsx:775`](../../src/components/SpecializedPages.tsx#L775) [`src/components/SpecializedPages.tsx:716`](../../src/components/SpecializedPages.tsx#L716)

恢复逻辑只从持久化快照恢复 Asset ID、版本和输入文本，未以快照中的 `reference_video`／`reference_image` 请求预览 URL；UI 也只在本地预览 URL 存在时渲染视频/图片。[`src/components/SpecializedPages.tsx:682`](../../src/components/SpecializedPages.tsx#L682) [`src/components/SpecializedPages.tsx:699`](../../src/components/SpecializedPages.tsx#L699) [`src/components/SpecializedPages.tsx:911`](../../src/components/SpecializedPages.tsx#L911)

仓库已有项目资产预览 API，且其他工作区在恢复项目资产后使用它，说明缺口在病毒复刻页面而非基础设施。[`src/data/api.ts:4566`](../../src/data/api.ts#L4566) [`src/components/BrandFilmWorkspace.tsx:228`](../../src/components/BrandFilmWorkspace.tsx#L228)

**修复。** 恢复 workspace 时并行请求源视频、参考图及候选输出的项目资产预览；请求失败时显示 Asset ID、版本、失败原因和“从素材库重新选择”，不得显示空白后仍允许确认。删除可手填的源 Asset ID/版本输入，改为项目素材选择器；手工输入只能保留给开发 fixture 并受 feature flag 限制。

**验证。** Playwright：上传→分析→刷新→仍可看到同一 source/ref 预览；更换项目不得显示旧项目 blob；预览接口 403/404 时不能点击授权确认或生成。还要验证预览 URL 的销毁与组件卸载不影响已入库资产。

### P1：生成成功后没有当前页成片预览或可继续交付的显式入口

**证据。** 轮询成功后页面仅刷新 workspace 并提示“已关联到当前 Project”。[`src/components/SpecializedPages.tsx:723`](../../src/components/SpecializedPages.tsx#L723) [`src/components/SpecializedPages.tsx:735`](../../src/components/SpecializedPages.tsx#L735)

候选摘要只显示 Asset ID、四项检查和“提交候选评审”；没有调用 `getProjectAssetPreview`、没有 `<video>`，也没有前往素材库/审核页的导航。[`src/components/SpecializedPages.tsx:938`](../../src/components/SpecializedPages.tsx#L938) [`src/components/SpecializedPages.tsx:944`](../../src/components/SpecializedPages.tsx#L944)

**修复。** 以 `latestCandidate.output_asset_ref` 请求预览 URL，并在候选卡中提供原生视频播放、下载/在素材库打开、检查详情与“提交评审”。提交前需明确显示“自动检查通过不等于人工创意/品牌审核”。成功轮询应同步候选对象，以避免只知道 job 成功但未出现输出资产的中间态。

**验证。** 成功 fixture 下断言 video `src` 指向项目资产预览、可见四项自动检查、人工提交后状态变为 `reviewed`；输出入库延迟时显示“任务成功，等待资产入库”并继续轮询，不误报完成。

### P1：产品事实可任意串写，当前链路没有一致性门禁

**证据。** 页面将产品、两个卖点和 CTA 都作为任意可编辑字符串传入手工 intake。[`src/components/SpecializedPages.tsx:815`](../../src/components/SpecializedPages.tsx#L815) [`src/components/SpecializedPages.tsx:821`](../../src/components/SpecializedPages.tsx#L821)

领域校验只检查产品名、用户指令和卖点的非空/长度，不验证其与 Project 产品档案、确认 Brief 或允许的声明一致。[`internal/systems/creative/viral_remake.go:45`](../../internal/systems/creative/viral_remake.go#L45) [`internal/systems/creative/viral_remake.go:53`](../../internal/systems/creative/viral_remake.go#L53) 这些字段随后原样写入不可变 `PromptPackage.ProductFacts` 和综合提示词。[`internal/systems/creative/viral_workflow.go:177`](../../internal/systems/creative/viral_workflow.go#L177) [`internal/systems/creative/viral_workflow.go:489`](../../internal/systems/creative/viral_workflow.go#L489)

**影响。** 截图中化妆品目标产品与工业精度/打样 CTA 的组合会通过技术校验并生成错误广告；这不是模型幻觉，而是输入数据缺少来源与一致性约束。

**修复。** 优先从已确认 Brief 或项目产品档案选择产品事实；manual 模式至少要求用户逐条标注来源（产品档案、已审核 brief、人工新增）并在服务端检查：产品归属当前 Project、卖点/CTA 未违反 prohibited claims、手工新增必须具有审批或风险标记。将来源 refs 和校验结果冻结到 PromptPackage。

**验证。** 单元与 API 测试覆盖：跨项目产品拒绝；禁止声明拒绝；已确认 Brief 的事实可通过；手工新增在未审批时只能保存草稿、不能创建付费任务。页面给出具体冲突字段，而非笼统失败。

### P1：时长输入范围与实际生成规格冲突，用户值会被静默改写为 15 秒

**证据。** 页面允许输入 9–180 秒。[`src/components/SpecializedPages.tsx:918`](../../src/components/SpecializedPages.tsx#L918) API 创建手工 workspace 时先把该值夹到 4–60 秒。[`src/data/api.ts:4794`](../../src/data/api.ts#L4794) 最终确认生成时服务端又固定为 `min(草稿时长, 15)`，并强制 9:16、720p、一个候选。[`internal/systems/creative/viral_workflow.go:173`](../../internal/systems/creative/viral_workflow.go#L173)

**影响。** 输入 30、60 或 180 秒的用户都会得到 15 秒成片，UI 没有解释或确认。该差异会影响预算、脚本结构和验收。

**修复。** 第一阶段应将 UI 明确固定为“15 秒 / 9:16 / 720p / 1 候选”并在生成前展示；若产品确需可配置，改为后端从已验证 route constraints 读取允许枚举，服务端保存用户选择并在 Provider input 中保持一致，不能静默裁剪。

**验证。** 合同测试验证每个可选时长在 workspace、PromptPackage 和 Provider input 中一致；不支持的时长返回 422 和支持集合。E2E 断言显示的规格等于实际 job 请求。

### P1：恢复“最新”任务的选择不稳定，重复任务会打开错误记录

**证据。** `getLatestViralRemakeWorkspace` 请求最多 100 条任务后使用 `.find(...)` 返回第一个未归档任务，既未按创建时间排序也未指定业务规则；函数名却承诺 latest。[`src/data/api.ts:5473`](../../src/data/api.ts#L5473) [`src/data/api.ts:5477`](../../src/data/api.ts#L5477)

**修复。** 提供后端的 `creative-workspaces/viral-remake` 单一恢复端点，定义排序（默认最近 `updated_at`，可带 `task_id` 精确恢复）和 archive/superseded 规则。前端 URL 传 task ID，列表页允许用户选择历史任务，不应猜测“第一个”。

**验证。** 建立同项目两个以上病毒复刻任务：刷新应恢复最近更新的任务；传入 task ID 必须恢复指定任务；归档任务不可自动恢复。API 测试验证分页大于 100 时仍正确。

### P1：前端为分析、手工 intake 和视频任务每次生成新的幂等键，网络重试会重复建任务或产生重复付费作业

**证据。** 手工 intake 使用 `Date.now()+Math.random()`；分析和视频任务也在每次调用加入当前时间。[`src/data/api.ts:4839`](../../src/data/api.ts#L4839) [`src/data/api.ts:5490`](../../src/data/api.ts#L5490) [`src/data/api.ts:5564`](../../src/data/api.ts#L5564)

后端明确要求幂等键后才创建 Provider job，表示该边界设计为可重试；前端每次重试改变 key 使该保障失效。[`internal/platform/httpserver/creative_handlers.go:1973`](../../internal/platform/httpserver/creative_handlers.go#L1973) [`internal/platform/httpserver/creative_handlers.go:2110`](../../internal/platform/httpserver/creative_handlers.go#L2110)

**修复。** 为一次用户意图生成持久 action ID：`manual-viral` 绑定输入 hash，分析绑定 task+源资产版本，生成绑定 task+confirmed PromptPackage hash。超时或刷新后复用同一 key，并先查询已有 action/job。后端维持 request-hash 与 key 的冲突检测，向 UI 返回已有 job 标记。

**验证。** 模拟请求超时但服务端已提交：第二次调用应返回同一 intake/task/job，不新增 Provider job。测试快速双击、浏览器刷新重试及相同 key 携带不同请求体的冲突。

### P1：授权确认只有布尔断言，缺少可审计的授权依据与逐素材选择

**证据。** 前端只有一个复选框；确认 API 固定发送源视频为 `true`，参考图仅按是否存在传布尔值。[`src/components/SpecializedPages.tsx:934`](../../src/components/SpecializedPages.tsx#L934) [`src/data/api.ts:5512`](../../src/data/api.ts#L5512)

服务端只将布尔值写入 `RightsConfirmed`，候选检查也只验证该状态。[`internal/systems/creative/viral_workflow.go:164`](../../internal/systems/creative/viral_workflow.go#L164) [`internal/systems/creative/viral_workflow.go:406`](../../internal/systems/creative/viral_workflow.go#L406) OpenAPI 同样只有两个 boolean，未承载授权类型、依据、到期日或确认人声明文本。[`api/openapi/creative-v1.yaml:2131`](../../api/openapi/creative-v1.yaml#L2131)

**修复。** 将每份条件素材建模为授权声明：AssetVersionRef、scope（分析/生成/投放）、basis（自有/合同授权/公开许可等）、evidence ref、有效期、确认人和确认时间。服务端在生成前要求 scope 覆盖本次操作；不可证明的素材可保存草稿但不可提交 Provider。短期内至少拆为两个复选框并在确认前强制显示已恢复预览和版本。

**验证。** 服务端拒绝仅确认视频却未确认参考图、过期授权、授权 scope 不含生成、资产版本已变更；审计查询可返回确认人和依据但不暴露敏感合同内容。

### P2：Provider job 创建与候选注册不是原子操作，可能留下孤儿作业

**证据。** HTTP handler 先调用 `CreateVideoJob`，成功后才调用 `RegisterViralCandidateJob`；后者失败时已创建的付费 job 不会取消，也没有 outbox/reconcile 补偿。[`internal/platform/httpserver/creative_handlers.go:2110`](../../internal/platform/httpserver/creative_handlers.go#L2110) [`internal/platform/httpserver/creative_handlers.go:2119`](../../internal/platform/httpserver/creative_handlers.go#L2119)

**修复。** 将“创建 intent/候选记录 → 调度 Provider → 回写 provider job ID”改为可恢复的 saga/outbox：候选先为 `submitting`，调度器有幂等键，失败可重试或显式取消；后台 reconciler 找出 SourceTaskID 为 viral 但无候选的 job 并报警/补偿。

**验证。** 注入候选注册数据库失败：不得静默丢失 job；重试后只关联同一个 job；超过阈值可见告警并可人工恢复。

### P2：现有自动化未覆盖真实授权门禁和恢复/交付闭环

**证据。** 服务单元测试覆盖“五维分析持久化”和“确认→候选→评审”，是有价值的领域回归，但使用内存 stub，不覆盖 HTTP、配置可用性、资产预览或真实浏览器状态。[`internal/systems/creative/viral_workflow_test.go:11`](../../internal/systems/creative/viral_workflow_test.go#L11) [`internal/systems/creative/viral_workflow_test.go:46`](../../internal/systems/creative/viral_workflow_test.go#L46)

现有 E2E 声称覆盖生成，但在点击“生成复刻视频”前没有勾选授权确认，和当前按钮条件 `!rightsConfirmed` 相矛盾；同时它轮询旧的 `/api/generation-jobs` 兼容接口，而不是验证当前 Creative workspace 的候选、预览和提交评审状态。[`e2e/investor-mvp.spec.ts:134`](../../e2e/investor-mvp.spec.ts#L134) [`e2e/investor-mvp.spec.ts:164`](../../e2e/investor-mvp.spec.ts#L164) [`src/components/SpecializedPages.tsx:936`](../../src/components/SpecializedPages.tsx#L936)

**修复。** 以真实当前契约重写 E2E：上传或选择真实项目内 MP4/图片→五维分析→逐素材授权→创建 Provider fixture job→恢复页面→预览 output asset→提交评审。拆分为成功、无视频路由、路由恢复、权限未确认、来源不一致、入库延迟和 Provider 失败用例。补 HTTP handler 和 MySQL 集成测试来覆盖配置 Gateway 装配、并发 revision 与幂等恢复。

## 建议实施顺序

1. **先解除生产阻塞（P0）。** 修 Gateway 配置装配；增加精确视频路由预检；页面只以精确预检放行。
2. **补齐可核验闭环（P1）。** 实现持久资产恢复/预览、输出预览与交付入口，限制资产选择为当前 Project，并修正时长规格展示与后端契约。
3. **保证事实与成本安全（P1）。** 产品事实来源校验、逐素材授权声明、稳定 action idempotency、显式任务恢复选择。
4. **提高故障可恢复性（P2）。** 将 job/candidate 关联做成 saga/outbox，补 reconciler、告警和端到端回归。

每一步都应保持兼容：先新增预检/恢复端点和数据字段，再迁移页面；旧 boolean 授权仅在限定过渡期读取，生成接口在切换后拒绝缺少新声明的请求。

## 验收清单

- Adapter Gateway、直连和 fake 三种部署下，设置页和业务页都能给出一致且可行动的视频能力状态。
- 用户刷新、换设备或从任务历史进入后，能看到与冻结 AssetVersionRef 一致的源视频、参考图、候选成片和全部检查。
- 任何未授权、过期、跨项目或版本不一致的素材都不能进入 Provider 请求；审核记录可追溯到资产版本和确认人。
- 页面所示时长、比例、分辨率、候选数和后端 Provider input 完全一致；不支持的组合在提交前被阻止并说明原因。
- 超时、刷新和重复点击不会产生重复 intake、task 或 Provider job；创建后候选一定可恢复，或系统自动补偿并告警。
- E2E 在 CI 中跑当前 `/api/creative/v1` 路径，验证授权门禁、生成、入库延迟、恢复、播放和提交评审；前端变更执行 `npm run build`，后端变更执行相关 `go test` 与 OpenAPI/迁移校验。

## 来源索引

- 领域状态、校验与候选检查：[`internal/systems/creative/viral_remake.go`](../../internal/systems/creative/viral_remake.go)、[`internal/systems/creative/viral_workflow.go`](../../internal/systems/creative/viral_workflow.go)、[`internal/systems/creative/service.go`](../../internal/systems/creative/service.go)。
- HTTP、Provider 路由和配置装配：[`internal/platform/httpserver/creative_handlers.go`](../../internal/platform/httpserver/creative_handlers.go)、[`internal/platform/httpserver/video_configuration_handlers.go`](../../internal/platform/httpserver/video_configuration_handlers.go)、[`cmd/cookies-api/main.go`](../../cmd/cookies-api/main.go)、[`internal/platform/provider/video_route_fallback_test.go`](../../internal/platform/provider/video_route_fallback_test.go)。
- 前端行为与 API 客户端：[`src/components/SpecializedPages.tsx`](../../src/components/SpecializedPages.tsx)、[`src/context/ModelConfigContext.tsx`](../../src/context/ModelConfigContext.tsx)、[`src/data/api.ts`](../../src/data/api.ts)。
- API 与测试基线：[`api/openapi/creative-v1.yaml`](../../api/openapi/creative-v1.yaml)、[`internal/systems/creative/viral_workflow_test.go`](../../internal/systems/creative/viral_workflow_test.go)、[`e2e/investor-mvp.spec.ts`](../../e2e/investor-mvp.spec.ts)。
