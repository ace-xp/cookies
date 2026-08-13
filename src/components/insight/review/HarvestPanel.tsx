import { useEffect, useMemo, useState } from 'react'
import { BookOpenCheck, CircleAlert, Save } from 'lucide-react'
import { api, type ApiExperience, type ApiInsightReport, type ApiReportFinding } from '../../../data/api'

/**
 * 把这一轮的结论留成经验。
 *
 * 复盘提交完不等于这一轮有产出：报告本身不会进下一轮投前洞察，只有从它留下、
 * 并且有人确认过的经验会。所以这一段只在提交之后出现——后端也只认已确认的复盘。
 *
 * **默认从这份复盘的发现里挑一条，而不是自己敲一句话。** 挑出来的那条是系统按
 * 当时那一版判定标准算出来的，它身上带着档位和标准版本号，留成经验时整条继承；
 * 自己敲的一句话没有任何标准参与过，只能落到最保守的一格，而且永远说不清它是
 * 按什么算的。原来这里只有一个空文本框、提示语写「从上面的发现里挑一条粘过来」
 * ——粘过来的是一串字符，那条发现的档位和出处全丢在了复制的路上。
 */
export function HarvestPanel({ report, projectId, onChanged }: {
  report: ApiInsightReport
  projectId: string
  onChanged: () => void
}) {
  const [harvested, setHarvested] = useState<ApiExperience[]>([])
  const [draft, setDraft] = useState('')
  // 挑中的是第几条发现。空串表示「自己写一句」。用序号不用对象：
  // 同一份报告里两条发现的文字可能一模一样（不同素材的同一个结论）。
  const [picked, setPicked] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')

  // 被人删掉的发现不出现在这里：那是人明确表示「这条不要」的，再让它有机会
  // 变成经验，等于把删除当成没发生。
  const kept = useMemo(
    () => (report.digest ?? []).filter(finding => !finding.dropped),
    [report.digest],
  )
  // ❓算不出来的那些也不出现：它们在复盘里是对的（「这一轮连这个都判断不了」
  // 是这一轮的事实），但经验是一句会被下一轮引用的主张。把一条判断不了的变成
  // 经验，等于凭空造出一个从来没算出来过的结论，而它在经验库里长得和真结论
  // 一模一样。后端也会拒（service.go 的 ❓ 守卫），这里先拦是为了不让人白填一遍。
  const options = useMemo(() => kept.filter(finding => finding.verdict !== 'unclear'), [kept])
  const unclearCount = kept.length - options.length
  const source = picked === '' ? undefined : options[Number(picked)]

  // 换一份复盘就把输入框清空：上一份的结论留在框里，很容易被顺手记到这一份上。
  useEffect(() => {
    setDraft('')
    setPicked('')
    setNotice('')
    if (!projectId) return
    let cancelled = false
    api.listExperiences(projectId).then(page => {
      if (cancelled) return
      setHarvested((page.items ?? []).filter(item => item.report_id === report.id))
    }).catch(() => { if (!cancelled) setHarvested([]) })
    return () => { cancelled = true }
  }, [projectId, report.id, report.version])

  const already = new Set(harvested.map(experience => experience.conclusion))

  const harvest = async () => {
    const conclusion = draft.trim()
    setBusy(true)
    setNotice('')
    try {
      // 类型和置信留空：挑了发现的，档位由后端从那条发现上继承；自己敲的会落到
      // 最保守的一格（假设 / 方向性）。这是故意的——一句话直接变出来的结论确实
      // 还没有依据，替录入的人标成「事实」是伪造。适用条件同理留空，空着，经验库
      // 才会明写「未填写适用条件」，人才知道还欠什么。
      await api.createExperienceFromReport(projectId, report.id, {
        expected_report_version: report.version,
        conclusion,
        // 三格照原样回传，后端按同一把尺去报告里找那条发现。找不到不报错，
        // 按「自己写的」处理。
        ...(source ? {
          source_finding: {
            dimension: source.dimension,
            variable: source.variable,
            source_ref: source.source_ref,
          },
        } : {}),
      })
      setDraft('')
      setPicked('')
      setNotice(source
        ? '已记成待确认的经验，档位和判定标准跟着那条发现一起带过来了。去经验库补上适用范围和数据依据，它才能当证据用。'
        : '已记成待确认的经验。自己写的一句话没有判定标准可依，它现在是「假设 / 方向性」，去经验库补上适用范围和数据依据后才能当证据用。')
      onChanged()
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '记成经验失败，请重试。')
    } finally {
      setBusy(false)
    }
  }

  return <div className="experience-reason">
    <span className="section-label">留成经验</span>

    {harvested.length ? <div className="feature-stack">
      <span>这份复盘留下的 {harvested.length} 条经验</span>
      {harvested.map(experience => <b key={experience.id}>{experience.conclusion}</b>)}
    </div> : <div className="prelaunch-boundary">
      <CircleAlert size={16}/>
      <span>
        <small>这份复盘还没留下任何经验</small>
        报告本身不会进入下一轮投前洞察，只有从它留下并确认的经验会。
      </span>
    </div>}

    <label className="harvest-source">
      <small>留的是哪一条发现</small>
      <select value={picked} onChange={event => {
        const value = event.target.value
        setPicked(value)
        // 挑中就把原话填进框里：绝大多数时候人只是想微调措辞。改完之后来源不变
        // ——换的是说法，不是依据。
        const finding = value === '' ? undefined : options[Number(value)]
        if (finding) setDraft(finding.text)
      }}>
        <option value="">自己写一句（没有判定标准可依）</option>
        {options.map((finding, index) => <option key={index} value={String(index)}>
          {findingOptionLabel(finding)}
        </option>)}
      </select>
    </label>
    {options.length === 0 ? <p className="harvest-hint">
      {kept.length
        ? '这份复盘里能留成经验的发现一条都没有——剩下的都是「❓算不出来」。只能自己写，而自己写的结论没有判定标准可依，到了经验库里会说不清它是按什么算出来的。'
        : '这份复盘里一条发现都没有，只能自己写。自己写的结论没有判定标准可依，到了经验库里会说不清它是按什么算出来的。'}
    </p> : null}
    {unclearCount ? <p className="harvest-hint">
      另有 {unclearCount} 条「❓算不出来」的发现没列在上面：那一档连差异存不存在都没判出来，
      留成经验等于凭空造一个结论。先按它去「素材」里找相似素材把样本做厚，重算成能归因之后再回来留。
    </p> : null}
    {source ? <p className="harvest-hint">
      这条发现的档位是「{source.verdict_label || source.confidence || '未标'}」，
      留成经验时连同判定标准的版本一起带过去。
    </p> : null}

    <textarea aria-label="要留下的结论" rows={3} value={draft}
      onChange={event => setDraft(event.target.value)}
      placeholder={options.length ? '挑一条发现，或自己写一句结论' : '写一句结论'}/>
    <div className="prelaunch-actions">
      {/* 同一句结论记两次会变成两条经验，投前洞察里就会看到重复的卡。
          成功后清空输入框，字面重复的也直接拦住。 */}
      <button className="primary-button full"
        disabled={busy || !draft.trim() || already.has(draft.trim())}
        onClick={() => { void harvest() }}>
        <Save size={15}/>{busy ? '正在保存…' : already.has(draft.trim()) ? '这条结论已经留过了' : '留成待确认经验'}
      </button>
    </div>
    {notice ? <div className="inline-notice" role="status">{notice}</div> : null}

    <div className="reference-count">
      <BookOpenCheck size={15}/>
      <span><b>{harvested.length} 条经验来自这份复盘</b><small>留下来还要确认，确认后才会进入投前洞察</small></span>
    </div>
  </div>
}

/**
 * 下拉里一条发现怎么念。
 *
 * 带上档位和出自哪个视图：同一份报告里「素材对比 · 时长」和「疲劳 · 时长」
 * 说的是两件事，只念结论正文的话，两行看上去几乎一样。
 */
function findingOptionLabel(finding: ApiReportFinding): string {
  const tags = [dimensionLabels[finding.dimension ?? ''] ?? finding.dimension, finding.verdict_label]
    .filter(Boolean).join(' · ')
  const text = finding.text.length > 42 ? `${finding.text.slice(0, 42)}…` : finding.text
  return tags ? `${text}（${tags}）` : text
}

// 维度是后端存的英文键，这里翻成人话；认不出来的原样显示——写死一张表然后把陌生的
// 挡掉，会让一条真实存在的发现在下拉里变成空白。
const dimensionLabels: Record<string, string> = {
  comparisons: '素材对比',
  trends: '趋势',
  fatigue: '疲劳',
  anomalies: '异常',
  drivers: '归因',
  overview: '总览',
}
