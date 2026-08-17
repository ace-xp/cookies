import { useEffect, useState } from 'react'
import { Camera } from 'lucide-react'
import { api, type ApiDeliveryExecutionResult } from '../../../data/api'
import { shortId } from '../../../data/shortId'
import { useProject } from '../../../context/ProjectContext'

/** 摘要上限，和后端 review.go 的 summaryLimit 是同一个数。 */
const SUMMARY_LIMIT = 200

/**
 * 「提交这一轮复盘」。提交不是重新算一遍，是把这一轮冻起来：人记的那几笔，
 * 加上系统这一刻补齐的发现。
 *
 * 这里额外问两件事，都只有这一次机会答（提交后报告不可改）：
 *  1. 这一轮讲的是什么——一句话摘要。它是复盘列表主列上唯一能把几份复盘区分开的
 *     东西，不问的话列表就是一列「（这一轮还没写摘要）」，人只能靠时间戳猜。
 *  2. 这一轮算哪次投放——可以不挂。挂的话只能从清单里挑，不给手打的口子：
 *     打错一个 ID，复盘就挂在了另一次投放上，而两边都不会报错。
 */
export function SubmitReviewAction({ window, busy, currentExecutionId, onSubmit }: {
  /** 这份复盘定格的窗口，原样传进来。这里不自己算窗口——一算就可能和屏幕上写的差一天。 */
  window: { start: string; end: string }
  busy: boolean
  /** 报告现在挂着的投放执行（记一笔建的草稿没有）。预选它，人不动就是不动。 */
  currentExecutionId?: string
  onSubmit: (input: { summary: string; executionId: string }) => Promise<void>
}) {
  const { currentProject } = useProject()
  const [open, setOpen] = useState(false)
  const [executions, setExecutions] = useState<ApiDeliveryExecutionResult[]>([])
  const [pickState, setPickState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [chosen, setChosen] = useState(currentExecutionId ?? '')
  const [summary, setSummary] = useState('')

  useEffect(() => {
    if (!open || !currentProject.id) return
    let cancelled = false
    setPickState('loading')
    api.listDeliveryExecutions(currentProject.id).then(page => {
      if (cancelled) return
      setExecutions(page.items ?? [])
      setPickState('ready')
    }).catch(() => {
      if (cancelled) return
      setExecutions([])
      setPickState('error')
    })
    return () => { cancelled = true }
  }, [open, currentProject.id])

  const windowText = `${formatDate(window.start)} ~ ${formatDate(window.end)}`
  const overLimit = summary.trim().length > SUMMARY_LIMIT

  if (!open) {
    return <div className="prelaunch-actions">
      <button className="secondary-button full" onClick={() => setOpen(true)}>
        <Camera size={15}/>提交这一轮复盘
      </button>
    </div>
  }

  return <div className="experience-reason">
    <span className="section-label">提交这一轮复盘（{windowText}）</span>
    <p className="prelaunch-disclosure">
      提交会把这一轮的复盘冻起来：你记的那几笔，加上系统这一刻补齐的素材表现、
      实验结论、相关经验和下一轮建议。系统补的只算这一次，之后再换窗口不影响它。
      提交之后这份复盘不能再改——它会被下一轮引用，改一条等于让引用它的人手上那份变成假的。
      所以下面这两格也是最后一次能填。
    </p>

    <label className="freeze-pick">
      <small>这一轮讲的是什么？（一句话，之后不能改）</small>
      <input aria-label="这一轮的摘要" value={summary} maxLength={SUMMARY_LIMIT + 20}
        placeholder="比如：7 月中旬短视频三版时长对比"
        onChange={event => setSummary(event.target.value)}/>
      <small className={overLimit ? 'freeze-pick-warn' : undefined}>
        {overLimit
          ? `超了 ${summary.trim().length - SUMMARY_LIMIT} 个字。这一格是列表上的一行，太长反而认不出哪份是哪份。`
          : `不填也能提交，那样列表上显示的是系统给这份报告的摘要。还能填 ${SUMMARY_LIMIT - summary.trim().length} 字。`}
      </small>
    </label>

    {pickState === 'loading' ? <div className="panel-empty">正在读取投放执行…</div>
      : <label className="freeze-pick">
          <small>这份复盘算哪次投放？</small>
          <select aria-label="投放执行" value={chosen} onChange={event => setChosen(event.target.value)}>
            {/* 「不挂」放在第一位并且是默认值：草稿是记一笔自动建的，那会儿根本没有
                投放这回事，默认替人挑上「最近一次」等于让复盘悄悄挂到一次不相干的
                投放上——而报告详情里会白纸黑字写着它，看的人不会怀疑。 */}
            <option value="">不挂投放执行（这一轮没有对应的投放）</option>
            {/* 只给日期加一句 evidence.summary 是不够的：同一天跑几次演练，summary 又是
                同一句模板话，四个选项会长得一模一样，人没法选——下拉的意义就没了。
                时分能分开同一天的几次，短码能分开同一分钟的几次，两个都得有。
                「最近一次」跟的是后端顺序（ListExecutions 按 started_at DESC），不是这里显示的
                完成时间——先开始的未必先跑完，两者极少但可能不一致。 */}
            {executions.map((item, index) => <option key={item.execution.id} value={item.execution.id}>
              {formatMoment(item.execution.completed_at || item.execution.started_at)}
              {' · '}{shortId(item.execution.id)}
              {index === 0 ? ' · 最近一次' : ''}
              {item.evidence.summary ? ` · ${item.evidence.summary}` : ''}
            </option>)}
          </select>
          <small>
            {pickState === 'error'
              ? '投放执行读取失败，这次只能先不挂。复盘本身照样提交得了。'
              : !executions.length
                ? '这个 Project 还没有投放执行，只能不挂。要挂的话，去「智能投放 → 上线后优化闭环」跑完那条 9 步的路，之后的复盘就挂得上了。'
                : '不挂也能提交：还没投出去的那批素材，一样值得复盘。'}
          </small>
        </label>}

    <div className="prelaunch-actions">
      {/* 不因为「没挑投放」而禁用提交。原来这里是 !chosen 就禁用，结果是人记了
          一屏发现之后被卡在最后一步，而他要复盘的恰恰是还没投出去的那批。 */}
      <button className="primary-button full" disabled={busy || overLimit}
        onClick={() => { void onSubmit({ summary: summary.trim(), executionId: chosen }) }}>
        <Camera size={15}/>{busy ? '正在提交…' : '提交'}
      </button>
      <button className="text-button" onClick={() => setOpen(false)}>取消</button>
    </div>
  </div>
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleDateString('zh-CN')
}

/** 选执行时要精确到分：同一天跑好几次演练是常态，只给日期等于没给。 */
function formatMoment(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return value
  return `${date.toLocaleDateString('zh-CN')} ${date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`
}
