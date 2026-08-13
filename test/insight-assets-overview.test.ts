import assert from 'node:assert/strict'
import test from 'node:test'
import {
  assetStatusLabel,
  buildOverviewAlerts,
  externalNote,
  overviewTally,
} from '../src/components/insight/assets/OverviewView.tsx'
import type { ApiExternalAsset } from '../src/data/api.ts'

/**
 * 素材总览这一屏的规矩是「有事才出声」：没事的时候一个字都不该出现。
 * 这里守的就是这条——一旦哪天有人给告警块加了个「全都正常」的空态，
 * 第一个 case 就会挂。
 */
test('四件事都没有时一行都不出', () => {
  assert.deepEqual(buildOverviewAlerts({ unmatched: 0, failures: 0, needsReview: 0, notImported: 0 }), [])
})

test('只有非零的那件事会出声', () => {
  const alerts = buildOverviewAlerts({ unmatched: 0, failures: 2, needsReview: 0, notImported: 0 })
  assert.equal(alerts.length, 1)
  assert.equal(alerts[0].key, 'failed')
  assert.equal(alerts[0].action, '去重跑')
  assert.equal(alerts[0].target, '变量')
  assert.match(alerts[0].text, /2 条提取失败/)
})

/**
 * 对不上号是唯一一个「不处理后面全错」的问题：花费算不到任何素材头上，
 * 后面每一个数字都建立在一份不完整的账上。它必须排第一，而且必须是红的。
 */
test('对不上号永远排第一而且是红的', () => {
  const alerts = buildOverviewAlerts({ unmatched: 1, failures: 3, needsReview: 2, notImported: 4 })
  assert.equal(alerts.length, 4)
  assert.equal(alerts[0].key, 'unmatched')
  assert.equal(alerts[0].tone, 'urgent')
  assert.deepEqual(alerts.slice(1).map(alert => alert.tone), ['warn', 'warn', 'warn'])
})

test('创意里没进来的那条落到导入，不是落到某一屏', () => {
  const [alert] = buildOverviewAlerts({ unmatched: 0, failures: 0, needsReview: 0, notImported: 5 })
  assert.equal(alert.target, 'import')
  assert.match(alert.text, /创意里还有 5 条/)
})

/**
 * 清单上每条右边挂的是「这条能不能用」，不是编号。
 * 已提出变量的两种状态显示同一个词——对看的人来说它们是同一件事：能用了。
 */
test('两种已提变量的状态说的是同一句话', () => {
  assert.deepEqual(assetStatusLabel('confirmed', false), { text: '已可解释', tone: 'ready' })
  assert.deepEqual(assetStatusLabel('pending_confirmation', false), { text: '已可解释', tone: 'ready' })
})

test('等着的几种各说各的，都不算好也不算坏', () => {
  assert.deepEqual(assetStatusLabel('awaiting_data', false), { text: '等投放数据', tone: 'waiting' })
  assert.deepEqual(assetStatusLabel('awaiting_match', false), { text: '等对上号', tone: 'waiting' })
  assert.deepEqual(assetStatusLabel('analysable', false), { text: '等提变量', tone: 'waiting' })
})

/**
 * 失败不在状态机里——跑挂了的素材原地停在「可分析」。要是照状态显示，
 * 一条一个变量都没有的素材会挂着「等提变量」，看起来像还没轮到它。
 */
test('跑挂了的素材压过它自己的状态', () => {
  assert.deepEqual(assetStatusLabel('analysable', true), { text: '提取失败', tone: 'bad' })
})

test('状态认不出来时不留白', () => {
  assert.deepEqual(assetStatusLabel('brand-new' as never, false), { text: '状态未知', tone: 'waiting' })
})

/**
 * 横幅那一行要守恒：第一个数是总数，后面每一项都是它的一部分。
 * 不守恒的话，人会拿它和下面两栏去对，对不上就以为哪里算错了。
 */
test('横幅按顿号排开，总数在最前面', () => {
  assert.equal(
    overviewTally(6, 5, ['1 条等投放数据']),
    '6 条素材 · 5 条已能解释 · 1 条等投放数据')
})

test('没有别的情况时横幅只有两段', () => {
  assert.equal(overviewTally(6, 6, []), '6 条素材 · 6 条已能解释')
})

const evidence = (patch: Partial<ApiExternalAsset>): ApiExternalAsset => ({
  id: 'external_1',
  project_id: 'project_1',
  title: '竞品B·信息流首帧',
  purpose: 'benchmark',
  asset_type: 'preroll_ad',
  features: {},
  retention_until: '2026-09-30',
  original_purged: false,
  created_at: '2026-08-01T00:00:00Z',
  ...patch,
} as ApiExternalAsset)

/**
 * 一条既没有文件也没标变量的登记，引用它等于引用一个名字。
 * 它必须在自己那一行上标红——以前是在页顶报个总数，人还得回头数是哪几条。
 */
test('什么都没有的证据标红', () => {
  assert.deepEqual(externalNote(evidence({})), { text: '只有标题', tone: 'bad' })
})

test('标了变量就算数，说清标了几条', () => {
  assert.deepEqual(
    externalNote(evidence({ features: { 开头形式: '人脸', 时长: '15s' } })),
    { text: '2 条变量', tone: 'ready' })
})

/**
 * 留存期管的只是原件。一条没有文件的登记根本没有东西会被清掉，
 * 对它说「留到 X」是在催人做一件做不到的事。
 */
test('只有真存了原件才提到期日', () => {
  const note = externalNote(evidence({ storage_key: 'external/1.mp4' }))
  assert.equal(note.tone, 'ready')
  assert.match(note.text, /^原件留到 /)
})

test('原件删了就说删了，不再挂一个过去的日期', () => {
  assert.deepEqual(
    externalNote(evidence({ storage_key: 'external/1.mp4', original_purged: true })),
    { text: '原件已删', tone: 'waiting' })
})
