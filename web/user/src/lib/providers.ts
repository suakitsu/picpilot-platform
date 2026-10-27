import { apiFetch, ApiError } from './api'

export const PROTOCOLS = {
  text: ['openai_chat_completions', 'openai_responses'],
  image: ['openai_images_generations', 'openai_images_edits', 'gemini_generate_content'],
  video: ['apimart_video', 'hfsy_video', 'agnes_video']
} as const
export type Capability = keyof typeof PROTOCOLS
export interface ProviderConfig {
  id: string
  name: string
  capability: Capability
  protocol: string
  base_url: string
  model: string
  enabled: boolean
  priority: number
  max_concurrency: number
  timeout_seconds: number
  has_api_key: boolean
  execution_mode?: 'http' | 'pull_worker'
  profile?: string
  version?: number
  local_wait_seconds?: number
  offline_action?: string
  lease_seconds?: number
  settings?: Record<string, unknown>
}
export interface ProviderCatalog {
  providers: ProviderConfig[]
  allowed_protocols: Record<Capability, string[]>
}
export type ProviderInput = Omit<ProviderConfig, 'has_api_key'> & { api_key?: string }

export function providerPayload(input: ProviderInput) {
  return {
    id: input.id, name: input.name.trim(), capability: input.capability,
    protocol: input.protocol, base_url: input.base_url.trim(), model: input.model.trim(),
    enabled: input.enabled, priority: input.priority, max_concurrency: input.max_concurrency,
    timeout_seconds: input.timeout_seconds,
    ...(input.execution_mode ? { execution_mode: input.execution_mode } : {}),
    ...(input.profile ? { profile: input.profile } : {}),
    ...(input.version !== undefined ? { version: input.version } : {}),
    ...(input.local_wait_seconds !== undefined ? { local_wait_seconds: input.local_wait_seconds } : {}),
    ...(input.offline_action ? { offline_action: input.offline_action } : {}),
    ...(input.lease_seconds !== undefined ? { lease_seconds: input.lease_seconds } : {}),
    ...(input.settings ? { settings: input.settings } : {}),
    ...(input.api_key?.trim() ? { api_key: input.api_key } : {})
  }
}

export async function listProviders(): Promise<ProviderCatalog> {
  const data = await apiFetch<ProviderCatalog>('/api/me/providers')
  if (!data || !Array.isArray(data.providers) || !data.allowed_protocols
    || (Object.keys(PROTOCOLS) as Capability[]).some(key => !Array.isArray(data.allowed_protocols[key]))) {
    throw new ApiError('Invalid provider configuration response', 'INVALID_PROVIDER_CONFIG')
  }
  for (const provider of data.providers) {
    if (!provider || !Object.prototype.hasOwnProperty.call(PROTOCOLS, provider.capability)
      || ['id', 'name', 'protocol', 'base_url', 'model'].some(key => typeof provider[key as keyof ProviderConfig] !== 'string')
      || typeof provider.enabled !== 'boolean' || typeof provider.has_api_key !== 'boolean'
      || ['priority', 'max_concurrency', 'timeout_seconds'].some(key => !Number.isInteger(provider[key as keyof ProviderConfig]))
      || (provider.execution_mode !== undefined && !['http', 'pull_worker'].includes(provider.execution_mode))) {
      throw new ApiError('Invalid provider configuration response', 'INVALID_PROVIDER_CONFIG')
    }
  }
  return {
    allowed_protocols: Object.fromEntries((Object.keys(PROTOCOLS) as Capability[]).map(capability => [
      capability, data.allowed_protocols[capability].filter(protocol => (PROTOCOLS[capability] as readonly string[]).includes(protocol))
    ])) as Record<Capability, string[]>,
    // Explicitly whitelist fields: credentials must never enter the read model.
    providers: data.providers.map(p => ({
      id: p.id, name: p.name, capability: p.capability, protocol: p.protocol,
      base_url: p.base_url, model: p.model, enabled: p.enabled, priority: p.priority,
      max_concurrency: p.max_concurrency, timeout_seconds: p.timeout_seconds,
      has_api_key: Boolean(p.has_api_key), execution_mode: p.execution_mode,
      profile: p.profile, version: p.version, local_wait_seconds: p.local_wait_seconds,
      offline_action: p.offline_action, lease_seconds: p.lease_seconds, settings: p.settings
    }))
  }
}

export async function saveProvider(input: ProviderInput) {
  await apiFetch('/api/me/providers/' + encodeURIComponent(input.id), { method: 'PUT', body: JSON.stringify(providerPayload(input)) })
}
export async function deleteProvider(id: string) {
  await apiFetch('/api/me/providers/' + encodeURIComponent(id), { method: 'DELETE' })
}
export async function checkProvider(id: string) {
  return apiFetch<{ valid?: boolean; errors?: string[]; message?: string }>('/api/me/providers/' + encodeURIComponent(id) + '/check', { method: 'POST' })
}
