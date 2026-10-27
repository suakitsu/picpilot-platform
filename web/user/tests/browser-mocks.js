async page => {
  const state = { edition: 'personal', needsSetup: true, billing: false, registration: false, emailLogin: true, siteKey: '', requests: [], external: [], failConfig: false, failSave: false, providers: [
    { id: 'mock-text', name: 'My text provider', capability: 'text', protocol: 'openai_responses', base_url: 'https://provider.example.test/v1', model: 'manual-text-model', enabled: true, priority: 1, max_concurrency: 2, timeout_seconds: 90, has_api_key: true, execution_mode: 'http' }
  ] }
  page.__picpilotMocks = state
  state.pageErrors = []
  page.on('pageerror', error => state.pageErrors.push(error.message))
  const user = { id: 'mock-owner', email_masked: 'owner@example.test', nickname: 'Owner', role: 'owner', credits: 0, ui_language: 'en-US', theme: 'light' }
  await page.context().addInitScript(user => {
    localStorage.setItem('openimage-language', 'en-US')
    localStorage.setItem('openimage-theme', 'light')
    if (!sessionStorage.getItem('mock-session-seeded')) {
      localStorage.setItem('openimage_token', 'mock-browser-token')
      localStorage.setItem('openimage_user', JSON.stringify(user))
      localStorage.setItem('openimage_admin_token', 'mock-browser-admin-token')
      localStorage.setItem('openimage_admin_user', JSON.stringify({ ...user, role: 'admin' }))
      sessionStorage.setItem('mock-session-seeded', '1')
    }
  }, user)
  await page.context().route('**/*', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin === 'https://challenges.cloudflare.com' && url.pathname.endsWith('/api.js')) {
      return route.fulfill({ contentType: 'application/javascript', body: `
        (() => {
          const widgets = new Map();
          window.turnstile = {
            render(container, options) {
              const id = 'mock-' + widgets.size;
              const button = document.createElement('button');
              button.type = 'button'; button.textContent = 'Complete mock verification';
              container.dataset.runtimeSiteKey = options.sitekey;
              button.addEventListener('click', () => options.callback('mock-captcha-token'));
              container.appendChild(button); widgets.set(id, { container, options }); return id;
            },
            reset(id) { widgets.get(id)?.options.callback(''); },
            remove(id) { widgets.get(id)?.container.replaceChildren(); widgets.delete(id); }
          };
        })();
      ` })
    }
    if (['127.0.0.1', 'localhost'].includes(url.hostname) && ['13110', '13111'].includes(url.port)) {
      return route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Mock canvas runtime</title><p>Mock canvas runtime</p>' })
    }
    if (!['127.0.0.1', 'localhost'].includes(url.hostname) || !['13280', '13281'].includes(url.port)) { state.external.push(url.origin); await route.abort(); return }
    if (!url.pathname.startsWith('/api/')) { await route.continue(); return }
    const path = url.pathname
    const method = request.method()
    state.requests.push({ path, method, body: request.postData(), hasAuthorization: Boolean(request.headers().authorization), hasSessionCookie: (request.headers().cookie || '').includes('picpilot_session=mock-oauth-cookie') })
    const ok = data => route.fulfill({ json: { success: true, code: 'OK', message: '', data, request_id: 'mock-browser' } })
    const fail = (code, message, status = 501) => route.fulfill({ status, json: { success: false, code, message, data: null, request_id: 'mock-browser-error' } })
    if (path === '/api/public/config') return state.failConfig ? fail('MOCK_OFFLINE', 'Mock unavailable') : ok({ edition: state.edition, setup_required: state.needsSetup, billing_enabled: state.billing, registration_enabled: state.registration, demo_mode: false, email_login_enabled: state.emailLogin, turnstile_site_key: state.siteKey })
    if (path === '/api/setup/status') return ok({ needs_setup: state.needsSetup })
    if (path === '/api/setup' && method === 'POST') { state.needsSetup = false; return ok({ created: true }) }
    const profile = { ...user, role: state.edition === 'personal' ? 'owner' : 'admin' }
    if (path === '/api/auth/session' && method === 'POST') {
      if (request.headers().authorization) return fail('STALE_BEARER', 'Cookie exchange must not send a Bearer token')
      if (state.failSession || !(request.headers().cookie || '').includes('picpilot_session=mock-oauth-cookie')) return fail('UNAUTHORIZED', 'Mock cookie session expired', 401)
      return ok({ access_token: 'mock-cookie-session-token', user: profile })
    }
    if (['/api/auth/login', '/api/auth/admin/login'].includes(path) && method === 'POST') {
      const body = request.postDataJSON()
      const allowed = ['email', 'password', 'remember_me', 'turnstile_token']
      if (Object.keys(body).some(key => !allowed.includes(key)) || typeof body.email !== 'string' || typeof body.password !== 'string'
        || ('remember_me' in body && typeof body.remember_me !== 'boolean') || ('turnstile_token' in body && typeof body.turnstile_token !== 'string')) return fail('INVALID_REQUEST', 'Invalid login field')
      if (path === '/api/auth/admin/login' && (state.edition !== 'platform' || !['admin', 'super_admin'].includes(profile.role))) return fail('FORBIDDEN', 'Administrator access is required')
      if (state.siteKey && body.turnstile_token !== 'mock-captcha-token') return fail('CAPTCHA_REQUIRED', 'Mock verification is required', 400)
      return ok({ user: profile, access_token: 'mock-browser-token' })
    }
    if (path === '/api/auth/logout') return ok({ logged_out: true })
    if (path === '/api/auth/email-code' && method === 'POST') return ok({ sent: true, cooldown_seconds: 1, expires_in_seconds: 300, purpose: request.postDataJSON().purpose, email_masked: 'owner@example.test' })
    if (path === '/api/auth/password-reset' && method === 'POST') return state.failReset ? fail('VERIFICATION_INVALID', 'Mock reset code expired', 400) : ok({ updated: true, requires_login: true, redirect_to: '/login' })
    if (path === '/api/auth/change-password') return state.failPassword ? fail('PASSWORD_INVALID', 'Current password is incorrect') : ok({ reset: true, requires_login: true })
    if (path === '/api/me') return ok(method === 'PATCH' ? { ...profile, ...request.postDataJSON() } : profile)
    if (path === '/api/me/avatar') return ok({ ...profile, avatar_url: '' })
    if ((path === '/api/me/providers' || path === '/api/admin/providers') && method === 'GET') return ok({ providers: state.providers, allowed_protocols: { text: ['openai_chat_completions', 'openai_responses'], image: ['openai_images_generations', 'openai_images_edits', 'gemini_generate_content'], video: ['apimart_video', 'hfsy_video', 'agnes_video'] } })
    if (path.startsWith('/api/me/providers/') || path.startsWith('/api/admin/providers/')) {
      if (path.endsWith('/check')) return ok({ valid: true, network_checked: false, model_called: false })
      const id = decodeURIComponent(path.split('/').pop())
      if (method === 'DELETE') { state.providers = state.providers.filter(p => p.id !== id); return ok({}) }
      if (method === 'PUT') {
        if (state.failSave) return fail('PROVIDER_UNSUPPORTED', 'This mock adapter is not implemented')
        const body = request.postDataJSON()
        const existing = state.providers.find(p => p.id === id)
        const next = { ...body, has_api_key: Boolean(body.api_key || existing?.has_api_key) }
        delete next.api_key
        state.providers = [...state.providers.filter(p => p.id !== id), next]
        return ok({})
      }
    }
    if (path === '/api/assistant/status') return ok({ enabled: false, model: '' })
    if (path === '/api/ai/capabilities') return ok({
      image: { enabled: true, default_model: 'mock-image-model', models: [{ id: 'mock-image-model', protocol: 'openai_images_edits' }] },
      text: { enabled: true, default_model: 'mock-text-model', models: [{ id: 'mock-text-model', protocol: 'openai_responses' }] },
      video: { enabled: true, default_model: 'mock-video-model', models: [{ id: 'mock-video-model', protocol: 'hfsy_video' }] }
    })
    if (path === '/api/projects/mock-project') return ok({ project: { id: 'mock-project', product_name: 'Mock preserved project', brand: 'Mock', country: 'US', language: 'en', status: 'draft', created_at: '2026-10-26T00:00:00Z' }, image_tasks: [], product_images: [], original_info: {}, generated_copy: {}, confirmed_facts: {} })
    if (path === '/api/auth/github/status') return ok({ enabled: false })
    if (path === '/api/billing/summary') return ok({ credits: 20, pending_orders: 0 })
    if (path === '/api/public/benefit-config') return ok({ enabled: false })
    if (path === '/api/messages' || path === '/api/projects' || path === '/api/videos/tasks') return ok([])
    if (path === '/api/videos/config') return ok({ enabled: true, mock_mode: true, default_model: 'mock-video-model', model_configs: [{ code: 'mock-video-model', name: 'Manual video model', provider: 'hfsy', model: 'vendor-video-model', enabled: true, default_resolution: '720p', default_duration_seconds: 8, duration_selectable: true, audio_toggle: true, max_reference_images: 3, aspect_ratios: ['16:9','9:16'], resolutions: ['720p'], durations: [8], variants: [{ resolution: '720p', duration_seconds: 8, price: 10, enabled: true }] }], aspect_ratios: ['16:9'], standard: {}, quality: {} })
    return fail('MOCK_NOT_IMPLEMENTED', 'No mock is configured for this workflow endpoint')
  })
  await page.goto('http://127.0.0.1:13280/workspace')
}
