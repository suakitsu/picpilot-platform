import { currentLanguage } from '@/components/i18n'

export const IMAGE_FILE_ACCEPT = 'image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp'
export const IMAGE_HEADER_READ_TIMEOUT_MS = 12_000

export type SupportedImageFormat = 'jpeg' | 'png' | 'webp' | 'heic'
export type ImageUploadErrorCode = 'UPLOAD_TOO_LARGE' | 'IMAGE_FORMAT_UNSUPPORTED' | 'IMAGE_CORRUPT' | 'IMAGE_READ_TIMEOUT'

export interface DetectedImageFile {
  file: File
  format: SupportedImageFormat
  contentType: string
  extension: string
  needsServerConversion: boolean
}

export class ImageUploadError extends Error {
  code: ImageUploadErrorCode

  constructor(code: ImageUploadErrorCode) {
    super(imageUploadMessage(code))
    this.name = 'ImageUploadError'
    this.code = code
  }
}

function language() {
  const value = currentLanguage()
  return value === 'en-US' || value === 'ja-JP' ? value : 'zh-CN'
}

export function imageUploadMessage(code: ImageUploadErrorCode | 'PROCESSING') {
  const lang = language()
  const messages = {
    'zh-CN': {
      PROCESSING: '正在处理图片...',
      UPLOAD_TOO_LARGE: '图片文件过大，请选择更小的图片。',
      IMAGE_FORMAT_UNSUPPORTED: '该照片格式暂时无法解析，请选择 JPG、PNG 或 WebP 图片。HEIC 转换尚未提供。',
      IMAGE_CORRUPT: '图片文件已损坏或无法读取，请重新选择一张图片。',
      IMAGE_READ_TIMEOUT: '读取照片超时。若照片保存在 iCloud，请等待下载完成后重试。'
    },
    'en-US': {
      PROCESSING: 'Processing image...',
      UPLOAD_TOO_LARGE: 'The image is too large. Please choose a smaller image.',
      IMAGE_FORMAT_UNSUPPORTED: 'This photo format cannot be read. Please choose a JPG, PNG, or WebP image. HEIC conversion is not available yet.',
      IMAGE_CORRUPT: 'The image is damaged or unreadable. Please choose another image.',
      IMAGE_READ_TIMEOUT: 'Reading the photo timed out. If it is stored in iCloud, wait for it to download and try again.'
    },
    'ja-JP': {
      PROCESSING: '画像を処理しています...',
      UPLOAD_TOO_LARGE: '画像ファイルが大きすぎます。より小さい画像を選択してください。',
      IMAGE_FORMAT_UNSUPPORTED: 'この写真形式は読み取れません。JPG、PNG、WebP画像を選択してください。HEIC 変換はまだ利用できません。',
      IMAGE_CORRUPT: '画像が破損しているか読み取れません。別の画像を選択してください。',
      IMAGE_READ_TIMEOUT: '写真の読み込みがタイムアウトしました。iCloud に保存されている場合は、ダウンロード完了後に再試行してください。'
    }
  } as const
  return messages[lang][code]
}

export function imageUploadErrorMessage(error: unknown) {
  if (error instanceof ImageUploadError) return error.message
  return imageUploadMessage('IMAGE_FORMAT_UNSUPPORTED')
}

function filenameExtension(name: string) {
  const match = /\.([^.]+)$/.exec(name.trim().toLowerCase())
  return match?.[1] || ''
}

function ascii(bytes: Uint8Array, start: number, length: number) {
  return String.fromCharCode(...bytes.slice(start, start + length))
}

function isHeifHeader(bytes: Uint8Array) {
  if (bytes.length < 12 || ascii(bytes, 4, 4) !== 'ftyp') return false
  const brands = new Set(['heic', 'heix', 'hevc', 'hevx', 'heim', 'heis', 'mif1', 'msf1'])
  for (let offset = 8; offset + 4 <= bytes.length; offset += 4) {
    if (brands.has(ascii(bytes, offset, 4).toLowerCase())) return true
  }
  return false
}

function detectMagic(bytes: Uint8Array): SupportedImageFormat | null {
  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'jpeg'
  if (bytes.length >= 8 && bytes[0] === 0x89 && ascii(bytes, 1, 3) === 'PNG' && bytes[4] === 0x0d && bytes[5] === 0x0a && bytes[6] === 0x1a && bytes[7] === 0x0a) return 'png'
  if (bytes.length >= 12 && ascii(bytes, 0, 4) === 'RIFF' && ascii(bytes, 8, 4) === 'WEBP') return 'webp'
  if (isHeifHeader(bytes)) return 'heic'
  return null
}

function hintedSupportedImage(file: File) {
  const extension = filenameExtension(file.name)
  const type = (file.type || '').toLowerCase()
  return ['jpg', 'jpeg', 'png', 'webp', 'heic', 'heif'].includes(extension)
    || ['image/jpeg', 'image/png', 'image/webp', 'image/heic', 'image/heif'].includes(type)
}

function readFileHeader(file: File, timeoutMs = IMAGE_HEADER_READ_TIMEOUT_MS) {
  const blob = file.slice(0, 256)
  if (typeof FileReader === 'undefined') {
    return Promise.race([
      blob.arrayBuffer(),
      new Promise<ArrayBuffer>((_, reject) => {
        setTimeout(() => reject(new ImageUploadError('IMAGE_READ_TIMEOUT')), timeoutMs)
      })
    ])
  }
  return new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    let settled = false
    const finish = (callback: () => void) => {
      if (settled) return
      settled = true
      window.clearTimeout(timer)
      callback()
    }
    const timer = window.setTimeout(() => {
      if (settled) return
      settled = true
      reader.abort()
      reject(new ImageUploadError('IMAGE_READ_TIMEOUT'))
    }, timeoutMs)
    reader.onload = () => finish(() => {
      if (reader.result instanceof ArrayBuffer) resolve(reader.result)
      else reject(new ImageUploadError('IMAGE_CORRUPT'))
    })
    reader.onerror = () => finish(() => reject(new ImageUploadError('IMAGE_CORRUPT')))
    reader.onabort = () => {
      if (!settled) finish(() => reject(new ImageUploadError('IMAGE_CORRUPT')))
    }
    reader.readAsArrayBuffer(blob)
  })
}

export async function detectImageFile(file: File): Promise<DetectedImageFile> {
  let header: ArrayBuffer
  try {
    header = await readFileHeader(file)
  } catch (error) {
    if (error instanceof ImageUploadError) throw error
    throw new ImageUploadError('IMAGE_CORRUPT')
  }
  const bytes = new Uint8Array(header)
  const format = detectMagic(bytes)
  if (!format) {
    throw new ImageUploadError(hintedSupportedImage(file) ? 'IMAGE_CORRUPT' : 'IMAGE_FORMAT_UNSUPPORTED')
  }
  const metadata = {
    jpeg: { contentType: 'image/jpeg', extension: '.jpg', needsServerConversion: false },
    png: { contentType: 'image/png', extension: '.png', needsServerConversion: false },
    webp: { contentType: 'image/webp', extension: '.webp', needsServerConversion: false },
    heic: { contentType: 'image/heic', extension: '.heic', needsServerConversion: true }
  } as const
  return { file, format, ...metadata[format] }
}

export async function validateImageFile(file: File, maxBytes = 25 * 1024 * 1024) {
  if (!file || file.size <= 0) throw new ImageUploadError('IMAGE_CORRUPT')
  if (file.size > maxBytes) throw new ImageUploadError('UPLOAD_TOO_LARGE')
  return detectImageFile(file)
}

export async function validateImageFiles(files: File[], maxBytes = 25 * 1024 * 1024) {
  const detected: DetectedImageFile[] = []
  for (const file of files) detected.push(await validateImageFile(file, maxBytes))
  return detected
}
