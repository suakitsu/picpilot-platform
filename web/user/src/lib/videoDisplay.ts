const MODEL_LABELS: Record<string, string> = {
  'gemini-omni-flash-preview': 'Gemini Omni',
  'agnes-video-v2.0': 'Agnes V2.0',
  'doubao-seedance-2.0-mini': 'Seedance Mini',
  'doubao-seedance-2.0-fast': 'Seedance 2.0 Fast',
  'doubao-seedance-2.0': 'Seedance 2.0',
  'sd-2.0-fast-480': 'Seedance 2.0 Fast 480p',
  'sd-2.0-fast': 'Seedance 2.0 Fast',
  'sd-2.0-480': 'Seedance 2.0 480p',
  'sd-2.0': 'Seedance 2.0',
}

export function videoModelLabel(model: string, provider = '') {
  const normalized = model?.trim()
  if (normalized && MODEL_LABELS[normalized]) return MODEL_LABELS[normalized]
  if (normalized) return normalized
  if (provider === 'gemini') return 'Gemini Omni'
  if (provider === 'seedance') return 'Seedance'
  if (provider === 'agnes') return 'Agnes V2.0'
  if (provider === 'hfsy') return 'Seedance'
  return '未记录'
}

export function videoSettlementLabel(refundedAt?: string) {
  if (!refundedAt) return ''
  return '已退款'
}
