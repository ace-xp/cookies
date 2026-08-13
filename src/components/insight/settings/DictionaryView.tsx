import { useCallback, useEffect, useState } from 'react'
import { FileCode2 } from 'lucide-react'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiSettingGroup } from '../../../data/api'
import type { DataState } from '../../../types'
import { CapabilityOperationsPage } from '../../CapabilityOperationsPage'

/**
 * 变量字典。原来的「能力运营」入口加上原来单开一栏的「名词表」，合成这一屏。
 *
 * 合并的理由是：这两样回答的是同一个问题——**这个模块里的每个词、每个变量、每个
 * 指标，到底指的是什么**。名词表管人说的话，特征体系和指标字典管系统算的数；
 * 分成两处，同一个「充分样本」会在两个地方各有一套说法。
 *
 * 「名词表」排在最前面：不知道词是什么意思的时候，后面五段都读不懂。
 */
const segments = ['名词表', '特征体系', '指标字典', '分析 Skills', '评测集', '质量看板'] as const

export function DictionaryView({ state }: { state: DataState }) {
  const [active, setActive] = useState<string>(segments[0])

  return <div className="assets-delegate">
    <p className="settings-intro">
      变量字典管的是「这个数、这个词到底指什么」。它不改任何判定，但每一条结论都建立在
      它上面——同一个词被两个人读成两个意思，比数字算错更难查。
    </p>
    <div className="assets-subtabs" role="tablist" aria-label="变量字典分段">
      {segments.map(item => <button key={item} type="button" role="tab" aria-selected={active === item}
        className={active === item ? 'assets-subtab active' : 'assets-subtab'}
        onClick={() => setActive(item)}>{item}</button>)}
    </div>
    {active === '名词表'
      ? <GlossarySegment/>
      : <CapabilityOperationsPage state={state} activeView={active}/>}
  </div>
}

/**
 * 名词表。它比阈值更容易悄悄失效：阈值写错了数字会不对，词用错了页面照样能跑，
 * 只是每个人读到的意思略有偏差，半年后谁也说不清「能归因」当初是不是这个意思。
 *
 * 词条全部由后端从 glossary.go 现算，前端一个字都不写死——写死的那一份第二天
 * 就和有测试拦着的那一份对不上了。
 */
function GlossarySegment() {
  const { currentProject } = useProject()
  const [group, setGroup] = useState<ApiSettingGroup | null>(null)
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const settings = await api.getInsightSettings(currentProject.id)
      setGroup(settings.groups.find(item => item.key === 'glossary') ?? null)
      setListState('ready')
    } catch (cause) {
      setGroup(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '名词表读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])

  return <div className="settings-page">
    {listState === 'loading' ? <p className="settings-status">读取中…</p> : null}
    {listState === 'error' ? <p className="settings-status error">{notice}</p> : null}
    {group ? <section className="settings-group">
      <span className="section-label">{group.label}</span>
      <p className="settings-summary">{group.summary}</p>
      <div className="setting-list">
        {group.items.map(item => <div className="setting-row" key={item.key}>
          <div className="setting-head"><b>{item.label}</b></div>
          <p className="setting-effect">{item.value}</p>
          <p className="setting-recommended">{item.effect}</p>
          {/* 源码路径不上屏。它是给改代码的人看的，而这一屏的读者是能力运营的
              管理员；何况写死的行号在代码一动之后全是错的，指错比不指更糟。
              依据（文档章节）留着——那是这条设定凭什么这么定，管理员看得懂也用得上。 */}
          <p className="setting-meta">
            <FileCode2 size={12}/>
            <span>依据：{item.basis}</span>
          </p>
        </div>)}
      </div>
    </section> : null}
  </div>
}
