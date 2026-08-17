import assert from 'node:assert/strict'
import test from 'node:test'
import { api } from '../src/data/api.ts'

/**
 * 权限不够时，界面上必须是一句中文，而且要说清楚「去哪儿看自己有哪几档、找谁改」。
 *
 * 后端在两个地方拒绝：HTTP 层回 SCOPE_REQUIRED，服务层回 `insights.confirm scope is
 * required`。两句都是英文，直接透出去等于把人扔在原地——他既不知道缺哪一档，
 * 也不知道这事该找谁。翻译只做一次，放在 API 层，所有确认类入口一起覆盖。
 */
async function failWith(status: number, payload: unknown): Promise<string> {
  const originalFetch = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify(payload), { status })
  try {
    await api.saveThresholds('project_demo', { values: {}, reason: '试一下' })
    return '(没有报错)'
  } catch (cause) {
    return cause instanceof Error ? cause.message : String(cause)
  } finally {
    globalThis.fetch = originalFetch
  }
}

test('HTTP 层的 SCOPE_REQUIRED 翻成中文并指路', async () => {
  const message = await failWith(403, {
    error: { code: 'SCOPE_REQUIRED', message: 'The required permission scope is missing.' },
  })
  assert.match(message, /没有做这一步所需要的权限/)
  assert.match(message, /设置 · 确认权限/)
  assert.match(message, /组织管理员/)
  assert.doesNotMatch(message, /scope/i)
})

test('服务层的「X scope is required」认得出缺的是哪一档', async () => {
  const confirm = await failWith(403, {
    error: { code: 'FORBIDDEN', message: 'insights.confirm scope is required' },
  })
  assert.match(confirm, /「确认」这一档权限/)
  assert.match(confirm, /设置 · 确认权限/)

  const write = await failWith(403, {
    error: { code: 'FORBIDDEN', message: 'insights.write scope is required' },
  })
  assert.match(write, /「编辑」这一档权限/)
})

test('其它错误照原样透出，不被权限文案盖掉', async () => {
  const message = await failWith(409, {
    error: { code: 'VERSION_CONFLICT', message: '这一版阈值已经被别人改过了。' },
  })
  assert.equal(message, '这一版阈值已经被别人改过了。')
})
