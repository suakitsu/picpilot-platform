import fs from 'node:fs'
import path from 'node:path'
import vm from 'node:vm'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const ts = require('typescript')

const adminRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = path.resolve(adminRoot, '..')
const source = path.join(repoRoot, 'openImage-ts', 'src', 'lib', 'templateCatalog.ts')
const target = path.join(adminRoot, 'src', 'lib', 'templateCatalogStats.ts')

function defaultImageGroup(item) {
  if (item.imageGroup) return item.imageGroup
  if (item.kind === 'image' && item.routeType === 'creative') return 'creative'
  if (item.kind !== 'image') return 'domestic'
  return item.market
}

const sourceCode = fs.readFileSync(source, 'utf8')
const js = ts.transpileModule(sourceCode, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020
  }
}).outputText

const module = { exports: {} }
vm.runInNewContext(js, { module, exports: module.exports, require, console })

const items = module.exports.TEMPLATE_CATALOG || []
const stats = {
  source: 'openImage-ts/src/lib/templateCatalog.ts',
  total: items.length,
  items: [],
  image: { all: 0, amazon: 0, domestic: 0, creative: 0, system: 0 },
  prompt: { all: 0, copy: 0, image: 0, agent: 0, system: 0 }
}

for (const item of items) {
  const imageGroup = defaultImageGroup(item)
  stats.items.push({
    id: item.id,
    kind: item.kind,
    market: item.market,
    imageGroup,
    routeType: item.routeType,
    platform: item.platform,
    category: item.category,
    title: item.title,
    subtitle: item.subtitle,
    previewImage: item.previewImage || ''
  })
  if (item.kind === 'image') {
    stats.image.all += 1
    if (imageGroup === 'domestic') stats.image.domestic += 1
    else if (imageGroup === 'creative') stats.image.creative += 1
    else if (imageGroup === 'overseas') stats.image.amazon += 1
    else stats.image.system += 1
  }
  if (item.kind === 'copy') {
    stats.prompt.copy += 1
  }
}

stats.prompt.image = stats.image.all
stats.prompt.all = stats.prompt.copy + stats.prompt.image + stats.prompt.agent + stats.prompt.system

const output = `export const TEMPLATE_CATALOG_STATS = ${JSON.stringify(stats, null, 2)} as const\n`
fs.writeFileSync(target, output)
console.log(`Synced ${stats.total} templates to ${path.relative(repoRoot, target)}`)
