import { serveCanvasAsset } from '@/lib/canvasStatic'

export const dynamic = 'force-dynamic'

export async function GET(_request: Request, context: { params: Promise<{ path?: string[] }> }) {
  const { path } = await context.params
  return serveCanvasAsset('nova', path)
}
