import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiExperience, type ApiExperienceLookup, type ApiExperienceMatch } from '../../../data/api'
import { ExperienceCard, applicabilityGroups } from './ExperienceCard'

/**
 * 「查」：下一轮要做素材，先看以前什么有效。
 *
 * 这一屏替代了原来的「投前洞察」页。那一页的五个视图——策略证据、创意建议、
 * 历史模式、风险与反例、引用记录——前四个只是同一批经验按不同条件筛，第五个是
 * 每条经验自己的引用历史（现在在卡片的展开层里）。它们不是五个功能，是一个功能
 * 的五种切法。
 *
 * 进来先自动按当前项目的条件筛一遍，不是给一张空表单等人填：查经验的人手上就是
 * 当前这个项目，让他把品牌产品再敲一遍是白费功夫。
 */
export function LookupView() {
  const { currentProject } = useProject()
  // 预填只填项目上真有的两格。渠道 / 广告类型 / 受众项目档案里没有，
  // 拿「目标」那种自由文本去卡 objective 会把大部分经验误筛掉——
  // 匹配是整值相等，一句「提升 30 天复购」对不上任何一条经验写的目标。
  const [lookup, setLookup] = useState<ApiExperienceLookup>({})
  const [matches, setMatches] = useState<ApiExperienceMatch[]>([])
  const [confirmed, setConfirmed] = useState<ApiExperience[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    setLookup({ brand: real(currentProject.brand), product: real(currentProject.product) })
  }, [currentProject.id, currentProject.brand, currentProject.product])

  // 下拉的选项从这个 Project 已有的经验里长出来，不写死一张渠道表。
  // 写死的话，人能选到「视频号」，但这里一条关于视频号的经验都没有，
  // 他会以为是筛坏了，而不是「本来就没有」。
  const options = useMemo(() => {
    const buckets: Record<string, Set<string>> = {}
    confirmed.forEach(experience => applicabilityGroups(experience.applicability).forEach(group => {
      buckets[group.key] ??= new Set()
      group.values.forEach(value => buckets[group.key].add(value))
    }))
    return buckets
  }, [confirmed])

  const runLookup = useCallback(async (next: ApiExperienceLookup) => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const [result, all] = await Promise.all([
        api.lookupExperiences(currentProject.id, clean(next)),
        api.listExperiences(currentProject.id, 'confirmed'),
      ])
      setMatches(result.items ?? [])
      setConfirmed(all.items ?? [])
      setListState('ready')
      setNotice('')
    } catch (cause) {
      setMatches([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '经验读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void runLookup(lookup) }, [runLookup, lookup])

  const update = (patch: Partial<ApiExperienceLookup>) => setLookup(current => ({ ...current, ...patch }))
  const scope = [lookup.brand, lookup.product, lookup.channel, lookup.ad_type, lookup.objective, lookup.audience]
    .filter(Boolean).join(' · ')

  // 空结果要分清三种空，因为该做的事完全不同：一个是勾开关，一个是放宽条件，
  // 一个是去复盘。说成「暂无数据」，人只会以为是系统坏了。
  //
  // 第一种最容易把人坑住：新 Project 留下来的头几条多半都是「👁 只是观察」，
  // 而这一屏默认不给观察级的。人辛苦复盘留完，回头一查还是空的，会以为白干了。
  // 「放宽条件」这种说法救不了他——要放宽的不是上面那排条件，是那个复选框。
  const observedHidden = !lookup.include_observed && confirmed.some(item => item.verdict === 'observed')
  const emptyHint = observedHidden
    ? '这些条件下没有能照着做的经验，但有「👁 只是观察」的被默认藏起来了——勾上「连只是观察的也看」能看到。它们没排除掉别的变量，只能当线索。'
    : confirmed.length
      ? '这些条件下还没有能用的经验。放宽条件再看看，或者去「复盘」里留一条。'
      : '这个 Project 还没有在用的经验。经验来自复盘——投完一轮、提交复盘、有人确认，它才会出现在这里。'

  return <div className="experience-lookup">
    <div className="core-flow-toolbar">
      <div>
        <span className="section-label">EXPERIENCE · LOOKUP</span>
        <h2>这一轮的条件下，以前什么有效</h2>
        <p>只列这个 Project 里在用的经验。默认不给「👁 只是观察」的——那些没排除掉别的变量，不能照着做。</p>
      </div>
      <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void runLookup(lookup) }}>
        <RefreshCw size={15}/>刷新
      </button>
    </div>

    <p className="lookup-context">
      {/* 没设条件时不把空占位塞进句子里，直说没筛。 */}
      {scope ? `按 ${scope} 筛出 ${matches.length} 条。` : `没设条件，这个 Project 里在用的一共 ${matches.length} 条。`}
      {lookup.include_observed ? '含只是观察的。' : ''}
    </p>

    {/* 和「投前」筛的是同一批经验，条件宽松时两边会给出一模一样的几条。不说清楚
        分工，人会以为其中一页是多余的。差别在把关：那一页敢少给，这一页是翻账用的，
        六个维度都能筛，还能把「👁 只是观察」的翻出来。 */}
    <p className="lookup-context">
      这里是完整目录，六个维度随便筛，勾上还能看「👁 只是观察」的。
      开工前要「这次照着做什么」的干净结论，去「投前」——那一页会替你把够不上的挡掉。
    </p>

    <div className="lookup-filters">
      <Field label="品牌" value={lookup.brand} options={options.brand} onChange={value => update({ brand: value })}/>
      <Field label="产品" value={lookup.product} options={options.product} onChange={value => update({ product: value })}/>
      <Field label="渠道" value={lookup.channel} options={options.channel} onChange={value => update({ channel: value })}/>
      <Field label="广告类型" value={lookup.ad_type} options={options.ad_type} onChange={value => update({ ad_type: value })}/>
      <Field label="目标" value={lookup.objective} options={options.objective} onChange={value => update({ objective: value })}/>
      <Field label="受众" value={lookup.audience} options={options.audience} onChange={value => update({ audience: value })}/>
      <label className="lookup-field">
        <small>内容特征</small>
        <input value={lookup.feature ?? ''} placeholder="例如：开场"
          onChange={event => update({ feature: event.target.value })}/>
      </label>
      <label className="lookup-toggle">
        <input type="checkbox" checked={Boolean(lookup.include_observed)}
          onChange={event => update({ include_observed: event.target.checked })}/>
        连只是观察的也看
      </label>
      <button type="button" className="text-button"
        onClick={() => setLookup({})}>清空条件</button>
    </div>

    {listState === 'loading' ? <p className="panel-empty">正在按条件筛…</p> : null}
    {listState === 'error' ? <p className="panel-empty">{notice || '经验读取失败，请重试。'}</p> : null}
    {listState === 'ready' && !matches.length ? <p className="panel-empty">{emptyHint}</p> : null}

    {matches.map(match => <ExperienceCard key={match.experience.id}
      experience={match.experience} matched={match.matched} citation={match.citation_text}/>)}
  </div>
}

/** 一格条件。有历史取值就给下拉，没有就只给输入框——下拉里一个选项都没有更让人困惑。 */
function Field({ label, value, options, onChange }: {
  label: string
  value: string | undefined
  options: Set<string> | undefined
  onChange: (value: string) => void
}) {
  // 选项是品牌、产品、渠道这些中文短语。不带比较函数的 sort 按 UTF-16 码元排，
  // 中文会排成人看不懂的顺序；localeCompare 按中文习惯排。
  const list = options ? [...options].sort((left, right) => left.localeCompare(right, 'zh-CN')) : []
  return <label className="lookup-field">
    <small>{label}</small>
    {list.length
      ? <select value={value ?? ''} onChange={event => onChange(event.target.value)}>
        <option value="">不限</option>
        {list.map(item => <option key={item} value={item}>{item}</option>)}
        {/* 预填的值可能不在历史取值里（项目品牌还没留下过经验）。
            不补这一项，select 会显示成「不限」，而 state 里其实卡着这个值。 */}
        {value && !list.includes(value) ? <option value={value}>{value}</option> : null}
      </select>
      : <input value={value ?? ''} onChange={event => onChange(event.target.value)} placeholder="不限"/>}
  </label>
}

/** 空字符串不发出去。后端把空串当「不限」，但发一堆空字段会让请求体读起来像设了条件。 */
/**
 * 项目档案里没填的那几格，后端存的不是空串而是一句占位话（「尚未关联产品」）。
 * 拿它去筛，等于在问「有没有哪条经验的适用产品正好叫『尚未关联产品』」——一条都
 * 匹配不上，而屏幕上那一格是填着字的，人只会以为这个项目真的没有可用经验。
 * 后端自己在别处就是这么排除的（internal/platform/project/mysql_store.go）。
 */
const placeholders = new Set(['尚未关联产品', '尚未设定项目目标', '尚未关联品牌'])

function real(value: string | undefined): string {
  const text = (value ?? '').trim()
  return placeholders.has(text) ? '' : text
}

function clean(lookup: ApiExperienceLookup): ApiExperienceLookup {
  const next: ApiExperienceLookup = {}
  // 这两格再过一遍占位词：预填之外还有别的路径能把项目档案的值塞进来。
  if (real(lookup.brand)) next.brand = real(lookup.brand)
  if (real(lookup.product)) next.product = real(lookup.product)
  if (lookup.channel?.trim()) next.channel = lookup.channel.trim()
  if (lookup.ad_type?.trim()) next.ad_type = lookup.ad_type.trim()
  if (lookup.objective?.trim()) next.objective = lookup.objective.trim()
  if (lookup.audience?.trim()) next.audience = lookup.audience.trim()
  if (lookup.feature?.trim()) next.feature = lookup.feature.trim()
  if (lookup.include_observed) next.include_observed = true
  return next
}
