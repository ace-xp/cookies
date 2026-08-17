import { useState } from 'react'
import { CircleCheck } from 'lucide-react'
import { api, type ApiInsightReport } from '../../../data/api'
import { FindingRow } from './FindingRow'
import { HarvestPanel } from './HarvestPanel'
import { SubmitReviewAction } from './SubmitReviewAction'

/**
 * 一份复盘的正文。
 *
 * 草稿态和已提交态是同一套布局，差别只在能不能改——两套布局会让人在提交前后
 * 对不上号，以为自己看的是另一份东西。
 */
export function DraftPanel({ report, projectId, onChanged, onSubmitted }: {
  report: ApiInsightReport
  projectId: string
  onChanged: () => void
  /** 提交成功时叫一声。提交是不可逆的，而且这份会从「本轮」里消失——
      不给回执的话，人按完只看到列表少了一行，会以为自己点错了或者东西丢了。 */
  onSubmitted?: (report: ApiInsightReport) => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const draft = report.status === 'draft'

  // 保留在完整 digest 里的下标：删减接口按下标定位，传分段后的下标会删错另一条。
  const rows = (report.digest ?? []).map((finding, index) => ({ finding, index }))
  const pinned = rows.filter(item => item.finding.origin === 'pinned')
  const system = rows.filter(item => item.finding.origin !== 'pinned')

  const drop = (index: number, dropped: boolean) => {
    setBusy(true); setError('')
    api.dropReportFinding(projectId, report.id, {
      index, dropped, expected_version: report.version,
    }).then(() => onChanged())
      .catch(() => setError('这一条没改成，页面可能不是最新的，刷新后再试。'))
      .finally(() => setBusy(false))
  }

  const submit = async ({ summary, executionId }: { summary: string; executionId: string }) => {
    setBusy(true); setError('')
    try {
      await api.submitReview(projectId, report.id, {
        summary, execution_id: executionId, expected_version: report.version,
      })
      onSubmitted?.(report)
      onChanged()
    } catch {
      setError('提交没成。可能是这一轮已经有人提交过同一次投放的复盘了，刷新看看。')
    } finally {
      setBusy(false)
    }
  }

  return <div className="review-body">
    <section>
      <span className="section-label">这一轮我记的（{pinned.length}）</span>
      {/* 档位是**记那一笔时定格下来的**，不是现在重算的。这句话必须写出来：
          badge 长得和分析页上的一模一样，人会默认它反映此刻的数据，于是拿一条
          两周前记的「✅能归因」去开今天的会——而那两周里可能又多跑了一批数据，
          现在重算未必还是这一档。定格本身是对的（复盘要能说清当时是怎么判的），
          没说出来才是错的。 */}
      {pinned.length ? <>
        <p className="prelaunch-disclosure">
          每条下面的档位是按下「记一笔」那一刻算出来的，之后不再重算。要看现在的判断，回「分析」里重新看一次。
        </p>
        <ul className="finding-list">
        {pinned.map(item => <FindingRow key={item.index} finding={item.finding}
          index={item.index} editable={draft && !busy} onDrop={drop}/>)}
        </ul>
      </> : <p className="panel-empty">
        这一轮还没记过。去「分析」里看，看到值得留的按「记一笔」。
      </p>}
    </section>

    <section>
      <span className="section-label">系统补的（{system.length}）</span>
      {draft ? <p className="prelaunch-disclosure">
        系统发现在提交那一刻才算——草稿还在改，现在补进来的数字到提交时就不是这个数了。
      </p> : system.length ? <ul className="finding-list">
        {system.map(item => <FindingRow key={item.index} finding={item.finding}
          index={item.index} editable={false} onDrop={drop}/>)}
      </ul> : <p className="panel-empty">这一轮系统没有补出新的发现。</p>}
    </section>

    {error ? <div className="inline-notice" role="status">{error}</div> : null}

    {draft
      ? <SubmitReviewAction window={{ start: report.window_start ?? '', end: report.window_end ?? '' }}
          busy={busy} currentExecutionId={report.execution_id} onSubmit={submit}/>
      : <>
          <p className="review-sealed"><CircleCheck size={15}/>这份复盘已提交，不能再改。</p>
          <HarvestPanel report={report} projectId={projectId} onChanged={onChanged}/>
        </>}
  </div>
}
