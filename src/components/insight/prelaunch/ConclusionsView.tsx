import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiPreLaunchFilter, type ApiPreLaunchInsight } from '../../../data/api'
import { InsightCardItem } from './InsightCardItem'

/**
 * 「结论」：这一轮开工前，以前什么有效。
 *
 * 这一屏只给**能照着做的**结论——后端只放行状态在用、且判定为 ✅ 能归因的经验。
 * 「👁 只是观察」的进不来，这是故意的：开工前的人拿到一条结论就会照着做，把
 * 没排除掉别的变量的东西摆在同一列里，等于请人踩坑。要看全量（含观察级、按
 * 品牌/产品/受众筛），走「经验 → 查经验」，那一屏是目录，这一屏是决定。
 */
export function ConclusionsView() {
  const { currentProject } = useProject()
  const [filter, setFilter] = useState<ApiPreLaunchFilter>({})
  const [data, setData] = useState<ApiPreLaunchInsight | null>(null)
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [notice, setNotice] = useState('')

  const load = useCallback(async (next: ApiPreLaunchFilter) => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      setData(await api.getPreLaunch(currentProject.id, next))
      setListState('ready')
      setNotice('')
    } catch (cause) {
      setData(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '投前洞察读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load(filter) }, [load, filter])
  // 换 Project 要把条件清掉：上一个项目筛的「视频号」在这个项目里可能一条都没有，
  // 人看到空页面会以为这个项目没经验，其实是被上一次的条件卡住了。
  // 已经是空条件时不重设——每次给一个新的 {} 都会让上面那个 effect 再取一次数。
  useEffect(() => {
    setFilter(current => Object.keys(current).length ? {} : current)
  }, [currentProject.id])

  const update = (patch: Partial<ApiPreLaunchFilter>) => setFilter(current => ({ ...current, ...patch }))

  const scope = useMemo(() => [filter.channel, filter.creative_type, filter.objective]
    .filter(Boolean).join(' · '), [filter])

  const cards = data?.cards ?? []
  // 跨渠道提示只在真横跨了两个以上渠道、而且人没显式打开时出现。抖音有效的开场
  // 未必在视频号有效，两个渠道的结论并排列出来，人会当成一份榜单从上往下照做。
  const mixed = (data?.mixed_channels?.length ?? 0) > 1 && !data?.cross_channel_comparison

  return <div className="experience-lookup">
    <div className="core-flow-toolbar">
      <div>
        <span className="section-label">PRE-LAUNCH</span>
        <h2>这一轮开工前，以前什么有效</h2>
        <p>{data?.disclosure || '只给状态在用、且能归因的经验。是否适用于这次投放仍要人判断。'}</p>
      </div>
      <button className="secondary-button" disabled={listState === 'loading'}
        onClick={() => { void load(filter) }}><RefreshCw size={15}/>刷新</button>
    </div>

    <p className="lookup-context">
      按 {scope || '（没设条件）'} 筛出 {cards.length} 条。
      {data?.patterns.length ? `另有 ${data.patterns.length} 个反复有效的内容特征，在「历史模式」里。` : ''}
    </p>

    <div className="lookup-filters">
      <Field label="渠道" value={filter.channel} options={data?.facets.channels}
        onChange={value => update({ channel: value })}/>
      <Field label="广告类型" value={filter.creative_type} options={data?.facets.creative_types}
        onChange={value => update({ creative_type: value })}/>
      <Field label="目标" value={filter.objective} options={data?.facets.objectives}
        onChange={value => update({ objective: value })}/>
      <label className="lookup-field">
        <small>关键词</small>
        <input value={filter.q ?? ''} placeholder="结论里出现的词"
          onChange={event => update({ q: event.target.value })}/>
      </label>
      <label className="lookup-toggle">
        <input type="checkbox" checked={Boolean(filter.cross_channel)}
          onChange={event => update({ cross_channel: event.target.checked })}/>
        跨渠道一起看
      </label>
      <button type="button" className="text-button" onClick={() => setFilter({})}>清空条件</button>
    </div>

    {/* 数据体检没过的时候，下面每一条结论都得先打折。这个提示必须排在结论前面：
        排在后面，人已经读完并且信了。 */}
    {data && !data.strong_conclusions_allowed ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>数据有问题，下面的结论只能当线索</small>
      {data.quality_blockers?.length
        ? data.quality_blockers.join('；')
        : '这一次的数据体检没通过，具体原因在「设置 → 数据体检」。'}
    </span></div> : null}
    {data && !data.quality_checked ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>这次没能做数据体检</small>
      不是「体检通过」，是压根没查成。下面的结论照样看，但别把它们当成已经验过的。
    </span></div> : null}

    {mixed ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>这些结论来自 {data?.mixed_channels.join('、')} 多个渠道</small>
      别横着比——抖音有效的开场未必在视频号有效。要合起来看，勾上「跨渠道一起看」；
      只看一个渠道的，上面选渠道。
    </span></div> : null}

    {/* 被挡掉的不能静默丢：一条没写适用范围的经验证明不了它适用于这次投放，
        所以筛的时候不返回；但不报数，人会以为经验库是空的。 */}
    {data && data.unscoped_excluded > 0 ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>另有 {data.unscoped_excluded} 条没写适用范围，被挡在外面</small>
      它们没说自己在什么条件下成立，所以证明不了适用于这次投放。要把它们补全，去「经验 → 管经验」。
    </span></div> : null}

    {listState === 'loading' ? <p className="panel-empty">正在按条件筛…</p> : null}
    {listState === 'error' ? <p className="panel-empty">{notice || '投前洞察读取失败，请重试。'}</p> : null}
    {listState === 'ready' && !cards.length ? <p className="panel-empty">{emptyHint(data, Boolean(scope || filter.q))}</p> : null}

    {cards.map(card => <InsightCardItem key={card.experience_id} card={card}/>)}
  </div>
}

/**
 * 空结果要分清三种空，该做的事完全不同：一个是清条件，一个是去复盘，一个是
 * 去把适用范围补上。说成「暂无数据」，人只会以为系统坏了。
 */
function emptyHint(data: ApiPreLaunchInsight | null, filtered: boolean): string {
  if (data && data.unscoped_excluded > 0) {
    return `这些条件下没有能照着做的结论——有 ${data.unscoped_excluded} 条经验因为没写适用范围被挡掉了。去「经验 → 管经验」把它们的适用范围补上，这里就能看到。`
  }
  if (filtered) return '这些条件下没有能照着做的结论。清空条件再看看。'
  return '这个 Project 还没有能照着做的结论。结论来自复盘——投完一轮、提交复盘、有人确认它「能归因」，才会出现在这里。'
}

/** 一格条件。取值从后端给的 facets 来，不写死一张渠道表——写死的话人能选到
    「小红书」，但这里一条相关经验都没有，他会以为是筛坏了而不是本来就没有。 */
function Field({ label, value, options, onChange }: {
  label: string
  value: string | undefined
  options: string[] | undefined
  onChange: (value: string) => void
}) {
  const list = options ?? []
  return <label className="lookup-field">
    <small>{label}</small>
    {list.length
      ? <select value={value ?? ''} onChange={event => onChange(event.target.value)}>
        <option value="">不限</option>
        {list.map(item => <option key={item} value={item}>{item}</option>)}
        {/* 选中的值可能已经不在 facets 里了（筛完之后剩下的经验不含它）。
            不补这一项，select 会显示成「不限」，而 state 里其实卡着这个值。 */}
        {value && !list.includes(value) ? <option value={value}>{value}</option> : null}
      </select>
      : <input value={value ?? ''} onChange={event => onChange(event.target.value)} placeholder="不限"/>}
  </label>
}
