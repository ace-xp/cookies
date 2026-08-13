import assert from 'node:assert/strict'
import test from 'node:test'
import { api } from '../src/data/api.ts'

const actor = {
  principal: { id: 'user_admin' },
  organization_id: 'org_local',
  scopes: ['insights.read', 'insights.write', 'insights.confirm'],
}

const membershipPage = {
  items: [{
    organization: { id: 'org_local', name: 'Local Organization', status: 'active' },
    membership: {
      organization_id: 'org_local',
      user_id: 'user_admin',
      role: 'owner',
      status: 'active',
      updated_at: '2026-08-12T00:00:00Z',
    },
  }],
}

test('authentication uses the Go platform routes and supports an empty logout response', async () => {
  const originalFetch = globalThis.fetch
  const calls: Array<{ url: string; init?: RequestInit }> = []
  globalThis.fetch = async (input, init) => {
    const url = String(input)
    calls.push({ url, init })
    if (url.endsWith('/auth/login')) return new Response(JSON.stringify({ actor }), { status: 200 })
    if (url.endsWith('/context')) return new Response(JSON.stringify({ actor }), { status: 200 })
    if (url.endsWith('/organizations')) return new Response(JSON.stringify(membershipPage), { status: 200 })
    if (url.endsWith('/auth/logout')) return new Response(null, { status: 204 })
    throw new Error(`unexpected request ${url}`)
  }

  try {
    // 会话里必须带着 scopes 和组织角色：界面上「你能不能改阈值」「你能不能确认」
    // 全靠这两样判断。少了 scopes，hasScope 恒为 false，判定阈值会对所有人锁死。
    assert.deepEqual(await api.login({ username: 'Admin', password: '123456' }), {
      authenticated: true,
      user: { id: 'user_admin', email: '', displayName: 'Admin' },
      scopes: ['insights.read', 'insights.write', 'insights.confirm'],
      organization: { id: 'org_local', name: 'Local Organization', status: 'active' },
      membership: { role: 'owner', status: 'active', updatedAt: '2026-08-12T00:00:00Z' },
    })
    assert.equal(await api.getSession().then((session) => session.authenticated), true)
    assert.deepEqual(await api.logout(), { authenticated: false })
  } finally {
    globalThis.fetch = originalFetch
  }

  const urls = calls.map((call) => call.url)
  assert.equal(urls[0], '/platform/v1/auth/login')
  assert.equal(calls[0].init?.method, 'POST')
  assert.deepEqual(JSON.parse(String(calls[0].init?.body)), { username: 'Admin', password: '123456' })
  assert.ok(urls.includes('/platform/v1/organizations'))
  assert.ok(urls.includes('/platform/v1/context'))
  assert.equal(urls.at(-1), '/platform/v1/auth/logout')
})

test('a failed organization lookup still yields a usable session', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (input) => {
    const url = String(input)
    if (url.endsWith('/context')) return new Response(JSON.stringify({ actor }), { status: 200 })
    if (url.endsWith('/organizations')) {
      return new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'boom' } }), { status: 500 })
    }
    throw new Error(`unexpected request ${url}`)
  }

  try {
    // 组织读不到不该把人挡在门外：权限判断只依赖 scopes，组织名只影响顶栏那一行显示。
    const session = await api.getSession()
    assert.equal(session.authenticated, true)
    assert.deepEqual(session.scopes, ['insights.read', 'insights.write', 'insights.confirm'])
    assert.equal(session.organization, undefined)
    assert.equal(session.membership, undefined)
  } finally {
    globalThis.fetch = originalFetch
  }
})
