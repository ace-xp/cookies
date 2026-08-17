import { useCallback, useEffect, useMemo, useState } from 'react'
import { CalendarRange, CircleAlert, CircleCheck, Database, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiExperience, type ApiInsightReport } from '../../../data/api'
import { shortId } from '../../../data/shortId'
import type { DataState } from '../../../types'
import { StateBoundary } from '../../StateBoundary'
import { DraftPanel } from './DraftPanel'

/**
 * 「复盘」入口。一轮投放收尾时人真正要做事的地方。
 *
 * 一屏干一件事：左边挑一份复盘，右边看「这一轮我记的」和「系统补的」，
 * 逐条决定留不留，然后提交。提交之后这份复盘冻住，才谈得上留成经验。
 *
 * 三个视图分的是「现在要做什么」，不是「按什么排序」：
 *   本轮     —— 只列还没提交的草稿，这是日常最常进的一屏；
 *   全部复盘 —— 含已提交的，用来回头查一轮投放当时是怎么判的；
 *   留下的经验 —— 查的不是报告而是经验按 report_id 的归属，答的是
 *                「这些复盘最后到底留下了什么」。一份提交了却没留下
 *                任何经验的复盘，等于这次收尾白做了，这个视图专门让它显出来。
 */
export type ReviewView = 'current' | 'all' | 'harvest'

const headings: Record<ReviewView, { label: string; title: string; lead: string }> = {
  current: {
    label: 'REVIEW',
    title: '这一轮我记了什么，该收尾了',
    lead: '还没提交的复盘草稿。草稿是你在分析页按「记一笔」时自动建的，一个窗口一份。提交时系统才会把漏掉的补齐。',
  },
  all: {
    label: 'REVIEW · ALL',
    title: '这个 Project 的所有复盘',
    lead: '含已提交的。提交过的不能再改——它会被下一轮引用，改一条等于让引用它的人手上那份变成假的。',
  },
  harvest: {
    label: 'REVIEW · HARVEST',
    title: '这些复盘最后留下了什么',
    lead: '按复盘看它留下了哪些经验。一份已确认却一条经验都没留下的报告，说明这次复盘的结论没人愿意为它背书——这比没做复盘更值得注意。',
  },
}

const emptyHints: Record<ReviewView, string> = {
  current: '这一轮还没有复盘草稿。去「分析」里看，看到值得留的按「记一笔」，草稿会自动建出来。',
  all: '这个 Project 还没有复盘。复盘从「记一笔」开始，不是等系统生成的。',
  harvest: '还没有提交过的复盘。提交之后才能从它留下经验。',
}

export function ReviewPage({ state, view, objectId }: {
  state: DataState
  view: ReviewView
  /** 从别处跳进来时带的复盘 ID，比如刚记完一笔想直接看草稿。 */
  objectId?: string
}) {
  const { currentProject } = useProject()
  const [reports, setReports] = useState<ApiInsightReport[]>([])
  const [experiences, setExperiences] = useState<ApiExperience[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const [reportPage, experiencePage] = await Promise.all([
        api.listReports(currentProject.id),
        api.listExperiences(currentProject.id),
      ])
      setReports(reportPage.items ?? [])
      setExperiences(experiencePage.items ?? [])
      setListState('ready')
    } catch (cause) {
      setReports([])
      setExperiences([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '复盘读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setNotice('') }, [view])

  // 一份复盘留下的经验按 report_id 归属。三个视图都要用它：
  // 「留下了几条」是判断一次收尾有没有落地的唯一依据。
  const harvested = useMemo(() => {
    const map = new Map<string, ApiExperience[]>()
    experiences.forEach(experience => {
      if (!experience.report_id) return
      map.set(experience.report_id, [...(map.get(experience.report_id) ?? []), experience])
    })
    return map
  }, [experiences])

  const visible = useMemo(() => {
    if (view === 'current') return reports.filter(report => report.status === 'draft')
    if (view === 'harvest') return reports.filter(report => report.status === 'confirmed')
    return reports
  }, [reports, view])

  // 默认选最近那份。列表按后端给的顺序（新的在前），所以取第一条。
  useEffect(() => {
    const ids = visible.map(report => report.id)
    setSelectedId(current => {
      if (objectId && ids.includes(objectId)) return objectId
      return ids.includes(current) ? current : ids[0] ?? ''
    })
  }, [visible, objectId])

  const selected = visible.find(report => report.id === selectedId)

  // 同一个窗口可以有不止一份复盘：提交完之后又看到值得留的结论，再按「记一笔」
  // 开出来的是下一份草稿，老的那份定格留档。不点破的话，人在「本轮」看到一份
  // 只有一条发现的草稿，会以为上次提交的东西丢了。
  const sameWindow = useMemo(() => {
    if (!selected?.window_start || !selected.window_end) return []
    return reports.filter(report => report.id !== selected.id
      && report.window_start === selected.window_start && report.window_end === selected.window_end)
  }, [reports, selected])
  const earlierConfirmed = sameWindow.filter(report => report.status === 'confirmed').length
  const laterDraft = sameWindow.some(report => report.status === 'draft')

  const draftCount = reports.filter(report => report.status === 'draft').length
  const barrenCount = reports.filter(report =>
    report.status === 'confirmed' && !(harvested.get(report.id)?.length)).length
  const heading = headings[view]

  return <StateBoundary state={state} onRetry={() => { void load() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">{heading.label}</span>
            <h2>{heading.title}</h2>
            <p>当前 Project：{currentProject.name}。{heading.lead}</p>
          </div>
          <div className="core-flow-actions">
            <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void load() }}>
              <RefreshCw size={15}/>刷新
            </button>
          </div>
        </div>

        {/* 已提交却一条经验都没留下的复盘，是这一页最该被看见的事：做了，什么也没留下。 */}
        {view === 'harvest' && barrenCount > 0 ? <div className="feature-stack">
          <span>有 {barrenCount} 份已提交的复盘一条经验都没留下</span>
          <b>提交只表示「这一轮的账算清了」，不表示有人愿意把它当成下一轮的依据。这几份复盘的结论目前不会出现在投前洞察里。</b>
        </div> : null}

        <div className="prelaunch-filterbar">
          <span>{visible.length} 份复盘</span>
          <span>全项目 {reports.length} 份 · 未提交 {draftCount} 份 · 留下的经验 {experiences.filter(item => item.report_id).length} 条</span>
        </div>

        {listState === 'loading' ? <div className="panel-empty">正在读取当前 Project 的复盘…</div> : null}
        {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}
        {listState === 'ready' && !visible.length ? <div className="panel-empty">{emptyHints[view]}</div> : null}

        {listState === 'ready' && visible.length ? <div className="prelaunch-table" role="list" aria-label={heading.title}>
          <div className="prelaunch-row header">
            <span>复盘</span><span>数据窗口</span><span>留下的经验</span><span>状态</span>
          </div>
          {visible.map(report => <button role="listitem" key={report.id}
            className={report.id === selectedId ? 'prelaunch-row active' : 'prelaunch-row'}
            /* 人自己换一份复盘时把回执清掉：那句「已提交」说的是刚才那一份，
               挂在另一份旁边就成了错的。提交后的自动跳转不走这里，所以回执留得住。 */
            onClick={() => { setSelectedId(report.id); setNotice('') }}>
            <span>
              <b>{reportTitle(report)}</b>
              <small>记了 {countPinned(report)} 笔 · {formatTime(report.created_at)}</small>
            </span>
            <span>{report.window_start && report.window_end
              ? `${formatDay(report.window_start)} ~ ${formatDay(report.window_end)}`
              : '未定格窗口'}</span>
            <span>{harvested.get(report.id)?.length ?? 0} 条经验</span>
            <span>
              {report.status === 'confirmed' ? <CircleCheck size={14}/> : <CircleAlert size={14}/>}
              {report.status === 'confirmed' ? '已提交' : '未提交'}
            </span>
          </button>)}
        </div> : null}
      </section>

      <aside className="prelaunch-detail">
        {selected ? <>
          <span className="section-label">
            {selected.status === 'confirmed' ? '已提交' : '未提交'} · 第 {selected.version} 版
          </span>
          <h3>{reportTitle(selected)}</h3>

          {/* 模拟投放要说在最前面：它的结论不能和真实投放的结论平起平坐。 */}
          {selected.is_simulated ? <div className="prelaunch-boundary">
            <CircleAlert size={16}/>
            <span>
              <small>这是模拟投放的复盘</small>
              数据来自演示数据集 {selected.dataset_version || '（未标版本）'}，不是任何一次真实投放的结果。
              从它留下的经验同样只是演示，不能作为真实决策依据。
            </span>
          </div> : null}

          {/* 这个窗口上还有别的复盘。两个方向都要说：手上是新草稿时，得知道
              「上次提交的没丢，在这个窗口的已提交里」；手上是已提交的那份时，
              得知道「后来又开了一份，别以为这份就是最新结论」。 */}
          {selected.status === 'draft' && earlierConfirmed > 0 ? <div className="prelaunch-boundary">
            <CircleAlert size={16}/>
            <span>
              <small>这个窗口之前已经提交过 {earlierConfirmed} 份复盘</small>
              提交过的不能再改，所以这一笔记进了新的一份草稿。老的那几份在「全部复盘」里，
              它们留下的经验也照样在用——这份提交之后，两份并存，不会互相覆盖。
            </span>
          </div> : null}
          {selected.status === 'confirmed' && laterDraft ? <div className="prelaunch-boundary">
            <CircleAlert size={16}/>
            <span>
              <small>这个窗口后来又开了一份草稿</small>
              有人在这份提交之后又记了新的东西。要看这一轮最新的判断，去「本轮」。
            </span>
          </div> : null}

          {selected.window_start && selected.window_end ? <div className="prelaunch-fact">
            <CalendarRange size={17}/><span><small>这一轮的数据窗口</small><b>
              {formatDay(selected.window_start)} ~ {formatDay(selected.window_end)}
            </b></span>
          </div> : null}

          {selected.status === 'confirmed' ? <div className="prelaunch-fact">
            <Database size={17}/><span><small>这一轮算哪次投放</small><b>
              {/* 没挂就明写「没挂」，不写「未记录」——后者听上去像系统漏了，
                  人会去找那个丢掉的 ID；实际是提交的人选了不挂，那是个有效的答案。 */}
              {/* 分隔符写成表达式：JSX 会把「· 换行 缩进」末尾那段空白连同换行一起吃掉，
                  直接换行写出来的是「·user_local」，中间那个空格在页面上不存在。 */}
              {selected.execution_id ? `执行 ${shortId(selected.execution_id)}` : '没挂投放执行（这一轮没有对应的投放）'}
              {' · '}
              {selected.confirmed_by || '某人'} 于 {selected.confirmed_at ? formatTime(selected.confirmed_at) : '（时间未记录）'} 提交
            </b></span>
          </div> : null}

          {/* 经验条数这一行由「留成经验」那一段自己给——它就在那个按钮下面，
              说的是刚刚按下去会发生什么。草稿态还留不成经验，这里再挂一个永远是 0 的
              计数只会让人以为哪里出了问题。 */}
          {/* 提交成功要有回执。在「本轮」里提交完，这份立刻从列表消失、选中跳到
              另一份草稿——不说一句的话，人看到的是「我点了提交，然后我那份没了」。
              而提交恰恰是这一页唯一不可逆的动作，最不该让人怀疑它有没有成。

              回执里用窗口指认这一份，不用 reportTitle：传给 onSubmitted 的是**提交前**
              那一份，摘要是这次提交才写上去的，它身上还没有——套 reportTitle 会对着一份
              刚提交完的复盘说「草稿，提交时写摘要」。 */}
          <DraftPanel report={selected} projectId={currentProject.id}
            onChanged={() => { void load() }}
            onSubmitted={submitted => {
              const window = submitted.window_start && submitted.window_end
                ? `${formatDay(submitted.window_start)} ~ ${formatDay(submitted.window_end)} 这一轮的复盘`
                : '这份复盘'
              setNotice(view === 'current'
                ? `已提交，${window}冻住了，不能再改。它已经不在「本轮」里——去「全部复盘」能看到它，也是在那里把它的结论留成经验。`
                : `已提交，${window}冻住了，不能再改。下面可以把它的结论留成经验。`)
            }}/>
        </> : <div className="panel-empty">左边选一份复盘，看这一轮记了什么、系统补了什么。</div>}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

/**
 * 一份复盘在列表和详情里怎么称呼。
 *
 * 摘要是提交时写的，草稿本来就还没有——对草稿说「还没写摘要」像是在指人漏了什么，
 * 而他压根还没到能写的那一步。已提交的没有摘要才是真的少了东西，但也不能再补了，
 * 所以这里退回到窗口：至少「7 月 1 日 ~ 7 月 14 日那份」还能把它认出来。
 */
function reportTitle(report: ApiInsightReport): string {
  if (report.summary) return report.summary
  const window = report.window_start && report.window_end
    ? `${formatDay(report.window_start)} ~ ${formatDay(report.window_end)}`
    : ''
  if (report.status !== 'confirmed') {
    return window ? `${window} 这一轮（草稿，提交时写摘要）` : '这一轮（草稿，提交时写摘要）'
  }
  return window ? `${window} 这一轮（提交时没写摘要）` : '（提交时没写摘要）'
}

function countPinned(report: ApiInsightReport): number {
  return (report.digest ?? []).filter(finding => finding.origin === 'pinned').length
}

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

// 窗口存的就是「2026-07-01」这样的日子，不是时间戳。走一遍 Date 会按本地时区
// 换算，可能差一天——而复盘上写的是记这一笔时人看到的那个区间。
function formatDay(value: string): string {
  const parts = value.split('-')
  return parts.length === 3 ? `${parts[0]}/${Number(parts[1])}/${Number(parts[2])}` : value
}
