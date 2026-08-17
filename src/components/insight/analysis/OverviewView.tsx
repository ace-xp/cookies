import { useEffect, useMemo, useRef, useState } from 'react'
import { CircleAlert, CircleCheck, Database, Layers3, Lightbulb, TrendingUp } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiMetricOverview, type ApiMetricRates, type ApiQualityStatus } from '../../../data/api'
import { PinFindingButton, VerdictBadge } from '../shared'
import type { ViewProps } from './AnalysisPage'
import { formatCount, formatDate, formatMoney, formatRate, formatRatio } from './format'
import { pinKey } from './usePinFinding'

/**
 * 总览。这段时间的钱换回了什么，以及这个数字能信到什么程度。
 *
 * 只有这一个视图要自己再取一次数：performance-analysis 回答的是「谁比谁好、
 * 为什么」，它里面没有总额、没有逐日曲线、也没有素材矩阵。窗口用的是壳传下来的
 * 那一个，不自己算——两屏都写着「近 30 天」却差一天，是最难被发现的那种错。
 *
 * 素材详情、数据源、分平台原本在右栏。这里挪进主栏：右栏现在是六个视图共用的
 * 「这一页怎么读」，塞一个只有总览有的素材详情进去，切到别的视图时它要么消失、
 * 要么留着一块和当前屏无关的内容。
 */
const qualityLabels: Record<ApiQualityStatus, string> = {
  healthy: '正常',
  delayed: '延迟',
  partial: '不完整',
  mapping_incomplete: '映射未完成',
  tracking_broken: '追踪异常',
  reconciling: '对账中',
  blocked: '已阻断',
}

export function OverviewView({ window, onPin, pinned, pinning, onJudgement }: ViewProps) {
  const { currentProject } = useProject()
  const [overview, setOverview] = useState<ApiMetricOverview | null>(null)
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    if (!currentProject.id) return
    let alive = true
    setListState('loading')
    api.getMetricOverview(currentProject.id, window.start, window.end)
      .then(next => {
        if (!alive) return
        setOverview(next)
        setListState('ready')
      })
      .catch((cause: unknown) => {
        if (!alive) return
        setOverview(null)
        setListState('error')
        setNotice(cause instanceof Error ? cause.message : '投后指标读取失败。')
      })
    // 切窗口时上一次请求可能后回来。不撤销的话，页面会闪回旧窗口的数字。
    return () => { alive = false }
  }, [currentProject.id, window.start, window.end])

  // 把这一屏自己的档位报给壳，让顶上的徽章说的是总览的话。
  // 用 ref 存回调：它由壳每次渲染新建，进依赖数组会把这个 effect 变成每帧都跑。
  const report = useRef(onJudgement)
  report.current = onJudgement
  // ApiMetricOverview 本身就 `& Judgement`，整个传上去即可——挑字段重组的话，
  // 后端哪天给 Judgement 加一个字段，这里会静默漏掉。
  useEffect(() => { report.current?.(overview) }, [overview])

  // 花得多的排前面：先看清钱去哪了，再谈哪一版更好。
  const assets = useMemo(() => [...(overview?.assets ?? [])]
    .sort((left, right) => right.counts.spend_cents - left.counts.spend_cents), [overview])

  useEffect(() => {
    const ids = assets.map(asset => asset.asset_id ?? asset.asset_title)
    setSelectedId(current => ids.includes(current) ? current : ids[0] ?? '')
  }, [assets])

  const selected = assets.find(asset => (asset.asset_id ?? asset.asset_title) === selectedId)
  const series = overview?.series ?? []
  const peakSpend = Math.max(...series.map(point => point.counts.spend_cents), 1)
  const hasData = Boolean(overview && overview.totals.impressions + overview.totals.spend_cents > 0)

  // 有问题的先说：延迟、质量异常、口径冲突、不可比，都要在图表之前出现。
  // 壳上那一条说的是分析结果的毛病，和这里说的取数的毛病不是一回事，两条都要有。
  const troubles = [
    ...(overview?.warnings ?? []),
    ...(overview?.caliber_conflicts ?? []),
    ...(overview && !overview.comparable
      ? [overview.comparable_reason || '这些数字口径不一致，不能直接放在一起比。']
      : []),
    ...(overview && overview.unmatched_objects > 0
      ? [`有 ${overview.unmatched_objects} 个平台对象还没认领，${formatMoney(overview.unmatched_spend_cents)} 花费计入了总盘但算不到任何素材头上。去「素材 · 数据接入」认领。`]
      : []),
  ]

  // 总览记一笔记的是整屏的结论——它没有单独的主语。
  const target = { dimension: 'overview' as const }

  if (listState === 'loading') return <div className="panel-empty">正在取数…</div>
  if (listState === 'error') return <div className="panel-empty">{notice || '读取失败，请重试。'}</div>
  if (!overview || !hasData) return <div className="panel-empty">
    这个窗口里没有任何指标数据。先在「数据接入」接一个数据源、配好字段映射并导入报表，这一页才有东西可看。
  </div>

  return <>
    {troubles.length ? <div className="feature-stack">
      <span>取数本身的问题（会影响下面每一个数字怎么读）</span>
      {troubles.map((item, index) => <b key={index}>{item}</b>)}
    </div> : null}

    <div className="insight-metric-grid">
      <MetricCard label="花费" value={formatMoney(overview.totals.spend_cents)} note={`收入 ${formatMoney(overview.totals.revenue_cents)}`}/>
      <MetricCard label="曝光" value={formatCount(overview.totals.impressions)} note={`点击 ${formatCount(overview.totals.clicks)}`}/>
      <MetricCard label="点击率" value={formatRate(overview.rates.ctr)}
        note={overview.ctr_interval
          ? `95% 区间 ${formatRate(overview.ctr_interval.low)} ~ ${formatRate(overview.ctr_interval.high)}`
          : '样本不足以给出区间'}/>
      <MetricCard label="转化成本" value={formatMoney(overview.rates.cpa_cents)}
        note={`转化 ${formatCount(overview.totals.conversions)} · ROAS ${formatRatio(overview.rates.roas)}`}/>
    </div>

    <div className="prelaunch-fact"><Layers3 size={17}/><span><small>数据窗口</small><b>
      {formatDate(overview.window.start)} ~ {formatDate(overview.window.end)} · 共 {series.length} 天有数据
    </b></span></div>
    <div className="prelaunch-fact"><Database size={17}/><span><small>口径（两个数字并排之前必须一致的东西）</small><b>
      {overview.caliber.currency} · 归因 {overview.caliber.attribution_window} · 指标口径 {overview.caliber.metric_schema_version} · 时区 {overview.caliber.time_zone}
    </b></span></div>
    <div className="prelaunch-fact">
      {overview.verdict === 'explained' ? <CircleCheck size={17}/> : <CircleAlert size={17}/>}
      <span><small>这段数字能用来做什么 <VerdictBadge judgement={overview}/></small><b>
        {overview.note}
        {' '}<PinFindingButton onPin={pinning ? undefined : () => onPin(target)} pinned={pinned.has(pinKey(target))}/>
      </b></span>
    </div>

    <div className="insight-series">
      <span className="section-label">每天的花费与点击率 · {series.length} 个数据点</span>
      {series.map(point => <div className="insight-series-row" key={point.date}>
        <span>{formatDate(point.date)}</span>
        <span className="insight-series-track">
          {/* 条形只表示花费的相对大小，用来一眼看出钱压在哪几天，不承担精确读数。 */}
          <i style={{ width: `${Math.max(2, Math.round((point.counts.spend_cents / peakSpend) * 100))}%` }}/>
        </span>
        <span>{formatMoney(point.counts.spend_cents)}</span>
        <span>点击率 {formatRate(point.rates.ctr)}</span>
        <span>曝光 {formatCount(point.counts.impressions)}</span>
      </div>)}
    </div>

    <div className="prelaunch-table" role="list" aria-label="素材表现矩阵">
      <div className="prelaunch-row insight-asset-row header">
        <span>素材版本</span><span>花费</span><span>点击率</span><span>转化成本</span><span>结论</span>
      </div>
      {assets.map(asset => {
        const id = asset.asset_id ?? asset.asset_title
        return <button role="listitem" key={id} className={`prelaunch-row insight-asset-row${id === selectedId ? ' active' : ''}`} onClick={() => setSelectedId(id)}>
          <span><b>{asset.asset_title}</b><small>{asset.objects} 个平台对象{asset.attributable ? '' : ' · 归属不确定'}</small></span>
          <span>{formatMoney(asset.counts.spend_cents)}</span>
          <span>{formatRate(asset.rates.ctr)}</span>
          <span>{formatMoney(asset.rates.cpa_cents)}</span>
          <span><VerdictBadge judgement={asset}/></span>
        </button>
      })}
      {assets.length ? null : <div className="panel-empty">
        这个窗口里的花费还没有认领到任何素材版本上，所以排不出素材矩阵。
      </div>}
    </div>

    {selected ? <div className="insight-analysis-card">
      <header><b>{selected.asset_title}</b><VerdictBadge judgement={selected}/></header>

      <div className="prelaunch-fact"><TrendingUp size={17}/><span><small>这一版的账</small><b>
        花 {formatMoney(selected.counts.spend_cents)}，换回 {formatCount(selected.counts.impressions)} 次曝光、
        {formatCount(selected.counts.clicks)} 次点击、{formatCount(selected.counts.conversions)} 个转化。
      </b></span></div>
      <div className="prelaunch-fact"><Layers3 size={17}/><span><small>派生指标</small><b>
        点击率 {formatRate(selected.rates.ctr)} · 转化率 {formatRate(selected.rates.cvr)} ·
        转化成本 {formatMoney(selected.rates.cpa_cents)} · 千次曝光成本 {formatMoney(selected.rates.cpm_cents)} ·
        ROAS {formatRatio(selected.rates.roas)}
      </b></span></div>
      {missingRates(selected.rates).length ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
        <small>有指标算不出来</small>{missingRates(selected.rates).join('、')}的分母是 0，所以没有值。这不是「表现差」，是「这个问题问不出答案」，排序和比较都不能把它当 0。
      </span></div> : null}

      <div className="prelaunch-fact"><Lightbulb size={17}/><span><small>这个数字能用来做什么</small><b>
        {selected.note}
      </b></span></div>
      {selected.attributable ? null : <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
        <small>归属不确定</small>这一版的部分花费来自还没认领的平台对象，数字只能当参考，不能拿去和别的版本比。
      </span></div>}
    </div> : null}

    {/* 契约说 sources 是必填数组，但后端历史上会在空窗口下发 null。这里不信任
        契约，因为这一句崩掉的代价是整页白屏，而防御的代价是一个问号。 */}
    {overview.sources?.length ? <div className="feature-stack">
      <span>数据从哪来（{overview.sources.length} 个数据源）</span>
      {overview.sources.map(source => <b key={source.data_source_id}>
        {source.label} · {qualityLabels[source.quality_status]}
        {source.data_through ? ` · 数据到 ${formatDate(source.data_through)}` : ' · 还没有数据'}
        {source.freshness_days > 1 ? ` · 已滞后 ${source.freshness_days} 天` : ''}
        {source.quality_note ? ` · ${source.quality_note}` : ''}
      </b>)}
    </div> : null}

    {(overview.platforms?.length ?? 0) > 1 ? <div className="feature-stack">
      <span>分平台</span>
      {overview.platforms.map(item => <b key={item.platform}>
        {item.label} · {formatMoney(item.counts.spend_cents)} · 点击率 {formatRate(item.rates.ctr)}
      </b>)}
    </div> : null}
  </>
}

function MetricCard({ label, value, note }: { label: string; value: string; note: string }) {
  return <div className="insight-metric-card">
    <small>{label}</small>
    <b>{value}</b>
    <span>{note}</span>
  </div>
}

/** 缺失的派生指标：给出人话名字，用来解释「为什么这里是不可用」。 */
function missingRates(rates: ApiMetricRates): string[] {
  const names: Array<[keyof ApiMetricRates, string]> = [
    ['ctr', '点击率'],
    ['cvr', '转化率'],
    ['completion_rate', '完播率'],
    ['cpa_cents', '转化成本'],
    ['cpm_cents', '千次曝光成本'],
    ['roas', 'ROAS'],
  ]
  return names.filter(([key]) => rates[key] === undefined).map(([, label]) => label)
}
