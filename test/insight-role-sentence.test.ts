import assert from 'node:assert/strict'
import test from 'node:test'
import { insightScopeSummary, roleSentence } from '../src/data/scopes.ts'

/**
 * 「你现在是当前角色」这种句子出现过一次：兜底把占位词当成了角色名。
 * 读的人会以为页面坏了，而真正管事的那一项（有哪几档权限）反而没说。
 */
test('角色读得到时，角色和权限一起说', () => {
  assert.equal(
    roleSentence('owner', ['insights.read', 'insights.write', 'insights.confirm']),
    '你现在是组织所有者，有读取 + 编辑 + 确认',
  )
  assert.equal(
    roleSentence('member', ['insights.read', 'insights.write']),
    '你现在是成员，有读取 + 编辑',
  )
})

test('角色读不到就直说读不到，绝不凑一个角色名出来', () => {
  const sentence = roleSentence(undefined, ['insights.read'])
  assert.match(sentence, /没读到你的组织角色/)
  assert.match(sentence, /读取/)
  assert.doesNotMatch(sentence, /你现在是/)
})

test('只列洞察这三档，一档都没有时也要说人话', () => {
  assert.equal(insightScopeSummary(['insights.read', 'delivery.execute']), '读取')
  assert.equal(insightScopeSummary([]), '连读取都没有（这一页多半也打不开）')
  assert.equal(insightScopeSummary(undefined), '连读取都没有（这一页多半也打不开）')
})
