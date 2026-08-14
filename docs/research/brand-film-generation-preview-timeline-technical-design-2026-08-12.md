# 品牌广告 Phase 03 主播放器与镜头时间轴技术设计

> 日期：2026-08-12
> 状态：待产品评审，尚未按本文继续修改 Phase 03 代码
> 范围：创意创作 → 品牌广告 → 视频生成、反馈重试与片段锁定
> 目标：把当前生成页改为严格的纵向三层工作台——**主播放器 → 镜头时间轴 → 当前镜头详情**。

## 1. 结论

用户提出的目标是正确的，当前实现尚未完全达到。目标页面不是“播放器旁边放一个时间轴”，而是一个以完整视频为中心的只读审片工作台：

```text
┌──────────────────────────────────────────────────────────────┐
│                     主播放器 / 合成预览                       │
│                    画面始终是页面视觉中心                      │
└──────────────────────────────────────────────────────────────┘

00:00          00:05          00:10                       00:30
  │ 播放头
  ▼
┌──────────┬──────────┬──────────┬──────────┬──────────┬──────────┐
│ 镜头 01  │ 镜头 02  │ 镜头 03  │ 镜头 04  │ 镜头 05  │ 镜头 06  │
│ 缩略图   │ 缩略图   │ 缩略图   │ 缩略图   │ 缩略图   │ 缩略图   │
└──────────┴──────────┴──────────┴──────────┴──────────┴──────────┘
←                         横向拖动                           →

┌──────────────────────────────────────────────────────────────┐
│ 当前镜头详情：版本、状态、Prompt 摘要、反馈、重生成、锁定     │
└──────────────────────────────────────────────────────────────┘
```

核心决策：

1. 页面采用单列纵向结构，不再使用“左播放器 + 右时间轴/详情”的两列布局。
2. 合成前播放镜头自身的最新成功 Attempt；合成后始终播放 `PreviewAsset`，镜头选择只改变 `currentTime`，不切换视频 URL。
3. 时间轴不仅能点镜头，还能按点击位置精确 seek 到镜头内部时刻。
4. 播放、选择、播放头与横向滚动由一个深 module 管理，页面只消费统一状态和意图级命令。
5. Phase 03 是“生成审片时间轴”，不是素材剪辑器；不提供拖动排序、裁切、分割或改变镜头时长。这些能力归属素材剪辑模块。
6. 现有后端模型和接口足够支撑 P0，无需修改数据库或推翻 GenerationUnit/Attempt/PreviewAsset 结构。

## 2. 需求冻结

### 2.1 页面信息架构

必须按以下顺序渲染，三层均占满内容区宽度：

| 区域 | 职责 | 禁止事项 |
| --- | --- | --- |
| 主播放器 | 播放当前镜头或完整合成视频，显示当前时间、总时长和播放状态 | 不在左右两侧并列其他大型工作区，不重复渲染每个镜头的 `<video controls>` |
| 镜头时间轴 | 显示所有镜头、播放头、刻度、缩略图、状态和横向滚动 | 不修改镜头顺序、长度或 Prompt，不变成多轨剪辑器 |
| 当前镜头详情 | 显示选中镜头的生成版本、PromptPackage、Attempt、反馈、重生成与锁定 | 不同时展开所有镜头的详情，避免页面重新变长 |

页面底部保留整片级动作：全部镜头锁定后执行“合成预览”；已经合成后显示成片 Asset 版本并开放下一阶段。

### 2.2 镜头块内容

每个镜头块至少展示：

- 视频缩略图；无视频时显示与状态相符的占位图；
- 镜头编号和短名称，优先取 FilmPlan 中对应 Shot 的 `purpose`，缺失时退回 `shot_id`；
- 起止时间与时长；
- `待生成 / 排队中 / 生成中 / 已生成 / 失败 / 已锁定` 状态；
- 当前选中态；
- 快捷动作：失败或已生成但未锁定时显示“重生成”，首次未生成显示“生成”；
- 锁定图标或错误提示，但不把完整反馈表单塞进镜头块。

快捷动作必须阻止事件冒泡，不能在点击“重生成”时意外触发时间轴 seek。

### 2.3 时间比例与滚动

- 时间轴使用统一 `pixelsPerSecond`，镜头宽度公式为 `durationSeconds × pixelsPerSecond`。
- 相同时长必须得到相同宽度；不允许按内容长度或卡片网格平均分配。
- 为保证可点击性设置 `minClipWidth`，但视觉上仍需用刻度或内部进度表达真实时长，避免短镜头看起来比长镜头更长。
- 画布宽度为 `max(viewportWidth, masterDuration × pixelsPerSecond)`。
- 超出视口时由一个显式横向滚动容器承载，底部滚动条始终可见，使用 `scrollbar-gutter: stable` 避免布局跳动。
- 选中镜头或播放进入新镜头时，只在目标不完全可见时调用 `scrollIntoView({ inline: 'nearest' })`；不能每次 `timeupdate` 都触发平滑滚动。

### 2.4 点击和播放规则

#### 点击镜头

- 合成前：选择该镜头，主播放器加载该单元最新可播放 Attempt，从局部 `0s` 开始播放。
- 合成后：保持主播放器 `src = PreviewAsset`，设置 `currentTime = unit.start_second` 后播放。

#### 点击镜头内部位置

设点击横坐标为 `clientX`，镜头元素左边界为 `rect.left`：

```text
ratio = clamp((clientX - rect.left) / rect.width, 0, 1)
targetGlobal = unit.start + ratio × (unit.end - unit.start)
```

- 合成前播放器的局部时间为 `targetGlobal - unit.start`。
- 合成后播放器的时间就是 `targetGlobal`。
- 点击快捷按钮、状态菜单和滚动条时不得触发 seek。

#### 播放同步

- 使用视频元素的 `timeupdate` 更新全局 `playheadMs`；若浏览器支持，可用 `requestVideoFrameCallback` 提升播放头流畅度，但它不是 P0 必需条件。
- 合成前：`globalTime = unit.start + video.currentTime`。
- 合成后：`globalTime = video.currentTime`。
- `activeUnit` 使用半开区间 `[start, end)`；视频尾点落在最后一个镜头。
- 播放跨越镜头边界后自动更新选中态和详情；不能重设 `currentTime`，否则会产生回跳。
- 合成前播放到当前镜头结尾后停止，不自动拼接播放下一个未合成资源；完整连续播放由合成预览负责。

> P1 可选增强：复用素材剪辑 `VideoPreviewPlayer` 的 source switching，让合成前连续预览多个已生成片段；遇到未生成或失败片段必须暂停并明确提示，不能静默跳过。该增强不改变 P0“点击镜头即可播放对应片段”的验收目标。

## 3. 当前代码与架构事实

### 3.1 后端数据已经具备

`BrandFilmGeneration` 已持久化 `MasterDurationMS`、`Units` 和最终 `PreviewAsset`；每个 `BrandFilmGenerationUnit` 已有镜头 ID、起止秒、PromptPackage、Attempt 列表和 `LockedAttemptID`。[来源：`internal/systems/creative/brand_film.go:376-427`]

当前权威规则已经是严格的“一 Shot 一 GenerationUnit”，并以 `one_shot_one_generation_unit` 作为规划原因码；15 秒对应 3 个镜头、30 秒对应 6 个镜头，自定义时长按约 5 秒一镜头向上取整。[来源：`internal/systems/creative/brand_film_audio.go:61-106`；`internal/systems/creative/CONTEXT.md:63-69`] 2026-07-31 早期调研中“把多个短 Shot 聚合成 4–15 秒 Unit”的建议属于历史方案，不能重新引入当前实现。

`ComposeBrandFilmPreview` 只消费每个单元锁定的成功 Attempt，按单元时长合成，并把输出入库为新的项目 AssetVersionRef。[来源：`internal/systems/creative/brand_film_generation.go:355-402`]

OpenAPI 已明确：确认分镜后按 GenerationUnit 冻结 PromptPackage；生成命令为单元创建 Seedance Attempt；合成预览已有独立命令。[来源：`api/openapi/creative-v1.yaml:526-604`]

因此 P0 不需要新增接口。后端仍然拥有业务事实，浏览器只维护临时播放状态。

### 3.2 可复用的前端能力

素材剪辑已经提供成熟的时间换算和播放同步范式：

- `VideoPreviewPlayer` 把全局播放头换算为片段源时间，并在播放时通过 `onTimeUpdate` 回写播放头。[来源：`src/features/video-editing/VideoPreviewPlayer.tsx:6-65`]
- `VideoTimeline` 已实现由指针横坐标、滚动偏移和 `pixelsPerMs` 计算 seek 时间，以及可拖动播放头。[来源：`src/features/video-editing/VideoTimeline.tsx:119-137,161-218`]
- 现有样式已经证明 `overflow-x: auto`、稳定滚动条、时间尺和播放头在项目设计系统中可行。[来源：`src/styles.css:1459-1481`]

复用应限于算法和交互原则，不直接复用素材剪辑的多轨编辑 interface，避免把不属于品牌广告的 trim/split/move 操作暴露出来。

### 3.3 当前 Phase 03 的偏差

当前 `BrandGenerationReview` 已有选中镜头、合成前/后时间映射、播放高亮和滚动的雏形，但布局是两列：播放器在左，时间轴与详情在右；不符合冻结的纵向三层结构。[来源：`src/components/BrandFilmWorkspace.tsx:701-809`；`src/styles.css:5830-5895`]

还存在以下缺口：

1. 镜头内部点击只调用 `focusUnit(unit)`，始终跳到起点，尚不能按点击位置 seek。[来源：`src/components/BrandFilmWorkspace.tsx:726-735,784`]
2. 镜头块没有生成/重生成快捷入口，动作只存在详情区。
3. 镜头名称只有 `shot_id`，没有把 FilmPlan Shot 的 `purpose` 映射进时间轴。
4. 时间轴宽度存在 `minWidth` 与单元宽度分别计算的双重规则，可能产生尺宽和片段总宽不一致；应统一由 layout module 返回几何结果。
5. 播放状态、选择状态、时间换算、滚动策略仍集中在页面组件内部，interface 偏浅，后续迭代容易再次散落。
6. 当前播放器只在已有媒体时存在，导致待生成镜头和已生成镜头切换时 DOM 类型变化；正式实现应保持稳定播放器画布，只切换内部媒体/占位状态。
7. 当前展示资源简单取 `attempts.at(-1)`，没有遵循“锁定 Attempt > 最近成功 Attempt > 最新 Attempt”的确定性优先级。最新一次重试失败时，页面可能隐藏此前仍可播放的成功候选；已锁定镜头也可能显示非锁定版本。[来源：`src/components/BrandFilmWorkspace.tsx:715-718,773-786`]
8. 当前时间尺只有起点、中点和终点，尚未按固定秒间隔与镜头边界生成刻度。[来源：`src/components/BrandFilmWorkspace.tsx:768-770`]

## 4. 目标 module 与 seam

### 4.1 `BrandFilmReviewTimeline` 深 module

建议新增目录：

```text
src/features/brand-film/review-timeline/
├── model.ts                 # 只读 view model 与状态
├── controller.ts            # 时间映射、选择、播放模式、active unit
├── layout.ts                # px/time 几何、刻度、点击 seek
├── useReviewPlayback.ts     # video element 与状态同步
├── ReviewPlayer.tsx
├── ReviewTimeline.tsx
├── ReviewInspector.tsx
└── review-timeline.css
```

对 `BrandFilmWorkspace` 暴露一个小 interface：

```ts
type BrandFilmReviewTimelineProps = {
  masterDurationMs: number
  units: ReviewUnit[]
  composedPreviewUrl?: string
  selectedUnitId?: string
  busyCommand?: string
  onGenerate(unitId: string, feedback?: string): void
  onLock(unitId: string, attemptId: string): void
}
```

页面不应该知道：

- 合成前和合成后的时间如何换算；
- 点击像素如何转换成全局毫秒；
- 哪个镜头是当前镜头；
- 何时自动滚动；
- 切换 `src` 后如何恢复局部播放位置。

这些复杂度全部藏在 module 后面。删除该 module 时，上述复杂度会重新散落到播放器、时间轴、详情和测试中，因此它是有真实 depth 的 module，而不是样式包装。

### 4.2 View model 适配

后端类型不直接穿透渲染层，先在一个纯函数中转为：

```ts
type ReviewUnit = {
  id: string
  order: number
  name: string
  startMs: number
  endMs: number
  status: 'idle' | 'queued' | 'running' | 'succeeded' | 'failed' | 'locked'
  thumbnailUrl?: string
  previewUrl?: string
  latestAttempt?: ReviewAttempt
  lockedAttemptId?: string
  promptPackage: { revision: number; hash: string; summary?: string }
}
```

适配规则必须集中测试：

- Locked 优先于 Attempt 状态；
- 最新成功 Attempt 决定合成前可播放资源，不能因为最新一次重试失败就隐藏此前可用候选；
- 展示 Attempt 的确定性优先级为 `locked attempt > latest succeeded attempt > latest attempt`；
- 缩略图优先使用专用 poster；P0 可退回视频首帧；
- 名称从 FilmPlan 的 Shot ID 映射到 `purpose`；
- 后端异常间隙或重叠需标记为不可合成错误，不在 UI 静默修复。

### 4.3 Playback state

```ts
type PlaybackMode = 'unit' | 'composed'

type ReviewPlaybackState = {
  mode: PlaybackMode
  selectedUnitId: string
  activeUnitId: string
  playheadMs: number
  playing: boolean
}
```

不把 `HTMLVideoElement`、临时签名 URL 或滚动位置写入服务端。它们只属于浏览器会话状态；刷新后根据持久化 Generation 数据恢复到第一个未锁定镜头，否则恢复第一个镜头。

## 5. UI 详细规格

### 5.1 主播放器

- 宽度：内容区 100%；桌面端画布高度建议 `min(56vh, 620px)`，最低 360px。
- 竖屏广告采用 `object-fit: contain`，左右使用深色画布，不裁切商品和 Logo。
- 顶部左侧显示“完整合成预览”或“镜头 03 预览”；右侧显示 `10.0s / 30.0s` 和模式说明。
- 没有视频时画布仍保持相同高度，显示状态、预计动作和“生成此镜头”按钮。
- 不再为每个镜头创建可见的原生 `<video controls>`；时间轴缩略图使用 poster/image，避免 6～10 个视频同时解码。

### 5.2 时间轴

- 单轨、只读、横向滚动；时间尺与 clip 使用同一内容坐标系。
- 默认 `pixelsPerSecond` 由视口和时长共同决定：15 秒尽量完整显示，30 秒默认显示约 3～4 个镜头，更多时横向滚动。
- 每 5 秒主刻度，必要时每 1 秒次刻度；总时长来自 `MasterDurationMS`，不从最后一个 DOM 卡片推断。
- 播放头是同一画布上的绝对定位元素，可点击/拖动；P0 至少支持点击 seek，P1 再支持拖动播放头。
- Clip 选中态、播放态和已锁定态须可同时表达，不能只依赖一种边框颜色：
  - 选中：蓝色外框；
  - 正在播放：顶部蓝色进度条或播放图标；
  - 已锁定：绿色锁图标与浅绿状态；
  - 失败：红色状态条；
  - 排队/生成：轻量动态进度，不改变卡片宽度。

### 5.3 当前镜头详情

- 单一详情面板位于时间轴下方，默认折叠技术字段、展开用户动作。
- 首行：镜头名称、起止时间、状态、版本选择。
- 内容：当前候选预览摘要、PromptPackage 修订、Attempt 历史、错误原因。
- 操作：首次生成、填写反馈重生成、锁定候选；已锁定时明确提示“修改将创建新 Attempt/版本”，不得静默覆盖。
- 对于多个 Attempt，应允许用户查看历史并锁定任一成功 Attempt；当前后端 `LockBrandFilmGenerationUnit` 已接受明确 Attempt ID，领域能力已具备。[来源：`internal/systems/creative/brand_film_generation.go:315-353`]

## 6. 状态与命令矩阵

| 单元状态 | 主播放器 | 镜头块 | 详情操作 |
| --- | --- | --- | --- |
| 未生成 | 占位画布 | 待生成 | 生成此镜头 |
| queued | 占位 + 排队时间 | 排队中 | 禁止重复提交，可取消暂不在 P0 |
| running | 占位 + 生成中 | 动态状态 | 禁止重复提交 |
| succeeded | 播放最新可用候选 | 已生成 | 反馈重生成、锁定 |
| failed 且无成功历史 | 错误占位 | 失败 | 查看原因、重新生成 |
| failed 但有成功历史 | 仍播放最近成功候选 | “重试失败” | 可锁定旧候选或继续重试 |
| locked | 播放锁定候选 | 已锁定 | 查看版本；重新打开属于后续版本策略 |
| composed | 始终播放完整 PreviewAsset | 所有镜头仍可点 | 点击只 seek，不切换 src |

命令 busy 状态必须按单元寻址，例如 `generate:unit_03`、`lock:unit_02`，一个镜头生成时不能锁死其他镜头的播放和查看。这个方向也符合既有前端改造文档对 UI4 的要求。[来源：`docs/research/brand-film-frontend-workbench-redesign-technical-plan-2026-08-05.md:358-367`]

## 7. 性能与可访问性

- 时间轴只加载缩略图，不并行渲染多个带 controls 的视频。
- 10 个镜头规模不需要虚拟化；超过 30 个镜头再评估窗口化，避免过早复杂化。
- 播放头的高频位置可存 ref，并以 `requestAnimationFrame` 批量更新视觉；React state 只在跨镜头或用户可见时间文本需要变化时更新。
- 所有镜头块是 button，提供 `aria-label`、`aria-pressed` 和状态文本。
- 键盘：左右方向键切换镜头，Enter 播放，Space 播放/暂停；焦点镜头自动进入可视区域。
- 支持 `prefers-reduced-motion`，关闭自动平滑滚动和非必要动画。
- 时间轴不能造成页面级横向溢出；只有时间轴 viewport 自身可横向滚动。

## 8. 实施阶段

### P0：结构与 controller

1. 删除 Phase 03 两列布局，建立单列三层 DOM。
2. 抽取 `ReviewUnit` adapter、timeline layout、playback controller。
3. 实现合成前/后两种播放模式、镜头起点跳转、播放高亮。
4. 把现有 GenerationUnitActions 迁入单一 Inspector。

完成条件：15 秒和 30 秒任务均符合“上播放器、中时间轴、下详情”；无页面级横向溢出。

### P1：精确 seek 与镜头快捷动作

1. 点击镜头内部位置精确 seek。
2. 时间轴空白与播放头点击 seek。
3. Clip 内加入生成/重生成快捷按钮并阻止事件冒泡。
4. 自动滚动采用 nearest 策略，并补键盘导航。

完成条件：点击 10s–15s 镜头 50% 位置，播放器到 12.5s；播放跨边界后选中态自动切换。

### P2：版本审阅与表现完善

1. Attempt 历史切换和锁定任一成功版本。
2. 真实缩略图/poster 生成与缓存。
3. 播放头拖动、缩放档位和更细时间尺。
4. 生成耗时、排队时长和失败恢复提示。

完成条件：失败重试不隐藏旧成功候选，用户可明确选择并锁定版本。

## 9. 测试方案

### 9.1 纯函数测试

- `global ↔ local time` 双向换算；
- 边界时间 `0 / unit.start / unit.end / masterDuration`；
- 不等长镜头的像素宽度；
- 点击坐标包含 scrollLeft 时的 seek；
- active unit 半开区间和最后尾点；
- latest playable Attempt 与状态优先级；
- 尺宽等于 clip 几何总宽，不出现漂移。

### 9.2 React 交互测试

- 合成前点击镜头 02 后 `src` 切换到镜头 Attempt，时间从局部 0 开始；
- 合成后点击镜头 03，`src` 不变且 `currentTime = 10`；
- 点击镜头 03 的 50% 位置跳到 12.5s；
- 播放跨 10s 后高亮镜头 03、Inspector 同步；
- 点击重生成不会触发 seek；
- 生成中只禁用对应单元命令。

### 9.3 浏览器验收

固定验收数据：

- 15 秒 / 3 镜头；
- 30 秒 / 6 镜头；
- 自定义 47 秒 / 10 镜头；
- 混合状态：成功、queued、failed、locked；
- 合成前与合成后各一套。

验收视口至少覆盖 1440×900、1280×720、移动窄屏。检查：播放器尺寸、时间轴滚动、滚动条可达、页面无横向溢出、详情不被固定底栏遮挡、控制台无错误。

## 10. 非目标

本次不做：

- 拖动镜头重新排序；
- 裁切、分割或改变镜头时长；
- 多轨视频/字幕/音频编辑；
- 转场、速度、滤镜；
- 用浏览器临时 Blob 或签名 URL替代 Project AssetVersion；
- 修改 Seedance、PromptPackage 或合成后端契约。

用户需要上述能力时，从完整品牌成片进入现有素材剪辑模块。保持这条领域边界能避免 Phase 03 演变为第二套不完整的视频编辑器。

## 11. 最终验收定义

只有同时满足以下条件才算完成：

1. 页面严格是纵向“主播放器 → 时间轴 → 当前镜头详情”；
2. 页面内只有一个主播放控制面；
3. 镜头宽度按时长比例计算；
4. 时间轴可横向拖动且不撑宽整个页面；
5. 点击镜头起点和镜头内部位置均能正确 seek；
6. 合成前切换镜头资源，合成后保持成片资源不变；
7. 播放头、选中镜头、详情和自动滚动保持同步；
8. 每个镜头块包含缩略图、名称、时间、状态、选中态和快捷生成入口；
9. 反馈、重生成、版本和锁定只集中显示当前镜头；
10. 3、6、10 镜头任务通过自动测试与真实浏览器验收。

本文是后续 Phase 03 重构的唯一目标基线。若实现与第 2 节或第 11 节不一致，应视为未完成，而不是视觉偏好差异。
