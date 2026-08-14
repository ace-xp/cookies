import assert from 'node:assert/strict'
import test from 'node:test'

import { findAuthoritativeVideo, requireAuthoritativeVideo, restorePersistedVideo } from '../src/features/short-drama-preroll-v2/sourceAuthority'

const videos = [
  { id: 'asset_current', version: 2, kind: 'video' as const },
  { id: 'asset_other', version: 1, kind: 'video' as const },
]

test('short-drama restore accepts only the exact project asset version bound to the task', () => {
  assert.equal(findAuthoritativeVideo(videos, { asset_id: 'asset_current', version: 2 }), videos[0])
  assert.equal(findAuthoritativeVideo(videos, { asset_id: 'asset_current', version: 1 }), null)
  assert.equal(findAuthoritativeVideo(videos, { asset_id: 'asset_missing', version: 1 }), null)
})

test('short-drama upload never fabricates a browser-local asset when persistence is not observable', () => {
  assert.throws(
    () => requireAuthoritativeVideo(videos, { asset_id: 'asset_missing', version: 1 }),
    /后端尚未确认该视频素材/,
  )
})

test('short-drama restore can materialize a server-confirmed source omitted from a bounded asset page', () => {
  const restored = restorePersistedVideo(
    { asset_id: 'asset_older_source', version: 3 },
    {
      projectId: 'project_1',
      contentUrl: '/platform/v1/projects/project_1/assets/asset_older_source/versions/3/content',
      durationSeconds: 202.4,
      width: 1080,
      height: 1920,
      createdAt: '2026-08-10T00:00:00Z',
    },
  )

  assert.deepEqual(restored, {
    id: 'asset_older_source',
    projectId: 'project_1',
    version: 3,
    kind: 'video',
    sourceType: 'imported',
    mimeType: 'video/mp4',
    sizeBytes: 0,
    durationSeconds: 202.4,
    width: 1080,
    height: 1920,
    createdAt: '2026-08-10T00:00:00Z',
    contentUrl: '/platform/v1/projects/project_1/assets/asset_older_source/versions/3/content',
    useAllowed: true,
  })
})
