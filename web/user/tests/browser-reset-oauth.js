async page => {
  const state = page.__picpilotMocks
  const countResets = () => state.requests.filter(r => r.path === '/api/auth/password-reset').length
  const exchangeStart = state.requests.filter(r => r.path === '/api/auth/session').length
  const checkCard = async () => {
    const clipped = await page.locator('.auth-card').evaluate(card => {
      const bounds = card.getBoundingClientRect()
      return bounds.left < 0 || bounds.right > document.documentElement.clientWidth
    })
    if (clipped) throw new Error('Mobile auth card is clipped')
  }
  state.edition = 'personal'
  state.billing = false
  state.registration = false
  state.siteKey = 'mock-runtime-site-key'
  state.emailLogin = true
  state.failSession = false
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('http://127.0.0.1:13280/forgot-password')
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).waitFor()
  if (await page.locator('[data-runtime-site-key]').getAttribute('data-runtime-site-key') !== state.siteKey) throw new Error('User Turnstile did not use runtime configuration')
  await page.getByRole('textbox', { name: 'Email Send code', exact: true }).fill('owner@example.test')
  if (!await page.getByRole('button', { name: 'Send code', exact: true }).isDisabled()) throw new Error('Email code did not require verification')
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).click()
  await page.getByRole('button', { name: 'Send code', exact: true }).click()
  await page.getByRole('textbox', { name: 'Verification code', exact: true }).fill('123456')
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill('too-short')
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill('too-short')
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).click()
  const before = countResets()
  await page.getByRole('button', { name: 'Reset password', exact: true }).click()
  await page.locator('.form-error').filter({ hasText: '12' }).waitFor()
  if (countResets() !== before) throw new Error('Short reset password reached the API')
  const password = 'mock-reset-password-123'
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill(password)
  await page.getByRole('textbox', { name: 'Confirm new password', exact: true }).fill(password)
  state.failReset = true
  await page.getByRole('button', { name: 'Reset password', exact: true }).click()
  await page.getByText(/Mock reset code expired/).waitFor()
  if (!await page.evaluate(() => localStorage.getItem('openimage_token'))) throw new Error('Failed reset cleared the current session')
  await checkCard()
  await page.screenshot({ path: 'output/playwright/reset-error-mobile.png', fullPage: true, animations: 'disabled' })
  state.failReset = false
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).click()
  await page.getByRole('button', { name: 'Reset password', exact: true }).click()
  await page.waitForURL('**/login')
  if (await page.evaluate(() => localStorage.getItem('openimage_token'))) throw new Error('Successful reset retained the current session')
  const reset = JSON.parse(state.requests.filter(r => r.path === '/api/auth/password-reset').at(-1).body)
  if (reset.password !== password || reset.confirm_password !== password || reset.verification_code !== '123456' || reset.turnstile_token !== 'mock-captcha-token') throw new Error('Reset contract mismatch')
  if (await page.evaluate(password => JSON.stringify(localStorage).includes(password), password)) throw new Error('Reset persisted a password')
  const code = JSON.parse(state.requests.filter(r => r.path === '/api/auth/email-code').at(-1).body)
  if (code.purpose !== 'reset_password' || code.turnstile_token !== 'mock-captcha-token') throw new Error('Email-code contract mismatch')
  await page.context().addCookies([{ name: 'picpilot_session', value: 'mock-oauth-cookie', domain: '127.0.0.1', path: '/', httpOnly: true, sameSite: 'Lax', secure: false }])
  await page.evaluate(() => localStorage.setItem('openimage_token', 'stale-mock-bearer'))
  await page.goto('http://127.0.0.1:13280/auth/github/callback?next=%2Fproviders&token=untrusted-query-token&user=untrusted-query-user')
  await page.waitForURL('**/providers')
  await page.getByRole('heading', { name: 'Model providers', exact: true }).waitFor()
  if (await page.evaluate(() => localStorage.getItem('openimage_token')) !== 'mock-cookie-session-token') throw new Error('OAuth did not save the cookie exchange token')
  if (await page.evaluate(() => JSON.stringify(localStorage).includes('untrusted-query'))) throw new Error('OAuth accepted legacy query credentials')
  if (await page.evaluate(() => document.cookie.includes('picpilot_session'))) throw new Error('Mock session cookie is not HttpOnly')
  await page.goto('http://127.0.0.1:13280/auth/github/callback?next=%2F%2Fevil.example.test')
  await page.waitForURL('**/workspace')
  state.failSession = true
  await page.goto('http://127.0.0.1:13280/auth/github/callback?next=%2Fproviders&token=untrusted-query-token')
  await page.getByRole('alert').filter({ hasText: 'Mock cookie session expired' }).waitFor()
  if (new URL(page.url()).search) throw new Error('Callback did not remove credential-shaped query data')
  await checkCard()
  await page.screenshot({ path: 'output/playwright/oauth-expired-mobile.png', fullPage: true, animations: 'disabled' })
  state.failSession = false
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await page.waitForURL('**/providers')
  const exchanges = state.requests.filter(r => r.path === '/api/auth/session').slice(exchangeStart)
  if (exchanges.length !== 4 || exchanges.some(r => r.method !== 'POST' || r.hasAuthorization || !r.hasSessionCookie)) throw new Error('OAuth exchange was not cookie-only or ran more than once per attempt')
  state.emailLogin = false
  await page.goto('http://127.0.0.1:13280/forgot-password')
  await page.getByRole('button', { name: 'Reset password', exact: true }).waitFor()
  if (!await page.getByRole('button', { name: 'Reset password', exact: true }).isDisabled()) throw new Error('Disabled email auth allowed reset')
  state.emailLogin = true
  state.edition = 'platform'
  state.billing = true
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('http://127.0.0.1:13281/login?next=%2Fproviders')
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).waitFor()
  if (await page.locator('[data-runtime-site-key]').getAttribute('data-runtime-site-key') !== state.siteKey) throw new Error('Admin Turnstile did not use runtime configuration')
  await page.getByRole('textbox', { name: 'Admin account', exact: true }).fill('owner@example.test')
  await page.getByRole('textbox', { name: 'Password', exact: true }).fill('mock-admin-password-123')
  await page.getByRole('button', { name: 'Complete mock verification', exact: true }).click()
  await page.screenshot({ path: 'output/playwright/admin-runtime-turnstile.png', fullPage: true })
  await page.locator('form').getByRole('button', { name: 'Sign in to admin', exact: true }).click()
  await page.waitForURL('**/providers')
  const adminLogin = JSON.parse(state.requests.filter(r => r.path === '/api/auth/admin/login').at(-1).body)
  if (adminLogin.turnstile_token !== 'mock-captcha-token') throw new Error('Admin login omitted runtime verification')
  if (state.external.length || state.pageErrors.length) throw new Error(JSON.stringify({ external: state.external, pageErrors: state.pageErrors }))
  return { reset: 'passed', oauth: 'passed', runtimeTurnstile: 'passed', cookieExchanges: exchanges.length, externalRequests: state.external.length, pageErrors: state.pageErrors.length }
}
