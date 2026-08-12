import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, CircleCheck, Database, GitBranch, Layers3, Lightbulb, Link2, Plus, RefreshCw, Search, Sparkles } from 'lucide-react'
import { useProject } from '../context/ProjectContext'
import {
  api,
  type ApiAnalysisStatus,
  type ApiAssetSourceKind,
  type ApiConfidence,
  type ApiFeatureSchema,
  type ApiFeatureValue,
  type ApiInsightAsset,
  type ApiInsightAssetFeature,
  type ApiInsightAssetMapping,
  type ApiInsightAssetType,
  type ApiMappingStatus,
  type ApiReviewState,
} from '../data/api'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'
import { shortId } from '../data/shortId'

/**
 * 分析素材库（03 §9 AM-001~006，导航见 19 §5.2）。
 *
 * 五个视图各查各的数据，而不是同一批素材换个标签：22 §8.3 要求每个可见的 L2
 * 标签都必须真实改变数据集。「待匹配」查的甚至不是素材，而是还没认领的平台对象。
 */
type ViewTarget = 'all' | 'awaiting-match' | 'awaiting-extraction' | 'features' | 'lineage'

// 导航配置（src/data/navigation.ts）与 19 §5.2 的标签用词略有出入，两种都接住。
const viewTargets: Record<string, ViewTarget> = {
  全部素材: 'all',
  待匹配: 'awaiting-match',
  待提取: 'awaiting-extraction',
  特征: 'features',
  特征库: 'features',
  版本与血缘: 'lineage',
  版本关系: 'lineage',
}

const statusLabels: Record<ApiAnalysisStatus, string> = {
  awaiting_data: '待数据',
  awaiting_match: '待匹配',
  analysable: '可分析',
  analysing: '分析中',
  pending_confirmation: '待确认',
  confirmed: '已确认',
  needs_review: '待复审',
  retired: '已失效',
}

const assetTypeLabels: Record<ApiInsightAssetType, string> = {
  xiaohongshu_note: '小红书图文',
  wechat_article: '公众号图文',
  brand_ad: '品牌广告',
  digital_human_ad: '数字人效果广告',
  preroll_ad: '广告前贴',
  hit_replica_ad: '爆款复刻',
}

const sourceKindLabels: Record<string, string> = {
  creative: '创意模块产物',
  upload: '上传文件',
  external: '外部引用',
}

const confidenceLabels: Record<ApiConfidence, string> = { low: '低', medium: '中', high: '高' }

const reviewLabels: Record<ApiReviewState, string> = {
  pending: '待人工复核',
  confirmed: '人工认可',
  rejected: '人工推翻',
  authored: '人工填写',
}

const emptyHints: Record<ViewTarget, string> = {
  all: '当前 Project 还没有可分析素材。素材来自创意模块产物、上传文件或平台回流。',
  'awaiting-match': '没有待认领的平台对象。平台上的创意或广告回流后，认不出对应素材版本的会排在这里。',
  'awaiting-extraction': '没有待提取素材。素材类型识别完成后才会进入这里等待特征提取。',
  features: '特征体系读取为空。六类素材的特征字段由后端 03 §5 定义，正常不应为空。',
  lineage: '当前 Project 还没有可分析素材，也就没有版本关系可看。',
}

export function AssetLibraryPage({ state, activeView }: { state: DataState; activeView: string }) {
  const { currentProject } = useProject()
  const target = viewTargets[activeView] ?? 'all'
  const [assets, setAssets] = useState<ApiInsightAsset[]>([])
  const [mappings, setMappings] = useState<ApiInsightAssetMapping[]>([])
  const [schemas, setSchemas] = useState<ApiFeatureSchema[]>([])
  const [features, setFeatures] = useState<ApiInsightAssetFeature[]>([])
  const [lineage, setLineage] = useState<ApiInsightAsset[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [query, setQuery] = useState('')
  const [notice, setNotice] = useState('')
  const [indexing, setIndexing] = useState(false)
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const loadList = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      if (target === 'awaiting-match') {
        // 素材列表也要一起取：认领时得从现有素材版本里挑一个，不能让人手填 ID。
        const [mappingPage, assetPage] = await Promise.all([
          api.listInsightAssetMappings(currentProject.id, 'unmatched'),
          api.listInsightAssets(currentProject.id, {}),
        ])
        setMappings(mappingPage.items)
        setAssets(assetPage.items)
      } else if (target === 'features') {
        const next = await api.listFeatureSchemas(currentProject.id)
        setSchemas(next.items)
        setAssets([])
      } else {
        // 待提取 = 类型已识别、还没提特征；全部素材与版本关系读同一批但用途不同。
        const next = await api.listInsightAssets(
          currentProject.id,
          target === 'awaiting-extraction' ? { statuses: ['analysable'] } : {},
        )
        setAssets(next.items)
        setMappings([])
      }
      setListState('ready')
    } catch (cause) {
      setAssets([])
      setMappings([])
      setSchemas([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '分析素材库读取失败。')
    }
  }, [currentProject.id, target])

  useEffect(() => { void loadList() }, [loadList])
  // 换视图就把表单收起来：填到一半切走再切回来，人会以为还在填刚才那一条。
  useEffect(() => { setNotice(''); setQuery(''); setIndexing(false) }, [target])

  const keyword = query.trim().toLowerCase()

  const filteredAssets = useMemo(() => assets.filter(asset =>
    `${asset.id} ${asset.title} ${asset.lineage_id} ${asset.source_ref ?? ''}`.toLowerCase().includes(keyword),
  ), [assets, keyword])

  const filteredMappings = useMemo(() => mappings.filter(mapping =>
    `${mapping.id} ${mapping.platform} ${mapping.platform_object_id} ${mapping.platform_object_name ?? ''}`
      .toLowerCase().includes(keyword),
  ), [mappings, keyword])

  const filteredSchemas = useMemo(() => schemas.filter(schema =>
    `${schema.asset_type} ${schema.label} ${schema.fields.map(field => `${field.key} ${field.label}`).join(' ')}`
      .toLowerCase().includes(keyword),
  ), [schemas, keyword])

  // 版本关系按 lineage 聚合：同一条创意的多次修订属于一个身份（AM-001）。
  const lineages = useMemo(() => {
    const grouped = new Map<string, ApiInsightAsset[]>()
    for (const asset of filteredAssets) {
      grouped.set(asset.lineage_id, [...(grouped.get(asset.lineage_id) ?? []), asset])
    }
    return [...grouped.entries()]
      .map(([lineageId, revisions]) => ({
        lineageId,
        revisions: [...revisions].sort((left, right) => left.revision - right.revision),
      }))
      .sort((left, right) => right.revisions.length - left.revisions.length)
  }, [filteredAssets])

  const rowIds = target === 'awaiting-match' ? filteredMappings.map(mapping => mapping.id)
    : target === 'features' ? filteredSchemas.map(schema => schema.asset_type)
    : target === 'lineage' ? lineages.map(entry => entry.lineageId)
    : filteredAssets.map(asset => asset.id)

  useEffect(() => {
    setSelectedId(current => rowIds.includes(current) ? current : rowIds[0] ?? '')
  }, [rowIds.join('|')])

  const selectedAsset = target === 'all' || target === 'awaiting-extraction'
    ? filteredAssets.find(asset => asset.id === selectedId)
    : undefined
  const selectedMapping = filteredMappings.find(mapping => mapping.id === selectedId)
  const selectedSchema = filteredSchemas.find(schema => schema.asset_type === selectedId)
  const selectedLineage = lineages.find(entry => entry.lineageId === selectedId)

  // 选中素材后读它的特征（两层都读，AI 与人工分开显示）。
  useEffect(() => {
    if (!selectedAsset) {
      setFeatures([])
      return
    }
    let active = true
    void api.listInsightAssetFeatures(currentProject.id, selectedAsset.id)
      .then(page => { if (active) setFeatures(page.items) })
      .catch(() => { if (active) setFeatures([]) })
    return () => { active = false }
  }, [currentProject.id, selectedAsset?.id])

  // 版本视图选中一条血缘时，从后端取权威的修订序列，而不是只用列表里凑到的那几条。
  useEffect(() => {
    const head = selectedLineage?.revisions[0]
    if (!head) {
      setLineage([])
      return
    }
    let active = true
    void api.listInsightAssetLineage(currentProject.id, head.id)
      .then(page => { if (active) setLineage(page.items) })
      .catch(() => { if (active) setLineage(selectedLineage?.revisions ?? []) })
    return () => { active = false }
  }, [currentProject.id, selectedLineage?.revisions[0]?.id])

  const header = headers[target]
  const counts = target === 'awaiting-match' ? `${filteredMappings.length} 个待认领平台对象`
    : target === 'features' ? `${filteredSchemas.length} 套特征体系`
    : target === 'lineage' ? `${lineages.length} 条创意血缘 · 共 ${filteredAssets.length} 个版本`
    : `${filteredAssets.length} 个素材`

  return <StateBoundary state={state} onRetry={() => { void loadList() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div><span className="section-label">ASSET LIBRARY</span><h2>{header.title}</h2><p>当前 Project：{currentProject.name}。{header.lead}</p></div>
          <div className="prelaunch-actions inline">
            {target === 'all' || target === 'lineage'
              ? <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { setNotice(''); setIndexing(true) }}><Plus size={15}/>登记素材</button>
              : null}
            <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void loadList() }}><RefreshCw size={15}/>刷新</button>
          </div>
        </div>
        <div className="prelaunch-filterbar">
          <div className="search-field"><Search size={15}/><input aria-label="搜索分析素材库" value={query} onChange={event => setQuery(event.target.value)} placeholder={header.placeholder}/></div>
          <span>{counts}</span>
        </div>
        <div className="prelaunch-table" role="list" aria-label={header.title}>
          <div className="prelaunch-row header">{header.columns.map(column => <span key={column}>{column}</span>)}</div>
          {listState === 'loading' ? <div className="panel-empty">正在读取当前 Project 的分析素材库…</div> : null}
          {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}
          {listState === 'ready' && !rowIds.length ? <div className="panel-empty">{emptyHints[target]}</div> : null}

          {(target === 'all' || target === 'awaiting-extraction') && filteredAssets.map(asset => <button role="listitem" key={asset.id} className={selectedId === asset.id ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(asset.id)}>
            <span><b>{asset.title}</b><small>{shortId(asset.id)} · {sourceKindLabels[asset.source_kind] ?? asset.source_kind} · {formatTime(asset.updated_at)}</small></span>
            <span>{asset.asset_type ? assetTypeLabels[asset.asset_type] : '待识别'}</span>
            <span>{statusLabels[asset.analysis_status]}</span>
            <span><CircleCheck size={14}/>v{asset.revision}</span>
          </button>)}

          {target === 'awaiting-match' && filteredMappings.map(mapping => <button role="listitem" key={mapping.id} className={selectedId === mapping.id ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(mapping.id)}>
            <span><b>{mapping.platform_object_name || mapping.platform_object_id}</b><small>{mapping.platform} · {mapping.platform_object_kind === 'ad' ? '广告' : '创意'} · {formatTime(mapping.created_at)}</small></span>
            <span>{mapping.platform_object_id.slice(0, 16)}</span>
            <span>{mapping.note || '未说明'}</span>
            <span><Link2 size={14}/>v{mapping.version}</span>
          </button>)}

          {target === 'features' && filteredSchemas.map(schema => <button role="listitem" key={schema.asset_type} className={selectedId === schema.asset_type ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(schema.asset_type)}>
            <span><b>{schema.label}</b><small>{schema.fields.length} 个特征字段 · 来源 {schema.source}</small></span>
            <span>{groupsOf(schema).length} 个分组</span>
            <span>{groupsOf(schema).join('、')}</span>
            <span><Sparkles size={14}/>{schema.asset_type}</span>
          </button>)}

          {target === 'lineage' && lineages.map(entry => <button role="listitem" key={entry.lineageId} className={selectedId === entry.lineageId ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(entry.lineageId)}>
            <span><b>{entry.revisions[entry.revisions.length - 1]?.title}</b><small>血缘 {shortId(entry.lineageId)} · 最近更新 {formatTime(entry.revisions[entry.revisions.length - 1]?.updated_at ?? '')}</small></span>
            <span>{entry.revisions.length} 个版本</span>
            <span>{[...new Set(entry.revisions.map(revision => statusLabels[revision.analysis_status]))].join('、')}</span>
            <span><GitBranch size={14}/>v{entry.revisions[entry.revisions.length - 1]?.revision}</span>
          </button>)}
        </div>
      </section>

      <aside className="prelaunch-detail">
        {indexing ? <AssetIndexForm
          assets={assets}
          projectId={currentProject.id}
          busy={listState === 'loading'}
          onCancel={() => setIndexing(false)}
          onDone={asset => {
            setIndexing(false)
            setNotice(`已登记「${asset.title}」，现在是${statusLabels[asset.analysis_status]}。${asset.asset_type ? '接下来去内容分析给它提特征。' : '先在这里确定它的内容类型，才知道要提哪套特征。'}`)
            void loadList().then(() => setSelectedId(asset.id))
          }}/> : selectedAsset ? <>
          <span className="section-label">素材版本</span><h3>{selectedAsset.title}</h3>
          <p>{shortId(selectedAsset.id)} · 第 {selectedAsset.revision} 版 · {statusLabels[selectedAsset.analysis_status]}</p>
          <div className="prelaunch-fact"><Layers3 size={17}/><span><small>内容类型</small><b>
            {selectedAsset.asset_type ? assetTypeLabels[selectedAsset.asset_type] : '待识别。没有类型就无法确定要提取哪套特征。'}
            {selectedAsset.asset_type_source === 'ai' ? `（AI 识别 · 置信 ${confidenceLabels[selectedAsset.asset_type_confidence ?? 'low']}）` : selectedAsset.asset_type_source === 'human' ? '（人工判定）' : ''}
          </b></span></div>
          <div className="prelaunch-fact"><Database size={17}/><span><small>来源</small><b>{sourceKindLabels[selectedAsset.source_kind] ?? selectedAsset.source_kind}{selectedAsset.source_ref ? ` · ${selectedAsset.source_ref}` : ''}{selectedAsset.source_job_id ? ` · 任务 ${shortId(selectedAsset.source_job_id)}` : ''}</b></span></div>
          {selectedAsset.analysis_status_reason ? <div className="prelaunch-fact"><Lightbulb size={17}/><span><small>最近一次状态说明</small><b>{selectedAsset.analysis_status_reason}</b></span></div> : null}

          <div className="feature-stack">
            <span>AI 推断（{features.filter(feature => feature.source === 'ai').length} 项 · 每项都带置信级别）</span>
            {features.filter(feature => feature.source === 'ai').map(feature => <b key={feature.id}>
              {feature.key}：{formatValue(feature.value)} · 置信{confidenceLabels[feature.confidence ?? 'low']}
              {features.some(other => other.source === 'human' && other.key === feature.key) ? ' · 已复核' : ' · 待复核'}
            </b>)}
            {features.some(feature => feature.source === 'ai') ? null : <b>还没有 AI 提取结果。</b>}
          </div>

          <div className="feature-stack">
            <span>人工结论（{features.filter(feature => feature.source === 'human').length} 项 · 不覆盖 AI 那一层）</span>
            {features.filter(feature => feature.source === 'human').map(feature => <b key={feature.id}>
              {feature.key}：{formatValue(feature.value)} · {reviewLabels[feature.review_state]}
            </b>)}
            {features.some(feature => feature.source === 'human') ? null : <b>还没有人工结论。确认分析结论前，每项 AI 取值都要有人看过。</b>}
          </div>

          {selectedAsset.asset_type && !features.length
            ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>还没有特征</small>类型已识别但特征尚未提取，这个素材还不能参与对比分析。</span></div>
            : null}
        </> : selectedMapping ? <>
          <span className="section-label">待认领平台对象</span><h3>{selectedMapping.platform_object_name || selectedMapping.platform_object_id}</h3>
          <p>{selectedMapping.platform} · {selectedMapping.platform_object_kind === 'ad' ? '广告' : '创意'} · {statusLabels.awaiting_match}</p>
          <div className="prelaunch-fact"><Link2 size={17}/><span><small>平台标识</small><b>{selectedMapping.platform_object_id}</b></span></div>
          <div className="prelaunch-fact"><Lightbulb size={17}/><span><small>为什么要人来认领</small><b>认不出对应的是哪一版素材，就不能把这条广告的效果算到任何一版身上；宁可留在队列里，也不猜。</b></span></div>
          {selectedMapping.note ? <div className="prelaunch-fact"><Database size={17}/><span><small>备注</small><b>{selectedMapping.note}</b></span></div> : null}
          <MappingResolveForm
            key={selectedMapping.id}
            mapping={selectedMapping}
            assets={assets}
            projectId={currentProject.id}
            onDone={message => { setNotice(message); void loadList() }}/>
        </> : selectedSchema ? <>
          <span className="section-label">特征体系</span><h3>{selectedSchema.label}</h3>
          <p>{selectedSchema.fields.length} 个字段 · 来源 {selectedSchema.source}</p>
          <div className="prelaunch-fact"><Sparkles size={17}/><span><small>为什么每类各有一套</small><b>视频的钩子类型问不到公众号文章头上。每类素材只带它自己的内容支持的字段，混用会得到一列没有意义的对比。</b></span></div>
          {groupsOf(selectedSchema).map(group => <div className="feature-stack" key={group}>
            <span>{group}</span>
            {selectedSchema.fields.filter(field => field.group === group).map(field => <b key={field.key}>
              {field.label}（{field.key}）· {kindLabels[field.kind] ?? field.kind}
              {field.unit ? ` · ${field.unit}` : ''}
              {field.vocabulary?.length ? ` · ${field.vocabulary.join('/')}` : ''}
            </b>)}
          </div>)}
          <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>词表可能是空的</small>枚举字段的词表由「能力运营 → 特征体系」维护（03 §5 末）。词表为空时提取仍可写入，只是暂时不校验取值。</span></div>
        </> : selectedLineage ? <>
          <span className="section-label">版本关系</span><h3>{selectedLineage.revisions[selectedLineage.revisions.length - 1]?.title}</h3>
          <p>血缘 {selectedLineage.lineageId} · {(lineage.length || selectedLineage.revisions.length)} 个版本</p>
          <div className="prelaunch-fact"><GitBranch size={17}/><span><small>为什么要留血缘</small><b>同一条创意改了几版，效果要分版本看；把它们合成一个素材，就分不出是哪一版带来的差异。</b></span></div>
          <div className="feature-stack">
            <span>修订序列</span>
            {(lineage.length ? lineage : selectedLineage.revisions).map(revision => <b key={revision.id}>
              v{revision.revision} · {revision.asset_type ? assetTypeLabels[revision.asset_type] : '待识别'} · {statusLabels[revision.analysis_status]} · {formatTime(revision.created_at)}
            </b>)}
          </div>
        </> : <div className="panel-empty">{header.detailEmpty}</div>}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

const headers: Record<ViewTarget, { title: string; lead: string; placeholder: string; columns: string[]; detailEmpty: string }> = {
  all: {
    title: '当前 Project 的可分析素材',
    lead: '每条记录是一个素材版本，带来源、内容类型和分析状态。',
    placeholder: '搜索素材标题、编号或来源',
    columns: ['素材', '内容类型', '分析状态', '版本'],
    detailEmpty: '选择左侧素材后查看来源、内容类型和两层特征。',
  },
  'awaiting-match': {
    title: '还认不出对应素材的平台对象',
    lead: '平台上的创意和广告回流后，认不出对应哪一版素材的会排在这里等人认领。',
    placeholder: '搜索平台、对象名称或标识',
    columns: ['平台对象', '平台标识', '备注', '版本'],
    detailEmpty: '选择左侧平台对象后查看它的标识和备注。',
  },
  'awaiting-extraction': {
    title: '类型已识别、等待提取特征的素材',
    lead: '类型确定后才知道该提哪套特征，所以提取排在识别之后。',
    placeholder: '搜索素材标题、编号或来源',
    columns: ['素材', '内容类型', '分析状态', '版本'],
    detailEmpty: '选择左侧素材后查看它还缺哪些特征。',
  },
  features: {
    title: '六类素材各自的特征体系',
    lead: '这是内容分析能问出的全部变量，按素材类型分开（03 §5）。',
    placeholder: '搜索素材类型或特征字段',
    columns: ['素材类型', '分组数', '分组', '标识'],
    detailEmpty: '选择左侧素材类型后查看它的特征字段。',
  },
  lineage: {
    title: '同一条创意的版本关系',
    lead: '一条创意改了几版仍是同一个身份，效果要分版本看（AM-001）。',
    placeholder: '搜索素材标题或血缘编号',
    columns: ['创意', '版本数', '分析状态', '最新版本'],
    detailEmpty: '选择左侧创意后查看它的修订序列。',
  },
}

const kindLabels: Record<string, string> = {
  text: '文本',
  tags: '开放标签',
  enum: '单选',
  enum_multi: '多选',
  number: '数值',
  bool: '是否',
  duration_seconds: '时长（秒）',
}

function groupsOf(schema: ApiFeatureSchema): string[] {
  return [...new Set(schema.fields.map(field => field.group))]
}

function formatValue(value: ApiFeatureValue): string {
  if (value.kind === 'bool') return value.bool ? '是' : '否'
  if (value.kind === 'number') return String(value.number ?? 0)
  if (value.kind === 'duration_seconds') return `${value.number ?? 0} 秒`
  if (value.terms?.length) return value.terms.join('、')
  return value.text || '（空）'
}

function formatTime(value: string): string {
  const time = new Date(value)
  return Number.isNaN(time.valueOf()) ? value : time.toLocaleString('zh-CN')
}

/**
 * 登记一个素材。
 *
 * 创意模块产出的素材会自己流进来，但在别处做完直接投出去的（别处剪的片子、代理商
 * 给的图文）没有那条通路。少了这个入口，平台回流的广告对象就没有任何素材可以认领
 * 过去，花费只能挂在总盘上，后面的内容分析、对比、报告全都无从谈起。
 *
 * 注意跟「外部证据」区分：这里登记的是**真的投出去过**的素材，它进共享素材库、
 * 参与归因；外部证据是竞品或参照物，只读、有到期日、永远不进素材库。
 *
 * 内容类型可以先不填：填了才知道要提哪套特征，但认不准就先留着「待识别」，
 * 比随便挑一个然后按错的特征表去提取要好。
 */
// 导出给「素材 · 总览」用。登记素材是这个模块唯一一处「凭空多出一条素材」的入口，
// 三个旧页面合并之后它不能跟着旧页面一起消失——没有它，一个新 Project 里
// 后面每一页都是空的，而人找不到从哪儿开始。
export function AssetIndexForm({ assets, projectId, busy, onCancel, onDone }: {
  assets: ApiInsightAsset[]
  projectId: string
  busy: boolean
  onCancel: () => void
  onDone: (asset: ApiInsightAsset) => void
}) {
  const [title, setTitle] = useState('')
  const [sourceKind, setSourceKind] = useState<ApiAssetSourceKind>('upload')
  const [sourceRef, setSourceRef] = useState('')
  const [assetType, setAssetType] = useState<ApiInsightAssetType | ''>('')
  const [lineageId, setLineageId] = useState('')
  const [error, setError] = useState('')
  const [working, setWorking] = useState(false)

  // 同一条创意的多个版本共用一个 lineage_id，列表里只需要露出每条血缘的最新一版。
  //
  // 整条都作废了的血缘不出现：给一条已经作废的创意再加一版没有意义，而它留在下拉里
  // 只会让人误选，选中之后新素材就挂到了一条不该再动的血缘上。版本号仍然按全部修订
  // 数——包括作废的——来显示，否则会写出「现在第 2 版」而后端实际给出第 4 版。
  const lineages = useMemo(() => {
    const latest = new Map<string, ApiInsightAsset>()
    const alive = new Set<string>()
    for (const asset of assets) {
      const seen = latest.get(asset.lineage_id)
      if (!seen || asset.revision > seen.revision) latest.set(asset.lineage_id, asset)
      if (asset.analysis_status !== 'retired') alive.add(asset.lineage_id)
    }
    return [...latest.values()].filter(asset => alive.has(asset.lineage_id))
  }, [assets])

  const submit = async () => {
    if (!title.trim()) {
      setError('先给它一个标题，后面所有地方都靠这个名字认人。')
      return
    }
    setError('')
    setWorking(true)
    try {
      const asset = await api.indexInsightAsset(projectId, {
        title: title.trim(),
        source_kind: sourceKind,
        source_ref: sourceRef.trim() || undefined,
        lineage_id: lineageId || undefined,
        // 类型是人在这儿挑的，就照实记成人工判定——记成 AI 推断的话，
        // 复核队列会以为这项已经有机器意见了，而实际上没有。
        asset_type: assetType || undefined,
        asset_type_source: assetType ? 'human' : undefined,
      })
      onDone(asset)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '登记失败，请稍后重试。')
    } finally {
      setWorking(false)
    }
  }

  return <>
    <span className="section-label">登记一个素材</span>
    <p>创意模块批准过的素材走「从创意导入」，一次勾完，不用在这儿一条条打。这里登记的是在别处做完直接投出去的那些——别处剪的片子、代理商给的图文，不登记就没法把平台上的广告认到它头上。（找竞品参照物请走「外部证据」，那边的东西不参与归因。）</p>
    {/* 沿用经验库修订表单的字段写法（.experience-revise）：标签在上、输入框整行。
        右边这一栏窄，标签和输入框并排会把输入框挤到只剩十几个字符宽。 */}
    <div className="experience-revise">
      <label className="experience-reason"><small>标题</small>
        <input value={title} onChange={event => setTitle(event.target.value)} placeholder="例如：夏季新品 数字人口播 A"/>
      </label>
      <div className="revise-grid">
        <label><small>来源</small>
          <select value={sourceKind} onChange={event => setSourceKind(event.target.value as ApiAssetSourceKind)}>
            {(Object.keys(sourceKindLabels) as ApiAssetSourceKind[]).map(kind => <option key={kind} value={kind}>{sourceKindLabels[kind]}</option>)}
          </select>
        </label>
        <label><small>内容类型（认不准就留空）</small>
          <select value={assetType} onChange={event => setAssetType(event.target.value as ApiInsightAssetType | '')}>
            <option value="">待识别</option>
            {(Object.keys(assetTypeLabels) as ApiInsightAssetType[]).map(type => <option key={type} value={type}>{assetTypeLabels[type]}</option>)}
          </select>
        </label>
      </div>
      <label className="experience-reason"><small>来源说明（可留空）</small>
        <input value={sourceRef} onChange={event => setSourceRef(event.target.value)} placeholder="例如：外部剪辑 20260720 交付批次"/>
      </label>
      <label className="experience-reason"><small>属于哪条创意</small>
        <select value={lineageId} onChange={event => setLineageId(event.target.value)}>
          <option value="">新的一条创意</option>
          {lineages.map(asset => <option key={asset.lineage_id} value={asset.lineage_id}>
            {asset.title} 的下一版（现在第 {asset.revision} 版）
          </option>)}
        </select>
      </label>
    </div>
    {error ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>没有登记成功</small>{error}</span></div> : null}
    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={busy || working} onClick={() => void submit()}>{working ? '登记中…' : '登记'}</button>
      <button className="secondary-button full" disabled={working} onClick={onCancel}>取消</button>
    </div>
  </>
}

/**
 * 认领一个平台对象（doc10 §5）。认领之后这条广告的花费才算得到某一版素材头上；
 * 不认领它照样计入总盘，只是撑不起「这条创意更好」这类结论。
 *
 * 忽略也是一个正当结果——测试广告、非本项目投放，明确标掉比一直挂在队列里干净。
 * 两种动作都要写理由：后面看到某个素材的数字时，得能回溯它凭什么算进来。
 *
 * 已认领的对象也要能改（「数据接入 → 素材映射」复用了这个表单）。认错归属在真实
 * 业务里很常见——投放同学换了素材没说、平台创意名和交付件对不上。之前认领一次
 * 就锁死，错了只能去数据库改，而错误的归属会一路污染投后分析和经验。
 */
export function MappingResolveForm({ mapping, assets, projectId, onDone }: {
  mapping: ApiInsightAssetMapping
  assets: ApiInsightAsset[]
  projectId: string
  onDone: (message: string) => void
}) {
  const claimed = mapping.status === 'matched'
  // 改认领时把当前归属带出来：人要改的往往只是其中一项，让他从空白重选容易选错。
  const [assetId, setAssetId] = useState(mapping.asset_id ?? '')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const resolve = async (status: ApiMappingStatus) => {
    if (status === 'matched' && !assetId) {
      onDone('认领要先选一个素材版本。')
      return
    }
    if (!note.trim()) {
      onDone('认领和忽略都要写清依据，否则后面没人说得清这条花费凭什么算进来。')
      return
    }
    setBusy(true)
    try {
      await api.resolveInsightAssetMapping(projectId, mapping.id, {
        expected_version: mapping.version,
        status,
        asset_id: status === 'matched' ? assetId : undefined,
        note: note.trim(),
      })
      onDone(status === 'matched'
        ? claimed
          ? '已改认领。这条对象过往的花费会整体重算到新选的素材版本上，原来那一版的数字会相应减少。'
          : '已认领，这条对象的花费从现在起算到所选素材版本上。'
        : '已忽略，它的花费仍计入总盘但不归属任何素材。')
    } catch (cause) {
      onDone(cause instanceof Error ? cause.message : '认领失败，请稍后重试。')
    } finally {
      setBusy(false)
    }
  }

  return <div className="feature-stack">
    <span>{claimed ? '改这个平台对象的归属' : '认领这个平台对象'}</span>
    <label className="field">归到哪个素材版本
      <select value={assetId} onChange={event => setAssetId(event.target.value)}>
        <option value="">未选择</option>
        {assets.map(asset => <option key={asset.id} value={asset.id}>
          {asset.title} · 第 {asset.revision} 版
        </option>)}
      </select>
    </label>
    <label className="field">依据（会连同操作人一起记下来）
      <input value={note} onChange={event => setNote(event.target.value)}
        placeholder={claimed ? '例如：原先认错了，平台创意名对应的是 B 版交付件' : '例如：平台创意名与 v2 交付件一致，投放同学确认'}/>
    </label>
    <button className="primary-button full" disabled={busy} onClick={() => void resolve('matched')}>{claimed ? '改到这一版' : '认领到这一版'}</button>
    <button className="secondary-button full" disabled={busy} onClick={() => void resolve('ignored')}>不属于本项目，忽略</button>
  </div>
}
