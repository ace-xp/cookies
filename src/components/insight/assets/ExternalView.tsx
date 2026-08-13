import { useCallback, useEffect, useState } from 'react'
import { Plus, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiExternalAsset,
  type ApiExternalPurpose,
  type ApiFeatureValue,
  type ApiInsightAssetType,
} from '../../../data/api'
import { formatDate } from '../analysis/format'

// 和后端 maxProbeFeatures 对齐（similar.go）。前端拦一道是为了让「超了」这件事在
// 按下之前就说清楚——后端超限只回一个 ErrInvalidRequest，界面上会变成「导入失败」
// 这种什么也没解释的话。
const maxExternalFeatures = 20

const purposeLabels: Record<ApiExternalPurpose, string> = {
  benchmark: '同类参照',
  reference: '解释用例',
}

const purposeHints: Record<ApiExternalPurpose, string> = {
  benchmark: '拿来回答「同类素材大概什么水平」。',
  reference: '拿来当正例或反例，解释本轮某一条结论。',
}

const assetTypeLabels: Record<ApiInsightAssetType, string> = {
  xiaohongshu_note: '小红书图文',
  wechat_article: '公众号图文',
  brand_ad: '品牌广告',
  digital_human_ad: '数字人效果广告',
  preroll_ad: '广告前贴',
  hit_replica_ad: '爆款复刻',
}

/**
 * 「外部素材」视图。平台外的素材以**证据**的身份收在这里。
 *
 * `window_end` 不给人手填的口子：它决定留存期限从哪天算起，填错了这条素材会比
 * 该留的时间多留或少留。从页面当前窗口取，和分析页同一套。
 */
export function ExternalView({ window }: { window: { start: string; end: string } }) {
  const { currentProject } = useProject()
  const [items, setItems] = useState<ApiExternalAsset[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  const [title, setTitle] = useState('')
  const [sourceNote, setSourceNote] = useState('')
  const [purpose, setPurpose] = useState<ApiExternalPurpose | ''>('')
  const [purposeNote, setPurposeNote] = useState('')
  const [assetType, setAssetType] = useState('')
  // 人对这条证据标的变量。**这一格以前不存在**，于是收进来的每一条都是「既没有
  // 文件、也没有变量」的一行标题——而整屏文案都在讲「原片清掉之后只剩人标过的
  // 变量」。没有这一格的话，那句话说的是一个永远为空的东西。
  //
  // 用一整块自由文本而不是键值对表单：外部素材的变量既不进归因也不进相似度检索
  // （后端一律按自由文本存），给它一套和平台内素材一样的结构化表单，只会诱使人
  // 拿它去和平台内素材比。
  const [features, setFeatures] = useState('')
  const featureCount = Object.keys(parseFeatures(features)).length

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const page = await api.listExternalAssets(currentProject.id)
      setItems(page.items)
      setListState('ready')
    } catch (cause) {
      setItems([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '外部素材读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])

  const submit = async () => {
    if (!currentProject.id || !title.trim() || !purpose) return
    setBusy(true)
    setNotice('')
    try {
      const parsed = parseFeatures(features)
      const created = await api.importExternalAsset(currentProject.id, {
        title: title.trim(),
        source_note: sourceNote.trim(),
        purpose,
        purpose_note: purposeNote.trim() || undefined,
        asset_type: assetType || undefined,
        window_end: window.end,
        features: Object.keys(parsed).length ? parsed : undefined,
      })
      setTitle('')
      setSourceNote('')
      setPurpose('')
      setPurposeNote('')
      setAssetType('')
      setFeatures('')
      await load()
      const count = Object.keys(created.features ?? {}).length
      setNotice(count
        ? `已收下「${created.title}」，记了 ${count} 条变量。`
        : `已收下「${created.title}」。它现在只有标题和用途——没标变量的话，`
          + `半年后再看这一条，谁也说不清当初看中的是什么。`)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '导入失败，请稍后重试。')
    } finally {
      setBusy(false)
    }
  }

  return <section className="external-view">
    <p className="prelaunch-disclosure">
      这里的素材是<b>证据</b>，不是资产。它们不会进共享素材库、不能被拿去投放，
      也不能改——要改就重新导一份。收它们只有一个用处：解释本轮结果时有个参照。
      <br/>
      {/* 「上传原片」这件事界面上一直没有入口，后端也没有接口。以前不说，于是
          列表上那句「原件将在 X 前后清掉」让人以为文件在系统里躺着。 */}
      <b>这里不能上传文件。</b>系统存的是你写下的东西：标题、来源、用途，
      以及你在它身上看到的那些变量。原片放在哪儿，写进「来源说明」里。
    </p>

    <div className="external-columns">
      <div className="external-list">
        <div className="prelaunch-filterbar">
          <span>{items.length} 条外部证据 · 本轮窗口结束日 {formatDate(window.end)}</span>
          <button type="button" className="secondary-button" disabled={listState === 'loading'}
            onClick={() => { void load() }}><RefreshCw size={15}/>刷新</button>
        </div>

        {listState === 'loading' ? <div className="panel-empty">正在读取…</div>
          : listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div>
          : !items.length ? <div className="panel-empty">
            还没有收过外部素材。看到值得当参照的，用右边的表单收一条——用途要选，
            变量要标：不标变量的话，收进来的就只是一行标题。
          </div>
          : <ul className="external-items">
            {items.map(item => <li key={item.id}>
              <div className="external-head">
                <strong>{item.title}</strong>
                <span className="external-purpose">{purposeLabels[item.purpose]}</span>
              </div>
              {/* 留存期只管**原件**。以前不管有没有文件都写一句「留到 X」，于是一条
                  只填了标题和用途、根本没有文件的登记，看起来和「存了原片」一模一样
                  ——那个到期日说的是一件不会发生的事。没有 storage_key 就直说没有文件。 */}
              <span className="external-retention">
                {item.storage_key
                  ? item.original_purged
                    ? '原件已按留存期删掉，只剩下面标的变量'
                    : `原件留到 ${formatDate(item.retention_until)}`
                  : '没存文件，只有下面这些标注'}
              </span>
              {item.source_note ? <small>来源：{item.source_note}</small> : null}
              {item.purpose_note ? <small>{item.purpose_note}</small> : null}
              {item.asset_type ? <small>{assetTypeLabels[item.asset_type] ?? item.asset_type}</small> : null}
              {/* 变量是这条证据的实际内容，不能只在详情里给。列表上看不到的话，
                  「收进来的东西有没有用」这个问题要一条条点开才答得了。 */}
              {Object.keys(item.features ?? {}).length
                ? <ul className="external-features">
                  {Object.entries(item.features).map(([key, value]) =>
                    <li key={key}><b>{key}</b>{featureText(value)}</li>)}
                </ul>
                : <small className="external-empty-features">
                  没人标过变量。这一条现在只是一行标题，解释不了任何结论。
                </small>}
            </li>)}
          </ul>}
      </div>

      <form className="external-form" onSubmit={event => { event.preventDefault(); void submit() }}>
        <span className="section-label">收一条外部素材</span>

        <label>标题<input value={title} onChange={event => setTitle(event.target.value)}
          placeholder="这条素材叫什么" required/></label>

        <label>来源说明<input value={sourceNote} onChange={event => setSourceNote(event.target.value)}
          placeholder="哪儿看到的，例如「竞品 A 的抖音信息流」"/></label>

        <fieldset className="external-purpose-field">
          <legend>用途</legend>
          {(['benchmark', 'reference'] as ApiExternalPurpose[]).map(value => <label key={value}>
            <input type="radio" name="external-purpose" value={value} checked={purpose === value}
              onChange={() => setPurpose(value)}/>
            {purposeLabels[value]}<small>{purposeHints[value]}</small>
          </label>)}
          <small className="form-hint">
            用途要选，因为留存期到了之后，「当初为什么收这个」只有这一栏说得清。
          </small>
        </fieldset>

        <label>补充说明<textarea value={purposeNote} onChange={event => setPurposeNote(event.target.value)}
          placeholder="为什么这一条值得留（选填）"/></label>

        <label>素材类型<select value={assetType} onChange={event => setAssetType(event.target.value)}>
          <option value="">不指定</option>
          {Object.entries(assetTypeLabels).map(([value, label]) =>
            <option key={value} value={value}>{label}</option>)}
        </select></label>

        <label>你在这条素材身上看到了什么
          <textarea value={features} onChange={event => setFeatures(event.target.value)}
            placeholder={'一行一条，冒号前是名字，冒号后是值：\n时长：15 秒\n开头：真人出镜说话\n卖点：先讲价格'}/>
        </label>
        <small className="form-hint">
          这一栏是这条证据留下来的<b>唯一</b>东西——外部素材不进归因、也不参与相似素材检索，
          它对本轮结论的用处全在你标的这几条上。不标的话，列表里就只剩一行标题。
          {featureCount ? `已经写了 ${featureCount} 条。` : ''}
        </small>
        {featureCount > maxExternalFeatures ? <small className="form-hint error">
          最多 {maxExternalFeatures} 条，现在写了 {featureCount} 条。挑最能说明问题的留下。
        </small> : null}

        <button type="submit" className="secondary-button"
          disabled={busy || !title.trim() || !purpose || featureCount > maxExternalFeatures}>
          <Plus size={15}/>{busy ? '正在收…' : '收下这条'}
        </button>

        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </form>
    </div>
  </section>
}

/**
 * 把「一行一条，冒号分隔」的自由文本切成键值。
 *
 * 中英文冒号都认：中文输入法下打出来的是「：」，而按半角冒号写的人也不该被判错。
 * 切第一个冒号，后面的原样留着——「时长：15 秒（含 3 秒片头）」这种值里本来就可能带冒号。
 *
 * 没有冒号的行整行当名字、值留空：那多半是人只想记一个标签（「竖版」），
 * 丢掉它比留一个空值更糟。
 */
function parseFeatures(raw: string): Record<string, string> {
  const result: Record<string, string> = {}
  for (const line of raw.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const at = trimmed.search(/[:：]/)
    const key = (at < 0 ? trimmed : trimmed.slice(0, at)).trim()
    const value = at < 0 ? '' : trimmed.slice(at + 1).trim()
    if (key) result[key] = value
  }
  return result
}

// 外部素材的变量后端一律按自由文本存（external.go 里写死 FeatureKindText），
// 所以这里只需要 text；其余分支是给「将来有人改了存法」留的兜底，不做花哨渲染。
function featureText(value: ApiFeatureValue): string {
  if (value.text) return value.text
  if (value.terms?.length) return value.terms.join('、')
  if (typeof value.number === 'number') return String(value.number)
  if (typeof value.bool === 'boolean') return value.bool ? '是' : '否'
  return '（空）'
}
