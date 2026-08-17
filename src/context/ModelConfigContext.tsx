import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  api,
  type ApiVideoModelConfiguration,
  type ApiVideoModelConfigurationInput,
  type ApiVideoModelVerification,
} from '../data/api'

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
  /** 视频生成模型的服务端配置，未读到时为 null。 */
  videoConfig: ApiVideoModelConfiguration | null
  isVideoLoading: boolean
  refreshVideoConfig: () => Promise<void>
  saveVideoConfig: (input: ApiVideoModelConfigurationInput) => Promise<ApiVideoModelConfiguration>
  verifyVideoConfig: (input: ApiVideoModelConfigurationInput) => Promise<ApiVideoModelVerification>
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
  const [videoConfig, setVideoConfig] = useState<ApiVideoModelConfiguration | null>(null)
  const [isVideoLoading, setIsVideoLoading] = useState(true)
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

  const refreshVideoConfig = useCallback(async () => {
    setIsVideoLoading(true)
    try {
      setVideoConfig(await api.getVideoModelConfiguration())
    } catch {
      setVideoConfig(null)
    } finally {
      setIsVideoLoading(false)
    }
  }, [])
  useEffect(() => { void refreshVideoConfig() }, [refreshVideoConfig])

  // 保存成功意味着服务端刚探通过一次，能力清单也随之变化，所以顺带刷一次。
  const saveVideoConfig = useCallback(async (input: ApiVideoModelConfigurationInput) => {
    const saved = await api.saveVideoModelConfiguration(input)
    setVideoConfig(saved)
    void refresh()
    return saved
  }, [refresh])

  const verifyVideoConfig = useCallback(
    (input: ApiVideoModelConfigurationInput) => api.verifyVideoModelConfiguration(input),
    [],
  )

  const value = useMemo(() => ({
    providers,
    configuredCount: providers.filter(provider => provider.status === '已配置').length,
    isLoading,
    refresh,
    videoConfig,
    isVideoLoading,
    refreshVideoConfig,
    saveVideoConfig,
    verifyVideoConfig,
  }), [providers, isLoading, refresh, videoConfig, isVideoLoading, refreshVideoConfig, saveVideoConfig, verifyVideoConfig])
  return <ModelConfigContext.Provider value={value}>{children}</ModelConfigContext.Provider>
}

export function useModelConfig() {
  const value = useContext(ModelConfigContext)
  if (!value) throw new Error('useModelConfig must be used inside ModelConfigProvider')
  return value
}
