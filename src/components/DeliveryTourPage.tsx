import { useCallback, useEffect, useState } from 'react'
import { ArrowRight, Check, CircleAlert, CircleCheck, LockKeyhole, Play, RefreshCw, RotateCcw, ShieldCheck } from 'lucide-react'
import { DeliveryApiError, deliveryTourApi, type DeliveryTourResetResult, type DeliveryTourRun } from '../api/delivery'

const stableRunId = /^[a-z0-9][a-z0-9_-]{2,63}$/
const latestTourRunStorageKey = 'cookies.delivery-tour-latest.v1'

export function getLatestDeliveryTourRunId(projectId: string): string | undefined {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(latestTourRunStorageKey) ?? '{}') as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined
    const value = (parsed as Record<string, unknown>)[projectId]
    return typeof value === 'string' && stableRunId.test(value) ? value : undefined
  } catch {
    return undefined
  }
}

function rememberLatestDeliveryTourRun(projectId: string, runId: string) {
  try {
    const raw = window.localStorage.getItem(latestTourRunStorageKey)
    const parsed = raw ? JSON.parse(raw) as unknown : {}
    const values = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {}
    window.localStorage.setItem(latestTourRunStorageKey, JSON.stringify({ ...values, [projectId]: runId }))
  } catch {
    // Tour progress remains authoritative in the Go API; this only restores navigation context.
  }
}

function defaultRunId() {
  const date = new Date()
  const stamp = [date.getFullYear(), date.getMonth() + 1, date.getDate()].map((part, index) => index === 0 ? String(part) : String(part).padStart(2, '0')).join('')
  return `delivery-tour-${stamp}`
}

function displayTime(value?: string) {
  if (!value) return '尚未发生'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function syncTourLocation(projectId: string, runId: string) {
  const url = new URL(window.location.href)
  url.pathname = `/projects/${encodeURIComponent(projectId)}/delivery/tour`
  url.searchParams.set('tour_run_id', runId)
  url.searchParams.delete('tour_case')
  window.history.replaceState({}, '', `${url.pathname}${url.search}`)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

function errorMessage(cause: unknown) {
  if (cause instanceof DeliveryApiError && cause.code === 'RESOURCE_NOT_FOUND') return '尚未准备这个运行。请核对运行 ID，或直接准备一套闭环数据。'
  if (cause instanceof Error) return cause.message
  return '上线后优化闭环暂时不可用，请稍后重试。'
}

export function DeliveryTourPage({ projectId, routeRunId }: { projectId: string; routeRunId?: string }) {
  const [runId, setRunId] = useState(routeRunId ?? getLatestDeliveryTourRunId(projectId) ?? defaultRunId)
  const [run, setRun] = useState<DeliveryTourRun | null>(null)
  const [resetResult, setResetResult] = useState<DeliveryTourResetResult | null>(null)
  const [busy, setBusy] = useState<'prepare' | 'refresh' | 'reset' | null>(null)
  const [error, setError] = useState('')
  const [resetArmed, setResetArmed] = useState(false)

  const loadRun = useCallback(async (targetRunId: string, restoreLocation = false) => {
    if (!stableRunId.test(targetRunId)) return
    setBusy('refresh')
    setError('')
    try {
      const value = await deliveryTourApi.get(projectId, targetRunId)
      setRun(value)
      setRunId(value.id)
      rememberLatestDeliveryTourRun(projectId, value.id)
      if (restoreLocation) syncTourLocation(projectId, value.id)
    } catch (cause) {
      setRun(null)
      setError(errorMessage(cause))
    } finally {
      setBusy(null)
    }
  }, [projectId])

  useEffect(() => {
    const targetRunId = routeRunId ?? getLatestDeliveryTourRunId(projectId)
    if (!targetRunId || !stableRunId.test(targetRunId)) return
    void loadRun(targetRunId, !routeRunId)
  }, [loadRun, projectId, routeRunId])

  useEffect(() => {
    if (!run?.id) return
    const refreshOnReturn = () => {
      if (document.visibilityState === 'visible') void loadRun(run.id)
    }
    window.addEventListener('focus', refreshOnReturn)
    document.addEventListener('visibilitychange', refreshOnReturn)
    return () => {
      window.removeEventListener('focus', refreshOnReturn)
      document.removeEventListener('visibilitychange', refreshOnReturn)
    }
  }, [loadRun, run?.id])

  const validRunId = stableRunId.test(runId)
  const completedSteps = run?.steps.filter(step => step.complete).length ?? 0
  const progress = run?.steps.length ? Math.round(completedSteps / run.steps.length * 100) : 0
  const nextStep = run?.steps.find(step => !step.complete)

  const prepare = async () => {
    if (!validRunId) return
    setBusy('prepare')
    setError('')
    setResetResult(null)
    setResetArmed(false)
    try {
      const value = await deliveryTourApi.prepare(projectId, runId)
      setRun(value)
      rememberLatestDeliveryTourRun(projectId, value.id)
      syncTourLocation(projectId, value.id)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(null)
    }
  }

  const refresh = async () => {
    if (!validRunId) return
    await loadRun(runId)
  }

  const reset = async () => {
    if (!run || !resetArmed) {
      setResetArmed(true)
      return
    }
    setBusy('reset')
    setError('')
    try {
      const value = await deliveryTourApi.reset(projectId, run.id)
      setRun(value.run)
      setResetResult(value)
      setResetArmed(false)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(null)
    }
  }

  return <div className="delivery-tour" data-testid="delivery-tour">
    <section className="delivery-tour-hero">
      <div>
        <span className="delivery-tour-kicker"><Play size={14}/>20–30 分钟业务演示</span>
        <h2>从计划创建到优化审批，一次走完上线后闭环</h2>
        <p>从计划来源、首次上线授权、平台操作演练，一直走到上线后指标、告警、优化建议与第二次授权。</p>
      </div>
      <div className="delivery-tour-trust">
        <ShieldCheck size={22}/>
        <div><strong>安全边界</strong><span>组织 + Project + 运行 ID + 当前登录会话</span></div>
      </div>
    </section>

    <section className="delivery-tour-controls" aria-label="演示运行控制">
      <label htmlFor="delivery-tour-run-id">稳定运行 ID</label>
      <div className="delivery-tour-control-row">
        <input id="delivery-tour-run-id" value={runId} onChange={event => { setRunId(event.target.value.trim()); setResetArmed(false) }} aria-invalid={!validRunId}/>
        <button className="primary-button" disabled={!validRunId || busy !== null} onClick={() => { void prepare() }}>
          {busy === 'prepare' ? '正在准备…' : '准备完整数据'}
        </button>
        <button className="secondary-button" disabled={!validRunId || busy !== null} onClick={() => { void refresh() }}><RefreshCw size={15}/>刷新进度</button>
      </div>
      {!validRunId ? <p className="delivery-tour-field-error">使用 3–64 位小写字母、数字、连字符或下划线，并以字母或数字开头。</p> : null}
    </section>

    {error ? <div className="delivery-tour-message danger" role="alert"><CircleAlert size={18}/><span>{error}</span></div> : null}
    {resetResult ? <div className="delivery-tour-message success" role="status"><CircleCheck size={18}/><span>已只复位运行 {resetResult.run.id}；共删除 {Object.values(resetResult.deleted).reduce((sum, count) => sum + count, 0)} 条关联记录，普通计划与其他运行不受影响。</span></div> : null}

    {run ? <>
      <section className="delivery-tour-overview">
        <div className="delivery-tour-progress-card">
          <div className="delivery-tour-progress-copy"><span>黄金路径</span><strong>{run.status === 'reset' ? '已复位' : `${completedSteps} / ${run.steps.length} 步完成`}</strong></div>
          <div className="delivery-tour-progress-track" aria-label={`完成 ${progress}%`}><span style={{ width: `${progress}%` }}/></div>
          <p>{run.status === 'reset' ? '运行的因果数据已删除，可使用相同 ID 重新准备。' : nextStep ? `下一步：${nextStep.title}` : '黄金路径已经完成，可检查异常场景或安全复位。'}</p>
          {run.status !== 'reset' && nextStep ? <a className="primary-button delivery-tour-next" href={nextStep.url}>继续下一步<ArrowRight size={15}/></a> : null}
        </div>
        <dl className="delivery-tour-facts">
          <div><dt>运行 ID</dt><dd>{run.id}</dd></div>
          <div><dt>运行边界</dt><dd>当前登录会话</dd></div>
          <div><dt>准备时间</dt><dd>{displayTime(run.preparedAt)}</dd></div>
          <div><dt>最近更新</dt><dd>{displayTime(run.updatedAt)}</dd></div>
          <div><dt>业务场景</dt><dd>{run.scenario}</dd></div>
        </dl>
      </section>

      <section className="delivery-tour-section">
        <div className="delivery-tour-section-heading"><div><span>黄金路径</span><h3>每一步都由服务端事实判定</h3></div><p>刷新页面不会丢失进度；完成条件和证据来自持久化 Plan、Approval、Execution、Alert 与 Recommendation。</p></div>
        <ol className="delivery-tour-steps">
          {run.steps.map((step, index) => {
            const available = step.complete || step.key === run.currentStep
            return <li key={step.key} className={step.complete ? 'complete' : step.key === run.currentStep ? 'current' : 'locked'}>
            <div className="delivery-tour-step-index">{step.complete ? <Check size={16}/> : index}</div>
            <div className="delivery-tour-step-copy"><strong>{step.title}</strong><p>{step.explanation}</p><span>完成条件：{step.completionCondition}</span>{step.evidence.length ? <code>{step.evidence.join(' · ')}</code> : null}</div>
            {available ? <a href={step.url} aria-label={`打开${step.title}`}>打开<ArrowRight size={14}/></a> : <span className="delivery-tour-step-locked"><LockKeyhole size={14}/>先完成上一步</span>}
          </li>})}
        </ol>
      </section>

      <section className="delivery-tour-section">
        <div className="delivery-tour-section-heading"><div><span>异常矩阵</span><h3>六类独立失败与恢复信号</h3></div><p>每张卡都提供固定场景、预期结果、观察时间和可复核证据，不依赖客户端拼装结论。</p></div>
        <div className="delivery-tour-cases">
          {run.cases.filter(tourCase => tourCase.key !== 'golden_path').map(tourCase => <article key={tourCase.key}>
            <div className="delivery-tour-case-top"><span className={`delivery-tour-case-status ${tourCase.status}`}>{tourCase.status === 'observed' ? '已观察' : tourCase.status === 'missing' ? '已复位' : '已准备'}</span><code>{tourCase.scenario}</code></div>
            <h4>{tourCase.title}</h4>
            <p>{tourCase.expectedOutcome}</p>
            <div className="delivery-tour-evidence"><span>证据</span>{tourCase.evidence.length ? tourCase.evidence.map(item => <code key={item}>{item}</code>) : <em>复位后无关联记录</em>}</div>
            <footer><span>{displayTime(tourCase.observedAt)}</span>{tourCase.planId ? <a href={tourCase.startUrl}>查看场景<ArrowRight size={14}/></a> : null}</footer>
          </article>)}
        </div>
      </section>

      <section className={`delivery-tour-reset ${resetArmed ? 'armed' : ''}`}>
        <div><h3>{resetArmed ? '确认精确复位' : '演示结束后安全复位'}</h3><p>{resetArmed ? `将只删除 ${run.organizationId} / ${run.projectId} / ${run.id} / ${run.ownerId} 的因果闭包。` : '不会按项目、名称或时间范围批量清理；其他运行和普通计划保持不变。'}</p></div>
        <div className="delivery-tour-reset-actions">{resetArmed ? <button className="secondary-button" onClick={() => setResetArmed(false)} disabled={busy !== null}>取消</button> : null}<button className="danger-button" onClick={() => { void reset() }} disabled={busy !== null || run.status === 'reset'}><RotateCcw size={15}/>{busy === 'reset' ? '正在复位…' : resetArmed ? '确认只复位此运行' : '准备复位'}</button></div>
      </section>
    </> : <section className="delivery-tour-empty"><span>尚未加载演示运行</span><h3>用一个稳定 ID 准备七套确定性场景</h3><p>再次使用相同 ID 会读取原运行，不会生成重复计划。准备中断时，服务端会在同一精确边界内清理后重建。</p></section>}

  </div>
}

export function DeliveryMockEnvironmentBanner() {
  return <aside className="delivery-mock-environment" aria-label="Delivery Mock 环境">
    <span>Mock 环境</span>
    <p>当前流程使用确定性场景数据；平台字段、真实账户与实时效果尚未接入。</p>
  </aside>
}

export function DeliveryTourContextBanner({ projectId, runId, tourCase }: { projectId: string; runId: string; tourCase?: string }) {
  const href = `/projects/${encodeURIComponent(projectId)}/delivery/tour?tour_run_id=${encodeURIComponent(runId)}`
  return <aside className="delivery-tour-context" aria-label="闭环走测上下文"><span>走测运行</span><strong>{runId}</strong>{tourCase ? <span>{tourCase}</span> : null}<a href={href}>返回走测总览<ArrowRight size={14}/></a></aside>
}
