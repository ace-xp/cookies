import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from '../data/api'

export type ModelProviderId = 'ark'
export type ModelProviderStatus = '未配置' | '已配置'

export interface ModelProviderConfig {
  id: ModelProviderId
  name: string
  description: string
  status: ModelProviderStatus
  baseUrl?: string
  source?: 'environment' | 'workspace'
  maskedApiKey?: string
  lastVerifiedAt?: string
}

interface ModelConfigValue {
  providers: ModelProviderConfig[]
  configuredCount: number
  isLoading: boolean
  refresh: () => Promise<void>
  saveProvider: (input: { apiKey: string; baseUrl?: string }) => Promise<void>
  clearProvider: () => Promise<void>
}

const ModelConfigContext = createContext<ModelConfigValue | null>(null)

const capabilityLabels: Record<string, string> = {
  'document.vision.parse': '文档视觉解析',
  'image.background.remove': '图片去底',
  'image.enhance': '图片增强',
  'image.generate': '图片生成',
  'research.web': '联网研究',
  'text.generate': '文本与策略',
  'video.enhance': '视频增强',
  'video.generate': '视频生成',
  'vision.understand': '图片理解',
}

function summarizeCapabilities(capabilities: Array<{ capability: string; available: boolean }>) {
  const available = capabilities.filter(item => item.available)
  const labels = [...new Set(available.map(item => capabilityLabels[item.capability] ?? '其他能力'))]
  return available.length
    ? `已接入 ${available.length} 条可用模型路由 · ${labels.join('、')}`
    : '尚未检测到可用模型路由'
}

export function ModelConfigProvider({ children }: { children: ReactNode }) {
  const [providers, setProviders] = useState<ModelProviderConfig[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const refresh = useCallback(async () => {
    setIsLoading(true)
    try {
      const capabilities = await api.getCapabilities()
      setProviders([{
        id: 'ark',
        name: '模型能力网关',
        description: summarizeCapabilities(capabilities.capabilities),
        status: capabilities.status === 'configured' ? '已配置' : '未配置',
        source: capabilities.credential?.source,
        maskedApiKey: capabilities.credential?.maskedApiKey,
        lastVerifiedAt: capabilities.checkedAt,
      }])
    } catch {
      setProviders([{ id: 'ark', name: '模型能力网关', description: '暂时无法连接模型服务', status: '未配置' }])
    } finally {
      setIsLoading(false)
    }
  }, [])
  useEffect(() => { void refresh() }, [refresh])

  const saveProvider = useCallback(async (input: { apiKey: string; baseUrl?: string }) => {
    const configuration = await api.updateProviderConfiguration(input)
    setProviders([{
      id: 'ark',
      name: '模型能力网关',
      description: summarizeCapabilities(configuration.capabilities.capabilities),
      status: configuration.status === 'configured' ? '已配置' : '未配置',
      baseUrl: configuration.baseUrl,
      source: configuration.source,
      maskedApiKey: configuration.maskedApiKey,
      lastVerifiedAt: configuration.capabilities.checkedAt,
    }])
  }, [])

  const clearProvider = useCallback(async () => {
    await api.deleteProviderConfiguration()
    await refresh()
  }, [refresh])

  const value = useMemo(() => ({
    providers,
    configuredCount: providers.filter(provider => provider.status === '已配置').length,
    isLoading,
    refresh,
    saveProvider,
    clearProvider,
  }), [providers, isLoading, refresh, saveProvider, clearProvider])
  return <ModelConfigContext.Provider value={value}>{children}</ModelConfigContext.Provider>
}

export function useModelConfig() {
  const value = useContext(ModelConfigContext)
  if (!value) throw new Error('useModelConfig must be used inside ModelConfigProvider')
  return value
}
