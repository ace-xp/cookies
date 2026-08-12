import { useState } from 'react'
import { ChevronRight, Quote } from 'lucide-react'
import type { ApiConfidenceLevel, ApiInsightCard } from '../../../data/api'
import { formatScope } from '../experience/ExperienceCard'
import { CiteTargetPanel } from './CiteTargetPanel'

/**
 * 洞察卡。同一条经验在「经验」入口里看的是它的生命周期（谁确认的、要不要复审、
 * 停用没有），在这里看的是**能不能用在这次投放上**——所以卡面第一眼给的是
 * 「建议怎么做」，不是「这条结论处于什么状态」。
 *
 * 卡面四行：结论 / 适用条件 / 建议动作 / 够不够硬。其余全收进展开层。九个字段
 * 全摊在列表上，一屏放不下两张卡，而开工前的人是来扫一遍挑出跟这次有关的。
 */
export function InsightCardItem({ card }: { card: ApiInsightCard }) {
  const [open, setOpen] = useState(false)
  const [citing, setCiting] = useState(false)
  // 后端给的引用数是这一页取数那一刻的。人在这张卡上刚记了一次引用，数字得
  // 当场跟上——为此重取整页会把他正在读的一屏抽走。
  const [added, setAdded] = useState(0)
  const soft = card.confidence !== 'sufficient'
  const uses = card.reference_count + added

  return <article className={`experience-card${soft ? ' experience-card-soft' : ''}`}>
    <header>
      <span className="insight-card-type">{card.type_label}</span>
      <h4>{card.conclusion}</h4>
      {/* 引用次数只在真被引用过时才出现。挂一个「被引用 0 次」，人会读成
          「这条没人用，可能不靠谱」——而绝大多数经验只是还没轮到。 */}
      {uses > 0 ? <span className="insight-card-uses">用过 {uses} 次</span> : null}
    </header>

    <p className="experience-scope">适用：{formatScope(card.applicability)}</p>

    {/* 建议动作是这一页存在的理由：开工前的人要的是「那我该怎么做」，
        不是再读一遍结论。没填这一格的卡也要说话，否则人会以为漏渲染了。 */}
    <p className="insight-card-action">
      {card.recommended_action || '这条没写建议动作，只能当背景知识看。'}
    </p>

    {/* 置信度不到「够硬」的当场说清能拿它干什么。只标一个词，人会把
        「方向性」读成一条弱一点的结论，照着做，然后归因到它头上。 */}
    {soft ? <p className="experience-caveat">
      {confidenceCaveat[card.confidence] ?? card.confidence_hint}
    </p> : null}

    {/* 「凭什么」是查证，「用进 Brief」是动作，两个并排。动作不能藏进展开层：
        开工前的人读完建议就要用，让他先展开一层证据才找得到用的地方，等于
        默认他会读完证据再动手——实际上他会复制那句话然后走人，系统什么都没记下。 */}
    <div className="insight-card-tools">
      <button type="button" className="text-button experience-trail-toggle"
        aria-expanded={open} onClick={() => setOpen(!open)}>
        <ChevronRight size={14} className={open ? 'rotated' : ''}/>
        凭什么
      </button>
      <button type="button" className="text-button experience-trail-toggle"
        aria-expanded={citing} onClick={() => setCiting(!citing)}>
        <Quote size={14}/>
        用进 Brief
      </button>
    </div>

    {citing ? <CiteTargetPanel card={card} onRecorded={created => setAdded(current => current + (created ? 1 : 0))}/> : null}

    {open ? <div className="evidence-trail">
      <section>
        <h5>成立的条件</h5>
        {card.conditions.length
          ? <ul>{card.conditions.map(item => <li key={item}>{item}</li>)}</ul>
          : <p className="panel-empty">没写限定条件。</p>}
      </section>

      <section>
        <h5>凭哪些数</h5>
        <p>{describeData(card)}</p>
        {card.data_basis.baseline ? <p>跟什么比：{card.data_basis.baseline}</p> : null}
        <p>够不够硬：{card.confidence_hint || confidenceLabel[card.confidence]}</p>
      </section>

      {card.content_basis.features?.length || card.content_basis.example_asset_versions?.length
        ? <section>
          <h5>看的是哪些内容变量</h5>
          {card.content_basis.features?.length
            ? <p>{card.content_basis.features.join('、')}</p> : null}
          {card.content_basis.example_asset_versions?.length
            ? <p>例子：{card.content_basis.example_asset_versions.join('、')}</p> : null}
          {card.content_basis.note ? <p>{card.content_basis.note}</p> : null}
        </section> : null}

      {/* 反例给底色：填了这一格的经验才是真被人推敲过的，不能和别的段落一样平。 */}
      <section>
        <h5>反例</h5>
        {card.counterexamples.length
          ? <ul className="evidence-counterexamples">
            {card.counterexamples.map(item => <li key={item}>{item}</li>)}
          </ul>
          : <p className="panel-empty">没记过反例。不代表没有，只代表还没人写下来。</p>}
      </section>

      {/* 缺哪几格由后端算好。藏起来的话，一条只填了结论的经验看上去和一条
          九格全填的一样可信。 */}
      {card.missing_fields.length ? <section>
        <h5>这条还缺什么</h5>
        <p>{card.missing_fields.join('、')}——缺这几格不影响它能不能用，但意味着它还没被完整推敲过。</p>
      </section> : null}
    </div> : null}
  </article>
}

const confidenceLabel: Record<ApiConfidenceLevel, string> = {
  sufficient: '样本量和口径都够。',
  directional: '只看得出方向，看不出幅度。',
  low_sample: '样本不够。',
  confounded: '有别的变量没排除掉。',
}

const confidenceCaveat: Record<string, string> = {
  directional: '这条只看得出方向，看不出幅度。可以决定「往哪边走」，别拿它定具体数值。',
  low_sample: '这条的样本不够，换一批素材很可能就不成立了。当线索用，别当依据。',
  confounded: '这条有别的变量没排除掉，效果不一定来自它说的那个原因。照着做之前先想想还有什么在起作用。',
}

/** 「凭哪些数」一行。三格都可能空，空的那格不出现——写成「样本 0」会读成真的是 0。 */
function describeData(card: ApiInsightCard): string {
  const parts: string[] = []
  if (card.data_basis.asset_count) parts.push(`${card.data_basis.asset_count} 条素材`)
  if (card.data_basis.sample_size) parts.push(`样本 ${card.data_basis.sample_size.toLocaleString('zh-CN')}`)
  if (card.data_basis.metrics?.length) parts.push(`看的是${card.data_basis.metrics.join('、')}`)
  const window = card.data_basis.window_start && card.data_basis.window_end
    ? `${card.data_basis.window_start.slice(0, 10)} 至 ${card.data_basis.window_end.slice(0, 10)}`
    : ''
  if (window) parts.push(window)
  return parts.length ? parts.join(' · ') : '这条没写数据依据。'
}
