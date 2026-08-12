import { useCallback, useEffect, useMemo, useState } from 'react'
import { CircleAlert, Import } from 'lucide-react'
import {
  api,
  type ApiCreativeVersionSummary,
  type ApiInsightAsset,
} from '../../../data/api'
import { formatDate } from '../analysis/format'

/**
 * 「从创意导入」。把创意组已经批准的创意版本，登记成洞察这边的可分析素材。
 *
 * 为什么需要它：创意每批准一个版本都会往库里写一条 `creative.approved.v1`
 * 事件，但全仓没有任何一处消费它。结果是创意组做完、批准、投出去的素材，洞察
 * 这边一条都不知道；要分析得有人再手工登记一遍，而没人会干这事。于是洞察永远
 * 在分析一批只存在于自己表里的影子素材——分析得再对也跟真实业务无关。
 *
 * 这一屏是那根管子的手动版：不改创意和洞察任何既有的写入逻辑，只是把「已批准
 * 但还没进来」的版本列出来，让人一次勾完。自动消费事件是下一步，但得先让人
 * 看见通了之后是什么样。
 */
export function ImportFromCreative({ projectId, existing, onCancel, onRefresh, onDone }: {
  projectId: string
  /** 洞察这边已有的素材，用来把导过的排掉。 */
  existing: ApiInsightAsset[]
  onCancel: () => void
  /** 有东西导进去了，但这一屏还得留着——部分失败时用，人要能看见是哪几条没成。 */
  onRefresh: () => void
  /** 全部导成功，收工。 */
  onDone: (imported: number) => void
}) {
  const [versions, setVersions] = useState<ApiCreativeVersionSummary[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [error, setError] = useState('')
  const [working, setWorking] = useState(false)

  const load = useCallback(async () => {
    if (!projectId) return
    setListState('loading')
    try {
      const page = await api.listCreativeVersions(projectId)
      setVersions(page.items ?? [])
      setListState('ready')
    } catch (cause) {
      setVersions([])
      setListState('error')
      setError(cause instanceof Error ? cause.message : '读取创意版本失败。')
    }
  }, [projectId])

  useEffect(() => { void load() }, [load])

  // 已经导过的靠 source_ref 认——登记时把创意版本 ID 原样写进去了。
  const importedRefs = useMemo(() => new Set(
    existing.filter(asset => asset.source_kind === 'creative' && asset.source_ref)
      .map(asset => asset.source_ref as string),
  ), [existing])

  // 只导已批准的。created / checked 还可能再改，superseded 已经被后面的版本顶掉——
  // 拿它们当分析对象，会出现「分析完了原件又变了」这种没法回溯的情况。
  const candidates = useMemo(() => versions
    .filter(item => item.status === 'approved' && !importedRefs.has(item.id))
    .sort((left, right) => right.created_at.localeCompare(left.created_at)),
  [versions, importedRefs])

  // 默认全勾。人打开这一屏就是为了把落下的补进来，让他再逐条勾一遍是白费手。
  useEffect(() => { setPicked(new Set(candidates.map(item => item.id))) }, [candidates])

  const toggle = (id: string) => setPicked(current => {
    const next = new Set(current)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const submit = async () => {
    const selected = candidates.filter(item => picked.has(item.id))
      // 按版本号从小到大导：同一条创意的第 1 版先进去开出血缘，后面的版本才挂得上。
      .sort((left, right) => left.version - right.version)
    if (!selected.length) return
    setError('')
    setWorking(true)

    // 同一个创意任务下的多个版本要共用一条血缘，否则投后分析会把「同一条创意改了
    // 三版」当成「三条不同的创意」混着比。已经导过的那些也算数：从它们的 source_ref
    // 反查出创意任务，把血缘接上，而不是又开一条新的。
    const versionById = new Map(versions.map(item => [item.id, item]))
    const lineageByTask = new Map<string, string>()
    for (const asset of existing) {
      if (asset.source_kind !== 'creative' || !asset.source_ref) continue
      const task = versionById.get(asset.source_ref)?.creative_task_id
      if (task && !lineageByTask.has(task)) lineageByTask.set(task, asset.lineage_id)
    }

    let done = 0
    let failure = ''
    for (const item of selected) {
      const media = item.video_snapshot?.final_video
      try {
        const created = await api.indexInsightAsset(projectId, {
          title: titleOf(item),
          source_kind: 'creative',
          source_ref: item.id,
          lineage_id: lineageByTask.get(item.creative_task_id) || undefined,
          // 媒体资产引用要么都给要么都不给，后端会校验（assets.go 的 validate）。
          // 图文版本没有单一成品文件，所以这两个字段是空的。
          ...(media?.asset_id && media.version
            ? { platform_asset_id: media.asset_id, platform_asset_version: media.version }
            : {}),
          // 不猜内容类型。image_text 可能是小红书也可能是公众号，video 可能是品牌广告、
          // 数字人、前贴或爆款复刻——从 format 猜会猜错，而错的类型会拉起错的一整套
          // 特征体系。留空就是「待识别」，人在「变量」那一屏点两下就定了。
        })
        if (!lineageByTask.has(item.creative_task_id)) {
          lineageByTask.set(item.creative_task_id, created.lineage_id)
        }
        done += 1
      } catch (cause) {
        // 一条失败不该把剩下的全放弃。记下第一条错，继续导完，最后如实报数。
        if (!failure) failure = cause instanceof Error ? cause.message : '登记失败。'
      }
    }

    setWorking(false)
    if (failure) {
      // 关掉面板等于把这句话吞了，人只会看见数字对不上却不知道为什么。留在原地，
      // 让外面把清单刷新一遍——成功的那几条会自己从下面的候选里消失，剩下的就是没成的。
      setError(`${selected.length} 条里成功 ${done} 条，剩下的还在下面。第一条失败的原因：${failure}`)
      if (done) onRefresh()
      return
    }
    onDone(done)
  }

  return <>
    <span className="section-label">从创意导入</span>
    <p>
      创意组批准过的版本才会出现在这里。批准过的版本不可改，拿它当分析对象才不会出现
      「分析完了原件又变了」。导进来只是登记，文件仍然躺在素材库里。
    </p>

    {listState === 'loading' ? <p className="panel-empty">正在读创意版本…</p> : null}
    {listState === 'error' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>读不到创意版本</small>{error || '创意模块没有响应。'}
    </span></div> : null}

    {listState === 'ready' && !candidates.length ? <p className="panel-empty">
      {versions.some(item => item.status === 'approved')
        ? '创意里已批准的版本都已经导进来了。'
        : '这个 Project 的创意还没有批准过的版本。批准之后再回来导。'}
    </p> : null}

    {candidates.length ? <>
      <ul className="import-pick-list">
        {candidates.map(item => <li key={item.id}>
          <label>
            <input type="checkbox" checked={picked.has(item.id)} onChange={() => toggle(item.id)}/>
            <span>
              <b>{titleOf(item)}</b>
              <small>
                {item.format === 'video' ? '视频' : '图文'} · 第 {item.version} 版 ·
                {' '}批准于 {formatDate(item.approval?.approved_at || item.created_at)}
              </small>
            </span>
          </label>
        </li>)}
      </ul>
      <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
        <small>导进来之后还差一步</small>
        内容类型这里不猜——图文可能是小红书也可能是公众号，视频可能是品牌广告、数字人、
        前贴或爆款复刻，猜错会拉起错的一整套特征体系。导进来的素材都是「待识别」，
        去「变量」那一屏点两下就定了。
      </span></div>
    </> : null}

    {error && listState === 'ready' ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
      <small>没有全部导成功</small>{error}
    </span></div> : null}

    <div className="prelaunch-actions">
      <button className="primary-button full" disabled={working || !picked.size}
        onClick={() => { void submit() }}>
        <Import size={15}/>{working ? '导入中…' : `导入选中的 ${picked.size} 条`}
      </button>
      <button className="secondary-button full" disabled={working} onClick={onCancel}>取消</button>
    </div>
  </>
}

/**
 * 给导进来的素材起名。创意版本本身没有「标题」字段：图文的标题在草稿快照里，
 * 视频压根没有，只能退回到格式加版本号。名字不好看没关系，但必须唯一到能认人，
 * 否则素材清单里会出现一排一模一样的条目。
 */
function titleOf(item: ApiCreativeVersionSummary): string {
  const picked = item.snapshot?.selected_title?.trim()
    || item.snapshot?.title_candidates?.find(value => value.trim())?.trim()
  const fallback = `${item.format === 'video' ? '视频' : '图文'}创意 第 ${item.version} 版`
  return (picked ? `${picked}（第 ${item.version} 版）` : fallback).slice(0, 255)
}
