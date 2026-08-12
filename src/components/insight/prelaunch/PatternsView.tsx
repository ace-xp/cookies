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

  return <div className="experience-lookup">
    <div className="core-flow-toolbar">
      <div>
        <span className="section-label">PRE-LAUNCH · PATTERNS</span>
        <h2>哪些内容特征反复有效</h2>
        <p>同一个特征被几条互相独立的结论提到，比只被提过一次可信得多。这一屏按次数排。</p>
      </div>
      <button className="secondary-button" disabled={listState === 'loading'}
        onClick={() => { void load() }}><RefreshCw size={15}/>刷新</button>
    </div>

    {listState === 'loading' ? <p className="panel-empty">正在统计…</p> : null}
    {listState === 'error' ? <p className="panel-empty">{notice || '历史模式读取失败，请重试。'}</p> : null}
    {listState === 'ready' && !patterns.length ? <p className="panel-empty">
      还没有反复出现的内容特征。要出现在这里，同一个特征得先被写进至少一条已确认、
      能归因的经验的「内容依据」里——去「复盘」沉淀几条就有了。
    </p> : null}

    {patterns.map(pattern => <article key={pattern.feature} className="experience-card">
      <header>
        <span className="insight-card-type">{pattern.card_count} 条结论提到</span>
        <h4>{pattern.feature}</h4>
      </header>
      <p className="experience-scope">
        出现在：{pattern.channels.length ? pattern.channels.join('、') : '没写渠道'}
        {/* 取最强而不是平均：一条充分证据和一条样本不足被平均成「方向性」，
            会把一条本来能照着做的结论说弱。 */}
        <small>（最强的一条：{confidenceLabel[pattern.best_confidence]}）</small>
      </p>
      <ul className="pattern-conclusions">
        {pattern.conclusions.map(item => <li key={item}>{item}</li>)}
      </ul>
    </article>)}
  </div>
}

const confidenceLabel: Record<ApiConfidenceLevel, string> = {
  sufficient: '样本和口径都够',
  directional: '只看得出方向',
  low_sample: '样本不够',
  confounded: '有别的变量没排除',
}
