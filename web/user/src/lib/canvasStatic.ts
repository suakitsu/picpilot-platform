import { readFile, stat } from 'node:fs/promises'
import path from 'node:path'

type CanvasKind = 'nova' | 'infinite'

const roots: Record<CanvasKind, string> = {
  nova: path.resolve(process.cwd(), '..', '..', 'third_party', 'nova-image-studio', 'frontend', 'out'),
  infinite: path.resolve(process.cwd(), '..', '..', 'third_party', 'infinite-canvas', 'web', 'dist')
}

const contentTypes: Record<string, string> = {
  '.css': 'text/css; charset=utf-8',
  '.gif': 'image/gif',
  '.html': 'text/html; charset=utf-8',
  '.ico': 'image/x-icon',
  '.jpeg': 'image/jpeg',
  '.jpg': 'image/jpeg',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.txt': 'text/plain; charset=utf-8',
  '.wasm': 'application/wasm',
  '.webp': 'image/webp',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2'
}

function safeTarget(root: string, segments: string[]) {
  if (segments.some(segment => !segment || segment === '.' || segment === '..' || segment.includes('/') || segment.includes('\\') || segment.includes('\0'))) return null
  const target = path.resolve(root, ...segments)
  const relative = path.relative(root, target)
  if (relative.startsWith('..') || path.isAbsolute(relative)) return null
  return target
}

export async function serveCanvasAsset(kind: CanvasKind, rawSegments: string[] | undefined) {
  const root = roots[kind]
  const segments = rawSegments || []
  let target = safeTarget(root, segments.length ? segments : ['index.html'])
  if (!target) return new Response('Not Found', { status: 404 })

  let info
  try {
    info = await stat(target)
  } catch {
    info = null
  }

  if (info?.isDirectory()) target = path.join(target, 'index.html')
  if (!info && kind === 'infinite' && !path.extname(target)) target = path.join(root, 'index.html')

  try {
    const fileInfo = await stat(target)
    if (!fileInfo.isFile()) return new Response('Not Found', { status: 404 })
    const body = new Uint8Array(await readFile(target))
    const extension = path.extname(target).toLowerCase()
    const isDocument = extension === '.html' || extension === '.txt'
    return new Response(body, {
      headers: {
        'Cache-Control': isDocument ? 'no-store' : 'public, max-age=31536000, immutable',
        'Content-Type': contentTypes[extension] || 'application/octet-stream',
        'X-Content-Type-Options': 'nosniff'
      }
    })
  } catch {
    return new Response('Not Found', { status: 404 })
  }
}
