import { useCallback, useEffect, useState } from 'react'
import { CircleAlert, FileCode2, Lock } from 'lucide-react'
import { useAuth } from '../../../context/AuthContext'
import { useProject } from '../../../context/ProjectContext'
import { api, type ApiSettingGroup } from '../../../data/api'
import { hasScope, roleSentence } from '../../../data/scopes'

/**
 * 确认权限。
 *
 * 这一屏**只读**，而且没有禁用态的输入框：画一个改不动的框，比直接说「这里改不了」
 * 更让人恼火，而且会让人以为是权限不够而不是功能没做。
 *
 * 三个权限已经在每个接口上生效，**只能按组织角色授予、不能按账号单配**——这一点写在
 * 后端给的 summary 里，不在前端另写一句。它和判定阈值摆在同一个入口下，是因为
 * 两者管的是同一件事的两半：阈值定「机器什么时候敢下结论」，权限定「谁能把机器
 * 说的变成我们认的」。
 *
 * 最上面先说「你自己有哪几档」。一屏讲权限却不说读的人自己有没有，等于把每个人
 * 都留在「我大概能吧」里，直到某个按钮报错才知道。
 */
export function PermissionView() {
  const { currentProject } = useProject()
  const { session } = useAuth()
  const [group, setGroup] = useState<ApiSettingGroup | null>(null)
  const [notice, setNotice] = useState('')
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading')

  const load = useCallback(async () => {
    if (!currentProject.id) return
    setListState('loading')
    try {
      const settings = await api.getInsightSettings(currentProject.id)
      setGroup(settings.groups.find(item => item.key === 'confirmation') ?? null)
      setListState('ready')
    } catch (cause) {
      setGroup(null)
      setListState('error')
      setNotice(cause instanceof Error ? cause.message : '确认权限读取失败。')
    }
  }, [currentProject.id])

  useEffect(() => { void load() }, [load])

  return <div className="settings-page">
    {listState === 'loading' ? <p className="settings-status">读取中…</p> : null}
    {listState === 'error' ? <p className="settings-status error">{notice}</p> : null}

    {group ? <>
      <div className="settings-lock">
        <Lock size={16}/>
        <span>
          <b>{roleSentence(session.membership?.role, session.scopes)}</b>
          权限跟着组织角色走，界面上改不了，也没法只给某个人多开一档。判定阈值那一段可以改，
          这里不行——谁能确认结论是账号体系的事，不是洞察模块自己能决定的。
          要变，去组织成员管理里改角色。
        </span>
      </div>

      {/* 「你能不能确认」是这一屏最实际的问题，单独说一句。上面那行列的是有哪几档，
          这一行说的是它意味着什么。 */}
      {!hasScope(session, 'insights.confirm') ? <p className="settings-summary">
        也就是说：素材分析、复盘报告、经验，你都能填能改，但**最后那一下认可按不动**，
        改判定阈值也按不动。这是分开授予的本意——提特征的人和认结论的人不必是同一个人。
      </p> : null}

      <section className="settings-group">
        <span className="section-label">{group.label}</span>
        <p className="settings-summary">{group.summary}</p>
        {group.state === 'not_built' ? <div className="settings-missing">
          <div className="settings-missing-head"><CircleAlert size={16}/><b>这一组还没有建设</b></div>
          {group.missing.map(line => <p key={line}>{line}</p>)}
        </div> : <div className="setting-list">
          {group.items.map(item => <div className="setting-row" key={item.key}>
            <div className="setting-head">
              <b>{item.label}</b>
              <span className="setting-value">{item.value}</span>
            </div>
            <p className="setting-effect">{item.effect}</p>
            {/* 这一组的「当前值」是管到哪些操作、「推荐」是该发给谁，本来就是两件事，
                所以不比对、也不提示偏离——后端的 deviates 在这一组恒为 false。 */}
            <p className="setting-recommended">建议授予：{item.recommended}</p>
            <p className="setting-meta">
              <FileCode2 size={12}/>
              <span>依据：{item.basis}</span>
            </p>
          </div>)}
        </div>}
      </section>
    </> : null}
  </div>
}
