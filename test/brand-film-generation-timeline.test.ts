import assert from 'node:assert/strict'
import test from 'node:test'
import type { ApiBrandFilmGenerationUnit } from '../src/data/api.ts'
import {
  activeGenerationUnit,
  createTimelineTicks,
  formatTimelineTime,
  scrollLeftForVisibleRange,
  selectDisplayAttempt,
  timelineCanvasWidth,
  timelineTimeFromClipPointer,
} from '../src/features/brand-film/review-timeline/model.ts'

const attempt = (id: string, ordinal: number, status: string, output = false) => ({
  id,
  ordinal,
  prompt_hash: `hash-${id}`,
  provider_job_id: `job-${id}`,
  status,
  ...(output ? { output_asset_ref: { asset_id: `asset-${id}`, version: 1 } } : {}),
  created_at: '2026-08-12T00:00:00Z',
  updated_at: '2026-08-12T00:00:00Z',
})

const unit = (overrides: Partial<ApiBrandFilmGenerationUnit> = {}): ApiBrandFilmGenerationUnit => ({
  id: 'unit-01',
  order: 1,
  shot_ids: ['shot-01'],
  start_second: 0,
  end_second: 5,
  prompt_packages: [],
  attempts: [],
  ...overrides,
})

test('timeline geometry uses one exact pixel-per-time coordinate system', () => {
  const width = timelineCanvasWidth(30_000, 48, 900)
  assert.equal(width, 1440)
  assert.equal(5_000 * (width / 30_000), 240)
  assert.equal(30_000 * (width / 30_000), width)
})

test('timeline keeps a short film fitted to the available viewport', () => {
  assert.equal(timelineCanvasWidth(15_000, 48, 960), 960)
})

test('clicking inside a shot maps to the corresponding local position', () => {
  assert.equal(timelineTimeFromClipPointer(5_000, 5_000, 350, 100, 500), 7_500)
  assert.equal(timelineTimeFromClipPointer(5_000, 5_000, 20, 100, 500), 5_000)
  assert.equal(timelineTimeFromClipPointer(5_000, 5_000, 700, 100, 500), 10_000)
})

test('active shot changes exactly at its timeline boundary', () => {
  const units = [
    { startMs: 0, endMs: 5_000, id: 'one' },
    { startMs: 5_000, endMs: 10_000, id: 'two' },
    { startMs: 10_000, endMs: 15_000, id: 'three' },
  ]
  assert.equal(activeGenerationUnit(units, 0)?.id, 'one')
  assert.equal(activeGenerationUnit(units, 5_000)?.id, 'two')
  assert.equal(activeGenerationUnit(units, 15_000)?.id, 'three')
})

test('display attempt prefers locked, then latest successful, then latest attempt', () => {
  const successful = attempt('success', 1, 'succeeded', true)
  const failed = attempt('failed', 2, 'failed')
  const locked = attempt('locked', 3, 'succeeded', true)
  assert.equal(selectDisplayAttempt(unit({ attempts: [successful, failed] })).id, 'success')
  assert.equal(selectDisplayAttempt(unit({ attempts: [successful, failed, locked], locked_attempt_id: 'locked' })).id, 'locked')
  assert.equal(selectDisplayAttempt(unit({ attempts: [failed] })).id, 'failed')
})

test('five-second ruler ticks include the exact final duration', () => {
  assert.deepEqual(createTimelineTicks(15_000), [0, 5_000, 10_000, 15_000])
  assert.deepEqual(createTimelineTicks(17_000), [0, 5_000, 10_000, 15_000, 17_000])
})

test('sub-second playhead labels retain two-digit seconds', () => {
  assert.equal(formatTimelineTime(200), '00:00.2')
  assert.equal(formatTimelineTime(5_000), '00:05')
})

test('auto follow scrolls only when the active shot leaves the visible range', () => {
  assert.equal(scrollLeftForVisibleRange(0, 800, 200, 440), 0)
  assert.equal(scrollLeftForVisibleRange(0, 800, 760, 1_000), 224)
  assert.equal(scrollLeftForVisibleRange(500, 800, 420, 620), 396)
})
