import { test } from 'node:test'
import assert from 'node:assert/strict'
import { serviceSubmitBody, summarizeServiceStatus } from '../src/data/serviceCatalog'

test('空字符串的密钥字段不提交，保持沿用已存凭据', () => {
  const body = serviceSubmitBody(
    [
      { name: 'base_url', label: '服务地址', kind: 'text', required: true },
      { name: 'api_key', label: 'API Key', kind: 'secret', required: false },
    ],
    { base_url: 'https://ark.example.com', api_key: '' },
    3,
  )
  assert.equal(body.values.base_url, 'https://ark.example.com')
  assert.equal('api_key' in body.values, false)
  assert.equal(body.expected_version, 3)
})

test('填了的密钥字段照常提交', () => {
  const body = serviceSubmitBody(
    [{ name: 'api_key', label: 'API Key', kind: 'secret', required: false }],
    { api_key: 'sk-new' },
    undefined,
  )
  assert.equal(body.values.api_key, 'sk-new')
  assert.equal(body.expected_version, undefined)
})

test('状态汇总区分四态', () => {
  assert.equal(summarizeServiceStatus({ configured: false, last_probe: { outcome: 'ok', message: '' } }), '未配置')
  assert.equal(summarizeServiceStatus({ configured: true, last_probe: { outcome: 'ok', message: '' } }), '可用')
  assert.equal(
    summarizeServiceStatus({ configured: true, last_probe: { outcome: 'auth_failed', message: '' } }),
    '已配置但连不通',
  )
  assert.equal(
    summarizeServiceStatus({ configured: true, last_probe: { outcome: 'unreachable', message: '' } }),
    '已配置但连不通',
  )
})
