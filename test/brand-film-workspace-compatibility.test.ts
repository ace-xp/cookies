import assert from 'node:assert/strict'
import test from 'node:test'
import { stringListOrEmpty } from '../src/features/brand-film/brand-film-value.ts'

test('brand film workspace tolerates null list fields from legacy revisions', () => {
  assert.deepEqual(stringListOrEmpty(null), [])
  assert.deepEqual(stringListOrEmpty(['产品材质', null, '镜头转场', 3]), ['产品材质', '镜头转场'])
  assert.equal(stringListOrEmpty(null).join('、') || '镜头动作与产品材质', '镜头动作与产品材质')
})
