async page => {
  const state = page.__picpilotMocks
  for (const app of ['admin', 'user']) {
    const port = app === 'admin' ? '13281' : '13280'
    state.edition = app === 'admin' ? 'platform' : 'personal'
    state.billing = app === 'admin'
    state.registration = app === 'admin'
    await page.goto('http://127.0.0.1:' + port + '/profile')
    await page.getByRole('textbox', { name: 'Current password', exact: true }).fill('mock-old-password')
    await page.getByRole('textbox', { name: /^New password/ }).fill('short')
    await page.getByRole('textbox', { name: 'Confirm password', exact: true }).fill('short')
    const before = state.requests.filter(r => r.path === '/api/auth/change-password').length
    await page.getByRole('button', { name: 'Change password', exact: true }).click()
    await page.locator('.password-settings [role="alert"]').waitFor()
    if (state.requests.filter(r => r.path === '/api/auth/change-password').length !== before) throw new Error('Invalid new password submitted')
    await page.getByRole('textbox', { name: /^New password/ }).fill('mock-long-new-password')
    await page.getByRole('textbox', { name: 'Confirm password', exact: true }).fill('mock-mismatch-password')
    await page.getByRole('button', { name: 'Change password', exact: true }).click()
    await page.getByText('Passwords do not match', { exact: true }).waitFor()
    await page.getByRole('textbox', { name: 'Confirm password', exact: true }).fill('mock-long-new-password')
    state.failPassword = true
    await page.getByRole('button', { name: 'Change password', exact: true }).click()
    await page.getByText(/Current password is incorrect \[PASSWORD_INVALID\]/).waitFor()
    await page.screenshot({ path: 'output/playwright/' + app + '-password-mobile.png', fullPage: true, animations: 'disabled' })
    state.failPassword = false
    await page.getByRole('button', { name: 'Change password', exact: true }).click()
    await page.waitForURL('**/login?password_changed=1')
    await page.getByText('Password updated. Sign in again.', { exact: true }).waitFor()
    const tokenKey = app === 'admin' ? 'openimage_admin_token' : 'openimage_token'
    if (await page.evaluate(key => localStorage.getItem(key), tokenKey)) throw new Error('Revoked token remained')
    if (await page.evaluate(() => JSON.stringify(localStorage).includes('mock-long-new-password') || JSON.stringify(sessionStorage).includes('mock-long-new-password'))) throw new Error('Password persisted')
  }
  if (state.external.length) throw new Error('Unexpected external request')
}
