import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, Check, Film, History, LoaderCircle, Lock, Play, RefreshCw } from 'lucide-react'
import type { ApiBrandFilmGenerationUnit, ApiBrandFilmShot } from '../../../data/api'
import {
  activeGenerationUnit,
  createGenerationTimelineView,
  createTimelineTicks,
  formatTimelineTime,
  generationTimelineDurationMs,
  scrollLeftForVisibleRange,
  timelineCanvasWidth,
  timelineTimeFromClipPointer,
} from './model'
import './review-timeline.css'

type Props = {
  units: ApiBrandFilmGenerationUnit[]
  shots: ApiBrandFilmShot[]
  masterDurationMs?: number
  finalPreview: string
  attemptPreviews: Record<string, string>
  busy: boolean
  feedbackByUnit: Record<string, string>
  onFeedback: (unitId: string, feedback: string) => void
  onGenerate: (unitId: string, feedback?: string) => void
  onLock: (unitId: string, attemptId: string) => void
}

const PIXELS_PER_SECOND = 48

const statusCopy = {
  idle: '待生成',
  queued: '排队中',
  running: '生成中',
  succeeded: '已生成',
  failed: '生成失败',
  retry_failed: '重试失败，保留上一版',
  locked: '已锁定',
} as const

export function BrandFilmReviewTimeline({ units, shots, masterDurationMs, finalPreview, attemptPreviews, busy, feedbackByUnit, onFeedback, onGenerate, onLock }: Props) {
  const timelineUnits = useMemo(() => createGenerationTimelineView(units, shots, attemptPreviews), [attemptPreviews, shots, units])
  const durationMs = generationTimelineDurationMs(timelineUnits, masterDurationMs)
  const [selectedUnitId, setSelectedUnitId] = useState(timelineUnits[0]?.unit.id ?? '')
  const [selectedAttemptId, setSelectedAttemptId] = useState('')
  const [playheadMs, setPlayheadMs] = useState(timelineUnits[0]?.startMs ?? 0)
  const [viewportWidth, setViewportWidth] = useState(0)
  const playerRef = useRef<HTMLVideoElement>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const pendingSeekRef = useRef<{ globalMs: number; autoplay: boolean } | null>(null)
  const composedSeekGuardRef = useRef<{ targetMs: number; expiresAt: number } | null>(null)
  const followModeRef = useRef<ScrollBehavior>('auto')

  const selectedUnit = timelineUnits.find(item => item.unit.id === selectedUnitId) ?? timelineUnits[0]
  const selectedAttempt = selectedUnit?.unit.attempts.find(attempt => attempt.id === selectedAttemptId) ?? selectedUnit?.displayAttempt
  const selectedPreview = selectedAttempt ? attemptPreviews[selectedAttempt.id] ?? '' : ''
  const playerSource = finalPreview || selectedPreview
  const composed = Boolean(finalPreview)
  const canvasWidth = timelineCanvasWidth(durationMs, PIXELS_PER_SECOND, viewportWidth)
  const pixelsPerMs = canvasWidth / durationMs
  const ticks = createTimelineTicks(durationMs)

  useEffect(() => {
    if (timelineUnits.some(item => item.unit.id === selectedUnitId)) return
    setSelectedUnitId(timelineUnits[0]?.unit.id ?? '')
  }, [selectedUnitId, timelineUnits])

  useEffect(() => {
    if (!selectedUnit) return
    const preferred = selectedUnit.displayAttempt?.id ?? ''
    if (selectedUnit.unit.attempts.some(attempt => attempt.id === selectedAttemptId)) return
    setSelectedAttemptId(preferred)
  }, [selectedAttemptId, selectedUnit])

  useEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    const updateWidth = () => setViewportWidth(viewport.clientWidth)
    updateWidth()
    const observer = new ResizeObserver(updateWidth)
    observer.observe(viewport)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    const pending = pendingSeekRef.current
    const player = playerRef.current
    if (!pending || !player || !selectedUnit || !playerSource) return
    const targetSeconds = composed ? pending.globalMs / 1000 : (pending.globalMs - selectedUnit.startMs) / 1000
    const applySeek = () => {
      player.currentTime = Math.max(0, targetSeconds)
      if (pending.autoplay) void player.play().catch(() => undefined)
      pendingSeekRef.current = null
    }
    if (player.readyState >= 1) applySeek()
    else player.addEventListener('loadedmetadata', applySeek, { once: true })
    return () => player.removeEventListener('loadedmetadata', applySeek)
  }, [composed, playerSource, selectedUnit])

  useEffect(() => {
    if (!selectedUnit || !viewportRef.current) return
    const viewport = viewportRef.current
    const nextLeft = scrollLeftForVisibleRange(
      viewport.scrollLeft,
      viewport.clientWidth,
      selectedUnit.startMs * pixelsPerMs,
      selectedUnit.endMs * pixelsPerMs,
    )
    if (Math.abs(nextLeft - viewport.scrollLeft) > 1) viewport.scrollTo({ left: nextLeft, behavior: followModeRef.current })
    followModeRef.current = 'auto'
  }, [pixelsPerMs, selectedUnit])

  if (!selectedUnit) return null

  const seekTo = (globalMs: number, autoplay = true) => {
    const clampedMs = Math.min(durationMs, Math.max(0, globalMs))
    const targetUnit = activeGenerationUnit(timelineUnits, clampedMs) ?? selectedUnit
    followModeRef.current = 'smooth'
    setSelectedUnitId(targetUnit.unit.id)
    setSelectedAttemptId(targetUnit.displayAttempt?.id ?? '')
    setPlayheadMs(clampedMs)
    if (composed && playerRef.current) {
      composedSeekGuardRef.current = { targetMs: clampedMs, expiresAt: Date.now() + 1500 }
      playerRef.current.currentTime = clampedMs / 1000
      if (autoplay) void playerRef.current.play().catch(() => undefined)
      pendingSeekRef.current = null
      return
    }
    pendingSeekRef.current = { globalMs: clampedMs, autoplay }
  }

  const handleTimeUpdate = () => {
    const player = playerRef.current
    if (!player) return
    const globalMs = composed ? player.currentTime * 1000 : selectedUnit.startMs + player.currentTime * 1000
    const seekGuard = composedSeekGuardRef.current
    if (seekGuard) {
      if (Math.abs(globalMs - seekGuard.targetMs) <= 750 || Date.now() >= seekGuard.expiresAt) composedSeekGuardRef.current = null
      else return
    }
    const clampedMs = Math.min(durationMs, globalMs)
    setPlayheadMs(clampedMs)
    const active = activeGenerationUnit(timelineUnits, clampedMs)
    if (composed && active && active.unit.id !== selectedUnit.unit.id) {
      setSelectedUnitId(active.unit.id)
      setSelectedAttemptId(active.displayAttempt?.id ?? '')
    }
  }

  const selectAdjacentUnit = (direction: -1 | 1) => {
    const currentIndex = timelineUnits.findIndex(item => item.unit.id === selectedUnit.unit.id)
    const target = timelineUnits[Math.min(timelineUnits.length - 1, Math.max(0, currentIndex + direction))]
    if (target) seekTo(target.startMs)
  }

  const activePrompt = selectedUnit.unit.prompt_packages.at(-1)
  const latestAttemptFailed = selectedUnit.latestAttempt?.status === 'failed'

  return <div className="brand-generation-review-v2">
    <section className="brand-generation-master-player" aria-label="视频主播放器">
      <header>
        <div><span>{composed ? `${formatTimelineTime(durationMs)} 合成预览` : `镜头 ${String(selectedUnit.unit.order).padStart(2, '0')} 预览`}</span><h4>{composed ? '已锁定片段合成视频' : selectedUnit.shot?.purpose || selectedUnit.unit.shot_ids.join(' + ')}</h4></div>
        <p>{composed ? '播放器保持完整成片，点击下方镜头会精准跳转，不会重新加载视频。' : '当前播放所选镜头的生成版本；全部锁定后可合成为完整成片。'}</p>
      </header>
      <div className="brand-generation-master-canvas">
        {playerSource ? <video ref={playerRef} key={playerSource} controls playsInline src={playerSource} onTimeUpdate={handleTimeUpdate}/> : <div className="brand-generation-player-empty"><Film size={30}/><b>{statusCopy[selectedUnit.status]}</b><span>生成完成后，当前镜头会在这里播放</span></div>}
      </div>
    </section>

    <section className="brand-generation-shot-timeline" aria-label="镜头时间轴">
      <header><div><h4>镜头时间轴</h4><span>点击镜头内部任意位置，可跳到该镜头对应时刻</span></div><time>{formatTimelineTime(playheadMs)} / {formatTimelineTime(durationMs)}</time></header>
      <div className="brand-generation-timeline-viewport" ref={viewportRef}>
        <div className="brand-generation-timeline-canvas" role="slider" tabIndex={0} aria-label="镜头时间轴播放位置" aria-valuemin={0} aria-valuemax={durationMs} aria-valuenow={playheadMs} style={{ width: `${canvasWidth}px` }} onKeyDown={event => {
          const stepMs = event.shiftKey ? 5000 : 1000
          if (event.key === 'ArrowLeft') { event.preventDefault(); seekTo(playheadMs - stepMs) }
          if (event.key === 'ArrowRight') { event.preventDefault(); seekTo(playheadMs + stepMs) }
          if (event.key === 'Home') { event.preventDefault(); seekTo(0) }
          if (event.key === 'End') { event.preventDefault(); seekTo(durationMs) }
        }} onClick={event => {
          if (event.target !== event.currentTarget) return
          const bounds = event.currentTarget.getBoundingClientRect()
          seekTo((event.clientX - bounds.left) / pixelsPerMs)
        }}>
          <div className="brand-generation-ruler-v2" aria-hidden="true">
            {ticks.map(tick => <span key={tick} style={{ left: `${tick * pixelsPerMs}px` }}><i/>{formatTimelineTime(tick)}</span>)}
          </div>
          <div className="brand-generation-playhead-v2" aria-hidden="true" style={{ left: `${playheadMs * pixelsPerMs}px` }}/>
          <div className="brand-generation-clips-v2">
            {timelineUnits.map(item => {
              const selected = item.unit.id === selectedUnit.unit.id
              const quickAction = item.status === 'idle' || item.status === 'failed' ? '生成' : item.status === 'succeeded' || item.status === 'retry_failed' ? '重生成' : ''
              return <div
                role="button"
                tabIndex={0}
                key={item.unit.id}
                data-timeline-clip="true"
                className={`${selected ? 'selected ' : ''}${item.status}`}
                style={{ left: `${item.startMs * pixelsPerMs}px`, width: `${item.durationMs * pixelsPerMs}px` }}
                aria-pressed={selected}
                aria-label={`镜头 ${item.unit.order}，${formatTimelineTime(item.startMs)} 至 ${formatTimelineTime(item.endMs)}，${statusCopy[item.status]}`}
                onClick={event => seekTo(timelineTimeFromClipPointer(item.startMs, item.durationMs, event.clientX, event.currentTarget.getBoundingClientRect().left, event.currentTarget.getBoundingClientRect().width))}
                onKeyDown={event => {
                  if (event.key === 'ArrowLeft') { event.preventDefault(); selectAdjacentUnit(-1) }
                  if (event.key === 'ArrowRight') { event.preventDefault(); selectAdjacentUnit(1) }
                  if (event.key === 'Enter') { event.preventDefault(); seekTo(item.startMs) }
                }}
              >
                <span className="brand-generation-clip-thumb-v2">{item.previewUrl ? <video muted preload="metadata" src={item.previewUrl}/> : <Film size={18}/>}</span>
                <span className="brand-generation-clip-copy-v2"><b>镜头 {String(item.unit.order).padStart(2, '0')}</b><em>{item.shot?.purpose || item.unit.shot_ids.join(' + ')}</em><small>{formatTimelineTime(item.startMs)}–{formatTimelineTime(item.endMs)} · {statusCopy[item.status]}</small></span>
                {item.status === 'locked' ? <Lock className="brand-generation-clip-lock" size={13}/> : null}
                {quickAction ? <button type="button" className="brand-generation-clip-action" aria-label={`${quickAction}镜头 ${item.unit.order}`} disabled={busy} onClick={event => { event.stopPropagation(); onGenerate(item.unit.id) }}>{quickAction === '生成' ? <Play size={11}/> : <RefreshCw size={11}/>}<i>{quickAction}</i></button> : null}
              </div>
            })}
          </div>
        </div>
      </div>
      <p>镜头较多时，可拖动底部滚动条横向查看；播放时仅在当前镜头离开可视区后自动跟随。</p>
    </section>

    <section className={`brand-generation-unit-inspector-v2 ${selectedUnit.status}`} aria-label="当前镜头详情">
      <header>
        <div><span>当前镜头</span><h4>镜头 {String(selectedUnit.unit.order).padStart(2, '0')} · {selectedUnit.shot?.purpose || selectedUnit.unit.shot_ids.join(' + ')}</h4><p>{formatTimelineTime(selectedUnit.startMs)}–{formatTimelineTime(selectedUnit.endMs)} · {selectedUnit.unit.shot_ids.join(' + ')}</p></div>
        <strong>{selectedUnit.status === 'locked' ? <Lock size={13}/> : latestAttemptFailed ? <AlertCircle size={13}/> : selectedUnit.status === 'queued' || selectedUnit.status === 'running' ? <LoaderCircle className="spin" size={13}/> : <Check size={13}/>} {statusCopy[selectedUnit.status]}</strong>
      </header>
      <div className="brand-generation-inspector-grid-v2">
        <div className="brand-generation-version-panel">
          <div className="brand-generation-meta-v2"><span>PromptPackage r{activePrompt?.revision ?? 0}</span><span title={activePrompt?.content_hash}>{activePrompt?.content_hash?.slice(0, 28) ?? '等待冻结 Prompt'}{activePrompt?.content_hash ? '…' : ''}</span></div>
          <div className="brand-generation-attempts-v2"><h5><History size={13}/>生成版本</h5>{selectedUnit.unit.attempts.length ? selectedUnit.unit.attempts.slice().reverse().map(attempt => {
            const playable = Boolean(attempt.output_asset_ref && attemptPreviews[attempt.id])
            const active = attempt.id === selectedAttempt?.id
            return <button type="button" key={attempt.id} className={active ? 'active' : ''} disabled={!playable && attempt.status !== 'failed'} onClick={() => { setSelectedAttemptId(attempt.id); if (playable && !composed) { pendingSeekRef.current = { globalMs: selectedUnit.startMs, autoplay: true }; setPlayheadMs(selectedUnit.startMs) } }}><span><b>Attempt {attempt.ordinal}</b><small>{attempt.status === 'succeeded' ? '生成成功' : attempt.status === 'failed' ? '生成失败' : statusCopy[attempt.status as 'queued' | 'running'] ?? attempt.status}{attempt.id === selectedUnit.unit.locked_attempt_id ? ' · 已锁定' : ''}</small></span>{playable ? <Play size={12}/> : attempt.status === 'failed' ? <AlertCircle size={12}/> : <LoaderCircle className="spin" size={12}/>}</button>
          }) : <p>尚无生成版本。</p>}</div>
        </div>
        <div className="brand-generation-actions-v2">
          {selectedUnit.status === 'queued' || selectedUnit.status === 'running' ? <div className="brand-unit-progress"><LoaderCircle className="spin" size={14}/><span>{selectedUnit.status === 'queued' ? '已进入生成队列，请稍候…' : '正在生成视频，请稍候…'}</span></div> : selectedUnit.status === 'locked' ? <p>该镜头已锁定并进入合成预览。需要修改时，请在后续版本中重新打开镜头。</p> : <>
            {latestAttemptFailed ? <div className="brand-unit-error"><b>最近一次生成失败</b><span>{selectedUnit.latestAttempt?.error_message || '上一版成功视频仍已保留，可继续播放或再次重试。'}</span></div> : null}
            {selectedUnit.displayAttempt?.status === 'succeeded' ? <textarea placeholder="填写对当前镜头的局部反馈，例如：稳定瓶身标签，减少镜头环绕" value={feedbackByUnit[selectedUnit.unit.id] ?? ''} onChange={event => onFeedback(selectedUnit.unit.id, event.target.value)}/> : null}
            <div><button className={selectedUnit.displayAttempt?.status === 'succeeded' ? 'secondary-button' : 'primary-button'} disabled={busy || (selectedUnit.displayAttempt?.status === 'succeeded' && !feedbackByUnit[selectedUnit.unit.id]?.trim())} onClick={() => onGenerate(selectedUnit.unit.id, feedbackByUnit[selectedUnit.unit.id]?.trim() || undefined)}><RefreshCw size={13}/>{selectedUnit.displayAttempt?.status === 'succeeded' ? '按反馈重新生成' : '生成此镜头'}</button>{selectedAttempt?.status === 'succeeded' && selectedAttempt.output_asset_ref ? <button className="primary-button" disabled={busy} onClick={() => onLock(selectedUnit.unit.id, selectedAttempt.id)}><Lock size={13}/>锁定当前版本</button> : null}</div>
          </>}
        </div>
      </div>
    </section>
  </div>
}
