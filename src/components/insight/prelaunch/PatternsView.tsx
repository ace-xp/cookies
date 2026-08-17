import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiConfidenceLevel, type ApiFeaturePattern } from '../../../data/api'

/**
 * 「历史模式」：哪些内容特征反复被证明有效。
 *
 * 跟「结论」那一屏的区别是**问法反过来了**。结论那边问「这次的条件下有什么能用」，
 * 一条条读；这边问「有没有什么东西一直有效」，看的是次数。一个特征出现在三条
 * 互相独立的结论里，比出现在一条里可信得多——而按条读结论时看不出这件事。
 *
 * 数据来自同一次 /prelaunch 请求，只是取的是 patterns 而不是 cards。
 */
export function PatternsView() {
  const { currentProject } = useProject()
  const [patterns, setPatterns] = useState<ApiFeaturePattern[]>([])
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const data = await api.getPreLaunch(currentProject.id)
      setPatterns(data.patterns ?? [])
      setListState('ready')
      setNotice('')
    } catch (cause) {
      setPatterns([])
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '历史模式读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])

  const repeated = patterns.filter(pattern => pattern.repeated)
  const once = patterns.filter(pattern => !pattern.repeated)

  return <div className="experience-lookup">
    <div className="core-flow-toolbar">
      <div>
        <span className="section-label">PRE-LAUNCH · PATTERNS</span>
        <h2>哪些内容特征反复有效</h2>
        <p>同一个特征被几条互相独立的结论提到，比只被提过一次可信得多。够得上「反复」的排在上面，其余的照样列出来，但不算数。</p>
      </div>
      <button className="secondary-button" disabled={listState === 'loading'}
        onClick={() => { void load() }}><RefreshCw size={15}/>刷新</button>
    </div>

    {listState === 'loading' ? <p className="panel-empty">正在统计…</p> : null}
    {listState === 'error' ? <p className="panel-empty">{notice || '历史模式读取失败，请重试。'}</p> : null}
    {listState === 'ready' && !patterns.length ? <p className="panel-empty">
      还没有可统计的内容特征。要出现在这里，同一个特征得先被写进至少一条已确认、
      能归因的经验的「内容依据」里——去「复盘」留下几条经验并确认，这里就有了。
    </p> : null}

    {/* 分两栏陈列，而不是混在一起按次数排：整屏标题写着「反复有效」，
        把「只被提过一次」的和「三条结论都提到」的排成一列，读的人会把整列
        当成同一回事。 */}
    {repeated.length ? <PatternGroup
      title="反复出现的"
      hint="被至少两条互相独立的结论提到，而且名字在特征体系里——同义写法已经合到一起算过了。"
      patterns={repeated}/> : null}
    {once.length ? <PatternGroup
      title="目前只算得上单次"
      hint="要么只被一条结论提到，要么名字没进特征体系。前者是次数不够，后者是同一个意思的几种写法可能被拆成了好几行，各自都没攒够。"
      patterns={once}/> : null}
  </div>
}

function PatternGroup({ title, hint, patterns }: {
  title: string
  hint: string
  patterns: ApiFeaturePattern[]
}) {
  return <>
    <div className="pattern-group-head">
      <span className="section-label">{title}（{patterns.length}）</span>
      <p>{hint}</p>
    </div>
    {patterns.map(pattern => <article key={pattern.feature} className="experience-card">
      <header>
        <span className="insight-card-type">{pattern.card_count} 条结论提到</span>
        {/* 显示 label 不显示 feature：feature 是分桶键，入表的是 hook_type 这种
            英文字段名，直接摆出来等于让人读数据库列名。 */}
        <h4>{pattern.label || pattern.feature}</h4>
      </header>
      <p className="experience-scope">
        出现在：{pattern.channels.length ? pattern.channels.join('、') : '没写渠道'}
        {/* 取最强而不是平均：一条充分证据和一条样本不足被平均成「方向性」，
            会把一条本来能照着做的结论说弱。 */}
        <small>（最强的一条：{confidenceLabel[pattern.best_confidence]}）</small>
      </p>
      {pattern.governed ? null : <p className="pattern-ungoverned">
        这个名字没进特征体系，是复盘时手写的。它的同义写法会各占一行，
        永远攒不到「反复」——去「设置 → 能力运营 → 特征体系」把这个词收进去。
      </p>}
      <ul className="pattern-conclusions">
        {pattern.conclusions.map(item => <li key={item}>{item}</li>)}
      </ul>
    </article>)}
  </>
}

const confidenceLabel: Record<ApiConfidenceLevel, string> = {
  sufficient: '样本和口径都够',
  directional: '只看得出方向',
  low_sample: '样本不够',
  confounded: '有别的变量没排除',
}
