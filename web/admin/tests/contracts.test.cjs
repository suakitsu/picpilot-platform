const { test, beforeEach, after } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const ts = require('typescript')

// Compile only local TypeScript modules in memory. All HTTP is mocked.
require.extensions['.ts'] = (module, filename) => {
  const source = fs.readFileSync(filename, 'utf8')
  module._compile(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, filename)
}
const runtime = require('../src/lib/runtime.ts')
const api = require('../src/lib/api.ts')
const password = require('../src/lib/password.ts')
const { proxyApi } = require('../src/lib/serverProxy.ts')
const originalFetch = global.fetch
const originalWindow = global.window
const originalOrigin = process.env.GO_API_INTERNAL_URL
let calls, config, needsSetup, routes, storage

const envelope = data => Response.json({ success: true, code: 'OK', message: '', data, request_id: 'mock-request' })
beforeEach(() => {
  runtime.resetRuntime()
  calls = []
  config = { edition: 'personal', setup_required: false, billing_enabled: false, registration_enabled: false, demo_mode: false }
  needsSetup = false
  routes = {}
  storage = new Map()
  global.window = { localStorage: { getItem: key => storage.get(key) || null, setItem: (key,value) => storage.set(key,value), removeItem: key => storage.delete(key) }, dispatchEvent() {}, location: { pathname: '/workspace', search: '', replace() {} } }
  global.fetch = async (url, init = {}) => {
    const path = String(url)
    calls.push({ path, init })
    if (path === '/api/public/config') return envelope(config)
    if (path === '/api/setup/status') return envelope({ needs_setup: needsSetup })
    if (routes[path]) return routes[path](init)
    throw new Error('Unexpected mock request: ' + path)
  }
})
after(() => {
  global.fetch = originalFetch
  global.window = originalWindow
  if (originalOrigin === undefined) delete process.env.GO_API_INTERNAL_URL
  else process.env.GO_API_INTERNAL_URL = originalOrigin
})

test('runtime auth settings come from public config and default disabled', async () => {
  let value = await runtime.loadRuntime()
  assert.equal(value.config.email_login_enabled, false)
  assert.equal(value.config.turnstile_site_key, '')
  runtime.resetRuntime()
  config.email_login_enabled = true
  config.turnstile_site_key = ' mock-runtime-site-key '
  value = await runtime.loadRuntime()
  assert.equal(value.config.email_login_enabled, true)
  assert.equal(value.config.turnstile_site_key, 'mock-runtime-site-key')
})

test('runtime rejects malformed auth settings', async () => {
  config.email_login_enabled = 'true'
  await assert.rejects(runtime.loadRuntime(), { code: 'INVALID_CONFIG' })
  config.email_login_enabled = true
  config.turnstile_site_key = false
  await assert.rejects(runtime.loadRuntime(), { code: 'INVALID_CONFIG' })
})

test('disabled email auth makes no email requests', async () => {
  for (const path of ['/api/auth/email-code', '/api/auth/login/email-code', '/api/auth/forgot-password', '/api/auth/password-reset']) {
    await assert.rejects(api.apiFetch(path, { method: 'POST' }), { code: 'EMAIL_AUTH_DISABLED' })
  }
  assert.equal(calls.length, 2)
})

test('cookie session exchange never sends the stale Bearer token', async () => {
  config.edition = 'platform'
  api.saveSession({ access_token: 'mock-stale-token', user: { id: 'mock-owner', role: 'admin' } })
  routes['/api/auth/session'] = init => {
    assert.equal(init.method, 'POST')
    assert.equal(init.credentials, 'same-origin')
    assert.equal(new Headers(init.headers).has('Authorization'), false)
    assert.deepEqual(JSON.parse(init.body), {})
    return envelope({ access_token: 'mock-cookie-token', user: { id: 'mock-owner', role: 'admin' } })
  }
  const auth = await api.refreshSession()
  assert.equal(auth.access_token, 'mock-cookie-token')
  api.saveSession(auth)
  assert.equal(api.getToken(), 'mock-cookie-token')
})

test('session exchange rejects missing session data and keeps explicit errors', async () => {
  config.edition = 'platform'
  routes['/api/auth/session'] = () => envelope({ access_token: '', user: {} })
  await assert.rejects(api.refreshSession(), { code: 'INVALID_SESSION' })
  routes['/api/auth/session'] = () => Response.json({ success: false, code: 'UNAUTHORIZED', message: 'Cookie session expired', request_id: 'mock-cookie-error' }, { status: 401 })
  await assert.rejects(api.refreshSession(), { code: 'UNAUTHORIZED', requestId: 'mock-cookie-error' })
  assert.equal(api.getToken(), '')
})

test('post-login navigation rejects external and encoded redirect tricks', () => {
  const { safeNextPath } = require('../src/lib/navigation.ts')
  for (const target of [null, 'https://external.test', '//external.test', '/\\external.test', '/%5cexternal.test', '/%2f%2fexternal.test', '/a/..//external.test', '/api/auth/github', '/%', '/\nexternal.test']) {
    assert.equal(safeNextPath(target), '/workspace', String(target))
    assert.equal(safeNextPath(target, '/'), '/', String(target))
  }
  assert.equal(safeNextPath('/projects/mock?view=images#result'), '/projects/mock?view=images#result')
})

test('admin login sends runtime verification and cookie refresh requires admin role', async () => {
  config.edition = 'platform'
  routes['/api/auth/admin/login'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'admin@example.test', password: 'mock-password', turnstile_token: 'mock-captcha' })
    return envelope({ access_token: 'mock-admin-token', user: { id: 'mock-admin', role: 'admin' } })
  }
  await api.adminLogin('admin@example.test', 'mock-password', 'mock-captcha')
  routes['/api/auth/session'] = () => envelope({ access_token: 'mock-user-token', user: { id: 'mock-user', role: 'user' } })
  await assert.rejects(api.refreshSession(), { code: 'FORBIDDEN' })
  assert.equal(api.getToken(), '')
})

test('Go proxy preserves HttpOnly cookie exchange and multiple Set-Cookie headers', async () => {
  process.env.GO_API_INTERNAL_URL = 'http://127.0.0.1:19001'
  global.fetch = async (url, init) => {
    assert.equal(String(url), 'http://127.0.0.1:19001/api/auth/session')
    assert.equal(init.headers.get('cookie'), 'picpilot_session=mock-cookie')
    assert.equal(init.headers.has('authorization'), false)
    const headers = new Headers()
    headers.append('Set-Cookie', 'picpilot_session=mock-new-cookie; HttpOnly; Path=/; SameSite=Lax')
    headers.append('Set-Cookie', 'picpilot_oauth=; HttpOnly; Path=/api/auth/github/callback; Max-Age=0')
    return new Response('{}', { headers })
  }
  const response = await proxyApi(new Request('http://frontend.invalid/api/auth/session', { method: 'POST', headers: { cookie: 'picpilot_session=mock-cookie' }, body: '{}' }))
  assert.deepEqual(response.headers.getSetCookie(), [
    'picpilot_session=mock-new-cookie; HttpOnly; Path=/; SameSite=Lax',
    'picpilot_oauth=; HttpOnly; Path=/api/auth/github/callback; Max-Age=0'
  ])
})

test('password login matches the strict Go payload', async () => {
  config.edition = 'platform'
  routes['/api/auth/admin/login'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'owner@example.test', password: 'mock-only-password' })
    return envelope({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  }
  await api.adminLogin('owner@example.test', 'mock-only-password')
})

test('logout revokes the server session before clearing local state', async () => {
  api.saveSession({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  routes['/api/auth/logout'] = init => {
    assert.equal(init.method, 'POST')
    assert.equal(api.getToken(), 'mock-token')
    return envelope({ logged_out: true })
  }
  await api.logoutSession()
  assert.equal(api.getToken(), '')
  assert.equal(api.getStoredUser(), null)
})

test('failed logout is not reported as successful revocation', async () => {
  api.saveSession({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  routes['/api/auth/logout'] = () => Response.json({ success: false, code: 'MOCK_OFFLINE', message: 'Retry', request_id: 'mock-error' }, { status: 503 })
  await assert.rejects(api.logoutSession(), { code: 'MOCK_OFFLINE' })
  assert.equal(api.getToken(), 'mock-token')
})

test('password change honors requires_login and does not persist passwords', async () => {
  api.saveSession({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  routes['/api/auth/change-password'] = init => {
    assert.deepEqual(JSON.parse(init.body), { current_password: 'mock-old-password', new_password: 'mock-new-password', confirm_password: 'mock-new-password' })
    return envelope({ reset: true, requires_login: true })
  }
  assert.equal((await api.changePassword('mock-old-password', 'mock-new-password', 'mock-new-password')).requires_login, true)
  assert.equal(api.getToken(), '')
  assert.equal(JSON.stringify([...storage]).includes('mock-new-password'), false)
})

test('profile patch and avatar multipart match Go routes', async () => {
  routes['/api/me'] = init => {
    assert.equal(init.method, 'PATCH')
    assert.deepEqual(JSON.parse(init.body), { nickname: 'Mock', ui_language: 'ja-JP', theme: 'dark' })
    return envelope({ id: 'mock-owner', nickname: 'Mock' })
  }
  assert.equal((await api.updateMe({ nickname: 'Mock', uiLanguage: 'ja-JP', theme: 'dark' })).nickname, 'Mock')
  routes['/api/me/avatar'] = init => {
    assert.equal(init.method, 'POST')
    assert.equal(init.body.get('file').name, 'mock.png')
    assert.equal(init.headers['Content-Type'], undefined)
    return envelope({ avatar_url: '/api/files/assets/mock' })
  }
  assert.equal((await api.uploadAvatar(new File(['mock-only'], 'mock.png', { type: 'image/png' }))).avatar_url, '/api/files/assets/mock')
})

test('platform rejects personal provider routes before fetch', async () => {
  config.edition = 'platform'
  await assert.rejects(api.apiFetch('/api/me/providers'), { code: 'EDITION_DISABLED' })
  assert.equal(calls.length, 2)
})

test('platform provider configuration uses admin scope and preserves masked keys', async () => {
  config.edition = 'platform'
  const providers = require('../src/lib/providers.ts')
  const provider = { id: 'platform-1', name: 'Platform', capability: 'text', protocol: 'openai_responses', base_url: 'https://provider.example.test', model: 'custom-model', enabled: true, priority: 1, max_concurrency: 1, timeout_seconds: 60, has_api_key: true, execution_mode: 'http', profile: 'legacy_openai', version: 2 }
  routes['/api/admin/providers'] = () => envelope({ providers: [{ ...provider, api_key: 'mock-hidden-key' }], allowed_protocols: { text: ['openai_responses'], image: [], video: [] } })
  const catalog = await providers.listProviders()
  assert.equal('api_key' in catalog.providers[0], false)
  routes['/api/admin/providers/platform-1'] = init => {
    const body = JSON.parse(init.body)
    assert.equal(init.method, 'PUT')
    assert.equal('api_key' in body, false)
    assert.equal(body.version, 2)
    assert.equal(body.model, 'custom-model')
    return envelope(provider)
  }
  await providers.saveProvider({ ...catalog.providers[0], api_key: '' })
  assert.equal(calls.some(c => c.path.startsWith('/api/me/providers')), false)
  assert.equal(storage.size, 0)
})

test('admin provider validation is not a billable probe and delete uses DELETE', async () => {
  config.edition = 'platform'
  const providers = require('../src/lib/providers.ts')
  routes['/api/admin/providers/platform-1/check'] = init => {
    assert.equal(init.method, 'POST')
    assert.equal(init.body, undefined)
    return envelope({ valid: true, network_checked: false, model_called: false })
  }
  assert.equal((await providers.checkProvider('platform-1')).valid, true)
  routes['/api/admin/providers/platform-1'] = init => { assert.equal(init.method, 'DELETE'); return envelope({ deleted: true }) }
  await providers.deleteProvider('platform-1')
  assert.equal(calls.length, 4)
})

test('runtime deduplicates initialization and respects personal flags', async () => {
  const values = await Promise.all([runtime.loadRuntime(), runtime.loadRuntime(), runtime.loadRuntime()])
  assert.equal(calls.length, 2)
  assert.equal(values[0].config.edition, 'personal')
  assert.equal(runtime.hasBilling({ ...config, billing_enabled: true }), false)
  assert.equal(runtime.hasRegistration({ ...config, registration_enabled: true }), false)
})

test('setup status and public setup flag each gate the application', async () => {
  needsSetup = true
  assert.equal((await runtime.loadRuntime()).needsSetup, true)
  await assert.rejects(api.apiFetch('/api/projects'), { code: 'SETUP_REQUIRED' })
  assert.equal(calls.length, 2)
  needsSetup = false
  config.setup_required = true
  runtime.resetRuntime()
  assert.equal((await runtime.loadRuntime()).needsSetup, true)
})

test('invalid config and unreachable setup fail closed', async () => {
  config.edition = 'unknown'
  await assert.rejects(runtime.loadRuntime(), { code: 'INVALID_CONFIG' })
  assert.equal(calls.length, 1)
  config.edition = 'platform'
  global.fetch = async () => { throw new Error('offline') }
  await assert.rejects(runtime.loadRuntime(), { code: 'SERVER_UNREACHABLE' })
})

test('personal blocks platform endpoints before any protected fetch', async () => {
  for (const path of ['/api/admin/users', '/api/auth/admin/login', '/api/billing/summary', '/api/billing/orders', '/api/benefits/codes', '/api/messages']) {
    await assert.rejects(api.apiFetch(path), { code: 'EDITION_DISABLED' })
  }
  assert.deepEqual(calls.map(c => c.path), ['/api/public/config', '/api/setup/status'])
})

test('same-origin API paths only', async () => {
  for (const path of ['https://former.invalid/api/me', '//former.invalid/api/me', '/api/\\former.invalid']) {
    await assert.rejects(api.apiFetch(path), { code: 'INVALID_API_PATH' })
  }
  assert.equal(calls.length, 0)
})

test('registration and registration codes are blocked by configuration', async () => {
  config.edition = 'platform'
  await assert.rejects(api.apiFetch('/api/auth/register', { method: 'POST' }), { code: 'REGISTRATION_DISABLED' })
  await assert.rejects(api.apiFetch('/api/auth/email-code', { method: 'POST', body: JSON.stringify({ purpose: 'register' }) }), { code: 'REGISTRATION_DISABLED' })
  assert.equal(calls.length, 2)
})

test('platform billing and registration remain functional when enabled', async () => {
  config = { ...config, edition: 'platform', billing_enabled: true, registration_enabled: true }
  routes['/api/billing/summary'] = () => envelope({ credits: 42 })
  routes['/api/auth/register'] = () => envelope({ created: true })
  assert.equal((await api.apiFetch('/api/billing/summary')).credits, 42)
  assert.equal((await api.apiFetch('/api/auth/register', { method: 'POST' })).created, true)
})

test('setup uses exact contract without persisting credentials', async () => {
  needsSetup = true
  routes['/api/setup'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'owner@example.test', password: 'test-password-123', confirm_password: 'test-password-123', setup_token: 'mock-only-token' })
    assert.equal(init.method, 'POST')
    return envelope({ created: true })
  }
  await runtime.completeSetup({ email: 'owner@example.test', password: 'test-password-123', confirm_password: 'test-password-123', setup_token: 'mock-only-token' })
  assert.equal(storage.size, 0)
})

test('API errors preserve backend code and request id', async () => {
  routes['/api/me'] = () => Response.json({ success: false, code: 'ADAPTER_NOT_IMPLEMENTED', message: 'Not implemented', data: null, request_id: 'mock-error' }, { status: 501 })
  await assert.rejects(api.apiFetch('/api/me'), { code: 'ADAPTER_NOT_IMPLEMENTED', requestId: 'mock-error' })
})

test('password policy measures UTF-8 bytes, not UTF-16 characters', () => {
  assert.equal(password.validNewPassword('a'.repeat(11)), false)
  assert.equal(password.validNewPassword('a'.repeat(12)), true)
  assert.equal(password.validNewPassword('a'.repeat(72)), true)
  assert.equal(password.validNewPassword('a'.repeat(73)), false)
  assert.equal(password.passwordByteLength('\u5bc6'.repeat(4)), 12)
  assert.equal(password.validNewPassword('\u5bc6'.repeat(24)), true)
  assert.equal(password.validNewPassword('\u5bc6'.repeat(25)), false)
  assert.equal(password.validNewPassword('\u{1f511}'.repeat(18)), true)
  assert.equal(password.validNewPassword('\u{1f511}'.repeat(19)), false)
})

test('proxy resolves Go origin at request time and preserves body and auth', async () => {
  for (const origin of ['http://127.0.0.1:19001', 'http://127.0.0.1:19002']) {
    process.env.GO_API_INTERNAL_URL = origin
    global.fetch = async (url, init) => {
      assert.equal(String(url), origin + '/api/me/providers/test?check=1')
      assert.equal(init.headers.get('authorization'), 'Bearer mock-auth')
      assert.equal(init.method, 'PUT')
      assert.equal(init.redirect, 'manual')
      assert.equal(await new Response(init.body).text(), '{"enabled":false}')
      return envelope({ saved: true })
    }
    const response = await proxyApi(new Request('http://frontend.invalid/api/me/providers/test?check=1', { method: 'PUT', headers: { authorization: 'Bearer mock-auth' }, body: '{"enabled":false}' }))
    assert.equal(response.status, 200)
    assert.equal((await response.json()).data.saved, true)
  }
})

test('proxy does not fake success when Go is unavailable or misconfigured', async () => {
  process.env.GO_API_INTERNAL_URL = 'http://user:password@127.0.0.1:19001'
  assert.equal((await proxyApi(new Request('http://frontend.invalid/api/public/config'))).status, 503)
  process.env.GO_API_INTERNAL_URL = 'http://127.0.0.1:19001'
  global.fetch = async () => { throw new Error('mock-unavailable') }
  const response = await proxyApi(new Request('http://frontend.invalid/api/public/config'))
  assert.equal(response.status, 502)
  assert.equal((await response.json()).code, 'GO_API_UNAVAILABLE')
})

test('proxy keeps Go-local OAuth redirects same-origin', async () => {
  process.env.GO_API_INTERNAL_URL = 'http://127.0.0.1:19001'
  global.fetch = async () => new Response(null, { status: 302, headers: { location: 'http://127.0.0.1:19001/auth/github/callback?code=mock' } })
  const response = await proxyApi(new Request('http://frontend.invalid/api/auth/github/callback?code=mock'))
  assert.equal(response.headers.get('location'), '/auth/github/callback?code=mock')
})

