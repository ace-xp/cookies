import { expect, test } from '@playwright/test'
import { createRuntimePlan } from './delivery-runtime-fixture'

const projectId = 'project_investor_precision_evidence'

test('DeliveryDecision diagnoses missing facts without forcing a candidate or enabling remote writes', async ({ page, request }) => {
  test.setTimeout(120_000)
  const suffix = `decision-${Date.now().toString(36)}`
  const plan = await createRuntimePlan(request, projectId, suffix)
  const response = await request.post(`/api/delivery/v1/projects/${projectId}/plans/${plan.id}/decisions:generate`, { data: { expected_version: plan.current_version_number } })
  expect(response.status()).toBe(201)
  const decision = await response.json()
  expect(decision).toMatchObject({
    schema_version: 'delivery-decision/v1',
    policy_version: 'delivery-decision-policy/v1',
    diagnostic: { code: 'insufficient_data' },
    candidates: [],
    recommended_candidate_id: '',
  })
  expect(decision.canonical_hash).toMatch(/^[0-9a-f]{64}$/)

  await page.goto(`/projects/${projectId}/delivery/optimization`)
  await page.locator('.delivery-optimization-toolbar select').selectOption(plan.id)
  await expect(page.getByRole('button', { name: '生成优化方案' })).toBeVisible()
  await expect(page.getByText('数据不足', { exact: true })).toBeVisible()
  await expect(page.getByText('所有调整先保存为本地方案，不会自动修改广告平台。')).toBeVisible()
  await expect(page.getByRole('button', { name: '生成优化建议' })).toHaveCount(0)
})

test('Mock Replay observatory is deterministic, auditable, and stops before remote write', async ({ page, request }) => {
  test.setTimeout(120_000)
  const suffix = `observatory-${Date.now().toString(36)}`
  const plan = await createRuntimePlan(request, projectId, suffix)
  const changeSetResponse = await request.post(`/api/delivery/v1/projects/${projectId}/plans/${plan.id}:create-change-set`, { data: { expected_version: plan.version } })
  expect(changeSetResponse.status()).toBe(201)
  let changeSet = await changeSetResponse.json()
  const preflight = await request.post(`/api/delivery/v1/projects/${projectId}/change-sets/${changeSet.id}:preflight`, { data: { expected_version: changeSet.version } })
  expect(preflight.status()).toBe(200)
  changeSet = await preflight.json()
  const approval = await request.post(`/api/delivery/v1/projects/${projectId}/change-sets/${changeSet.id}:approve`, { data: { expected_version: changeSet.version } })
  expect(approval.status()).toBe(200)
  changeSet = await approval.json()
  const executionResponse = await request.post(`/api/delivery/v1/projects/${projectId}/change-sets/${changeSet.id}:execute`, { headers: { 'Idempotency-Key': `observatory-execution-${suffix}` }, data: { expected_version: changeSet.version, scenario: 'success' } })
  expect(executionResponse.status()).toBe(201)
  const execution = await executionResponse.json()
  const simulationResponse = await request.post(`/api/delivery/v1/projects/${projectId}/executions/${execution.execution.id}/simulation-runs`, { data: { scenario: 'cost_pressure', stable_seed: suffix } })
  expect([200, 201]).toContain(simulationResponse.status())

  const decisionResponse = await request.post(`/api/delivery/v1/projects/${projectId}/plans/${plan.id}/decisions:generate`, { data: { expected_version: plan.current_version_number } })
  expect(decisionResponse.status()).toBe(201)
  const decision = await decisionResponse.json()
  expect(decision.diagnostic.code).toBe('ready')

  await page.goto(`/projects/${projectId}/delivery/optimization`)
  await page.locator('.delivery-optimization-toolbar select').selectOption(plan.id)
  await expect(page.getByRole('heading', { name: '稳健方案' })).toBeVisible()
  await expect(page.getByText('判断把握较高', { exact: true })).toBeVisible()
  await expect(page.getByText('¥160.00', { exact: true }).first()).toBeVisible()
  await expect(page.getByText(/当前转化成本为基准值的 [0-9.]+ 倍；根据当前决策规则，建议将每日预算下调 \d+%/).first()).toBeVisible()
  await expect(page.getByText('low uncertainty', { exact: true })).toHaveCount(0)
  const recommendedCard = page.locator('article.delivery-optimization-card > div.delivery-config-recommendations > article.delivery-recommendation-card').filter({ hasText: '推荐' })
  const selectedButton = recommendedCard.getByRole('button', { name: '选为待确认方案' })
  const selectionPromise = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/delivery/v1/projects/${projectId}/decisions/${decision.id}:select`)
  await selectedButton.click()
  const selectionResponse = await selectionPromise
  expect(selectionResponse.status()).toBe(201)
  const selection = await selectionResponse.json()
  await expect(page.getByRole('button', { name: '待运营确认' })).toBeDisabled()
  await expect(page.getByRole('heading', { name: '优化方案调整明细' })).toBeVisible()
  await expect(page.getByRole('table', { name: '优化方案调整前后对比' })).toContainText('每日预算')
  await expect(page.getByRole('table', { name: '优化方案调整前后对比' })).toContainText('¥200.00')
  await expect(page.getByRole('table', { name: '优化方案调整前后对比' })).toContainText('¥160.00')
  await expect(page.getByRole('table', { name: '优化方案调整前后对比' })).toContainText('将调整')
  await expect(page.getByText('方案尚未应用到广告平台', { exact: true })).toBeVisible()
  await expect(page.getByLabel('修改后的每日预算')).toBeVisible()
  await expect(page.getByRole('button', { name: '拒绝此方案' })).toBeVisible()
  const feedbackPromise = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/feedback'))
  await page.getByRole('button', { name: '接受优化方案' }).click()
  expect((await feedbackPromise).status()).toBe(201)
  await expect(page.getByText('已接受优化方案', { exact: true })).toBeVisible()

  const observedValues = Object.fromEntries(selection.workflow.steps.flatMap((step: any) => step.fields.map((field: any) => [`${step.id}.${field.key}`, field.expected_readback])))
  const fixture = { fixture_id: `fixture-${suffix}`, data_state: 'ready', data_state_reason: '', observed_at: new Date().toISOString(), data_through: new Date().toISOString(), observed_values: observedValues, selector_matches: {}, evidence_refs: [`replay://fixture/${suffix}`], page_refs: ['replay://page/project'] }
  const runURL = `/api/delivery/v1/projects/${projectId}/decision-selections/${selection.id}/observatory-runs`
  const runResponse = await request.post(runURL, { data: { source: 'replay', mode: 'observe_existing', fixture } })
  expect(runResponse.status()).toBe(201)
  const run = await runResponse.json()
  expect(run).toMatchObject({ schema_version: 'delivery-observatory-run/v1', source: 'replay', status: 'completed', outcome: 'in_sync', remote_write_enabled: false })
  expect(run.binding).toMatchObject({ decision_canonical_hash: decision.canonical_hash, configuration_canonical_hash: selection.configuration.canonical_hash, workflow_canonical_hash: selection.workflow.canonical_hash })
  expect(run.steps.at(-1)).toMatchObject({ executed_action: 'observe', status: 'blocked', block_reason: 'PHASE_C_REMOTE_WRITE_PROHIBITED' })
  expect(run.steps.every((step: any) => step.executed_action !== 'remote_write')).toBe(true)
  expect((await request.post(runURL, { data: { source: 'replay', mode: 'observe_existing', fixture } })).status()).toBe(200)

  const feedbackResponse = await request.post(`/api/delivery/v1/projects/${projectId}/observatory-runs/${run.id}/feedback`, { headers: { 'Idempotency-Key': `observatory-feedback-${suffix}` }, data: { disposition: 'accepted', reason: 'evidence reviewed in e2e', diff_keys: [] } })
  expect(feedbackResponse.status()).toBe(201)
  expect(await feedbackResponse.json()).toMatchObject({ run_id: run.id, run_canonical_hash: run.canonical_hash, disposition: 'accepted' })

  const forbiddenSource = await request.post(runURL, { data: { source: 'connector', mode: 'observe_existing', fixture: { ...fixture, fixture_id: `forbidden-${suffix}` } } })
  expect(forbiddenSource.status()).toBe(400)

  await page.goto(`/projects/${projectId}/delivery/optimization`)
  await expect(page.getByRole('button', { name: '只读比较已有对象' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '准备本地未提交表单' })).toHaveCount(0)
  await expect(page.getByText('接受观察结果', { exact: true })).toHaveCount(0)
})
