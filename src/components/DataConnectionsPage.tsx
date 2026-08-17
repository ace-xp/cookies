import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, CircleCheck, Database, Link2, Plug, RefreshCw, Search, Upload } from 'lucide-react'
import { useProject } from '../context/ProjectContext'
import {
  api,
  ApiRequestError,
  type ApiDataSource,
  type ApiDataSourceStatus,
  type ApiImportBatch,
  type ApiIngestMode,
  type ApiInsightAsset,
  type ApiInsightAssetMapping,
  type ApiMetricRow,
  type ApiPlatform,
  type ApiQualityStatus,
} from '../data/api'
import { builtInAliasesOf, hashText, metricColumns, parseMetricCsv } from '../data/metricCsv'
import type { DataState } from '../types'
import { StateBoundary } from './StateBoundary'
import { MappingResolveForm } from './AssetLibraryPage'
import { shortId } from '../data/shortId'

/**
 * 数据接入（10-ad-data-connectors.md，导航见 19 §5.2）。
 *
 * 五个视图各查各的数据（22 §8.3）：数据源与字段映射读同一批行但看不同字段、
 * 改不同东西；导入任务与同步记录是同一张批次表按 kind 分开——一个是人发起的
 * 文件导入与回补，一个是系统自己跑的增量同步，出了问题要找的人不一样；
 * 素材映射查的根本不是数据源，而是平台对象与素材版本的对应关系。
 *
 * 每个列表都把「有问题的」排在最前面，这是 20 §4.1 对本页的硬要求：错误与
 * 延迟必须置顶，不能让人翻到第三屏才发现昨天的数据没同步进来。
 */
type ViewTarget = 'sources' | 'imports' | 'field-mapping' | 'asset-mapping' | 'syncs'

const viewTargets: Record<string, ViewTarget> = {
  数据源: 'sources',
  导入任务: 'imports',
  字段映射: 'field-mapping',
  素材映射: 'asset-mapping',
  同步记录: 'syncs',
}

const platformLabels: Record<ApiPlatform, string> = {
  douyin: '抖音',
  kuaishou: '快手',
  xiaohongshu: '小红书',
  wechat: '公众号',
  tencent_ads: '腾讯广告',
  other: '其他',
}

const ingestModeLabels: Record<ApiIngestMode, string> = {
  api: '官方 API',
  service_account: '服务账号',
  file_import: '报表导入',
  computer_use: '页面读取',
  business: '业务转化源',
}

const statusLabels: Record<ApiDataSourceStatus, string> = {
  draft: '未启用',
  active: '同步中',
  paused: '已暂停',
  revoked: '已撤销授权',
}

const qualityLabels: Record<ApiQualityStatus, string> = {
  healthy: '正常',
  delayed: '延迟',
  partial: '不完整',
  mapping_incomplete: '映射未完成',
  tracking_broken: '追踪异常',
  reconciling: '对账中',
  blocked: '已阻断',
}

const importStatusLabels: Record<string, string> = {
  pending: '待执行',
  running: '执行中',
  succeeded: '成功',
  partial: '部分成功',
  failed: '失败',
}

const importKindLabels: Record<string, string> = {
  sync: '增量同步',
  backfill: '历史回补',
  file: '文件导入',
  correction: '更正批次',
}

const mappingStatusLabels: Record<string, string> = {
  unmatched: '待匹配',
  matched: '已匹配',
  ignored: '已忽略',
}

const emptyHints: Record<ViewTarget, string> = {
  sources: '当前 Project 还没有接入任何数据源。点右上角「接入数据源」接一个，接入后才谈得上投后分析。',
  imports: '还没有人工发起过导入。文件导入、历史回补和更正批次会出现在这里。',
  'field-mapping': '还没有数据源，也就没有字段映射可配。字段映射把平台报表的列名对到统一指标名，没配完不允许导入。',
  'asset-mapping': '还没有平台对象回流。素材映射决定一条花费能不能算到某个素材头上，认不出来的会留在待匹配。',
  syncs: '还没有系统同步记录。官方 API 数据源启用后，每次增量同步都会在这里留一条。',
}

export function DataConnectionsPage({ state, activeView }: { state: DataState; activeView: string }) {
  const { currentProject } = useProject()
  const target = viewTargets[activeView] ?? 'sources'
  const [sources, setSources] = useState<ApiDataSource[]>([])
  const [batches, setBatches] = useState<ApiImportBatch[]>([])
  const [mappings, setMappings] = useState<ApiInsightAssetMapping[]>([])
  // 改归属时要从现有素材版本里挑，不能让人手填 ID。只在素材映射视图取。
  const [assets, setAssets] = useState<ApiInsightAsset[]>([])
  const [selectedId, setSelectedId] = useState('')
  // 新建数据源是一个独立的模式，不和详情并排。两者都住在右栏，同时出现时
  // 上下两组长得一样的表单会被读成「上面是这条的字段、下面还是这条的字段」，
  // 而实际上填下面那组是在造第二条数据源。一栏同一时刻只讲一件事。
  const [creating, setCreating] = useState(false)
  const [query, setQuery] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const loadList = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      if (target === 'asset-mapping') {
        const [mappingPage, assetPage] = await Promise.all([
          api.listInsightAssetMappings(currentProject.id),
          api.listInsightAssets(currentProject.id, {}),
        ])
        setMappings(mappingPage.items)
        setAssets(assetPage.items)
      } else if (target === 'imports' || target === 'syncs') {
        // 同一张表，两种来源：同步是系统跑的，导入是人发起的（doc10 §7）。
        const [batchPage, sourcePage] = await Promise.all([
          api.listImportBatches(currentProject.id),
          api.listDataSources(currentProject.id),
        ])
        setBatches(batchPage.items.filter(batch =>
          target === 'syncs' ? batch.kind === 'sync' : batch.kind !== 'sync'))
        setSources(sourcePage.items)
      } else {
        const next = await api.listDataSources(currentProject.id)
        setSources(next.items)
      }
      setListState('ready')
    } catch (cause) {
      setSources([])
      setBatches([])
      setMappings([])
      setAssets([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '数据接入读取失败。')
    }
  }, [currentProject.id, target])

  useEffect(() => { void loadList() }, [loadList])
  useEffect(() => { setNotice(''); setQuery(''); setCreating(false) }, [target])

  const matches = (text: string) => text.toLowerCase().includes(query.trim().toLowerCase())

  // 排序即优先级：坏掉的、慢的排最前，其余按最近更新（20 §4.1）。
  const visibleSources = useMemo(() => sources
    .filter(source => matches(`${sourceName(source)} ${source.account_ref ?? ''} ${statusLabels[source.status]} ${qualityLabels[source.quality_status]}`))
    .slice().sort((left, right) => sourceTrouble(right) - sourceTrouble(left)
      || right.updated_at.localeCompare(left.updated_at)),
  [sources, query])

  const visibleBatches = useMemo(() => batches
    .filter(batch => matches(`${batch.source_label ?? ''} ${importKindLabels[batch.kind] ?? batch.kind} ${importStatusLabels[batch.status] ?? batch.status} ${batch.error_summary ?? ''}`))
    .slice().sort((left, right) => batchTrouble(right) - batchTrouble(left)
      || right.created_at.localeCompare(left.created_at)),
  [batches, query])

  const visibleMappings = useMemo(() => mappings
    .filter(mapping => matches(`${mapping.platform} ${mapping.platform_object_id} ${mapping.platform_object_name ?? ''} ${mappingStatusLabels[mapping.status] ?? mapping.status}`))
    .slice().sort((left, right) => mappingTrouble(right) - mappingTrouble(left)
      || right.updated_at.localeCompare(left.updated_at)),
  [mappings, query])

  const rowCount = target === 'asset-mapping' ? visibleMappings.length
    : target === 'imports' || target === 'syncs' ? visibleBatches.length
      : visibleSources.length

  useEffect(() => {
    const ids = target === 'asset-mapping' ? visibleMappings.map(item => item.id)
      : target === 'imports' || target === 'syncs' ? visibleBatches.map(item => item.id)
        : visibleSources.map(item => item.id)
    setSelectedId(current => ids.includes(current) ? current : ids[0] ?? '')
  }, [target, visibleSources, visibleBatches, visibleMappings])

  const selectedSource = visibleSources.find(source => source.id === selectedId)
  const selectedBatch = visibleBatches.find(batch => batch.id === selectedId)
  const selectedMapping = visibleMappings.find(mapping => mapping.id === selectedId)

  // explain 把后端的通用错误码换成这个场景下的人话——后端对一整类冲突只回一句
  // 「当前状态不允许该操作」，不告诉你到底撞了哪条规则。
  const runWrite = async (label: string, work: () => Promise<unknown>, explain?: (code: string) => string) => {
    setBusy(true)
    try {
      await work()
      await loadList()
      setNotice(`${label}已生效。`)
    } catch (cause) {
      const specific = cause instanceof ApiRequestError ? explain?.(cause.code) : ''
      setNotice(specific || (cause instanceof Error ? cause.message : `${label}失败，请稍后重试。`))
    } finally {
      setBusy(false)
    }
  }

  return <StateBoundary state={state} onRetry={() => { void loadList() }}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">DATA CONNECTIONS</span>
            <h2>{headings[target].title}</h2>
            <p>当前 Project：{currentProject.name}。{headings[target].blurb}</p>
          </div>
          <div className="core-flow-actions">
            {target === 'sources'
              ? <button className="secondary-button" disabled={busy}
                onClick={() => setCreating(current => !current)}><Plug size={15}/>{creating ? '取消接入' : '接入数据源'}</button>
              : null}
            <button className="secondary-button" disabled={listState === 'loading'} onClick={() => { void loadList() }}><RefreshCw size={15}/>刷新</button>
          </div>
        </div>
        <div className="prelaunch-filterbar">
          <div className="search-field"><Search size={15}/><input aria-label="搜索" value={query} onChange={event => setQuery(event.target.value)} placeholder={headings[target].search}/></div>
          <span>{rowCount} {headings[target].unit}</span>
        </div>
        <div className="prelaunch-table" role="list" aria-label={headings[target].title}>
          <div className="prelaunch-row header">{headings[target].columns.map(column => <span key={column}>{column}</span>)}</div>
          {listState === 'loading' ? <div className="panel-empty">正在读取当前 Project 的数据接入情况…</div> : null}
          {listState === 'error' ? <div className="panel-empty">读取失败，请重试。</div> : null}
          {listState === 'ready' && !rowCount ? <div className="panel-empty">{emptyHints[target]}</div> : null}

          {(target === 'sources' || target === 'field-mapping') && visibleSources.map(source =>
            <button role="listitem" key={source.id} className={selectedId === source.id ? 'prelaunch-row active' : 'prelaunch-row'}
              onClick={() => { setSelectedId(source.id); setCreating(false) }}>
              <span><b>{sourceName(source)}</b><small>{ingestModeLabels[source.ingest_mode]} · {source.account_ref || '未填账户标识'} · 更新于 {formatTime(source.updated_at)}</small></span>
              <span>{target === 'field-mapping' ? `${Object.keys(source.field_mapping ?? {}).length} 个字段` : statusLabels[source.status]}</span>
              <span>{target === 'field-mapping'
                ? (Object.keys(source.field_mapping ?? {}).length ? '已配置，可导入' : '未配置，导入会被拒绝')
                : `${qualityLabels[source.quality_status]}${source.quality_note ? ` · ${source.quality_note}` : ''}`}</span>
              <span>{sourceTrouble(source) ? <CircleAlert size={14}/> : <CircleCheck size={14}/>}{freshnessLabel(source)}</span>
            </button>)}

          {(target === 'imports' || target === 'syncs') && visibleBatches.map(batch =>
            <button role="listitem" key={batch.id} className={selectedId === batch.id ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(batch.id)}>
              <span><b>{batch.source_label || importKindLabels[batch.kind] || batch.kind}</b><small>{sourceName(sources.find(source => source.id === batch.data_source_id))} · {formatTime(batch.created_at)}</small></span>
              <span>{importStatusLabels[batch.status] ?? batch.status}</span>
              <span>{batch.accepted_rows} 行入库{batch.rejected_rows ? ` · ${batch.rejected_rows} 行被拒` : ''}{batch.error_summary ? ` · ${batch.error_summary}` : ''}</span>
              <span>{batchTrouble(batch) ? <CircleAlert size={14}/> : <CircleCheck size={14}/>}{importKindLabels[batch.kind] ?? batch.kind}</span>
            </button>)}

          {target === 'asset-mapping' && visibleMappings.map(mapping => {
            // 归属列要给名字。素材短号（ast_9f3c…）在这一列里认不出是哪条创意，
            // 而这一列问的正是「这笔钱花在哪条创意上」。
            const owner = mapping.asset_id ? assets.find(asset => asset.id === mapping.asset_id) : undefined
            return <button role="listitem" key={mapping.id} className={selectedId === mapping.id ? 'prelaunch-row active' : 'prelaunch-row'} onClick={() => setSelectedId(mapping.id)}>
              <span><b>{mapping.platform_object_name || mapping.platform_object_id}</b><small>{platformLabels[mapping.platform as ApiPlatform] ?? mapping.platform} · {mapping.platform_object_kind} · {mapping.platform_object_id}</small></span>
              <span>{mappingStatusLabels[mapping.status] ?? mapping.status}</span>
              <span>{mapping.asset_id
                ? owner ? owner.title : `素材 ${shortId(mapping.asset_id)}（已不在当前列表里）`
                : mapping.note || '还没认领，花费计入总盘但不算到任何素材头上'}</span>
              <span>
                {mapping.status === 'matched' ? <CircleCheck size={14}/> : <CircleAlert size={14}/>}
                {owner ? `第 ${owner.revision} 版` : mapping.asset_id ? '查不到版本' : '未认领'}
              </span>
            </button>
          })}
        </div>
      </section>

      <aside className="prelaunch-detail">
        {target === 'sources' ? <DataSourceDetail source={selectedSource} busy={busy} projectId={currentProject.id}
          creating={creating} onCancelCreate={() => setCreating(false)}
          onWrite={runWrite} onCreated={() => setCreating(false)}/> : null}
        {target === 'field-mapping' ? <FieldMappingDetail source={selectedSource} busy={busy} projectId={currentProject.id} onWrite={runWrite}/> : null}
        {target === 'imports' ? <ImportDetail batch={selectedBatch} sources={sources} busy={busy} projectId={currentProject.id} onWrite={runWrite}/> : null}
        {target === 'syncs' ? <SyncDetail batch={selectedBatch} sources={sources}/> : null}
        {target === 'asset-mapping' ? <AssetMappingDetail mapping={selectedMapping} assets={assets}
          projectId={currentProject.id} onDone={message => { setNotice(message); void loadList() }}/> : null}
        {notice ? <div className="inline-notice" role="status">{notice}</div> : null}
      </aside>
    </div>
  </StateBoundary>
}

// --- 数据源 ---

function DataSourceDetail({ source, busy, projectId, creating, onCancelCreate, onWrite, onCreated }: {
  source?: ApiDataSource
  busy: boolean
  projectId: string
  creating: boolean
  onCancelCreate: () => void
  onWrite: (label: string, work: () => Promise<unknown>, explain?: (code: string) => string) => Promise<void>
  onCreated: () => void
}) {
  const [platform, setPlatform] = useState<ApiPlatform>('douyin')
  const [ingestMode, setIngestMode] = useState<ApiIngestMode>('file_import')
  const [accountLabel, setAccountLabel] = useState('')
  const [accountRef, setAccountRef] = useState('')
  const [credentialRef, setCredentialRef] = useState('')
  const [qualityStatus, setQualityStatus] = useState<ApiQualityStatus>('healthy')
  const [qualityNote, setQualityNote] = useState('')

  useEffect(() => {
    setQualityStatus(source?.quality_status ?? 'healthy')
    setQualityNote(source?.quality_note ?? '')
  }, [source])

  const register = () => onWrite('接入数据源', async () => {
    await api.registerDataSource(projectId, {
      platform,
      ingest_mode: ingestMode,
      account_label: accountLabel.trim(),
      account_ref: accountRef.trim(),
      credential_ref: credentialRef.trim(),
    })
    setAccountLabel('')
    setAccountRef('')
    setCredentialRef('')
    onCreated()
  })

  const mappedFields = Object.keys(source?.field_mapping ?? {}).length

  // 新建模式独占右栏：接入表单和某条数据源的详情不同时出现，避免两组同样的
  // 字段读成同一件事，也避免「启用数据源」和「接入数据源」两个主按钮并排。
  if (creating) return <>
    <span className="section-label">接入一个新数据源</span><h3>这个账户的数据从哪来</h3>
    <p>新接入的数据源是「未启用」，配完字段映射才能启用——这是防止半配好的映射把脏数据导进来。</p>
    <label className="experience-reason">
      <small>平台</small>
      <select value={platform} onChange={event => setPlatform(event.target.value as ApiPlatform)}>
        {Object.entries(platformLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    <label className="experience-reason">
      <small>接入方式</small>
      <select value={ingestMode} onChange={event => setIngestMode(event.target.value as ApiIngestMode)}>
        {Object.entries(ingestModeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    <label className="experience-reason">
      <small>账户名称</small>
      <input value={accountLabel} onChange={event => setAccountLabel(event.target.value)} placeholder="例如：品牌主账户"/>
    </label>
    <label className="experience-reason">
      <small>账户标识 · 平台后台里的账户 ID（同一项目下同平台同账户只能接一次）</small>
      <input value={accountRef} onChange={event => setAccountRef(event.target.value)} placeholder="例如：1234567890 或 adv-1234567890"/>
      <small>只能用英文字母、数字和 - _ . : / @。中文名字填上面的「账户名称」。</small>
    </label>
    <label className="experience-reason">
      <small>凭据引用键（不是凭据本身）</small>
      <input value={credentialRef} onChange={event => setCredentialRef(event.target.value)} placeholder="例如：vault://douyin/brand-main"/>
    </label>
    <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>这里不要粘凭据</small>只填密钥服务里的引用键，凭据不进业务库。含 bearer、access_token、refresh_token、secret、password 字样或超过 128 字的值会被后端直接拒绝。</span></div>
    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={busy} onClick={() => void register()}><Plug size={15}/>{busy ? '处理中…' : '接入数据源'}</button>
      <button className="secondary-button full" disabled={busy} onClick={onCancelCreate}>取消</button>
    </div>
  </>

  return <>
    {source ? <>
      <span className="section-label">当前数据源</span><h3>{sourceName(source)}</h3>
      <p>{ingestModeLabels[source.ingest_mode]} · {statusLabels[source.status]} · v{source.version}</p>
      <div className="prelaunch-fact"><Database size={17}/><span><small>口径</small><b>{source.caliber.currency} · 归因 {source.caliber.attribution_window} · 指标口径 {source.caliber.metric_schema_version} · 时区 {source.caliber.time_zone}</b></span></div>
      <div className="prelaunch-fact"><Link2 size={17}/><span><small>数据覆盖到</small><b>{source.data_through ? formatDate(source.data_through) : '还没有任何数据'}{source.last_synced_at ? ` · 最近同步 ${formatTime(source.last_synced_at)}` : ''}</b></span></div>
      {sourceTrouble(source) ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>{qualityLabels[source.quality_status]}</small>{source.quality_note || '这个状态会阻止它的数字算出能归因的结论，也不会触发自动优化动作。'}</span></div> : null}
      {!mappedFields ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>字段映射未配置</small>没配完字段映射不允许导入，也不能启用。去「字段映射」视图补齐。</span></div> : null}

      <label className="experience-reason">
        <small>质量状态（正常以外必须写原因，会显示在每一张用到它的图旁边）</small>
        <select value={qualityStatus} onChange={event => setQualityStatus(event.target.value as ApiQualityStatus)}>
          {Object.entries(qualityLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select>
        <textarea value={qualityNote} onChange={event => setQualityNote(event.target.value)} rows={2} placeholder="例如：平台报表延迟一天，今天的数字还会变。"/>
      </label>
      <div className="prelaunch-actions">
        <button className="secondary-button full" disabled={busy} onClick={() => void onWrite('质量状态', () =>
          api.setDataSourceQuality(projectId, source.id, {
            expected_version: source.version, quality_status: qualityStatus, note: qualityNote.trim(),
          }))}>{busy ? '处理中…' : '记录质量状态'}</button>
        {source.status === 'active'
          ? <button className="secondary-button full" disabled={busy} onClick={() => void onWrite('暂停', () =>
            api.updateDataSource(projectId, source.id, { expected_version: source.version, status: 'paused' }))}>暂停同步</button>
          : <button className="primary-button full" disabled={busy || !mappedFields} onClick={() => void onWrite('启用', () =>
            api.updateDataSource(projectId, source.id, { expected_version: source.version, status: 'active' }))}>{mappedFields ? '启用数据源' : '先配字段映射'}</button>}
      </div>
    </> : <div className="panel-empty">左侧选一个数据源查看口径、新鲜度和质量状态；要接新的，点右上角「接入数据源」。</div>}
  </>
}

// --- 字段映射 ---

function FieldMappingDetail({ source, busy, projectId, onWrite }: {
  source?: ApiDataSource
  busy: boolean
  projectId: string
  onWrite: (label: string, work: () => Promise<unknown>, explain?: (code: string) => string) => Promise<void>
}) {
  // 一个指标名一格，人只填自己报表里的表头。以前是让人在文本框里手拼
  // 「平台列名 = 指标名」——等号那套格式是给机器看的，人还得记住十一个英文名
  // 怎么拼，打错一个字母就是一次失败的导入，而报错只会说「这列对不上」。
  const [columns, setColumns] = useState<Record<string, string>>({})

  useEffect(() => { setColumns(mappingToColumns(source?.field_mapping)) }, [source])

  if (!source) return <div className="panel-empty">左侧选一个数据源，配置它的报表列名怎么对到统一指标名。</div>

  const filled = Object.values(columns).filter(value => value.trim()).length

  return <>
    <span className="section-label">字段映射</span><h3>{sourceName(source)}</h3>
    <p>左边填你报表里的表头原文，一字不差。留空表示这份报表没有这一项——不是错误，系统按「这个平台不提供」处理。</p>

    <div className="mapping-form">
      {metricColumns.map(column => {
        const aliases = builtInAliasesOf(column.key)
        return <label key={column.key} className={column.required ? 'mapping-field required' : 'mapping-field'}>
          <small>
            {column.label}
            {column.required ? ' · 必填' : ''}
            {column.note ? ` · ${column.note}` : ''}
          </small>
          <input
            value={columns[column.key] ?? ''}
            onChange={event => setColumns(current => ({ ...current, [column.key]: event.target.value }))}
            // 内置别名当占位符：表头正好是这几个写法之一就不用填，留空也认得出来。
            placeholder={aliases.length ? `${aliases[0]}（这几个写法已内置认得，可留空）` : '你报表里的表头'}/>
          <em>{column.key}</em>
        </label>
      })}
    </div>

    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={busy} onClick={() => void onWrite('字段映射', () =>
        api.updateDataSource(projectId, source.id, {
          expected_version: source.version, field_mapping: columnsToMapping(columns),
        }))}>{busy ? '处理中…' : `保存字段映射（填了 ${filled} 项）`}</button>
    </div>
    <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>整体替换</small>保存会用这里的内容整体覆盖原映射，不是增量合并。清空某一格就是把那条映射删掉。</span></div>
  </>
}

// --- 导入任务 ---

function ImportDetail({ batch, sources, busy, projectId, onWrite }: {
  batch?: ApiImportBatch
  sources: ApiDataSource[]
  busy: boolean
  projectId: string
  onWrite: (label: string, work: () => Promise<unknown>, explain?: (code: string) => string) => Promise<void>
}) {
  const importable = sources.filter(source => Object.keys(source.field_mapping ?? {}).length)
  const [dataSourceId, setDataSourceId] = useState('')
  const [sourceLabel, setSourceLabel] = useState('')
  const [text, setText] = useState('')
  const [parseError, setParseError] = useState('')

  useEffect(() => {
    setDataSourceId(current => importable.some(source => source.id === current) ? current : importable[0]?.id ?? '')
  }, [sources])

  const submit = () => {
    let rows: ApiMetricRow[]
    try {
      // 用这个数据源自己配的字段映射解析。以前只认一张硬编码的中文别名表，
      // 平台把「曝光」导成「曝光量」就对不上，然后静默填 0 导进去。
      const source = importable.find(candidate => candidate.id === dataSourceId)
      rows = parseMetricCsv(text, source?.field_mapping ?? {})
    } catch (cause) {
      setParseError(cause instanceof Error ? cause.message : '表格解析失败。')
      return
    }
    setParseError('')
    void onWrite('导入', async () => {
      await api.importMetrics(projectId, {
        data_source_id: dataSourceId,
        kind: 'file',
        source_label: sourceLabel.trim() || '手工导入',
        content_hash: hashText(text),
        rows,
        register_objects: true,
      })
      setText('')
    }, code => code === 'INVALID_STATE'
      ? '这份内容之前已经导过了，系统拦下来防止重复计花费。要重导，先在平台侧改好再作为更正批次提交；数据源被暂停时也导不进来。'
      : '')
  }

  return <>
    {batch ? <>
      <span className="section-label">当前批次</span><h3>{batch.source_label || importKindLabels[batch.kind] || batch.kind}</h3>
      <p>{importKindLabels[batch.kind] ?? batch.kind} · {importStatusLabels[batch.status] ?? batch.status} · {formatTime(batch.created_at)}</p>
      <div className="prelaunch-fact"><CircleCheck size={17}/><span><small>结果</small><b>提交 {batch.requested_rows} 行，入库 {batch.accepted_rows} 行，被拒 {batch.rejected_rows} 行</b></span></div>
      <div className="prelaunch-fact"><Database size={17}/><span><small>数据源与范围</small><b>{sourceName(sources.find(source => source.id === batch.data_source_id))}{batch.window_start ? ` · ${formatDate(batch.window_start)} ~ ${formatDate(batch.window_end ?? batch.window_start)}` : ''}</b></span></div>
      {batch.errors?.length ? <div className="feature-stack">
        <span>被拒的行</span>
        {batch.errors.map((message, index) => <b key={index}>{message}</b>)}
      </div> : null}
      {batch.status === 'partial' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>部分成功</small>没被拒的行已经入库了。修好上面这些行之后重新导一次，同一天的数据会被覆盖而不是加倍。</span></div> : null}
    </> : <div className="panel-empty">左侧选一个批次，查看它导进去多少行、拒了哪些行。</div>}

    <div className="feature-stack">
      <span>导入一份报表</span>
      <b>同一份内容重复导会被拦下；确实要重导，先在平台侧改好再作为更正批次提交。同一天同一个对象重导是覆盖，不会让花费翻倍。</b>
    </div>
    {importable.length ? <>
      <label className="experience-reason">
        <small>导入到哪个数据源（只列出已配好字段映射的）</small>
        <select value={dataSourceId} onChange={event => setDataSourceId(event.target.value)}>
          {importable.map(source => <option key={source.id} value={source.id}>{sourceName(source)}</option>)}
        </select>
      </label>
      <label className="experience-reason">
        <small>批次名称</small>
        <input value={sourceLabel} onChange={event => setSourceLabel(event.target.value)} placeholder="例如：抖音 7 月第 4 周报表"/>
      </label>
      <label className="experience-reason">
        <small>粘贴报表内容（第一行是表头，逗号分隔）</small>
        <textarea value={text} onChange={event => setText(event.target.value)} rows={6}
          placeholder={'platform_object_id,stat_date,impressions,clicks,conversions,spend_cents\nAD-1001,2026-07-20,12000,340,18,86000'}/>
      </label>
      {parseError ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>一行都没导入</small>{parseError}</span></div> : null}
      <div className="prelaunch-actions">
        <button className="primary-button full" disabled={busy || !dataSourceId || !text.trim()} onClick={submit}><Upload size={15}/>{busy ? '处理中…' : '导入'}</button>
      </div>
    </> : <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>还不能导入</small>没有任何数据源配好了字段映射。先去「字段映射」视图配一个。</span></div>}
  </>
}

// --- 同步记录 ---

function SyncDetail({ batch, sources }: { batch?: ApiImportBatch; sources: ApiDataSource[] }) {
  if (!batch) {
    return <>
      <div className="panel-empty">左侧选一条同步记录，查看它覆盖的时间范围和失败原因。</div>
      <div className="feature-stack">
        <span>同步记录和导入任务的区别</span>
        <b>同步是系统按数据源配置自己跑的；导入是人发起的。前者出问题要查授权和平台接口，后者出问题要查报表本身。</b>
      </div>
    </>
  }
  return <>
    <span className="section-label">同步记录</span><h3>{sourceName(sources.find(source => source.id === batch.data_source_id))}</h3>
    <p>{importStatusLabels[batch.status] ?? batch.status} · {formatTime(batch.created_at)}{batch.finished_at ? ` · 结束于 ${formatTime(batch.finished_at)}` : ''}</p>
    <div className="prelaunch-fact"><CircleCheck size={17}/><span><small>结果</small><b>入库 {batch.accepted_rows} 行，被拒 {batch.rejected_rows} 行</b></span></div>
    <div className="prelaunch-fact"><Database size={17}/><span><small>覆盖范围</small><b>{batch.window_start ? `${formatDate(batch.window_start)} ~ ${formatDate(batch.window_end ?? batch.window_start)}` : '未记录时间范围'}</b></span></div>
    {batch.error_summary ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>失败原因</small>{batch.error_summary}</span></div> : null}
    {batch.errors?.length ? <div className="feature-stack"><span>逐行原因</span>{batch.errors.map((message, index) => <b key={index}>{message}</b>)}</div> : null}
  </>
}

// --- 素材映射 ---

/**
 * 这里是全部平台对象（含已认领的），所以改归属只能在这一屏做：别处列的都是
 * 未认领队列，认领完那条就从队列里消失了，想改回来就再也找不到它。
 */
function AssetMappingDetail({ mapping, assets, projectId, onDone }: {
  mapping?: ApiInsightAssetMapping
  assets: ApiInsightAsset[]
  projectId: string
  onDone: (message: string) => void
}) {
  if (!mapping) return <div className="panel-empty">左侧选一个平台对象，查看它归到了哪个素材版本。</div>
  // 归属这一行以前直接把 asset_id 整串打出来。那串东西回答不了「这笔钱花在哪条创意上」，
  // 而这正是人点进来要问的。名字在前，短号在后当核对用。
  const owner = mapping.asset_id ? assets.find(asset => asset.id === mapping.asset_id) : undefined
  return <>
    <span className="section-label">素材映射</span><h3>{mapping.platform_object_name || mapping.platform_object_id}</h3>
    <p>{platformLabels[mapping.platform as ApiPlatform] ?? mapping.platform} · {mapping.platform_object_kind} · {mappingStatusLabels[mapping.status] ?? mapping.status}</p>
    <div className="prelaunch-fact"><Link2 size={17}/><span><small>归到哪个素材</small><b>
      {!mapping.asset_id ? '还没认领'
        : owner ? `${owner.title} · 第 ${owner.revision} 版 · ${shortId(owner.id)}`
        : `${shortId(mapping.asset_id)}（这条素材不在当前列表里，可能已作废）`}
    </b></span></div>
    <div className="prelaunch-fact"><Database size={17}/><span><small>怎么认的</small><b>{mapping.match_source === 'human' ? `人工指定${mapping.matched_by ? ` · ${mapping.matched_by}` : ''}` : mapping.match_source === 'auto' ? '系统自动匹配' : '尚未匹配'}{mapping.matched_at ? ` · ${formatTime(mapping.matched_at)}` : ''}</b></span></div>
    {mapping.note ? <div className="prelaunch-fact"><CircleCheck size={17}/><span><small>备注</small><b>{mapping.note}</b></span></div> : null}
    {mapping.status !== 'matched' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>没认领的后果</small>它的花费仍然计入总盘，但不会算到任何素材头上，也不能支撑「这条创意更好」这类结论。就在下面那一格里挑素材认领，不用去别处。</span></div> : null}
    <MappingResolveForm key={mapping.id} mapping={mapping} assets={assets} projectId={projectId} onDone={onDone}/>
    {mapping.status === 'matched' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span><small>改归属会动历史数字</small>这条对象过往所有天的花费都会跟着换主人，不是从今天起才改。原来那一版素材的投后结论如果已经出过报告，报告里的数字不会自动更新——那是定格的快照，得重新出一份。</span></div> : null}
  </>
}

// --- 文案与工具 ---

const headings: Record<ViewTarget, { title: string; blurb: string; search: string; unit: string; columns: string[] }> = {
  sources: {
    title: '接进来的每一个账户，都要说得清它的数据从哪来、可不可信',
    blurb: '一个数据源就是一个平台账户。有问题的排在最前面。',
    search: '搜索平台、账户或状态',
    unit: '个数据源',
    columns: ['数据源', '状态', '数据质量', '新鲜度'],
  },
  imports: {
    title: '人发起的每一次导入，都留下导了多少、拒了多少',
    blurb: '文件导入、历史回补和更正批次都在这里。失败和部分成功排在最前面。',
    search: '搜索批次、状态或错误',
    unit: '个批次',
    columns: ['批次', '状态', '结果', '类型'],
  },
  'field-mapping': {
    title: '平台报表的列名，对到系统统一的指标名',
    blurb: '没配完字段映射的数据源不允许导入，也不能启用。未配置的排在最前面。',
    search: '搜索数据源',
    unit: '个数据源',
    columns: ['数据源', '已配字段', '是否可导入', '新鲜度'],
  },
  'asset-mapping': {
    title: '平台上的这条广告，到底是我们哪个素材版本',
    blurb: '认不出来的排在最前面——它的花费算进总盘，但撑不起任何创意级结论。',
    search: '搜索平台对象',
    unit: '个平台对象',
    // 第四列以前写「版本」，显示的却是这条映射记录自己的乐观锁版本号（v3 = 被改过三次）
    // ——标题问的是「哪个素材版本」，这个数字答的是「这行记录改过几次」，风马牛不相及。
    // 现在直接给素材版本；一条还没认领的自然没有版本可给。
    columns: ['平台对象', '状态', '归到哪个素材', '素材第几版'],
  },
  syncs: {
    title: '系统自己跑的每一次同步，成功和失败都留痕',
    blurb: '这些是按数据源配置自动执行的增量同步。失败的排在最前面。',
    search: '搜索数据源或状态',
    unit: '条同步记录',
    columns: ['同步', '状态', '结果', '类型'],
  },
}

function sourceName(source?: ApiDataSource): string {
  if (!source) return '未知数据源'
  return `${platformLabels[source.platform] ?? source.platform} · ${source.account_label || source.account_ref || '未命名账户'}`
}

/** 排序权重：数字越大越靠前。坏掉的比慢的更急，慢的比没配完的更急。 */
function sourceTrouble(source: ApiDataSource): number {
  if (source.quality_status === 'blocked' || source.quality_status === 'tracking_broken') return 4
  if (source.quality_status === 'delayed' || source.quality_status === 'partial') return 3
  if (source.quality_status !== 'healthy') return 2
  if (!Object.keys(source.field_mapping ?? {}).length) return 1
  return 0
}

function batchTrouble(batch: ApiImportBatch): number {
  if (batch.status === 'failed') return 3
  if (batch.status === 'partial') return 2
  if (batch.status === 'pending' || batch.status === 'running') return 1
  return 0
}

function mappingTrouble(mapping: ApiInsightAssetMapping): number {
  return mapping.status === 'unmatched' ? 1 : 0
}

function freshnessLabel(source: ApiDataSource): string {
  if (!source.data_through) return '无数据'
  const days = Math.floor((Date.now() - new Date(source.data_through).valueOf()) / 86_400_000)
  if (Number.isNaN(days)) return '无数据'
  if (days <= 0) return '当天'
  return `落后 ${days} 天`
}

/**
 * 存的是「平台列名 → 指标名」，表单要的是「指标名 → 平台列名」，这里翻过来。
 *
 * 同一个指标被两个列名映射（一份报表里「消耗」和「花费」两列都指向花费）在表单里
 * 表达不了，这里只取第一个。真出现这种报表，那两列本身就该先在平台侧合并。
 */
function mappingToColumns(mapping?: Record<string, string>): Record<string, string> {
  const columns: Record<string, string> = {}
  for (const [column, metric] of Object.entries(mapping ?? {})) {
    if (!columns[metric]) columns[metric] = column
  }
  return columns
}

function columnsToMapping(columns: Record<string, string>): Record<string, string> {
  const mapping: Record<string, string> = {}
  for (const [metric, column] of Object.entries(columns)) {
    // 留空是「这份报表没有这一项」，不写进映射；写进去会变成一条空键的映射，
    // 解析时每一列都能匹配上它。
    if (column.trim()) mapping[column.trim()] = metric
  }
  return mapping
}

function formatTime(value: string): string {
  const time = new Date(value)
  return Number.isNaN(time.valueOf()) ? value : time.toLocaleString('zh-CN')
}

function formatDate(value: string): string {
  const time = new Date(value)
  return Number.isNaN(time.valueOf()) ? value : time.toLocaleDateString('zh-CN')
}
