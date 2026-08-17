export type ServiceFieldKind = 'text' | 'secret'
export type ProbeOutcome = 'ok' | 'auth_failed' | 'unreachable' | 'rejected'

export interface ApiServiceField {
  name: string
  label: string
  kind: ServiceFieldKind
  required: boolean
  placeholder?: string
  help?: string
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
