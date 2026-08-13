import assert from 'node:assert/strict'
import test from 'node:test'
import { api } from '../src/data/api.ts'

/**
 * 缩略图是 <img src> 直接打过去的，不走 fetch——所以它必须是一个能拼出来的地址，
 * 不是一个要先 await 的 Promise。一屏几十张图各发一次 JSON 请求再取地址，
 * 清单会卡住。
 */
test('封面地址拼得出来，而且带项目和素材两段', () => {
  const url = api.insightAssetPosterUrl('k_project_1', 'insightasset_7')
  assert.match(url, /k_project_1/)
  assert.match(url, /insightasset_7/)
  assert.match(url, /\/poster$/)
})
