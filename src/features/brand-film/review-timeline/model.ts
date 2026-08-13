import type { ApiBrandFilmGenerationAttempt, ApiBrandFilmGenerationUnit, ApiBrandFilmShot } from '../../../data/api'

export type GenerationDisplayStatus = 'idle' | 'queued' | 'running' | 'succeeded' | 'failed' | 'retry_failed' | 'locked'

export type GenerationTimelineUnitView = {
  unit: ApiBrandFilmGenerationUnit
  shot?: ApiBrandFilmShot
  startMs: number
  endMs: number
  durationMs: number
  displayAttempt?: ApiBrandFilmGenerationAttempt
  latestAttempt?: ApiBrandFilmGenerationAttempt
  previewUrl: string
  status: GenerationDisplayStatus
}

export function selectDisplayAttempt(unit: ApiBrandFilmGenerationUnit) {
  const locked = unit.locked_attempt_id
    ? unit.attempts.find(attempt => attempt.id === unit.locked_attempt_id)
    : undefined
  if (locked) return locked

  for (let index = unit.attempts.length - 1; index >= 0; index -= 1) {
    const attempt = unit.attempts[index]
    if (attempt.status === 'succeeded' && attempt.output_asset_ref) return attempt
  }
  return unit.attempts.at(-1)
}

export function generationDisplayStatus(unit: ApiBrandFilmGenerationUnit, displayAttempt?: ApiBrandFilmGenerationAttempt): GenerationDisplayStatus {
  if (unit.locked_attempt_id) return 'locked'
  const latest = unit.attempts.at(-1)
  if (!latest) return 'idle'
  if (latest.status === 'failed' && displayAttempt?.status === 'succeeded') return 'retry_failed'
  if (latest.status === 'queued' || latest.status === 'running' || latest.status === 'succeeded' || latest.status === 'failed') return latest.status
  return displayAttempt?.status === 'succeeded' ? 'succeeded' : 'failed'
}

export function createGenerationTimelineView(
  units: ApiBrandFilmGenerationUnit[],
  shots: ApiBrandFilmShot[],
  previewUrls: Record<string, string>,
) {
  const shotsById = new Map(shots.map(shot => [shot.id, shot]))
  return units.map(unit => {
    const displayAttempt = selectDisplayAttempt(unit)
    const latestAttempt = unit.attempts.at(-1)
    return {
      unit,
      shot: shotsById.get(unit.shot_ids[0]),
      startMs: Math.round(unit.start_second * 1000),
      endMs: Math.round(unit.end_second * 1000),
      durationMs: Math.max(1, Math.round((unit.end_second - unit.start_second) * 1000)),
      displayAttempt,
      latestAttempt,
      previewUrl: displayAttempt ? previewUrls[displayAttempt.id] ?? '' : '',
      status: generationDisplayStatus(unit, displayAttempt),
    } satisfies GenerationTimelineUnitView
  })
}

export function generationTimelineDurationMs(units: Array<Pick<GenerationTimelineUnitView, 'endMs'>>, declaredDurationMs?: number) {
  return Math.max(1000, declaredDurationMs ?? 0, ...units.map(unit => unit.endMs))
}

export function activeGenerationUnit<T extends Pick<GenerationTimelineUnitView, 'startMs' | 'endMs'>>(units: T[], timeMs: number) {
  return units.find(unit => timeMs >= unit.startMs && timeMs < unit.endMs) ?? units.at(-1)
}

export function timelineTimeFromClipPointer(startMs: number, durationMs: number, pointerX: number, left: number, width: number) {
  const ratio = width > 0 ? Math.min(1, Math.max(0, (pointerX - left) / width)) : 0
  return Math.round(startMs + durationMs * ratio)
}

export function createTimelineTicks(durationMs: number, intervalMs = 5000) {
  const ticks: number[] = []
  for (let time = 0; time < durationMs; time += intervalMs) ticks.push(time)
  if (ticks.at(-1) !== durationMs) ticks.push(durationMs)
  return ticks
}

export function timelineCanvasWidth(durationMs: number, pixelsPerSecond: number, viewportWidth: number) {
  return Math.max(viewportWidth, durationMs / 1000 * pixelsPerSecond)
}

export function scrollLeftForVisibleRange(scrollLeft: number, viewportWidth: number, rangeLeft: number, rangeRight: number, padding = 24) {
  const visibleLeft = scrollLeft + padding
  const visibleRight = scrollLeft + viewportWidth - padding
  if (rangeLeft < visibleLeft) return Math.max(0, rangeLeft - padding)
  if (rangeRight > visibleRight) return Math.max(0, rangeRight - viewportWidth + padding)
  return scrollLeft
}

export function formatTimelineTime(timeMs: number) {
  const totalSeconds = Math.max(0, timeMs) / 1000
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds - minutes * 60
  const formattedSeconds = seconds.toFixed(seconds % 1 ? 1 : 0).padStart(seconds % 1 ? 4 : 2, '0')
  return `${String(minutes).padStart(2, '0')}:${formattedSeconds}`
}
