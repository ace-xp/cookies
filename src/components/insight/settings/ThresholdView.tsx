import { useCallback, useEffect, useMemo, useState } from 'react'
import { FileCode2, History, Lock, RotateCcw } from 'lucide-react'
import { useAuth } from '../../../context/AuthContext'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiInsightSettings,
  type ApiSettingItem,
  type ApiResolvedThresholds,
  type ApiThresholdSet,
} from '../../../data/api'
import { hasScope, roleSentence } from '../../../data/scopes'
import { formatDate } from '../analysis/format'

/**
 * 判定阈值。
 *
 * 单列分组表单，不是仪表盘：这一屏一次只回答一个问题——「现在按什么标准判，我要不要
 * 改它」。每一格三样东西缺一不可：**当前值、调它会发生什么、出厂推荐**。
 *
 * 说明文案全部渲染后端给的 `effect` / `recommended`，前端一个字都不另写。前端抄一份，
 * 改了 Go 忘了改这里，这一页就从说明变成误导——那比不做更糟。
 *
 * **留空 = 跟出厂设定走**，不是 0。所以输入框里只在「有人调过」时才有数字；没调过的
 * 格子留空并把出厂值写在 placeholder 上。这样将来改了代码里的默认值，没调过的部署
 * 会跟着走，而它们本来就该跟着走。
 *
 * 理由必填。改判定标准是要负责的事：写不出理由的改动多半是试出来的，而试出来的阈值
 * 三个月后没人说得清为什么是这个数。
 */
type Draft = Record<string, string>

export function ThresholdView() {
  const { currentProject } = useProject()
  // 改阈值走的是「确认」那一档权限（改的是全组织的判定标准，影响面比写一条数据大）。
  // 在这里先问一次，是为了让没这个权限的人在**填理由之前**就知道自己保存不了。
  const { session } = useAuth()
  const canEdit = hasScope(session, 'insights.confirm')
  const [settings, setSettings] = useState<ApiInsightSettings | null>(null)
  const [current, setCurrent] = useState<ApiResolvedThresholds | null>(null)
  const [history, setHistory] = useState<ApiThresholdSet[]>([])
  const [draft, setDraft] = useState<Draft>({})
  const [reason, setReason] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const [nextSettings, nextCurrent] = await Promise.all([
        api.getInsightSettings(currentProject.id),
        api.getThresholds(currentProject.id),
      ])
      setSettings(nextSettings)
      setCurrent(nextCurrent)
      setDraft(initialDraft(nextSettings, nextCurrent))
      setReason('')
      // 改动史读不到不该让整页打不开：它是旁证，不是这一屏的主体。
      try {
        setHistory((await api.listThresholdHistory(currentProject.id)).items)
      } catch {
        setHistory([])
      }
      setListState('ready')
    } catch (cause) {
      setSettings(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '设置读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])

  const groups = (settings?.groups ?? []).filter(group => group.view === 'thresholds')
  const baseline = useMemo(
    () => settings && current ? initialDraft(settings, current) : {},
    [settings, current],
  )
  // 改动史里显示的是人看的名字，不是 sufficient_impressions 这种键名。翻译表从后端
  // 给的条目现取：另写一份中文对照，改了 Go 的措辞这里就悄悄对不上。
  const labels = useMemo(() => {
    const map: Record<string, string> = {}
    for (const group of settings?.groups ?? []) {
      for (const item of group.items) if (item.editable_key) map[item.editable_key] = item.label
    }
    return map
  }, [settings])
  const dirty = Object.keys(draft).some(key => (draft[key] ?? '') !== (baseline[key] ?? ''))
  const malformed = Object.values(draft).some(value => value.trim() !== '' && !isPositiveInteger(value))
  const canSave = canEdit && dirty && !malformed && reason.trim() !== '' && !busy

  const save = useCallback(async () => {
    if (!settings) return
    setBusy(true)
    setNotice('')
    try {
      const values: Record<string, number | null> = {}
      for (const key of Object.keys(draft)) {
        const raw = draft[key].trim()
        values[key] = raw === '' ? null : Number(raw)
      }
      const saved = await api.saveThresholds(currentProject.id, { values, reason: reason.trim() })
      await load()
      setNotice(`已存为第 ${saved.version} 版。从现在起判出来的结论都按这一版算，之前的结论保持原样。`)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '保存失败。')
    } finally {
      setBusy(false)
    }
  }, [currentProject.id, draft, load, reason, settings])

  return <div className="settings-page">
    {listState === 'loading' ? <p className="settings-status">读取中…</p> : null}
    {listState === 'error' ? <p className="settings-status error">{notice}</p> : null}

    {settings ? <>
      <div className="settings-lock">
        <Lock size={16}/>
        <span>
          <b>现在生效的是{current && current.version > 0 ? `第 ${current.version} 版` : '出厂设定'}</b>
          {settings.editable_note}
        </span>
      </div>

      {groups.map(group => <section className="settings-group" key={group.key}>
        <span className="section-label">{group.label}</span>
        <p className="settings-summary">{group.summary}</p>
        <div className="setting-list">
          {group.items.map(item => <ThresholdRow
            key={item.key}
            item={item}
            value={item.editable_key ? draft[item.editable_key] ?? '' : ''}
            onChange={next => setDraft(state => ({ ...state, [item.editable_key as string]: next }))}/>)}
        </div>
      </section>)}

      <section className="settings-group">
        <span className="section-label">存这一版</span>
        {/* 权限不够就先说，别等人填完理由按下保存才回一句 403。 */}
        {!canEdit ? <div className="settings-lock">
          <Lock size={16}/>
          <span>
            <b>你改不了这一段</b>
            {/* 角色读不到时不能凑一个「当前角色」出来——那句话读起来像页面坏了。
                真正管事的是有哪几档权限，所以角色和权限一起说，由 roleSentence 兜底。 */}
            改判定阈值要「确认」这一档权限。{roleSentence(session.membership?.role, session.scopes)}。
            上面的数字仍然能看——看得见的阈值和改得动的阈值是两件事。
            要改的话，找组织的管理员。
          </span>
        </div> : null}
        <div className="threshold-save">
          <label>
            为什么改
            <textarea value={reason} onChange={event => setReason(event.target.value)}
              placeholder="例：本项目单条素材曝光量普遍在 3000 上下，按 10000 判的话整轮都出不了结论。"/>
          </label>
          <small className="form-hint">
            理由会跟着这一版阈值一起存下来。三个月后有人问「为什么门槛是这个数」，
            这一栏就是答案。
          </small>
          {malformed ? <small className="form-hint error">
            阈值只能填正整数。要让某一格回到出厂设定，把它清空，不要填 0。
          </small> : null}
          <div className="threshold-save-actions">
            <button type="button" className="primary-button" disabled={!canSave} onClick={() => { void save() }}>
              {busy ? '保存中…' : '保存为新一版'}
            </button>
            <button type="button" className="secondary-button" disabled={!dirty || busy}
              onClick={() => { setDraft(baseline); setReason('') }}>
              撤销改动
            </button>
            {!dirty ? <small className="form-hint">还没有改动。</small> : null}
          </div>
          {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
        </div>
      </section>

      <section className="settings-group">
        <span className="section-label">改动史</span>
        <p className="settings-summary">
          阈值只增版本、从不原地改，所以每一版都还在。一条结论盖着「第 3 版」时，
          在这里能查到第 3 版当时是什么数、谁改的、为什么改。
        </p>
        {history.length ? <div className="setting-list">
          {history.map(set => <div className="setting-row" key={set.id}>
            <div className="setting-head">
              <b><History size={12}/> 第 {set.version} 版</b>
              <span className="setting-value">{formatDate(set.changed_at)}</span>
            </div>
            <p className="setting-effect">{set.reason}</p>
            <p className="setting-recommended">{describeValues(set, labels)}</p>
            <p className="setting-meta"><span>改的人：{set.changed_by}</span></p>
          </div>)}
        </div> : <div className="settings-missing">
          <p>还没有人改过。现在跑的是代码里的出厂设定，页面上标的是「按出厂阈值判定」。</p>
        </div>}
      </section>
    </> : null}
  </div>
}

/**
 * 一行。可写的给输入框，不可写的照旧只读并写清为什么不给改——画一个禁用的输入框
 * 比直接说「这里改不了」更恼人，而且会让人以为是权限不够而不是本来就不给改。
 */
function ThresholdRow({ item, value, onChange }: {
  item: ApiSettingItem
  value: string
  onChange: (next: string) => void
}) {
  const editable = Boolean(item.editable_key)
  return <div className={`setting-row${item.deviates ? ' tuned' : ''}`}>
    <div className="setting-head">
      <b>{item.label}</b>
      {editable
        ? <span className="threshold-input">
          <input type="text" inputMode="numeric" value={value} aria-label={item.label}
            placeholder={`留空 = 出厂设定 ${item.recommended}`}
            onChange={event => onChange(event.target.value)}/>
          <span className="setting-value">现在：{item.value}</span>
        </span>
        : <span className="setting-value">{item.value}</span>}
    </div>
    <p className="setting-effect">{item.effect}</p>
    <p className="setting-recommended">
      出厂推荐：{item.recommended}
      {item.deviates ? <em>（有人调过它）</em> : null}
      {editable && item.deviates
        ? <button type="button" className="link-button" onClick={() => onChange('')}>
          <RotateCcw size={11}/>改回出厂设定
        </button>
        : null}
    </p>
    <p className="setting-meta">
      <FileCode2 size={12}/>
      <span>依据：{item.basis}</span>
      {editable ? null : <span>这一条不给改：它是保护性上限或统计口径，不是判定标准。</span>}
    </p>
  </div>
}

/**
 * 表单初值。
 *
 * 只有被调过的格子（`deviates`）才填数字，其余留空。全部预填的话，一次保存会把七格
 * 都写死成显式值，那些本来「跟着代码默认走」的格子从此不再跟着走——而人只想改其中一格。
 */
function initialDraft(settings: ApiInsightSettings, current: ApiResolvedThresholds): Draft {
  const draft: Draft = {}
  const numbers = current as unknown as Record<string, number>
  for (const group of settings.groups) {
    for (const item of group.items) {
      if (!item.editable_key) continue
      const value = numbers[item.editable_key]
      draft[item.editable_key] = item.deviates && typeof value === 'number' ? String(value) : ''
    }
  }
  return draft
}

function describeValues(set: ApiThresholdSet, labels: Record<string, string>): string {
  const entries = Object.entries(set.values).filter(([, value]) => typeof value === 'number')
  if (!entries.length) return '这一版把所有格子都交回给出厂设定。'
  const parts = entries.map(([key, value]) => `${labels[key] ?? key} = ${value}`)
  // 「改了 N 格」会被读成「这一次动了 N 格」，可这里存的是**整版的快照**：
  // 只动了一格、其余两格沿用上一版，也照样列三条。版本本来就是一整套阈值，
  // 说成「这一版偏离出厂设定的格子」才对得上它实际的意思。
  return `这一版有 ${entries.length} 格不走出厂设定：${parts.join('、')}`
}

function isPositiveInteger(value: string): boolean {
  return /^\d+$/.test(value.trim()) && Number(value.trim()) > 0
}
