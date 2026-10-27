const fs = require('node:fs')
const path = require('node:path')
const ts = require('typescript')
const app = process.argv[2] || 'user'
if (!['user', 'admin'].includes(app)) throw new Error('Choose user or admin')
const root = path.resolve(__dirname, '..', '..', app)
const routes = []

function routeText(node, source) {
  if (ts.isStringLiteralLike(node)) return node.text
  if (ts.isTemplateExpression(node)) {
    let value = node.head.text
    for (const span of node.templateSpans) {
      const expression = span.expression.getText(source)
      const query = /[Qq]uery/.test(expression) || expression.includes('?')
      value += (query && !value.includes('?') ? '?{query}' : '{' + expression + '}') + span.literal.text
    }
    return value
  }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) return routeText(node.left, source) + routeText(node.right, source)
  return '{' + node.getText(source) + '}'
}

for (const file of ['src/lib/api.ts', 'src/lib/runtime.ts', 'src/lib/providers.ts']) {
  const source = ts.createSourceFile(file, fs.readFileSync(path.join(root, file), 'utf8'), ts.ScriptTarget.Latest, true)
  function visit(node, owner) {
    if (ts.isFunctionDeclaration(node)) owner = node
    if (ts.isCallExpression(node) && ['apiFetch', 'publicRequest'].includes(node.expression.getText(source))) {
      const first = node.arguments[0]
      if (first) {
        const route = routeText(first, source)
        if (route.startsWith('/api/')) {
          const options = node.arguments[1]
          const properties = options && ts.isObjectLiteralExpression(options) ? options.properties : []
          const property = name => properties.find(p => p.name?.getText(source) === name)?.initializer
          const method = property('method')?.text || 'GET'
          const body = property('body')
          const bodyExpression = body?.getText(source)
          let fields = []
          if (body && ts.isCallExpression(body) && body.expression.getText(source) === 'JSON.stringify' && body.arguments[0] && ts.isObjectLiteralExpression(body.arguments[0])) {
            fields = body.arguments[0].properties.map(p => p.name?.getText(source)).filter(Boolean)
          }
          const formFields = new Set()
          function formVisit(n) {
            if (ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression) && n.expression.name.text === 'append' && n.arguments[0] && ts.isStringLiteral(n.arguments[0])) formFields.add(n.arguments[0].text)
            ts.forEachChild(n, formVisit)
          }
          if (owner && bodyExpression === 'form') formVisit(owner)
          routes.push({
            method, route,
            function: owner?.name?.getText(source) || '(module)',
            source: file + ':' + (source.getLineAndCharacterOfPosition(node.getStart()).line + 1),
            response_type: node.typeArguments?.[0]?.getText(source) || 'unknown',
            ...(bodyExpression ? { body_expression: bodyExpression.length > 180 ? bodyExpression.slice(0, 177) + '...' : bodyExpression } : {}),
            ...(fields.length ? { json_fields: fields } : {}),
            ...(formFields.size ? { multipart_fields: [...formFields] } : {})
          })
        }
      }
    }
    ts.forEachChild(node, child => visit(child, owner))
  }
  visit(source)
}
const serverRoot = path.resolve(root, '../../core/server')
const declared = [...new Set(fs.readdirSync(serverRoot).filter(file => file.endsWith('.go') && !file.endsWith('_test.go')).flatMap(file => {
  const source = fs.readFileSync(path.join(serverRoot, file), 'utf8')
  return [
    ...[...source.matchAll(/Handle(?:Func)?\("([A-Z]+) ([^"]+)"/g)].map(m => m[1] + ' ' + m[2]),
    ...[...source.matchAll(/"((?:GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD) \/api\/[^"]+)"\s*:/g)].map(m => m[1])
  ]
}))].sort()
const normalize = route => route.split('?')[0].replace(/\{[^{}]*\}/g, '{id}')
const declaredSet = new Set(declared.map(entry => normalize(entry)))
for (const route of routes) route.core_declaration_found = declaredSet.has(normalize(route.method + ' ' + route.route))
const result = {
  app, source_snapshot_date: '2026-10-26', source_snapshot_utc: new Date().toISOString(),
  notes: [
    'Source inventory only. No backend calls or paid requests were made. Parent implementation may advance after this snapshot.',
    'All browser requests are same-origin. Next /api/[...path] is a transport proxy, not a business backend.',
    'Path expressions preserve variable names. Query helpers and body helper functions remain documented at the referenced source.',
    'core_declaration_found scans direct HandleFunc/Handle literals and route-map keys in non-test core/server Go files. It does not verify the registration call graph.',
    'A declaration is not proof of a live route, feature parity, adapter availability, or a successful integration test.'
  ],
  next_server_handlers: [{ route: '/api/[...path]', methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS', 'HEAD'], role: 'Streaming same-origin Go proxy; runtime GO_API_INTERNAL_URL' }],
  supplemental_routes: app === 'user' ? [
    { method: 'GET', route: '/api/auth/github', purpose: 'Legacy OAuth start navigation; Go must own state, exchange and redirects' },
    { method: 'GET', route: '/api/auth/github/callback', purpose: 'Legacy Next redirect shim removed; now forwarded to Go' },
    { method: 'GET', route: '/api/files/assets/{id}', purpose: 'Server-returned signed asset URLs; images and downloads' }
  ] : [{ method: 'GET', route: '/api/files/assets/{id}', purpose: 'Server-returned signed asset URLs' }],
  routes,
  core_declared_routes_at_snapshot: declared
}
const json = JSON.stringify(result, null, 2) + '\n'
if (process.argv.includes('--patch')) {
  process.stdout.write('*** Begin Patch\n*** Add File: ' + path.join(root, 'API-INVENTORY.json').replaceAll('\\', '/') + '\n' + json.trimEnd().split('\n').map(line => '+' + line).join('\n') + '\n*** End Patch')
} else process.stdout.write(json)
