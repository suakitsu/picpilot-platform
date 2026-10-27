export type ImageSizeOption = {
  quality: string
  ratio: string
  value: string
}

export type OutputSizeOption = {
  value: string
  label: string
}

const IMAGE_SIZE_GROUPS = [
  { ratio: '1:1', oneK: '1024x1024', twoK: '2048x2048', fourK: '2880x2880' },
  { ratio: '5:4', oneK: '1040x832', twoK: '2080x1664', fourK: '3200x2560' },
  { ratio: '9:16', oneK: '720x1280', twoK: '1152x2048', fourK: '2160x3840' },
  { ratio: '16:9', oneK: '1280x720', twoK: '2048x1152', fourK: '3840x2160' },
  { ratio: '4:3', oneK: '1024x768', twoK: '2048x1536', fourK: '3264x2448' },
  { ratio: '3:2', oneK: '1008x672', twoK: '2016x1344', fourK: '3504x2336' },
  { ratio: '4:5', oneK: '832x1040', twoK: '1664x2080', fourK: '2560x3200' },
  { ratio: '3:4', oneK: '768x1024', twoK: '1536x2048', fourK: '2448x3264' },
  { ratio: '2:3', oneK: '672x1008', twoK: '1344x2016', fourK: '2336x3504' },
  { ratio: '21:9', oneK: '1344x576', twoK: '2016x864', fourK: '3696x1584' }
]

export const IMAGE_SIZE_OPTIONS: ImageSizeOption[] = IMAGE_SIZE_GROUPS.flatMap(item => [
  { quality: '1K', ratio: item.ratio, value: item.oneK },
  { quality: '2K', ratio: item.ratio, value: item.twoK },
  { quality: '4K', ratio: item.ratio, value: item.fourK }
])

export const APLUS_OUTPUT_SIZE_OPTIONS: OutputSizeOption[] = [
  '970x600',
  '300x300',
  '600x180',
  '300x400',
  '350x135',
  '135x135',
  '220x220',
  '970x300',
  '150x300'
].map(value => ({ value, label: outputSizeLabel(value) }))

const LEGACY_SIZE_MAP: Record<string, string> = {
  '970x600': '1280x720',
  '970x300': '1344x576',
  '300x300': '1024x1024',
  '1464x600': '1344x576'
}

const LEGACY_APLUS_OUTPUT_SIZE_MAP: Record<string, string> = {
  '1280x720': '970x600',
  '1008x672': '970x600',
  '1024x1024': '300x300',
  '768x1024': '300x400',
  '720x1280': '150x300',
  '1344x576': '970x300',
  '1464x600': '970x300'
}

export function normalizeImageSize(value: string | undefined, fallback = '1024x1024') {
  const trimmed = (value?.trim() || '').replace('×', 'x')
  if (IMAGE_SIZE_OPTIONS.some(option => option.value === trimmed)) return trimmed
  if (LEGACY_SIZE_MAP[trimmed]) return LEGACY_SIZE_MAP[trimmed]
  return IMAGE_SIZE_OPTIONS.some(option => option.value === fallback) ? fallback : '1024x1024'
}

export function normalizeAplusOutputSize(value: string | undefined, fallback = '970x600') {
  const trimmed = (value?.trim() || '').replace('×', 'x')
  if (APLUS_OUTPUT_SIZE_OPTIONS.some(option => option.value === trimmed)) return trimmed
  if (LEGACY_APLUS_OUTPUT_SIZE_MAP[trimmed]) return LEGACY_APLUS_OUTPUT_SIZE_MAP[trimmed]
  return APLUS_OUTPUT_SIZE_OPTIONS.some(option => option.value === fallback) ? fallback : '970x600'
}

export function imageSizeLabel(value: string) {
  const option = IMAGE_SIZE_OPTIONS.find(item => item.value === value)
  return option ? `${option.quality} ${option.ratio} ${option.value}` : value
}

export function outputSizeLabel(value: string) {
  return value.replace('x', '\u00d7')
}

export function imageCreditsForSize(value: string) {
  const [width, height] = value.replace('×', 'x').split('x').map(item => Number(item))
  const maxEdge = Math.max(Number.isFinite(width) ? width : 0, Number.isFinite(height) ? height : 0)
  if (maxEdge >= 2800) return 30
  if (maxEdge >= 1800) return 24
  return 6
}
