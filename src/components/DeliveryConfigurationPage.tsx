import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowRight, Check, CircleAlert, RefreshCw, ShieldCheck } from 'lucide-react'
import {
  DeliveryApiError,
  deliveryPlanApi,
  type DeliveryControlChangeSet,
  type DeliveryPlan,
  type PlatformConfiguration,
} from '../api/delivery'
import { useProject } from '../context/ProjectContext'
import { projectPath } from '../lib/router'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'

function formatCny(value: number) {
  return (value / 100).toLocaleString('zh-CN', { style: 'currency', currency: 'CNY' })
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }) : '暂无记录'
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof DeliveryApiError) {
    if (error.code === 'LEGACY_CONFIGURATION_UNSUPPORTED') return '这份历史配置仅供查看，不能继续提交或修改。请新建投放计划。'
    if (error.code === 'VERSION_CONFLICT') return '计划版本已更新，请刷新后重试。'
    return error.message
  }
  return error instanceof Error ? error.message : fallback
}

function PlatformConfigurationDetails({ value }: { value: PlatformConfiguration }) {
  if (value.platform === 'magnetic_engine') {
    return <div className="delivery-config-empty-inline"><CircleAlert size={18}/><div><b>磁力引擎能力尚未开放</b><p>{value.payload.magnetic_engine?.reason}</p></div></div>
  }
  const ocean = value.payload.ocean_engine
  if (!ocean?.project) return <div className="delivery-config-empty-inline"><CircleAlert size={18}/>平台配置缺少主投放项目。</div>
  const project = ocean.project
  return <div className="delivery-config-business-map">
    <section className="delivery-config-project-card">
      <header><div><span className="delivery-config-eyebrow">主投放项目</span><h4>{project.project_name}</h4><p>预算、排期和营销目标在此项目下统一生效。</p></div><strong className="delivery-config-ready-state">配置已就绪</strong></header>
      <dl className="delivery-config-project-facts">
        <div><dt>营销目标</dt><dd>{project.marketing_purpose}</dd><small>{project.marketing_scenario}</small></div>
        <div><dt>每日预算</dt><dd>{formatCny(project.budget_and_bidding.daily_budget_minor)}</dd><small>{project.budget_and_bidding.bidding_strategy} · {project.budget_and_bidding.charging_mode}</small></div>
        <div><dt>投放时间</dt><dd>{new Date(project.schedule.start_at).toLocaleDateString('zh-CN')} — {new Date(project.schedule.end_at).toLocaleDateString('zh-CN')}</dd><small>{project.schedule.timezone}</small></div>
        <div><dt>广告账户</dt><dd>{project.account_reference.display_name_snapshot || '已选择广告账户'}</dd><small>{project.account_reference.state}</small></div>
      </dl>
    </section>
    <section className="delivery-config-promotion-section">
      <header><div><span className="delivery-config-eyebrow">推广单元</span><h4>素材与文案组合</h4><p>所有推广单元均归属于上方主投放项目。</p></div><strong>{ocean.promotions.length} 个</strong></header>
      {ocean.promotions.length ? <div className="delivery-config-promotion-grid">{ocean.promotions.map((promotion, index) => <article key={promotion.promotion_draft_id}>
        <header><span>推广单元 {index + 1}</span><h5>{promotion.promotion_name}</h5></header>
        <dl><div><dt>素材</dt><dd>{promotion.base_material_references.length} 个</dd></div><div><dt>文案</dt><dd>{promotion.copy_items.length} 条</dd></div><div><dt>落地页</dt><dd>{promotion.landing_page_reference ? '已关联' : '未关联'}</dd></div></dl>
      </article>)}</div> : <div className="delivery-config-empty-inline">暂未添加推广单元，可稍后从投放计划补充素材与文案。</div>}
    </section>
  </div>
}

export function DeliveryConfigurationPage({ state, activeView, tourRunId, tourCase }: { state: DataState; activeView: string; tourRunId?: string; tourCase?: string }) {
  const { currentProject } = useProject()
  const projectId = currentProject.id
  const [plans, setPlans] = useState<DeliveryPlan[]>([])
  const [selectedId, setSelectedId] = useState(() => new URLSearchParams(window.location.search).get('plan_id') ?? '')
  const [changeSet, setChangeSet] = useState<DeliveryControlChangeSet>()
  const [preflightMessage, setPreflightMessage] = useState('尚未检查当前计划。')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const refreshGenerationRef = useRef(0)

  const selectedPlan = useMemo(() => plans.find(plan => plan.id === selectedId), [plans, selectedId])
  const platformConfiguration = selectedPlan?.currentVersion.platformConfiguration
  const legacyReadOnly = Boolean(selectedPlan?.currentVersion.readOnly || (selectedPlan && !platformConfiguration))
  const showConfiguration = activeView === '配置映射'
  const showPreflight = activeView === '检查与提交'
  const approvalURL = changeSet ? projectPath(projectId, 'delivery', 'approvals', changeSet.id, '待我审批', undefined, tourRunId, tourCase) : undefined
  const planEditorURL = projectPath(projectId, 'delivery', 'plans', undefined, '计划列表', undefined, tourRunId, tourCase)
  const canSubmit = !legacyReadOnly && (!changeSet || changeSet.status === 'draft' || changeSet.status === 'preflight_failed' || changeSet.status === 'rejected')

  const restoreWorkflow = async (planId: string) => {
    if (!planId) return
    try {
      const changeSets = await deliveryPlanApi.listChangeSets(projectId)
      setChangeSet(changeSets.filter(item => item.planId === planId).sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))[0])
    } catch (error) {
      setNotice(errorMessage(error, '恢复计划的变更申请失败。'))
    }
  }

  const refresh = async () => {
    const generation = ++refreshGenerationRef.current
    setBusy(true)
    try {
      const nextPlans = await deliveryPlanApi.list(projectId)
      if (generation !== refreshGenerationRef.current) return
      setPlans(nextPlans)
      setSelectedId(current => nextPlans.some(plan => plan.id === current) ? current : nextPlans[0]?.id ?? '')
      setNotice(nextPlans.length ? '已刷新当前 Project 的平台配置。' : '当前 Project 暂无投放计划。')
    } catch (error) {
      if (generation === refreshGenerationRef.current) setNotice(errorMessage(error, '读取平台配置失败。'))
    } finally {
      if (generation === refreshGenerationRef.current) setBusy(false)
    }
  }

  useEffect(() => { void refresh() }, [projectId])
  useEffect(() => { if (selectedId) void restoreWorkflow(selectedId) }, [projectId, selectedId])
  useEffect(() => {
    if (!selectedId) return
    const url = new URL(window.location.href)
    url.searchParams.set('plan_id', selectedId)
    window.history.replaceState(window.history.state, '', url)
  }, [selectedId])

  const preflightPlan = async () => {
    if (!selectedPlan || legacyReadOnly) return
    setBusy(true)
    try {
      const result = await deliveryPlanApi.preflight(projectId, selectedPlan.id)
      setPreflightMessage(result.passed ? `检查通过（${formatTime(result.checkedAt)}）。` : `检查未通过：${result.checks.filter(check => !check.passed).map(check => check.message).join('；')}`)
    } catch (error) { setNotice(errorMessage(error, '检查计划失败。')) } finally { setBusy(false) }
  }

  const createAndPreflightChangeSet = async () => {
    if (!selectedPlan || legacyReadOnly) return
    setBusy(true)
    try {
      const draft = changeSet?.status === 'draft' ? changeSet : await deliveryPlanApi.createChangeSet(projectId, selectedPlan.id, selectedPlan.currentVersionNumber)
      const checked = await deliveryPlanApi.preflightChangeSet(projectId, draft.id, draft.version)
      setChangeSet(checked)
      setNotice(checked.status === 'preflight_passed' ? '变更申请已提交审批中心。' : '变更申请未通过最终检查。')
    } catch (error) { setNotice(errorMessage(error, '提交变更申请失败。')) } finally { setBusy(false) }
  }

  return <StateBoundary state={state} contextLabel="智能投放 / 平台配置" errorDetail="当前 Project 的平台配置无法读取。">
    <div className="delivery-config-workspace">
      <header className="delivery-config-heading"><div><span className="section-label">投放配置</span><h2>平台投放配置</h2><p>核对业务意图对应的平台项目、预算、排期和推广单元，并在提交前完成检查。</p></div><div className="delivery-config-source"><button onClick={() => void refresh()} disabled={busy}><RefreshCw size={14}/>刷新当前 Project</button></div></header>
      <section className="delivery-config-toolbar"><label>投放计划<select value={selectedId} onChange={event => setSelectedId(event.target.value)}>{plans.map(plan => <option value={plan.id} key={plan.id}>{plan.currentVersion.name} · V{plan.currentVersionNumber}</option>)}</select></label><a className="secondary-button" href={planEditorURL}>查看投放计划</a></section>

      {!selectedPlan ? <div className="panel-empty">当前 Project 暂无投放计划。<a href={planEditorURL}>前往创建</a></div> : legacyReadOnly ? <section className="delivery-config-config-card">
        <div className="delivery-config-empty-inline"><CircleAlert size={20}/><div><b>历史配置，仅供查看</b><p>这份计划不能继续修改、检查或提交。若要继续投放，请新建计划并选择目标广告平台。</p></div></div>
      </section> : <>
        {showConfiguration && platformConfiguration ? <section className="delivery-config-config-card"><header><div><span>当前计划 V{selectedPlan.currentVersionNumber}</span><h3>{selectedPlan.currentVersion.name}</h3><p>更新于 {formatTime(selectedPlan.updatedAt)}</p></div><div className="delivery-config-contract"><b>配置已就绪</b><span>内容随当前计划版本锁定</span></div></header><PlatformConfigurationDetails value={platformConfiguration}/></section> : null}
        {showPreflight ? <section className="delivery-config-flow-grid delivery-config-flow-grid--preflight"><article className="delivery-config-preflight-card">
          <header><div><span className="section-label">提交前检查</span><h3>检查并提交变更申请</h3></div><strong className="delivery-config-preflight-state">{changeSet ? `变更申请 ${changeSet.status}` : '尚未提交'}</strong></header>
          <div className="delivery-config-preflight-summary"><b>检查结果</b><p>{preflightMessage}</p></div>
          <div className="delivery-config-actions delivery-config-preflight-actions"><button onClick={() => void preflightPlan()} disabled={busy}><ShieldCheck size={14}/>检查当前计划</button><button onClick={() => void createAndPreflightChangeSet()} disabled={busy || !canSubmit}><Check size={14}/>提交变更申请</button>{approvalURL && changeSet?.status !== 'draft' ? <a className="primary-button" href={approvalURL}>查看审批记录<ArrowRight size={14}/></a> : null}</div>
        </article></section> : null}
      </>}
      {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
    </div>
  </StateBoundary>
}
