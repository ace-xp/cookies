import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, FlaskConical, Layers3, RefreshCw, Ruler } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiPerformanceAnalysis } from '../../../data/api'
import { verdictLabel, type Judgement } from '../../../data/verdict'
import type { DataState } from '../../../types'
import { StateBoundary } from '../../StateBoundary'
import { NotEnoughSample, ThresholdStamp, VerdictBadge } from '../shared'
import { AnomalyView } from './AnomalyView'
import { ComparisonView } from './ComparisonView'
import { DriverView } from './DriverView'
import { FatigueView } from './FatigueView'
import { formatDate, isoDate } from './format'
import { OverviewView } from './OverviewView'
import { TrendView } from './TrendView'
import { usePinFinding, type PinTarget } from './usePinFinding'

/**
 * 「分析」入口。六个视图自由探索，每一屏顶上标三档。
 *
 * 六个视图共用一次 performance-analysis 请求：拆成六次的话，「趋势里看到的」
 * 和「疲劳里算的」会来自两份数据，对不上的时候没人解释得清哪个对。
 *
 * 这一页对结论**只读**。唯一的写是「记一笔」——把这一条钉进本轮复盘草稿；
 * 要不要留成经验，是复盘页上另一次明确的决定。分析页是自由探索的地方，
 * 一个误点不该把一条没想清楚的东西直接变成下一轮的依据。
 *
 * 全页只有一条纪律：**能不能归因，和差异有多大，是两件事**。所以每条结论旁边
 * 都有一句 note 说明它为什么是这个档位。
 */
export type AnalysisView = 'overview' | 'comparisons' | 'trends' | 'fatigue' | 'anomalies' | 'drivers'

/** 六个视图统一的入参。overview 另外要窗口，因为它自己还要取一次指标总览。 */
export interface ViewProps {
  analysis: ApiPerformanceAnalysis
  /** 素材对比和驱动因素上「找相似素材」要用。在这一层取，六个视图不各拿一次。 */
  projectId: string
  window: { start: string; end: string }
  onPin: (target: PinTarget) => void
  pinned: ReadonlySet<string>
  pinning: boolean
  /**
   * 总览专用：把它自己那一次取数算出来的档位报上来，让壳上的屏级徽章显示它。
   *
   * 总览和另外五屏不是一回事——它读的是指标总览接口，档位是那个接口给的，
   * performance-analysis 里没有对应的一份。不报上来的话，壳只能拿跨视图那一档
   * 顶上，于是「总览每条都是能归因」和「徽章说算不出来」同屏出现。
   */
  onJudgement?: (judgement: Judgement | null) => void
}

const rangeOptions = [
  { label: '近 7 天', days: 7 },
  { label: '近 30 天', days: 30 },
  { label: '近 90 天', days: 90 },
]

const headings: Record<AnalysisView, { label: string; title: string; lead: string }> = {
  overview: {
    label: 'OVERVIEW',
    title: '这段时间的钱换回了什么，以及这个数字能信到什么程度',
    lead: '只统计已接入数据源导进来的真实指标，没有的就是没有。派生指标分母为零时显示「不可用」，不退化成 0。',
  },
  comparisons: {
    label: 'VARIANT COMPARISON',
    title: '两个版本之间到底改了什么，差异能不能算到它头上',
    lead: '同类型素材两两配对。先识别变量，再谈差异——同时改了两个地方的对比，数字差得再多也归不了因。',
  },
  trends: {
    label: 'TREND',
    title: '每个素材在这段时间里是往上走还是往下走',
    lead: '按真正有数据的天数把窗口对半切开比较，不是按日历天数。缺数据的日子不算平，算不出来。',
  },
  fatigue: {
    label: 'FATIGUE',
    title: '哪些素材开始跑不动了，以及有哪些解释还没被排除',
    lead: '曝光还在放大但点击率下滑、或点击率下滑同时成本上升，才判为「疑似疲劳」；单项恶化只到「需要观察」。',
  },
  anomalies: {
    label: 'ANOMALY',
    title: '哪一天的数字明显偏离了常态',
    lead: '用中位数和 MAD 判断偏离，不用平均值和标准差——一次大促尖峰会把标准差自己抬高，之后什么都不再异常。',
  },
  drivers: {
    label: 'DRIVER',
    title: '哪些内容特征和表现一起变化',
    lead: '一起变化不等于起了作用。组内还有别的特征同向变化时，这里会直接把它标成「分不开」，不给能归因。',
  },
}

// 空态的文案说清「缺什么」和「怎么补」。只写「暂无数据」的话，人无从下手。
//
// 异常那条不写「没有发现异常」：这一屏空着有两种相反的含义（查过了很干净 /
// 天数不够根本没查），哪一种只有后端知道。后端会在 view_judgements.anomalies
// 里给出带档位的那句话，空态优先用它，这里的文案只是取不到时的兜底。
const emptyHints: Record<AnalysisView, string> = {
  overview: '这个窗口里没有任何指标数据。先在「数据接入」接一个数据源、配好字段映射并导入报表，这一页才有东西可看。',
  comparisons: '还没有可配对的素材。需要同一类型下至少两个素材、且都在这个窗口里有投放数据。',
  trends: '这个窗口里没有素材产生过投放数据。',
  fatigue: '这个窗口里没有素材跑够两段可比较的时间。',
  anomalies: '这个窗口里没有列出偏离常态的日子。',
  drivers: '还没有素材记录内容特征，或同一取值下的素材不足 2 个。去「素材 · 变量」补齐特征后再来看。',
}

export function AnalysisPage({ state, view, onOpenExperiments }: {
  state: DataState
  view: AnalysisView
  onOpenExperiments: () => void
}) {
  const { currentProject } = useProject()
  const [rangeLabel, setRangeLabel] = useState('近 30 天')
  const [analysis, setAnalysis] = useState<ApiPerformanceAnalysis | null>(null)
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  // 窗口在这一层算，六个视图共用同一个：换成各视图各算一次的话，人在总览上
  // 看到的那几天和在趋势上看到的那几天可能差一天，而两屏都写着「近 30 天」。
  const window = useMemo(() => {
    const days = rangeOptions.find(option => option.label === rangeLabel)?.days ?? 30
    const end = new Date()
    const start = new Date(end.valueOf() - days * 24 * 60 * 60 * 1000)
    return { start: isoDate(start), end: isoDate(end) }
  }, [rangeLabel])

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      setAnalysis(await api.getPerformanceAnalysis(currentProject.id, window.start, window.end))
      setListState('ready')
    } catch (cause) {
      setAnalysis(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '分析结果读取失败。')
    }
  }, [currentProject.id, window.start, window.end])

  useEffect(() => { void load() }, [load])

  const { pin, pinned, pinning, notice: pinNotice } = usePinFinding(currentProject.id, window)

  const heading = headings[view]

  // 总览自己那一屏的档位，由 OverviewView 报上来。切走视图时清掉：留着的话，
  // 从总览切到趋势的那一瞬间，徽章会短暂显示上一屏的档位。
  const [overviewJudgement, setOverviewJudgement] = useState<Judgement | null>(null)
  useEffect(() => { setOverviewJudgement(null) }, [view, window.start, window.end])

  // 屏级徽章说的必须是**当前这一屏**。跨视图那一档挪到右栏，标明它是跨视图的。
  const screenJudgement = view === 'overview'
    ? overviewJudgement
    : analysis?.view_judgements?.[view] ?? analysis?.judgement ?? null

  // 会影响下面每一条结论怎么读的东西，一律放在结论前面。
  const troubles = useMemo(() => [
    ...(analysis?.notes ?? []),
    ...(analysis && !analysis.comparable
      ? [analysis.comparable_reason || '窗口内口径不一致，可归因的判定已全部降级为方向性观察。']
      : []),
  ], [analysis])

  const rows = analysis ? countFor(analysis, view) : 0

  return <StateBoundary state={state} onRetry={() => { void load() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">{heading.label}</span>
            <h2>{heading.title}</h2>
            <p>{heading.lead}</p>
          </div>
          <div className="core-flow-actions">
            {/* 屏级档位挨着窗口放：先知道这一屏能信到什么程度，再看下面的数字。
                取的是**这一屏自己**的档位——取跨视图那一档的话，会出现「正文每条
                都是 ✅」而徽章写 ❓ 的同屏打架。 */}
            {screenJudgement ? <VerdictBadge judgement={screenJudgement}/> : null}
            {/* 每屏只标一处。判定阈值是可调的，不标的话，同一屏在两次调整之间
                看起来完全一样，实际是按两套标准算的。 */}
            {analysis ? <ThresholdStamp version={analysis.judgement.threshold_version}/> : null}
            <label>窗口<select aria-label="数据窗口" value={rangeLabel} onChange={event => setRangeLabel(event.target.value)}>
              {rangeOptions.map(option => <option key={option.label}>{option.label}</option>)}
            </select></label>
            <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void load() }}>
              <RefreshCw size={15}/>刷新
            </button>
            {/* 实验中心没有侧栏入口了。只从 👁 徽章的「做个实验」跳进去的话，
                一屏里一个 👁 都没出现时这个页面就完全不可达——功能还在，路断了。
                这个按钮常驻，六个视图下都在。 */}
            <button type="button" className="secondary-button" onClick={onOpenExperiments}>
              <FlaskConical size={15}/>实验
            </button>
          </div>
        </div>

        {troubles.length ? <div className="feature-stack">
          <span>先看这些（会影响下面每一条结论怎么读）</span>
          {troubles.map((item, index) => <b key={index}>{item}</b>)}
        </div> : null}

        {pinNotice ? <div className="inline-notice" role="status">{pinNotice}</div> : null}

        {listState === 'loading' ? <div className="panel-empty">正在取数…</div>
          : listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div>
          : !analysis ? <div className="panel-empty">{emptyHints[view]}</div>
          /* 空列表不是「暂无数据」，档位照样要标出来，而且要标这一屏自己的那一档
             ——整屏取的是六个视图里最弱的一档，别的视图有结论时它可能是「只是观察」，
             安到一条都没有的这一屏上就成了「❓ 只是观察」，图标和字自己打架。

             档位和理由都取后端 view_judgements 里这一屏那一份，前端不再自己判成
             「算不出来」：异常屏空着的常见含义是**查过了，很干净**，那是一条站得住
             的结论；前端一律判 unclear 的话，一屏干净数据会被说成没查成。
             总览除外：它自己还要取一次指标总览，空不空由它自己判断。 */
          : rows === 0 && view !== 'overview' ? <NotEnoughSample judgement={
            analysis.view_judgements?.[view] ?? {
              ...analysis.judgement, verdict: 'unclear', verdict_label: verdictLabel.unclear, note: emptyHints[view],
            }
          }/>
          : renderView(view, {
            analysis, projectId: currentProject.id, window, onPin: pin, pinned, pinning,
            onJudgement: setOverviewJudgement,
          })}
      </section>

      <aside className="prelaunch-detail">
        <span className="section-label">这一页怎么读</span>
        <h3>{heading.title}</h3>
        <p>{heading.lead}</p>

        {analysis ? <>
          <div className="prelaunch-fact"><Layers3 size={17}/><span><small>数据窗口</small><b>
            {formatDate(analysis.window.start)} ~ {formatDate(analysis.window.end)}
          </b></span></div>
          <div className="prelaunch-fact"><Ruler size={17}/><span><small>口径（两个数字并排之前必须一致的东西）</small><b>
            {analysis.caliber.currency} · 归因 {analysis.caliber.attribution_window} ·
            指标口径 {analysis.caliber.metric_schema_version} · 时区 {analysis.caliber.time_zone}
          </b></span></div>
          <div className="prelaunch-fact"><Layers3 size={17}/><span><small>覆盖情况</small><b>
            窗口内有 {analysis.assets_in_window} 个素材产生了数据，其中 {analysis.assets_with_features} 个记录了内容特征。
          </b></span></div>
          {/* 跨视图那一档挪到这里，并且写明它是跨视图的。它有用——别的屏上有算不
              出来的结论，是「这次分析整体别急着下结论」的信号；但它不能挂在屏级
              徽章的位置上替当前这一屏说话。 */}
          <div className="prelaunch-fact"><Layers3 size={17}/><span><small>另外五屏合起来</small><b>
            <VerdictBadge judgement={analysis.judgement}/> {analysis.judgement.note}
          </b></span></div>
          {analysis.assets_with_features < analysis.assets_in_window ? <div className="prelaunch-boundary">
            <CircleAlert size={16}/><span><small>特征没覆盖全</small>
              还有 {analysis.assets_in_window - analysis.assets_with_features} 个素材没有内容特征。它们不会出现在素材对比和驱动因素里
              ——不是它们表现不好，是没法判断它们和别人差在哪。去「素材 · 变量」补。
            </span></div> : null}
        </> : null}

        <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>这一页给不了什么</small>
          这里全部是观察到的相关，没有一条是实验证明的因果。要确认某个变量真的有效，得在同一批投放里只改它一个
          ——那是一个实验，可以从「只是观察」那一档的升级通道里建。
        </span></div>

        <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>看到值得留的怎么办</small>
          按那一行的「记一笔」。它只把这条钉进本轮复盘草稿，不改任何结论，也不算数你已经认可了它
          ——要不要留成下一轮能用的经验，是复盘页上另一次明确的决定。
        </span></div>

        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

function renderView(view: AnalysisView, props: ViewProps) {
  switch (view) {
    case 'comparisons': return <ComparisonView {...props}/>
    case 'trends': return <TrendView {...props}/>
    case 'fatigue': return <FatigueView {...props}/>
    case 'anomalies': return <AnomalyView {...props}/>
    case 'drivers': return <DriverView {...props}/>
    default: return <OverviewView {...props}/>
  }
}

// 后端已经保证这些数组不会是 null，这里再兜一层：一个字段没返回不该让整页打不开。
function countFor(analysis: ApiPerformanceAnalysis, view: AnalysisView): number {
  if (view === 'comparisons') return (analysis.comparisons ?? []).length
  if (view === 'trends') return (analysis.trends ?? []).length
  if (view === 'fatigue') return (analysis.fatigue ?? []).length
  if (view === 'anomalies') return (analysis.anomalies ?? []).length
  if (view === 'drivers') return (analysis.drivers ?? []).length
  return 1
}
