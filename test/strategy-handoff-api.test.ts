import assert from 'node:assert/strict'
import test from 'node:test'
import { strategyApi } from '../src/features/strategy/api'
import type { CreativeTaskPlan } from '../src/features/strategy/types'

function handoffPlan(taskOverlayRef?: { overlay_id: string; content_hash: string }): CreativeTaskPlan {
  return {
    contract_version: 'strategy-creative-task-plan/v2',
    id: 'plan_brand_1',
    project_id: 'project_1',
    brief_id: 'brief_1',
    brief_version: 2,
    package_ref: {
      package_id: 'package_1',
      package_version: 3,
      package_content_hash: 'sha256:package',
      handoff_contract_version: 'strategy-creative-handoff/v1',
      handoff_content_hash: 'sha256:handoff',
    },
    handoff_ref: { contract_version: 'strategy-creative-handoff/v1', content_hash: 'sha256:handoff' },
    selected_route_id: 'route_brand_video',
    status: 'generated',
    business_code: 'brand_video',
    selection_source: 'recommended',
    answers: {},
    completeness: { ready: true, blockers: [], warnings: [] },
    current_revision: 1,
    current_strategy_version: 1,
    version: 1,
    current_strategy: {
      plan_id: 'plan_brand_1',
      version: 1,
      plan_revision: 1,
      contract_version: 'creative-task-strategy/v2',
      document: {} as never,
      content_hash: 'sha256:strategy',
      created_at: '2026-08-05T00:00:00Z',
      ...(taskOverlayRef ? { task_overlay_ref: taskOverlayRef } : {}),
    },
  }
}

test('Strategy handoff sends only frozen refs when no task overlay was requested', async () => {
  const originalFetch = globalThis.fetch
  const calls: Array<{ url: string; init: RequestInit }> = []
  globalThis.fetch = async (input, init = {}) => {
    calls.push({ url: String(input), init })
    return new Response(JSON.stringify({
      contract_version: 'creative-intake/v3',
      id: 'intake_1',
      status: 'ready',
      selected_route_id: 'route_brand_video',
      input_identity_hash: 'sha256:intake',
    }), { status: 201, headers: { 'Content-Type': 'application/json' } })
  }
  try {
    await strategyApi.handoffCreativeTaskStrategy('project_1', handoffPlan(), 'handoff-key')
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.equal(calls[0].url, '/api/creative/v1/projects/project_1/creative-intakes')
  assert.deepEqual(JSON.parse(String(calls[0].init.body)), {
    contract_version: 'creative-intake-create/v3',
    source: 'strategy_package',
    strategy_package_ref: handoffPlan().package_ref,
    selected_route_id: 'route_brand_video',
  })
  assert.equal(new Headers(calls[0].init.headers).get('Idempotency-Key'), 'handoff-key')
})

test('Approved package and frozen route can hand off before task strategy generation', async () => {
  const originalFetch = globalThis.fetch
  let requestBody: Record<string, unknown> = {}
  globalThis.fetch = async (_input, init = {}) => {
    requestBody = JSON.parse(String(init.body)) as Record<string, unknown>
    return new Response(JSON.stringify({ id: 'intake_base', status: 'ready' }), {
      status: 201,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  const plan = handoffPlan()
  delete plan.current_strategy
  plan.status = 'ready'
  plan.current_strategy_version = 0
  try {
    await strategyApi.handoffCreativeTaskStrategy('project_1', plan)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(requestBody, {
    contract_version: 'creative-intake-create/v3',
    source: 'strategy_package',
    strategy_package_ref: plan.package_ref,
    selected_route_id: 'route_brand_video',
  })
})

test('Strategy handoff includes an overlay only when the user created one', async () => {
  const originalFetch = globalThis.fetch
  let requestBody: Record<string, unknown> = {}
  globalThis.fetch = async (_input, init = {}) => {
    requestBody = JSON.parse(String(init.body)) as Record<string, unknown>
    return new Response(JSON.stringify({ id: 'intake_2', status: 'ready' }), {
      status: 201,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  const overlay = { overlay_id: 'overlay_1', content_hash: 'sha256:overlay' }
  try {
    await strategyApi.handoffCreativeTaskStrategy('project_1', handoffPlan(overlay))
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(requestBody.task_overlay_ref, overlay)
})

test('Strategy brand acceptance binds its idempotency key and body to the frozen intake identity', async () => {
  const originalFetch = globalThis.fetch
  const calls: Array<{ url: string; init: RequestInit }> = []
  const intake = {
    contract_version: 'creative-intake/v3' as const,
    id: 'intake_1',
    status: 'ready' as const,
    selected_route_id: 'route_brand_video',
    input_identity_hash: `sha256:${'b'.repeat(64)}`,
  }
  globalThis.fetch = async (input, init = {}) => {
    calls.push({ url: String(input), init })
    return new Response(JSON.stringify({ mode: 'direction_ready', next_action: 'generate_directions' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  try {
    await strategyApi.prepareStrategyBrandWorkflow('project 1', intake)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.equal(calls[0].url, '/api/creative/v1/projects/project%201/creative-intakes/intake_1/brand-workflow:prepare')
  assert.equal(calls[0].init.method, 'POST')
  assert.equal(
    new Headers(calls[0].init.headers).get('Idempotency-Key'),
    `strategy-brand-prepare-sha256:${'b'.repeat(64)}`,
  )
  assert.deepEqual(JSON.parse(String(calls[0].init.body)), {
    expected_input_identity_hash: `sha256:${'b'.repeat(64)}`,
    selected_route_id: 'route_brand_video',
    accept_strategy_projection: true,
  })
})
