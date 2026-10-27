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

test('email password reset requires confirmation and invalidates the old session', async () => {
  config.email_login_enabled = true
  api.saveSession({ access_token: 'mock-stale-token', user: { id: 'mock-owner' } })
  routes['/api/auth/password-reset'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'owner@example.test', verification_code: '123456', password: 'mock-new-password', confirm_password: 'mock-new-password', turnstile_token: 'mock-captcha' })
    return envelope({ updated: true, requires_login: true, redirect_to: '/login' })
  }
  const result = await api.confirmPasswordReset('owner@example.test', '123456', 'mock-new-password', 'mock-new-password', 'mock-captcha')
  assert.equal(result.redirect_to, '/login')
  assert.equal(api.getToken(), '')
  assert.equal(JSON.stringify([...storage]).includes('mock-new-password'), false)
  routes['/api/auth/password-reset'] = () => envelope({ updated: false })
  await assert.rejects(api.confirmPasswordReset('owner@example.test', '123456', 'mock-new-password', 'mock-new-password'), { code: 'RESET_UNCONFIRMED' })
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

test('canvas discovery uses sanitized capabilities in both editions without provider details', async () => {
  for (const edition of ['personal', 'platform']) {
    runtime.resetRuntime()
    config.edition = edition
    calls.length = 0
    routes['/api/ai/capabilities'] = () => envelope({
      image: { enabled: true, default_model: 'manual-image-model', models: [{ id: 'manual-image-model', protocol: 'openai_images_edits', api_key: 'mock-secret' }], base_url: 'https://mock-provider.test' },
      text: { enabled: true, default_model: 'manual-text-model', models: [{ id: 'manual-text-model', protocol: 'openai_responses' }] },
      video: { enabled: false, default_model: '', models: [] }
    })
    const result = await api.getAiCapabilities()
    assert.equal(result.image.default_model, 'manual-image-model')
    assert.equal(result.text.models[0].protocol, 'openai_responses')
    assert.equal(JSON.stringify(result).includes('mock-secret'), false)
    assert.equal(JSON.stringify(result).includes('base_url'), false)
    assert.deepEqual(calls.map(call => call.path), ['/api/public/config', '/api/setup/status', '/api/ai/capabilities'])
  }
})

test('invalid model discovery does not invent defaults or availability', async () => {
  routes['/api/ai/capabilities'] = () => envelope({ image: { enabled: true, default_model: 'unknown', models: [] } })
  await assert.rejects(api.getAiCapabilities(), { code: 'INVALID_CAPABILITIES' })
  routes['/api/ai/capabilities'] = () => Response.json({ success: false, code: 'CAPABILITIES_UNAVAILABLE', message: 'Mock unavailable', request_id: 'mock-capability-error' }, { status: 503 })
  await assert.rejects(api.getAiCapabilities(), { code: 'CAPABILITIES_UNAVAILABLE', requestId: 'mock-capability-error' })
})

test('password login matches the strict Go payload', async () => {
  routes['/api/auth/login'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'owner@example.test', password: 'mock-only-password' })
    return envelope({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  }
  await api.login('owner@example.test', 'mock-only-password')
})

test('password login sends optional remember and verification fields when supplied', async () => {
  routes['/api/auth/login'] = init => {
    assert.deepEqual(JSON.parse(init.body), { email: 'owner@example.test', password: 'mock-only-password', remember_me: true, turnstile_token: 'mock-verification' })
    return envelope({ access_token: 'mock-token', user: { id: 'mock-owner' } })
  }
  await api.login('owner@example.test', 'mock-only-password', true, 'mock-verification')
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

const providers = require('../src/lib/providers.ts')
const provider = { id: 'owned-1', name: 'My model', capability: 'text', protocol: 'openai_responses', base_url: 'https://model.example.test/v1', model: 'vendor/model-custom', enabled: true, priority: 1, max_concurrency: 2, timeout_seconds: 80, execution_mode: 'pull_worker', has_api_key: true }

test('personal generation does not require a fabricated billing balance', async () => {
  assert.equal(await api.getOptionalBillingSummary(), null)
  assert.equal(calls.length, 2)
  routes['/api/projects/creative-images'] = () => envelope({ project: { id: 'mock-project' }, image_tasks: [] })
  const result = await api.createCreativeImage({ productName: 'Mock product', brand: '', prompt: 'Mock prompt', size: '1024x1024', language: 'en', module: 'custom' })
  assert.equal(result.project.id, 'mock-project')
  assert.equal(calls.filter(c => c.path.includes('/billing')).length, 0)
})

test('video multipart generation preserves idempotency and manual model code', async () => {
  routes['/api/videos/tasks'] = init => {
    assert.equal(init.headers['Idempotency-Key'], 'mock-idempotency')
    assert.equal(init.body.get('model_code'), 'my-video-model')
    assert.equal(init.headers['Content-Type'], undefined)
    return envelope({ id: 'mock-video', status: 'queued' })
  }
  const result = await api.createVideoTask({ idempotencyKey: 'mock-idempotency', title: 'Mock', prompt: 'Mock only', aspectRatio: '16:9', modelCode: 'my-video-model', durationSeconds: 8, resolution: '720p', generateAudio: false, images: [] })
  assert.equal(result.id, 'mock-video')
  assert.equal(calls.filter(c => c.path.includes('/billing')).length, 0)
})

test('provider read model strips keys and does not invent unsupported protocols', async () => {
  routes['/api/me/providers'] = () => envelope({ providers: [{ ...provider, api_key: 'mock-secret-never-exposed' }], allowed_protocols: { text: ['openai_responses', 'made_up'], image: [], video: ['hfsy_video'] } })
  const data = await providers.listProviders()
  assert.equal('api_key' in data.providers[0], false)
  assert.deepEqual(data.allowed_protocols.text, ['openai_responses'])
  assert.deepEqual(data.allowed_protocols.image, [])
  assert.equal(storage.size, 0)
})

test('provider writes whitelist fields and preserve blank keys', async () => {
  const input = { ...provider, profile: 'legacy_openai', version: 3, settings: { size_mapping: { square: '1024x1024' } }, api_key: '   ', unexpected_secret: 'ignored' }
  const body = providers.providerPayload(input)
  assert.equal('api_key' in body, false)
  assert.equal('has_api_key' in body, false)
  assert.equal('unexpected_secret' in body, false)
  assert.equal(body.model, 'vendor/model-custom')
  assert.equal(body.profile, 'legacy_openai')
  assert.equal(body.version, 3)
  assert.deepEqual(body.settings, input.settings)
  routes['/api/me/providers/owned-1'] = init => {
    assert.equal(init.method, 'PUT')
    assert.equal(JSON.parse(init.body).api_key, 'mock-new-key')
    return envelope({})
  }
  await providers.saveProvider({ ...provider, api_key: 'mock-new-key' })
  assert.equal(storage.size, 0)
})

test('provider check only calls validation endpoint, never generation', async () => {
  routes['/api/me/providers/owned-1/check'] = init => {
    assert.equal(init.method, 'POST')
    assert.equal(init.body, undefined)
    return envelope({ valid: true })
  }
  assert.equal((await providers.checkProvider('owned-1')).valid, true)
  assert.deepEqual(calls.map(c => c.path), ['/api/public/config', '/api/setup/status', '/api/me/providers/owned-1/check'])
})

test('provider deletion uses the owned-provider endpoint', async () => {
  routes['/api/me/providers/owned-1'] = init => { assert.equal(init.method, 'DELETE'); return envelope({}) }
  await providers.deleteProvider('owned-1')
  assert.equal(calls.at(-1).path, '/api/me/providers/owned-1')
})

