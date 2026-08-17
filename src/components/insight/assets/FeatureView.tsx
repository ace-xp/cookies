import { useState } from 'react'
import type { DataState } from '../../../types'
import { ContentAnalysisPage } from '../../ContentAnalysisPage'
import { featureSourceLabel } from '../../../data/featureSource'

/**
 * 「变量」视图。把创意内容转成可比较的变量。
 *
 * 原来这里是七个侧栏入口（六类素材各一个 + 单素材拆解）。七个入口的差别只是
 * 特征体系不同——那是一个筛选条件，不是七个页面。做成七个入口，人得先决定
 * 「我要看哪一类」才进得来，而多数时候人想的是「我要看这条素材的变量」。
 *
 * 所以这里改成一个下拉。**每个变量都会显示它的来源**（量出来的 / 人标的 /
 * 模型猜的），这是 P0 定的准入规则在这一页的落点：归因只认前两种。
 */
const options = [
  { value: '单素材拆解', label: '单条素材逐项拆解' },
  { value: '小红书图文', label: '小红书图文' },
  { value: '公众号图文', label: '公众号图文' },
  { value: '品牌广告', label: '品牌广告' },
  { value: '数字人', label: '数字人效果广告' },
  { value: '广告前贴', label: '广告前贴' },
  { value: '爆款复刻', label: '爆款复刻' },
]

export function FeatureView({ state, focusAssetId }: {
  state: DataState
  /** 路由上带过来的素材 ID，进来直接选中它。米云素材页把人送过来时会带。 */
  focusAssetId?: string
}) {
  // 类型选择留在这一层，理由同 IntakeView：抛给导航会把二级视图名冲掉。
  // 指名送过来一条素材时停在「单素材拆解」——它本来就是第一项，这里不必特判。
  const [active, setActive] = useState(options[0].value)

  return <div className="assets-delegate">
    <div className="assets-subtabs">
      <label>看哪一类<select aria-label="素材类型" value={active}
        onChange={event => setActive(event.target.value)}>
        {options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select></label>
      <span className="assets-subtab-hint">
        每个取值后面都标着来源：{Object.values(featureSourceLabel).join(' / ')}。
        归因只认前两种，模型猜的只能参考。
      </span>
    </div>
    <ContentAnalysisPage state={state} activeView={active} focusAssetId={focusAssetId}/>
  </div>
}
