import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, CircleCheck, Clock, Database, RefreshCw, ShieldCheck, Wrench } from 'lucide-react'
import { useProject } from '../context/ProjectContext'
import {
  api,
  type ApiDataQualityDispositionState,
  type ApiDataQualityIssue,
  type ApiDataQualityIssueKind,
  type ApiDataQualityIssueState,
  type ApiDataQualityReport,
  type ApiDataQualitySeverity,
  type ApiDataSourceStatus,
  type ApiQualityStatus,
  type ApiSourceHealth,
} from '../data/api'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'

/**
 * 数据质量（10-ad-data-connectors.md §10/§11，导航见 19 §5.2）。
 *
 * 这个模块存在的理由只有一条：阻止错误数据被拿去当依据。所以它不是一个仪表盘，
 * 是一个队列——20 §4.1 明确要求问题队列为主，影响范围和修复状态为辅，错误与
 * 延迟置顶。
 *
 * 六个视图确实查的是不同的东西（22 §8.3）：前五个按问题类型切，看的是「这一类
 * 出了什么事」，包括已经处置掉的；第六个「修复队列」跨类型，只留还需要人管的
 * ——同一批数据，两种问法。
 *
 * 六个视图共用一次请求。它们共用同一套严重度排序和队列判断，拆成六个请求会让
 * 「队列里还有几条」在不同视图里算出不同的数。
 *
 * 问题本身不入库，每次实时算出；界面上的状态是后端把实时检测结果和处置记录比对
 * 的结论。其中「报修后又出现」意味着有人标了已修复但问题还在——这条必须显眼，
 * 否则点一次「已修复」就能让问题永久消失。
 */
type ViewTarget = ApiDataQualityIssueKind | 'queue'

const viewTargets: Record<string, ViewTarget> = {
  新鲜度: 'freshness',
  缺失: 'missing',
  异常: 'anomaly',
  口径: 'caliber',
  对账: 'reconciliation',
  修复队列: 'queue',
}

const rangeOptions = [
  { label: '近 7 天', days: 7 },
  { label: '近 30 天', days: 30 },
  { label: '近 90 天', days: 90 },
]

const kindLabels: Record<ApiDataQualityIssueKind, string> = {
  freshness: '新鲜度',
  missing: '缺失',
  anomaly: '异常',
  caliber: '口径',
  reconciliation: '对账',
}

const severityLabels: Record<ApiDataQualitySeverity, string> = {
  blocking: '阻断',
  warning: '警告',
  info: '提示',
}

// 每一档说清「它对结论意味着什么」，而不是只给一个颜色。
const severityMeaning: Record<ApiDataQualitySeverity, string> = {
  blocking: '在修好之前，这批数据不能用来下结论，也不会触发任何自动优化动作。',
  warning: '结论还能给，但必须带着这条一起看，不能单独引用那几个数字。',
  info: '只是记一笔，不影响下面的数字怎么读。',
}

const stateLabels: Record<ApiDataQualityIssueState, string> = {
  open: '待处理',
  acknowledged: '跟进中',
  resolved: '已修复',
  ignored: '已忽略',
  reopened: '报修后又出现',
}

const dispositionLabels: Record<ApiDataQualityDispositionState, string> = {
  acknowledged: '跟进中',
  resolved: '已修复',
  ignored: '已忽略',
}

// 三种处置的语义差别很大，写在选项旁边，避免有人拿「已忽略」当「已修复」用。
const dispositionMeaning: Record<ApiDataQualityDispositionState, string> = {
  acknowledged: '我知道了，有人在跟。问题继续留在队列里，只是标了责任人。',
  resolved: '已经修好了。下次检测如果还检出，会自动弹回「报修后又出现」。',
  ignored: '判定这条不用管。它从队列里下去，再次检出也不会弹回来。',
}

const qualityStatusLabels: Record<ApiQualityStatus, string> = {
  healthy: '正常',
  delayed: '延迟',
  partial: '不完整',
  mapping_incomplete: '映射未完成',
  tracking_broken: '追踪异常',
  reconciling: '对账中',
  blocked: '已阻断',
}

// 数据源的**生命周期**状态。和上面那张 qualityStatusLabels 不是一回事：这一张说的是
// 「这条通路开着没有」，那一张说的是「收进来的数对不对」。措辞和「数据接入」页保持一致，
// 免得同一个源在两页里叫两个名字。
const sourceStatusLabels: Record<ApiDataSourceStatus, string> = {
  draft: '未启用',
  active: '同步中',
  paused: '已暂停',
  revoked: '已撤销授权',
}

const headings: Record<ViewTarget, { title: string; blurb: string }> = {
  freshness: {
    title: '数据够不够新',
    blurb: '任何洞察都必须说清数据截止到哪天。滞后的数据不会变成错的数据，但会让「最近效果下滑」这类判断变成幻觉。',
  },
  missing: {
    title: '该有的数据是不是都在',
    blurb: '窗口里缺了几天，直接按窗口求和就会低估花费。导入被拒的行也在这里——那些数据根本没进来。',
  },
  anomaly: {
    title: '数据本身对不对劲',
    blurb: '点击数超过曝光数这种物理上不成立的值，几乎都是字段映射接错了列。它不会让指标变成空，只会变成一个荒谬但不为零的数。',
  },
  caliber: {
    title: '两个数字并排之前必须一致的东西',
    blurb: '币种、归因窗口、指标口径版本。这三样只要在一个窗口里混了，加出来的总额和比出来的高低就都不作数。',
  },
  reconciliation: {
    title: '花的钱能不能归到素材上',
    blurb: '归不上的钱照样是花掉的钱，会计入总盘，但撑不起「这条创意更好」这类结论。',
  },
  queue: {
    title: '还需要人管的问题',
    blurb: '跨类型的队列，只留待处理、跟进中和报修后又出现的。已修复且没再出现的、判定可忽略的都已经下队列。',
  },
}

const emptyHints: Record<ViewTarget, string> = {
  // 「没有滞后」不能一句话说死：底表里可能正躺着一个落后 2 天的源。这一句要把
  // 「为什么它不算问题」一并说了，否则同一屏上下两句互相打脸。
  freshness: '没有需要处理的新鲜度问题。落后两天以内算正常波动，不进这个队列；'
    + '已暂停和已撤销的源不判滞后，但它们如果在窗口中途停掉，会另报一条覆盖不全。',
  missing: '窗口内没有发现日期空洞，也没有导入被拒的行。',
  anomaly: '没有数据源被标为异常，也没有检出物理上不成立的数值。',
  caliber: '这个窗口里的数据口径一致，可以直接相加和比较。',
  reconciliation: '这个窗口里的花费都归到了具体素材上，没有待认领的平台对象。',
  queue: '修复队列是空的。要么本来就没问题，要么都已经处置完了——切到左边的类型视图能看到处置记录。',
}

export function DataQualityPage({ state, activeView }: { state: DataState; activeView: string }) {
  const { currentProject } = useProject()
  const target = viewTargets[activeView] ?? 'queue'
  const [rangeLabel, setRangeLabel] = useState('近 30 天')
  const [report, setReport] = useState<ApiDataQualityReport | null>(null)
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const days = rangeOptions.find(option => option.label === rangeLabel)?.days ?? 30
      const end = new Date()
      const start = new Date(end.valueOf() - days * 24 * 60 * 60 * 1000)
      // 后端只收 2006-01-02，带时分秒会被判成参数无效。
      const next = await api.getDataQuality(currentProject.id, isoDate(start), isoDate(end))
      setReport(next)
      setListState('ready')
    } catch (cause) {
      setReport(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '数据质量读取失败。')
    }
  }, [currentProject.id, rangeLabel])

  useEffect(() => { void load() }, [load])

  // 切视图时把上一条处置回执清掉：那句话是对上一个动作的回应，
  // 留在新视图里会被读成「这个视图刚发生了什么」。
  useEffect(() => { setNotice('') }, [target])

  // 后端已经按「在队列里 → 严重度 → 影响花费」排好序，这里只做视图筛选，不重排。
  const issues = useMemo(() => {
    const all = report?.issues ?? []
    return target === 'queue' ? all.filter(issue => inQueue(issue.state)) : all.filter(issue => issue.kind === target)
  }, [report, target])

  useEffect(() => {
    const fingerprints = issues.map(issue => issue.fingerprint)
    setSelected(current => fingerprints.includes(current) ? current : fingerprints[0] ?? '')
  }, [issues])

  const current = issues.find(issue => issue.fingerprint === selected)

  const resolve = useCallback(async (issue: ApiDataQualityIssue, next: ApiDataQualityDispositionState, note: string) => {
    setBusy(true)
    setNotice('')
    try {
      await api.resolveQualityIssue(currentProject.id, {
        fingerprint: issue.fingerprint,
        issue_kind: issue.kind,
        state: next,
        note,
        // 回传界面上这条问题的观测时间，而不是当前时间：处置的是你看到的那个版本，
        // 中间问题若又恶化，不会被这次处置一并盖掉。
        observed_through: issue.last_observed_at,
      })
      setNotice(`已记为「${dispositionLabels[next]}」。`)
      await load()
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '处置写入失败。')
    } finally {
      setBusy(false)
    }
  }, [currentProject.id, load])

  return <StateBoundary state={state} onRetry={() => { void load() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">DATA QUALITY</span>
            <h2>{headings[target].title}</h2>
            <p>当前 Project：{currentProject.name}。{headings[target].blurb}</p>
          </div>
          <div className="core-flow-actions">
            <label>窗口<select aria-label="数据窗口" value={rangeLabel} onChange={event => setRangeLabel(event.target.value)}>
              {rangeOptions.map(option => <option key={option.label}>{option.label}</option>)}
            </select></label>
            <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void load() }}>
              <RefreshCw size={15}/>刷新
            </button>
          </div>
        </div>

        {/* 能不能下结论是这一页最该先说的事，比任何一条具体问题都靠前（PRD §10.3）。 */}
        {report && !report.strong_conclusions_allowed ? <div className="feature-stack">
          <span>这个窗口的数据现在不能用来下结论</span>
          <b>{report.blocked_reason || '存在阻断级问题。'}</b>
          <b>投前洞察和投后分析都不应据此给出能归因的结论，也不应触发自动优化动作。先把上面这些修掉。</b>
        </div> : null}

        <div className="prelaunch-filterbar">
          <span>{issues.length} 条{target === 'queue' ? '待处理' : `「${kindLabels[target]}」`}</span>
          {report ? <span>
            全项目队列 {report.queue_count} 条 · 其中未处理 {report.open_count} 条 ·
            窗口 {formatDate(report.window.start)} ~ {formatDate(report.window.end)}
          </span> : null}
        </div>

        <div className="prelaunch-table" role="list" aria-label={headings[target].title}>
          <div className="prelaunch-row insight-issue-row header">
            <span>问题</span><span>严重度</span><span>影响范围</span><span>状态</span>
          </div>
          {listState === 'loading' ? <div className="panel-empty">正在检查当前 Project 的数据质量…</div> : null}
          {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}
          {listState === 'ready' && !issues.length ? <div className="panel-empty">{emptyHints[target]}</div> : null}

          {issues.map(issue =>
            <button role="listitem" key={issue.fingerprint}
              className={`prelaunch-row insight-issue-row ${issue.severity}${issue.fingerprint === selected ? ' active' : ''}`}
              onClick={() => setSelected(issue.fingerprint)}>
              <span><b>{issue.title}</b><small>{kindLabels[issue.kind]} · {issue.scope_label}</small></span>
              <span>{severityLabels[issue.severity]}</span>
              <span>{describeScope(issue)}</span>
              <span>
                {issue.severity === 'blocking' || issue.state === 'reopened' ? <CircleAlert size={14}/> : <CircleCheck size={14}/>}
                {stateLabels[issue.state]}
              </span>
            </button>)}
        </div>

        {/* 新鲜度视图额外给出底表：哪个源截止到哪天，是所有滞后判断的依据（doc10 §11）。
            这张表以前只写「滞后 N 天」，于是一个滞后 2 天（还在容忍范围内）或者已经暂停
            （本来就不判滞后）的源，会顶着一个天数躺在这里，而上面的队列一条问题都没有
            ——同一屏两句相反的话。现在每一行都说清自己算不算数。 */}
        {target === 'freshness' && report?.sources?.length ? <div className="feature-stack">
          <span>数据源截止到哪天（{report.sources.length} 个）</span>
          {report.sources.map(source => <b key={source.data_source_id}>
            {/* 这两段必须连在一行里写。跨行时 JSX 会把表达式和下一行文字之间的换行
                连同缩进一起吃掉，屏幕上出现「接入未启用· 数据正常」——分隔符贴在前一个
                词屁股上，读起来像少打了一个字。 */}
            {source.label} · 接入{sourceStatusLabels[source.status] ?? source.status} · 数据{qualityStatusLabels[source.quality_status]}
            {source.data_through ? ` · 数据到 ${formatDate(source.data_through)}` : ' · 还没有任何数据'}
            {source.freshness_days > 0 ? ` · 落后今天 ${source.freshness_days} 天` : ''}
            {source.quality_note ? ` · ${source.quality_note}` : ''}
            <small>{freshnessVerdict(source)}</small>
          </b>)}
          <b><small>
            「接入状态」说的是这条通路开着还是关着，「数据状态」说的是收进来的数东西对不对
            ——两个都叫状态，但一个是通路的事，一个是数的事。
          </small></b>
        </div> : null}
      </section>

      <aside className="prelaunch-detail">
        {current
          ? <IssueDetail issue={current} busy={busy} onResolve={resolve}/>
          : <div className="panel-empty">左侧选一条问题，查看它影响了什么、下一步该查什么。</div>}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

function IssueDetail({ issue, busy, onResolve }: {
  issue: ApiDataQualityIssue
  busy: boolean
  onResolve: (issue: ApiDataQualityIssue, next: ApiDataQualityDispositionState, note: string) => Promise<void>
}) {
  const [next, setNext] = useState<ApiDataQualityDispositionState>('acknowledged')
  const [note, setNote] = useState('')

  // 换一条问题就把表单清空：把上一条的处置理由留在框里，很容易被顺手提交到这一条上。
  useEffect(() => {
    setNext('acknowledged')
    setNote('')
  }, [issue.fingerprint])

  return <>
    <span className="section-label">{kindLabels[issue.kind]} · {severityLabels[issue.severity]}</span>
    <h3>{issue.title}</h3>
    <p>{issue.scope_label} · 最近一次观测 {formatTime(issue.last_observed_at)}</p>

    <div className="prelaunch-fact"><Database size={17}/><span><small>具体是什么情况</small><b>{issue.detail}</b></span></div>
    <div className="prelaunch-fact"><Wrench size={17}/><span><small>下一步查什么</small><b>{issue.suggestion}</b></span></div>
    <div className="prelaunch-fact"><ShieldCheck size={17}/><span><small>这条对结论意味着什么</small><b>
      {severityMeaning[issue.severity]}
    </b></span></div>
    {issue.affected_spend_cents > 0 || issue.affected_days ? <div className="prelaunch-fact">
      <Clock size={17}/><span><small>影响范围</small><b>{describeScope(issue)}</b></span>
    </div> : null}

    {issue.state === 'reopened' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>报修后又出现</small>
      有人在 {issue.disposition ? formatTime(issue.disposition.updated_at) : '之前'} 把它标成了「已修复」，
      但在那之后这条问题又被观测到了。要么当时没真修好，要么修好后又坏了一次。
    </span></div> : null}

    {issue.disposition ? <div className="feature-stack">
      <span>处置记录（第 {issue.disposition.version} 次）</span>
      <b>{dispositionLabels[issue.disposition.state]} · {issue.disposition.decided_by} · {formatTime(issue.disposition.updated_at)}</b>
      <b>{issue.disposition.note}</b>
    </div> : null}

    <label className="experience-reason">
      <span>怎么处置</span>
      <select aria-label="处置" value={next} onChange={event => setNext(event.target.value as ApiDataQualityDispositionState)}>
        {(Object.keys(dispositionLabels) as ApiDataQualityDispositionState[]).map(value =>
          <option key={value} value={value}>{dispositionLabels[value]}</option>)}
      </select>
    </label>
    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>{dispositionLabels[next]}</small>{dispositionMeaning[next]}
    </span></div>
    <label className="experience-reason">
      <span>依据（必填）</span>
      <textarea rows={3} value={note} onChange={event => setNote(event.target.value)}
        placeholder="谁在跟、怎么修的、或者为什么可以不管。后面有人看到这个数字时要能回溯它凭什么算数。"/>
    </label>
    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={busy || !note.trim()}
        onClick={() => { void onResolve(issue, next, note.trim()) }}>
        {busy ? '处理中…' : `记为「${dispositionLabels[next]}」`}
      </button>
    </div>
    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>处置不会改数据</small>
      这里记的只是「人做了什么判断」。问题本身每次都从数据实时算出——真修好了它自然消失，
      没修好的话标什么都还会再出现。
    </span></div>
  </>
}

// state 的判断口径要和后端 QualityIssueState.InQueue() 一致，否则同一批问题
// 在「修复队列」里数出来的条数会和角标对不上。
function inQueue(state: ApiDataQualityIssueState): boolean {
  return state === 'open' || state === 'acknowledged' || state === 'reopened'
}

/**
 * 底表每一行后面那句话：这个天数算不算问题。
 *
 * 「算不算」由后端给（`freshness_judged`），前端不自己按天数推——阈值是后端的口径，
 * 这边复制一份，改了 Go 忘了改这里，底表就会开始说一件和队列相反的事。这里只负责
 * 把「为什么不算」讲清楚，而理由分三种：没启用、还没有数据、在容忍范围内。
 */
function freshnessVerdict(source: ApiSourceHealth): string {
  if (source.freshness_judged) return '已经进了上面的待处理队列。'
  if (source.status !== 'active') {
    return `这个源${sourceStatusLabels[source.status]}，不判滞后——停着的通路本来就不会有新数据。`
      + '它如果是在窗口中途停的，会另报一条「覆盖不全」。'
  }
  if (!source.data_through) return '还没有回过任何数据，已经另报了一条。'
  return '落后在容忍范围内，不算问题：平台侧报表本身就有一两天延迟。'
}

function describeScope(issue: ApiDataQualityIssue): string {
  const parts: string[] = []
  if (issue.affected_spend_cents > 0) parts.push(`${formatMoney(issue.affected_spend_cents)} 花费`)
  if (issue.affected_days) parts.push(`${issue.affected_days} 天`)
  if (issue.stat_date) parts.push(issue.stat_date)
  return parts.length ? parts.join(' · ') : issue.scope_label
}

function formatMoney(cents: number): string {
  return `¥${(cents / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function isoDate(value: Date): string {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`
}

function formatDate(value: string): string {
  const time = new Date(value)
  return Number.isNaN(time.valueOf()) ? value : time.toLocaleDateString('zh-CN')
}

function formatTime(value: string): string {
  const time = new Date(value)
  return Number.isNaN(time.valueOf()) ? value : time.toLocaleString('zh-CN', { hour12: false })
}
