import type { DataState } from '../../../types'
import { StateBoundary } from '../../StateBoundary'
import { ConclusionsView } from './ConclusionsView'
import { PatternsView } from './PatternsView'

/**
 * 「投前」入口。
 *
 * 这一页在 2026-08-04 那版重构里被并进了「经验 → 查经验」，理由是「同一批数据的
 * 两种读法」。数据上这话没错，但它在导航上抹掉了一件事：**下一轮该做什么**。
 * 人只看见「经验」，不会意识到那是开工前该来的地方——于是这一页在整个模块里
 * 没有位置，而后端 `/prelaunch` 一直活着、一直没人调。
 *
 * 拎回来之后，它和「经验 → 查经验」的分工是清楚的：
 *   投前 —— 这次该怎么做。只给 ✅ 能归因的，带数据体检闸门和跨渠道警告。
 *   查经验 —— 完整目录。六个维度筛，可以打开看 👁 只是观察的。
 * 前者是决定，后者是翻账。所以前者敢少给，后者不敢。
 */
export type PreLaunchView = 'conclusions' | 'patterns'

export function PreLaunchPage({ state, view }: {
  state: DataState
  view: PreLaunchView
}) {
  // 两个视图各自取数、各自管空态，壳只负责挑一个——壳再包一层标题的话，
  // 每一屏会顶着两条工具条，人分不清哪条说的是当前这一屏。
  return <StateBoundary state={state} onRetry={() => {}}>
    {view === 'patterns' ? <PatternsView/> : <ConclusionsView/>}
  </StateBoundary>
}
