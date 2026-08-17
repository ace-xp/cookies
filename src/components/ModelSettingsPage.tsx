import { MiyunConnectionSettings } from './MiyunConnectionSettings'
import { ServiceCatalogPage } from './settings/ServiceCatalogPage'

export function ModelSettingsPage() {
  return <div className="model-settings-page">
    <header className="model-settings-heading">
      <div><span>组织级配置</span><h1>系统设置</h1><p>平台要连的外部服务统一在此处管理；不会分散在策略、创意、洞察或投放模块。</p></div>
    </header>

    <div className="model-settings-layout">
      <section className="provider-form">
        <ServiceCatalogPage/>
      </section>

      <aside className="model-settings-guide">
        <h3>安全说明</h3>
        <ol><li><span>01</span><p><b>统一校验</b>每次模型调用都由服务端检查路由和凭据。</p></li><li><span>02</span><p><b>不回传密钥</b>浏览器和接口响应都不包含完整密钥。</p></li><li><span>03</span><p><b>变更可追踪</b>模型路由和配置版本会随任务记录。</p></li></ol>
      </aside>
    </div>

    {/* 密云的连接按项目存在 insight_miyun_connections 里，不走 provider 存储，
        所以它留在这里；上面的清单里那一行是只读的，指回这一节。 */}
    <MiyunConnectionSettings/>
  </div>
}
