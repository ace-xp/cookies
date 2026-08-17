# 品牌广告 AI 声音设计改造技术方案

> 日期：2026-08-17
> 状态：调研完成，待评审
> 范围：品牌广告的“声音导演”从旁白/TTS 工作台改造成无旁白的 AI 音乐、环境氛围与镜头音效工作台。

## 1. 决策摘要

本次不再把品牌广告的声音阶段定义为“配音”。它应被定义为 **AI 声音设计（AI Sound Design）**：在视觉成片锁定后，基于已确认的声音意图、镜头表与合成预览，自动规划并生成 AI 品牌音乐、环境氛围和镜头音效；用户可在时间轴上试听、编辑描述、局部重新生成、调节混音并输出带声音的最终视频。

以下是本方案的确定决策：

1. 新品牌广告不生成数字人配音、口播或 TTS；不展示音色、发音词典、旁白时长适配和语言版配音。
2. 剧本分镜阶段保留高层 **声音意图**，不保留实际声音素材和混音参数。声音意图由“音乐氛围与音乐意图”“音效重点”“原视频声音策略”组成。
3. 声音设计阶段拥有真实声音资产：AI 音乐、AI 环境声、AI 镜头音效；Seedance 原声为可选轨道，默认静音。
4. 默认每个声音事件只生成一个候选；不满意时局部重新生成，历史 Attempt 保留可回退，避免一次生成多候选造成费用与审核负担。
5. 现有 `AudioMixVersion` 的不可变修订、`AudioGenerationAttempt`、项目资产入库和 FFmpeg 混音渲染应保留并扩展；不能推翻已经可用的持久化与混音闭环。
6. 现有 MiniMax TTS 不是 AI 音乐/音效生成能力。新方案需要一个独立的 `SoundAssetGenerator` seam；在真实供应商配置前，只能使用明确标记的 Fixture，不能把 Fixture 伪装成 AI 音效。

## 2. 目标体验

### 2.1 用户看到的流程

```text
确认 Brief / 创意 / 剧本分镜
  └─ 仅确认声音意图：音乐氛围、音效重点、原声策略
        ↓
锁定视觉成片
        ↓
AI 声音设计：自动输出可编辑的声音事件方案
        ↓
生成 AI 音乐、环境声、镜头音效
        ↓
在时间轴试听、修改描述、局部重生成与混音
        ↓
FFmpeg 输出带声音的完整品牌广告
```

### 2.2 页面结构

声音阶段不应是文件列表，也不应是专业非编软件。建议保持“预览优先、时间轴为核心、当前事件就近编辑”的结构：

```text
┌──────────────────────────────────────────────────────┐
│              完整视频 + 当前声音设计混音预览            │
│                播放 / 暂停 / 当前时间                  │
└──────────────────────────────────────────────────────┘

声音方案： [高级克制] [沉浸水感] [年轻轻快]

00:00              00:05              00:10      00:15
─────────────────────────────────────────────────────────
原视频声音  （默认静音，可选开启）
环境氛围    ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
镜头音效    蜂翼声       液体流动      金色转场    品牌收束
品牌音乐    ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

当前事件：镜头 02 · 精华液流动
用途：强化水润与高级流动质感
声音描述：细腻金色精华液在玻璃容器内缓慢流动……
[试听] [编辑描述] [重新生成] [历史版本] [删除]
```

点击任意声音块时，主播放器跳转到该时间；播放时，当前声音事件和对应镜头高亮。编辑声音不会重新生成画面。

## 3. 业务边界与术语

### 3.1 新的通用语言

| 术语 | 定义 | 不是 |
|---|---|---|
| 声音意图（Sound Design Intent） | 剧本阶段的高层创作约束，指导后续声音方案 | 具体音频文件或底层 Prompt |
| 声音事件（Sound Cue） | 某一时间区间内的可生成、可编辑、可试听声音安排 | 等同于一个镜头；一个镜头可有 0–多个事件 |
| 声音资产（Sound Asset） | 某个声音事件一次生成后入库的可播放项目音频 | 视觉视频 Asset |
| 声音方案（Sound Treatment） | 一套全片风格和事件规划，例如“沉浸水感版” | 多语言配音版本 |
| 混音修订（Mix Revision） | 对轨道、事件位置、响度或选中资产的不可变快照 | 覆盖旧成片 |
| 原视频声音 | Seedance 生成视频携带的音频，可选择纳入 | AI 声音设计的默认输入 |

### 3.2 确定不做的内容

- 数字人、真人口播、画外音旁白。
- MiniMax TTS 探测、音色选择、品牌发音词典、语速与咬字控制。
- 旁白时长适配、文字压缩建议、字幕从旁白生成。
- 多语言“配音版”。将来的不同语言如果有需求，作为独立的字幕/本地化能力，而非本期声音设计的一部分。
- 用户上传音频替换作为主路径。第一期只支持 AI 生成、重生成、选择已有生成版本；外部素材导入可以作为以后受权素材能力单列。
- 拖动重排视频镜头、裁剪视觉。声音时间轴只能编辑声音，视觉结构仍回到剧本分镜或素材剪辑。

### 3.3 应保留的声音层

| 轨道 | 默认 | 作用 | 第一版生成策略 |
|---|---|---|---|
| 原视频声音 | 静音 | 保留 Seedance 自带环境/动作声音的可选入口 | 不生成；仅可开启、静音、调音量 |
| 环境氛围 | 开启 | 建立空间、季节、自然感和情绪 | 一条或多个 AI ambience 事件 |
| 镜头音效 | 开启 | 强化动作、材质、转场、产品定格 | 按事件 AI 生成，允许一个镜头多个事件 |
| 品牌音乐 | 开启 | 建立整支广告的节奏、情绪和结尾收束 | 一首 AI 生成的全片音乐，可带分段能量曲线 |

如果业务坚持“所有最终声音都由本系统 AI 生成”，原视频声音保持默认静音且不能加入最终输出；它仍可作为用户对比试听的可选层。

## 4. 剧本阶段与声音阶段的职责分层

### 4.1 剧本分镜阶段：保留意图，不做执行

当前页面显示“音乐方向”和“口播方向”。改造后应为：

```text
声音氛围与音乐意图
  例：前 2 秒以自然蜂鸣和柔和金色闪光开启；中段克制、流动、留白；结尾有清晰而高级的品牌收束。

音效重点
  例：突出蜂翼、精华液流动、金色光泽转场和产品定格；避免夸张水花、嘈杂自然声和人声。

原视频声音策略
  默认静音 / 可作为轻微底噪保留
```

“口播方向”应从 UI 和新计划生成中删除。每个镜头中的“旁白”编辑框也应从品牌广告的剧本分镜 UI 移除；屏幕字仍属于视觉叙事，应保留。

高层声音意图必须在视频生成前确认，因为它会影响镜头节奏、留白、产品定格长度与转场设计；但它不是最终音频资产的单一事实源。真实的音乐、环境声、音效 Prompt、位置和音量只在声音设计阶段确定。

### 4.2 声音设计阶段：拥有执行与成片

声音设计模块读取：

- 已确认的 `SoundDesignIntent`；
- 有序镜头的起止时间、画面、动作、运镜、光线、屏幕字和镜头目的；
- 已锁定的合成视觉预览；
- 当前已选声音方案和音频资产历史。

它输出：

- 可解释的声音事件方案；
- 每条事件的生成描述、负向约束、时间、时长和重要度；
- 可播放的 AI 声音资产与生成 Attempt；
- 不可变的混音修订与最终混音视频。

## 5. 当前代码事实与改造缺口

### 5.1 可直接复用的能力

1. Brand Film 已拥有锁定视觉预览、音频工作区、混音版本、生成 Attempt、渲染任务和最终混音 Asset 的持久化位置。`BrandAudioWorkspace` 记录 `VisualPreview`、`Variants`、`Attempts`、`RenderJobs` 与混音输出。来源：`internal/systems/creative/brand_film_audio.go:109-124`。
2. 当前 `AudioMixVersion` 和 `ReviseBrandAudioMix` 已以追加修订而非覆盖方式保存轨道修改；修改后会失效旧混音预览。来源：`internal/systems/creative/brand_film_audio.go:449-562`。
3. `AudioGenerationAttempt` 已支持成功/失败、Provider 快照、输出 Asset、Retry 链路和 Fixture 标识，适合复用为 AI 音乐与音效的版本历史。来源：`internal/systems/creative/brand_film_audio.go:260-275`。
4. `CompileBrandAudioMix` 会将领域混音转为渲染器中立请求，FFmpeg 渲染器已经支持片段裁切、延迟、淡入淡出、增益、限制器、响度归一和输出视频时长检查。来源：`internal/systems/creative/brand_film_audio_mix.go:11-55`、`internal/platform/media/audio_mix.go:150-203`。
5. 声音阶段已通过 API 和异步任务运行 FFmpeg 预览渲染，无需重新生成视觉。来源：`internal/systems/creative/brand_film_audio_render.go:26-86,89-127`。

### 5.2 必须修正的现状

1. 当前领域模型强制 `voiceover` 轨道，`AudioMixVersion.Validate` 要求四轨且 `BrandAudioWorkspace` 固定为 v1。来源：`internal/systems/creative/brand_film_audio.go:401-445`。
2. `PrepareBrandAudioFixture` 从每个镜头的 `Voiceover` 构建旁白 Cue 和 voice Clip，并把音乐与音效写成 Fixture。来源：`internal/systems/creative/brand_film_audio.go:293-346`。
3. 声音导演当前核心逻辑是发音词典、旁白时长适配、BGM 为旁白自动 ducking；这些与无旁白目标冲突。来源：`internal/systems/creative/brand_film_audio_director.go:46-92`。
4. 前端仍显示 MiniMax TTS 探测、逻辑音色、真实旁白、发音词典和旁白时长编辑。来源：`src/components/BrandFilmWorkspace.tsx:128-151,514-529,605,654-656,666`。
5. OpenAPI 同样暴露 `generate-voice` 与 `speech-capability`，需要从新产品契约移除或弃用。来源：`api/openapi/creative-v1.yaml:684-724`。
6. 当前工程只有 `BrandFilmSpeech` / MiniMax Speech 的接入；仓库中未发现通用的“文本生成音乐”或“文本生成音效” Provider Adapter。TTS 不能替代音效生成。来源：`internal/systems/creative/brand_film_voice.go:21-87`、`cmd/cookies-api/main.go:835-837`。

## 6. 目标领域模型

### 6.1 版本策略：新增 v2，不就地破坏 v1

不要直接删除存量 JSON 字段或让旧任务无法读取。新增 `creative-brand-sound-design-workspace/v2`，读取层同时识别 v1 和 v2：

- v1：历史“旁白 + 音乐 + 音效”任务，按兼容视图展示，可继续播放历史成片；不再提供 TTS 操作。
- v2：新建品牌广告只生成无旁白的声音设计工作区。
- 第一次进入旧任务的声音页时可提供“升级为 AI 声音设计”操作：保留已存在的音乐、音效、Attempt 和混音历史；旁白轨道默认静音并标为“历史人声，不参与新声音设计”。不自动删除资产。

同样，`BrandBriefAnalysisVersion.VoiceDirection`、`BrandFilmPlanVersion.VoiceDirection` 与 `BrandFilmShot.Voiceover` 应先改为兼容字段（`omitempty`，不再是校验必填）；新版本改由 `SoundDesignIntent` 作为唯一输入。现行校验仍强制 `VoiceDirection` 非空，必须同时修正。这个改造还必须同步到 Deterministic / AI planner 和策略转品牌广告的映射，不能只改前端字段。来源：`internal/systems/creative/brand_film.go:222-258,300-356`、`internal/systems/creative/brand_film_planner.go:247-266`、`internal/systems/creative/strategy_brand_film.go:213-214`。

### 6.2 新的声音意图

```go
type SoundDesignIntent struct {
    MusicDirection      string   `json:"music_direction"`
    SoundEffectFocus    []string `json:"sound_effect_focus"`
    SourceAudioPolicy   string   `json:"source_audio_policy"` // mute | optional | include
    Avoid               []string `json:"avoid"`
}
```

规则：`MusicDirection` 与至少一条 `SoundEffectFocus` 是新计划的必填信息；`SourceAudioPolicy` 默认为 `mute`；`Avoid` 默认包含 `人声`，但不写死品牌专属词汇。

`BrandFilmPlanVersion` 直接拥有该对象。`BrandBriefAnalysisVersion` 可提供初步建议，但用户在剧本分镜阶段确认的计划版本才是声音设计的输入快照。

### 6.3 新的声音蓝图

```go
type SoundDesignBlueprintVersion struct {
    Revision       int64
    PlanRevision   int64
    VisualPreview  AssetVersionRef
    Intent         SoundDesignIntent
    MusicPlan      MusicPlan
    Cues           []SoundCue
    Decisions      []SoundDesignDecision
    SemanticChecks []SoundSemanticCheck
    PlannerVersion string
    ContentHash    string
}

type SoundCue struct {
    ID, ShotID, TrackType string // ambience | sfx | music
    StartMS, EndMS        int
    Label, Purpose        string
    Prompt, NegativePrompt string
    Intensity             string // subtle | medium | accent
    Status                string // planned | generating | ready | failed | removed
    Locked                bool
}
```

`Cue` 才是用户编辑和 AI 生成的最小单位：音乐可以是跨全片的一条 Cue；一个镜头可以没有音效，也可以有蜂翼、液体、转场、产品定格等多条 Cue。它不能假设“一个镜头等于一条声音”。

### 6.4 新轨道与混音版本

v2 的 `AudioTrack.Type` 为：

```text
source_audio | ambience | music | sfx
```

四轨顺序固定，但 `source_audio` 可为空或静音。`ambience`、`music`、`sfx` 可以含多个 Clip，且允许重叠；应删除仅针对 `voiceover` 的不重叠校验。`AudioClip` 增加 `SoundCueRef`、`PromptSnapshot`、`NegativePromptSnapshot`、`GenerationProfile`、`RightsProvenance`，而不再写入 `NarrationSource` 和 `WordTimings`。

现有 `AudioMixVersion`、`ParentRevision`、`ContentHash`、`ChangeSummary`、`AudioGenerationAttempt`、`AudioMixRenderJob` 保持。它们分别承担版本、可追溯性、局部重试与异步渲染，具有足够的模块深度，不应在前端重复实现版本逻辑。

## 7. AI 音频生成 seam

### 7.1 新接口

在 Creative 中定义一个小而稳定的 `SoundAssetGenerator` interface；具体供应商 Adapter 放在 Provider 层。不要把供应商名称、API Key、参数格式泄漏到 Brand Film 领域或浏览器。

```go
type SoundAssetGenerator interface {
    Capability(ctx context.Context, orgID OrganizationID) (SoundGenerationCapability, error)
    Submit(ctx context.Context, request SoundGenerationRequest) (SoundGenerationJob, error)
    Reconcile(ctx context.Context, job SoundGenerationJob) (SoundGenerationResult, error)
}
```

`SoundGenerationRequest` 只包含：组织/项目范围、幂等键、Cue ID、资产种类（music / ambience / sfx）、自然语言 Prompt、负向约束、目标时长、采样率、声道、可选 Seed，以及来源的计划/蓝图/视觉预览资源引用。

该 module 应隐藏：供应商路由选择、请求轮询、超时、重试分类、格式标准化、入库和 Provider 快照记录。调用者只需要发起“生成某 Cue”或“重生成某 Cue”。

### 7.2 供应商与降级规则

当前 MiniMax 配置仅适用于 TTS，不能被误用为文本到音乐/音效；运行时当前也只注入了 speech Adapter，未注入通用 AI 音频生成器。真实音频生成上线前需要提供支持下列之一的网关模型配置：

- text-to-music；
- text-to-sound-effect / text-to-audio；
- 或一个能够可靠区分二者的统一 audio-generation 模型。

若 `SoundAssetGenerator.Capability` 不可用：

- UI 显示“AI 音频生成尚未配置”；
- 可显示 Fixture 预览，但每条 Asset 和最终预览必须标识 Fixture；
- 不得显示“已生成 AI 音效”；
- 用户的 Prompt 和时间轴编辑仍可保存，待能力配置后再逐条生成。

### 7.3 生成与版本规则

- 生成默认只提交当前 Cue 的一个 Attempt。
- 重新生成生成新的 Attempt，`retry_of` 指向上一 Attempt；旧成功资产永不覆盖。
- 默认展示：已锁定成功 Attempt > 最新成功 Attempt > 最新进行中 Attempt > 计划占位。
- AI 生成失败保留之前成功版本，时间轴不丢失；只在当前 Cue 显示可解释错误与“重试”。
- 切换声音方案只修改 Blueprint / Mix Variant；不应偷偷覆盖用户锁定的 Cue。对已锁定 Cue 应先提示“保留 / 按新方案重生成”。

## 8. 声音导演规划逻辑

### 8.1 输入和输出

声音规划器以声音意图、镜头语义与成片时长为输入，输出“事件”而非一句笼统的音乐方向。

对于 15 秒、3 镜头的娇兰样例，合理的默认蓝图可以是：

| 时间 | 轨道 | 声音事件 | 强度 |
|---|---|---|---|
| 0–15s | 音乐 | 留白感轻奢音乐，结尾收束 | subtle → medium |
| 0–5s | 环境氛围 | 清晨花园、轻微蜂鸣与空气感 | subtle |
| 0.5–2.0s | 镜头音效 | 近距离蜂翼振动 | subtle |
| 5–8s | 镜头音效 | 细腻精华液在玻璃中流动 | medium |
| 8–10s | 镜头音效 | 金色光泽转场 | subtle |
| 12–15s | 镜头音效 | 产品定格的品牌收束音 | medium |

### 8.2 规则优先，模型增强

第一版不应把所有安排交给不可解释的大模型。使用“规则生成基础时间位置 + 模型生成创作描述”的方式：

1. 用镜头边界、产品露出、转场、定格等确定候选时间窗；
2. 用 `SoundDesignIntent`、画面、动作、运镜和光线生成事件描述与负向约束；
3. 对每条事件输出目的、证据、置信度和可编辑性；
4. 对“画面无液体却生成水声”“产品定格缺品牌收束”“事件过密”“人声禁用被违反”等给出声画检查；
5. 用户确认/编辑后才进入资产生成。

### 8.3 声画检查的新含义

保留 `SemanticChecks`，但将其从“口播和画面是否一致”改为：

- 事件是否有画面动作或镜头目的支持；
- 事件是否落在有效镜头时间区间；
- 产品定格是否有可选品牌收束；
- 多个强音效是否堆叠；
- 是否违反“无人声”“避免夸张水花”等意图；
- 背景音乐是否在 CTA / 产品定格处给出足够留白。

## 9. API 设计

保留读、混音、选择方案、渲染接口的职责，但采用 v2 名称和内容。推荐新增或替换为：

```text
POST  /brand-film:prepare-sound-design
GET   /brand-film/sound-design
PATCH /brand-film/sound-design/blueprint
POST  /brand-film/sound-design/cues/{cue_id}:generate
POST  /brand-film/sound-design/cues/{cue_id}:regenerate
POST  /brand-film/sound-design/cues/{cue_id}:select-attempt
PATCH /brand-film/sound-design/mix
POST  /brand-film/sound-design:select-treatment
POST  /brand-film/sound-design:render-preview
```

`PATCH blueprint` 用于编辑声音意图或 Cue 的自然语言描述、时间窗、强度、锁定状态、删除/新增；它产生新的 Blueprint revision，但不直接生成音频。

`PATCH mix` 延续结构化操作并补齐：

```text
set_track_gain | set_track_muted | set_track_solo
set_clip_gain | set_clip_timing | set_clip_fade
remove_clip | add_clip | select_clip_attempt
```

所有写接口继续要求 `expected_revision` 和 `Idempotency-Key`；生成 Cue、切换声音方案与渲染预览都必须是幂等操作。旧 `generate-voice`、`speech-capability` 在 v2 UI 与新 OpenAPI 中移除；如果需要保留历史兼容，标注 deprecated，限制为读取历史状态。

## 10. 前端改造方案

### 10.1 剧本分镜页

1. 将“音乐方向”改名为“声音氛围与音乐意图”。
2. 删除“口播方向”。
3. 增加“音效重点”与“原视频声音策略”。
4. 删除每个镜头内“旁白”字段，保留画面、动作、屏幕字、运镜、光线与连贯性。
5. 已确认计划重新编辑时，继续沿用当前的失效规则：视觉生成、声音蓝图、音频资产和混音预览均需重新确认；但旧资产保留在历史修订中，不物理删除。

### 10.2 声音设计页

移除：TTS 检测条、逻辑音色、真实旁白卡、品牌发音词典、旁白时长卡、上传替换音频入口。

新增：

- 顶部“声音方案”切换，显示变化说明而非立即重生成全部资产；
- 自动声音事件列表，明确显示用途、时间、轨道、状态和 AI 生成描述；
- 选中事件详情面板：试听、编辑描述、负向约束、重新生成、历史版本、锁定、删除；
- 时间轴支持点击 seek、拖动时间、调整时长、淡入淡出、增益、静音/独听；
- “在播放头新增音效”操作，默认由 AI 建议描述；
- “声画检查”卡片，只展示可行动的 warning；
- “生成混音预览”前展示本次会使用的事件数、Fixture 数、AI 资产数和未解决错误。

### 10.3 易用性规则

- 用户只编辑自然语言“声音描述”，不直接编辑供应商 Prompt。
- 每个 Cue 初始仅一个生成结果；“重新生成”才产生下一版本。
- UI 不暴露毫秒输入框作为默认编辑方式，优先使用时间轴拖拽；毫秒值在详情中作为精确高级设置。
- 任何重新生成、删除或声音方案切换都不触发视觉重生成。
- 所有 AI 资产卡片显示模型/版本、生成时间、Prompt 快照和权利来源，便于交付追溯。

## 11. 混音与输出调整

现有 FFmpeg `BuildAudioMixFilter` 仅在 voiceover 与 music 同时存在时做 sidechain ducking。来源：`internal/platform/media/audio_mix.go:188-202`。v2 应删除这段旁白 ducking，改为：

- 音乐与关键音效可使用固定或事件驱动的 ducking；
- 环境氛围始终低于音乐与关键音效；
- 仍保留 `alimiter` 和 `loudnorm`；
- 首版继续固定 48 kHz stereo、AAC/H.264 输出与全片时长验证；
- 输出前增加“无活跃音频”“未就绪 Cue”“全部轨道静音”的明确校验。

不用为“声音设计”单独实现视频合成器；复用 `AudioMixCompiler → AudioMixRenderer` 的既有 deep module，让复杂的素材裁切、延迟、淡入淡出、响度控制仍集中在 Media 层。

## 12. 迁移与失效规则

| 变化 | 视觉生成 | 声音蓝图 | 已生成 AI 音频 | 混音预览 |
|---|---|---|---|---|
| 编辑声音意图 | 保留 | 新修订 | 保留为历史，需按新蓝图选择是否重生成 | 失效 |
| 编辑声音事件描述/位置 | 保留 | 新修订 | 当前 Cue 待重新生成或选历史版本 | 失效 |
| 调整音量/静音/淡入淡出 | 保留 | 保留 | 保留 | 失效 |
| 局部重新生成一个 Cue | 保留 | 保留 | 仅该 Cue 新增 Attempt | 失效 |
| 切换声音方案 | 保留 | 切换 Variant | 未锁定 Cue 可待重生成；锁定 Cue 保留 | 失效 |
| 重新确认剧本分镜 | 全部失效 | 失效 | 保留为历史，不作为新版本输入 | 失效 |

旧 v1 音频工作区迁移必须可逆：保留 JSON 快照和历史资产；只创建 v2 视图/新版 Blueprint，不通过后台任务删除任何 voiceover Asset。

## 13. 验收与测试

### 13.1 领域与 API 测试

- 新 Brand Film 计划没有 `VoiceDirection` 或 `Voiceover` 时能校验、保存和生成视觉；旧 v1 Brief 契约仍可读取，新 v2 Brief 契约不再强制 `narration_required`、`voice_direction`。
- v2 声音工作区要求 `source_audio / ambience / music / sfx`，不要求 `voiceover`。
- 一个镜头 0/1/多条 Cue 均可校验；Cue 不得超出 master timeline。
- 声音意图更新只失效声音侧结果，不失效锁定视觉。
- 生成 Attempt 成功、失败、重复幂等提交、局部重试和历史选回正确保存。
- 没有真实 `SoundAssetGenerator` 时返回明确 capability 状态，不得将 Fixture 伪装为 AI 成功。
- 旧 v1 任务可恢复、播放其历史成片并升级到 v2；新 UI 不出现 TTS 控件。

### 13.2 混音测试

- ambience / music / sfx 的时间、淡入淡出、增益与静音正确编译。
- source audio 默认静音不进入混音；启用后按轨道音量进入。
- key SFX 触发音乐 ducking 时，音频图正确；不含 voice bus。
- 输出时长与视觉预览误差不超过 250ms，音轨为 AAC，响度和峰值约束保留。

### 13.3 前端验收

- 剧本页只有“声音氛围与音乐意图”“音效重点”“原视频声音策略”，无口播方向与旁白框。
- 声音页无 MiniMax/TTS、音色、发音词典、旁白时长和上传替换入口。
- 15 秒 3 镜头、30 秒 6 镜头、10 镜头自定义时长均能横向滚动和正确同步播放头。
- 点击声音块跳转主播放器；播放时当前镜头/声音事件高亮。
- 重生成一条音效不改变视频 URL、其他 Cue、已锁定资产或历史 Mix。
- 真实 AI 不可用、生成中、失败、Fixture、成功、历史版本均有清晰文案。

## 14. 实施顺序

### Phase S0：契约与剧本去旁白

- 引入 `SoundDesignIntent`；改造 Brief、Concept、Plan、Shot 的 v2 契约与校验。
- 删除新任务 UI 中口播方向和镜头旁白编辑；音乐方向改名并增加音效重点、原声策略。
- 更新 OpenAPI、前端 API 类型、Fixture 和领域测试。

### Phase S1：无旁白声音蓝图与 v2 工作区

- 以 `ambience / music / sfx / source_audio` 建立 v2 Blueprint 与 Mix。
- 将当前导演逻辑替换为声音事件规划、声画检查和声音方案。
- 保留不可变 Mix、版本、Attempt、资产和渲染任务模型。

### Phase S2：时间轴与声音事件编辑

- 重构声音页 UI，移除 TTS / 上传路径。
- 实现 Cue 描述编辑、位置/时长/音量/淡化/静音/独听/删除/新增与历史选择。
- 完成混音预览 UI 与失效状态展示。

### Phase S3：真实 AI 音频生成 Adapter

- 已实现 `SoundAssetGenerator` seam 与 `HTTPSoundAssetGenerator`。它接收统一的 `music / ambience / sfx` Prompt、负向约束、时长和 WAV 规格，并调用供应商 normalizer，而不把供应商载荷带进 Brand Film 领域。
- 已提供 `:generate-sound-assets` 接口：成功结果入项目资产库、写入不可变 Mix 修订和非 Fixture `AudioGenerationAttempt`；Provider 未配置或失败会透明返回错误，绝不回退成 Fixture 并声称是 AI。
- 当前 adapter 使用同步的 normalizer 响应契约，适合短品牌广告的初始实现；供应商需要长任务轮询时，在相同 seam 后增加异步 Submit/Reconcile，不改变前端、Cue、资产或 Mix 契约。
- 运行配置：`COOKIES_PROVIDER_SOUND_ASSET_ADAPTER=http`、`COOKIES_SOUND_ASSET_ENDPOINT`、`COOKIES_SOUND_ASSET_API_KEY`、`COOKIES_SOUND_ASSET_MODEL`。Endpoint 必须为 HTTPS，并接受 `POST` JSON `{model, track_type, prompt, negative_prompt, duration_ms, format:"wav", sample_rate:48000}`，返回 `{audio_base64, codec:"wav", duration_ms, request_id, model_snapshot}`。
- “使用开发演示音频”仍是一个分离的、明确标记的按钮，仅用于未配置真实 Provider 时的本地演示。

### Phase S4：声音方案、自动混音与交付

- 已完成高级克制、沉浸水感、年轻轻快三种声音方案；它们作为独立 Mix Variant 保存，可随时切换，切换后只失效混音预览，不影响已锁定画面。
- 已将无旁白主路径的自动让位改为 key SFX 驱动：关键镜头音效出现时，音乐通过 sidechaincompress 自动降低；历史旁白任务仍保留 voice-driven ducking 兼容逻辑。
- 继续使用 FFmpeg `alimiter + loudnorm` 做输出响度与峰值控制；音频混音预览一旦存在，质量检查、冻结版本与交付追溯会指向该带声音的成片，而非无声视觉预览。

## 15. 外部条件与风险

| 条件 | 当前状态 | 对实施的影响 |
|---|---|---|
| FFmpeg 混音 | 已具备 | 可直接复用 |
| 项目音频 Asset 入库 | 已具备 | 可直接复用 |
| MiniMax TTS | 已具备但不再需要 | 从品牌声音页面移除，不作为 AI 音效能力 |
| text-to-music 模型路由 | Adapter 已就绪，真实路由尚未配置 | 提供 HTTPS normalizer 的 Endpoint / API Key / Model 并验证 |
| text-to-sfx / text-to-audio 模型路由 | Adapter 已就绪，真实路由尚未配置 | 可与 music 共用 normalizer，须支持 `track_type=sfx` |
| AI 音频商用权利条款 | 未确认 | 正式生产输出前必须确认并写入 `RightsProvenance` |

最大的产品风险不是时间轴或混音，而是“AI 音频供应商真实能力与商用权利”。因此 S0–S2 可以完整完成无旁白声音设计、版本、编辑和 Fixture 验收；S3 必须在真实模型路由可用后才宣称为 AI 音乐/音效生成闭环。

## 16. 最终建议

S0–S3 已让“声音意图 → 自动声音事件 → 时间轴编辑 → 真实 Provider 资产入库 → 混音预览”的无旁白链路具备接入条件；下一项外部条件是提供实际 AI 音乐/音效 normalizer 路由，并验证其商用权利。TTS 或 Seedance 原声不会被误称为 AI 音效生成。

这条路径保留当前代码中成熟的资产、版本和 FFmpeg 混音模块，同时把旁白相关复杂度从新的品牌广告主路径中彻底移除。
