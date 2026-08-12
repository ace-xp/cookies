import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, CircleAlert, FolderOpen, Import, Layers3, Plus, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiCreativeVersionSummary,
  type ApiExternalAsset,
  type ApiInsightAsset,
  type ApiInsightAssetMapping,
} from '../../../data/api'
import { shortId } from '../../../data/shortId'
import { formatDate } from '../analysis/format'

/**
 * 「总览」视图。
 *
 * **左右两栏不只是分类，是两种所有权。**左栏的素材躺在素材库里，洞察一个字节都
 * 没存，所以左栏顶上是一个跳回素材库的口子；右栏的文件是洞察自己存的，所以只有
 * 右栏有「原片到期」这一行——到期提示必须放在这里，为的是让人在原片被清掉之前
 * 还有机会做完想做的分析。
 *
 * 下面三个队列是「还差什么才能进复盘」。**对不上号的红色置顶**：它是唯一一个
 * 「不处理后面全错」的问题（花费算不到任何素材头上），其余两个只是少几条样本。
 */
export function OverviewView({ selectedId, onSelect, onOpenLibrary, onOpenView, onOpenAnalysis, onIndex, onImport, reloadKey }: {
  selectedId: string
  onSelect: (assetId: string) => void
  onOpenLibrary: () => void
  onOpenView: (view: string) => void
  onOpenAnalysis: () => void
  onIndex: () => void
  onImport: () => void
  /** 登记完一条素材之后由壳递增，用来把这一屏重新取一次数。 */
  reloadKey: number
}) {
  const { currentProject } = useProject()
  const [assets, setAssets] = useState<ApiInsightAsset[]>([])
  const [unmatched, setUnmatched] = useState<ApiInsightAssetMapping[]>([])
  const [external, setExternal] = useState<ApiExternalAsset[]>([])
  const [creativeVersions, setCreativeVersions] = useState<ApiCreativeVersionSummary[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const [assetPage, mappingPage, externalPage, creativePage] = await Promise.all([
        api.listInsightAssets(currentProject.id, {}),
        api.listInsightAssetMappings(currentProject.id, 'unmatched'),
        api.listExternalAssets(currentProject.id),
        // 创意是另一个系统，它没配好或者报错都不该让这一屏整个读不出来——
        // 读不到就当没有待导入的，其余三块照常显示。
        api.listCreativeVersions(currentProject.id).catch(() => ({ items: [] })),
      ])
      setAssets(assetPage.items)
      setUnmatched(mappingPage.items)
      setExternal(externalPage.items)
      setCreativeVersions(creativePage.items ?? [])
      setListState('ready')
    } catch {
      setAssets([])
      setUnmatched([])
      setExternal([])
      setCreativeVersions([])
      setListState('error')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load, reloadKey])

  const live = useMemo(() => assets.filter(asset => asset.analysis_status !== 'retired'), [assets])
  // 可分析 = 齐了三样里的前两样，就等投放数据。这个数是这一屏的落点：
  // 上面两栏说的是「有什么」，这一行说的是「其中能真的拿去解释的有几条」。
  const analysable = useMemo(() => live.filter(asset =>
    asset.analysis_status === 'analysable' || asset.analysis_status === 'confirmed'
    || asset.analysis_status === 'pending_confirmation'), [live])
  const awaitingExtraction = useMemo(() =>
    live.filter(asset => asset.analysis_status === 'analysable'), [live])
  const failed = useMemo(() =>
    live.filter(asset => asset.analysis_status === 'needs_review'), [live])

  // 原片还剩几天到期。到期日过了但还没被清掉的也算进来——清理是个定时任务，
  // 它跑之前这条素材已经不该再被当成随时可看的东西了。
  const expiring = useMemo(() => {
    const soon = new Date()
    soon.setDate(soon.getDate() + 14)
    return external.filter(item => !item.original_purged && new Date(item.retention_until) <= soon)
  }, [external])
  const purged = external.filter(item => item.original_purged)

  // 创意批准了、洞察这边还没有的。这个数字是这一屏最上游的缺口：另外三个队列说的
  // 是「进来了但还差点什么」，这一条说的是「压根还没进来」——包括那些「对不上号」，
  // 有一部分正是因为素材根本没登记，平台回流的对象才找不到东西可认。
  const notImported = useMemo(() => {
    const imported = new Set(assets
      .filter(asset => asset.source_kind === 'creative' && asset.source_ref)
      .map(asset => asset.source_ref))
    return creativeVersions.filter(item => item.status === 'approved' && !imported.has(item.id))
  }, [assets, creativeVersions])

  if (listState === 'loading') return <div className="panel-empty">正在读取…</div>
  if (listState === 'error') {
    return <div className="panel-empty">读取失败。<button type="button" className="secondary-button"
      onClick={() => { void load() }}><RefreshCw size={15}/>重试</button></div>
  }

  return <section className="assets-overview">
    <div className="assets-columns">
      <div className="assets-column">
        <div className="assets-column-head">
          <span className="section-label">平台内素材</span>
          {/* 左栏顶上这个跳转不是方便，是所有权的声明：这些东西不归洞察管，
              要改标题、换封面、加一版，都得回素材库去做。 */}
          <button type="button" className="link-button" onClick={onOpenLibrary}>
            <ArrowLeft size={13}/>创意模块 · 素材库
          </button>
          {/* 登记素材是唯一一处「凭空多出一条素材」的入口。放在左栏，因为它登记的
              是平台内素材；外部证据走右栏那条路，两条路收下的东西不一样。 */}
          <button type="button" className="link-button" onClick={onIndex}>
            <Plus size={13}/>登记素材
          </button>
          {/* 「从创意导入」跟旁边两个按钮回答的是同一个问题——素材是怎么进这一栏的。
              三条路：从素材库看（跳出去）、创意批准的批量导（这个）、外面做的手工登记。
              放在别处人会以为它是另一件事。 */}
          <button type="button" className="link-button" onClick={onImport}>
            <Import size={13}/>从创意导入
          </button>
        </div>
        <p className="assets-column-lead">
          {live.length} 条。文件躺在素材库里，洞察这边只记住它是哪一条、变量是什么、跑得怎么样。
        </p>
        {/* 只在真有落下的时候才出现。常态是 0，天天挂一行「还有 0 条」等于噪音。 */}
        {notImported.length ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
          <small>创意里还有 {notImported.length} 条没进来</small>
          创意组批准了但洞察这边没登记。它们的投放数据回流时认不到任何素材头上，
          这一轮的分析等于漏掉了这几条。
          <button type="button" className="link-button" onClick={onImport}>去导入</button>
        </span></div> : null}
        {live.length ? <ul className="assets-mini-list">
          {live.slice(0, 8).map(asset => <li key={asset.id}>
            <button type="button" className={selectedId === asset.id ? 'active' : ''}
              onClick={() => onSelect(asset.id)}>
              <b>{asset.title}</b><small>{shortId(asset.id)} · 第 {asset.revision} 版</small>
            </button>
          </li>)}
        </ul> : <p className="panel-empty">还没有可分析素材。</p>}
      </div>

      <div className="assets-column">
        <div className="assets-column-head">
          <span className="section-label">外部证据</span>
          <button type="button" className="link-button" onClick={() => onOpenView('外部素材')}>
            <FolderOpen size={13}/>去收一条
          </button>
        </div>
        <p className="assets-column-lead">
          {external.length} 条。这些文件是洞察自己存的，所以它们有到期日——也只有它们有。
        </p>
        {/* 到期提示只出现在右栏。放到左栏或者页顶，人会以为平台内素材也会被清掉。 */}
        {expiring.length ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
          <small>原片快到期了</small>
          {expiring.length} 条外部素材的原片将在 {formatDate(expiring[0].retention_until)} 前后清掉，
          只留下人标过的变量。要看原片才能做的分析，赶在那之前做完。
        </span></div> : null}
        {purged.length ? <p className="assets-column-lead">
          另有 {purged.length} 条原片已删，只剩标注的变量。引用过它们的复盘仍然说得清当时看到的是什么。
        </p> : null}
        {external.length ? <ul className="assets-mini-list">
          {external.slice(0, 8).map(item => <li key={item.id}>
            <span><b>{item.title}</b><small>留到 {formatDate(item.retention_until)}</small></span>
          </li>)}
        </ul> : <p className="panel-empty">还没有外部证据。</p>}
      </div>
    </div>

    <div className="assets-converge">
      <Layers3 size={17}/>
      <span>可分析素材 <b>{analysable.length}</b> 条</span>
      <button type="button" className="secondary-button" onClick={onOpenAnalysis}>进分析</button>
    </div>

    <div className="assets-queues">
      {/* 顺序就是优先级，红色那条永远第一。它和另外两条不是一个量级的问题：
          对不上号意味着这条花费算不到任何素材头上，后面每一个数字都是错的。 */}
      <div className="assets-queue urgent">
        <div className="assets-queue-head">
          <strong>对不上号 {unmatched.length}</strong>
          <button type="button" className="secondary-button" onClick={() => onOpenView('数据接入')}>
            去认领
          </button>
        </div>
        <p>平台上回流的对象认不出对应哪一版素材。不处理的话，它们的花费算不到任何素材头上
          ——后面每一条结论都建立在一份不完整的账上。</p>
        {unmatched.slice(0, 3).map(mapping => <small key={mapping.id}>
          {mapping.platform} · {mapping.platform_object_name || mapping.platform_object_id}
        </small>)}
      </div>

      <div className="assets-queue">
        <div className="assets-queue-head">
          <strong>待提取变量 {awaitingExtraction.length}</strong>
          <button type="button" className="secondary-button" onClick={() => onOpenView('变量')}>
            去提取
          </button>
        </div>
        <p>类型已识别、还没提变量。它们不会出现在素材对比和驱动因素里——不是表现不好，
          是没法判断它们和别人差在哪。</p>
      </div>

      <div className="assets-queue">
        <div className="assets-queue-head">
          <strong>提取出了问题 {failed.length}</strong>
          <button type="button" className="secondary-button" onClick={() => onOpenView('变量')}>
            去看看
          </button>
        </div>
        <p>提取跑过但结果需要人复审。放着不管，它们带着一份没人确认过的变量进分析。</p>
      </div>
    </div>
  </section>
}
