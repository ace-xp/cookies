import { useMemo, useState } from 'react'
import { Save, X } from 'lucide-react'
import type {
  ApiApplicability, ApiConfidenceLevel, ApiContentBasis, ApiDataBasis,
  ApiExperience, ApiInsightCardType, ReviseExperienceBody,
} from '../data/api'
import { cardTypeLabels, cardTypeMeaning, confidenceLabels } from '../data/insightCard'
import { useProject } from '../context/ProjectContext'
import { useInsightAssets } from '../data/useInsightAssets'
import { shortId } from '../data/shortId'

/**
 * 补齐一条经验的依据。
 *
 * 复盘把结论留过来时只带得动结论本身——适用范围、数据依据、内容依据这三样是
 * 「凭什么这么说、在哪儿成立」，报告里没有对应字段，只能到这里由人补。补不了的话，
 * 经验库那句「没写数据依据」就永远是没写，而下游引用时看到的正是这句。
 *
 * 后端的修订是新增一条修订、把旧的标成被取代，不是就地改。所以提交时九个字段要整份
 * 发过去：表单里没露出来的（品牌、产品、统计窗口）也得从原件带上，否则新版本会把它们
 * 弄丢，而页面上不会有任何提示。
 */
export function ExperienceReviseForm({ experience, busy, onCancel, onSubmit }: {
  experience: ApiExperience
  busy: boolean
  onCancel: () => void
  onSubmit: (body: ReviseExperienceBody) => void
}) {
  const [conclusion, setConclusion] = useState(experience.conclusion)
  const [cardType, setCardType] = useState<ApiInsightCardType>(experience.card_type)
  const [confidence, setConfidence] = useState<ApiConfidenceLevel>(experience.confidence)
  const [action, setAction] = useState(experience.recommended_action)
  const [channels, setChannels] = useState(joinList(experience.applicability.channels))
  const [creativeTypes, setCreativeTypes] = useState(joinList(experience.applicability.creative_types))
  const [objectives, setObjectives] = useState(joinList(experience.applicability.objectives))
  const [audiences, setAudiences] = useState(joinList(experience.applicability.audiences))
  const [timeNote, setTimeNote] = useState(experience.applicability.time_range_note ?? '')
  const [assetCount, setAssetCount] = useState(numberText(experience.data_basis.asset_count))
  const [sampleSize, setSampleSize] = useState(numberText(experience.data_basis.sample_size))
  const [metrics, setMetrics] = useState(joinList(experience.data_basis.metrics))
  const [baseline, setBaseline] = useState(experience.data_basis.baseline ?? '')
  const [features, setFeatures] = useState(joinList(experience.content_basis.features))
  // 样例素材存的是素材 ID，只能从素材库里挑，不能手打。手打出来的串谁也验证不了，
  // 显示时又换不回名字，这一格就白填了。
  const [examples, setExamples] = useState<string[]>(experience.content_basis.example_asset_versions ?? [])
  const [assetQuery, setAssetQuery] = useState('')
  const [contentNote, setContentNote] = useState(experience.content_basis.note ?? '')
  const { currentProject } = useProject()
  const { assets, loading: assetsLoading } = useInsightAssets(currentProject.id)

  const visibleAssets = useMemo(() => {
    const keyword = assetQuery.trim().toLowerCase()
    if (!keyword) return assets
    return assets.filter(asset => `${asset.title} ${asset.id}`.toLowerCase().includes(keyword))
  }, [assets, assetQuery])

  // 早先手打进来的 ID 现在可能在素材库里找不到（打错了，或者素材被换了血缘）。
  // 换成选择器不能把它们悄悄抹掉——那等于替人删了他填过的东西。列出来让人自己决定。
  const strays = useMemo(() => examples.filter(id => !assets.some(asset => asset.id === id)),
    [examples, assets])

  const toggleExample = (id: string) => setExamples(current =>
    current.includes(id) ? current.filter(item => item !== id) : [...current, id])
  const [conditions, setConditions] = useState(experience.conditions.join('\n'))
  const [counterexamples, setCounterexamples] = useState(experience.counterexamples.join('\n'))
  const [reason, setReason] = useState('')

  const ready = conclusion.trim().length > 0 && reason.trim().length > 0

  const submit = () => {
    const applicability: ApiApplicability = {
      // 品牌和产品这一版没做输入框（当前项目里没人填过），但不能因此丢掉。
      brands: experience.applicability.brands,
      products: experience.applicability.products,
      channels: splitList(channels),
      creative_types: splitList(creativeTypes),
      objectives: splitList(objectives),
      audiences: splitList(audiences),
      time_range_note: timeNote.trim(),
    }
    const dataBasis: ApiDataBasis = {
      asset_count: parseCount(assetCount),
      sample_size: parseCount(sampleSize),
      // 统计窗口来自留下它的那份复盘，人不该在这里手改——改了就和来源报告对不上了。
      window_start: experience.data_basis.window_start,
      window_end: experience.data_basis.window_end,
      metrics: splitList(metrics),
      baseline: baseline.trim(),
    }
    const contentBasis: ApiContentBasis = {
      features: splitList(features),
      example_asset_versions: examples,
      note: contentNote.trim(),
    }
    onSubmit({
      expected_version: experience.version,
      reason: reason.trim(),
      conclusion: conclusion.trim(),
      conditions: splitLines(conditions),
      counterexamples: splitLines(counterexamples),
      card_type: cardType,
      confidence,
      recommended_action: action.trim(),
      applicability,
      data_basis: dataBasis,
      content_basis: contentBasis,
    })
  }

  return <div className="experience-revise">
    <span className="section-label">修订这条经验</span>
    <p>修订会新增一个版本，原来那条保留可查。下游引用记的是版本号，所以改过之后旧的引用不会跟着变。</p>

    <label className="experience-reason">
      <small>结论</small>
      <textarea value={conclusion} onChange={event => setConclusion(event.target.value)} rows={3}/>
    </label>

    <div className="revise-grid">
      <label><small>这条能被怎么用</small>
        <select value={cardType} onChange={event => setCardType(event.target.value as ApiInsightCardType)}>
          {(Object.keys(cardTypeLabels) as ApiInsightCardType[]).map(key => <option key={key} value={key}>{cardTypeLabels[key]}</option>)}
        </select>
      </label>
      <label><small>置信提示</small>
        <select value={confidence} onChange={event => setConfidence(event.target.value as ApiConfidenceLevel)}>
          {(Object.keys(confidenceLabels) as ApiConfidenceLevel[]).map(key => <option key={key} value={key}>{confidenceLabels[key]}</option>)}
        </select>
      </label>
    </div>
    <p className="revise-hint">{cardTypeMeaning[cardType]}</p>

    <label className="experience-reason"><small>建议动作（选「建议」时必填）</small>
      <input value={action} onChange={event => setAction(event.target.value)} placeholder="例如：下一批数字人口播先做钩子类型 A/B"/>
    </label>

    <span className="section-label">适用范围 · 这条结论在哪儿成立</span>
    <div className="revise-grid">
      <label><small>渠道（逗号分隔）</small><input value={channels} onChange={event => setChannels(event.target.value)} placeholder="抖音, 小红书"/></label>
      <label><small>创意形式</small><input value={creativeTypes} onChange={event => setCreativeTypes(event.target.value)} placeholder="数字人口播"/></label>
      <label><small>投放目标</small><input value={objectives} onChange={event => setObjectives(event.target.value)} placeholder="转化"/></label>
      <label><small>人群</small><input value={audiences} onChange={event => setAudiences(event.target.value)} placeholder="25-34 女性"/></label>
    </div>
    <label className="experience-reason"><small>时间说明</small>
      <input value={timeNote} onChange={event => setTimeNote(event.target.value)} placeholder="例如：仅适用于大促期间"/>
    </label>

    <span className="section-label">数据依据 · 凭多少数据这么说</span>
    <div className="revise-grid">
      <label><small>素材数</small><input inputMode="numeric" value={assetCount} onChange={event => setAssetCount(event.target.value)} placeholder="6"/></label>
      <label><small>样本量（展示数）</small><input inputMode="numeric" value={sampleSize} onChange={event => setSampleSize(event.target.value)} placeholder="120000"/></label>
      <label><small>看的是哪些指标</small><input value={metrics} onChange={event => setMetrics(event.target.value)} placeholder="CTR, CVR"/></label>
      <label><small>对照是什么</small><input value={baseline} onChange={event => setBaseline(event.target.value)} placeholder="同期未改钩子的素材"/></label>
    </div>

    <span className="section-label">内容依据 · 指的是素材上的什么</span>
    <div className="revise-grid">
      <label><small>特征</small><input value={features} onChange={event => setFeatures(event.target.value)} placeholder="钩子类型, 开场节奏"/></label>
    </div>

    <div className="asset-picker">
      <small>样例素材 · 这条结论看的是哪几条素材（已选 {examples.length} 条）</small>
      <input aria-label="搜索素材" value={assetQuery} onChange={event => setAssetQuery(event.target.value)} placeholder="按名字或 ID 搜素材"/>
      <div className="asset-picker-list" role="group" aria-label="样例素材">
        {assetsLoading ? <p className="revise-hint">正在读取当前 Project 的素材…</p> : null}
        {!assetsLoading && !assets.length ? <p className="revise-hint">当前 Project 还没有登记素材，先去「素材 · 总览」登记，这里才挑得到。</p> : null}
        {!assetsLoading && assets.length && !visibleAssets.length ? <p className="revise-hint">没有匹配的素材。</p> : null}
        {visibleAssets.map(asset => <label key={asset.id} className="asset-picker-item">
          <input type="checkbox" checked={examples.includes(asset.id)} onChange={() => toggleExample(asset.id)}/>
          <span><b>{asset.title}</b><small>v{asset.revision} · {shortId(asset.id)}</small></span>
        </label>)}
      </div>
      {strays.length ? <p className="revise-hint">
        另有 {strays.length} 个填过但在当前素材库里找不到的 ID（{strays.map(shortId).join('、')}），会照原样保留。
        要去掉的话，点一下它自己：
        {strays.map(id => <button key={id} type="button" className="text-button" onClick={() => toggleExample(id)}>{shortId(id)}</button>)}
      </p> : null}
    </div>
    <label className="experience-reason"><small>内容备注</small>
      <input value={contentNote} onChange={event => setContentNote(event.target.value)} placeholder="例如：三条样例都是前 3 秒出人脸"/>
    </label>

    <label className="experience-reason"><small>适用条件（一行一条）</small>
      <textarea value={conditions} onChange={event => setConditions(event.target.value)} rows={3} placeholder="例如：单条素材展示量达到 5 万以上才适用"/>
    </label>
    {/* 这一格的名字要和卡片上「还缺：风险与反例」那句、以及证据层的
        「什么情况下不成立」对得上。只写「反例」的话，人是照着卡片的提示点进来补
        格子的，在表单里找不到叫这个名字的格子，会以为补不了。 */}
    <label className="experience-reason"><small>风险与反例 · 什么情况下不成立（一行一条）</small>
      <textarea value={counterexamples} onChange={event => setCounterexamples(event.target.value)} rows={2} placeholder="例如：品牌片上不成立"/>
    </label>
    <label className="experience-reason"><small>修订理由（必填，写进审计记录）</small>
      <textarea value={reason} onChange={event => setReason(event.target.value)} rows={2} placeholder="例如：补齐来源报告里的样本量与指标口径"/>
    </label>

    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={busy || !ready} onClick={submit}><Save size={15}/>{busy ? '提交中…' : '保存为新修订'}</button>
      <button className="secondary-button full" disabled={busy} onClick={onCancel}><X size={15}/>取消</button>
    </div>
  </div>
}

function joinList(values?: string[]): string {
  return (values ?? []).join(', ')
}

function splitList(text: string): string[] {
  // 中英文逗号、顿号都收。人在中文输入法下打出来的是「，」和「、」，
  // 只认英文逗号的话，整串会被当成一个值存进去。
  return text.split(/[,，、]/).map(item => item.trim()).filter(Boolean)
}

function splitLines(text: string): string[] {
  return text.split('\n').map(item => item.trim()).filter(Boolean)
}

function numberText(value?: number): string {
  return value ? String(value) : ''
}

function parseCount(text: string): number {
  const parsed = Number(text.replace(/[\s,，]/g, ''))
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : 0
}
