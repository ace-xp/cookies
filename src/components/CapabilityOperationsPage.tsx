import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, CircleCheck, GitBranch, RefreshCw, Ruler, ShieldCheck, Sparkles } from 'lucide-react'
import { useProject } from '../context/ProjectContext'
import {
  api,
  type ApiCaliberFactor,
  type ApiCapabilityOperations,
  type ApiConfidenceLevel,
  type ApiFeatureFieldUsage,
  type ApiFeatureSource,
  type ApiFeatureSystemHealth,
  type ApiFeatureValueKind,
  type ApiMetricDictionaryEntry,
  type ApiSkillEvaluation,
} from '../data/api'
import { admissibleForAttribution, featureSourceLabel } from '../data/featureSource'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'

/**
 * 能力运营（03 §一级导航；20 §4.1「防止特征碎片化和指标口径漂移，支持算法升级与
 * 回归」；22 §6.2 仅分析运营角色可见）。
 *
 * 这不是一个业务工作台（20 §4.1 明确列为非目标）。它服务的是一个人：负责让分析
 * 能力本身保持可用的那个人。所以每个视图回答的都是「我们的判断依据还站不站得住」，
 * 不是「这条素材表现怎么样」。
 *
 * 五个视图共用一次请求。它们算的是同一批素材、特征、数据源和日指标——拆开会让
 * 「特征体系里有多少字段」和「看板上待办几条」在两次读取之间对不上，而这一页的
 * 全部价值就是让人相信这些数字。
 *
 * 这个模块一张表都不建。特征体系读后端的六套 schema，指标字典是后端声明的常量，
 * Skill 和评测集从已有的特征行现算。可编辑的口径是没有意义的口径：它能被人静默
 * 改掉，而 03 §14 要求口径变更后能重算且不静默改写历史结论。
 */
type ViewTarget = 'features' | 'metrics' | 'skills' | 'evaluation' | 'dashboard'

const viewTargets: Record<string, ViewTarget> = {
  特征体系: 'features',
  指标字典: 'metrics',
  '分析 Skills': 'skills',
  评测集: 'evaluation',
  质量看板: 'dashboard',
  // 导航里旧的第五个视图名。03 与 19 都写「质量看板」，这里兼容一下，
  // 免得导航先改完这一页就白屏。
  版本与质量: 'dashboard',
}

const rangeOptions = [
  { label: '近 7 天', days: 7 },
  { label: '近 30 天', days: 30 },
  { label: '近 90 天', days: 90 },
]

const headings: Record<ViewTarget, { title: string; blurb: string }> = {
  features: {
    title: '大家到底在用哪些词描述素材',
    blurb: '同一个意思被写成好几种说法，后面按特征分组的对比就会散成一堆样本量不够的小格子。这里看的是每个字段实际被写成了多少种取值。',
  },
  metrics: {
    title: '每个指标到底怎么算出来的',
    blurb: '同一个「转化率」在两个平台可能算的不是一件事。这里是全系统唯一的口径出处，改它要改代码并过评审，不能在界面上随手改。',
  },
  skills: {
    title: '现在是哪一版在提特征',
    blurb: '历史版本提的特征还留在库里，换版本不会追溯重提。所以「这条特征是哪一版提的」必须能查——不然升级之后没人说得清哪些结论该重算。',
  },
  evaluation: {
    title: '被人看过的地方，机器错了多少',
    blurb: '样本全部来自人工复核记录。这不是独立评测集，回答不了「整体准确率是多少」，只能回答「人看过的那些，机器对了几条」。',
  },
  dashboard: {
    title: '现在有哪些欠账',
    blurb: '词表没发布、取值待归并、口径不一致、样本不够——这四类欠账不修，下面所有模块给出的数字都会带着它们的偏差。',
  },
}

const caliberLabels: Record<ApiCaliberFactor, string> = {
  time_zone: '时区',
  currency: '币种',
  attribution_window: '归因窗口',
  metric_schema_version: '指标口径版本',
}

const confidenceLabels: Record<ApiConfidenceLevel, string> = {
  sufficient: '样本充足',
  directional: '仅供参考',
  low_sample: '样本不足',
  confounded: '存在混淆',
}

const severityLabels: Record<string, string> = {
  blocking: '阻断',
  warning: '警告',
  info: '提示',
}

// 待归并没有对应的待办类型：单次取值可能是同义碎片，也可能就是那条素材独有的，
// 系统分不清，所以它只出现在字段详情里给人看，不进队列充数。
const todoKindLabels: Record<string, string> = {
  vocabulary: '词表未发布',
  off_vocabulary: '词表外存量',
  caliber: '口径不一致',
  evaluation: '评测样本不足',
}

export function CapabilityOperationsPage({ state, activeView }: { state: DataState; activeView: string }) {
  const { currentProject } = useProject()
  const target = viewTargets[activeView] ?? 'dashboard'
  const [rangeLabel, setRangeLabel] = useState('近 30 天')
  const [report, setReport] = useState<ApiCapabilityOperations | null>(null)
  const [selected, setSelected] = useState('')
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const days = rangeOptions.find(option => option.label === rangeLabel)?.days ?? 30
      const end = new Date()
      const start = new Date(end.valueOf() - days * 24 * 60 * 60 * 1000)
      // 后端只收 2006-01-02，带时分秒会被判成参数无效。
      const next = await api.getCapabilityOperations(currentProject.id, isoDate(start), isoDate(end))
      setReport(next)
      setListState('ready')
    } catch (cause) {
      setReport(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '能力运营数据读取失败。')
    }
  }, [currentProject.id, rangeLabel])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setNotice('') }, [target])

  // 特征字段跨六套 schema 拉平：字段 key 在不同素材类型下可以重名但含义不同，
  // 所以行标识必须带上素材类型。
  const fieldRows = useMemo(() => {
    const rows: Array<{ id: string; system: ApiFeatureSystemHealth; field: ApiFeatureFieldUsage }> = []
    for (const system of report?.feature_systems ?? []) {
      for (const field of system.fields) rows.push({ id: `${system.asset_type}::${field.key}`, system, field })
    }
    // 先按用得多的排，再按取值散得厉害的排：没人用的字段散不散都无所谓。
    return rows.sort((left, right) =>
      right.field.asset_count - left.field.asset_count
      || right.field.distinct_values - left.field.distinct_values)
  }, [report])

  const rowIds = useMemo(() => {
    if (target === 'features') return fieldRows.map(row => row.id)
    if (target === 'metrics') return (report?.metrics ?? []).map(metric => metric.key)
    if (target === 'evaluation') return (report?.evaluations ?? []).map(item => `${item.skill_id}@${item.skill_version}`)
    return []
  }, [target, fieldRows, report])

  useEffect(() => {
    setSelected(current => rowIds.includes(current) ? current : rowIds[0] ?? '')
  }, [rowIds])

  const currentField = fieldRows.find(row => row.id === selected)
  const currentMetric = report?.metrics.find(metric => metric.key === selected)
  const currentEvaluation = report?.evaluations.find(item => `${item.skill_id}@${item.skill_version}` === selected)

  return <StateBoundary state={state} onRetry={() => { void load() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">CAPABILITY OPERATIONS</span>
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

        {listState === 'loading' ? <div className="panel-empty">正在核对当前 Project 的分析能力…</div> : null}
        {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}

        {listState === 'ready' && report ? <>
          {target === 'features' ? <FeatureSystemView report={report} rows={fieldRows} selected={selected} onSelect={setSelected}/> : null}
          {target === 'metrics' ? <MetricDictionaryView report={report} selected={selected} onSelect={setSelected}/> : null}
          {target === 'skills' ? <SkillView report={report}/> : null}
          {target === 'evaluation' ? <EvaluationView report={report} selected={selected} onSelect={setSelected}/> : null}
          {target === 'dashboard' ? <DashboardView report={report}/> : null}
        </> : null}
      </section>

      <aside className="prelaunch-detail">
        {target === 'features' && currentField ? <FieldDetail system={currentField.system} field={currentField.field}/> : null}
        {target === 'metrics' && currentMetric && report ? <MetricDetail metric={currentMetric} report={report}/> : null}
        {target === 'evaluation' && currentEvaluation ? <EvaluationDetail evaluation={currentEvaluation}/> : null}
        {target === 'skills' && report ? <SkillAside report={report}/> : null}
        {target === 'dashboard' && report ? <DashboardAside report={report}/> : null}
        {['features', 'metrics', 'evaluation'].includes(target) && !currentField && !currentMetric && !currentEvaluation
          ? <div className="panel-empty">左侧选一行看细节。</div> : null}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

function FeatureSystemView({ report, rows, selected, onSelect }: {
  report: ApiCapabilityOperations
  rows: Array<{ id: string; system: ApiFeatureSystemHealth; field: ApiFeatureFieldUsage }>
  selected: string
  onSelect: (id: string) => void
}) {
  const used = rows.filter(row => row.field.asset_count > 0)
  return <>
    <div className="prelaunch-filterbar">
      <span>{report.feature_systems.length} 类素材 · {report.dashboard.feature_field_total} 个字段</span>
      <span>其中 {report.dashboard.feature_field_used} 个真的有人在填 · {report.dashboard.open_vocabulary_fields} 个枚举字段还没发布词表</span>
    </div>

    <div className="prelaunch-table" role="list" aria-label="特征体系">
      <div className="prelaunch-row insight-issue-row header">
        <span>字段</span><span>词表</span><span>覆盖</span><span>取值散度</span>
      </div>
      {!used.length ? <div className="panel-empty">
        这个项目还没有任何素材填过特征。等内容分析跑过一轮，这里才有东西可看。
      </div> : null}
      {used.map(row => <button role="listitem" key={row.id}
        className={`prelaunch-row insight-issue-row${row.id === selected ? ' active' : ''}${row.field.merge_candidates?.length ? ' warning' : ''}`}
        onClick={() => onSelect(row.id)}>
        <span>
          <b>{row.field.label}</b>
          <small>{row.system.label} · {row.field.group} · {describeSources(row.field.source_counts)}</small>
        </span>
        <span>{vocabularyLabel(row.field)}</span>
        <span>{row.field.asset_count} / {row.system.asset_count} 条素材</span>
        <span>
          {row.field.merge_candidates?.length ? <CircleAlert size={14}/> : <CircleCheck size={14}/>}
          {row.field.distinct_values} 种取值
        </span>
      </button>)}
    </div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>取值怎么数的</small>
      同一条素材上人工结论覆盖机器结论（03 §14），机器的旧值不再计入。否则「机器写 A、人改成 B」
      会被数成两个取值，词表看上去比实际更碎，而碎掉的那一半根本没人在用。
    </span></div>
  </>
}

function FieldDetail({ system, field }: { system: ApiFeatureSystemHealth; field: ApiFeatureFieldUsage }) {
  const top = field.values ?? []
  const head = top[0]
  return <>
    <span className="section-label">{system.label}</span>
    <h3>{field.label}</h3>
    <p>{field.group} · {field.key}{field.unit ? ` · 单位 ${field.unit}` : ''}</p>
    {field.note ? <p>{field.note}</p> : null}

    <div className="prelaunch-fact"><Ruler size={17}/><span><small>覆盖</small><b>
      {system.asset_count} 条{system.label}里有 {field.asset_count} 条填了这个字段，写出了 {field.distinct_values} 种不同的取值。
    </b></span></div>

    {/* 「谁写的」和「填了多少」必须并排看。归因只认量出来的和人标的，一个几乎全是
        模型猜的字段，填得再满也不能拿去分组比较——只看覆盖率看不出这一点。 */}
    <div className="prelaunch-fact"><Sparkles size={17}/><span><small>这些取值是谁写的</small><b>
      {describeSources(field.source_counts)}。
      {admissibleShare(field.source_counts) < 0.5
        ? '一半以上是模型猜的：这个字段现在不适合拿去做驱动因素分组，先让人复核。'
        : '归因只认量出来的和人标的，模型猜的那部分不参与分组比较。'}
    </b></span></div>

    {head ? <div className="prelaunch-fact"><Sparkles size={17}/><span><small>最常见的写法</small><b>
      {head.value}（{head.asset_count} 条）
      {top.length > 1 ? ` · 其余 ${top.length - 1} 种见下` : ''}
    </b></span></div> : null}

    {top.length ? <div className="feature-stack">
      <span>取值分布（按使用量，最多 12 个）</span>
      {top.map(item => <b key={item.value}>{item.value} · {item.asset_count} 条</b>)}
    </div> : null}

    {field.merge_candidates?.length ? <div className="feature-stack">
      <span>只有一条素材用过（{field.merge_candidates.length} 个）</span>
      {field.merge_candidates.map(value => <b key={value}>{value}</b>)}
      <b>
        这些是「候选」，不是结论。它可能是别人的同义写法，也可能就是那条素材独有的。
        系统不替你判断——把两个真正不同的取值合并，比词表碎一点更糟。
      </b>
    </div> : null}

    {!needsVocabulary(field)
      ? <div className="prelaunch-fact"><ShieldCheck size={17}/><span><small>{fieldKindLabels[field.kind] ?? '这类字段'}不需要词表</small><b>
          {field.kind === 'text' || field.kind === 'tags'
            ? '它本来就是自由填的，每条不一样是设计如此，不存在「该收敛没收敛」。'
            : '它的取值是量出来的数，两条素材写 15 和 16 不是两个该合并的说法。'}
        </b></span></div>
      : field.governed
      ? <div className="prelaunch-fact"><ShieldCheck size={17}/><span><small>词表已发布</small><b>
          共 {field.vocabulary?.length ?? 0} 个受控取值，新的提取只能落在表内。
        </b></span></div>
      : <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
          <small>词表还没发布</small>
          这个字段现在接受任何取值（03 §5 末把词表发布权交给能力运营的管理员）。
          在发布之前，它每多用一次就多一分散开的可能。
        </span></div>}

    {field.off_vocabulary?.length ? <div className="feature-stack">
      <span>词表外的存量取值（{field.off_vocabulary.length} 个）</span>
      {field.off_vocabulary.map(value => <b key={value}>{value}</b>)}
      <b>这些是词表发布之前存下来的。它们不会自动消失，也不会自动被纠正。</b>
    </div> : null}
  </>
}

function MetricDictionaryView({ report, selected, onSelect }: {
  report: ApiCapabilityOperations
  selected: string
  onSelect: (key: string) => void
}) {
  // 后端把空列表补成了 []，这里再兜一层：治理面白屏比少显示一行严重得多。
  const conflicts = report.caliber_conflicts ?? []
  return <>
    <div className="prelaunch-filterbar">
      <span>{report.metrics.length} 个指标</span>
      <span>
        {conflicts.length
          ? `${conflicts.length} 处口径不一致 · 受影响的指标已标出`
          : '各数据源口径一致，指标可以直接跨源相加和比较'}
      </span>
    </div>

    {conflicts.length ? <div className="feature-stack">
      <span>数据源之间对不上的口径</span>
      {conflicts.map(conflict => <b key={conflict.factor}>
        {conflict.label}：{conflict.values.join(' / ')} —— {conflict.note}
      </b>)}
    </div> : null}

    <div className="prelaunch-table" role="list" aria-label="指标字典">
      <div className="prelaunch-row insight-issue-row header">
        <span>指标</span><span>类型</span><span>本项目数据</span><span>可比性</span>
      </div>
      {report.metrics.map(metric => <button role="listitem" key={metric.key}
        className={`prelaunch-row insight-issue-row${metric.key === selected ? ' active' : ''}${metric.comparable ? '' : ' warning'}`}
        onClick={() => onSelect(metric.key)}>
        <span><b>{metric.label}</b><small>{metric.key}</small></span>
        <span>{metric.kind === 'fact' ? '事实' : '派生'}</span>
        {/* 派生指标不写「合计」：比率不能相加，而这一页正是在说这件事。
            它是整个窗口的总量除总量，所以叫「窗口值」。 */}
        <span>{metric.day_count
          ? `${metric.day_count} 天 · ${metric.kind === 'fact' ? '合计' : '窗口值'} ${formatMetric(metric)}`
          : '还没有数据'}</span>
        <span>
          {metric.comparable ? <CircleCheck size={14}/> : <CircleAlert size={14}/>}
          {metric.comparable ? '可跨源比较' : '跨源不可比'}
        </span>
      </button>)}
    </div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>为什么这里不能编辑</small>
      口径是全系统唯一的解释出处。能在界面上改的口径等于能被静默改掉，
      而 03 §14 要求口径变更之后必须能重算、且不能悄悄改写已经发出去的历史结论。
      改口径要改代码、走评审、留版本。
    </span></div>
  </>
}

function MetricDetail({ metric, report }: { metric: ApiMetricDictionaryEntry; report: ApiCapabilityOperations }) {
  const conflicts = (report.caliber_conflicts ?? []).filter(conflict => (metric.conflict_notes ?? []).includes(conflict.factor))
  return <>
    <span className="section-label">{metric.kind === 'fact' ? '平台事实' : '派生指标'}</span>
    <h3>{metric.label}</h3>
    <p>{metric.key}{metric.unit ? ` · ${metric.unit}` : ''} · 口径出处：{metric.source}</p>

    <div className="prelaunch-fact"><Ruler size={17}/><span><small>它是什么</small><b>{metric.definition}</b></span></div>
    {metric.formula ? <div className="prelaunch-fact"><Sparkles size={17}/><span><small>怎么算</small><b>{metric.formula}</b></span></div> : null}

    <div className="prelaunch-fact"><CircleCheck size={17}/><span><small>本项目这个窗口</small><b>
      {metric.day_count
        ? `${metric.day_count} 天有数据，${metric.kind === 'fact' ? '合计' : '整窗口算下来是'} ${formatMetric(metric)}`
        : '一天数据都没有。不是「等于 0」，是根本没导进来过。'}
    </b></span></div>

    {metric.kind === 'derived' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>派生指标只能这么算</small>
      用窗口内的总量除总量，不能把每天的比率平均起来。曝光量差别大的两天，
      日均比率和真实比率能差好几倍——而这个差值没有任何业务含义。
    </span></div> : null}

    {metric.caliber_factors?.length ? <div className="feature-stack">
      <span>这个指标的口径取决于</span>
      {metric.caliber_factors.map(factor => <b key={factor}>
        {caliberLabels[factor]}
        {(metric.conflict_notes ?? []).includes(factor) ? ' · 各数据源当前不一致' : ' · 各数据源一致'}
      </b>)}
    </div> : null}

    {conflicts.length ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>现在不能跨源比这个数</small>
      {conflicts.map(conflict => `${conflict.label}：${conflict.values.join(' / ')}`).join('；')}。
      03 §7 要求任何跨渠道对比都必须展示口径差异——在差异消掉之前，把它们并排放在一起看没有意义。
    </span></div> : null}
  </>
}

function SkillView({ report }: { report: ApiCapabilityOperations }) {
  return <>
    <div className="prelaunch-filterbar">
      <span>{report.skills.length} 个 Skill 版本在库里留有特征</span>
      <span>标「在用」的是最近一次提取所用的版本</span>
    </div>

    <div className="prelaunch-table" role="list" aria-label="分析 Skills">
      <div className="prelaunch-row insight-issue-row header">
        <span>Skill 版本</span><span>提取量</span><span>置信分布</span><span>最近一次</span>
      </div>
      {!report.skills.length ? <div className="panel-empty">
        还没有任何 AI 提取记录。内容分析跑过一轮之后，这里会按 Skill 版本列出来。
      </div> : null}
      {report.skills.map(skill => <div role="listitem" key={`${skill.skill_id}@${skill.skill_version}`}
        className={`prelaunch-row insight-issue-row${skill.latest ? ' active' : ''}`}>
        <span>
          <b>{skill.skill_id || '未标注 Skill'} · {skill.skill_version || '未标注版本'}</b>
          <small>{skill.latest ? '在用' : '历史版本'} · {skill.field_keys.length} 个字段</small>
        </span>
        <span>{skill.extraction_count} 条 / {skill.asset_count} 条素材</span>
        <span>高 {skill.high_confidence} · 中 {skill.medium_confidence} · 低 {skill.low_confidence}</span>
        <span>{skill.last_extracted_at ? formatTime(skill.last_extracted_at) : '未记录'}</span>
      </div>)}
    </div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>换版本不会重提历史特征</small>
      旧版本提出来的特征还留在库里，也还在被投前投后拿去分组和对比。
      所以升级 Skill 之后，「哪些结论是旧版本提的特征撑起来的」必须能查——这一页就是查它的地方。
    </span></div>
  </>
}

function SkillAside({ report }: { report: ApiCapabilityOperations }) {
  const latest = report.skills.filter(skill => skill.latest)
  const stale = report.skills.filter(skill => !skill.latest)
  return <>
    <span className="section-label">版本现状</span>
    <h3>{latest.length ? `${latest.length} 个版本在用` : '还没有在用的版本'}</h3>
    <p>库里另有 {stale.length} 个历史版本提的特征。</p>

    <div className="prelaunch-fact"><GitBranch size={17}/><span><small>怎么判「在用」</small><b>
      按最近一次提取时间判，不按版本号排序。v9 和 v10 按字符串比大小会得出 v9 更新的结论。
    </b></span></div>

    {stale.length ? <div className="feature-stack">
      <span>历史版本还留着的特征</span>
      {stale.map(skill => <b key={`${skill.skill_id}@${skill.skill_version}`}>
        {skill.skill_id} · {skill.skill_version} · {skill.extraction_count} 条
        {skill.last_extracted_at ? ` · 最后提取于 ${formatTime(skill.last_extracted_at)}` : ''}
      </b>)}
    </div> : null}

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>AI 自己都没把握的那些提取值得单独看</small>
      AI 提取时把握不大的特征不会被自动丢弃，它们照样进了特征库，也照样参与分组。
      要么让人复核掉，要么在结论里带上样本量说明。
    </span></div>
  </>
}

function EvaluationView({ report, selected, onSelect }: {
  report: ApiCapabilityOperations
  selected: string
  onSelect: (id: string) => void
}) {
  return <>
    <div className="prelaunch-filterbar">
      <span>{report.evaluations.length} 个 Skill 版本有复核样本</span>
      <span>累计 {report.dashboard.evaluation_samples} 条人机对照</span>
    </div>

    <div className="prelaunch-table" role="list" aria-label="评测集">
      <div className="prelaunch-row insight-issue-row header">
        <span>Skill 版本</span><span>样本</span><span>一致 / 不一致</span><span>结论</span>
      </div>
      {!report.evaluations.length ? <div className="panel-empty">
        还没有人复核过任何 AI 提取的特征。在有人看过之前，这里给不出任何准确率——
        没人看过不等于机器做对了。
      </div> : null}
      {report.evaluations.map(item => {
        const id = `${item.skill_id}@${item.skill_version}`
        return <button role="listitem" key={id}
          className={`prelaunch-row insight-issue-row${id === selected ? ' active' : ''}${item.confidence === 'low_sample' ? ' warning' : ''}`}
          onClick={() => onSelect(id)}>
          <span><b>{item.skill_id || '未标注 Skill'} · {item.skill_version || '未标注版本'}</b>
            <small>{confidenceLabels[item.confidence]}</small></span>
          <span>{item.reviewed} 条</span>
          <span>{item.agreed} / {item.disagreed}</span>
          <span>{item.confidence === 'low_sample'
            ? <>{<CircleAlert size={14}/>}样本不够，不给数</>
            : <>{<CircleCheck size={14}/>}{formatPercent(item.accuracy)}</>}</span>
        </button>
      })}
    </div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>这不是独立评测集</small>
      样本全部来自人工复核记录，回答的是「被人看过的地方机器错了多少」。
      没人复核过的提取一条都不算——把它们算成「机器对了」，准确率会随提取量自动上涨，
      而那个上涨和模型好不好毫无关系。
    </span></div>
  </>
}

function EvaluationDetail({ evaluation }: { evaluation: ApiSkillEvaluation }) {
  return <>
    <span className="section-label">{confidenceLabels[evaluation.confidence]}</span>
    <h3>{evaluation.skill_id || '未标注 Skill'} · {evaluation.skill_version || '未标注版本'}</h3>
    <p>{evaluation.reviewed} 条人机对照 · 一致 {evaluation.agreed} 条 · 不一致 {evaluation.disagreed} 条</p>

    <div className="prelaunch-fact"><ShieldCheck size={17}/><span><small>能不能给准确率</small><b>
      {evaluation.confidence === 'low_sample' ? evaluation.note : `${formatPercent(evaluation.accuracy)}。${evaluation.note}`}
    </b></span></div>

    <div className="prelaunch-fact"><CircleAlert size={17}/><span><small>怎么判一致</small><b>
      按取值文本比对，不看复核状态。人工行的复核状态默认就是「已确认」，
      写一个完全不同的值也还是「已确认」——拿它当「认可了机器」会把改写数成一致。
    </b></span></div>

    {evaluation.fields?.length ? <div className="feature-stack">
      <span>分字段看（一致 / 样本）</span>
      {evaluation.fields.map(field => <b key={field.key}>
        {field.label} · {field.agreed} / {field.reviewed}
      </b>)}
    </div> : null}

    {evaluation.examples?.length ? <div className="feature-stack">
      <span>机器和人写得不一样的例子（{evaluation.examples.length} 条）</span>
      {evaluation.examples.map((example, index) => <b key={`${example.asset_id}-${example.feature_key}-${index}`}>
        {example.asset_title || example.asset_id} · {example.label}：机器「{example.ai_value}」→ 人「{example.human_value}」
      </b>)}
    </div> : <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>没有可看的例子</small>
      看不到具体例子的准确率改不了 Skill——知道错了 25% 而不知道错在哪，等于什么都不知道。
    </span></div>}
  </>
}

function DashboardView({ report }: { report: ApiCapabilityOperations }) {
  const todos = report.dashboard.todos
  return <>
    <div className="prelaunch-filterbar">
      <span>{todos.length} 条欠账</span>
      <span>窗口 {formatDate(report.window.start)} ~ {formatDate(report.window.end)} · 生成于 {formatTime(report.generated_at)}</span>
    </div>

    <div className="prelaunch-table" role="list" aria-label="质量看板">
      <div className="prelaunch-row insight-issue-row header">
        <span>欠账</span><span>类型</span><span>影响</span><span>严重度</span>
      </div>
      {!todos.length ? <div className="panel-empty">
        目前没有欠账：词表都发布了，没有待归并的取值，数据源口径一致，评测样本也够。
      </div> : null}
      {todos.map((todo, index) => <div role="listitem" key={`${todo.kind}-${todo.feature_key ?? index}`}
        className={`prelaunch-row insight-issue-row ${todo.severity}`}>
        <span><b>{todo.title}</b><small>{todo.detail}</small></span>
        <span>{todoKindLabels[todo.kind] ?? todo.kind}</span>
        <span>{todo.asset_type ? todo.asset_type : '全项目'}</span>
        <span>{severityLabels[todo.severity] ?? todo.severity}</span>
      </div>)}
    </div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>词表待办只列已经有人用的字段</small>
      六套 schema 里没发布词表的字段不少，但没人填过的字段散不散没有影响。
      把它们都列进来，真正在散的那几个会被埋掉。
    </span></div>
  </>
}

function DashboardAside({ report }: { report: ApiCapabilityOperations }) {
  const dashboard = report.dashboard
  return <>
    <span className="section-label">能力现状</span>
    <h3>{dashboard.feature_field_used} / {dashboard.feature_field_total} 个特征字段在用</h3>
    <p>窗口 {formatDate(report.window.start)} ~ {formatDate(report.window.end)}。</p>

    <div className="prelaunch-fact"><Ruler size={17}/><span><small>词表敞口</small><b>
      {dashboard.open_vocabulary_fields} 个枚举字段还没发布词表，
      {dashboard.off_vocabulary_count} 个存量取值落在已发布词表之外。
    </b></span></div>

    <div className="prelaunch-fact"><Sparkles size={17}/><span><small>待归并</small><b>
      {dashboard.merge_candidate_count} 个取值全项目只有一条素材用过。
    </b></span></div>

    <div className="prelaunch-fact"><ShieldCheck size={17}/><span><small>口径</small><b>
      {dashboard.caliber_conflict_count
        ? `${dashboard.caliber_conflict_count} 处数据源之间对不上，受影响的指标在指标字典里已标成不可跨源比较。`
        : '各数据源口径一致。'}
    </b></span></div>

    <div className="prelaunch-fact"><GitBranch size={17}/><span><small>Skill 与评测</small><b>
      {dashboard.skill_version_count} 个版本在库里留有特征，累计 {dashboard.evaluation_samples} 条人机对照样本。
    </b></span></div>

    <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>这些数字每次都重新算</small>
      能力运营不落库。特征体系读的是代码里的六套 schema，指标字典是代码里声明的常量，
      Skill 和评测集从已有的特征行现算。存一份治理台账，它迟早会和它治理的数据对不上。
    </span></div>
  </>
}

// 金额（含千次曝光成本）后端存的是分；比率类派生指标按百分比读；
// 投产比是倍数——写成百分比会被读成「回本 190%」这种模棱两可的说法。
/**
 * 哪些字段才谈得上「词表发没发布」。
 *
 * 只有枚举字段。时长、目标时长这种数值字段本来就没有词表可发——把它们也标成
 * 「未发布」，等于在治理页上凭空造出一堆永远修不好的欠账，而顶上那句
 * 「N 个枚举字段还没发布词表」数的只有枚举字段，同一屏两个数字算的不是一回事。
 * 后端在算待归并队列时就是这么分的（operations.go 的 convergent）。
 */
function needsVocabulary(field: ApiFeatureFieldUsage): boolean {
  return field.kind === 'enum' || field.kind === 'enum_multi' || field.governed
}

function vocabularyLabel(field: ApiFeatureFieldUsage): string {
  if (!needsVocabulary(field)) return '不适用'
  return field.governed ? '已发布' : '未发布'
}

const fieldKindLabels: Record<ApiFeatureValueKind, string> = {
  text: '自由文本字段',
  tags: '开放标签字段',
  enum: '枚举字段',
  enum_multi: '多选枚举字段',
  number: '数值字段',
  bool: '是否字段',
  duration_seconds: '时长字段',
}

/** 「量出来的 3 · 人标的 1 · 模型猜的 8」。缺省时说清是没人填过，不是数出来都是 0。 */
function describeSources(counts?: Partial<Record<ApiFeatureSource, number>>): string {
  const entries = (Object.keys(featureSourceLabel) as ApiFeatureSource[])
    .filter(source => (counts?.[source] ?? 0) > 0)
    .map(source => `${featureSourceLabel[source]} ${counts?.[source]}`)
  return entries.length ? entries.join(' · ') : '还没人填过'
}

/** 能进归因的那部分占比。没人填过时算 1：没有数据不等于「都是模型猜的」。 */
function admissibleShare(counts?: Partial<Record<ApiFeatureSource, number>>): number {
  const total = (Object.values(counts ?? {}) as number[]).reduce((sum, value) => sum + value, 0)
  if (!total) return 1
  const admissible = (Object.keys(counts ?? {}) as ApiFeatureSource[])
    .filter(admissibleForAttribution)
    .reduce((sum, source) => sum + (counts?.[source] ?? 0), 0)
  return admissible / total
}

function formatMetric(metric: ApiMetricDictionaryEntry): string {
  if (metric.unit === '分') return formatMoney(metric.total)
  if (metric.key === 'roi') return `${metric.total.toFixed(2)} 倍`
  if (metric.kind === 'derived') return formatPercent(metric.total)
  return `${Math.round(metric.total).toLocaleString('zh-CN')}${metric.unit ?? ''}`
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(2)}%`
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
