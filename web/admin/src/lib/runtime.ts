export type Edition = 'platform' | 'personal'
export interface PublicConfig {
  edition: Edition
  setup_required: boolean
  billing_enabled: boolean
  registration_enabled: boolean
  demo_mode: boolean
  email_login_enabled: boolean
  turnstile_site_key: string
}
export interface RuntimeState { config: PublicConfig; needsSetup: boolean }
export interface SetupInput { email: string; password: string; confirm_password: string; setup_token: string }

export class RuntimeError extends Error {
  constructor(message: string, public code = 'RUNTIME_UNAVAILABLE', public requestId = '') {
    super(message)
    this.name = 'RuntimeError'
  }
}

let pending: Promise<RuntimeState> | undefined

export async function publicRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15000)
  try {
    const response = await fetch(path, {
      ...init, cache: 'no-store', credentials: 'same-origin', redirect: 'error',
      headers: { 'Content-Type': 'application/json', ...init.headers }, signal: controller.signal
    })
    let payload
    try { payload = await response.json() } catch {
      throw new RuntimeError('Invalid server response', 'INVALID_RESPONSE')
    }
    if (!response.ok || payload?.success === false) {
      throw new RuntimeError(payload?.message || 'Server request failed', payload?.code || String(response.status), payload?.request_id || '')
    }
    return (payload && typeof payload === 'object' && 'success' in payload ? payload.data : payload) as T
  } catch (error) {
    if (error instanceof RuntimeError) throw error
    throw new RuntimeError('Cannot reach the Go server', 'SERVER_UNREACHABLE')
  } finally { clearTimeout(timeout) }
}

export function resetRuntime() { pending = undefined }

export function loadRuntime(): Promise<RuntimeState> {
  if (!pending) {
    pending = (async () => {
      const config = await publicRequest<PublicConfig>('/api/public/config')
      if (!config || !['platform', 'personal'].includes(config.edition)
        || ['setup_required', 'billing_enabled', 'registration_enabled', 'demo_mode'].some(key => typeof config[key as keyof PublicConfig] !== 'boolean')
        || (config.email_login_enabled !== undefined && typeof config.email_login_enabled !== 'boolean')
        || (config.turnstile_site_key !== undefined && typeof config.turnstile_site_key !== 'string')) {
        throw new RuntimeError('Invalid public configuration', 'INVALID_CONFIG')
      }
      const status = await publicRequest<{ needs_setup: boolean }>('/api/setup/status')
      if (!status || typeof status.needs_setup !== 'boolean') throw new RuntimeError('Invalid setup status', 'INVALID_SETUP_STATUS')
      return { config: { ...config, email_login_enabled: config.email_login_enabled === true, turnstile_site_key: (config.turnstile_site_key || '').trim() }, needsSetup: status.needs_setup || config.setup_required }
    })().catch(error => { pending = undefined; throw error })
  }
  return pending
}

export function hasBilling(config: PublicConfig) { return config.edition === 'platform' && config.billing_enabled }
export function hasRegistration(config: PublicConfig) { return config.edition === 'platform' && config.registration_enabled }

export async function assertApiAllowed(path: string, body?: BodyInit | null) {
  if (!path.startsWith('/api/') || path.includes('\\') || path.includes('#')) throw new RuntimeError('Only same-origin API paths are allowed', 'INVALID_API_PATH')
  const pathname = new URL(path, 'http://local.invalid').pathname
  const { config, needsSetup } = await loadRuntime()
  if (needsSetup) throw new RuntimeError('Initial setup is required', 'SETUP_REQUIRED')
  const admin = /^\/api\/(admin|auth\/admin)(\/|$)/.test(pathname)
  const billing = pathname === '/api/public/benefit-config' || /^\/api\/(billing|benefits)(\/|$)/.test(pathname) || /^\/api\/admin\/(billing|benefits)(\/|$)/.test(pathname)
  const community = /^\/api\/messages(\/|$)/.test(pathname)
  const ownedProviders = /^\/api\/me\/providers(\/|$)/.test(pathname)
  if ((config.edition === 'personal' && (admin || community)) || (config.edition === 'platform' && ownedProviders) || (!hasBilling(config) && billing)) {
    throw new RuntimeError('This feature is unavailable in this edition', 'EDITION_DISABLED')
  }
  const registerCode = pathname === '/api/auth/email-code' && typeof body === 'string' && JSON.parse(body).purpose === 'register'
  if (!hasRegistration(config) && (pathname === '/api/auth/register' || registerCode)) {
    throw new RuntimeError('Registration is disabled', 'REGISTRATION_DISABLED')
  }
  if (!config.email_login_enabled && ['/api/auth/email-code', '/api/auth/login/email-code', '/api/auth/forgot-password', '/api/auth/password-reset'].includes(pathname)) {
    throw new RuntimeError('Email authentication is unavailable on this server', 'EMAIL_AUTH_DISABLED')
  }
}

export async function completeSetup(input: SetupInput) {
  await publicRequest('/api/setup', { method: 'POST', body: JSON.stringify(input) })
  resetRuntime()
}

