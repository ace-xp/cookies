import type { ApiServiceConfiguration } from '../../data/serviceCatalog'

export type CatalogLoadState = 'loading' | 'load-failed' | 'empty' | 'ready'

/**
 * A read failure and an empty catalog look identical if you only check
 * services.length. They must not: telling the operator "nothing is
 * configured" when the backend is simply down sends them re-entering
 * credentials that were never lost.
 */
export function catalogLoadState(input: {
  loading: boolean
  error: string
  services: readonly ApiServiceConfiguration[]
}): CatalogLoadState {
  if (input.loading) return 'loading'
  if (input.error) return 'load-failed'
  return input.services.length === 0 ? 'empty' : 'ready'
}

/**
 * 表单初值。只读服务没有字段，后端把它序列化成空数组——但一个 null 就足以
 * 让整块设置页白屏，所以这里不假设它一定是数组。密钥字段永远从空开始，留空
 * 即沿用已存的那一份。
 */
export function initialFormValues(
  service: Pick<ApiServiceConfiguration, 'fields' | 'values' | 'environment_fallback'>,
): Record<string, string> {
  const values: Record<string, string> = {}
  for (const field of service.fields ?? []) {
    if (field.kind === 'secret') continue
    values[field.name] = service.values?.[field.name] ?? service.environment_fallback?.[field.name] ?? ''
  }
  return values
}

export function readOnlyHint(
  service: Pick<ApiServiceConfiguration, 'env_keys' | 'restart_required'> & { managed_note?: string },
): string {
  // A service configured elsewhere must say where. Listing its environment
  // variables instead would send the operator to edit values that are not the
  // ones actually in use.
  if (service.managed_note) return service.managed_note
  const envKeys = service.env_keys ?? []
  if (envKeys.length === 0) return '这项没有配置项，只展示是否可达。'
  const keys = envKeys.join('、')
  return service.restart_required
    ? `这项要改服务器上的 ${keys}，改完需要重启后端。`
    : `这项要改服务器上的 ${keys}。`
}
