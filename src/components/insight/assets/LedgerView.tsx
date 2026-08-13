import { useCallback, useEffect, useState } from 'react'
import { CircleAlert } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiAssetSourceKind, type ApiInsightAsset } from '../../../data/api'
import { isoDate } from '../analysis/format'

/**
 * 台账。平台里所有素材的账本——创意做的每一张图、每一版剪辑、每一段配音都在这儿，
 * 绝大多数永远不会拿去投流。
 *
 * 它和隔壁「总览」的区别不是筛选条件不同，是**回答的问题不同**：总览问「这几条素材
 * 还差什么才能进复盘」，台账问「我们手上到底有些什么」。所以这一屏没有队列、没有红点，
 * 只有一个搜索框和一条条记录——它不催人做事。
 *
 * 分页是游标不是页码：台账和平台素材库一样大，翻到第 50 页要数过前面 4900 行。
 */
const sourceLabels: Record<ApiAssetSourceKind, string> = {
  creative: '创意产出',
  upload: '手工上传',
  external: '外部导入',
  miyun: '米云采集',
}

export function ledgerSourceLabel(kind: ApiAssetSourceKind): string {
  return sourceLabels[kind] ?? '来源未知'
}

export function LedgerView({ onPromoted }: { onPromoted: () => void }) {
  const { currentProject } = useProject()
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<ApiInsightAsset[]>([])
  const [cursor, setCursor] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  // 每改一次搜索词就加一，让下面那个 effect 从头取一遍而不是接着上一页翻。
  const [searchKey, setSearchKey] = useState(0)

  const load = useCallback(async (nextCursor: string, append: boolean) => {
    if (!currentProject.id) return
    setLoading(true)
    setError('')
    try {
      const page = await api.listInsightAssets(currentProject.id, {
        roles: ['ledger'], query: query.trim() || undefined,
        cursor: nextCursor || undefined, limit: 50,
      })
      setItems(previous => append ? [...previous, ...page.items] : page.items)
      setCursor(page.next_cursor ?? '')
    } catch {
      setError('台账读不出来。刷新一次，还不行就是后端没起来。')
    } finally {
      setLoading(false)
    }
  }, [currentProject.id, query])

  // 只认项目和搜索键：query 每敲一个字都变，跟着它取数等于边打字边发请求。
  useEffect(() => { void load('', false) }, [currentProject.id, searchKey])

  const promote = async (asset: ApiInsightAsset) => {
    try {
      await api.promoteInsightAsset(currentProject.id, asset.id, {
        expected_version: asset.version, reason: '这条要投了，拉进分析。',
      })
      // 拉进分析之后它就不在台账里了，从当前这一页摘掉，不必重取。
      setItems(previous => previous.filter(item => item.id !== asset.id))
      onPromoted()
    } catch {
      setError('拉进分析没成功。多半是这条素材刚被别人改过，刷新后再试。')
    }
  }

  return <div className="assets-ledger">
    <div className="assets-ledger-search">
      <input
        type="search"
        value={query}
        placeholder="按标题搜，比如「春节」"
        onChange={event => setQuery(event.target.value)}
        onKeyDown={event => { if (event.key === 'Enter') setSearchKey(key => key + 1) }}/>
      <button type="button" onClick={() => setSearchKey(key => key + 1)}>搜索</button>
    </div>

    {error ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>{error}</span></div> : null}

    <ul className="assets-ledger-list">
      {items.map(asset => <li key={asset.id}>
        <div>
          <strong>{asset.title}</strong>
          <small>{ledgerSourceLabel(asset.source_kind)} · {isoDate(new Date(asset.created_at))}</small>
        </div>
        <button type="button" onClick={() => void promote(asset)}>拉进分析</button>
      </li>)}
    </ul>

    {items.length === 0 && !loading
      ? <div className="prelaunch-boundary"><CircleAlert size={16}/><span>
        <small>台账是空的</small>
        创意做出来的素材会自动记进这里。一条都没有，说明这个 Project 还没产出过素材，
        或者素材是在台账建起来之前入的库——那种要跑一次 cookies-maintain backfill-ledger 补。
      </span></div>
      : null}

    {cursor
      ? <button type="button" disabled={loading} onClick={() => void load(cursor, true)}>
        {loading ? '加载中…' : '加载更多'}
      </button>
      : null}
  </div>
}
