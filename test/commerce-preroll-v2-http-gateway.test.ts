import assert from 'node:assert/strict'
import test from 'node:test'
import { HttpCommercePrerollGateway } from '../src/features/commerce-preroll-v2/httpGateway.ts'

const projectId = 'project_demo'
const taskId = 'task_commerce'

function detail(revision: number, overrides: Record<string, unknown> = {}) {
  return {
    task: { id: taskId, display_name: '电商前贴', version: 1, status: 'in_progress', updated_at: '2026-08-12T00:00:00Z' },
    video_draft: {
      revision,
      commerce_preroll_v2: {
        revision,
        active_stage: 'frame_ready',
        source_video: { project_id: projectId, asset_version: { asset_id: 'asset_source', version: 1 } },
        source_metadata: { WidthPixels: 720, HeightPixels: 1280, DurationMS: 41000 },
        analysis: {
          status: 'ready', progress: 100,
          content: {
            product: { name: '商品', category: '护肤', description: '描述', selling_points: ['卖点'], appearance_guardrails: ['瓶型'], logo_guardrails: ['Logo'] },
            visual_style: '暖色', subtitle_summary: '无', voice_summary: '无', audio_mood: '平稳', opening_shot: '正面', evidence: [], risks: [],
          },
        },
        ...overrides,
      },
    },
  }
}

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

test('commerce V2 refreshes the authoritative revision and retries one conflicting command', async () => {
  const originalFetch = globalThis.fetch
  const postRevisions: number[] = []
  let detailReads = 0
  globalThis.fetch = async (input, init = {}) => {
    const url = String(input)
    if (url.includes('/preview')) return json({ url: '/preview/source.mp4' })
    if (init.method === 'POST' && url.endsWith(':select-first-frame')) {
      const body = JSON.parse(String(init.body)) as { expected_revision: number }
      postRevisions.push(body.expected_revision)
      if (postRevisions.length === 1) return json({ error: { message: 'The Creative draft changed. Refresh the task and try again.' } }, 412)
      return json(detail(6, { first_frame_batch: { id: 'batch_1', status: 'ready', selected_id: 'frame_1', candidates: [] }, generation_spec: { draft_revision: 5, spec_hash: 'sha256:spec' } }))
    }
    detailReads += 1
    return json(detail(detailReads === 1 ? 4 : 5))
  }
  try {
    const gateway = new HttpCommercePrerollGateway(projectId)
    await gateway.openTask(taskId)
    await gateway.selectFirstFrame?.({ id: 'frame_1', batchId: 'batch_1', imageUrl: '/frame.png', label: 'A', title: 'A', description: 'A' })
    assert.deepEqual(postRevisions, [4, 5])
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('commerce V2 restores durable first frames, selection, and generated video after refresh', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = async (input) => {
    const url = String(input)
    if (url.includes('/preview')) return json({ url: `/durable/${url.includes('asset_output') ? 'output.mp4' : url.includes('asset_frame') ? 'frame.png' : 'source.mp4'}` })
    return json(detail(9, {
      active_stage: 'video_ready',
      prompt_draft: {
        revision: 4, prompt_summary: '摘要', compiled_prompt: 'Prompt', creative_prompt: 'Prompt', locked_constraints: ['保真'],
        beats: [
          { id: 'hook', label: '钩子', start_ms: 0, end_ms: 2000, detail: 'A' },
          { id: 'change', label: '变化', start_ms: 2000, end_ms: 6000, detail: 'B' },
          { id: 'lockup', label: '定格', start_ms: 6000, end_ms: 8000, detail: 'C' },
        ],
      },
      first_frame_batch: {
        id: 'batch_1', status: 'ready', selected_id: 'frame_1',
        candidates: [{ id: 'frame_1', provider_job_id: 'job_image', status: 'ready', asset: { project_id: projectId, asset_version: { asset_id: 'asset_frame', version: 2 } }, variant_key: 'native', title: '原片同调', description: '描述' }],
      },
      generation_spec: { draft_revision: 8, spec_hash: 'sha256:spec' },
      output_asset: { project_id: projectId, asset_version: { asset_id: 'asset_output', version: 3 } },
    }))
  }
  try {
    const state = await new HttpCommercePrerollGateway(projectId).openTask(taskId)
    assert.equal(state.firstFramesStatus, 'ready')
    assert.equal(state.firstFrames[0]?.assetId, 'asset_frame')
    assert.equal(state.selectedFirstFrameId, 'frame_1')
    assert.equal(state.videoStatus, 'ready')
    assert.equal(state.output?.assetId, 'asset_output')
    assert.match(state.output?.videoUrl ?? '', /output\.mp4$/)
  } finally {
    globalThis.fetch = originalFetch
  }
})
