# Delivery 上线后优化闭环走测手册

本手册用于在 20–30 分钟内验证 `计划来源 → 平台配置 → 首次审批 → 平台操作演练 → 指标与告警 → 优化建议 → 新 ChangeSet → 第二次审批`。所有数据来自确定性情景模拟器，不预测真实投放效果，也不代表已连接或写入广告平台。

## 准备

1. 进入当前 Project 的“上线后优化闭环”。
2. 输入稳定运行 ID 并准备走测数据。
3. 确认黄金路径与六个异常场景各自绑定独立计划。

## 黄金路径

1. 在投放计划核对策略和素材稳定引用。
2. 在平台配置核对一个巨量 Project 与零个或多个 Promotions；运行当前计划检查。
3. 创建 ChangeSet、通过最终检查并在审批中心完成首次批准。
4. 运行本地平台操作演练，确认 Execution、Steps 与 Evidence 已持久化。
5. 选择情景和稳定 seed，生成同一 SimulationRun 的三段指标窗口与告警。
6. 在优化中心生成 Recommendation，确认它引用同一 Execution、SimulationRun、指标窗口和告警。
7. 采纳 Recommendation，确认只生成一个新的 draft ChangeSet，没有自动应用配置。
8. 对新 ChangeSet 再次检查并人工批准。确认页面说明行为工作流编译和真实平台写入尚未实现。

## 异常场景

- 预检失败：平台字段证据 pending，ChangeSet 不得进入审批。
- 审批过期：过期 Approval 明确阻止执行。
- Plan stale：PlanVersion 更新后旧 Approval 永久失效。
- 部分执行：显示已完成、未完成步骤和补偿候选。
- 结果未知：禁止盲目重试，要求查询和重新识别。
- 审核拒绝：用户触发后才产生对应事件与告警。

## 历史兼容检查

- 历史计划显示“历史配置，仅供查看”；
- 修改、检查、提交、审批、执行和建议决策均被拒绝；
- compile、override 与旧操作包 POST 返回 `LEGACY_CONFIGURATION_UNSUPPORTED`；
- `/delivery/three-tier` 只重定向到 `/delivery/configuration`；
- 历史 payload、canonical hash 与 action hash 不发生变化。

## 复位

复位事务按 `organization / Project / run_id / owner_id` 精确隔离，只清理该运行创建的记录。旧操作包表只可能参与历史残留清理；当前 Tour 不创建操作包。
