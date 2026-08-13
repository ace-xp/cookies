import type { ApiSimilarAssetResult } from '../../../data/api'
import { featureSourceLabel } from '../../../data/featureSource'
import { shortId } from '../../../data/shortId'

/**
 * 相似素材结果。每条都列出「像在哪」——这是这个功能和一个相似度分数的全部区别。
 *
 * 单独一个文件，是因为它有两个调用方：「素材 · 找相似」这一屏，和分析页上
 * ❓「算不出来」那个占位里的升级通道（Task 6）。两边渲染成两个样子的话，
 * 同一批结果在两屏上会被读成两回事。
 */
export function SimilarPanel({ result }: { result: ApiSimilarAssetResult }) {
  return <div className="similar-panel">
    {/* 探针要显示出来。不写「按哪几个变量找的」，人没法判断这批结果值不值得信
        ——按 5 个变量凑齐的一致，和按 1 个变量凑出来的一致，分量差很远。 */}
    {result.probe.length ? <p className="similar-probe">
      按这些变量找的：{result.probe.map(item => `${item.label}=${item.value}`).join('、')}
    </p> : null}

    {result.note ? <p className="similar-note">{result.note}</p> : null}

    {result.items.length ? <ul className="similar-list">
      {result.items.map(item => <li key={item.asset_id}>
        <div className="similar-head">
          <strong>{item.title || shortId(item.asset_id)}</strong>
          <span className="similar-count">重叠 {item.overlap} 个变量
            {item.admissible_overlap < item.overlap
              ? `（其中 ${item.admissible_overlap} 个能进归因）` : ''}</span>
        </div>
        <ul className="similar-reasons">
          {item.reasons.map(reason => <li key={reason.key}>
            {reason.label} = {reason.value}
            <small>（{featureSourceLabel[reason.source]}）</small>
          </li>)}
        </ul>
      </li>)}
    </ul> : result.note
      // 后端一条都没找到时已经给了 note（「库里没有在这些变量上和它一致的素材。」），
      // 再补一句自己的空态，人会连着读到两句一模一样的话，像是页面出了故障。
      ? null
      : <p className="panel-empty">没有在这些变量上一致的素材。</p>}

    {/* 探针太薄这件事，找到和没找到都要说。
        没找到时：按 1~2 个变量本来就极容易落空，这个否定说明不了任何事，
        而屏幕上它和一个货真价实的否定长得一模一样。
        找到了时同样危险——一条只在「时长=15」上一致的素材，会被当成同一类拉进
        样本里去撑结论，而那一行「重叠 1 个变量」看着像结果，不像警告。 */}
    {result.probe.length > 0 && result.probe.length < 3
      ? <p className="similar-note">
        这次只按 {result.probe.length} 个变量找的——种子素材身上能比对的变量本来就少
        （可能是提取失败，也可能是还没填）。{result.items.length
          ? '按这么薄的条件凑出来的一致，不足以说明它们是同一类；拿去把样本做厚之前，先把变量补齐再找一次。'
          : '所以「一条都没有」不代表库里真的没有相似素材。先去「素材 · 变量」把它的变量补齐，再找一次。'}
      </p>
      : null}
  </div>
}
