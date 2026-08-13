import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, ChevronRight, CircleAlert, FolderOpen, Import, Layers3, Plus, RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import {
  api,
  type ApiAnalysisRun,
  type ApiAnalysisStatus,
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
 * **这一屏只讲平台内素材。**外部证据以前并排占右边半屏，可这两拨东西的性质是相反的：
 * 一拨能拿去投、进归因，一拨永远不能投、只在解释结论时当参照读。并排摆着，人得先
 * 分清哪边归哪边，才想得起来右边那半屏不算进任何一个数——而它本来就有自己的
 * 标签页，并排的那份不过是同一批东西的第二个入口。现在正文分家，只在页脚留一条
 * 去处；外部证据出事的那一行仍然汇到顶上来（见 buildOverviewAlerts）。
 *
 * **这一屏默认是安静的。**要处理的事以前是四张常驻卡片，一年里有 360 天四个数
 * 全是 0，占掉下半屏——人扫惯了四个 0，真跳出一个 2 也照样滑过去。现在没事的时候
 * 一个字都不出现，有事才在顶上多一行，一件事一行。
 */

/** 清单上每条素材右边挂的那个词。 */
export type AssetTone = 'ready' | 'waiting' | 'bad'

const statusLabels: Record<ApiAnalysisStatus, { text: string; tone: AssetTone }> = {
  confirmed: { text: '已可解释', tone: 'ready' },
  pending_confirmation: { text: '已可解释', tone: 'ready' },
  awaiting_data: { text: '等投放数据', tone: 'waiting' },
  awaiting_match: { text: '等对上号', tone: 'waiting' },
  analysable: { text: '等提变量', tone: 'waiting' },
  analysing: { text: '正在提变量', tone: 'waiting' },
  needs_review: { text: '等人复审', tone: 'waiting' },
  retired: { text: '已停用', tone: 'waiting' },
}

/**
 * 每条素材右边那个词。
 *
 * 这个位置以前挂的是 8 位编号加版本号。那两样东西没人拿来做判断，却占掉了整整
 * 一行，于是六条素材撑满一屏，而真正要看的「这条到底能不能用」一个字都没有。
 *
 * 失败不在状态机里（跑挂了的素材原地停在「可分析」），所以要外面算好了传进来。
 */
export function assetStatusLabel(status: ApiAnalysisStatus, failed: boolean): { text: string; tone: AssetTone } {
  if (failed) return { text: '提取失败', tone: 'bad' }
  // 后端加了新状态而前端还没跟上时，宁可显示「状态未知」也不要空着一格——
  // 空着的话，看的人以为这条素材没事。
  return statusLabels[status] ?? { text: '状态未知', tone: 'waiting' }
}

/** 顶上那一行要说的一件事。target 是点下去落到哪一屏。 */
export type OverviewAlert = {
  key: string
  tone: 'urgent' | 'warn'
  text: string
  action: string
  target: '数据接入' | '变量' | '外部素材' | 'import'
}

/**
 * 「有事才出声」：把要人动手的事按非零筛一遍，一件也没有就返回空数组，
 * 那一整块连边框带标题都不渲染。
 *
 * 顺序就是优先级，对不上号永远第一——它是唯一一个「不处理后面全错」的问题
 * （花费算不到任何素材头上），其余几条只是少几条样本或者少个参照。
 *
 * **外部证据的两件事也走这条路。**总览这一屏归平台内素材，外部证据有自己的
 * 标签页；但「原片快到期」和「只有标题」是有时限、会白白错过的事，只挂在那一屏上，
 * 人不点进去就永远看不到。所以正文分家，出事的那一行仍然汇到这里来。
 */
export function buildOverviewAlerts(counts: {
  unmatched: number
  failures: number
  needsReview: number
  notImported: number
  /** 原片十四天内到期的外部证据。expiringDate 是其中最早的那个日子。 */
  expiring?: number
  expiringDate?: string
  /** 既没有文件也没标变量的外部证据。 */
  emptyEvidence?: number
}): OverviewAlert[] {
  return ([
    {
      key: 'unmatched', count: counts.unmatched, tone: 'urgent' as const,
      text: `${counts.unmatched} 条回流数据对不上号，它们的花费算不到任何素材头上`,
      action: '去认领', target: '数据接入' as const,
    },
    {
      key: 'failed', count: counts.failures, tone: 'warn' as const,
      text: `${counts.failures} 条提取失败，现在一个变量都没有，它不会自己重试`,
      action: '去重跑', target: '变量' as const,
    },
    {
      key: 'review', count: counts.needsReview, tone: 'warn' as const,
      text: `${counts.needsReview} 条等人复审，放着不管会带着一份没人认可的变量进分析`,
      action: '去看看', target: '变量' as const,
    },
    {
      key: 'not-imported', count: counts.notImported, tone: 'warn' as const,
      text: `创意里还有 ${counts.notImported} 条批准了没进来，它们的投放数据回流时认不到人头上`,
      action: '去导入', target: 'import' as const,
    },
    {
      key: 'expiring', count: counts.expiring ?? 0, tone: 'warn' as const,
      text: `${counts.expiring} 条外部证据的原片${counts.expiringDate ? ` ${counts.expiringDate} 前后` : '就快'}被清掉，只留下人标过的变量`,
      action: '去看看', target: '外部素材' as const,
    },
    {
      key: 'empty-evidence', count: counts.emptyEvidence ?? 0, tone: 'warn' as const,
      text: `${counts.emptyEvidence} 条外部证据既没有文件也没标变量，引用它等于引用一个名字`,
      action: '去补变量', target: '外部素材' as const,
    },
  ]).flatMap(({ count, ...alert }) => count ? [alert] : [])
}

/**
 * 横幅那一行。
 *
 * 以前是一句话套一个括号套一串小字，三个数字散在里面。改成一串顿开的短句：
 * 第一个数是总数，后面每一项都是它的一部分，加起来正好回到总数——不守恒的话，
 * 人会拿它和左栏那个数去对，对不上就以为哪里算错了。
 */
export function overviewTally(total: number, explainable: number, rest: string[]): string {
  return [`${total} 条素材`, `${explainable} 条已能解释`, ...rest].join(' · ')
}
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
   * 清单上标红的只能是**还挡着路**的那些。
   *
   * 照 failedRuns 标的话，一条跑挂之后被人手工补上变量的素材会同时是
   * 「已可解释」和「提取失败」——横幅把它算进能解释的 5 条，清单上却给它标红，
   * 同一屏两句话互相打脸，人只能挨个点开去对。
   */
  const blockingIds = useMemo(() =>
    new Set(blockingFailures.map(run => run.asset_id)), [blockingFailures])

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
    { count: awaitingExtraction.length, note: '等提变量' },
    { count: failedExtraction.length, note: '提取失败' },
    { count: live.filter(asset => asset.analysis_status === 'awaiting_data').length, note: '等投放数据' },
    { count: live.filter(asset => asset.analysis_status === 'awaiting_match').length, note: '等对上号' },
    { count: live.filter(asset => asset.analysis_status === 'analysing').length, note: '正在提变量' },
    { count: needsReview.length, note: '等人复审' },
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

  const alerts = useMemo(() => buildOverviewAlerts({
    unmatched: unmatched.length,
    failures: blockingFailures.length,
    needsReview: needsReview.length,
    notImported: notImported.length,
    expiring: expiring.length,
    expiringDate: expiring.length ? formatDate(expiring[0].retention_until) : '',
    emptyEvidence: emptyEvidence.length,
  }), [unmatched, blockingFailures, needsReview, notImported, expiring, emptyEvidence])

  if (listState === 'loading') return <div className="panel-empty">正在读取…</div>
  if (listState === 'error') {
    return <div className="panel-empty">读取失败。<button type="button" className="secondary-button"
      onClick={() => { void load() }}><RefreshCw size={15}/>重试</button></div>
  }

  return <section className="assets-overview">
    {/* 要人动手的事全在这儿，一件一行。四件都没有时这一块整个不存在——
        这一屏在正常日子里应该是安静的，跳出一行就意味着真出了事。 */}
    {alerts.length ? <div className="assets-alerts">
      {alerts.map(alert => <div key={alert.key}
        className={alert.tone === 'urgent' ? 'assets-alert urgent' : 'assets-alert'}>
        <CircleAlert size={15}/>
        <span>{alert.text}</span>
        <button type="button" className="secondary-button"
          onClick={() => { if (alert.target === 'import') onImport(); else onOpenView(alert.target) }}>
          {alert.action}
        </button>
      </div>)}
    </div> : null}

    {/* 失败的原因跟在告警后面。只在真有挡路的失败时出现——原因这种东西
        平时摆出来没人读，出事时又是第一个要看的。 */}
    {blockingFailures.length ? <div className="assets-alert-detail">
      {blockingFailures.slice(0, 3).map(run => <small key={run.id}>
        {/* 库里存的是整条错误链，前半截是 Go 的英文 sentinel。原样摆出来的话，
            人先读到一串读不懂的英文，真正的原因（常常是「还没配模型」这种自己
            就能处理的事）躲在后面，一眼看去只像「系统崩了」。 */}
        {assetTitle(run.asset_id)}：{readableError(run.error_message) || run.error_code || '没有留下原因'}
      </small>)}
      {/* 一屏之内三条失败都是同一个原因是常态（模型没配，谁跑谁挂）。
          指路只说一次，挂在末尾，不跟着每一条重复。 */}
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
    </div> : null}

    {/* 这一行是全屏的落点，所以口径要能一眼数清：第一个数是总数，后面每一项
        都是它的一部分。不守恒的话，人会拿它和下面两栏去对，对不上就以为算错了。 */}
    <div className="assets-converge">
      <Layers3 size={17}/>
      <span>{overviewTally(live.length, explainable.length, unaccounted)}</span>
      <button type="button" className="secondary-button" onClick={onOpenAnalysis}>进分析</button>
    </div>

    {/* **这一屏只讲平台内素材。**外部证据以前并排占右边半屏，可这两拨东西的性质
        是相反的：一拨能拿去投、进归因，一拨永远不能投、只当参照读。并排摆着，人先要
        分清哪边归哪边，才想得起来右边那半屏不算进结论——而它本来就有自己的标签页，
        并排的那份只是同一批东西的第二个入口。正文分家，出事的那一行照样汇到上面去。 */}
    <div className="assets-panel">
      <div className="assets-column">
        <div className="assets-column-head">
          <span className="section-label">平台内素材 {live.length}</span>
          {/* 顶上这个跳转不是方便，是所有权的声明：这些东西不归洞察管，
              要改标题、换封面、加一版，都得回素材库去做。 */}
          <button type="button" className="link-button" onClick={onOpenLibrary}>
            <ArrowLeft size={13}/>创意模块 · 素材库
          </button>
          {/* 登记素材是唯一一处「凭空多出一条素材」的入口。它登记的是平台内素材；
              外部证据走「外部素材」那一屏，两条路收下的东西不一样。 */}
          <button type="button" className="link-button" onClick={onIndex}>
            <Plus size={13}/>登记素材
          </button>
          {/* 「从创意导入」跟旁边两个按钮回答的是同一个问题——素材是怎么进来的。
              三条路：从素材库看（跳出去）、创意批准的批量导（这个）、外面做的手工登记。
              放在别处人会以为它是另一件事。 */}
          <button type="button" className="link-button" onClick={onImport}>
            <Import size={13}/>从创意导入
          </button>
        </div>
        {/* 每条右边挂状态而不是编号。八条素材列完，人一眼能数出哪几条是好的、
            哪几条在等——这本来就是打开这一屏唯一想知道的事。 */}
        {live.length ? <ul className="assets-mini-list">
          {live.slice(0, 8).map(asset => {
            const status = assetStatusLabel(asset.analysis_status, blockingIds.has(asset.id))
            return <li key={asset.id}>
              <button type="button" className={selectedId === asset.id ? 'active' : ''}
                onClick={() => onSelect(asset.id)}>
                <b>{asset.title}</b>
                <em className={`assets-mini-tag tone-${status.tone}`}>{status.text}</em>
              </button>
            </li>
          })}
        </ul> : <p className="panel-empty">还没有可分析素材。</p>}
        {/* 只列八条是为了这一屏不被一份清单撑满。剩下的必须说一声，
            不然人会以为库里就这么几条。 */}
        {live.length > 8
          ? <p className="assets-column-lead">还有 {live.length - 8} 条没列出来，在「分析」那一屏能看全。</p>
          : null}
      </div>
    </div>

    {/* 外部证据在这一屏只剩一条去处。数目还是要报的——不报的话，人会以为这个
        Project 压根没收过外部参照；但它绝不能再摆成一份清单，那样又回到了
        「两拨东西并排，得先分清哪边归哪边」。 */}
    <button type="button" className="assets-crosslink" onClick={() => onOpenView('外部素材')}>
      <FolderOpen size={14}/>
      <span>外部证据 {external.length} 条。它们不能投放、不参与归因，只在解释结论时当参照读。</span>
      <ChevronRight size={14}/>
    </button>
  </section>
}
