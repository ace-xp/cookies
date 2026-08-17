import { Fragment, useCallback, useEffect, useState } from 'react'
import { Check, CircleAlert, CircleDashed, RotateCcw } from 'lucide-react'
import { api, summarizeServiceStatus, type ApiServiceConfiguration } from '../../data/api'
import { catalogLoadState } from './serviceCatalogState'
import { ServiceEditor } from './ServiceEditor'

function formatCheckedAt(value?: string) {
  if (!value) return '尚未检查'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '已检查' : date.toLocaleString('zh-CN', { hour12: false })
}

export function ServiceCatalogPage() {
  const [services, setServices] = useState<ApiServiceConfiguration[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [openCode, setOpenCode] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const listed = await api.listServices()
      setServices(listed.services)
    } catch (cause) {
      // 读失败时清空列表并留住 error：catalogLoadState 靠这两个字段
      // 把「读不到」和「真的一条都没配」分开。
      setServices([])
      setError(cause instanceof Error ? cause.message : '读取服务清单失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const replace = (next: ApiServiceConfiguration) =>
    setServices(current => current.map(item => (item.code === next.code ? next : item)))

  const state = catalogLoadState({ loading, error, services })
  const configuredCount = services.filter(item => item.configured).length

  return <section className="service-catalog">
    <div className="provider-form-title">
      <div>
        <h2>外部服务</h2>
        <p>平台要连的所有外部服务都在这里。第一档可以直接改，保存即生效；其余只展示状态与该去哪里改。</p>
      </div>
      <button className="secondary-button" type="button" onClick={() => void load()} disabled={loading}>
        <RotateCcw size={15}/>{loading ? '读取中…' : '刷新'}
      </button>
    </div>

    {state === 'loading' ? <div className="config-notice">正在读取服务清单…</div> : null}
    {state === 'load-failed' ? <div className="config-notice error">读不到服务清单：{error}。这不代表服务没配好，请确认后端是否可用后再刷新。</div> : null}
    {state === 'empty' ? <div className="config-notice">后端没有登记任何外部服务。</div> : null}

    {state === 'ready' ? <>
      <div className="provider-metadata">
        <div><span>已配置</span><b>{configuredCount} / {services.length}</b></div>
      </div>
      <table className="service-catalog-table">
        <thead><tr><th>服务</th><th>状态</th><th>影响面</th><th>最近检查</th></tr></thead>
        <tbody>
          {services.map(service => {
            const status = summarizeServiceStatus(service)
            const open = openCode === service.code
            return <Fragment key={service.code}>
              <tr className={open ? 'open' : ''} onClick={() => setOpenCode(open ? '' : service.code)}>
                <td><b>{service.display_name}</b><small>{service.tier === 'editable' ? '可在页面修改' : '只读'}</small></td>
                <td>
                  {/* 「没法自动检查」既不是好也不是坏，给它一个不催人去修的中性图标。 */}
                  <span className={status === '可用' ? 'config-status configured' : 'config-status'}>
                    {status === '可用'
                      ? <Check size={14}/>
                      : status === '已配置，没法自动检查'
                        ? <CircleDashed size={14}/>
                        : <CircleAlert size={14}/>} {status}
                  </span>
                </td>
                <td>{service.impact}</td>
                <td>{formatCheckedAt(service.last_probe.probed_at)}</td>
              </tr>
              {open ? <tr className="service-catalog-detail">
                <td colSpan={4}><ServiceEditor service={service} onSaved={replace}/></td>
              </tr> : null}
            </Fragment>
          })}
        </tbody>
      </table>
    </> : null}
  </section>
}
