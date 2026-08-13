import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, CircleAlert, FolderOpen, Import, Layers3, Plus, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiAnalysisRun,
  type ApiCreativeVersionSummary,
  type ApiExternalAsset,
  type ApiInsightAsset,
  type ApiInsightAssetMapping,
} from '../../../data/api'
import { isModelSetupFailure, modelSetupHint, readableError } from '../../../data/readableError'
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
 * 下面四个队列是「还差什么才能进复盘」。**对不上号的红色置顶**：它是唯一一个
 * 「不处理后面全错」的问题（花费算不到任何素材头上），其余三个只是少几条样本。
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
  const [runs, setRuns] = useState<ApiAnalysisRun[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const [assetPage, mappingPage, externalPage, creativePage, runPage] = await Promise.all([
        // 显式写 analysis：这里数出来的是四个队列和红点，绝不能混进台账那几千条。
        api.listInsightAssets(currentProject.id, { roles: ['analysis'] }),
        api.listInsightAssetMappings(currentProject.id, 'unmatched'),
        api.listExternalAssets(currentProject.id),
        // 创意是另一个系统，它没配好或者报错都不该让这一屏整个读不出来——
        // 读不到就当没有待导入的，其余三块照常显示。
        api.listCreativeVersions(currentProject.id).catch(() => ({ items: [] })),
        // 分析留痕同理：读不到就当没跑过，不因此让整屏空掉。
        api.listInsightAnalysisRuns(currentProject.id).catch(() => ({ items: [] })),
      ])
      setAssets(assetPage.items)
      setUnmatched(mappingPage.items)
      setExternal(externalPage.items)
      setCreativeVersions(creativePage.items ?? [])
      setRuns(runPage.items ?? [])
      setListState('ready')
    } catch {
      setAssets([])
      setUnmatched([])
      setExternal([])
      setCreativeVersions([])
      setRuns([])
      setListState('error')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load, reloadKey])

  const live = useMemo(() => assets.filter(asset => asset.analysis_status !== 'retired'), [assets])
  // 「已经能拿来解释」= 变量已经提出来了（待确认或已确认）。
  //
  // **不要把「可分析」也算进来**：那个状态的意思是「类型认好了，可以开始提变量了」，
  // 一条变量都还没有。以前两个数都含它，于是同一条素材既被算进「可分析素材」又被
  // 算进「待提取变量」，两个数加起来比库里的素材还多，看的人只能怀疑其中一个是错的。
  const explainable = useMemo(() => live.filter(asset =>
    asset.analysis_status === 'confirmed' || asset.analysis_status === 'pending_confirmation'), [live])
  // 待复审：提取跑完了，但结果被打回来要人再看一眼。它不是失败。
  const needsReview = useMemo(() =>
    live.filter(asset => asset.analysis_status === 'needs_review'), [live])

  // 提取失败的素材。
  //
  // **失败不在素材状态里**（PRD §11.1 的状态机没有失败分支，跑挂了的素材原地停在
  // 「可分析」），所以只能从分析留痕反推：按素材取最新那一次运行，它是 failed 就说明
  // 这条现在还是坏的。取最新那一条而不是「有没有失败过」——重跑成功之后不该还挂着红字。
  const failedRuns = useMemo(() => {
    const liveIds = new Set(live.map(asset => asset.id))
    const latest = new Map<string, ApiAnalysisRun>()
    for (const run of runs) {
      // 接口按 started_at 倒序返回，所以第一次见到某条素材的那一行就是最新的。
      if (!latest.has(run.asset_id)) latest.set(run.asset_id, run)
    }
    return [...latest.values()].filter(run => run.status === 'failed' && liveIds.has(run.asset_id))
  }, [runs, live])

  /**
   * 「还没提变量」要把跑挂了的那些摘出去。
   *
   * 跑挂的素材原地停在「可分析」，于是同一条素材同时被数进「待提取变量」和
   * 「提取失败」两条队列——两个 1 摆在一起，看的人以为是两条素材，去点「去提取」
   * 又只找到一条。摘出去之后，两条队列各说各的，加起来正好是可分析的总数。
   */
  const failedIds = useMemo(() => new Set(failedRuns.map(run => run.asset_id)), [failedRuns])
  const awaitingExtraction = useMemo(() => live.filter(asset =>
    asset.analysis_status === 'analysable' && !failedIds.has(asset.id)), [live, failedIds])
  const failedExtraction = useMemo(() => live.filter(asset =>
    asset.analysis_status === 'analysable' && failedIds.has(asset.id)), [live, failedIds])

  /**
   * 队列里要摆的失败，只是**还挡着路**的那些。
   *
   * 跑挂之后有人手工把变量填上了，这条素材就已经能进分析了——它的状态离开了
   * 「可分析」，可最新一次运行仍然是 failed。照 failedRuns 数的话，队列上会一直挂着
   * 一条红字，写着「这条素材现在一个变量都没有」，而那条素材同时又被算进上面
   * 「已经能拿来解释的」——同一屏里两句话互相打脸，人只能挨个点开去对。
   *
   * 已经不挡路的那些不删掉、只降级成一行小字：重跑失败本身仍然值得知道，
   * 只是它不再是「今天必须处理」的事。
   */
  const blockingFailures = useMemo(() => {
    const ids = new Set(failedExtraction.map(asset => asset.id))
    return failedRuns.filter(run => ids.has(run.asset_id))
  }, [failedRuns, failedExtraction])
  const staleFailures = failedRuns.length - blockingFailures.length

  /**
   * 「没进这个数的都在哪儿」——按状态逐类点名。
   *
   * 下面那行小字原来只提「还没提变量」这一类，可素材状态有八种，除去停用和已提出
   * 变量的两类，还剩「等数据 / 等对上号 / 正在提 / 等人复审」四种。库里真有一条
   * 停在「待数据」时，左栏写着 6 条、这行只交代得了 5 条，剩下那一条在整屏上一个
   * 字都没有——人只能怀疑某个数算错了。这里把活着的素材全铺开，任何时候
   * 「能解释的 + 这一串」都等于左栏那个总数。
   */
  const unaccounted = useMemo(() => ([
    { count: awaitingExtraction.length, note: '只登记了还没提变量' },
    { count: failedExtraction.length, note: '提变量跑挂了' },
    { count: live.filter(asset => asset.analysis_status === 'awaiting_data').length, note: '在等投放数据回流' },
    { count: live.filter(asset => asset.analysis_status === 'awaiting_match').length, note: '还没和平台上的对象对上号' },
    { count: live.filter(asset => asset.analysis_status === 'analysing').length, note: '正在提变量' },
    { count: needsReview.length, note: '提完了在等人复审' },
  ]).flatMap(bucket => bucket.count ? [`${bucket.count} 条${bucket.note}`] : []),
  [live, awaitingExtraction, failedExtraction, needsReview])

  const assetTitle = useCallback((assetId: string) =>
    live.find(asset => asset.id === assetId)?.title ?? shortId(assetId), [live])

  // 原片还剩几天到期。到期日过了但还没被清掉的也算进来——清理是个定时任务，
  // 它跑之前这条素材已经不该再被当成随时可看的东西了。
  //
  // **只算真有文件的那些**（storage_key 非空）。外部证据大多数只是一条登记：标题、
  // 来源、人标的变量，没有任何文件。对这些说「原片将在 X 前后清掉、赶在那之前看完」，
  // 是在催人去做一件做不到的事——那个原片从来不在系统里。
  const withFile = useMemo(() => external.filter(item => item.storage_key), [external])
  const expiring = useMemo(() => {
    const soon = new Date()
    soon.setDate(soon.getDate() + 14)
    return withFile.filter(item => !item.original_purged && new Date(item.retention_until) <= soon)
  }, [withFile])
  const purged = withFile.filter(item => item.original_purged)
  // 一条变量都没标、又没有文件的登记：它对本轮结论起不了任何作用。
  const emptyEvidence = useMemo(() =>
    external.filter(item => !item.storage_key && !Object.keys(item.features ?? {}).length), [external])

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
          {external.length} 条。它们不进共享素材库、不参与归因，只在解释结论时当参照读。
          {withFile.length ? `其中 ${withFile.length} 条存了原片，有到期日。` : '这里存的是标注，不是文件。'}
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
        {/* 空登记要说出来。它看起来和别的证据一样占一行，实际上什么也证明不了。 */}
        {emptyEvidence.length ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
          <small>{emptyEvidence.length} 条只有标题</small>
          既没有文件也没标变量，引用它等于引用一个名字。去「外部素材」把看到的东西补上。
          <button type="button" className="link-button"
            onClick={() => onOpenView('外部素材')}>去补变量</button>
        </span></div> : null}
        {external.length ? <ul className="assets-mini-list">
          {external.slice(0, 8).map(item => <li key={item.id}>
            <span><b>{item.title}</b><small>{externalNote(item)}</small></span>
          </li>)}
        </ul> : <p className="panel-empty">还没有外部证据。</p>}
      </div>
    </div>

    {/* 这一行是全屏的落点，所以口径要能被一句话说清楚：**只数已经提出变量的那些**。
        下面那行小字把「不在这个数里的」全列出来——不列的话，人会拿它和上面两栏的
        总数去对，对不上就会以为哪里漏了。 */}
    <div className="assets-converge">
      <Layers3 size={17}/>
      <span>已经能拿来解释的 <b>{explainable.length}</b> 条
        {/* 小字接在同一行里排。不给分隔符的话，读到的是「4 条另有 1 条」——
            两句话糊成一句，第一眼看不出在哪儿断开。 */}
        <small> · {unaccounted.length ? `另有 ${unaccounted.join('、')}。` : ''}
          {external.length
            ? `${external.length} 条外部证据不进这个数（它们没有投放数据，只能当旁证读）。`
            : ''}</small>
      </span>
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

      {/* 「提取失败」和「等人复审」以前是同一条队列，标题写着失败、数的却是待复审，
          于是真正跑挂的那些一条都没被数进去——素材停在「可分析」，界面上看不出
          任何异常，人以为只是还没轮到它。现在分成两条，各数各的。 */}
      <div className="assets-queue">
        <div className="assets-queue-head">
          <strong>提取失败 {blockingFailures.length}</strong>
          <button type="button" className="secondary-button" onClick={() => onOpenView('变量')}>
            去重跑
          </button>
        </div>
        <p>提取跑挂了，这条素材现在一个变量都没有。它不会自己重试——不重跑的话，
          它在后面所有对比里都是缺席的。</p>
        {blockingFailures.slice(0, 3).map(run => <small key={run.id}>
          {/* 库里存的是整条错误链，前半截是 Go 的英文 sentinel。原样摆出来的话，
              人先读到一串读不懂的英文，真正的原因（常常是「还没配模型」这种自己
              就能处理的事）躲在后面，一眼看去只像「系统崩了」。 */}
          {assetTitle(run.asset_id)}：{readableError(run.error_message) || run.error_code || '没有留下原因'}
        </small>)}
        {/* 一屏之内三条失败都是同一个原因是常态（模型没配，谁跑谁挂）。
            指路只说一次，挂在队列末尾，不跟着每一条重复。 */}
        {blockingFailures.some(run => isModelSetupFailure(run.error_message))
          ? <small>{modelSetupHint}</small>
          : null}
        {blockingFailures.length > 3
          ? <small>还有 {blockingFailures.length - 3} 条，去「变量」那一屏逐条看。</small>
          : null}
        {/* 已经有变量、不挡路的那些失败降到这一行。不提的话，「变量」那一屏上还挂着
            红色留痕，而总览这边一个字都没有，人会以为两屏对不上。 */}
        {staleFailures ? <small>
          另有 {staleFailures} 条上一次跑挂了，但变量已经被人补上，不挡分析。
        </small> : null}
      </div>

      <div className="assets-queue">
        <div className="assets-queue-head">
          <strong>等人复审 {needsReview.length}</strong>
          <button type="button" className="secondary-button" onClick={() => onOpenView('变量')}>
            去看看
          </button>
        </div>
        <p>提取跑完了，但有人把结果打回来要求再看一眼。放着不管，它们带着一份
          没人认可的变量进分析。</p>
      </div>
    </div>
  </section>
}

// 右栏每条外部证据后面那句小字。以前一律写「留到 X」——可留存期管的只是原件，
// 一条没有文件的登记根本没有东西会被清掉，那个日期对它毫无意义。
function externalNote(item: ApiExternalAsset): string {
  const marked = Object.keys(item.features ?? {}).length
  if (item.storage_key) {
    return item.original_purged ? '原件已删' : `原件留到 ${formatDate(item.retention_until)}`
  }
  return marked ? `标了 ${marked} 条变量` : '只有标题'
}
