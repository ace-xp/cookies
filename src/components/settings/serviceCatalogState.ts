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

export function readOnlyHint(
  service: Pick<ApiServiceConfiguration, 'env_keys' | 'restart_required'> & { managed_note?: string },
): string {
  // A service configured elsewhere must say where. Listing its environment
  // variables instead would send the operator to edit values that are not the
  // ones actually in use.
  if (service.managed_note) return service.managed_note
  if (service.env_keys.length === 0) return '这项没有配置项，只展示是否可达。'
  const keys = service.env_keys.join('、')
  return service.restart_required
    ? `这项要改服务器上的 ${keys}，改完需要重启后端。`
    : `这项要改服务器上的 ${keys}。`
}
