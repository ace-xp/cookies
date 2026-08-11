import { useState, type FormEvent } from 'react'
import { Check, CircleAlert, KeyRound, LockKeyhole, RotateCcw, Save, ShieldCheck, Trash2 } from 'lucide-react'
import { useModelConfig } from '../context/ModelConfigContext'

function formatCheckedAt(value?: string) {
  if (!value) return '尚未检查'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '已检查' : date.toLocaleString('zh-CN', { hour12: false })
}

export function ModelSettingsPage() {
  const { providers, configuredCount, isLoading, refresh, saveProvider, clearProvider } = useModelConfig()
  const selected = providers[0]
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('https://ark.cn-beijing.volces.com/api/v3')
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [isSaving, setIsSaving] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setIsSaving(true)
    setNotice('')
    setError('')
    try {
      await saveProvider({ apiKey, baseUrl })
      setApiKey('')
      setNotice('模型密钥已保存到服务端，本页面只保留掩码状态。')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存失败')
    } finally {
      setIsSaving(false)
    }
  }

  const clear = async () => {
    setNotice('')
    setError('')
    try {
      await clearProvider()
      setNotice('已清除页面配置的工作区密钥，服务端将回退到环境变量配置。')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '清除失败')
    }
  }

  return <div className="model-settings-page">
    <header className="model-settings-heading">
      <div><span>组织级配置</span><h1>系统设置</h1><p>模型能力、默认规则和通知配置统一在此处管理；不会分散在策略、创意、洞察或投放模块。</p></div>
      <div className="provider-summary"><b>{configuredCount} / {providers.length || 1}</b><span>服务已配置</span></div>
    </header>

    <div className="model-settings-layout">
      <aside className="provider-index" aria-label="模型服务商">
          <div className="provider-index-title"><KeyRound size={16}/><b>模型服务</b></div>
        {providers.map(provider => <div key={provider.id} className="menu-item">
          <span className={`provider-status-dot ${provider.status === '已配置' ? 'configured' : ''}`}/>
          <span><b>{provider.name}</b><small>{provider.status}</small></span>
        </div>)}
        <div className="provider-index-note"><ShieldCheck size={16}/><span><b>凭据隔离</b><small>完整密钥只保存在服务端；项目数据、导出包和审计记录均不包含密钥。</small></span></div>
      </aside>

      <section className="provider-form">
        <div className="provider-form-title"><div><h2>{selected?.name ?? '正在读取能力'}</h2><p>{selected?.description ?? '等待服务端响应'}</p></div><span className={selected?.status === '已配置' ? 'config-status configured' : 'config-status'}>{selected?.status === '已配置' ? <Check size={14}/> : <CircleAlert size={14}/>} {selected?.status ?? '读取中'}</span></div>
        <div className="secret-policy"><LockKeyhole size={18}/><div><b>密钥由服务端加密保存</b><p>保存后立即生效，页面只显示配置状态，不会读取或展示完整密钥。</p></div></div>

        <form className="provider-fields" onSubmit={submit}>
          <label>模型服务密钥
            <input value={apiKey} onChange={event => setApiKey(event.target.value)} type="password" placeholder={selected?.maskedApiKey ?? '输入新的访问密钥'} autoComplete="off" required/>
          </label>
          <label>服务地址
            <input value={baseUrl} onChange={event => setBaseUrl(event.target.value)} placeholder="https://ark.cn-beijing.volces.com/api/v3"/>
          </label>
          <div className="provider-actions inline">
            <button className="secondary-button danger-text" type="button" onClick={() => void clear()} disabled={isSaving}><Trash2 size={15}/>清除页面配置</button>
            <button className="secondary-button" type="button" onClick={() => void refresh()} disabled={isLoading || isSaving}><RotateCcw size={15}/>{isLoading ? '检查中…' : '刷新状态'}</button>
            <button className="primary-button" type="submit" disabled={isSaving}><Save size={15}/>{isSaving ? '保存中…' : '保存密钥'}</button>
          </div>
        </form>

        {notice ? <div className="config-notice">{notice}</div> : null}
        {error ? <div className="config-notice error">{error}</div> : null}

        <div className="provider-metadata">
          <div><span>生效范围</span><b>服务端全部项目</b></div>
          <div><span>配置来源</span><b>{selected?.source === 'workspace' ? '服务端加密配置' : selected?.source === 'environment' ? '部署环境配置' : selected?.status === '已配置' ? '服务端路由配置' : '尚未配置'}</b></div>
          <div><span>密钥状态</span><b>{selected?.maskedApiKey ?? (selected?.status === '已配置' ? '已加密保存' : '未保存')}</b></div>
          <div><span>最近检查</span><b>{formatCheckedAt(selected?.lastVerifiedAt)}</b></div>
          <div><span>异常处理</span><b>调用失败时显示可操作的诊断信息</b></div>
        </div>
      </section>

      <aside className="model-settings-guide">
        <h3>安全说明</h3>
        <ol><li><span>01</span><p><b>统一校验</b>每次模型调用都由服务端检查路由和凭据。</p></li><li><span>02</span><p><b>不回传密钥</b>浏览器和接口响应都不包含完整密钥。</p></li><li><span>03</span><p><b>变更可追踪</b>模型路由和配置版本会随任务记录。</p></li></ol>
      </aside>
    </div>
  </div>
}
