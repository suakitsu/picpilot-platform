async page => {
  const state = page.__picpilotMocks
  state.siteKey = ''
  state.needsSetup = false
  const counts = []
  for (const edition of ['personal', 'platform']) {
    state.edition = edition
    state.billing = edition === 'platform'
    const start = state.requests.length
    await page.goto('http://127.0.0.1:13280/workflows')
    await page.getByRole('tab', { name: '自由画布', exact: true }).click()
    const frame = page.locator('iframe[title="PicPilot 无限画布"]')
    await frame.waitFor()
    await page.waitForFunction(() => document.querySelector('iframe[title="PicPilot 无限画布"]')?.src.includes('picpilotConfig='))
    const config = JSON.parse(new URL(await frame.getAttribute('src')).searchParams.get('picpilotConfig'))
    if (!config.image.enabled || config.image.model !== 'mock-image-model' || !config.text.enabled || config.text.model !== 'mock-text-model') throw new Error('Canvas did not use authenticated model capabilities')
    if (!config.video.enabled || config.video.models[0].code !== 'mock-video-model') throw new Error('Detailed video model configuration was lost')
    const requests = state.requests.slice(start)
    if (!requests.some(r => r.path === '/api/ai/capabilities') || !requests.some(r => r.path === '/api/videos/config')) throw new Error('Missing canvas discovery request')
    if (requests.some(r => /^\/api\/(me|admin)\/providers/.test(r.path))) throw new Error('Canvas discovery fetched provider credentials')
    if (edition === 'personal' && requests.some(r => /^\/api\/(billing|admin|benefits|messages)(\/|$)/.test(r.path))) throw new Error('Personal canvas called platform APIs')
    counts.push({ edition, capabilities: true, videoDetails: true })
  }
  state.edition = 'personal'
  state.billing = false
  await page.goto('http://127.0.0.1:13280/projects/mock-project')
  await page.getByRole('heading', { name: 'Mock preserved project', exact: true }).waitFor()
  await page.screenshot({ path: 'output/playwright/project-next16-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Project page mobile overflow')
  await page.screenshot({ path: 'output/playwright/project-next16-mobile.png', fullPage: true })
  // Only existing admin dashboard fields are mocked, to exercise the React 19 chart wrapper.
  await page.context().route('**/api/admin/dashboard*', async route => {
    state.requests.push({ path: '/api/admin/dashboard', method: route.request().method(), body: null })
    await route.fulfill({ json: { success: true, code: 'OK', message: '', request_id: 'mock-dashboard', data: {
      users: 8, projects: 12, image_tasks: 20, video_tasks: 10, total_tasks: 30,
      queued_image_tasks: 1, queued_video_tasks: 1, queued_tasks: 2,
      active_image_tasks: 2, active_video_tasks: 1, active_tasks: 3,
      failed_image_tasks: 1, failed_video_tasks: 0, failed_tasks: 1,
      credits: 100, video_credits_income: 40, video_refunded_credits: 0, pending_orders: 0,
      image_actual_cost: 4, video_actual_cost: 2, actual_cost: 6,
      trends: [1, 2, 3].map(day => ({ date: `2026-10-${20 + day}`, label: `10-${20 + day}`,
        recharge: 20 * day, usage: 4 * day, image_usage: 3 * day, video_usage: day,
        users: day, revenue: 10 * day, credits: 15 * day, failures: 1,
        image_failures: 1, video_failures: 0, cost: 2 * day, image_cost: day, video_cost: day }))
    } } })
  })
  state.edition = 'platform'
  state.billing = true
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('http://127.0.0.1:13281/')
  await page.getByRole('heading', { name: '运营总览', exact: true }).waitFor()
  await page.locator('.echarts-box canvas').first().waitFor()
  await page.waitForFunction(() => {
    const canvas = document.querySelector('.echarts-box canvas')
    if (!canvas) return false
    const data = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
    let colored = 0
    for (let i = 3; i < data.length; i += 4) if (data[i]) colored++
    const previous = window.__mockChartPaint
    const stable = previous?.colored === colored ? previous.stable + 1 : 0
    window.__mockChartPaint = { colored, stable }
    return colored > 12000 && stable >= 4
  }, null, { polling: 100 })
  const charts = await page.locator('.echarts-box canvas').evaluateAll(canvases => canvases.map(canvas => {
    const rgba = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
    let colored = 0
    for (let i = 3; i < rgba.length; i += 4) if (rgba[i]) colored++
    return { width: canvas.width, height: canvas.height, colored }
  }))
  if (charts.length < 4 || charts.some(chart => !chart.width || !chart.height || chart.colored < 100)) throw new Error('React 19 dashboard charts are blank')
  await page.screenshot({ path: 'output/playwright/admin-charts-next16-desktop.png', fullPage: true, animations: 'disabled' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForFunction(() => {
    const canvas = document.querySelector('.echarts-box canvas')
    return canvas && canvas.width < 390
  })
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Admin dashboard mobile overflow')
  await page.screenshot({ path: 'output/playwright/admin-charts-next16-mobile.png', fullPage: true, animations: 'disabled' })
  if (state.external.length || state.pageErrors.length) throw new Error(JSON.stringify({ external: state.external, pageErrors: state.pageErrors }))
  return { capabilities: counts, dynamicProjectRoute: 'passed', charts, externalRequests: state.external.length, pageErrors: state.pageErrors.length }
}
