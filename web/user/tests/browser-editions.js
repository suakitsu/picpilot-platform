async page => {
  const state = page.__picpilotMocks
  const forbidden = r => /^\/api\/(billing|admin|benefits|messages)(\/|$)/.test(r.path)
  if (state.requests.some(forbidden)) throw new Error('Personal edition fetched a platform endpoint')
  await page.getByRole('heading', { name: 'Personal edition', exact: true }).waitFor()
  await page.screenshot({ path: 'output/playwright/admin-personal.png', fullPage: true })
  state.needsSetup = true
  await page.reload()
  await page.waitForURL('**/setup')
  await page.getByRole('heading', { name: 'Set up PicPilot', exact: true }).waitFor()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: 'output/playwright/admin-setup-mobile.png', fullPage: true })
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Setup mobile overflow')
  state.needsSetup = false
  state.failConfig = true
  await page.goto('http://127.0.0.1:13280/providers')
  await page.getByText('Cannot load server configuration. Check the Go service and retry.', { exact: true }).waitFor()
  if (state.requests.some(forbidden)) throw new Error('Runtime failure was not fail-closed')
  state.failConfig = false
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await page.getByRole('heading', { name: 'Model providers', exact: true }).waitFor()
  await page.evaluate(() => { localStorage.setItem('openimage-language', 'ja-JP'); window.dispatchEvent(new Event('openimage:language-updated')) })
  await page.getByRole('heading', { name: 'モデルサービス', exact: true }).waitFor()
  await page.screenshot({ path: 'output/playwright/providers-ja-mobile.png', fullPage: true })
  await page.evaluate(() => { localStorage.setItem('openimage-language', 'zh-CN'); window.dispatchEvent(new Event('openimage:language-updated')) })
  await page.getByRole('heading', { name: '模型服务', exact: true }).waitFor()
  await page.screenshot({ path: 'output/playwright/providers-zh-mobile.png', fullPage: true })
  await page.goto('http://127.0.0.1:13280/register')
  await page.waitForURL('**/login')
  if (state.requests.some(forbidden)) throw new Error('Personal registration route leaked platform requests')
  const personalRequestCount = state.requests.length
  state.edition = 'platform'
  state.billing = true
  state.registration = true
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('http://127.0.0.1:13280/providers')
  await page.getByText('Platform model providers are configured by administrators in the admin app.', { exact: true }).waitFor()
  if (state.requests.slice(personalRequestCount).some(r => r.path.startsWith('/api/me/providers'))) throw new Error('Platform user called a personal provider endpoint')
  await page.getByRole('link', { name: 'Top up / redeem', exact: true }).waitFor()
  if (!state.requests.slice(personalRequestCount).some(r => r.path === '/api/billing/summary')) throw new Error('Platform billing no longer loads')
  await page.goto('http://127.0.0.1:13281/login')
  await page.getByRole('textbox', { name: 'Password', exact: true }).waitFor()
  await page.screenshot({ path: 'output/playwright/admin-platform-login.png', fullPage: true })
  await page.goto('http://127.0.0.1:13281/providers')
  await page.getByRole('heading', { name: 'Model providers', exact: true }).waitFor()
  await page.getByRole('button', { name: 'Edit provider My text provider', exact: true }).click()
  await page.getByRole('textbox', { name: 'Model name', exact: true }).fill('platform/manual-model')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await page.getByText('platform/manual-model', { exact: true }).waitFor()
  if (!state.requests.some(r => r.path === '/api/admin/providers/mock-text' && r.method === 'PUT')) throw new Error('Admin provider did not use platform scope')
  await page.getByRole('button', { name: 'Validate configuration My text provider', exact: true }).click()
  await page.getByText('Configuration validated. This does not verify connectivity or model availability; no generation request was made.', { exact: true }).waitFor()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: 'output/playwright/admin-providers-mobile.png', fullPage: true })
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Admin provider mobile overflow')
  if (state.external.length) throw new Error('Unexpected external requests')
}
