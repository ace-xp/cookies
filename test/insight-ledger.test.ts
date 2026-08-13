import assert from 'node:assert/strict'
import test from 'node:test'
import { ledgerSourceLabel } from '../src/components/insight/assets/LedgerView.tsx'

/**
 * 台账清单上每一条都要说清出处。「external」和「miyun」在这里必须是两个词——
 * 界面上的「外部素材」指的是竞品参照证据，那些永远不能投；米云的能投。
 * 两者共用一个标签的时候，看的人会以为米云素材也不能投。
 */
test('四种来源各有各的中文名', () => {
  assert.equal(ledgerSourceLabel('creative'), '创意产出')
  assert.equal(ledgerSourceLabel('upload'), '手工上传')
  assert.equal(ledgerSourceLabel('external'), '外部导入')
  assert.equal(ledgerSourceLabel('miyun'), '米云采集')
})

test('来源读不出来时不编一个名字', () => {
  assert.equal(ledgerSourceLabel('brand-new' as never), '来源未知')
})
