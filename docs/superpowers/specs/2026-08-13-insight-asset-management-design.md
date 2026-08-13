# 素材洞察 · 素材管理基建设计

**日期**：2026-08-13
**状态**：待评审
**范围**：素材洞察模块「素材」入口的**底层基建**。界面入口怎么摆不在本文范围内（使用者原话：「入口能调，但是基建难，先把基建做了」）。

---

## 一、要解决的问题

洞察的「素材」这一屏现在只回答一个问题：**这条素材还差什么才能进复盘**。

它是一张分析前置检查清单：四个缺口队列（对不上号 / 待提取变量 / 提取失败 / 等人复审），标题写的就是「手上有哪些素材，它们还差什么才能进复盘」（`src/components/insight/assets/AssetsPage.tsx:31`）。

使用者要的不止这个。原话两句：

> 「我指的还有其他模块产生的素材，也需要管理，他们是属于引用」
> 「我只看最终的成功项目素材嘛，因为只有最后的素材视频才回去投流，才会有数据分析……其他的素材，就当做是素材管理中心那样了」

也就是要把两件事分开。

---

## 二、两类素材

| | **分析素材** | **平台素材（台账）** |
|---|---|---|
| 它是什么 | 能投流、有投放数据、要出结论的成品 | 平台上存在的素材，一份引用记录 |
| 核心问题 | 还差什么才能进复盘 | 它是什么、在哪、谁的、第几版、跟谁有关 |
| 规模 | 一轮几十条 | 几千条起 |
| 会不会催人做事 | 会（缺口队列、红点） | **不会**，安静躺着 |
| 能不能改 | 洞察这边的标题/变量可以改 | 只读，文件归各模块所有 |
| 收录口径 | 只收成品（创意已批准的、米云正式导入的） | 收全平台，中间产物除外（见基建二） |

两者靠一个显式动作连接：台账里某条素材投了、有数据了，**把它拉进分析**，它才开始被追问缺口。

**外部证据不在这两类里。** 它是竞品参照，单独一张表、单独存储前缀、永不写进素材表（`internal/systems/insights/external.go:12-32`，2026-08-04 与创意组确认的合规约束）。它既不是分析素材也不是平台素材，它不是资产。

---

## 三、为什么台账必须建在洞察这边

排查了平台素材库（`internal/platform/assets`）能提供什么：

| | 内容 |
|---|---|
| **能给** | 按 Project 拉一页（≤100 条）、技术参数很全（kind/mime/大小/时长/尺寸/编码/来自哪次渲染）、逐条签名预览（5 分钟 TTL）、血缘关系（`derived_from`/`generated_from`/`returned_from`）、单条按 `AssetVersionRef` 精确反查 |
| **给不了** | **标题**、缩略图、创建者、翻页游标、任何筛选维度、跨 Project 全量 |

最要命的一条：**平台素材没有标题**。`Asset` 和 `AssetVersion` 两个结构体（`internal/platform/assets/model.go:28-70`）通篇没有 title 或文件名列，`projectAssetSelect`（`mysql_repository.go:782`）一个名字列都不选。文件名只活在上传会话上（`model.go:242`），而**创意渲染出来的成品根本不走上传会话**，那批素材连文件名都没有。

洞察现在列表里显示的标题，是洞察自己表里存的（`insight_assets.title`），登记时拼出来的。

反过来看，洞察自己的素材列表已经支持按状态 / 类型 / 来源 / 血缘多值筛选（`AssetFilter`，`internal/systems/insights/assets.go:497-503`）——**查询能力本来就强于平台素材库本身**。

**结论**：台账建在洞察侧，平台当「原件与技术参数的按需反查源」。这不是绕路，是唯一可行的位置。

---

## 四、四块基建

### 基建一：身份与分析状态分离

**现状**：`insight_assets` 只有一个 `analysis_status`，八个取值全部落在分析流水线上——待数据 / 待匹配 / 可分析 / 分析中 / 待确认 / 已确认 / 待复审 / 已失效（`migrations/insights/20260729103000_insight_asset_index.up.sql:38`、`internal/systems/insights/assets.go:22-29`）。

一条素材只要进表就被推上流水线，**没有「只是收着」的位置**。后果：创意做完、投都没投过的素材，进来就是「待数据」，堆在红色队列里谁也消不掉——它本来就不欠数据。红点永远亮着，久了人就不看红点了。

**改动**：新增一列 `role`，两个取值：

| 取值 | 含义 |
|---|---|
| `ledger` | 台账。收录在册，不参与任何缺口计算、不进队列、不产生红点 |
| `analysis` | 分析对象。推上流水线，`analysis_status` 才有意义 |

**为什么不在 `analysis_status` 上加第九态**：身份（要不要分析）和进度（分析到哪一步）是两个正交的维度。塞成一个枚举，所有状态流转判断都要多一个特例分支，且「台账态的素材是可分析还是不可分析」这种问题无解。

**约束**：

- `role='ledger'` 的素材不得写入任何特征（`insight_asset_features` 的写入要加这道门）。
- **`analysis_status` 保持原值不清零。** 队列与红点一律按 `role='analysis'` 过滤，而不是靠把状态归零来实现。这样「退回台账 → 再拉进分析」是无损的，素材记得自己曾经走到哪一步。代价是数据上会出现「台账态 + 已确认」这种组合，界面上按 role 显示即可，不冲突。
- `ledger → analysis`：显式动作「拉进分析」，需要 `insights.write`。
- `analysis → ledger`：允许，但**该素材不得被任何已提交的复盘引用过**。已经拿它出过结论的素材退回台账，会让那份复盘的证据链断掉。
- 存量数据全部回填 `analysis`，现状行为不变。

**影响面**：`AssetFilter` 加 `Roles` 维度；`OverviewView` 的四个队列与对账不变量（`explainable.length + 各类 unaccounted = live.length`）要按 `role='analysis'` 的子集重算；HTTP 层加查询参数。

---

### 基建二：台账收录通路

**现状**：只有一条路——人打开界面，点「从创意导入」，前端拉创意版本列表、自己过滤出已批准的、一条条登记过去（`src/components/insight/assets/ImportFromCreative.tsx:41-65`）。手工的、只覆盖创意、只覆盖已批准。

#### 为什么不接事件

`asset.ready.v1` 事件确实在发（`internal/platform/assets/upload_service.go:805-811`），但：

- 它落进的是 `assets_outbox` 表，**全仓没有任何 SELECT**——只有一处 INSERT 和两处测试清理。
- 共享的 `event_outbox` 表也一样：`eventoutbox.Dispatcher` 在生产代码中从未被实例化，`Publisher` 接口没有任何生产实现，`cmd/` 目录对 `eventoutbox` 零引用。
- 两张表的列名还不兼容（`aggregate_*` vs `subject_*`），Dispatcher 的 SQL 写死了 `FROM event_outbox`。

**全仓没有任何一个平台事件消费方。** 接事件等于先把整套事件投递基础设施从零建起来——那是一个独立项目，不该捆在这件事上。

#### 采用的做法：反向注入适配器

平台所有素材入库路径最后都收敛到**同一个函数** `ingestStoredObject`（`internal/platform/assets/upload_service.go:691-823`），六条路径全覆盖：

| 路径 | 位置 |
|---|---|
| 上传 finalize | `upload_service.go:175` |
| 渲染视频入库 | `upload_service.go:348` |
| 派生图片 / 派生音频 | `upload_service.go:440` / `:516` |
| 渲染图片入库 | `upload_service.go:614` |
| 模型产物回收 | `generated_intake_service.go:203` |
| 外部导入 | `external_import.go:143` |

在 `internal/integrations/` 下加一个适配器包（本仓已有 10 个同类包，装配见 `cmd/cookies-api/main.go:524`），由 `main.go` 把洞察服务作为一个**可选 hook** 注入给 assets 侧。assets 只依赖自己定义的窄接口，洞察实现它——分层不反向。

#### 必须先定的四件事

**1. 收录口径。** `ingestStoredObject` 是「每一个二进制入库」都过，包括渲染中间产物。全量收录会让台账被中间物淹没。按 `contract.AssetSourceType`（`internal/platform/contract/platform_events.go:18-26`）分：

| 来源 | 收不收 | 理由 |
|---|---|---|
| `upload` | ✅ | 人传的，是素材 |
| `provider_generated` | ✅ | 模型产出的成品 |
| `rendered` | ✅ | 渲染成品，就是要投的那个 |
| `imported` | ✅ | 外部导入（米云走这条） |
| `captured` | ✅ | 采集的 |
| `derived` | ❌ | 派生中间物（视频代理、波形、海报帧）。它是别的素材的附属品，不是独立素材 |

**2. 失败策略：吞掉，只记日志。** 洞察登记失败**不得**回滚素材上传。素材上传是平台的核心动作，不能因为一个下游台账挂了而失败。本仓既定原则（`docs/research/xiaohongshu-stage-1-and-adapter-compatibility-research.md:75`）：消费者不存在时不阻塞写入。

**3. 标题从哪来。** 平台没有标题。适配器按优先级取：上传会话文件名 → 渲染任务反查出的创意任务名 → 结构化兜底（`未命名 · <类型> · <日期>`）。台账界面允许人改标题——**改的是洞察这边的标题，不动平台**。

**4. 幂等。** `idx_insight_assets_platform`（migration `:52`）只是普通索引，不是唯一键，并发下会重复插同一条平台素材。要加唯一约束 `(organization_id, platform_asset_id, platform_asset_version)`（含 NULL 行不受约束，正好——手工登记的素材没有平台引用，不该被这条约束管）。

#### 存量回填

新装的钩子只管新入库的。已经在平台里的素材需要一次性回填：给 `cmd/cookies-maintain` 加一个子命令（现有结构见 `cmd/cookies-maintain/main.go:38-42` 的 `purge-empty-drafts`），按 Project 遍历、幂等可重跑。

---

### 基建三：台账的查询能力

收得进来还得查得出来。台账几千条起，而现有的读接口是照着「一轮几十条分析素材」设计的。

**现状**：

- `AssetFilter`（`internal/systems/insights/assets.go:497-503`）有 `Statuses` / `AssetTypes` / `SourceKinds` / `LineageID` / `Limit`——**没有游标，没有标题搜索**。
- 前端总览拉数时传的是**空筛选**，然后 `slice(0, 8)` 写死只显示 8 条（`src/components/insight/assets/OverviewView.tsx`）。几千条台账上这么干，等于把整张表拉进浏览器再扔掉 99%。
- `listInsightAssetLineage` 接口早就存在（`src/data/api.ts`），**前端从未调用过**。血缘是台账最有价值的一列，现在是通的但没接。

**改动**：

1. **游标分页**。`AssetFilter` 加 `Cursor`，返回加 `NextCursor`。游标按 `(updated_at, id)` 编码——现有索引 `idx_insight_assets_project (organization_id, project_id, updated_at)`（migration `:49`）正好支撑，不用加索引。不用 offset：台账在持续收录，offset 翻页会漏也会重。
2. **按标题搜索**。`AssetFilter` 加 `Query`，对 `title` 做前缀/包含匹配。规模到十万级再考虑全文索引，现阶段 `LIKE` 够用且行为可预期。
3. **`role` 维度**（基建一的配套）。
4. **前端接上血缘**：台账详情里调 `listInsightAssetLineage`，把「这条素材的历次版本」显示出来。接口现成，纯前端工作。

**为什么不复用平台的列表接口**：`ListProjectAssets`（`internal/platform/assets/mysql_repository.go:627-648`）只有 `LIMIT`，没有 offset、没有游标、没有任何筛选，服务层还把上限钳到 100。项目素材一超过 100 条，那个端点就无法完整遍历。

---

### 基建四：缩略图

**现状**：平台的封面字段 `MediaMetadata.PosterFrameRef`（`model.go:196`）**在生产环境永远是空的**——`MediaProbe` 接口只有 `UnconfiguredMediaProbe`（恒返回错误）和测试替身 `StaticMediaProbe` 两个实现（`media_probe.go:20-38`），降级路径 `mediaMetadataFromVideo` / `mediaMetadataFromAudio` 都不设置它。

它还是个不透明字符串，不是 URL 也不是 ref，没有任何端点能把它换成可访问地址。

平台其实**建好了派生物那整套脚手架**：`DerivativeProfile` 常量里就有 `poster_v1`（`derivatives.go:17`），`EnsureDerivative` / `RetryDerivative` 都实现了，表也建了（`migrations/assets/20260811101000_asset_derivatives.up.sql`）。但**没有任何生产调用方、没有 HTTP 路由、没有调度**。

好消息：**ffmpeg/ffprobe 在这套系统里是可用的**——`main.go:63-64` 解析路径，`FFprobeVideoProbe` / `FFprobeAudioProbe` 已经接上（`main.go:133-135`），渲染镜像里装了 ffmpeg 8.1.2（`deployments/render-worker/Dockerfile:13`），另有六处服务在用 `FFmpegPath`。所以这不是造一个不存在的能力，是**接线**。

**要做的三件事**：

1. 一个 `poster_v1` 的抽帧实现（ffmpeg 取首帧或中间帧，复用现有的 `WorkRoot` 约定）。
2. 一个触发时机——素材入库后异步生成，挂在既有的 `jobruntime` 作业池上（洞察的米云采集就是这么挂的，`main.go:565-566`），不阻塞入库。
3. 一个取用端点，把 `poster_v1` 派生物换成可访问 URL。

**为什么不用现成的 `preview` 端点**：它签的是原件（`upload_service.go:238-256`），TTL 5 分钟，要求素材 ready 且过用途授权。列表页几千条素材逐条签名 = N+1 请求 + 5 分钟后全部失效。做不了台账的缩略图。

**这一块动的是 `internal/platform/assets`，不是洞察模块。** 需要跟平台/创意那边打招呼。

**退路**（谈不拢时）：台账列表不放图，只放类型图标 + 技术参数。立刻能做，但「素材中心」没有图就废了一半。

---

### 基建五：米云正名

**现状**：素材来源三档 `creative` / `upload` / `external`（`assets.go:66-70`，migration `:57` 的 CHECK 约束）。米云采集回来的素材被塞进 `external`（`miyun_crawl.go:609`、`miyun_service.go:468`、`miyun_return_service.go:142`）。

但米云素材带着平台素材 ID、能投、是货真价实的平台内素材。而界面上「外部素材」那个视图给人看的是竞品证据（另一张表）——**两个「外部」指的不是同一样东西**，人以为米云素材在那一屏，其实不在。

**改动**：加一档 `miyun`。迁移改 CHECK 约束，并回填现有数据（`source_ref` 以 `miyun://` 开头的行）。

**影响面**：`AssetSourceKind` 常量与 `valid()`、前端来源筛选、`ExternalView` 的措辞。改动最小的一块，但不做的话台账上的来源列从第一天就是错的。

---

## 五、不在本期范围

- 「素材」入口的导航结构与界面布局（使用者：入口能调，后面再说）。
- 跨 Project 的全组织素材视图。平台侧所有查询都以 `(organization_id, project_id)` 为界，没有全组织扫描接口。
- 统一两张 outbox 表 + 建事件投递基础设施。正确的长期方向，但是独立项目。
- 平台素材库补 title 列。台账用洞察自己的标题，不推动平台改。

---

## 六、顺带发现的两个既有问题（不在本期修，但要登记）

**1. 外部证据的到期清理从来没有执行过。** `Service.PurgeExpiredExternalOriginals`（`external.go:217-224`）注释写着「由维护命令调用」，但**全仓没有任何调用方**。到期日在导入时算好存下了，数据是对的，只是没人来收。

`external.go:23` 那条「到期删原件只留派生物」的合规约束，目前只存在于代码注释里——**外部素材原件事实上永不删除**。这是合规问题，应当单开一条修。

**2. 事件 schema 已经漂移。** `api/events/asset-ready-v1.schema.json:31` 的 `source_type` 枚举只有四个值且 `additionalProperties: false`，而 Go 侧多出 `rendered` / `derived` 两种来源和两个字段。渲染/派生素材发出的事件按自己的 schema 校验会失败。现在没人校验所以不报错，谁先接这个事件谁先撞上。

---

## 七、验收标准

1. 一条 `role='ledger'` 的素材，在「素材」总览的四个缺口队列里**一条都不出现**，不产生任何红点。
2. 把它拉进分析后，它按现有的八态流转，行为与今天完全一致。
3. 平台新入库一条渲染成品，无需任何人操作，台账里出现对应记录，带标题、来源、技术参数。
4. 派生中间物（`derived`）入库时，台账里**不出现**。
5. 洞察服务不可用时，平台素材上传照常成功。
6. 同一条平台素材重复触发收录，台账里只有一条。
7. 存量回填命令可重复执行，不产生重复记录。
8. 米云素材在来源列显示「米云」，不显示「外部引用」；竞品证据仍在自己的表里，不出现在台账中。
9. 台账列表能显示缩略图（或退路：类型图标），列表规模 1000 条时首屏不发起 1000 次请求。
10. `go test ./...` 与 `npm test` 全绿；前端 `npx tsc --noEmit` 无错。
