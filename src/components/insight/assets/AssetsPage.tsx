import { useEffect, useMemo, useState } from 'react'
import { CircleAlert } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiInsightAsset } from '../../../data/api'
import type { DataState } from '../../../types'
import { StateBoundary } from '../../StateBoundary'
import { AssetIndexForm } from '../../AssetLibraryPage'
import { isoDate } from '../analysis/format'
import { AssetDetail } from './AssetDetail'
import { ExternalView } from './ExternalView'
import { FeatureView } from './FeatureView'
import { ImportFromCreative } from './ImportFromCreative'
import { IntakeView } from './IntakeView'
import { OverviewView } from './OverviewView'
import { SimilarView } from './SimilarView'

/**
 * 「素材」入口。原来的三个入口——数据接入、分析素材库、内容分析——合成这一个。
 *
 * 合并的理由是：这三件事回答的是同一个问题「这条素材能不能进复盘」。一条素材要能
 * 进复盘必须齐三样：对上号（素材映射）、有内容变量（内容分析）、有投放数据（数据
 * 接入）。分成三个入口，人得在三个地方各查一遍才知道差在哪。
 *
 * 另外补了两件原来没有的：按变量重叠找相似素材（❓「算不出来」的升级通道），
 * 以及把平台外的素材作为**证据**收进来。
 */
export type AssetsView = 'overview' | 'intake' | 'features' | 'similar' | 'external'

const headings: Record<AssetsView, { label: string; title: string; lead: string }> = {
  overview: {
    label: 'ASSETS',
    title: '手上有哪些素材，它们还差什么才能进复盘',
    lead: '左边是平台内的（躺在素材库里），右边是外部证据（洞察自己存的，有到期日）。下面三个队列是还差的那几样。',
  },
  intake: {
    label: 'DATA INTAKE',
    title: '把平台上的投放数据接进来',
    lead: '一条素材要能进复盘必须齐三样：对上号、有内容变量、有投放数据。这一段管第三样。',
  },
  features: {
    label: 'FEATURES',
    title: '把创意内容转成可比较的变量',
    lead: '每个变量都标了来源。归因只认量出来的和人标的，模型猜的只能参考。',
  },
  similar: {
    label: 'SIMILAR',
    title: '按变量重叠找相似素材',
    lead: '本轮样本不够时，从库里把同样取值的素材拉过来，样本就厚了。每条结果都说得清「像在哪」。',
  },
  external: {
    label: 'EXTERNAL EVIDENCE',
    title: '平台外的素材，以证据的身份收在这里',
    lead: '它们永远不进共享素材库、不能被拿去投放。收它们只有一个用处：解释本轮结果时有个参照。',
  },
}

export function AssetsPage({ state, view, onOpenView, onOpenLibrary, onOpenAnalysis }: {
  state: DataState
  view: AssetsView
  /** 切到另一个二级视图。传的是侧栏上的那个中文名，不是 AssetsView 的键。 */
  onOpenView: (view: string) => void
  onOpenLibrary: () => void
  onOpenAnalysis: () => void
}) {
  const { currentProject } = useProject()
  const [selectedId, setSelectedId] = useState('')
  const [assets, setAssets] = useState<ApiInsightAsset[]>([])
  // 登记表单开在右栏。它是这个模块唯一一处「凭空多出一条素材」的入口，三页合并
  // 之后不能跟着旧页面一起消失——没有它，一个新 Project 里后面每一页都是空的。
  const [indexing, setIndexing] = useState(false)
  // 从创意批量导入。跟登记表单共用右栏，但它们是两条不同的路：登记是「外面做的，
  // 手工补一条」，导入是「创意组已经批准的，一次全接过来」。
  const [importing, setImporting] = useState(false)
  // 登记成功后加一，让总览和这里的素材清单都重新取一次数。
  const [reloadKey, setReloadKey] = useState(0)

  // 窗口和分析页同一套算法：外部素材的留存期从窗口结束日算起，两屏各算一套的话，
  // 同一天导进来的两条素材会有两个到期日。
  const window = useMemo(() => {
    const end = new Date()
    const start = new Date(end.valueOf() - 30 * 24 * 60 * 60 * 1000)
    return { start: isoDate(start), end: isoDate(end) }
  }, [])

  useEffect(() => {
    if (!currentProject.id) return
    let active = true
    void api.listInsightAssets(currentProject.id, {})
      .then(page => { if (active) setAssets(page.items) })
      .catch(() => { if (active) setAssets([]) })
    return () => { active = false }
  }, [currentProject.id, reloadKey])

  // 换视图就把选中的素材放开：详情面板留在那儿，人会以为新视图说的还是这一条。
  // 登记表单同理：它只属于总览那一屏。
  useEffect(() => { setSelectedId(''); setIndexing(false); setImporting(false) }, [view])

  const heading = headings[view]
  const selected = assets.find(asset => asset.id === selectedId)

  // 数据接入和变量这两段是整页委托过去的，它们自带工具条和两栏布局。
  // 再套一层壳会得到两个标题、两条工具条，人分不清哪条是当前这一屏的。
  if (view === 'intake') return <IntakeView state={state}/>
  if (view === 'features') return <FeatureView state={state}/>

  return <StateBoundary state={state} onRetry={() => {}}>
    <div className="prelaunch-workspace">
      <section className="prelaunch-main">
        <div className="core-flow-toolbar">
          <div>
            <span className="section-label">{heading.label}</span>
            <h2>{heading.title}</h2>
            <p>当前 Project：{currentProject.name}。{heading.lead}</p>
          </div>
        </div>

        {view === 'overview' ? <OverviewView
          selectedId={selectedId}
          onSelect={setSelectedId}
          onOpenLibrary={onOpenLibrary}
          onOpenView={onOpenView}
          onOpenAnalysis={onOpenAnalysis}
          onIndex={() => { setSelectedId(''); setImporting(false); setIndexing(true) }}
          onImport={() => { setSelectedId(''); setIndexing(false); setImporting(true) }}
          reloadKey={reloadKey}/> : null}
        {view === 'similar' ? <SimilarView/> : null}
        {view === 'external' ? <ExternalView window={window}/> : null}
      </section>

      <aside className="prelaunch-detail">
        {importing
          ? <ImportFromCreative
            projectId={currentProject.id}
            existing={assets}
            onCancel={() => setImporting(false)}
            onRefresh={() => setReloadKey(key => key + 1)}
            onDone={() => {
              setImporting(false)
              // 导进来的是一批不是一条，没有「刚登记的那一条」可选中。
              // 让总览重新取一次数，人自己在清单里看这一批。
              setReloadKey(key => key + 1)
            }}/>
          : indexing
          ? <AssetIndexForm
            assets={assets}
            projectId={currentProject.id}
            busy={false}
            onCancel={() => setIndexing(false)}
            onDone={asset => {
              setIndexing(false)
              setReloadKey(key => key + 1)
              setSelectedId(asset.id)
            }}/>
          : selected
          ? <AssetDetail asset={selected} onOpenLibrary={onOpenLibrary}/>
          : <>
            <span className="section-label">这一页怎么读</span>
            <h3>{heading.title}</h3>
            <p>{heading.lead}</p>
            <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
              <small>这一页给不了什么</small>
              这里只回答「素材备齐了没有」。它跑得怎么样、为什么是这个结果，在「分析」那一页。
            </span></div>
            <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
              <small>素材文件不在这儿</small>
              平台内素材的文件躺在创意模块的素材库里，洞察一个字节都没存。要改标题、换封面、
              加一版，都得回那边做。只有外部证据是洞察自己存的，也只有它们有到期日。
            </span></div>
          </>}
      </aside>
    </div>
  </StateBoundary>
}
