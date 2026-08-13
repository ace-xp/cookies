# cookies 产品文档

> 项目名称：cookies<br>
> 产品名称：cookies<br>
> 产品口号：**从一句需求，到持续增长。**

## 文档导航

| 文档 | 说明 | 主要读者 |
| --- | --- | --- |
| [设计系统](../DESIGN.md) | 已选定的智能蓝图方向、品牌令牌、官网与产品工作台规范 | 品牌、设计、前端、产品 |
| [项目总纲](./00-project-overview.md) | 产品目标、Codex 技术底座、范围、指标、架构与路线图 | 全体成员 |
| [广告需求收集及策略分析 PRD](./01-demand-strategy-prd.md) | 通过 Codex + Skills 对话助手完成需求澄清与策略产出 | 产品、设计、研发、算法 |
| [广告创意创作 PRD](./02-creative-studio-prd.md) | 生产小红书/公众号图文、电影感品牌广告、效果广告与独立素材剪辑 | 产品、设计、研发、算法 |
| [广告素材经验与数据分析 PRD](./03-asset-management-prd.md) | 从素材内容与投放数据中沉淀经验、洞察和创作建议 | 产品、数据、算法、运营 |
| [广告智能投放 PRD](./04-intelligent-delivery-prd.md) | 通过 Codex + Computer Use 完成配置、上线、监控与优化 | 产品、研发、算法、投手 |
| [共享基座规格](./05-shared-foundation.md) | 用户组织、Provider、知识库、Agent、Computer Use、任务和治理能力 | 架构、平台、研发、运维 |
| [ORAG 知识库集成](./06-orag-integration.md) | ORAG submodule、Knowledge Gateway、租户映射、Provider 与升级测试 | 架构、平台、后端、运维 |
| [统一模型 Provider 规格](./07-unified-model-provider.md) | 默认火山引擎，统一 LLM、VLM、图片、视频、音频、3D、路由与治理 | 架构、平台、后端、算法、运维 |
| [项目、品牌与产品域](./08-project-brand-domain.md) | Project 如何连接四系统，以及 Brand、Product、资源索引、版本、权限和上下文所有权 | 产品、设计、架构、后端 |
| [Codex 与 Skills 运行时](./09-codex-skills-runtime.md) | Agent Task、Worker、Skill 包、隔离、恢复和产物落库 | 架构、后端、算法、安全 |
| [广告数据 Connector](./10-ad-data-connectors.md) | 平台授权、同步、原始数据、统一指标、归因、对账和数据质量 | 数据、后端、投放、分析 |
| [媒体资产平台](./11-media-asset-platform.md) | 上传、扫描、转码、生成资产、授权、分发和删除 | 后端、创意、算法、运维 |
| [Computer Use 运行时](./12-computer-use-runtime.md) | 受控设备、浏览器会话、人工接管、页面证据和紧急停机 | 架构、投放、安全、运维 |
| [API 与领域事件契约](./13-api-event-contracts.md) | API 命名、错误、幂等、并发、SSE、事件目录、契约治理和已实现广告 AIGC/AI 混剪 OpenAPI 覆盖范围 | 全体研发 |
| [工程、运维与安全基线](./14-engineering-operations-security.md) | 环境、CI/CD、多租户、SLO、RPO/RTO、可观测性与 AI 合规 | 研发、运维、安全 |
| [PRD 通用交互与质量要求](./15-prd-cross-cutting-requirements.md) | 页面状态、并发编辑、AI 披露、可追踪性、无障碍和运营能力 | 产品、设计、研发、测试 |
| [文档补充说明与决策清单](./16-document-gap-closure.md) | 缺口关闭映射、剩余产品/技术决策和研发前门禁 | 产品、架构、项目负责人 |
| [品牌视觉与网站整体风格提案](./17-brand-visual-directions.md) | 官网与产品工作台的三套品牌视觉方向、对比与选型建议 | 产品、品牌、设计、前端 |
| [四大模块导航与信息架构](./19-module-navigation-architecture.md) | 全局壳层、四系统三级导航树、路由、状态记忆与交互规范 | 产品、设计、前端、后端、测试 |
| [四大模块子板块分析](./20-module-submodule-analysis.md) | 每个子板块的功能、必要性、价值、优先级、角色与展示形式 | 产品、设计、研发、业务负责人 |
| [视频素材剪辑与开源框架方案](./21-video-material-editor-spec.md) | 素材剪辑子模块、编辑器布局、OpenCut/FFmpeg 选型、数据模型与 MVP | 产品、设计、前端、后端、音视频研发 |
| [Project 中心化页面路径整改规划](./22-project-centered-navigation-remediation-plan.md) | Project 管理、唯一工作台、八阶段落点、四模块逐页问题、整改计划与全面评估 | 产品、设计、前端、后端、测试 |
| [Strategy × Creative 并行开发契约（已废止）](./23-strategy-creative-parallel-contracts-v2.md) | 仅保留为历史记录；前端已改为单一负责人，本文不再约束职责、路由或实施顺序 | 历史资料 |
| [Strategy → Creative 开发契约 v2](./25-strategy-to-creative-development-contract-v2.md) | 冻结 StrategyPackage、CreativeHandoff、CreativeIntake、CreativeVideoIntake、稳定 Route ID 与三级 readiness 边界 | Strategy、Creative、后端、测试 |
| [Strategy → 品牌广告 Brief 断裂修复方案](./plans/2026-08-12-strategy-brand-brief-continuity-implementation-and-adversarial-review.md) | 消除已批准策略进入品牌广告后的重复 Brief 分析，统一编排、状态门禁、兼容迁移与反方评审 | 产品、Creative、Strategy、前端、后端、测试 |
| [爆款复刻 MVP 技术实施方案](./plans/2026-07-28-viral-remake-mvp-technical-plan.md) | Strategy 未接线时以 manual Intake 跑通 Creative 爆款分析、原创改写、Provider 生成、Assets 入库和检查闭环 | Creative、平台、前端、后端、测试 |
| [Kanon 前端 × Go 后端集成技术方案](./plans/2026-07-29-kanon-frontend-go-backend-integration-plan.md) | 冻结 Kanon 前端权威范围、cookies-platform Go 后端事实来源、适配 seam、Workbench 数据映射与分阶段验收 | 前端、后端、架构、测试 |
| [广告 AIGC 与 AI 混剪开发知识沉淀](./23-ad-aigc-remix-development-knowledge.md) | 从飞书 Base 重点资料沉淀素材工厂、AI 混剪、RenderJob、VLM 质检、Agent/RAG、评测和合规开发知识 | 产品、架构、后端、前端、算法、音视频研发 |
| [广告 AIGC 与 AI 混剪逐点技术方案](./24-ad-aigc-remix-technical-breakdown.md) | 将素材元数据、Shot、RenderJob、质检、爆款复刻、前贴、Agent、RAG、评测和反馈逐点拆成可研发落地的技术方案 | 产品、架构、后端、前端、算法、音视频研发 |
| [本地演示与测试数据手册](./25-local-demo-runbook.md) | 默认测试身份、兼容服务账号密码、演示数据导入、对象存储和验证步骤 | 演示、测试、研发、运维 |
| [广告策略库](./策略/README.md) | 素材洞察、漫剧制作、剪辑包装、商品与素材投放及全链路闭环策略 | 策略、创意、投放、数据、产品 |

## 系统划分

cookies 的四个模块按四个完整垂直系统建设：

| 系统 | 路由前缀 | 自有导航 | 核心数据所有权 |
| --- | --- | --- | --- |
| 需求与策略 | `/projects/:projectId/strategy/*` | 需求中心、策略任务、策略工作区、策略资产、研究、评审、能力运营 | StrategyWorkspace、Conversation、Brief、Strategy |
| 创意创作 | `/projects/:projectId/creative/*` | 创意任务、图文、视频、生成队列、评审、交付、创意运营 | CreativeTask、CreativeVersion、CreativePackage |
| 素材洞察 | `/projects/:projectId/insight/*` | 投前洞察、投后分析、分析素材库、内容分析、实验、经验、报告 | AssetFeature、AnalysisRun、Insight、Experience |
| 智能投放 | `/projects/:projectId/delivery/*` | 计划、执行、监控、优化、审批、证据 | DeliveryPlan、ChangeSet、PlatformEntity、Evidence |

四个系统不共享业务页面、业务状态机或数据库表。它们通过全局 Project 保持同一业务上下文，只复用 [共享基座](./05-shared-foundation.md)，并通过契约 API 与领域事件传递版本化产物。

Home 的 Project 列表、`/projects/:projectId/manage` 项目管理和 `/projects/:projectId/home` 项目工作台构成业务系统上层。Project 工作台是唯一总工作台；四个业务模块不再设置模块工作台，详细整改决策见 [Project 中心化页面路径整改规划](./22-project-centered-navigation-remediation-plan.md)。

共享知识库由 [ORAG](https://github.com/shikanon/orag) 实现，源码以 Git submodule 固定在 `third_party/orag`；四个业务系统只访问 cookies Knowledge Gateway，不直接依赖 ORAG 数据库或内部包。所有模型能力由 [统一模型 Provider](./07-unified-model-provider.md) 提供，默认使用火山引擎。

品牌、产品和广告项目由 [项目、品牌与产品域](./08-project-brand-domain.md) 统一拥有；媒体文件、广告数据、Agent 和 Computer Use 分别使用独立共享规格，避免把技术基座职责散落在四份 PRD 中。

## 统一约定

### 文档状态

- 草案：用于讨论，范围和方案可能变化。
- 评审中：核心范围已稳定，等待产品、技术、业务和合规确认。
- 已确认：可进入设计与研发拆解。
- 已上线：功能已交付，文档应同步真实行为。

### 需求优先级

- P0：完成核心业务闭环所必需，缺失则不能上线 MVP。
- P1：明显改善效率或效果，核心闭环稳定后交付。
- P2：增强型能力，根据数据和商业价值排期。

### 核心术语

| 术语 | 定义 |
| --- | --- |
| 广告项目（Project） | 围绕一个产品、活动或增长目标建立的全局工作空间，是需求、策略、创意、素材洞察和投放对象的共同上下文根。 |
| 策略工作区（StrategyWorkspace） | Project 内由需求与策略系统拥有的流程容器，承载对话、Brief、研究、策略、实验和评审。 |
| 广告任务（Campaign Task） | 一次从需求收集到投放复盘的完整业务闭环。 |
| Brief | 经过确认的结构化广告需求，是后续策略和创意的事实来源。 |
| 策略方案（Strategy） | 对受众、卖点、渠道、内容方向、预算和指标的可执行建议。 |
| 创意（Creative） | 图文或视频广告内容；视频区分电影感品牌广告和效果广告，效果广告首期包括数字人、广告前贴和爆款视频复刻，并通过独立素材剪辑完成通用后期制作。 |
| 素材（Asset） | 图片、视频、音频、文案、脚本等分析对象及其内容特征。 |
| 素材经验（Asset Insight） | 从素材内容、使用上下文和效果数据中归纳出的可复用结论。 |
| Skill | 为 Codex 提供广告领域工作流、规则、参考和脚本的可复用能力包。 |
| Computer Use | 由 Codex 操作已授权广告平台图形界面的执行能力。 |
| 投放计划（Delivery Plan） | 映射到广告平台的目标、预算、受众、版位、创意和排期配置。 |
| 闭环 | Brief 已确认，至少一个创意已上线，且效果数据已回流。 |

## 文档维护规则

1. 功能范围变化时，先更新对应 PRD，再更新研发任务。
2. 业务字段由所属系统维护；跨系统只传递稳定 ID、版本和必要快照，不建立共享业务表。
3. AI 输出必须可追溯到输入、模型版本、提示词版本和人工修改记录。
4. 预算、上线、暂停、扩量等影响真实账户的操作必须记录操作者和审计日志。
5. 每次正式评审后更新文档版本、状态和变更记录。
6. 每个业务系统独立维护导航、路由、权限、指标、API 和发布计划；公共能力进入共享基座前必须满足复用和业务无关条件。
