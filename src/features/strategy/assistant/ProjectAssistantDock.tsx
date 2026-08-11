import Bot from 'lucide-react/dist/esm/icons/bot.js'
import Check from 'lucide-react/dist/esm/icons/check.js'
import LoaderCircle from 'lucide-react/dist/esm/icons/loader-circle.js'
import GripVertical from 'lucide-react/dist/esm/icons/grip-vertical.js'
import Maximize2 from 'lucide-react/dist/esm/icons/maximize-2.js'
import Minimize2 from 'lucide-react/dist/esm/icons/minimize-2.js'
import Pencil from 'lucide-react/dist/esm/icons/pencil.js'
import Send from 'lucide-react/dist/esm/icons/send.js'
import ShieldAlert from 'lucide-react/dist/esm/icons/shield-alert.js'
import X from 'lucide-react/dist/esm/icons/x.js'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { buildConversationLens } from '../strategyConversationModel'
import type { ArtifactProposal, BriefDraft, BriefPatchOperation, Message, ProjectContextManifest } from '../types'
import { strategyStageLabel } from '../workspace/StageRail'
import type { StrategyStage } from '../workspace/workspaceRoute'

export function ProjectAssistantDock({
  contextLabels,
  contextError,
  brief,
  disabled,
  excludedSourceIds,
  expanded,
  manifest,
  messages,
  onClose,
  onExpandedChange,
  onSend,
  onToggleSource,
  onResizeStart,
  onApplyProposal,
  onIgnoreProposal,
  pending,
  proposalBusyId,
  proposalError,
  proposals,
  stage,
}: {
  contextLabels: string[]
  contextError: string
  brief: BriefDraft | null
  disabled: boolean
  excludedSourceIds: string[]
  expanded: boolean
  manifest: ProjectContextManifest | null
  messages: Message[]
  onClose: () => void
  onExpandedChange: (expanded: boolean) => void
  onSend: (content: string) => Promise<boolean>
  onToggleSource: (sourceId: string) => void
  onResizeStart: (clientX: number) => void
  onApplyProposal: (proposal: ArtifactProposal, operations?: BriefPatchOperation[]) => Promise<boolean>
  onIgnoreProposal: (proposal: ArtifactProposal) => Promise<boolean>
  pending: boolean
  proposalBusyId: string
  proposalError: string
  proposals: ArtifactProposal[]
  stage: StrategyStage
}) {
  const [content, setContent] = useState('')
  const [feedback, setFeedback] = useState('')
  const recentMessages = messages.filter(message => message.role !== 'system_event').slice(-4)
  const excludedSources = useMemo(() => new Set(excludedSourceIds), [excludedSourceIds])
  const starterRequests = useMemo(() => assistantStarterRequests(brief, stage), [brief, stage])
  const understanding = useMemo(() => buildConversationLens(brief, []), [brief])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const next = content.trim()
    if (!next || pending || disabled) return
    setFeedback('')
    const accepted = await onSend(next)
    if (accepted) setContent('')
    else setFeedback('消息未发送，请查看工作区错误提示后重试。')
  }

  return <aside className="project-assistant-dock" aria-label="项目 AI 助手">
    <button
      type="button"
      className="project-assistant-resizer"
      aria-label="调整 AI 助手宽度"
      onPointerDown={event => {
        event.currentTarget.setPointerCapture?.(event.pointerId)
        onResizeStart(event.clientX)
      }}
    ><GripVertical size={14}/></button>
    <header>
      <span className="project-assistant-mark"><Bot size={17}/></span>
      <div><b>项目 AI 助手</b><small>正在使用同一工作区上下文 · {strategyStageLabel(stage)}</small></div>
      <div className="project-assistant-header-actions">
        <button
          type="button"
          aria-label={expanded ? '退出 AI 助手沉浸模式' : '沉浸展开 AI 助手'}
          aria-pressed={expanded}
          onClick={() => onExpandedChange(!expanded)}
        >{expanded ? <Minimize2 size={15}/> : <Maximize2 size={15}/>}</button>
        <button type="button" aria-label="收起 AI 助手" onClick={onClose}><X size={16}/></button>
      </div>
    </header>
    <div className="project-assistant-context" aria-label="当前助手上下文">
      {contextLabels.map(label => <span key={label}>{label}</span>)}
      <section className="project-assistant-understanding" aria-label="AI 当前理解">
        <header>
          <b>AI 当前理解</b>
          <small>{understanding.completedCore}/{understanding.totalCore} 项核心信息</small>
        </header>
        <div>{understanding.items.map(item => <article data-captured={Boolean(item.value)} key={item.key}>
          <span>{item.value ? <Check size={11}/> : null}</span>
          <div><small>{item.label}{item.required ? '' : ' · 可选'}</small><p>{item.value || '待确认'}</p></div>
        </article>)}</div>
      </section>
      {manifest ? <details>
        <summary>结构化上下文</summary>
        <dl>
          <div><dt>项目上下文</dt><dd>v{manifest.project_context_ref.version}</dd></div>
          <div><dt>工作区</dt><dd>v{manifest.workspace_ref.version}</dd></div>
          <div><dt>Brief</dt><dd>{manifest.brief_ref ? `v${manifest.brief_ref.version ?? 1}` : '未创建'}</dd></div>
          <div><dt>策略</dt><dd>{manifest.strategy_ref ? `r${manifest.strategy_ref.version ?? 1}` : '未生成'}</dd></div>
          <div><dt>下轮使用来源</dt><dd>{manifest.selected_source_refs.length - excludedSources.size}/{manifest.selected_source_refs.length} 项</dd></div>
          <div><dt>记忆</dt><dd>v{manifest.memory_version}</dd></div>
        </dl>
        {manifest.selected_source_refs.length ? <div className="project-assistant-context-sources" aria-label="下一轮 AI 来源上下文">
          <p>来源只针对下一轮调用；取消后不会删除资料或改变 Brief。</p>
          <ul>{manifest.selected_source_refs.map(ref => {
            const excluded = excludedSources.has(ref.id)
            return <li data-excluded={excluded} key={`${ref.type}:${ref.id}`}>
              <div><b>{assistantSourceLabel(ref.type)}</b><small>{ref.id}</small></div>
              <button
                type="button"
                aria-label={`${excluded ? '恢复' : '下一轮不使用'}来源 ${ref.id}`}
                aria-pressed={excluded}
                onClick={() => onToggleSource(ref.id)}
              >{excluded ? <><Check size={12}/>恢复</> : <><X size={12}/>下轮不使用</>}</button>
            </li>
          })}</ul>
        </div> : null}
        <small>每次 AI 请求会冻结这份引用快照；摘要不能覆盖 Brief 或策略事实。</small>
      </details> : <small className="project-assistant-context-loading">{contextError || '正在核对结构化上下文…'}</small>}
    </div>
    <div className="project-assistant-messages" aria-live="polite">
      {recentMessages.length ? recentMessages.map(message => <article key={message.id} data-role={message.role}>
        <small>{message.role === 'assistant' ? 'AI' : '你'}</small>
        <p>{message.content}</p>
      </article>) : <div className="project-assistant-empty">
        <Bot size={22}/><b>上下文已连接</b><p>你可以在任意阶段补充信息，消息会回到当前项目的同一条策略对话。</p>
      </div>}
      {!pending && !disabled ? <section className="project-assistant-starters" aria-label="基于当前上下文的 AI 建议入口">
        <header><span>不知道怎么写？</span><small>选择一个方向，AI 会结合当前项目给出 2—3 个候选，不会自动写入。</small></header>
        <div>{starterRequests.map(request => <button
          key={request.label}
          type="button"
          onClick={() => setContent(request.prompt)}
        ><b>{request.label}</b><small>{request.detail}</small></button>)}</div>
      </section> : null}
      <AssistantProposalList
        brief={brief}
        busyId={proposalBusyId}
        disabled={disabled}
        error={proposalError}
        onApply={onApplyProposal}
        onIgnore={onIgnoreProposal}
        proposals={proposals}
      />
      {pending ? <div className="project-assistant-pending" role="status">
        <LoaderCircle className="spin" size={15}/><span><b>AI 正在处理</b><small>可以收起面板或切换阶段，后台任务不会取消。</small></span>
      </div> : null}
    </div>
    <form onSubmit={event => { void submit(event) }}>
      <label htmlFor="project-assistant-input">给当前项目补充信息</label>
      <div>
        <textarea
          id="project-assistant-input"
          rows={3}
          value={content}
          disabled={disabled}
          placeholder={disabled ? '当前工作区只读' : '补充约束、询问建议，或要求 AI 检查当前阶段…'}
          onChange={event => setContent(event.target.value)}
          onKeyDown={event => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              event.currentTarget.form?.requestSubmit()
            }
          }}
        />
        <button type="submit" aria-label="发送给项目 AI 助手" disabled={!content.trim() || pending || disabled}><Send size={16}/></button>
      </div>
      {feedback ? <p className="project-assistant-feedback" role="alert">{feedback}</p> : <small>Enter 发送 · Shift + Enter 换行</small>}
    </form>
  </aside>
}

function assistantSourceLabel(type: string) {
  return type === 'knowledge_research_artifact' ? '研究证据' : '项目资料'
}

const assistantFieldLabels: Record<string, string> = {
  'product.name': '主推产品',
  'campaign.objective': '推广目标',
  'audience.primary': '核心受众',
  proposition: '核心主张',
  channels: '渠道组合',
  'measurement.primary_kpi': '核心指标',
}

function assistantStarterRequests(brief: BriefDraft | null, stage: StrategyStage) {
  const blockerField = brief?.completeness.blockers[0]?.field ?? ''
  const blockerLabel = assistantFieldLabels[blockerField] ?? '当前最高优先级缺口'
  const stageLabel = strategyStageLabel(stage)
  return [
    {
      label: `补充${blockerLabel}`,
      detail: '给出差异明确、可直接比较的候选',
      prompt: `我不确定${blockerLabel}该怎么写。请只基于当前项目上下文给出 2—3 个差异明确的候选，分别说明适用理由、依据和需要我确认的假设；不要自动修改 Brief。`,
    },
    {
      label: `${stageLabel}第二视角`,
      detail: '从客观角度指出最值得补强的方向',
      prompt: `请站在客观第二视角审视当前${stageLabel}，结合已有 Brief、项目资料和研究结论，给出 2—3 个最值得补强的候选方向，并说明取舍；不要自动修改任何业务对象。`,
    },
    {
      label: '从资料提炼候选',
      detail: '区分事实、推断与待确认假设',
      prompt: '请从当前项目资料和可引用研究中提炼 2—3 个可用于下一步的候选表达，明确区分事实、推断和仍需确认的假设；资料不足时直接说明缺口，不要提供通用模板。',
    },
  ]
}

export function AssistantProposalList({ brief, busyId, disabled, error, onApply, onIgnore, proposals }: {
  brief: BriefDraft | null
  busyId: string
  disabled: boolean
  error: string
  onApply: (proposal: ArtifactProposal, operations?: BriefPatchOperation[]) => Promise<boolean>
  onIgnore: (proposal: ArtifactProposal) => Promise<boolean>
  proposals: ArtifactProposal[]
}) {
  if (!proposals.length && !error) return null
  return <>
    {proposals.length ? <section className="project-assistant-proposals" aria-label="AI 待确认建议">
      <header><span><Pencil size={13}/><b>{proposals.length} 条待确认建议</b></span><small>先核对、可编辑，不会自动修改 Brief</small></header>
      {proposals.map(proposal => <AssistantProposalCard
        key={proposal.id}
        brief={brief}
        busy={busyId === proposal.id}
        disabled={disabled || Boolean(busyId)}
        proposal={proposal}
        onApply={onApply}
        onIgnore={onIgnore}
      />)}
    </section> : null}
    {error ? <p className="project-assistant-proposal-error" role="alert"><ShieldAlert size={14}/>{error}</p> : null}
  </>
}

const proposalFieldLabels: Record<string, string> = {
  'brand.name': '品牌名称',
  'product.name': '产品名称',
  'product.category': '产品品类',
  'product.selling_points': '核心卖点',
  'product.evidence': '产品证据',
  industry: '行业',
  region: '地区',
  language: '语言',
  'campaign.objective': '推广目标',
  'audience.primary': '核心受众',
  proposition: '核心主张',
  channels: '渠道',
  'budget.total': '预算',
  'schedule.window': '时间范围',
  constraints: '约束条件',
  'measurement.primary_kpi': '核心 KPI',
  'creative.tone': '创意语气',
  'creative.mandatory_elements': '必含元素',
  'creative.prohibited_claims': '禁用表述',
}

function AssistantProposalCard({
  brief,
  busy,
  disabled,
  proposal,
  onApply,
  onIgnore,
}: {
  brief: BriefDraft | null
  busy: boolean
  disabled: boolean
  proposal: ArtifactProposal
  onApply: (proposal: ArtifactProposal, operations?: BriefPatchOperation[]) => Promise<boolean>
  onIgnore: (proposal: ArtifactProposal) => Promise<boolean>
}) {
  const [editing, setEditing] = useState(false)
  const [values, setValues] = useState<Record<string, string>>(() => proposalEditorValues(proposal))

  useEffect(() => {
    setValues(proposalEditorValues(proposal))
    setEditing(false)
  }, [proposal])

  const editedOperations = proposal.operations.map(operation => ({
    ...operation,
    value: parseProposalEditorValue(values[operation.field_path] ?? '', operation.value),
  }))
  const canEdit = proposal.operations.every(operation =>
    typeof operation.value === 'string' || Array.isArray(operation.value))

  return <article className="project-assistant-proposal" data-risk={proposal.risk}>
    <header>
      <div><b>补充 Brief · 基于 v{proposal.target_version}</b><small>{proposal.risk === 'high' ? '涉及已确认字段，请重点核对' : '采用后会记录为你的确认修改'}</small></div>
      <span>{proposal.risk === 'high' ? '高风险' : '建议'}</span>
    </header>
    <div className="project-assistant-proposal-fields">
      {proposal.operations.map(operation => <div key={operation.field_path}>
        <b>{proposalFieldLabels[operation.field_path] ?? operation.field_path}</b>
        <small>当前：{proposalCurrentValue(brief, operation.field_path)}</small>
        {editing ? <textarea
          rows={Array.isArray(operation.value) ? Math.min(4, Math.max(2, operation.value.length)) : 2}
          value={values[operation.field_path] ?? ''}
          aria-label={`编辑${proposalFieldLabels[operation.field_path] ?? operation.field_path}`}
          onChange={event => setValues(current => ({ ...current, [operation.field_path]: event.target.value }))}
        /> : <p>{proposalValueText(operation.value)}</p>}
      </div>)}
    </div>
    <p className="project-assistant-proposal-rationale">{proposal.rationale}</p>
    <footer>
      <button type="button" disabled={disabled} onClick={() => void onIgnore(proposal)}>忽略</button>
      {canEdit ? <button type="button" disabled={disabled} onClick={() => setEditing(value => !value)}>
        <Pencil size={12}/>{editing ? '取消编辑' : '编辑'}
      </button> : null}
      <button className="primary" type="button" disabled={disabled} onClick={() => void onApply(proposal, editing ? editedOperations : undefined)}>
        {busy ? <LoaderCircle className="spin" size={12}/> : <Check size={12}/>} {editing ? '采用编辑稿' : '采用'}
      </button>
    </footer>
  </article>
}

function proposalEditorValues(proposal: ArtifactProposal) {
  return Object.fromEntries(proposal.operations.map(operation => [
    operation.field_path,
    proposalValueText(operation.value, true),
  ]))
}

function proposalValueText(value: unknown, editable = false) {
  if (Array.isArray(value)) return value.map(item => String(item)).join(editable ? '\n' : '、')
  if (typeof value === 'string') return value
  if (value == null) return '未填写'
  return '结构化内容（采用前请在 Brief 中复核）'
}

function parseProposalEditorValue(value: string, original: unknown) {
  if (Array.isArray(original)) return value.split('\n').map(item => item.trim()).filter(Boolean)
  return value.trim()
}

function proposalCurrentValue(brief: BriefDraft | null, fieldPath: string) {
  if (!brief) return '加载中'
  const value = fieldPath.split('.').reduce<unknown>((current, key) => {
    if (!current || typeof current !== 'object') return undefined
    return (current as Record<string, unknown>)[key]
  }, brief.document)
  return proposalValueText(value)
}
