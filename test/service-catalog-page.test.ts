import { test } from 'node:test'
import assert from 'node:assert/strict'
import { catalogLoadState, readOnlyHint } from '../src/components/settings/serviceCatalogState'

// A failed load must not render as "everything is unconfigured" — that
// misleads the operator into re-entering credentials that are already fine.
test('后端不可用时是读取失败，不是全部未配置', () => {
  assert.equal(catalogLoadState({ loading: false, error: '网络错误', services: [] }), 'load-failed')
})

test('读取中就是读取中', () => {
  assert.equal(catalogLoadState({ loading: true, error: '', services: [] }), 'loading')
})

test('读到空目录才算真的空', () => {
  assert.equal(catalogLoadState({ loading: false, error: '', services: [] }), 'empty')
})

test('只读服务给出要改的环境变量与是否需重启', () => {
  const hint = readOnlyHint({ env_keys: ['COOKIES_TOS_ACCESS_KEY'], restart_required: true })
  assert.ok(hint.includes('COOKIES_TOS_ACCESS_KEY'))
  assert.ok(hint.includes('重启'))
})

test('无配置项的只读服务不编造环境变量', () => {
  const hint = readOnlyHint({ env_keys: [], restart_required: false })
  assert.ok(!hint.includes('COOKIES_'))
})

// 密云的实际配置在按项目的「米云连接」里，不在环境变量里。列环境变量
// 会把人指去改一份根本没在生效的值。
test('别处配置的服务只说去哪儿改，不列环境变量', () => {
  const hint = readOnlyHint({
    env_keys: ['COOKIES_MIYUN_ENDPOINT'],
    restart_required: false,
    managed_note: '密云按项目单独连接，请在本页的「米云连接」里填写地址与 Cookie。',
  })
  assert.equal(hint, '密云按项目单独连接，请在本页的「米云连接」里填写地址与 Cookie。')
  assert.ok(!hint.includes('COOKIES_MIYUN_ENDPOINT'))
})
