import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, Layers3, Lightbulb, RefreshCw, ScrollText, Sparkles, UserCheck, Wand2 } from 'lucide-react'
import { useProject } from '../context/ProjectContext'
import {
  api,
  ApiRequestError,
  type ApiAnalysisRun,
  type ApiConfidence,
  type ApiReviewState,
  type ApiFeatureInput,
  type ApiFeatureMatrix,
  type ApiFeatureMatrixCell,
  type ApiFeatureSchema,
  type ApiFeatureSource,
  type ApiFeatureValue,
  type ApiFeatureValueKind,
  type ApiInsightAsset,
  type ApiInsightAssetFeature,
  type ApiInsightAssetType,
} from '../data/api'
import { admissibleForAttribution, featureSourceLabel } from '../data/featureSource'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'

/**
 * 内容分析（03 §5、AM-004~006，导航见 19 §5.2）。
 *
 * 六类素材各有一套特征体系，不得混用——03 §MVP② 把「不把视频钩子字段套到公众号
 * 文章」列为明文验收。所以这里的每个类型标签只渲染该类型自己的字段，多素材看矩阵、
 * 单素材看逐项拆解（20 §4.1、22 §6.2 要求「不要堆洞察卡」）。
 */
type ViewTarget = ApiInsightAssetType | 'single'

const viewTargets: Record<string, ViewTarget> = {
  小红书: 'xiaohongshu_note',
  小红书图文: 'xiaohongshu_note',
  公众号: 'wechat_article',
  公众号图文: 'wechat_article',
  品牌广告: 'brand_ad',
  数字人: 'digital_human_ad',
  广告前贴: 'preroll_ad',
  爆款复刻: 'hit_replica_ad',
  单素材拆解: 'single',
}

const assetTypeLabels: Record<ApiInsightAssetType, string> = {
  xiaohongshu_note: '小红书图文',
  wechat_article: '公众号图文',
  brand_ad: '品牌广告',
  digital_human_ad: '数字人效果广告',
  preroll_ad: '广告前贴',
  hit_replica_ad: '爆款复刻',
}

const assetTypeOrder: ApiInsightAssetType[] = [
  'xiaohongshu_note', 'wechat_article', 'brand_ad', 'digital_human_ad', 'preroll_ad', 'hit_replica_ad',
]

const confidenceLabels: Record<ApiConfidence, string> = { low: '低', medium: '中', high: '高' }

// 一格里同时有多种来源时的显示顺序。归因只认前两种，所以它们排在前面。
const sourceOrder: ApiFeatureSource[] = ['derived', 'human', 'ai']

// 人工那一行的措辞。authored 不能写成「推翻 AI」——AI 根本没提过这一项，
// 没有东西可推翻；也不能写成「认可」，人并没有认可过任何机器结论。
const reviewLabels: Record<ApiReviewState, string> = {
  pending: '待复核',
  confirmed: '认可 AI',
  rejected: '推翻 AI',
  authored: '人工填写',
}

const statusLabels: Record<string, string> = {
  awaiting_data: '待数据', awaiting_match: '待匹配', analysable: '可分析', analysing: '分析中',
  pending_confirmation: '待确认', confirmed: '已确认', needs_review: '待复审', retired: '已失效',
}

export function ContentAnalysisPage({ state, activeView }: { state: DataState; activeView: string }) {
  const { currentProject } = useProject()
  const target = viewTargets[activeView] ?? 'single'
  const [schemas, setSchemas] = useState<ApiFeatureSchema[]>([])
  const [assets, setAssets] = useState<ApiInsightAsset[]>([])
  const [matrix, setMatrix] = useState<ApiFeatureMatrix | null>(null)
  const [features, setFeatures] = useState<ApiInsightAssetFeature[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [editingKey, setEditingKey] = useState('')
  const [draft, setDraft] = useState('')
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  // 图文类的正文不在库里（洞察只存身份和状态，不存正文），提取时必须由人带上。
  // 视频类不用：画面交给多模态自己去看，这个框只是选填的补充。
  const [content, setContent] = useState('')
  const [note, setNote] = useState('')
  const [analyzing, setAnalyzing] = useState(false)
  const [runs, setRuns] = useState<ApiAnalysisRun[]>([])

  const loadList = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    setNotice('')
    try {
      const schemaPage = await api.listFeatureSchemas(currentProject.id)
      setSchemas(schemaPage.items)
      // 类型标签只取该类型的素材，单素材拆解取全部——每个标签都真实换一批数据（22 §8.3）。
      const page = await api.listInsightAssets(
        currentProject.id,
        target === 'single' ? {} : { assetTypes: [target] },
      )
      // 已失效素材不进对比：它的结论已经不作数，摆在矩阵里只会让人误读差异。
      const live = page.items.filter(asset => asset.analysis_status !== 'retired')
      setAssets(live)
      if (target !== 'single' && live.length) {
        setMatrix(await api.getFeatureMatrix(currentProject.id, live.map(asset => asset.id)))
      } else {
        setMatrix(null)
      }
      setListState('ready')
    } catch (cause) {
      setAssets([])
      setMatrix(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '内容分析读取失败。')
    }
  }, [currentProject.id, target])

  useEffect(() => { void loadList() }, [loadList])
  // 换素材就把草稿全部清掉。上一条素材的正文留在框里，一不小心就会提到另一条素材头上。
  useEffect(() => { setEditingKey(''); setDraft(''); setContent(''); setNote('') }, [target, selectedId])

  const breakdownAssets = assets

  useEffect(() => {
    const ids = breakdownAssets.map(asset => asset.id)
    setSelectedId(current => (ids.includes(current) ? current : ids[0] ?? ''))
  }, [breakdownAssets.map(asset => asset.id).join('|')])

  const selectedAsset = breakdownAssets.find(asset => asset.id === selectedId)

  const loadFeatures = useCallback(async () => {
    if (target !== 'single' || !selectedAsset) {
      setFeatures([])
      return
    }
    try {
      const page = await api.listInsightAssetFeatures(currentProject.id, selectedAsset.id)
      setFeatures(page.items)
    } catch {
      setFeatures([])
    }
  }, [currentProject.id, target, selectedAsset?.id])

  useEffect(() => { void loadFeatures() }, [loadFeatures])

  /**
   * 分析历史（03 §82「数据与方法」）。失败的任务也要列出来：只显示成功的，
   * 成功率看上去永远是 100%，运营面上就没法判断供应商是不是出问题了（§310）。
   */
  const loadRuns = useCallback(async () => {
    if (target !== 'single' || !selectedAsset) {
      setRuns([])
      return
    }
    try {
      const page = await api.listInsightAssetAnalysisRuns(currentProject.id, selectedAsset.id, 10)
      setRuns(page.items)
    } catch {
      setRuns([])
    }
  }, [currentProject.id, target, selectedAsset?.id])

  useEffect(() => { void loadRuns() }, [loadRuns])

  const typeSchema = schemas.find(schema =>
    schema.asset_type === (target === 'single' ? selectedAsset?.asset_type : target))

  // 「这一条，模型能自己去看画面吗」——决定正文框是必填还是选填。
  // 两个条件缺一不可：类型是视频（后端 IsVideo），而且指向了素材库里的文件。
  // 只满足前一条的，模型没有文件可看，人还是得写一段描述（下面那条提示会说明）。
  const modelWatchesTheVideo = Boolean(
    typeSchema?.is_video && selectedAsset?.platform_asset_id && selectedAsset?.platform_asset_version,
  )

  /**
   * 写人工层。后端的 PATCH 是整层替换（ReplaceLayer: human），所以要把已有的人工结论
   * 一起带上，否则会把别人此前的判断抹掉。AI 那一层始终不动（AM-006、§14）。
   */
  const writeHumanLayer = useCallback(async (key: string, value: ApiFeatureValue, verdict: 'confirmed' | 'rejected' | 'authored', reason: string) => {
    if (!selectedAsset) return
    const kept: ApiFeatureInput[] = features
      .filter(feature => feature.source === 'human' && feature.key !== key)
      .map(feature => ({ key: feature.key, value: feature.value, review_state: feature.review_state }))
    try {
      await api.patchInsightAssetFeatures(currentProject.id, selectedAsset.id, {
        expected_version: selectedAsset.version,
        features: [...kept, { key, value, review_state: verdict }],
        reason,
      })
      setEditingKey('')
      setDraft('')
      await loadList()
      await loadFeatures()
      // loadList 会清空提示，所以放在它之后再写，否则这条反馈一闪就没了。
      setNotice(verdict === 'confirmed'
        ? `已认可「${key}」，人工结论已单独记一行。`
        : verdict === 'authored'
          ? `已填写「${key}」。AI 没提过这一项，所以记的是人工原创，不是推翻。`
          : `已推翻「${key}」并写入人工结论。`)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '写入人工结论失败。')
    }
  }, [currentProject.id, features, selectedAsset, loadList, loadFeatures])

  const identifyType = useCallback(async (assetType: ApiInsightAssetType) => {
    if (!selectedAsset) return
    try {
      await api.identifyInsightAssetType(currentProject.id, selectedAsset.id, {
        expected_version: selectedAsset.version,
        asset_type: assetType,
        source: 'human',
        reason: '人工判定素材类型',
      })
      await loadList()
      setNotice(`已判定为「${assetTypeLabels[assetType]}」，接下来才谈得上提取特征。`)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : '类型判定失败。')
    }
  }, [currentProject.id, selectedAsset, loadList])

  /**
   * AI 提特征（AM-005）。**只有人点这个按钮才会调模型**——登记素材时自动排队，
   * 会把复核队列灌满没人看的结果，而复核是这套东西唯一的质量闸门。
   *
   * 提出来的结果只写 AI 那一层，状态停在待确认，不会自动变成已确认的结论（09 §8）。
   */
  const analyze = useCallback(async () => {
    if (!selectedAsset) return
    setAnalyzing(true)
    try {
      const result = await api.analyzeInsightAsset(currentProject.id, selectedAsset.id, {
        expected_version: selectedAsset.version,
        // 视频类可以空着——后端会去问多模态。填了也不浪费，会当成人的补充一起给模型。
        content: content.trim(),
        note: note.trim() || undefined,
      })
      await loadList()
      await loadFeatures()
      await loadRuns()
      const dropped = result.dropped_fields?.length ? `，另有 ${result.dropped_fields.length} 项没采信：${result.dropped_fields.join('；')}` : ''
      // loadList 会清空提示，所以放在它之后再写。
      setNotice(`提取完成，AI 给出 ${result.features.length} 项特征${dropped}。这些还只是待确认，需要逐项认可或推翻。`)
    } catch (cause) {
      // 「模型正在看这条视频」不是失败：这一趟压根没跑，后端也没记留痕
      // （understanding.go），所以不刷新历史，也不说「具体原因见……」——
      // 那边什么都没有，让人去翻只会更困惑。
      if (cause instanceof ApiRequestError && cause.code === 'UNDERSTANDING_PENDING') {
        setNotice(`${cause.message}。视频要先由多模态看一遍关键帧，通常一两分钟。`)
        return
      }
      // 400 是「这个请求本身不成立」（缺正文、模型看不了这条视频），后端在跑之前
      // 就退回来了，没有留痕。指路「见右侧最新一条」会让人去翻一条不存在的记录，
      // 而报错里已经把原因说全了。
      if (cause instanceof ApiRequestError && cause.status === 400) {
        setNotice(cause.message)
        return
      }
      // 剩下的是真的跑过并且失败了，后端记了一行，所以照样刷新历史，让人看得见。
      // 接口按平台惯例只回一句笼统的错误码文案，真正的原因在那条记录里。
      await loadRuns()
      setNotice(`${cause instanceof Error ? cause.message : '提取失败'}。具体原因见右侧「数据与方法」最新一条。`)
    } finally {
      setAnalyzing(false)
    }
  }, [currentProject.id, selectedAsset, content, note, loadList, loadFeatures, loadRuns])

  const header = target === 'single'
    ? { title: '单素材逐项拆解', lead: '一条素材的全部特征，按它自己那套体系分组；AI 与人工两层分开显示。' }
    : { title: `${assetTypeLabels[target]}的特征矩阵`, lead: '同类素材横向对比同一批变量，才知道差异出在哪一项，而不是只知道谁表现好。' }

  return <StateBoundary state={state} onRetry={() => { void loadList() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div><span className="section-label">CONTENT ANALYSIS</span><h2>{header.title}</h2><p>当前 Project：{currentProject.name}。{header.lead}</p></div>
          <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void loadList() }}><RefreshCw size={15}/>刷新</button>
        </div>

        {listState === 'loading' ? <div className="panel-empty">正在读取内容分析数据…</div> : null}
        {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}

        {listState === 'ready' && target !== 'single' ? <>
          <div className="prelaunch-filterbar">
            <div><span className="section-label">{typeSchema?.label ?? assetTypeLabels[target]}</span></div>
            <span>{assets.length} 个素材 · {typeSchema?.fields.length ?? 0} 个可提取变量 · {matrix?.rows.filter(row => row.cells?.length).length ?? 0} 个已有取值</span>
          </div>
          {matrix && matrix.rows.some(row => row.cells?.length)
            ? <FeatureMatrixTable matrix={matrix}/>
            : <div className="panel-empty">{assets.length
              ? '这批素材还没有提取过特征。类型识别完成后要先跑一次提取，矩阵才有内容。'
              : `当前 Project 还没有${assetTypeLabels[target]}素材。右侧列出的是这类素材能问出的全部变量。`}</div>}
        </> : null}

        {listState === 'ready' && target === 'single' ? <>
          <div className="content-asset-strip">
            {breakdownAssets.length
              ? breakdownAssets.map(asset => <button key={asset.id} className={asset.id === selectedId ? 'active' : ''} onClick={() => setSelectedId(asset.id)}>
                {asset.title} · v{asset.revision}
              </button>)
              : <span className="panel-empty">当前 Project 还没有可分析素材。</span>}
          </div>
          {selectedAsset && !selectedAsset.asset_type
            ? <div className="content-breakdown">
              <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>类型待识别</small>不知道这是哪类素材，就不知道该问哪套变量。先判定类型（AM-004），再谈特征提取。</span></div>
              <div className="content-asset-strip">
                {assetTypeOrder.map(assetType => <button key={assetType} onClick={() => { void identifyType(assetType) }}>判定为 {assetTypeLabels[assetType]}</button>)}
              </div>
            </div>
            : null}
          {selectedAsset?.asset_type && typeSchema ? <div className="content-extract">
            <div><span className="section-label">AI 提特征</span></div>
            <p>
              照「{typeSchema.label}」自己那套 {typeSchema.fields.length} 个变量提问，不会把别类素材的字段套过来。
              {/* 视频但没有文件引用的那种情况这里不说话——下面那条提示专门讲它，
                  在这儿再说一句「本体就是那段文字」是错的，视频的本体是画面。 */}
              {modelWatchesTheVideo
                ? '这是视频，画面由模型自己去看素材库里的那个文件，你不用再写一遍——写了也行，会当成你的补充一起给模型。'
                : typeSchema.is_video ? '' : '这类素材的本体就是那段文字，洞察这边没存正文，要提取就得先贴进来。'}
              提出来的结果只进 AI 那一层，仍然要逐项人工认可才算数。
            </p>
            {typeSchema.is_video && !modelWatchesTheVideo
              ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
                <small>模型看不到画面</small>这条视频没指向素材库里的文件，只有从创意导入的素材才带这个引用。
                现在只能靠你写一段画面描述或脚本——那样模型分析的是这段描述，不是这条视频。
              </span></div>
              : null}
            <textarea
              aria-label="素材内容"
              rows={6}
              value={content}
              placeholder={modelWatchesTheVideo
                ? '选填：模型从画面里看不出来的背景，例如「这条是给老客户的续费提醒」'
                : '把这条素材的正文、口播稿或分镜说明贴进来'}
              onChange={event => setContent(event.target.value)}
            />
            <input
              aria-label="补充说明"
              value={note}
              placeholder="补充说明（选填）：投放场景、已知背景，会一起给到模型"
              onChange={event => setNote(event.target.value)}
            />
            <div className="actions">
              <button className="primary-button" disabled={analyzing || (!content.trim() && !modelWatchesTheVideo)} onClick={() => { void analyze() }}>
                <Wand2 size={15}/>{analyzing ? '提取中…' : '提取特征'}
              </button>
              <span>只有点这个按钮才会真的调一次模型。登记素材时不会自动提——那样只会把待复核的结果堆满，没人看得完。</span>
            </div>
          </div> : null}
          {selectedAsset?.asset_type && typeSchema ? <FeatureBreakdown
            schema={typeSchema}
            features={features}
            editingKey={editingKey}
            draft={draft}
            onDraft={setDraft}
            onEdit={key => { setEditingKey(key); setDraft('') }}
            onCancel={() => { setEditingKey('') }}
            onConfirm={(key, value) => { void writeHumanLayer(key, value, 'confirmed', '人工认可 AI 提取结果') }}
            onReject={(key, value, hadAi) => {
              void writeHumanLayer(key, value, hadAi ? 'rejected' : 'authored',
                hadAi ? '人工推翻并改写 AI 提取结果' : '人工填写 AI 未提取的特征')
            }}
          /> : null}
        </> : null}
      </section>

      <aside className="prelaunch-detail">
        {target === 'single' && selectedAsset ? <>
          <span className="section-label">素材</span><h3>{selectedAsset.title}</h3>
          <p>第 {selectedAsset.revision} 版 · {statusLabels[selectedAsset.analysis_status] ?? selectedAsset.analysis_status}</p>
          <div className="prelaunch-fact"><Layers3 size={17}/><span><small>内容类型</small><b>
            {selectedAsset.asset_type ? assetTypeLabels[selectedAsset.asset_type] : '待识别'}
            {selectedAsset.asset_type_source === 'ai' ? `（AI 识别 · 置信 ${confidenceLabels[selectedAsset.asset_type_confidence ?? 'low']}）`
              : selectedAsset.asset_type_source === 'human' ? '（人工判定）' : ''}
          </b></span></div>
          <div className="prelaunch-fact"><Sparkles size={17}/><span><small>已提取</small><b>
            {sourceOrder.map(source =>
              `${featureSourceLabel[source]} ${features.filter(feature => feature.source === source).length} 项`).join(' · ')}
            {typeSchema ? ` · 这类素材共 ${typeSchema.fields.length} 个变量` : ''}
          </b></span></div>
          {/* 能进归因的有几项，要单独说一句。前面那行三个数并排，人会把它们加起来
              当成「提取了多少」，而其中模型猜的那部分是不能拿去做归因的。 */}
          <div className="prelaunch-fact"><UserCheck size={17}/><span><small>其中能进归因的</small><b>
            {features.filter(feature => admissibleForAttribution(feature.source)).length} 项。
            模型猜的那些只能参考——要让它们算数，得有人看过并确认。
          </b></span></div>
          {selectedAsset.analysis_status_reason
            ? <div className="prelaunch-fact"><Lightbulb size={17}/><span><small>最近一次状态说明</small><b>{selectedAsset.analysis_status_reason}</b></span></div>
            : null}
          <div className="prelaunch-fact"><UserCheck size={17}/><span><small>两层为什么不合并</small><b>人工结论单独成行，后台再跑一次提取也不会盖掉它；AI 那一行也保留，方便回头看机器当时判断成了什么。</b></span></div>
          <div className="analysis-run-list">
            <span className="section-label">数据与方法</span>
            {runs.length
              ? runs.map(run => <div className={`analysis-run ${run.status}`} key={run.id}>
                <b>{runStatusLabels[run.status] ?? run.status} · {formatTime(run.started_at)}</b>
                <small>
                  {run.skill_id ? `${run.skill_id}@${run.skill_version ?? '—'}` : '未记录 Skill'}
                  {' · '}{run.model_alias ?? '未记录模型'}{run.model_version ? `（${run.model_version}）` : ''}
                  {' · '}耗时 {formatLatency(run.latency_ms)}
                </small>
                {run.status === 'succeeded' ? <small>
                  提出 {run.feature_count} 项特征
                  {run.dropped_fields?.length ? ` · 有 ${run.dropped_fields.length} 项没采信：${run.dropped_fields.join('；')}` : ''}
                </small> : null}
                {run.status === 'failed' ? <small>{readableError(run.error_message) || '没有记下失败原因。'}</small> : null}
              </div>)
              : <div className="prelaunch-fact"><ScrollText size={17}/><span><small>还没有跑过提取</small><b>每次提取都会留一条记录：用的哪版 Skill、哪个模型、花了多久、哪几项没采信。失败的也会留，不然成功率永远是 100%。</b></span></div>}
          </div>
        </> : target !== 'single' ? <>
          <span className="section-label">特征体系</span><h3>{typeSchema?.label ?? assetTypeLabels[target]}</h3>
          <p>{typeSchema?.fields.length ?? 0} 个变量 · 来源 {typeSchema?.source ?? '03 §5'}</p>
          <div className="prelaunch-fact"><Sparkles size={17}/><span><small>为什么按类型分开</small><b>视频的钩子类型问不到公众号文章头上。每类素材只带它自己的内容支持的变量，混用会得到一列没有意义的对比。</b></span></div>
          {typeSchema ? groupsOf(typeSchema).map(group => <div className="feature-stack" key={group}>
            <span>{group}</span>
            {typeSchema.fields.filter(field => field.group === group).map(field => <b key={field.key}>{field.label}</b>)}
          </div>) : null}
          {matrix?.disclosure ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>口径</small>{matrix.disclosure}</span></div> : null}
        </> : <div className="panel-empty">选择上方素材后查看它的类型与提取情况。</div>}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

function FeatureMatrixTable({ matrix }: { matrix: ApiFeatureMatrix }) {
  const groups = [...new Set(matrix.rows.map(row => row.group))]
  return <div className="content-matrix" style={{ '--matrix-columns': matrix.assets.length } as React.CSSProperties}>
    <div className="content-matrix-row header">
      <span>特征</span>
      {matrix.assets.map(asset => <span key={asset.id}>{asset.title}<small>v{asset.revision}</small></span>)}
    </div>
    {groups.map(group => <Fragment key={group}>
      <div className="content-matrix-group">{group}</div>
      {matrix.rows.filter(row => row.group === group).map(row => <div className="content-matrix-row" key={row.key}>
        <span><b>{row.label}</b><small>{row.key}</small></span>
        {matrix.assets.map(asset => {
          // 三种来源都要显示，顺序固定成「量出来的 → 人标的 → 模型猜的」：
          // 归因只认前两种，把能归因的排在前面，人一眼就知道这一格能不能用。
          // 少列 derived 那一种的话，一个量出来的取值会显示成「—」，被读成「没提取」。
          const cells = sourceOrder
            .map(source => (row.cells ?? []).find(cell => cell.asset_id === asset.id && cell.source === source))
            .filter((cell): cell is ApiFeatureMatrixCell => Boolean(cell))
          return <span key={asset.id}>
            {cells.map(cell => <b key={cell.source} className={`content-layer ${cell.source}`}>
              {featureSourceLabel[cell.source]} · {formatValue(cell.value)}
              {cell.source === 'ai' ? ` · 置信${confidenceLabels[cell.confidence ?? 'low']}` : ''}
            </b>)}
            {cells.length ? null : <b className="content-layer">—</b>}
          </span>
        })}
      </div>)}
    </Fragment>)}
  </div>
}

function FeatureBreakdown({ schema, features, editingKey, draft, onDraft, onEdit, onCancel, onConfirm, onReject }: {
  schema: ApiFeatureSchema
  features: ApiInsightAssetFeature[]
  editingKey: string
  draft: string
  onDraft: (value: string) => void
  onEdit: (key: string) => void
  onCancel: () => void
  onConfirm: (key: string, value: ApiFeatureValue) => void
  /**
   * hadAi 决定这次保存的语义：AI 提过这一项，人给了别的值，那是**推翻**；
   * AI 压根没提过，人是第一个填的，那是**人工原创**。两者不能都记成推翻——
   * 记成推翻的话，投后分析会把这条特征当成被否掉的推断丢掉，人填了等于白填。
   */
  onReject: (key: string, value: ApiFeatureValue, hadAi: boolean) => void
}) {
  return <div className="content-breakdown">
    {groupsOf(schema).map(group => <Fragment key={group}>
      <div className="content-matrix-group">{group}</div>
      {schema.fields.filter(field => field.group === group).map(field => {
        const ai = features.find(feature => feature.source === 'ai' && feature.key === field.key)
        const human = features.find(feature => feature.source === 'human' && feature.key === field.key)
        // 量出来的那一层是只读的：它是算出来的，改它等于改算法，不该在这里改。
        const derived = features.find(feature => feature.source === 'derived' && feature.key === field.key)
        const editing = editingKey === field.key
        return <div className="content-breakdown-row" key={field.key}>
          <span><b>{field.label}</b><small>{field.key} · {kindLabels[field.kind] ?? field.kind}{field.unit ? ` · ${field.unit}` : ''}</small></span>
          <span>
            {derived ? <b className="content-layer derived">{featureSourceLabel.derived} · {formatValue(derived.value)}</b> : null}
            {ai ? <b className="content-layer ai">{featureSourceLabel.ai} · {formatValue(ai.value)} · 置信{confidenceLabels[ai.confidence ?? 'low']}</b> : <b className="content-layer">模型还没提取这一项</b>}
            {human ? <b className="content-layer human">{featureSourceLabel.human} · {formatValue(human.value)} · {reviewLabels[human.review_state] ?? human.review_state}</b> : null}
            {editing ? <input
              autoFocus
              aria-label={`改写 ${field.label}`}
              value={draft}
              placeholder={placeholderFor(field.kind, field.vocabulary)}
              onChange={event => onDraft(event.target.value)}
            /> : null}
          </span>
          <span className="actions">
            {editing ? <>
              <button className="secondary-button" disabled={!draft.trim()} onClick={() => onReject(field.key, parseValue(field.kind, draft), Boolean(ai))}>保存人工结论</button>
              <button className="secondary-button" onClick={onCancel}>取消</button>
            </> : <>
              {ai && !human ? <button className="secondary-button" onClick={() => onConfirm(field.key, ai.value)}>认可</button> : null}
              <button className="secondary-button" onClick={() => onEdit(field.key)}>{human ? '改写' : ai ? '推翻并改写' : '人工填写'}</button>
            </>}
          </span>
        </div>
      })}
    </Fragment>)}
  </div>
}

const runStatusLabels: Record<string, string> = { running: '进行中', succeeded: '成功', failed: '失败' }

function formatTime(raw: string): string {
  const at = new Date(raw)
  return Number.isNaN(at.getTime()) ? raw : at.toLocaleString('zh-CN', { hour12: false })
}

/**
 * 库里存的是整条错误链（英文 sentinel + 中文原因），排查时有用，但摆在页面上
 * 只会让人多看一串英文。这里只取那句给人看的中文。
 */
function readableError(raw = ''): string {
  const at = raw.search(/[一-龥]/)
  return at > 0 && /: $/.test(raw.slice(0, at)) ? raw.slice(at) : raw
}

function formatLatency(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} 秒` : `${ms} 毫秒`
}

const kindLabels: Record<string, string> = {
  text: '文本', tags: '开放标签', enum: '单选', enum_multi: '多选',
  number: '数值', bool: '是否', duration_seconds: '时长（秒）',
}

function groupsOf(schema: ApiFeatureSchema): string[] {
  return [...new Set(schema.fields.map(field => field.group))]
}

function placeholderFor(kind: ApiFeatureValueKind, vocabulary?: string[]): string {
  if (kind === 'bool') return '填「是」或「否」'
  if (kind === 'number') return '填数字'
  if (kind === 'duration_seconds') return '填秒数'
  if (vocabulary?.length) return `可选：${vocabulary.slice(0, 6).join('、')}`
  if (kind === 'text') return '填写人工结论'
  return '多个取值用顿号分隔'
}

function parseValue(kind: ApiFeatureValueKind, raw: string): ApiFeatureValue {
  const text = raw.trim()
  if (kind === 'bool') return { kind, bool: /^(是|yes|true|1)$/i.test(text) }
  if (kind === 'number' || kind === 'duration_seconds') return { kind, number: Number(text) || 0 }
  if (kind === 'text') return { kind, text }
  return { kind, terms: text.split(/[、,，/|\s]+/).filter(Boolean) }
}

function formatValue(value: ApiFeatureValue): string {
  if (value.kind === 'bool') return value.bool ? '是' : '否'
  if (value.kind === 'number') return String(value.number ?? 0)
  if (value.kind === 'duration_seconds') return `${value.number ?? 0} 秒`
  if (value.terms?.length) return value.terms.join('、')
  return value.text || '（空）'
}
