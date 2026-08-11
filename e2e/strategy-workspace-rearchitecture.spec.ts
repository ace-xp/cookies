import { expect, test, type Page } from '@playwright/test'

const projectId = 'project_local'
const projectRoot = `/projects/${projectId}/strategy`

const centers = [
  { id: 'briefs', label: '需求中心', accessibleName: '需求中心：Brief、版本与确认' },
  { id: 'research', label: '研究洞察', accessibleName: '研究洞察：联网研究与证据' },
  { id: 'strategies', label: '策略中心', accessibleName: '策略中心：策略方案与版本' },
  { id: 'reviews', label: '评审中心', accessibleName: '评审中心：确认、协作与变更' },
] as const

function captureConsoleErrors(page: Page) {
  const errors: string[] = []
  page.on('console', message => {
    if (message.type() === 'error') errors.push(message.text())
  })
  page.on('pageerror', error => errors.push(error.message))
  return errors
}

async function waitForProject(page: Page, projectName = 'Local Project') {
  const login = page.getByRole('button', { name: '登录工作台' })
  if (await login.isVisible().catch(() => false)) {
    await page.getByRole('textbox', { name: '账号' }).fill('Admin')
    await page.getByRole('textbox', { name: '密码' }).fill('123456')
    await login.click()
  }
  await expect(page.getByRole('button', { name: projectName })).toBeVisible()
  await expect(page.getByText('尚未连接到服务端')).toHaveCount(0)
}

async function createIsolatedProject(page: Page, productIds: string[] = []) {
  const suffix = `${Date.now()}-${Math.random().toString(16).slice(2)}`
  const projectName = `Strategy E2E ${suffix}`
  const brandResponse = await page.request.post('/platform/v1/brands', {
    data: { name: `Strategy E2E Brand ${suffix}` },
  })
  if (!brandResponse.ok()) throw new Error(`Failed to create isolated brand: ${await brandResponse.text()}`)
  const brand = await brandResponse.json() as { id: string }

  const projectResponse = await page.request.post('/platform/v1/projects', {
    data: {
      name: projectName,
      brand: `Strategy E2E Brand ${suffix}`,
      goal: '验证策略工作区导航、持久化与故障恢复',
      industry: 'ecommerce',
      primary_brand_id: brand.id,
      product_ids: productIds,
      activate: true,
    },
  })
  if (!projectResponse.ok()) throw new Error(`Failed to create isolated project: ${await projectResponse.text()}`)
  const project = await projectResponse.json() as { id: string }
  return { id: project.id, name: projectName }
}

async function ensureStartedWorkspace(page: Page, root = projectRoot, projectName = 'Local Project') {
  await page.goto(`${root}/workspaces`)
  await waitForProject(page, projectName)
  await expect(page.getByRole('heading', { name: '策略工作区', exact: true })).toBeVisible()

  const stageNavigation = page.getByRole('navigation', { name: '策略工作阶段' })
  const createWorkspace = page.getByRole('button', { name: '创建主策略工作区' })
  await expect(createWorkspace.or(stageNavigation)).toBeVisible()
  if (await createWorkspace.isVisible().catch(() => false)) {
    await createWorkspace.click()
  }

  const startWorkspace = page.getByRole('button', { name: '开始策略梳理' })
  const assistantTrigger = page.getByRole('button', { name: /^AI 助手/ })
  await expect(stageNavigation).toBeVisible()
  await expect.poll(async () => {
    if (await startWorkspace.isVisible().catch(() => false)) return 'ready-to-start'
    if (await assistantTrigger.isEnabled().catch(() => false)) return 'started'
    return 'waiting'
  }).not.toBe('waiting')
  if (!(await assistantTrigger.isEnabled().catch(() => false))) {
    // Workspace creation reloads the shell. The start control can briefly
    // unmount between the first readiness observation and this action, so let
    // Playwright wait for the stable actionable control instead of treating
    // that transition as an already-started conversation.
    await expect(startWorkspace).toBeVisible({ timeout: 15_000 })
    const conversationResponse = page.waitForResponse(response =>
      response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/strategy/v1/conversations',
    )
    await startWorkspace.click()
    const response = await conversationResponse
    if (!response.ok()) throw new Error(`Failed to start Strategy conversation: ${await response.text()}`)
  }

  await expect(stageNavigation).toBeVisible()
  await expect(assistantTrigger).toBeEnabled({ timeout: 15_000 })
  await expect(page).toHaveURL(/\/strategy\/workspaces\/[^/]+\/intake$/)
}

function onePageTextPDF(text: string) {
  const escaped = text.replaceAll('\\', '\\\\').replaceAll('(', '\\(').replaceAll(')', '\\)')
  const stream = `BT /F1 12 Tf 72 720 Td (${escaped}) Tj ET`
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    `<< /Length ${Buffer.byteLength(stream, 'ascii')} >>\nstream\n${stream}\nendstream`,
  ]
  let body = '%PDF-1.4\n'
  const offsets = [0]
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(body, 'ascii'))
    body += `${index + 1} 0 obj\n${object}\nendobj\n`
  })
  const xrefOffset = Buffer.byteLength(body, 'ascii')
  body += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
  body += offsets.slice(1).map(offset => `${String(offset).padStart(10, '0')} 00000 n \n`).join('')
  body += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xrefOffset}\n%%EOF\n`
  return Buffer.from(body, 'ascii')
}

test('four Strategy centers form one prominent shared-context navigation hub', async ({ page }, testInfo) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.addInitScript(() => {
    const target = window as Window & { __cookiesLargestContentfulPaint?: number }
    target.__cookiesLargestContentfulPaint = 0
    new PerformanceObserver(entries => {
      const latest = entries.getEntries().at(-1)
      if (latest) target.__cookiesLargestContentfulPaint = latest.startTime
    }).observe({ type: 'largest-contentful-paint', buffered: true })
  })
  const requestedSourceModules: string[] = []
  const centerInteractionMs: Record<string, number> = {}
  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (pathname.startsWith('/src/')) requestedSourceModules.push(pathname)
  })
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  consoleErrors.length = 0

  await page.keyboard.press('Tab')
  const skipLink = page.getByRole('link', { name: '跳到主内容' })
  await expect(skipLink).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.locator('#main-content')).toBeFocused()

  const hub = page.locator('.nav-hub-group')
  await expect(hub).toHaveCount(1)
  await expect(hub).toContainText('核心')
  await expect(hub).toContainText('4 个独立中心 · 共享项目上下文')

  for (const center of centers) {
    const navigationItem = page.getByRole('button', { name: center.accessibleName })
    await expect(navigationItem).toBeVisible()
    await expect(navigationItem).toHaveClass(/nav-item-hub/)
    await navigationItem.focus()
    await expect(navigationItem).toBeFocused()
    const interactionStartedAt = performance.now()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(`${projectRoot}/${center.id}`)
    await expect(page.getByRole('heading', { name: center.label, exact: true }).first()).toBeVisible()
    centerInteractionMs[center.id] = performance.now() - interactionStartedAt
    await expect(navigationItem).toHaveAttribute('aria-current', 'page')
    await expect(page.getByText('Project 数据自动关联 · 无需重复建任务')).toBeVisible()
  }

  expect(requestedSourceModules).toContain('/src/components/Pages.tsx')
  expect(requestedSourceModules).not.toContain('/src/components/SpecializedPages.tsx')
  const largestContentfulPaintMs = await page.evaluate(() =>
    (window as Window & { __cookiesLargestContentfulPaint?: number }).__cookiesLargestContentfulPaint ?? 0)
  const performanceEvidence = { largestContentfulPaintMs, centerInteractionMs }
  console.info(`[strategy-performance] centers ${JSON.stringify(performanceEvidence)}`)
  await testInfo.attach('strategy-center-performance.json', {
    body: Buffer.from(JSON.stringify(performanceEvidence, null, 2)),
    contentType: 'application/json',
  })
  expect(largestContentfulPaintMs).toBeGreaterThan(0)
  expect(largestContentfulPaintMs).toBeLessThan(5_000)
  expect(Math.max(...Object.values(centerInteractionMs))).toBeLessThan(1_500)
  expect(consoleErrors).toEqual([])
})

test('a plain-text upload shows truthful parse progress, preview, and Brief context lineage', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  consoleErrors.length = 0

  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)
  await page.getByRole('button', { name: /^Brief：/ }).click()
  await page.getByRole('button', { name: '资料', exact: true }).click()
  const materials = page.getByRole('complementary', { name: '项目资料' })
  await expect(materials).toBeVisible()

  const filename = `strategy-brief-${Date.now()}.txt`
  const body = '项目目标：验证纯文本文档解析。\n核心受众：研发负责人。\n证据要求：所有判断都保留来源定位。'
  const uploadResponsePromise = page.waitForResponse(response =>
    response.request().method() === 'POST' &&
    new URL(response.url()).pathname === `/platform/v1/projects/${isolatedProject.id}/knowledge/documents`,
  )
  await materials.locator('input[type="file"]').setInputFiles({
    name: filename,
    mimeType: 'text/plain',
    buffer: Buffer.from(body, 'utf8'),
  })
  const uploadResponse = await uploadResponsePromise
  expect(uploadResponse.ok()).toBe(true)
  const uploadedDocument = await uploadResponse.json() as { id: string }

  const queueItem = materials.getByRole('button', { name: new RegExp(filename) })
  await expect(queueItem).toBeVisible({ timeout: 15_000 })
  await expect(queueItem).toContainText(/已就绪|解析完成/)
  await expect(queueItem).toContainText('已引用')
  await expect(materials.getByRole('region', { name: '解析进度' })).toContainText('100%')
  await expect(materials.getByText('TEXT PREVIEW', { exact: true })).toBeVisible()
  await expect(materials.locator('.strategy-materials__preview pre')).toContainText('核心受众：研发负责人')
  await expect(materials.locator('.strategy-materials__preview')).toContainText(/来源定位|片段/)

  await page.getByRole('button', { name: '关闭项目资料' }).click()
  await expect(materials).toHaveCount(0)
  const briefURL = new URL(page.url())
  briefURL.search = `panel=materials&resource=${encodeURIComponent(uploadedDocument.id)}`
  await page.goto(briefURL.toString())
  await expect(page).toHaveURL(/\/brief\?panel=materials&resource=[^&]+$/)
  const resourceURL = page.url()
  await expect(materials).toBeVisible()
  await expect(materials.getByRole('heading', { name: filename })).toBeVisible()

  await page.getByRole('button', { name: /^策略：/ }).click()
  await expect(page).toHaveURL(/\/strategy$/)
  await page.goBack()
  await expect(page).toHaveURL(resourceURL)
  await expect(materials).toBeVisible()
  await expect(materials.getByRole('heading', { name: filename })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(/\/strategy$/)
  await page.getByRole('button', { name: /^理解需求：/ }).click()
  await expect(page.locator('#kanon-strategy-message')).toBeEnabled()
  expect(consoleErrors).toEqual([])
})

test('a low-quality PDF preserves extracted text and offers only an explicit visual fallback', async ({ page }) => {
  test.setTimeout(60_000)
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  consoleErrors.length = 0

  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)
  await page.getByRole('button', { name: /^Brief：/ }).click()
  await page.getByRole('button', { name: '资料', exact: true }).click()
  const materials = page.getByRole('complementary', { name: '项目资料' })
  const filename = `low-quality-${Date.now()}.pdf`
  await materials.locator('input[type="file"]').setInputFiles({
    name: filename,
    mimeType: 'application/pdf',
    buffer: onePageTextPDF('A'),
  })

  const queueItem = materials.getByRole('button', { name: new RegExp(filename) })
  await expect(queueItem).toBeVisible({ timeout: 15_000 })
  await expect(queueItem).toContainText(/片段可用 · 建议检查/, { timeout: 30_000 })
  await expect(materials.getByText('解析质量路由信号')).toBeVisible()
  await expect(materials.locator('.strategy-materials__quality')).toContainText('低')
  await expect(materials.getByText('可选：用视觉模型补充低质量内容')).toBeVisible()
  await expect(materials.getByText('只有你确认后才会执行', { exact: false }).or(
    materials.getByText('当前文本结果仍可继续使用', { exact: false }),
  )).toBeVisible()
  await expect(materials.locator('.strategy-materials__preview pre')).toContainText('A')
  expect(consoleErrors).toEqual([])
})

test('Brief fields autosave to the server without silently confirming high-risk decisions', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  consoleErrors.length = 0
  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)

  const intakeComposer = page.getByRole('textbox', { name: /例如：我们要给 FlowKit/ })
  await intakeComposer.fill('产品：FlowKit；推广目标：提升企业试用转化；核心受众：科技公司运营负责人；核心主张：减少跨团队重复沟通。')
  await page.getByRole('button', { name: '发送需求消息' }).click()
  await page.getByRole('button', { name: /^AI 助手/ }).click()
  await expect(page.getByLabel('AI 当前理解')).toContainText('FlowKit')
  await page.getByRole('button', { name: '收起 AI 助手' }).click()
  await page.getByRole('button', { name: /^Brief：/ }).click()

  const objective = page.getByRole('textbox', { name: /推广目标/ })
  const objectivePatch = page.waitForRequest(request =>
    request.method() === 'PATCH' && new URL(request.url()).pathname.endsWith('/brief-draft') &&
    request.postDataJSON().operations?.[0]?.field_path === 'campaign.objective',
  )
  await objective.fill('在 30 天内提升企业试用转化')
  await expect(objective.locator('xpath=..').getByText('等待自动保存…')).toBeVisible()
  const objectiveRequest = await objectivePatch
  expect(objectiveRequest.postDataJSON().confirmation_mode).toBe('draft')
  await expect(objective.locator('xpath=..').getByText('已自动保存 · 待确认')).toBeVisible()

  const audienceValues: string[] = []
  let releaseFirstAudienceRequest!: () => void
  const firstAudienceRequest = new Promise<void>(resolve => { releaseFirstAudienceRequest = resolve })
  await page.route('**/api/strategy/v1/tasks/*/brief-draft', async route => {
    const body = route.request().postDataJSON()
    if (route.request().method() !== 'PATCH' || body.confirmation_mode !== 'draft' || body.operations?.[0]?.field_path !== 'audience.primary') {
      await route.continue()
      return
    }
    audienceValues.push(body.operations[0].value)
    if (audienceValues.length === 1) {
      releaseFirstAudienceRequest()
      await new Promise(resolve => setTimeout(resolve, 400))
    }
    await route.continue()
  })
  const audience = page.getByRole('textbox', { name: /核心受众/ })
  await audience.fill('科技公司运营负责人和市场负责人')
  await firstAudienceRequest
  await audience.fill('科技公司运营负责人、市场负责人和销售负责人')
  await expect.poll(() => audienceValues).toEqual([
    '科技公司运营负责人和市场负责人',
    '科技公司运营负责人、市场负责人和销售负责人',
  ])
  await expect(audience.locator('xpath=..').getByText('已自动保存 · 待确认')).toBeVisible()
  await expect(audience.locator('xpath=..').getByText('服务端已更新，请核对保留的输入')).toHaveCount(0)

  const proposition = page.getByRole('textbox', { name: /核心主张/ })
  const propositionPatch = page.waitForRequest(request =>
    request.method() === 'PATCH' && new URL(request.url()).pathname.endsWith('/brief-draft') &&
    request.postDataJSON().operations?.[0]?.field_path === 'proposition',
  )
  await proposition.fill('所有团队在同一事实源上协作')
  const propositionRequest = await propositionPatch
  expect(propositionRequest.postDataJSON().confirmation_mode).toBe('draft')
  await expect(proposition.locator('xpath=..').getByText('已自动保存 · 需单独确认')).toBeVisible()

  const confirmPatch = page.waitForRequest(request =>
    request.method() === 'PATCH' && new URL(request.url()).pathname.endsWith('/brief-draft') &&
    request.postDataJSON().operations?.[0]?.field_path === 'proposition' &&
    request.postDataJSON().confirmation_mode === 'confirm',
  )
  await proposition.locator('xpath=..').getByRole('button', { name: '确认此字段' }).click()
  await confirmPatch
  await expect(proposition.locator('xpath=..').getByText('已保存 · 已确认')).toBeVisible()
  expect(consoleErrors).toEqual([])
})

test('one natural-language requirement reaches an immutable StrategyPackage and read-only handoff', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  const failedResponses: string[] = []
  page.on('response', response => {
    if (response.status() >= 400) failedResponses.push(`${response.status()} ${response.url()}`)
  })
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page, ['product_guerlain_abeille_royale'])
  consoleErrors.length = 0
  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)

  const requirement = '推广 娇兰第三代黄金复原蜜，目标是提升 30 天企业试用转化，核心受众是 20-200 人科技公司的运营负责人；强调跨团队流程透明和减少重复沟通。'
  const intakeComposer = page.getByRole('textbox', { name: /例如：我们要给 FlowKit/ })
  await intakeComposer.fill('这是一条尚未发送、切换阶段后仍应保留的需求草稿。')
  await page.getByRole('button', { name: /^Brief：/ }).click()
  await page.getByRole('button', { name: /^理解需求：/ }).click()
  await expect(intakeComposer).toHaveValue('这是一条尚未发送、切换阶段后仍应保留的需求草稿。')
  await intakeComposer.fill(requirement)
  await page.getByRole('button', { name: '发送需求消息' }).click()

  await expect(page.getByLabel('需求收敛状态')).toContainText('3 / 3 项核心信息')
  await page.getByRole('button', { name: /^AI 助手/ }).click()
  await expect(page.getByLabel('AI 当前理解')).toContainText('娇兰第三代黄金复原蜜')
  await expect(page.getByLabel('AI 当前理解')).toContainText('提升 30 天企业试用转化')
  await expect(page.getByLabel('AI 当前理解')).toContainText('20-200 人科技公司的运营负责人')
  await expect(page.getByLabel('AI 当前理解')).toContainText('跨团队流程透明和减少重复沟通')
  await expect(page.getByText('这次最重要的业务目标是什么？')).toHaveCount(0)
  await expect(page.getByText('最希望影响哪一类核心人群？')).toHaveCount(0)
  await page.getByRole('button', { name: '收起 AI 助手' }).click()

  await page.getByRole('button', { name: /^Brief：/ }).click()
  const unsavedRegion = page.getByRole('region', { name: '受众与情境' }).getByRole('textbox', { name: /^地区/ })
  await unsavedRegion.fill('华东临时草稿')
  const strategyMain = page.locator('.kanon-strategy-main')
  const briefScrollTop = await strategyMain.evaluate(element => {
    element.scrollTo({ top: element.scrollHeight })
    return element.scrollTop
  })
  expect(briefScrollTop).toBeGreaterThan(0)
  await page.getByRole('button', { name: /^理解需求：/ }).click()
  await page.getByRole('button', { name: /^Brief：/ }).click()
  await expect(unsavedRegion).toHaveValue('华东临时草稿')
  await expect.poll(() => strategyMain.evaluate(element => element.scrollTop)).toBeGreaterThanOrEqual(briefScrollTop - 2)
  await unsavedRegion.fill('')
  await page.getByRole('button', { name: /^理解需求：/ }).click()
  await page.getByRole('button', { name: '确认理解并锁定需求' }).click()
  await page.getByRole('button', { name: /^Brief：/ }).click()
  const objectiveGroup = page.getByRole('region', { name: '业务目标' })
  await expect(objectiveGroup).toContainText('2/2 已确认')
  await expect(objectiveGroup).toContainText('跨团队流程透明和减少重复沟通')

  await page.getByRole('button', { name: /^策略：/ }).click()
  await page.getByRole('button', { name: '创建 Brief 补充修订' }).click()
  await page.getByRole('textbox', { name: '渠道 待补充' }).fill('douyin')
  await page.getByRole('button', { name: '保存并确认' }).click()
  await expect(page.getByRole('button', { name: '确认并冻结 Brief' })).toBeEnabled()
  await page.getByRole('button', { name: '确认并冻结 Brief' }).click()
  await expect(page.getByRole('button', { name: 'Brief 已冻结' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '策略：可以生成' })).toBeVisible()

  await page.getByRole('button', { name: '策略：可以生成' }).click()
  await page.getByRole('button', { name: '生成第一版策略' }).click()
  await expect(page.getByText('STRATEGY REVISION 1')).toBeVisible({ timeout: 15_000 })
  const publishStrategy = page.getByRole('button', { name: '确认并发布策略包' })
  await expect(publishStrategy).toBeVisible()
  const sectionEditor = page.locator('.kanon-strategy-section-editor')
  const objectiveSection = sectionEditor.getByRole('button', { name: /目标/ })
  const audienceSection = sectionEditor.getByRole('button', { name: /受众/ })
  const objectiveEditor = sectionEditor.getByRole('textbox', { name: '目标', exact: true })
  const originalObjective = await objectiveEditor.inputValue()
  await objectiveEditor.fill(`${originalObjective}（临时修改）`)
  await audienceSection.click()
  await expect(objectiveSection).toContainText('未保存')
  await objectiveSection.click()
  await expect(objectiveEditor).toHaveValue(`${originalObjective}（临时修改）`)
  await expect(publishStrategy).toBeDisabled()
  await page.getByRole('button', { name: '放弃本次修改' }).click()
  await expect(objectiveEditor).toHaveValue(originalObjective)
  await expect(publishStrategy).toBeEnabled()

  await page.getByRole('button', { name: /^确认 \/ 评审：/ }).click()
  await expect(page.getByText('SELF CONFIRMATION')).toBeVisible()
  await expect(page.getByText('个人模式不会创建“提交给自己评审”的中间步骤。')).toBeVisible()
  await page.getByRole('button', { name: '确认并发布策略包' }).click()
  await expect(page.getByText('已发布 v1')).toBeVisible()
  await page.getByRole('button', { name: '开始创意交接' }).click()

  const sourcePackage = page.getByRole('region', { name: '创意交接来源包' })
  await expect(sourcePackage).toContainText('只读来源')
  await expect(sourcePackage).toContainText('StrategyPackage v1')
  await expect(sourcePackage).toContainText('Package hash')
  await expect(sourcePackage).toContainText('不会改变以上 Package 内容或哈希')
  const packageSummary = await sourcePackage.textContent()

  await expect(page.getByLabel('本次创作路线')).toHaveValue('route_douyin_commerce_preroll')
  await page.getByRole('button', { name: '确认此业务并创建任务计划' }).click()
  const sellingPointAnswer = page.getByRole('textbox', { name: '核心卖点的优先顺序是什么？' })
  await sellingPointAnswer.fill('流程透明优先，其次减少重复沟通')
  await expect(page.getByRole('button', { name: '生成任务规格' })).toBeDisabled()
  await page.getByRole('button', { name: /^策略：/ }).click()
  await page.getByRole('button', { name: /^创意交接：/ }).click()
  await expect(sellingPointAnswer).toHaveValue('流程透明优先，其次减少重复沟通')
  await page.getByRole('textbox', { name: '价格、优惠、赠品和期限有哪些已确认事实？' }).fill('当前没有已确认优惠，不在创意中虚构')
  await page.getByRole('combobox', { name: '希望用户下一步做什么？' }).selectOption('visit')
  await page.getByRole('button', { name: '保存并重新校验' }).click()
  await expect(page.getByRole('button', { name: '生成任务规格' })).toBeEnabled()
  await page.getByRole('button', { name: '生成任务规格' }).click()
  await expect(page.getByText('已可交接到创意工作台')).toBeVisible({ timeout: 15_000 })

  await page.reload()
  await expect(page).toHaveURL(/\/strategy\/workspaces\/[^/]+\/handoff$/)
  await expect(page.getByRole('region', { name: '创意交接来源包' })).toHaveText(packageSummary ?? '')
  expect(consoleErrors, failedResponses.join('\n')).toEqual([])
})

test('formal review binds approval to one revision and invalidates a stale candidate', async ({ page }) => {
  test.setTimeout(60_000)
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/reviews`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page, ['product_guerlain_abeille_royale'])
  const root = `/projects/${isolatedProject.id}/strategy`
  consoleErrors.length = 0

  await page.goto(`${root}/reviews`)
  await waitForProject(page, isolatedProject.name)
  await page.getByLabel('评审模式').selectOption('designated_approvers')
  await page.getByRole('textbox', { name: '审批人 User ID' }).fill('user_local')
  await page.getByRole('checkbox', { name: '允许发起人同时作为指定审批人' }).check()
  await page.getByRole('button', { name: '保存评审策略' }).click()
  await expect.poll(async () => {
    const response = await page.request.get(`/api/strategy/v1/projects/${isolatedProject.id}/review-policy`)
    return response.ok() ? await response.json() : null
  }).toMatchObject({
    mode: 'designated_approvers',
    approver_user_ids: ['user_local'],
    allow_self_approval: true,
  })

  await ensureStartedWorkspace(page, root, isolatedProject.name)
  await page.getByRole('textbox', { name: /例如：我们要给 FlowKit/ }).fill(
    '推广 娇兰第三代黄金复原蜜，目标是提升购买转化，核心受众是关注高端护肤的消费者；强调修护价值和可信产品体验。',
  )
  await page.getByRole('button', { name: '发送需求消息' }).click()
  await expect(page.getByLabel('需求收敛状态')).toContainText('3 / 3 项核心信息')
  await page.getByRole('button', { name: '确认理解并锁定需求' }).click()
  await page.getByRole('button', { name: /^策略：/ }).click()
  await page.getByRole('button', { name: '创建 Brief 补充修订' }).click()
  await page.getByRole('textbox', { name: '渠道 待补充' }).fill('douyin')
  await page.getByRole('button', { name: '保存并确认' }).click()
  await page.getByRole('button', { name: '确认并冻结 Brief' }).click()
  await page.getByRole('button', { name: '策略：可以生成' }).click()
  await page.getByRole('button', { name: '生成第一版策略' }).click()
  await expect(page.getByText('STRATEGY REVISION 1')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByRole('button', { name: '提交正式评审' })).toBeVisible()

  const firstSubmission = page.waitForResponse(response =>
    response.request().method() === 'POST' && /\/strategy-drafts\/[^/]+:submit$/.test(new URL(response.url()).pathname),
  )
  await page.getByRole('button', { name: '提交正式评审' }).click()
  const firstReviewResponse = await firstSubmission
  expect(firstReviewResponse.ok()).toBe(true)
  const firstReview = await firstReviewResponse.json() as {
    id: string
    strategy_id: string
    candidate_revision: number
    candidate_content_hash: string
  }
  expect(firstReview.candidate_revision).toBe(1)

  await page.getByRole('button', { name: /^确认 \/ 评审：/ }).click()
  await expect(page.getByRole('heading', { name: 'Revision 1 评审' })).toBeVisible()
  await expect(page.getByText('指定审批人')).toBeVisible()
  await expect(page.getByText('user_local')).toBeVisible()
  const approveReview = page.getByRole('button', { name: '批准并发布策略包' })
  const reviewComment = page.getByPlaceholder('留下可执行评论')
  await expect(approveReview).toBeVisible()
  await reviewComment.fill('确认渠道证据后再批准')
  await expect(approveReview).toBeDisabled()
  await page.getByRole('button', { name: /^策略：/ }).click()
  await page.getByRole('button', { name: /^确认 \/ 评审：/ }).click()
  await expect(reviewComment).toHaveValue('确认渠道证据后再批准')
  await reviewComment.fill('')
  await expect(approveReview).toBeEnabled()

  const draftResponse = await page.request.get(`/api/strategy/v1/strategy-drafts/${firstReview.strategy_id}`)
  expect(draftResponse.ok()).toBe(true)
  const draft = await draftResponse.json() as { version: number; current_revision: number }
  const patchResponse = await page.request.patch(`/api/strategy/v1/strategy-drafts/${firstReview.strategy_id}`, {
    headers: { 'Idempotency-Key': `formal-review-patch-${Date.now()}` },
    data: {
      expected_version: draft.version,
      base_revision: draft.current_revision,
      section: 'objective',
      value: '在不改变证据边界的前提下提升购买转化与高意向访问',
    },
  })
  expect(patchResponse.ok()).toBe(true)
  const patchedDraft = await patchResponse.json() as { version: number; current_revision: number }
  expect(patchedDraft.current_revision).toBe(2)

  const staleApproval = await page.request.post(`/api/strategy/v1/strategy-drafts/${firstReview.strategy_id}:approve`, {
    headers: { 'Idempotency-Key': `formal-review-stale-${Date.now()}` },
    data: {
      review_id: firstReview.id,
      candidate_content_hash: firstReview.candidate_content_hash,
      expected_version: patchedDraft.version,
    },
  })
  expect(staleApproval.status()).toBe(409)
  expect(await staleApproval.json()).toMatchObject({ error: { code: 'REVIEW_STALE' } })

  await page.goto(`${root}/reviews`)
  await waitForProject(page, isolatedProject.name)
  await page.getByRole('tab', { name: '已完成', exact: true }).click()
  await expect(page.getByText('已失效')).toBeVisible()

  const workspaceId = /\/workspaces\/([^/]+)/.exec(firstReviewResponse.url())?.[1]
    ?? /\/workspaces\/([^/]+)/.exec(page.url())?.[1]
  const workspaceList = await page.request.get(`/api/strategy/v1/projects/${isolatedProject.id}/workspaces`)
  const workspaceItems = await workspaceList.json() as { items: Array<{ id: string }> }
  const activeWorkspaceId = workspaceId ?? workspaceItems.items[0]?.id
  expect(activeWorkspaceId).toBeTruthy()
  await page.goto(`${root}/workspaces/${activeWorkspaceId}/strategy`)
  await expect(page.getByText('STRATEGY REVISION 2')).toBeVisible()

  await page.getByRole('button', { name: '提交正式评审' }).click()
  await page.getByRole('button', { name: /^确认 \/ 评审：/ }).click()
  await expect(page.getByRole('heading', { name: 'Revision 2 评审' })).toBeVisible()
  await page.getByRole('button', { name: '批准并发布策略包' }).click()
  await expect(page.getByText('已发布 v1')).toBeVisible()
  await expect(page.getByText('策略已确认并发布')).toBeVisible()
  expect(consoleErrors).toEqual([])
})

test('deep research stays non-blocking while Activity streams rounds into explicit Brief adoption', async ({ page }) => {
  test.setTimeout(60_000)
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/research`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  const researchRunId = `researchrun_e2e_${Date.now()}`
  const allowRunning = deferred<void>()
  const allowCompletion = deferred<void>()
  let workspaceId = ''
  let conversationId = ''
  let taskId = ''
  let briefDraft: Record<string, any> | null = null
  let researchQuery = ''
  let researchPosted = false
  let runningDelivered = false
  let completed = false
  let adopted = false
  let applyRequest: { body?: Record<string, unknown>; idempotencyKey?: string } = {}

  await page.route(`**/api/strategy/v1/projects/${isolatedProject.id}/activities**`, async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/events')) {
      await allowRunning.promise
      runningDelivered = true
      const snapshot = researchActivitySnapshot(completed ? 'completed' : 'running', {
        projectId: isolatedProject.id, workspaceId, conversationId, researchRunId,
      })
      await route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: `id: ${snapshot.snapshot_id}\nevent: activity.snapshot\ndata: ${JSON.stringify(snapshot)}\n\n`,
      })
      return
    }
    if (!runningDelivered) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(emptyActivitySnapshot('1')) })
      return
    }
    await allowCompletion.promise
    completed = true
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(researchActivitySnapshot('completed', {
        projectId: isolatedProject.id, workspaceId, conversationId, researchRunId,
      })),
    })
  })

  const researchRunsPath = `/platform/v1/projects/${isolatedProject.id}/knowledge/research-runs`
  await page.route(`**${researchRunsPath}**`, async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === researchRunsPath && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, any>
      researchQuery = String(body.query ?? '')
      researchPosted = true
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(researchRunFixture('queued', {
          projectId: isolatedProject.id, workspaceId, researchRunId, query: researchQuery,
        })),
      })
      return
    }
    if (url.pathname === researchRunsPath && request.method() === 'GET') {
      const items = researchPosted
        ? [researchRunFixture(completed ? 'completed' : 'running', {
            projectId: isolatedProject.id, workspaceId, researchRunId, query: researchQuery,
          })]
        : []
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items }) })
      return
    }
    if (url.pathname === `${researchRunsPath}/${researchRunId}` && request.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(researchRunFixture(completed ? 'completed' : 'running', {
          projectId: isolatedProject.id, workspaceId, researchRunId, query: researchQuery,
        })),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/strategy/v1/workspaces/*/research-adoption-proposals?*', async route => {
    const items = completed && briefDraft
      ? [researchProposalFixture(adopted ? 'applied' : 'proposed', {
          projectId: isolatedProject.id, workspaceId, conversationId, researchRunId,
          briefDraft,
        })]
      : []
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items }) })
  })
  await page.route('**/api/strategy/v1/research-adoption-proposals/*:apply', async route => {
    const request = route.request()
    applyRequest = {
      body: request.postDataJSON() as Record<string, unknown>,
      idempotencyKey: request.headers()['idempotency-key'],
    }
    adopted = true
    const updatedBrief = adoptedBriefFixture(briefDraft as Record<string, any>)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        proposal: researchProposalFixture('applied', {
          projectId: isolatedProject.id, workspaceId, conversationId, researchRunId,
          briefDraft: briefDraft as Record<string, any>,
        }),
        brief_draft: updatedBrief,
      }),
    })
  })

  consoleErrors.length = 0
  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)
  workspaceId = page.url().match(/\/workspaces\/([^/]+)\//)?.[1] ?? ''
  const workspaceResponse = await page.request.get(`/api/strategy/v1/workspaces/${workspaceId}`)
  expect(workspaceResponse.ok()).toBe(true)
  const workspace = await workspaceResponse.json() as Record<string, any>
  conversationId = String(workspace.current_conversation?.id ?? '')
  taskId = String(workspace.current_task?.id ?? '')
  const briefResponse = await page.request.get(`/api/strategy/v1/tasks/${taskId}/brief-draft`)
  expect(briefResponse.ok()).toBe(true)
  briefDraft = await briefResponse.json() as Record<string, any>

  await page.route(`**/api/strategy/v1/tasks/${taskId}/brief-draft`, async route => {
    if (route.request().method() === 'GET' && adopted && briefDraft) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(adoptedBriefFixture(briefDraft)) })
      return
    }
    await route.continue()
  })

  await page.getByRole('button', { name: /^Brief：/ }).click()
  await page.getByRole('button', { name: '研究', exact: true }).click()
  const researchQuestion = '哪些证据边界可以提高渠道效率主张的可信度？'
  const researchQuestionInput = page.getByRole('textbox', { name: '要验证的问题' })
  await researchQuestionInput.fill(researchQuestion)
  await page.getByRole('button', { name: '关闭研究' }).click()
  await expect(page.getByRole('complementary', { name: '研究' })).toHaveCount(0)
  await page.getByRole('button', { name: '研究', exact: true }).click()
  await expect(researchQuestionInput).toHaveValue(researchQuestion)
  const startResearch = page.getByRole('button', { name: '开始深度研究' })
  await expect(startResearch).toBeEnabled({ timeout: 15_000 })
  await startResearch.click()
  await expect.poll(() => researchPosted).toBe(true)
  await expect(page.getByLabel('研究进度摘要')).toContainText('0 / 6')

  allowRunning.resolve()
  await expect(page.getByLabel('研究进度摘要')).toContainText('1 / 6', { timeout: 10_000 })
  await expect(page.getByRole('heading', { name: '双来源证据边界能降低渠道效率主张的误导风险' })).toBeVisible()

  await page.getByRole('button', { name: /^理解需求：/ }).click()
  const requirementInput = page.getByRole('textbox', { name: /例如：我们要给 FlowKit/ })
  await expect(requirementInput).toBeEnabled()
  await requirementInput.fill('研究在后台运行时，用户仍可继续补充需求。')
  await expect(requirementInput).toHaveValue('研究在后台运行时，用户仍可继续补充需求。')

  await page.getByRole('button', { name: /^Brief：/ }).click()
  await page.getByRole('button', { name: '研究', exact: true }).click()
  await expect(page.getByLabel('研究进度摘要')).toContainText('1 / 6')
  allowCompletion.resolve()
  await expect(page.getByLabel('研究进度摘要')).toContainText('2 / 6', { timeout: 10_000 })
  await expect(page.getByText('研究完成', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '研究报告' })).toBeVisible()
  const reportDownloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载报告' }).click()
  const reportDownload = await reportDownloadPromise
  expect(reportDownload.suggestedFilename()).toBe(`research-report-${researchRunId}.md`)
  await expect(page.locator('.kanon-research-proposal')).toContainText('当前值')
  await expect(page.locator('.kanon-research-proposal')).toContainText('所有渠道效率主张必须附双来源证据')

  const desktopViewport = page.viewportSize()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('complementary', { name: '研究' })).toBeVisible()
  const mobilePanelGeometry = await page.evaluate(() => {
    const stage = document.querySelector('.strategy-v2-stage-region')?.getBoundingClientRect()
    const panel = document.querySelector('.strategy-supporting-panel')?.getBoundingClientRect()
    if (!stage || !panel) return null
    return {
      stageLeft: stage.left,
      stageRight: stage.right,
      panelLeft: panel.left,
      panelRight: panel.right,
    }
  })
  expect(mobilePanelGeometry).not.toBeNull()
  expect(mobilePanelGeometry!.panelLeft).toBeGreaterThanOrEqual(mobilePanelGeometry!.stageLeft - 1)
  expect(mobilePanelGeometry!.panelRight).toBeLessThanOrEqual(mobilePanelGeometry!.stageRight + 1)
  await page.setViewportSize(desktopViewport ?? { width: 1440, height: 900 })

  await page.getByRole('button', { name: '采纳', exact: true }).click()
  await expect.poll(() => adopted).toBe(true)
  expect(applyRequest.body).toMatchObject({ expected_version: 1 })
  expect(applyRequest.idempotencyKey).toMatch(/^research-proposal-apply-/)
  await expect(page.getByText('已由用户确认并写入新版本')).toBeVisible({ timeout: 10_000 })

  await page.getByRole('button', { name: /^Brief：/ }).click()
  await expect(page.getByRole('region', { name: '资源与约束' })).toContainText('所有渠道效率主张必须附双来源证据')
  expect(consoleErrors).toEqual([])
})

test('workspace stage, supporting panel, assistant, and activity state survive navigation and reload', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  const assistantDocumentResponse = await page.request.post(
    `/platform/v1/projects/${isolatedProject.id}/knowledge/documents`,
    { data: { title: 'Assistant context E2E', source_type: 'docs', text: '本资料只用于验证下一轮来源选择。' } },
  )
  expect(assistantDocumentResponse.ok()).toBe(true)
  const assistantDocument = await assistantDocumentResponse.json() as { id: string }
  let activityStreamAvailable = false
  let excludedAssistantSourceHeader = ''
  const assistantSourceId = assistantDocument.id
  page.on('request', request => {
    if (request.method() === 'POST' && /\/api\/strategy\/v1\/conversations\/[^/]+\/messages$/.test(new URL(request.url()).pathname)) {
      excludedAssistantSourceHeader = request.headers()['x-strategy-excluded-source-ids'] ?? ''
    }
  })
  await page.route(`**/api/strategy/v1/projects/${isolatedProject.id}/activities/events?*`, async route => {
    if (activityStreamAvailable) {
      await route.continue()
      return
    }
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: { message: 'E2E injected activity stream disconnect' } }),
    })
  })
  consoleErrors.length = 0
  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)
  const assistantWorkspaceId = /\/workspaces\/([^/]+)/.exec(page.url())?.[1]
  expect(assistantWorkspaceId).toBeTruthy()
  const assistantWorkspaceResponse = await page.request.get(`/api/strategy/v1/workspaces/${assistantWorkspaceId}`)
  expect(assistantWorkspaceResponse.ok()).toBe(true)
  const assistantWorkspace = await assistantWorkspaceResponse.json() as { current_task: { id: string } }
  const assistantBriefResponse = await page.request.get(`/api/strategy/v1/tasks/${assistantWorkspace.current_task.id}/brief-draft`)
  expect(assistantBriefResponse.ok()).toBe(true)
  const assistantBrief = await assistantBriefResponse.json() as { version: number }
  const assistantBriefPatchResponse = await page.request.patch(
    `/api/strategy/v1/tasks/${assistantWorkspace.current_task.id}/brief-draft`,
    {
      headers: { 'Idempotency-Key': `assistant-context-e2e-${Date.now()}` },
      data: {
        expected_version: assistantBrief.version,
        operations: [{ op: 'set', field_path: 'reference_ids', value: [assistantSourceId] }],
      },
    },
  )
  expect(assistantBriefPatchResponse.ok(), await assistantBriefPatchResponse.text()).toBe(true)

  await page.getByRole('button', { name: /^后台任务/ }).click()
  await expect(page.getByText(/连接恢复中，已核对服务端快照|暂时离线，系统会自动重试/)).toBeVisible()
  activityStreamAvailable = true
  await expect(page.getByText('状态实时同步')).toBeVisible({ timeout: 10_000 })
  consoleErrors.length = 0

  const briefStage = page.getByRole('button', { name: /^Brief：/ })
  await briefStage.focus()
  await expect(briefStage).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/strategy\/workspaces\/[^/]+\/brief$/)
  await expect(page.getByRole('heading', { name: '确认 Brief', exact: true })).toBeFocused()

  await page.getByRole('button', { name: '研究', exact: true }).click()
  await expect(page).toHaveURL(/\/brief\?panel=research$/)
  await expect(page.getByRole('complementary', { name: '研究' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '把外部与内部资料变成决策依据', exact: true })).toBeVisible()

  await page.reload()
  await expect(page).toHaveURL(/\/brief\?panel=research$/)
  await expect(page.getByRole('complementary', { name: '研究' })).toBeVisible()

  const assistant = page.getByRole('button', { name: /^AI 助手/ })
  await assistant.click()
  await expect(assistant).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('complementary', { name: '项目 AI 助手' })).toBeVisible()
  await expect(page).toHaveURL(/\/brief$/)
  await expect(page.getByRole('complementary', { name: '研究' })).toHaveCount(0)
  await page.getByText('结构化上下文', { exact: true }).click()
  await expect(page.getByText(assistantSourceId, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: `下一轮不使用来源 ${assistantSourceId}` }).click()
  await page.getByRole('textbox', { name: '给当前项目补充信息' }).fill('仅检查当前阶段，不修改 Brief。')
  const assistantMessageResponsePromise = page.waitForResponse(response =>
    response.request().method() === 'POST' && /\/api\/strategy\/v1\/conversations\/[^/]+\/messages$/.test(new URL(response.url()).pathname),
  )
  await page.getByRole('button', { name: '发送给项目 AI 助手' }).click()
  const assistantMessageResponse = await assistantMessageResponsePromise
  expect(assistantMessageResponse.ok(), JSON.stringify({
    response: await assistantMessageResponse.text(),
    headers: assistantMessageResponse.request().headers(),
    body: assistantMessageResponse.request().postData(),
  })).toBe(true)
  await expect.poll(() => excludedAssistantSourceHeader).toBe(JSON.stringify([assistantSourceId]))
  await expect(page.getByRole('button', { name: `恢复来源 ${assistantSourceId}` })).toHaveCount(0)

  await page.getByRole('button', { name: '沉浸展开 AI 助手' }).click()
  await expect(page.locator('.strategy-v2-shell')).toHaveAttribute('data-assistant-expanded', 'true')
  await expect(page.getByRole('complementary', { name: '项目 AI 助手' })).toBeVisible()
  await page.reload()
  await expect(page.locator('.strategy-v2-shell')).toHaveAttribute('data-assistant-expanded', 'true')
  const expandedAssistantLayout = await page.evaluate(() => {
    const body = document.querySelector('.strategy-v2-body')?.getBoundingClientRect()
    const dock = document.querySelector('.project-assistant-dock')?.getBoundingClientRect()
    const stage = document.querySelector('.strategy-v2-stage-region')
    return {
      widthDifference: body && dock ? Math.abs(body.width - dock.width) : Number.POSITIVE_INFINITY,
      stageDisplay: stage ? getComputedStyle(stage).display : '',
    }
  })
  expect(expandedAssistantLayout.widthDifference).toBeLessThanOrEqual(1)
  expect(expandedAssistantLayout.stageDisplay).toBe('none')
  await page.getByRole('button', { name: '收起 AI 助手' }).click()
  await expect(page.locator('.strategy-v2-shell')).toHaveAttribute('data-assistant-expanded', 'false')
  await expect(page.getByRole('heading', { name: '确认 Brief', exact: true })).toBeVisible()
  await page.getByRole('button', { name: /^AI 助手/ }).click()
  await expect(page.locator('.strategy-v2-shell')).toHaveAttribute('data-assistant-expanded', 'true')
  await page.keyboard.press('Escape')
  await expect(page.locator('.strategy-v2-shell')).toHaveAttribute('data-assistant-expanded', 'false')
  await expect(page.getByRole('complementary', { name: '项目 AI 助手' })).toBeVisible()

  await page.getByRole('button', { name: /^后台任务/ }).click()
  await expect(page).toHaveURL(/\/brief\?panel=activity$/)
  await expect(page.getByRole('complementary', { name: '后台任务' })).toBeVisible()
  await expect(page.getByText(/状态实时同步|正在连接任务状态|连接恢复中|暂时离线/)).toBeVisible()

  await page.reload()
  await expect(page).toHaveURL(/\/brief\?panel=activity$/)
  await expect(page.getByRole('complementary', { name: '后台任务' })).toBeVisible()
  expect(consoleErrors).toEqual([])
})

test('long Chinese content and dense project state remain navigable without layout overflow', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  const isolatedProject = await createIsolatedProject(page)
  const stressResearchRunId = `researchrun_stress_${Date.now()}`
  const stressWorkspaceId = 'workspace_stress_projection'
  const stressRun = stressResearchRunFixture(isolatedProject.id, stressWorkspaceId, stressResearchRunId)
  const stressDocuments = Array.from({ length: 20 }, (_, index) => stressDocumentFixture(isolatedProject.id, index + 1))
  const stressMessages = Array.from({ length: 50 }, (_, index) => ({
    id: `message_stress_${index + 1}`,
    conversation_id: 'conversation_stress_projection',
    role: index % 2 === 0 ? 'user' : 'assistant',
    content_type: 'text',
    content: `第 ${index + 1} 条项目对话：${'这是一段用于验证长中文内容、自动换行、滚动恢复和信息密度的真实长度描述。'.repeat(4)}`,
    ai_generated: index % 2 === 1,
    created_at: `2026-08-11T03:${String(index).padStart(2, '0')}:00Z`,
  }))
  const stressActivities = stressActivitySnapshot(isolatedProject.id, 10)

  await page.route('**/api/strategy/v1/conversations/*/messages?*', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: stressMessages, next_cursor: '' }) })
  })
  await page.route('**/api/strategy/v1/conversations/*/memory', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      summary: '项目上下文已压缩，保留目标、约束和来源引用。',
      summary_kind: 'deterministic', summary_content_hash: `sha256:${'c'.repeat(64)}`,
      open_questions: ['需确认最终投放地区'], last_message_id: stressMessages.at(-1)?.id ?? '',
      recent_window_start_message_id: stressMessages.at(-20)?.id ?? '',
      artifact_manifest: { brief_ref: { type: 'brief_draft', id: 'brief_stress', version: 1, content_hash: `sha256:${'a'.repeat(64)}` }, selected_source_ids: stressDocuments.slice(0, 4).map(item => item.id) },
      version: 1,
    }) })
  })
  await page.route(`**/platform/v1/projects/${isolatedProject.id}/knowledge/documents`, async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: stressDocuments }) })
  })
  await page.route(`**/platform/v1/projects/${isolatedProject.id}/knowledge/documents/*/preview`, async route => {
    const documentId = new URL(route.request().url()).pathname.split('/').at(-2) ?? stressDocuments[0].id
    const document = stressDocuments.find(item => item.id === documentId) ?? stressDocuments[0]
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      contract_version: 'platform-document-preview/v1', document_id: document.id,
      filename: document.filename, mime_type: document.mime_type, status: document.status,
      parse_strategy: document.parse_strategy, parse_phase: document.parse_phase,
      parse_progress: document.parse_progress, progress_kind: document.progress_kind,
      processed_pages: document.processed_pages, total_pages: document.total_pages,
      quality_score: document.quality_score, quality_tier: document.quality_tier,
      fallback_reason: '', preview_status: 'ready', page_quality_summary: document.page_quality_summary,
      heartbeat_at: null, chunk_count: document.chunk_count,
      text: `资料 ${document.id} 的长中文预览。${'项目证据与约束需要保留精确来源定位。'.repeat(20)}`,
      text_truncated: false, total_characters: 420, original_available: true,
      chunks: [{ id: `chunk_${document.id}`, index: 0, section: '正文', start_line: 1, end_line: 8, snippet: '项目证据与约束需要保留精确来源定位。', locator: { start_line: 1, end_line: 8 } }],
    }) })
  })
  const researchRunsPath = `/platform/v1/projects/${isolatedProject.id}/knowledge/research-runs`
  await page.route(`**${researchRunsPath}**`, async route => {
    const url = new URL(route.request().url())
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(
      url.pathname === researchRunsPath ? { items: [stressRun] } : stressRun,
    ) })
  })
  await page.route('**/api/strategy/v1/workspaces/*/research-adoption-proposals?*', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
  })
  await page.route(`**/api/strategy/v1/projects/${isolatedProject.id}/activities**`, async route => {
    if (new URL(route.request().url()).pathname.endsWith('/events')) {
      await route.fulfill({
        status: 200, contentType: 'text/event-stream',
        body: `id: ${stressActivities.snapshot_id}\nevent: activity.snapshot\ndata: ${JSON.stringify(stressActivities)}\n\n`,
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(stressActivities) })
  })

  consoleErrors.length = 0
  const denseBootstrapStartedAt = performance.now()
  await ensureStartedWorkspace(page, `/projects/${isolatedProject.id}/strategy`, isolatedProject.name)
  await expect(page.locator('.kanon-message-list .kanon-message')).toHaveCount(50)
  await expect(page.locator('.kanon-message-list')).toContainText('第 50 条项目对话')
  const denseMessagesReadyMs = performance.now() - denseBootstrapStartedAt

  const briefStartedAt = performance.now()
  await page.getByRole('button', { name: /^Brief：/ }).click()
  await expect(page.getByRole('heading', { name: '确认策略输入' })).toBeVisible()
  const briefReadyMs = performance.now() - briefStartedAt
  const researchStartedAt = performance.now()
  await page.getByRole('button', { name: '研究', exact: true }).click()
  await expect(page.locator('.kanon-finding-card')).toHaveCount(30)
  const researchReadyMs = performance.now() - researchStartedAt
  await expect(page.getByLabel('研究进度摘要')).toContainText('已确认结论25')
  await expect(page.getByLabel('研究进度摘要')).toContainText('5 条存在冲突')
  await page.getByRole('button', { name: '关闭研究' }).click()

  const materialsStartedAt = performance.now()
  await page.getByRole('button', { name: '资料', exact: true }).click()
  await expect(page.locator('.strategy-materials__queue > button')).toHaveCount(20)
  const materialsReadyMs = performance.now() - materialsStartedAt
  await expect(page.locator('.strategy-materials__preview pre')).toContainText('项目证据与约束')
  await page.getByRole('button', { name: '关闭项目资料' }).click()

  const activityStartedAt = performance.now()
  await page.getByRole('button', { name: /^后台任务/ }).click()
  await expect(page.locator('.strategy-activity-card')).toHaveCount(10)
  const activityReadyMs = performance.now() - activityStartedAt
  const layout = await page.evaluate(() => ({
    viewportWidth: window.innerWidth,
    documentWidth: document.documentElement.scrollWidth,
    stageWidth: document.querySelector('.strategy-v2-stage-region')?.scrollWidth ?? 0,
    stageClientWidth: document.querySelector('.strategy-v2-stage-region')?.clientWidth ?? 0,
  }))
  expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth)
  expect(layout.stageWidth).toBeLessThanOrEqual(layout.stageClientWidth + 1)
  const performanceEvidence = {
    denseMessagesReadyMs, briefReadyMs, researchReadyMs, materialsReadyMs, activityReadyMs,
    messageCount: 50, findingCount: 30, documentCount: 20, runningActivityCount: 10,
  }
  console.info(`[strategy-performance] dense ${JSON.stringify(performanceEvidence)}`)
  await testInfo.attach('strategy-dense-performance.json', {
    body: Buffer.from(JSON.stringify(performanceEvidence, null, 2)),
    contentType: 'application/json',
  })
  expect(Math.max(briefReadyMs, researchReadyMs, materialsReadyMs, activityReadyMs)).toBeLessThan(1_500)
  expect(denseMessagesReadyMs).toBeLessThan(15_000)
  expect(consoleErrors).toEqual([])
})

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>(next => { resolve = next })
  return { promise, resolve }
}

function emptyActivitySnapshot(seed: string) {
  return {
    contract_version: 'strategy-task-activity-snapshot/v1',
    snapshot_id: `sha256:${seed.repeat(64).slice(0, 64)}`,
    captured_at: '2026-08-11T02:00:00Z',
    items: [],
  }
}

function researchActivitySnapshot(stage: 'running' | 'completed', context: {
  projectId: string
  workspaceId: string
  conversationId: string
  researchRunId: string
}) {
  const terminal = stage === 'completed'
  return {
    contract_version: 'strategy-task-activity-snapshot/v1',
    snapshot_id: `sha256:${(terminal ? '3' : '2').repeat(64)}`,
    captured_at: terminal ? '2026-08-11T02:02:00Z' : '2026-08-11T02:01:00Z',
    items: [{
      contract_version: 'strategy-task-activity/v1',
      id: `activity_${context.researchRunId}`,
      kind: 'deep_research',
      status: terminal ? 'succeeded' : 'running',
      phase: terminal ? 'completed' : 'cross_checking',
      round: { current: terminal ? 2 : 1, max: 6 },
      progress: { kind: 'milestone', value: terminal ? 100 : 45, message: terminal ? '研究与引用审计已完成' : '正在交叉核验两个独立来源' },
      summary: terminal ? '报告和采纳建议已准备。' : '第一轮已形成一条可核验结论。',
      confirmed_conclusions: [{
        id: `finding_${context.researchRunId}`,
        text: '双来源证据边界能降低渠道效率主张的误导风险',
        status: 'verified',
        source_count: 2,
      }],
      source_scope: { project_id: context.projectId, workspace_id: context.workspaceId, conversation_id: context.conversationId },
      resource_ref: { type: 'knowledge_research_run', id: context.researchRunId },
      execution_ref: { type: 'platform_job', id: `job_${context.researchRunId}`, version: terminal ? 3 : 2 },
      actions: terminal ? ['open'] : ['open', 'cancel'],
      cancel_requested: false,
      heartbeat_at: terminal ? '2026-08-11T02:02:00Z' : '2026-08-11T02:01:00Z',
      updated_at: terminal ? '2026-08-11T02:02:00Z' : '2026-08-11T02:01:00Z',
    }],
  }
}

function researchRunFixture(stage: 'queued' | 'running' | 'completed', context: {
  projectId: string
  workspaceId: string
  researchRunId: string
  query: string
}) {
  const source = (suffix: 'a' | 'b') => ({
    id: `source_${suffix}_${context.researchRunId}`,
    organization_id: 'org_local',
    project_id: context.projectId,
    research_run_id: context.researchRunId,
    source_class: 'web',
    media_type: 'article',
    title: `独立来源 ${suffix.toUpperCase()}`,
    url: `https://source-${suffix}.example/research-boundary`,
    canonical_url: `https://source-${suffix}.example/research-boundary`,
    domain: `source-${suffix}.example`,
    retrieved_at: '2026-08-11T02:01:00Z',
    verification_status: 'content_verified',
    content_hash: `sha256:${suffix.repeat(64)}`,
    start_index: 12,
    end_index: 48,
    support_level: 'content_verified',
  })
  const sources = [source('a'), source('b')]
  const finding = {
    contract_version: 'strategy-research-finding/v1',
    id: `finding_${context.researchRunId}`,
    research_run_id: context.researchRunId,
    claim: '双来源证据边界能降低渠道效率主张的误导风险',
    status: 'verified',
    time_scope: '近六个月',
    confidence: 'high',
    supporting_source_ids: sources.map(item => item.id),
    conflicting_source_ids: [],
    target: { artifact: 'brief', field_path: 'constraints' },
    implication: '将证据要求写入 Brief，避免后续策略把未经核验的效率结论当作事实。',
    proposed_value: ['所有渠道效率主张必须附双来源证据'],
    round: 1,
    content_hash: `sha256:${'f'.repeat(64)}`,
  }
  const evidenceArtifact = {
    id: `artifact_evidence_${context.researchRunId}`,
    organization_id: 'org_local',
    project_id: context.projectId,
    research_run_id: context.researchRunId,
    source_type: 'seed_web_research',
    category: 'general',
    title: '渠道效率证据边界',
    content: '两个独立来源均支持：渠道效率主张应披露可复核证据边界。',
    citations: sources.map(item => item.url),
    sources,
    content_hash: `sha256:${'e'.repeat(64)}`,
    created_at: '2026-08-11T02:01:00Z',
  }
  const iteration = (round: number, final = false) => ({
    id: `iteration_${round}_${context.researchRunId}`,
    research_run_id: context.researchRunId,
    round,
    status: 'completed',
    objective: round === 1 ? '建立证据边界' : '复核引用并形成可采纳建议',
    query: context.query,
    action_summary: round === 1 ? '搜索、读取并交叉核验两个独立来源' : '复查来源定位并完成报告审计',
    source_ids: sources.map(item => item.id),
    artifact_ids: [evidenceArtifact.id],
    finding_ids: [finding.id],
    coverage: { evidence_boundary: true, citation_audit: final },
    open_gaps: final ? [] : ['仍需完成报告引用审计'],
    usage: { input_tokens: 800, output_tokens: 300, total_tokens: 1100 },
    input_hash: `sha256:${String(round).repeat(64)}`,
    output_hash: `sha256:${String(round + 2).repeat(64)}`,
    started_at: `2026-08-11T02:0${round}:00Z`,
    completed_at: `2026-08-11T02:0${round}:20Z`,
  })
  const terminal = stage === 'completed'
  const started = stage !== 'queued'
  const reportId = `artifact_report_${context.researchRunId}`
  const reportArtifact = {
    ...evidenceArtifact,
    id: reportId,
    title: '研究报告：渠道效率证据边界',
    content: '结论：效率主张只有在两个独立来源和可定位摘录同时存在时，才适合作为 Brief 的执行约束。',
    content_hash: `sha256:${'d'.repeat(64)}`,
    created_at: '2026-08-11T02:02:00Z',
  }
  return {
    contract_version: 'strategy-research-run/v2',
    id: context.researchRunId,
    organization_id: 'org_local',
    project_id: context.projectId,
    mode: 'web',
    run_mode: 'deep',
    category: 'general',
    purpose: 'deep_research',
    source_ref: { type: 'strategy_workspace', id: context.workspaceId },
    query: context.query,
    document_ids: [],
    disclosed_fields: ['query'],
    disclosed_chunk_ids: [],
    status: stage === 'queued' ? 'queued' : terminal ? 'completed' : 'cross_checking',
    current_round: stage === 'queued' ? 0 : terminal ? 2 : 1,
    max_rounds: 6,
    time_budget_seconds: 900,
    token_budget: 72000,
    input_snapshot_ref: `strategy_workspace:${context.workspaceId}:v1`,
    input_snapshot_hash: `sha256:${'c'.repeat(64)}`,
    coverage: started ? { evidence_boundary: true, citation_audit: terminal } : {},
    open_gaps: terminal ? [] : started ? ['仍需完成报告引用审计'] : [],
    stop_reason: terminal ? 'coverage_complete' : '',
    heartbeat_at: started ? (terminal ? '2026-08-11T02:02:00Z' : '2026-08-11T02:01:00Z') : null,
    report_artifact_id: terminal ? reportId : null,
    started_at: started ? '2026-08-11T02:00:10Z' : undefined,
    completed_at: terminal ? '2026-08-11T02:02:00Z' : undefined,
    confirmed_by: 'user_local',
    confirmed_at: '2026-08-11T02:00:00Z',
    created_at: '2026-08-11T02:00:00Z',
    updated_at: terminal ? '2026-08-11T02:02:00Z' : started ? '2026-08-11T02:01:00Z' : '2026-08-11T02:00:00Z',
    usage: started ? { input_tokens: terminal ? 1600 : 800, output_tokens: terminal ? 600 : 300, total_tokens: terminal ? 2200 : 1100 } : undefined,
    artifacts: started ? terminal ? [evidenceArtifact, reportArtifact] : [evidenceArtifact] : [],
    iterations: started ? terminal ? [iteration(1), iteration(2, true)] : [iteration(1)] : [],
    findings: started ? [finding] : [],
  }
}

function researchProposalFixture(status: 'proposed' | 'applied', context: {
  projectId: string
  workspaceId: string
  conversationId: string
  researchRunId: string
  briefDraft: Record<string, any>
}) {
  return {
    contract_version: 'strategy-research-adoption-proposal/v1',
    id: `proposal_${context.researchRunId}`,
    organization_id: 'org_local',
    project_id: context.projectId,
    workspace_id: context.workspaceId,
    conversation_id: context.conversationId,
    proposal_kind: 'research',
    target_type: 'brief_draft',
    target_id: context.briefDraft.id,
    target_version: context.briefDraft.version,
    base_content_hash: `sha256:${'b'.repeat(64)}`,
    operations: [{
      op: 'set',
      field_path: 'constraints',
      value: ['所有渠道效率主张必须附双来源证据'],
      source: { type: 'research_finding', id: `finding_${context.researchRunId}` },
      confidence: 'high',
      confirmation: 'proposed',
    }],
    rationale: '两个独立来源均通过正文与摘录核验，应将证据边界写入执行约束。',
    risk: 'medium',
    status,
    finding_ids: [`finding_${context.researchRunId}`],
    source_research_run_id: context.researchRunId,
    stale_reason: '',
    created_by: context.researchRunId,
    applied_by: status === 'applied' ? 'user_local' : undefined,
    applied_at: status === 'applied' ? '2026-08-11T02:03:00Z' : undefined,
    version: status === 'applied' ? 2 : 1,
    created_at: '2026-08-11T02:02:00Z',
    updated_at: status === 'applied' ? '2026-08-11T02:03:00Z' : '2026-08-11T02:02:00Z',
  }
}

function adoptedBriefFixture(briefDraft: Record<string, any>) {
  return {
    ...briefDraft,
    version: Number(briefDraft.version) + 1,
    document: {
      ...briefDraft.document,
      constraints: ['所有渠道效率主张必须附双来源证据'],
    },
    field_states: {
      ...briefDraft.field_states,
      constraints: {
        source: { type: 'research_finding', id: 'finding_e2e' },
        confidence: 'high',
        confirmation: 'confirmed',
        updated_at: '2026-08-11T02:03:00Z',
      },
    },
  }
}

function stressResearchRunFixture(projectId: string, workspaceId: string, researchRunId: string) {
  const run = researchRunFixture('completed', {
    projectId, workspaceId, researchRunId, query: '压力验证：大量研究结论是否仍可读、可滚动并保留目标定位',
  })
  const baseFinding = run.findings[0]
  return {
    ...run,
    findings: Array.from({ length: 30 }, (_, index) => ({
      ...baseFinding,
      id: `finding_stress_${index + 1}`,
      claim: `研究结论 ${index + 1}：${'独立来源支持该结论，但仍需结合项目数据确认适用范围。'.repeat(2)}`,
      implication: `对应 Brief 或策略决策影响 ${index + 1}，不得在未经用户采纳时自动写入。`,
      status: index % 7 === 0 ? 'conflicting' : 'verified',
      conflicting_source_ids: index % 7 === 0 ? [`source_b_${researchRunId}`] : [],
      round: index % 6 + 1,
      content_hash: `sha256:${(index + 1).toString(16).padStart(64, '0')}`,
    })),
  }
}

function stressDocumentFixture(projectId: string, index: number) {
  return {
    contract_version: 'platform-document-parse/v2', id: `document_stress_${index}`, project_id: projectId,
    title: `项目资料 ${String(index).padStart(2, '0')}`, filename: `project-material-${String(index).padStart(2, '0')}.txt`,
    source_type: 'upload', mime_type: 'text/plain', size_bytes: 2048 + index,
    content_sha256: 'a'.repeat(64), text_sha256: 'b'.repeat(64), chunk_count: 3,
    status: 'ready', parse_strategy: 'text_native', parse_phase: 'ready', parse_progress: 100,
    progress_kind: 'milestone', processed_pages: null, total_pages: null,
    quality_score: 0.98, quality_tier: 'high', fallback_reason: '', preview_status: 'ready',
    page_quality_summary: undefined, heartbeat_at: null, vision_fallback_status: 'not_requested',
    vision_selected_pages: [], vision_completed_pages: [], parser_code: 'native', parser_version: 'v1',
    parsed_at: '2026-08-11T03:00:00Z', created_at: '2026-08-11T03:00:00Z', updated_at: '2026-08-11T03:00:00Z',
  }
}

function stressActivitySnapshot(projectId: string, count: number) {
  return {
    contract_version: 'strategy-task-activity-snapshot/v1',
    snapshot_id: `sha256:${'9'.repeat(64)}`,
    captured_at: '2026-08-11T03:30:00Z',
    items: Array.from({ length: count }, (_, index) => ({
      contract_version: 'strategy-task-activity/v1', id: `activity_stress_${index + 1}`,
      kind: index % 2 === 0 ? 'deep_research' : 'strategy_generation', status: 'running',
      phase: index % 2 === 0 ? 'cross_checking' : 'drafting',
      round: index % 2 === 0 ? { current: index % 6 + 1, max: 6 } : null,
      progress: { kind: 'milestone', value: 10 + index * 8, message: `后台任务 ${index + 1} 正在处理` },
      summary: `后台任务 ${index + 1} 保留心跳、阶段和恢复入口。`, confirmed_conclusions: [],
      source_scope: { project_id: projectId },
      resource_ref: { type: index % 2 === 0 ? 'knowledge_research_run' : 'strategy_draft', id: `resource_stress_${index + 1}` },
      execution_ref: { type: 'platform_job', id: `job_stress_${index + 1}`, version: 2 },
      actions: ['open', 'cancel'], cancel_requested: false,
      heartbeat_at: '2026-08-11T03:30:00Z', updated_at: '2026-08-11T03:30:00Z',
    })),
  }
}

test('a failed center request gives a retry path without freezing the shell', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.goto(`${projectRoot}/briefs`)
  await waitForProject(page)
  consoleErrors.length = 0

  let briefServiceAvailable = false
  await page.route(`**/api/strategy/v1/projects/${projectId}/briefs`, async route => {
    if (briefServiceAvailable) {
      await route.continue()
      return
    }
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: { message: 'E2E injected temporary outage' } }),
    })
  })

  await page.reload()
  await expect(page.getByText('需求中心暂时不可用')).toBeVisible()
  await expect(page.getByText('E2E injected temporary outage')).toBeVisible()
  briefServiceAvailable = true
  consoleErrors.length = 0

  await page.getByRole('button', { name: '重新加载' }).click()
  await expect(page.getByText('需求中心暂时不可用')).toHaveCount(0)
  await expect(page.getByText(/BRIEF CENTER|没有“Brief 列表”Brief/)).toBeVisible()
  await expect(page.getByRole('button', { name: '研究洞察：联网研究与证据' })).toBeEnabled()

  expect(consoleErrors).toEqual([])
})

test('Strategy centers remain usable from desktop through mobile breakpoints', async ({ page }) => {
  const consoleErrors = captureConsoleErrors(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1280, height: 800 },
    { width: 1024, height: 768 },
    { width: 768, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto(`${projectRoot}/briefs`)
    await waitForProject(page)
    if (viewport.width === 1440) consoleErrors.length = 0
    await expect(page.getByRole('heading', { name: '需求中心', exact: true }).first()).toBeVisible()

    const layout = await page.evaluate(() => {
      const copy = document.querySelector('.page-header > div:first-child')?.getBoundingClientRect()
      const context = document.querySelector('.page-context-label')?.getBoundingClientRect()
      const overlaps = Boolean(copy && context && !(
        copy.right <= context.left || context.right <= copy.left ||
        copy.bottom <= context.top || context.bottom <= copy.top
      ))
      return {
        innerWidth: window.innerWidth,
        scrollWidth: document.documentElement.scrollWidth,
        headerOverlaps: overlaps,
      }
    })
    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.innerWidth)
    expect(layout.headerOverlaps).toBe(false)

    for (const center of centers) {
      await expect(page.getByRole('button', { name: center.accessibleName })).toBeVisible()
    }
  }

  await expect(page.locator('.module-page > .tabs')).toHaveCSS('overflow-x', 'auto')
  expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(true)
  expect(consoleErrors).toEqual([])
})
