import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mergeModelOptions, serviceSubmitBody, summarizeServiceStatus } from '../src/data/serviceCatalog'

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

test('模型下拉：上游读到的排前面，目录候选补在后面', () => {
  const merged = mergeModelOptions(
    ['doubao-seedance-2-0-260128', 'doubao-seedance-9-9-new'],
    ['doubao-seedance-2-0-fast-260128', 'doubao-seedance-2-0-260128'],
  )
  assert.deepEqual(merged, [
    'doubao-seedance-2-0-260128',
    'doubao-seedance-9-9-new',
    'doubao-seedance-2-0-fast-260128',
  ])
})

test('模型下拉：没读过上游时只剩目录候选，两边都空就是空', () => {
  assert.deepEqual(mergeModelOptions(undefined, ['doubao-seedream-5-0-pro-260628']), ['doubao-seedream-5-0-pro-260628'])
  assert.deepEqual(mergeModelOptions(undefined, undefined), [])
  assert.deepEqual(mergeModelOptions([' '], ['']), [])
})
