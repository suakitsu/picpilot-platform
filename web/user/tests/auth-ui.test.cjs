const { test, afterEach } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const ts = require('typescript')
const originalWindow = global.window
const originalFetch = global.fetch

afterEach(() => { global.window = originalWindow; global.fetch = originalFetch })

function component(file, overrides = {}) {
  const effects = [], updates = []
  const react = {
    forwardRef: render => render,
    useRef: value => ({ current: value }),
    useEffect: effect => effects.push(effect),
    useImperativeHandle: (ref, create) => { if (ref) ref.current = create() },
    useState: value => [value, next => updates.push(next)]
  }
  const jsx = (type, props) => ({ type, props })
  const mocks = {
    react, 'react/jsx-runtime': { jsx, jsxs: jsx, Fragment: 'fragment' },
    'next/link': 'a', 'lucide-react': { RefreshCw: 'svg' },
    '@/components/i18n': { useI18n: () => key => key },
    ...overrides
  }
  const module = { exports: {} }
  const compiled = ts.transpileModule(fs.readFileSync(path.resolve(__dirname, '../src', file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX }
  }).outputText
  new Function('require', 'module', 'exports', compiled)(name => {
    if (!(name in mocks)) throw new Error('Unmocked component import: ' + name)
    return mocks[name]
  }, module, module.exports)
  function mountRefs(node) {
    if (!node || typeof node !== 'object') return
    if (node.props?.ref) node.props.ref.current = {}
    for (const child of [node.props?.children].flat()) mountRefs(child)
  }
  global.fetch = () => { throw new Error('Unexpected real HTTP request') }
  return { exports: module.exports, effects, updates, mountRefs }
}

const flush = () => new Promise(resolve => setImmediate(resolve))

test('Turnstile with no runtime key makes no script or verification request', () => {
  let rendered = 0
  global.window = { turnstile: { render() { rendered++ } } }
  const tokens = []
  const harness = component('components/TurnstileWidget.tsx', { '@/components/AppRuntime': { useRuntime: () => ({ config: { turnstile_site_key: '' } }) } })
  assert.equal(harness.exports.TurnstileWidget({ action: 'login', onToken: token => tokens.push(token) }, null), null)
  harness.effects.forEach(effect => effect())
  assert.equal(rendered, 0)
  assert.deepEqual(tokens, [''])
})

test('Turnstile uses runtime key and clears expired, failed and reset tokens', async () => {
  let options, removed = '', reset = ''
  global.window = { turnstile: {
    render(container, value) { assert.ok(container); options = value; return 'mock-widget' },
    remove(id) { removed = id }, reset(id) { reset = id }
  } }
  const tokens = [], handle = { current: null }
  const harness = component('components/TurnstileWidget.tsx', { '@/components/AppRuntime': { useRuntime: () => ({ config: { turnstile_site_key: 'mock-runtime-key' } }) } })
  const tree = harness.exports.TurnstileWidget({ action: 'login', onToken: token => tokens.push(token) }, handle)
  harness.mountRefs(tree)
  const cleanups = harness.effects.map(effect => effect())
  await flush()
  assert.equal(options.sitekey, 'mock-runtime-key')
  assert.equal(options.action, 'login')
  options.callback('mock-verification')
  options['expired-callback']()
  options['error-callback']()
  handle.current.reset()
  assert.deepEqual(tokens, ['', 'mock-verification', '', '', ''])
  assert.equal(reset, 'mock-widget')
  cleanups.forEach(cleanup => cleanup?.())
  assert.equal(removed, 'mock-widget')
})

function callbackHarness(search, response) {
  const redirects = [], histories = [], saved = []
  let exchanges = 0
  global.window = { location: { search, pathname: '/auth/github/callback', replace: url => redirects.push(url) }, history: { state: {}, replaceState: (...args) => histories.push(args[2]) } }
  const nav = { exports: {} }
  const compiled = ts.transpileModule(fs.readFileSync(path.resolve(__dirname, '../src/lib/navigation.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  new Function('module', 'exports', compiled)(nav, nav.exports)
  const harness = component('app/auth/github/callback/page.tsx', {
    '@/lib/navigation': nav.exports,
    '@/lib/api': {
      refreshSession: () => { exchanges++; return response() },
      saveSession: auth => saved.push(auth)
    }
  })
  harness.exports.default()
  return { ...harness, redirects, histories, saved, count: () => exchanges }
}

test('GitHub callback ignores URL credentials and deduplicates effect replay', async () => {
  const auth = { access_token: 'mock-cookie-token', user: { id: 'mock-owner' } }
  const harness = callbackHarness('?token=untrusted&user=untrusted&next=%2Fproviders', () => Promise.resolve(auth))
  const cleanup = harness.effects[0]()
  cleanup()
  harness.effects[0]()
  await flush()
  assert.equal(harness.count(), 1)
  assert.deepEqual(harness.saved, [auth])
  assert.deepEqual(harness.redirects, ['/providers'])
  assert.ok(harness.histories.every(url => url === '/auth/github/callback'))
})

test('GitHub callback uses safe fallback for malicious next', async () => {
  const harness = callbackHarness('?next=%2F%5Cexternal.test', () => Promise.resolve({ access_token: 'mock-cookie-token', user: { id: 'mock-owner' } }))
  harness.effects[0]()
  await flush()
  assert.deepEqual(harness.redirects, ['/workspace'])
})

test('GitHub callback session failure does not save or redirect', async () => {
  const harness = callbackHarness('?token=untrusted&user=untrusted', () => Promise.reject(new Error('Cookie session expired')))
  harness.effects[0]()
  await flush()
  assert.deepEqual(harness.saved, [])
  assert.deepEqual(harness.redirects, [])
  assert.ok(harness.updates.includes('Cookie session expired'))
})
