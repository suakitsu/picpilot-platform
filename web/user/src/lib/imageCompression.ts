import { detectImageFile } from '@/lib/imageUpload'

export const MAX_UPLOAD_IMAGE_BYTES = 25 * 1024 * 1024
export const TARGET_UPLOAD_IMAGE_BYTES = Math.floor(4.5 * 1024 * 1024)
export const MAX_UPLOAD_IMAGE_EDGE = 2048

export interface CompressedImageResult {
  file: File
  compressed: boolean
  originalSize: number
}

function compressedFileName(name: string) {
  const base = name.replace(/\.[^.]+$/, '') || 'reference'
  return `${base}.jpg`
}

function imageFromFile(file: File) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const image = new Image()
    image.onload = () => {
      URL.revokeObjectURL(url)
      resolve(image)
    }
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('Image decode failed'))
    }
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement, quality: number) {
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('Image compression failed')), 'image/jpeg', quality)
  })
}

export async function compressImageForUpload(file: File): Promise<CompressedImageResult> {
  const detected = await detectImageFile(file)
  if (detected.needsServerConversion) {
    return { file, compressed: false, originalSize: file.size }
  }
  const image = await imageFromFile(file)
  const sourceWidth = image.naturalWidth || image.width
  const sourceHeight = image.naturalHeight || image.height
  const sourceEdge = Math.max(sourceWidth, sourceHeight)

  if (file.size <= TARGET_UPLOAD_IMAGE_BYTES && sourceEdge <= MAX_UPLOAD_IMAGE_EDGE) {
    return { file, compressed: false, originalSize: file.size }
  }

  let edge = Math.min(MAX_UPLOAD_IMAGE_EDGE, sourceEdge)
  let best: Blob | null = null
  while (edge >= 960) {
    const scale = Math.min(1, edge / sourceEdge)
    const width = Math.max(1, Math.round(sourceWidth * scale))
    const height = Math.max(1, Math.round(sourceHeight * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) break
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, width, height)
    ctx.drawImage(image, 0, 0, width, height)
    for (const quality of [0.92, 0.82, 0.72, 0.62, 0.54]) {
      const blob = await canvasToBlob(canvas, quality)
      if (!best || blob.size < best.size) best = blob
      if (blob.size <= TARGET_UPLOAD_IMAGE_BYTES) {
        return {
          file: new File([blob], compressedFileName(file.name), { type: 'image/jpeg', lastModified: Date.now() }),
          compressed: true,
          originalSize: file.size
        }
      }
    }
    edge = Math.floor(edge * 0.75)
  }

  if (best && best.size < file.size) {
    return {
      file: new File([best], compressedFileName(file.name), { type: 'image/jpeg', lastModified: Date.now() }),
      compressed: true,
      originalSize: file.size
    }
  }
  return { file, compressed: false, originalSize: file.size }
}
