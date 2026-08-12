import { useCallback, useEffect, useMemo, useState } from 'react'
import { Check, CircleAlert } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiExperienceReference,
  type ApiExperienceReferenceOutcome,
  type ApiInsightCard,
} from '../../../data/api'
import { strategyApi } from '../../../features/strategy/api'
import { formatScope } from '../experience/ExperienceCard'

/**
 * 「用进 Brief」。
 *
 * 这是投前洞察最后一米：读完一条结论，人得能当场把它按进正在写的 Brief 或者
 * 创意任务里，而且这次引用要留下来。文档从一开始就是这么要求的——19 §投前洞察
 * 的关键要素写着「证据、适用边界、Brief 引用、CreativeTask 引用」，22 §验收第
 * 2 条写着「Brief 和 CreativeTask 可以引用投前洞察，引用关系在刷新后保持」。
 *
 * 留痕不是为了审计好看，是为了下一轮能问一句**这条经验到底管不管用**：被引用
 * 过五次、五次都标了「照做了」，和被引用过五次、四次标了「看过没用」，是两条
 * 完全不同的经验，而结论本身一个字都不会变。
 *
 * 后端 `:record-reference` 按 (经验, 目标类型, 目标 ID) 唯一：同一条经验按进
 * 同一个 Brief 第二次不会多出一条记录，而是把结果和备注更新掉。所以这个面板
 * 同时是「引用」和「回来改结果」两件事的入口——投之前记「只是引用」，投完回来
 * 改成「照做了」或者「改了之后用」。
 */
export function CiteTargetPanel({ card, onRecorded }: {
  card: ApiInsightCard
  /**
   * 记成功之后通知卡片更新「用过 N 次」——不更新的话人按完看不出发生了什么。
   * `created` 区分新记一条和改掉旧的：改旧的不该让计数往上跳。
   */
  onRecorded: (created: boolean) => void
}) {
  const { currentProject } = useProject()
  const [targets, setTargets] = useState<CiteTarget[]>([])
  const [existing, setExisting] = useState<ApiExperienceReference[]>([])
  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [selected, setSelected] = useState('')
  const [outcome, setOutcome] = useState<ApiExperienceReferenceOutcome>('referenced')
  const [note, setNote] = useState('')
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState('')
  const [done, setDone] = useState(false)

  /**
   * 备注预填。带上目标的名字，是因为引用记录那一行最终只显示 `Brief · a1b2c3d4`
   * ——真实 ID 是 32 位哈希，切尾之后谁也认不出那是哪个 Brief。名字写进备注里，
   * 半年后翻这条经验的引用记录才知道它当初被用到哪去了。
   */
  const buildNote = useCallback((target?: CiteTarget) => [
    target ? `用进${target.group}《${target.label}》。` : '',
    `引用自经验 ${card.experience_id}：${card.conclusion}`,
    `（适用：${formatScope(card.applicability)}）`,
  ].join('').slice(0, 1000), [card])

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setLoadState('loading')
    // 三条一起取。Brief 和创意任务分属两个系统，任何一个不可用都不该让整个
    // 面板打不开——人手上正要用的那个可能恰好在活着的那一边。
    const [briefs, tasks, references] = await Promise.allSettled([
      strategyApi.listProjectBriefs(currentProject.id),
      api.listCreativeTasks(currentProject.id),
      api.listExperienceReferences(currentProject.id, card.experience_id),
    ])

    const list: CiteTarget[] = []
    if (briefs.status === 'fulfilled') {
      for (const item of briefs.value.items) {
        if (item.discarded_at) continue
        list.push({ kind: 'brief', id: item.brief_id, group: 'Brief', label: item.name || item.brief_id, hint: item.objective })
      }
    }
    if (tasks.status === 'fulfilled') {
      for (const item of tasks.value.items) {
        list.push({
          kind: 'creative_task', id: item.id, group: '创意任务',
          label: item.direction.concept || item.direction.core_message || item.id,
          hint: [item.channel, item.format === 'video' ? '视频' : '图文'].filter(Boolean).join(' · '),
        })
      }
    }
    setTargets(list)
    setExisting(references.status === 'fulfilled' ? references.value.items : [])
    // 两边都挂了才叫读不出来。只挂一边的话下拉里少一组，比整个面板变成
    // 一句报错有用——另一组照样能选。
    setLoadState(briefs.status === 'rejected' && tasks.status === 'rejected' ? 'error' : 'ready')
    setNotice(briefs.status === 'rejected' && tasks.status === 'rejected'
      ? '需求中心和创意任务都读不出来，先确认这两个系统在不在。' : '')
  }, [currentProject.id, card.experience_id])

  useEffect(() => { void load() }, [load])

  // 选中一个已经引用过的目标，把上次记的结果和备注调出来——这个面板的第二个
  // 身份是「回来改结果」，不预填的话人会以为是新记一条，然后把备注重写一遍。
  const previous = useMemo(() => {
    const target = targets.find(item => key(item) === selected)
    if (!target) return undefined
    return existing.find(item => item.consumer_kind === target.kind && item.consumer_id === target.id)
  }, [existing, selected, targets])

  const pick = (value: string) => {
    setSelected(value)
    setDone(false)
    const target = targets.find(item => key(item) === value)
    const seen = target && existing.find(item => item.consumer_kind === target.kind && item.consumer_id === target.id)
    setOutcome((seen?.outcome as ApiExperienceReferenceOutcome) ?? 'referenced')
    setNote(seen?.note || buildNote(target))
  }

  const submit = async () => {
    const target = targets.find(item => key(item) === selected)
    if (!target) return
    setSaving(true)
    try {
      const saved = await api.recordExperienceReference(currentProject.id, card.experience_id, {
        consumer_kind: target.kind, consumer_id: target.id, outcome, note: note.trim(),
      })
      setExisting(current => [saved, ...current.filter(item =>
        !(item.consumer_kind === saved.consumer_kind && item.consumer_id === saved.consumer_id))])
      setDone(true)
      setNotice('')
      onRecorded(!previous)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '这次引用没记下来。')
    } finally {
      setSaving(false)
    }
  }

  if (loadState === 'loading') return <div className="cite-panel"><p className="panel-empty">正在找能用的地方…</p></div>
  if (loadState === 'error') return <div className="cite-panel"><p className="panel-empty">{notice}</p></div>
  if (!targets.length) return <div className="cite-panel"><p className="panel-empty">
    这个 Project 下还没有 Brief，也没有创意任务。先去「需求与策略」建一个，这条结论才有地方可用。
  </p></div>

  const groups = ['Brief', '创意任务'].filter(group => targets.some(item => item.group === group))

  return <div className="cite-panel">
    <label className="lookup-field">
      <small>用到哪里</small>
      <select value={selected} onChange={event => pick(event.target.value)}>
        <option value="">选一个 Brief 或创意任务</option>
        {groups.map(group => <optgroup key={group} label={group}>
          {targets.filter(item => item.group === group).map(item => <option key={key(item)} value={key(item)}>
            {item.label}
            {item.hint ? ` · ${item.hint}` : ''}
            {existing.some(ref => ref.consumer_kind === item.kind && ref.consumer_id === item.id) ? '（已引用）' : ''}
          </option>)}
        </optgroup>)}
      </select>
    </label>

    {selected ? <>
      <label className="lookup-field">
        <small>这次是怎么用的</small>
        <select value={outcome} onChange={event => setOutcome(event.target.value as ApiExperienceReferenceOutcome)}>
          {outcomeOptions.map(item => <option key={item.value} value={item.value}>{item.label}</option>)}
        </select>
      </label>
      {/* 四挡的意思写出来。「已修改」这种词每个人的理解都不一样，而这一格
          决定的是下一轮怎么看这条经验，不能靠猜。 */}
      <p className="cite-hint">{outcomeHint[outcome]}</p>

      <label className="lookup-field cite-note">
        <small>记下什么（会跟着这条引用一起存，别人回头能看懂当时用的是哪一条）</small>
        <textarea value={note} maxLength={1000} onChange={event => setNote(event.target.value)}/>
      </label>

      {previous ? <p className="cite-hint">
        这条已经引用进这里过一次（{new Date(previous.updated_at).toLocaleDateString('zh-CN')}），
        再记一次是改掉上次那条，不会多出一条记录。
      </p> : null}

      <div className="cite-actions">
        <button type="button" className="primary-button" disabled={saving} onClick={() => { void submit() }}>
          {saving ? '正在记…' : previous ? '改掉上次那条' : '记下这次引用'}
        </button>
        {done ? <span className="cite-done"><Check size={14}/>记下了</span> : null}
        {notice ? <span className="cite-error"><CircleAlert size={14}/>{notice}</span> : null}
      </div>
    </> : null}
  </div>
}

type CiteTarget = {
  kind: 'brief' | 'creative_task'
  id: string
  group: 'Brief' | '创意任务'
  label: string
  hint: string
}

/** Brief 和创意任务的 ID 各自唯一，但没保证互不相同，所以下拉的值带上类型。 */
function key(target: CiteTarget): string {
  return `${target.kind}:${target.id}`
}

const outcomeOptions: { value: ApiExperienceReferenceOutcome; label: string }[] = [
  { value: 'referenced', label: '只是引用' },
  { value: 'adopted', label: '照做了' },
  { value: 'modified', label: '改了之后用' },
  { value: 'rejected', label: '看过没用' },
]

const outcomeHint: Record<ApiExperienceReferenceOutcome, string> = {
  referenced: '写进去当依据了，但还没决定照不照做。投完回来把这一格改掉。',
  adopted: '按这条结论说的做了。这一格是它下一轮还算不算数的主要依据。',
  modified: '用了它的思路，但改了做法。备注里写清改了什么，否则下一轮看不出是这条经验起的作用。',
  rejected: '看过，决定不用。记下来比不记有用——省得下一轮又有人从头评估一遍。',
}
