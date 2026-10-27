const DEFAULT_PREVIEW_WIDTH = 640
const DEFAULT_PREVIEW_TIMEOUT_MS = 12_000

function bitmapWithTimeout(file: File, width: number, timeoutMs: number) {
  return new Promise<ImageBitmap>((resolve, reject) => {
    let settled = false
    const timer = window.setTimeout(() => {
      settled = true
      reject(new Error('Image preview timed out'))
    }, timeoutMs)
    createImageBitmap(file, {
      imageOrientation: 'from-image',
      resizeWidth: width,
      resizeQuality: 'medium'
    }).then(bitmap => {
      if (settled) {
        bitmap.close()
        return
      }
      settled = true
      window.clearTimeout(timer)
      resolve(bitmap)
    }).catch(error => {
      if (settled) return
      settled = true
      window.clearTimeout(timer)
      reject(error)
    })
  })
}

function canvasBlob(canvas: HTMLCanvasElement) {
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      blob => blob ? resolve(blob) : reject(new Error('Image preview encoding failed')),
      'image/jpeg',
      0.82
    )
  })
}

async function bitmapBlob(bitmap: ImageBitmap) {
  if (typeof OffscreenCanvas !== 'undefined') {
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height)
    const context = canvas.getContext('2d')
    if (!context) throw new Error('Image preview canvas unavailable')
    context.fillStyle = '#fff'
    context.fillRect(0, 0, bitmap.width, bitmap.height)
    context.drawImage(bitmap, 0, 0)
    return canvas.convertToBlob({ type: 'image/jpeg', quality: 0.82 })
  }
  const canvas = document.createElement('canvas')
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  const context = canvas.getContext('2d')
  if (!context) throw new Error('Image preview canvas unavailable')
  context.fillStyle = '#fff'
  context.fillRect(0, 0, bitmap.width, bitmap.height)
  context.drawImage(bitmap, 0, 0)
  return canvasBlob(canvas)
}

export async function createImagePreviewUrl(
  file: File,
  width = DEFAULT_PREVIEW_WIDTH,
  timeoutMs = DEFAULT_PREVIEW_TIMEOUT_MS
) {
  if (typeof createImageBitmap !== 'function') return ''
  let bitmap: ImageBitmap | null = null
  try {
    bitmap = await bitmapWithTimeout(file, width, timeoutMs)
    const preview = await bitmapBlob(bitmap)
    return URL.createObjectURL(preview)
  } catch {
    return ''
  } finally {
    bitmap?.close()
  }
}

export function revokeImagePreviewUrls(urls: string[]) {
  urls.filter(url => url.startsWith('blob:')).forEach(url => URL.revokeObjectURL(url))
}
