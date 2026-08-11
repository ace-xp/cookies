import { expect, test } from '@playwright/test'

const projectId = 'project_guerlain_abeille_royale_acceptance'

test('isolated Guerlain Project starts a persistent Strategy workspace', async ({ page }) => {
  await page.goto(`/projects/${projectId}/strategy/workspaces`)

  await expect(page.getByRole('heading', { name: '策略工作区', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '娇兰第三代黄金复原蜜品牌广告验收' })).toBeVisible()
  await expect(page.getByText('尚未连接到服务端')).toHaveCount(0)

  const createWorkspace = page.getByRole('button', { name: '创建主策略工作区' })
  const startWorkspace = page.getByRole('button', { name: '开始策略梳理' })
  const currentChain = page.getByText('当前工作链', { exact: true })
  await expect(createWorkspace.or(startWorkspace).or(currentChain)).toBeVisible()
  if (await createWorkspace.isVisible().catch(() => false)) {
    await createWorkspace.click()
    await expect(startWorkspace).toBeVisible()
  }
  await expect(startWorkspace.or(currentChain)).toBeVisible()
  if (await startWorkspace.isVisible().catch(() => false)) {
    await startWorkspace.click()
  }

  const stageNavigation = page.getByRole('navigation', { name: '策略工作阶段' })
  await expect(stageNavigation).toBeVisible()
  await expect(stageNavigation.getByRole('button', { name: /^理解需求：/ })).toHaveAttribute('aria-current', 'step')
  await expect(page).toHaveURL(/\/strategy\/workspaces\/[^/]+\/intake$/)
  await expect(page.getByRole('heading', { name: '先说清楚要解决什么。' })).toHaveCount(0)
  await expect(page.getByLabel('需求收敛状态')).toContainText('核心信息')
  await expect(page.locator('#kanon-strategy-message')).toBeEnabled()
  await expect(page.getByRole('button', { name: '发送需求消息' })).toBeDisabled()
})
