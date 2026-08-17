export type ServiceFieldKind = 'text' | 'secret'
export type ProbeOutcome = 'ok' | 'auth_failed' | 'unreachable' | 'rejected'

export interface ApiServiceField {
  name: string
  label: string
  kind: ServiceFieldKind
  required: boolean
  placeholder?: string
  help?: string
  /** 候选值，不是白名单：填别的照样能存。 */
  options?: string[]
}

export interface ApiServiceModels {
  models: string[]
  outcome: ProbeOutcome
  message: string
  upstream_message?: string
}

/**
 * 下拉里该出现哪些值：上游读回来的排前面，目录里的候选补在后面，去重。
 * 上游那份是这把密钥真能调的，所以它优先；目录那份是编进二进制的，会随
 * 服务商上新而过时，只当兜底。
 */
export function mergeModelOptions(
  fromUpstream: readonly string[] | undefined,
  fromCatalog: readonly string[] | undefined,
): string[] {
  const merged: string[] = []
  for (const value of [...(fromUpstream ?? []), ...(fromCatalog ?? [])]) {
    const trimmed = value.trim()
    if (trimmed !== '' && !merged.includes(trimmed)) merged.push(trimmed)
  }
  return merged
}

export interface ApiServiceProbe {
  outcome: ProbeOutcome
  message: string
  upstream_message?: string
  probed_at?: string
}

export interface ApiServiceConfiguration {
  code: string
  display_name: string
  tier: 'editable' | 'readonly'
  impact: string
  fields: ApiServiceField[]
  env_keys: string[]
  restart_required: boolean
  /** 这项上游能不能读出模型清单；读不出的（比如火山语音填的是资源 ID）不显示按钮。 */
  models_listable?: boolean
  /** Set when the service is configured somewhere other than this page. */
  managed_note?: string
  configured: boolean
  values: Record<string, string>
  masked_secrets: Record<string, string>
  credential_readable: boolean
  version: number
  updated_at?: string
  last_probe: ApiServiceProbe
  environment_fallback?: Record<string, string>
}

export interface ServiceSubmitBody {
  values: Record<string, string>
  expected_version: number | undefined
}

/**
 * An untouched secret input is submitted as absent rather than as an empty
 * string, so the server keeps the stored credential instead of clearing it.
 */
export function serviceSubmitBody<Field extends Pick<ApiServiceField, 'name' | 'kind'>>(
  fields: readonly Field[],
  values: Record<string, string>,
  expectedVersion: number | undefined,
): ServiceSubmitBody {
  const submitted: Record<string, string> = {}
  for (const field of fields) {
    const value = (values[field.name] ?? '').trim()
    if (field.kind === 'secret' && value === '') continue
    submitted[field.name] = value
  }
  return { values: submitted, expected_version: expectedVersion }
}

export function summarizeServiceStatus(
  config: Pick<ApiServiceConfiguration, 'configured' | 'last_probe'>,
): '可用' | '已配置但连不通' | '未配置' {
  if (!config.configured) return '未配置'
  return config.last_probe.outcome === 'ok' ? '可用' : '已配置但连不通'
}
