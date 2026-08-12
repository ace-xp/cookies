import { useEffect, useState } from 'react'
import { CircleAlert, ExternalLink, Layers3, Ruler, Sparkles } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiFeatureField, type ApiInsightAsset, type ApiInsightAssetFeature } from '../../../data/api'
import { admissibleForAttribution, featureSourceLabel } from '../../../data/featureSource'
import { shortId } from '../../../data/shortId'

const statusLabels: Record<string, string> = {
  awaiting_data: '待数据', awaiting_match: '待匹配', analysable: '可分析', analysing: '分析中',
  pending_confirmation: '待确认', confirmed: '已确认', needs_review: '待复审', retired: '已失效',
}

/**
 * 单条素材。
 *
 * **上半截和下半截视觉上是分开的**：上半截是素材库那边的东西（预览、时长、版本、
 * 血缘），下半截才是洞察自己的（内容变量、投放数据、分析状态）。分开是为了让人
 * 知道哪些东西改了要去别处改——混在一屏里，人会在这里找编辑时长的按钮然后找不到。
 *
 * 上半截现在**没有数据可读**：洞察侧只存了 PlatformAssetID / PlatformAssetVersion
 * 两个裸字段，没有真链接。所以这里只显示这两个号码和跳转，并明写「预览与版本信息
 * 在素材库那边」。不为了让它好看而在洞察这边复制一份素材元数据——复制出来的那份
 * 第二天就和素材库对不上了。真接上是创意侧交接时的事。
 */
export function AssetDetail({ asset, onOpenLibrary }: {
  asset: ApiInsightAsset
  onOpenLibrary: () => void
}) {
  const { currentProject } = useProject()
  const [features, setFeatures] = useState<ApiInsightAssetFeature[]>([])
  // 素材版本单独存一份。量一次客观变量会让它加一，而外面那份 asset 是列表拉下来的
  // 快照——不跟着走的话，连按两次「量一下」第二次必然撞乐观锁。
  const [version, setVersion] = useState(asset.version)
  const [measuring, setMeasuring] = useState(false)
  const [measureNote, setMeasureNote] = useState('')
  // 特征体系。取它只为了两件事：把 duration 显示成「时长」，把 15 显示成「15 秒」。
  // 名字和单位的权威定义在后端 features.go，不在这儿写死一份。
  const [fields, setFields] = useState<Record<string, ApiFeatureField>>({})

  useEffect(() => {
    setVersion(asset.version)
    setMeasureNote('')
    let active = true
    void api.listInsightAssetFeatures(currentProject.id, asset.id)
      .then(page => { if (active) setFeatures(page.items) })
      .catch(() => { if (active) setFeatures([]) })
    return () => { active = false }
  }, [currentProject.id, asset.id, asset.version])

  useEffect(() => {
    if (!asset.asset_type) { setFields({}); return }
    let active = true
    void api.listFeatureSchemas(currentProject.id)
      .then(page => {
        if (!active) return
        const schema = page.items.find(item => item.asset_type === asset.asset_type)
        setFields(Object.fromEntries((schema?.fields ?? []).map(field => [field.key, field])))
      })
      .catch(() => { if (active) setFields({}) })
    return () => { active = false }
  }, [currentProject.id, asset.asset_type])

  const admissible = features.filter(feature => admissibleForAttribution(feature.source))
  const derived = features.filter(feature => feature.source === 'derived')

  // 量客观变量。没有文件引用就不必按了——洞察这边只有一条索引，量不到东西。
  const hasFile = Boolean(asset.platform_asset_id && asset.platform_asset_version)
  async function measure() {
    setMeasuring(true)
    setMeasureNote('')
    try {
      const page = await api.deriveInsightAssetFeatures(currentProject.id, asset.id, { expected_version: version })
      setFeatures(page.items)
      // 服务端每写一次就把素材版本推一格，哪怕状态没动。
      setVersion(current => current + 1)
      setMeasureNote(`量到 ${page.items.filter(item => item.source === 'derived').length} 项。`)
    } catch (error) {
      setMeasureNote(error instanceof Error ? error.message : '量不出来。')
    } finally {
      setMeasuring(false)
    }
  }

  return <div className="asset-detail">
    <section className="asset-detail-upper">
      <div className="asset-detail-upper-head">
        <span className="section-label">素材库那边的</span>
        <button type="button" className="secondary-button" onClick={onOpenLibrary}>
          <ExternalLink size={14}/>去素材库
        </button>
      </div>
      <h3>{asset.title}</h3>
      <p>预览与版本信息在素材库那边。洞察这边只记住它是哪一条，不复制一份元数据过来
        ——复制出来的那份第二天就和素材库对不上了。</p>
      <div className="prelaunch-fact"><Layers3 size={17}/><span><small>平台素材号</small><b>
        {asset.platform_asset_id || '还没有。这条素材还没和平台上的对象对上号。'}
        {asset.platform_asset_version ? ` · 第 ${asset.platform_asset_version} 版` : ''}
      </b></span></div>
    </section>

    <section className="asset-detail-lower">
      <span className="section-label">洞察这边的</span>
      <div className="prelaunch-fact"><Layers3 size={17}/><span><small>分析状态</small><b>
        {statusLabels[asset.analysis_status] ?? asset.analysis_status}
        {asset.analysis_status_reason ? ` · ${asset.analysis_status_reason}` : ''}
      </b></span></div>

      <div className="feature-stack">
        <span>内容变量（{features.length} 项 · 其中 {admissible.length} 项能进归因）</span>
        {features.length ? features.map(feature => <b key={feature.id}>
          {fields[feature.key]?.label ?? feature.key}：{formatValue(feature.value)}
          {feature.value.number !== undefined ? fields[feature.key]?.unit ?? '' : ''}
          <small>（{featureSourceLabel[feature.source]}）</small>
        </b>) : <b>还没有内容变量。去「素材 · 变量」给它提一次。</b>}
      </div>

      {/* 量客观变量。和「提取」分开摆是有意的：那个要调模型、要人复核，这个读的是
          素材库上传时就探测好的数，按几次结果都一样，也不进复核队列。 */}
      <div className="feature-measure">
        <button type="button" className="secondary-button" disabled={!hasFile || measuring} onClick={() => void measure()}>
          <Ruler size={14}/>{measuring ? '量着…' : derived.length ? '重新量一下' : '量一下'}
        </button>
        <small>{hasFile
          ? '从素材库那个文件本身量出时长和画幅。不调模型，直接能进归因。'
          : '这条素材没指向素材库里的文件，量不到东西。从创意导入的才带这个引用。'}</small>
        {measureNote ? <small className="feature-measure-note">{measureNote}</small> : null}
      </div>

      {features.length && !admissible.length ? <div className="prelaunch-boundary">
        <CircleAlert size={16}/><span><small>这些变量还不能拿来归因</small>
          它们现在全是模型猜的。要让它们算数有两条路：能量的先量一下（时长、画幅），
          剩下靠判断的得有人逐项看过并确认——在「素材 · 变量」里做。
        </span></div> : null}

      <div className="prelaunch-fact"><Sparkles size={17}/><span><small>编号</small><b>
        {shortId(asset.id)} · 血缘 {shortId(asset.lineage_id)} · 第 {asset.revision} 版
      </b></span></div>
    </section>
  </div>
}

function formatValue(value: ApiInsightAssetFeature['value']): string {
  if (value.text) return value.text
  if (value.terms?.length) return value.terms.join('、')
  if (value.number !== undefined) return String(value.number)
  if (value.bool !== undefined) return value.bool ? '是' : '否'
  return '—'
}
