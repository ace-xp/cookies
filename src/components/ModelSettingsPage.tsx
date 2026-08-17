import { useEffect, useState, type FormEvent } from 'react'
import { Check, CircleAlert, KeyRound, LockKeyhole, PlugZap, RotateCcw, Save, ShieldCheck } from 'lucide-react'
import { useModelConfig } from '../context/ModelConfigContext'
import { MiyunConnectionSettings } from './MiyunConnectionSettings'

const defaultVideoBaseUrl = 'https://ark.cn-beijing.volces.com/api/v3'

function formatCheckedAt(value?: string) {
  if (!value) return '尚未检查'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '已检查' : date.toLocaleString('zh-CN', { hour12: false })
}

export function ModelSettingsPage() {
  const {
    providers, configuredCount, isLoading, refresh,
    videoConfig, isVideoLoading, refreshVideoConfig, saveVideoConfig, verifyVideoConfig,
    videoGenerationAvailable, videoGenerationManagedByGateway,
  } = useModelConfig()
  const [baseUrl, setBaseUrl] = useState(defaultVideoBaseUrl)
  const [model, setModel] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [isVerifying, setIsVerifying] = useState(false)

  // 读回来的配置是这张表单的初始值；本地正在编辑的内容不被覆盖，
  // 所以只在拿到一份新的服务端配置时同步。
  useEffect(() => {
    if (!videoConfig) return
    setBaseUrl(videoConfig.base_url || videoConfig.environment_fallback.base_url || defaultVideoBaseUrl)
    setModel(videoConfig.model || videoConfig.environment_fallback.model || '')
  }, [videoConfig])

  const configured = videoConfig?.configured ?? false
  const fallback = videoConfig?.environment_fallback
  const status = configured ? '已配置' : fallback?.configured ? '沿用部署配置' : '未配置'
  const needsKey = !configured || !videoConfig?.credential_readable

  const currentInput = () => ({
    baseUrl,
    model,
    apiKey: apiKey || undefined,
    expectedVersion: videoConfig?.configured ? videoConfig.version : undefined,
  })

  const verify = async () => {
    setIsVerifying(true)
    setNotice('')
    setError('')
    try {
      const result = await verifyVideoConfig(currentInput())
      if (result.ok) setNotice(`${result.message}（模型名要等真正出片时才验得出来）`)
      else setError(result.message)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '测试失败')
    } finally {
      setIsVerifying(false)
    }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setIsSaving(true)
    setNotice('')
    setError('')
    try {
      await saveVideoConfig(currentInput())
      setApiKey('')
      setNotice('已保存并立即生效，之后的视频生成都会走这套配置。')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存失败')
    } finally {
      setIsSaving(false)
    }
  }

  return <div className="model-settings-page">
    <header className="model-settings-heading">
      <div><span>组织级配置</span><h1>系统设置</h1><p>模型能力、默认规则和通知配置统一在此处管理；不会分散在策略、创意、洞察或投放模块。</p></div>
      <div className="provider-summary"><b>{configuredCount} / {providers.length || 1}</b><span>服务已配置</span></div>
    </header>

    <MiyunConnectionSettings/>

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
        <div className="provider-form-title">
          <div><h2>视频生成模型</h2><p>创意模块出视频时调用的模型服务。填好后立即生效，不需要重启。</p></div>
          <span className={videoGenerationAvailable ? 'config-status configured' : 'config-status'}>
            {videoGenerationAvailable ? <Check size={14}/> : <CircleAlert size={14}/>} {videoGenerationManagedByGateway ? '由网关托管' : isVideoLoading ? '读取中' : status}
          </span>
        </div>
        {videoGenerationManagedByGateway ? <>
          <div className="secret-policy"><LockKeyhole size={18}/><div><b>视频路由由模型能力网关统一管理</b><p>当前环境通过适配器网关调用视频模型，密钥、上游地址与模型选择不在此页面写入；页面不会再请求不适用的自管配置接口。</p></div></div>
          <div className="provider-metadata">
            <div><span>视频能力</span><b>{videoGenerationAvailable ? '可用' : '暂不可用'}</b></div>
            <div><span>模型别名</span><b>cookies.video.standard</b></div>
            <div><span>配置方式</span><b>适配器网关托管</b></div>
            <div><span>处理方式</span><b>{videoGenerationAvailable ? '可直接在创意页面生成' : '请联系网关管理员检查路由或凭据'}</b></div>
          </div>
        </> : <>
        <div className="secret-policy"><LockKeyhole size={18}/><div><b>密钥由服务端加密保存</b><p>保存前会先探一次连通性，探不通就不写入。页面只显示密钥末四位。</p></div></div>

        <form className="provider-fields" onSubmit={submit}>
          <label>服务地址
            <input value={baseUrl} onChange={event => setBaseUrl(event.target.value)} placeholder={defaultVideoBaseUrl} required/>
          </label>
          <label>模型名
            <input value={model} onChange={event => setModel(event.target.value)} placeholder="doubao-seedance-1-0-lite-t2v-250428" required/>
          </label>
          <label>API Key
            <input value={apiKey} onChange={event => setApiKey(event.target.value)} type="password"
              placeholder={needsKey ? '输入访问密钥' : `留空则沿用已保存的 ${videoConfig?.masked_api_key ?? '密钥'}`}
              autoComplete="off" required={needsKey}/>
          </label>
          <div className="provider-actions inline">
            <button className="secondary-button" type="button" onClick={() => { void refreshVideoConfig(); void refresh() }} disabled={isLoading || isVideoLoading || isSaving}>
              <RotateCcw size={15}/>{isVideoLoading ? '读取中…' : '刷新状态'}
            </button>
            <button className="secondary-button" type="button" onClick={() => void verify()} disabled={isVerifying || isSaving}>
              <PlugZap size={15}/>{isVerifying ? '测试中…' : '测试连接'}
            </button>
            <button className="primary-button" type="submit" disabled={isSaving || isVerifying}><Save size={15}/>{isSaving ? '保存中…' : '保存配置'}</button>
          </div>
        </form>

        {notice ? <div className="config-notice">{notice}</div> : null}
        {error ? <div className="config-notice error">{error}</div> : null}

        <div className="provider-metadata">
          <div><span>生效范围</span><b>服务端全部项目</b></div>
          <div><span>模型别名</span><b>{videoConfig?.model_alias ?? 'cookies.video.standard'}</b></div>
          <div><span>密钥状态</span><b>{configured ? (videoConfig?.credential_readable ? videoConfig.masked_api_key : '主密钥已更换，请重填') : fallback?.configured ? '沿用部署环境密钥' : '未保存'}</b></div>
          <div><span>最近检查</span><b>{formatCheckedAt(videoConfig?.last_verification.verified_at)}</b></div>
          <div><span>检查结果</span><b>{videoConfig?.last_verification.message || '尚未检查'}</b></div>
        </div>
        </>}
      </section>

      <aside className="model-settings-guide">
        <h3>安全说明</h3>
        <ol><li><span>01</span><p><b>统一校验</b>每次模型调用都由服务端检查路由和凭据。</p></li><li><span>02</span><p><b>不回传密钥</b>浏览器和接口响应都不包含完整密钥。</p></li><li><span>03</span><p><b>变更可追踪</b>模型路由和配置版本会随任务记录。</p></li></ol>
      </aside>
    </div>
  </div>
}
