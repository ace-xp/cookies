# 投放效果情景模拟器计划

> 2026-08-11：本文中的 `ManualActionPackage` 链路已被当前运行时废弃，仅保留为历史规划背景。活动闭环止于第二次人工审批，详见 [`architecture-and-implementation.md`](./architecture-and-implementation.md)。

| 属性 | 内容 |
| --- | --- |
| 状态 | 历史 mock 闭环的剩余验收能力；待实现 |
| 日期 | 2026-08-06 |
| Owner | 智能投放模块 |
| 定位 | 规则驱动、可重复、可解释的 Mock 情景模拟；不是真实效果预测模型 |

## 1. 问题与目标

现有 `success / failed / partial / result_unknown` 只模拟平台操作是否成功以及如何恢复，不模拟投放后的曝光、点击、转化、消耗和变化趋势。当前代码虽已在成功 Execution 后用 Plan hash 生成两个固定指标窗口，但预算、目标、出价、定向和创意差异没有形成可解释的因果影响，第二窗口仍近似固定为高消耗、零转化。因此它只是把 Execution、Metric、Alert 和 Recommendation 接起来的临时接缝，不构成完整 mock 闭环所需的投放效果模拟能力。

在历史 mock 闭环内补齐一个统一的情景模拟器，使完整 Mock 主路径满足：

```text
PlanVersion + 三段配置 + 策略/创意引用 + 情景假设
→ PerformanceSimulationRun
→ MetricSnapshots / PlatformEvents
→ Alerts
→ Recommendations
→ 优化后的 PlanVersion
→ 一次业务审批
→ ManualActionPackage
```

效果模拟与平台操作演练必须分开：效果模拟不产生平台写入，不应复用 `success / failed` 作为效果好坏；平台操作演练继续验证执行状态、幂等、部分成功和结果未知。

## 2. 第一版输入与输出

### 2.1 冻结输入

每次模拟冻结以下输入，生成 canonical hash：

- Organization、Project、Plan、PlanVersion 和版本内容 hash；
- 投放目标、预算、排期以及广告组/计划/创意三个内部配置区段；
- `source_strategy_version`、创意/素材版本引用及可用的 Mock 特征；
- 情景、参数集版本、时间窗口、稳定 seed 和发起人；
- 所有 `platform_pending`、缺失来源和未经真实数据校准的假设。

第一版只使用有界参数集，不训练模型。参数至少覆盖 CPM、CTR、CVR、预算消耗率、转化价值和疲劳衰减；每个参数保存来源、默认值、适用目标和版本。

### 2.2 可解释关系

模拟器至少建立以下方向性关系：

| 输入变化 | 可解释影响 |
| --- | --- |
| 预算与消耗节奏 | 可用消耗、曝光规模和跑量状态 |
| 出价/优化目标 | CPM、获得流量能力和目标指标 |
| 定向范围 | 可触达人群规模、CPM 与衰减速度 |
| 创意 Mock 特征 | CTR、CVR 与素材疲劳 |
| 排期和观察窗口 | 每日分布、学习期与疲劳趋势 |
| 追踪状态 | 转化是否可用；追踪异常不能被解释为零转化 |

基础计算可以使用 `impressions = spend / CPM × 1000`、`clicks = impressions × CTR`、`conversions = clicks × CVR` 等受控公式，但所有结果都必须标为情景输出，并展示参数版本和限制。

### 2.3 输出

- `PerformanceSimulationRun`：输入 hash、scenario、assumption version、seed、状态、时间和 provenance；
- 至少两个连续 `MetricSnapshot` 窗口：曝光、点击、转化、消耗以及可选收入；
- 与指标分开的平台事件，例如审核拒绝、追踪异常或状态不可确认；
- 基于同一个 run/plan version/evidence 的 Alert 与 Recommendation；
- 结果摘要、参数说明、限制与“并非真实效果预测”的统一页面披露。

## 3. 首期情景

| 情景 | 主要表现 | 用途 |
| --- | --- | --- |
| `baseline` | 正常消耗和稳定转化 | 验证无异常路径与基线建议 |
| `under_delivery` | 消耗率和曝光不足 | 验证跑量诊断 |
| `cost_worsening` | CPA 上升但仍有转化 | 验证预算/出价/定向建议 |
| `zero_conversion` | 有点击但无转化 | 验证转化诊断，不与追踪异常混淆 |
| `creative_fatigue` | CTR/CVR 随窗口衰减 | 验证素材替换建议 |
| `tracking_anomaly` | 转化指标不可用 | 验证质量门禁；不得生成确定性优化结论 |

审核拒绝属于平台事件情景，不应伪装成指标模型输出。

## 4. 实现任务

| 任务 | 交付物 | 完成门 |
| --- | --- | --- |
| `OE-SIM-01` 契约冻结 | SimulationRun、输入快照、参数集、情景和指标/事件 Schema；明确与 Execution 的区别 | 同一输入和 seed 可稳定重放；接口不接受客户端伪造结果 |
| `OE-SIM-02` 规则引擎 | 预算、目标、定向、出价、创意和时间窗口驱动的有界计算；持久 SimulationRun 与 MetricSnapshots | 修改至少一种业务输入时，输出按已说明关系发生变化 |
| `OE-SIM-03` 因果闭环 | Alert/Recommendation 只读取同一 SimulationRun 的指标与事件，保存精确 evidence | 计划创建时没有指标/告警；显式运行后才产生，且不存在跨 run 串线 |
| `OE-SIM-04` 前端呈现 | 情景选择/参数摘要、趋势和结果、告警及建议；使用页面级统一 Mock 横幅 | 用户能解释结果为何变化；技术阶段代号不进入业务页面 |
| `OE-SIM-05` 验收 | 单元/契约/E2E：稳定重放、输入敏感性、质量门禁、无告警基线、异常链路和安全复位 | 完整 Mock 主路径可由一次 SimulationRun 串联，且不声称真实预测 |

这些任务属于完整 mock 闭环原目标的补齐，不另立内部阶段代号，也不以新阶段替代未完成验收。

## 5. 与后续阶段的关系

- 只读业务校准：通过巨量只读页面和数据洞察 Connector 明确真实对象、可用指标、字段含义和合理参数范围；不在 Delivery 内复制采集逻辑。
- 行为流程编译与影子分析：使用真实历史数据对情景参数回测，保存误差、置信区间和适用范围；只有达到明确评测门槛后才可称为影子效果评估。
- 在影子分析完成前，产品统一使用“效果情景模拟”或“投后演练”，不得使用“高置信度预测”“预计真实效果”等表述。
