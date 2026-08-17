import { useEffect, useState, type FormEvent } from 'react'
import { ListChecks, LockKeyhole, PlugZap, RotateCcw, Save } from 'lucide-react'
import { api, mergeModelOptions, serviceSubmitBody, type ApiServiceConfiguration } from '../../data/api'
import { initialFormValues, readOnlyHint } from './serviceCatalogState'

function formatCheckedAt(value?: string) {
  if (!value) return '尚未检查'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '已检查' : date.toLocaleString('zh-CN', { hour12: false })
}

function messageOf(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback
}

// platformRequest 只把 error.message 抛出来，状态码丢了。409 的文案由
// writeServiceConfigurationError 固定发出，按它的前缀识别；两边改文案要一起改。
const conflictMarker = '配置已被其他人改动'

// ClassifyHTTP 已经把上游原话拼进 message 了（「密钥无效……（上游说明：xxx）」），
// upstream_message 是同一句话的原料。这里再拼一次就成了「（上游说明：xxx）（上游
// 说：xxx）」，所以直接用 message。
function probeMessage(probe: { message: string; upstream_message?: string }) {
  return probe.message || probe.upstream_message || '检查失败'
}

export function ServiceEditor({
  service,
  onSaved,
}: {
  service: ApiServiceConfiguration
  onSaved: (next: ApiServiceConfiguration) => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [stale, setStale] = useState(false)
  const [busy, setBusy] = useState<'' | 'verify' | 'save' | 'refresh' | 'models'>('')
  // 上游读回来的模型清单。空数组和「还没读过」要分开：读过但上游一个都没给，
  // 该说「上游没给清单」，而不是继续显示按钮好像没点过。
  const [upstreamModels, setUpstreamModels] = useState<string[] | null>(null)

  // 服务端读回来的值是表单初值；正在编辑的内容不被覆盖，所以只在拿到新
  // 的一份配置时同步。这个 effect 在只读服务上也会跑（早退发生在它之后），
  // 所以取初值的活儿交给 initialFormValues，它不假设字段一定存在。
  useEffect(() => {
    setValues(initialFormValues(service))
    setStale(false)
    setUpstreamModels(null)
  }, [service])

  if (service.tier !== 'editable') {
    return <div className="secret-policy">
      <LockKeyhole size={18}/>
      <div><b>这项不在页面里改</b><p>{readOnlyHint(service)}</p></div>
    </div>
  }

  const body = () => serviceSubmitBody(service.fields, values, service.configured ? service.version : undefined)

  const verify = async () => {
    setBusy('verify')
    setNotice('')
    setError('')
    try {
      const probe = await api.verifyService(service.code, body())
      // unverified 不是失败，是「查不出来」：标红会让人去修一个不存在的故障。
      if (probe.outcome === 'ok' || probe.outcome === 'unverified') setNotice(probeMessage(probe) || '连通正常。')
      else setError(probeMessage(probe))
    } catch (cause) {
      setError(messageOf(cause, '测试失败'))
    } finally {
      setBusy('')
    }
  }

  // 读上游的模型清单。不需要先填对「模型名」——这个按钮存在的意义就是操作
  // 的人还不知道该填什么，所以服务端那条路径只校验地址和密钥。
  const loadModels = async () => {
    setBusy('models')
    setNotice('')
    setError('')
    try {
      const listed = await api.listServiceModels(service.code, body())
      // 这个上游根本没有清单接口，跟填错了不是一回事，所以是提示不是报错。
      if (listed.outcome === 'unverified') {
        setNotice('这个上游不提供模型清单，请照服务商文档把模型名手填进去。')
        return
      }
      if (listed.outcome !== 'ok') {
        setError(probeMessage(listed))
        return
      }
      setUpstreamModels(listed.models)
      setNotice(listed.models.length > 0
        ? `上游给出 ${listed.models.length} 个模型，展开「模型名」右侧的下拉挑一个。`
        : '连通正常，但上游没有给出模型清单，请照服务商文档手填。')
    } catch (cause) {
      setError(messageOf(cause, '读取模型列表失败'))
    } finally {
      setBusy('')
    }
  }

  const refresh = async () => {
    setBusy('refresh')
    setNotice('')
    setError('')
    try {
      const listed = await api.listServices()
      const next = listed.services.find(item => item.code === service.code)
      if (next) onSaved(next)
      setStale(false)
    } catch (cause) {
      setError(messageOf(cause, '刷新失败'))
    } finally {
      setBusy('')
    }
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy('save')
    setNotice('')
    setError('')
    try {
      const saved = await api.saveService(service.code, body())
      onSaved(saved)
      setNotice(saved.last_probe.outcome === 'unverified'
        ? '已保存并立即生效。这项没法自动检查，请自己确认一次是否可用。'
        : '已保存并立即生效。')
    } catch (cause) {
      const message = messageOf(cause, '保存失败')
      // 版本对不上只能刷新后重来；其它错误直接重试就行，所以只有这一种
      // 会把保存按钮锁住。
      if (message.includes(conflictMarker)) setStale(true)
      setError(message)
    } finally {
      setBusy('')
    }
  }

  return <div className="service-editor">
    <div className="secret-policy">
      <LockKeyhole size={18}/>
      <div><b>密钥由服务端加密保存</b><p>保存前会先探一次连通性：确实探出问题就不写入；有的上游没法探，那就照常保存并注明。页面只显示密钥末四位。</p></div>
    </div>

    <form className="provider-fields" onSubmit={submit}>
      {service.fields.map(field => {
        const masked = service.masked_secrets[field.name]
        const secret = field.kind === 'secret'
        // datalist 而不是 select：清单只是候选，服务商随时会上新型号，页面不
        // 能因为编在二进制里的那份没更新就让人填不进去。
        const options = secret ? [] : mergeModelOptions(
          field.name === 'model' ? upstreamModels ?? undefined : undefined, field.options)
        const listID = options.length > 0 ? `${service.code}-${field.name}-options` : undefined
        const listable = field.name === 'model' && service.models_listable === true
        return <label key={field.name}>{field.label}
          <input
            type={secret ? 'password' : 'text'}
            autoComplete={secret ? 'off' : undefined}
            list={listID}
            value={values[field.name] ?? ''}
            onChange={event => setValues(current => ({ ...current, [field.name]: event.target.value }))}
            placeholder={secret && masked ? `留空则沿用已保存的 ${masked}` : field.placeholder}
            required={field.required && !(secret && Boolean(masked))}
          />
          {listID ? <datalist id={listID}>{options.map(option => <option key={option} value={option}/>)}</datalist> : null}
          {listable
            ? <button className="link-button" type="button" onClick={() => void loadModels()} disabled={busy !== ''}>
                <ListChecks size={14}/>{busy === 'models' ? '读取中…' : '读取可用模型'}
              </button>
            : null}
          {field.help ? <small>{field.help}</small> : null}
        </label>
      })}
      <div className="provider-actions inline">
        <button className="secondary-button" type="button" onClick={() => void refresh()} disabled={busy !== ''}>
          <RotateCcw size={15}/>{busy === 'refresh' ? '读取中…' : '刷新状态'}
        </button>
        <button className="secondary-button" type="button" onClick={() => void verify()} disabled={busy !== ''}>
          <PlugZap size={15}/>{busy === 'verify' ? '测试中…' : '测试连接'}
        </button>
        <button className="primary-button" type="submit" disabled={busy !== '' || stale}>
          <Save size={15}/>{busy === 'save' ? '保存中…' : '保存配置'}
        </button>
      </div>
    </form>

    {notice ? <div className="config-notice">{notice}</div> : null}
    {error ? <div className="config-notice error">{error}</div> : null}

    <div className="provider-metadata">
      <div><span>影响面</span><b>{service.impact}</b></div>
      <div><span>密钥状态</span><b>{
        service.configured
          ? service.credential_readable
            ? Object.values(service.masked_secrets)[0] ?? '已保存'
            : '主密钥已更换，请重填'
          : '未保存'
      }</b></div>
      <div><span>最近检查</span><b>{formatCheckedAt(service.last_probe.probed_at)}</b></div>
      <div><span>检查结果</span><b>{service.last_probe.message || '尚未检查'}</b></div>
    </div>
  </div>
}
