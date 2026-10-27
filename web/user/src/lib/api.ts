import { assertApiAllowed, hasBilling, loadRuntime, publicRequest } from './runtime'

export interface ApiResponse<T> { success: boolean; code: string; message: string; data: T; request_id: string }
export async function getOptionalBillingSummary(): Promise<BillingSummary | null> {
  const { config } = await loadRuntime()
  return hasBilling(config) ? getBillingSummary() : null
}
export interface UserProfile { id: string; email_masked: string; email_display?: string; nickname: string; role: string; credits: number; ui_language: string; theme: string; avatar_url?: string }
export interface AuthResponse { user: UserProfile; access_token: string }
export interface AiCapability { enabled: boolean; default_model: string; models: Array<{ id: string; protocol: string }> }
export type AiCapabilities = Record<'image' | 'text' | 'video', AiCapability>

export async function getAiCapabilities(): Promise<AiCapabilities> {
  const data = await apiFetch<AiCapabilities>('/api/ai/capabilities')
  const result = {} as AiCapabilities
  for (const capability of ['image', 'text', 'video'] as const) {
    const value = data?.[capability]
    if (!value || typeof value.enabled !== 'boolean' || typeof value.default_model !== 'string' || !Array.isArray(value.models)
      || value.models.some(model => !model || typeof model.id !== 'string' || !model.id || typeof model.protocol !== 'string' || !model.protocol)
      || (value.enabled && !value.models.some(model => model.id === value.default_model))) {
      throw new ApiError('Invalid model capability response', 'INVALID_CAPABILITIES')
    }
    result[capability] = { enabled: value.enabled, default_model: value.default_model, models: value.models.map(model => ({ id: model.id, protocol: model.protocol })) }
  }
  return result
}
export interface EmailCodeSendResult { sent: boolean; email_masked: string; purpose: string; expires_in_seconds: number; cooldown_seconds: number }
export interface ProjectSummary { id: string; brand: string; product_name: string; country: string; language: string; status: string; success_images: number; failed_images: number; running_images: number; total_images: number; credits_cost: number; latest_failure_reason?: string; latest_failed_task_id?: string; cover_url?: string; preview_urls?: string[]; created_at: string; updated_at: string; source_type?: string; source_chat_id?: string; version_count?: number }
export interface ProductImageView { id: string; image_type: string; original_filename: string; url: string; content_type: string; image_format?: string; file_size: number; width: number; height: number; created_at: string }
export interface ProjectDetail { project: ProjectSummary; original_info: Record<string, unknown>; generated_copy: Record<string, any>; confirmed_facts: Record<string, any>; product_images: ProductImageView[]; image_tasks: ImageTaskView[] }
export interface GenerateCopyResponse { project_id: string; status: string; generated_copy: { title?: string; bullets?: string[]; description?: string; confirmed_facts_candidate?: Record<string, any>; compliance?: { passed?: boolean; warnings?: string[]; blocked_claims?: string[] }; image_briefs?: Array<{ image_type: string; image_name: string; goal: string; overlay_text?: string; required_facts?: string; credits_cost: number }> } }
export interface ImageTaskView { id: string; image_type: string; image_name: string; status: string; prompt_snapshot: string; result_url: string; credits_cost: number; provider: string; model: string; safe_error: string; created_at: string }
export interface ListingPackageResponse { image_tasks: ImageTaskView[]; aplus_project: ProjectDetail | null; aplus_requested: boolean; aplus_status: string }
export interface CreativeChatSummary { id: string; title: string; style: string; size: string; language: string; reference_url: string; latest_status: string; latest_result_url: string; version_count: number; created_at: string; updated_at: string }
export interface CreativeChatMessageView { id: string; chat_id: string; role: string; content: string; project_id?: string; task_id?: string; result_url?: string; status?: string; version_no?: number; created_at: string }
export interface CreativeChatVersionView { id: string; chat_id: string; version_no: number; user_message_id?: string; assistant_message_id?: string; project_id?: string; task_id?: string; user_request: string; final_prompt: string; style: string; size: string; reference_url?: string; reference_urls?: string[]; status: string; result_url?: string; safe_error?: string; created_at: string; updated_at: string }
export interface CreativeChatView { chat: CreativeChatSummary; messages: CreativeChatMessageView[]; versions: CreativeChatVersionView[] }
export interface CreativeChatGenerateResponse { chat: CreativeChatView; project?: ProjectDetail | null; final_prompt: string; version_no: number }
export interface CreativeChatEstimateResponse { size: string; quality_label: string; credits_cost: number }
export interface RechargePlan { id: string; name: string; credits: number; price_cents: number; currency: string; enabled: boolean; sort_order: number }
export interface PaymentChannel { id: string; name: string; code: string; visible: boolean; enabled: boolean; sort_order: number; config: string }
export interface RechargeOrder { id: string; user_id: string; plan_id: string; channel_code: string; amount_cents: number; credits: number; currency: string; status: string; remark: string; created_at: string }
export interface PaymentLink { label: string; url: string }
export interface CreateRechargeOrderResponse { order_id: string; status: string; message: string; payment_url?: string; payment_links?: PaymentLink[]; payment_hint?: string }
export interface SyncRechargeOrderResponse { order_id: string; status: string; message?: string; credits?: number; balance_after?: number }
export interface CreditLedger { id: string; user_id: string; change_amount: number; balance_after: number; biz_type: string; biz_id: string; remark: string; created_at: string }
export interface BillingSummary { credits: number; pending_orders: number }
export interface RedeemCodeResponse { credits: number; balance_after: number; record_id: string }
export interface StationMessage { id: string; title: string; content: string; published_at: string; expires_at?: string; read: boolean }
export interface BenefitConfig { enabled: boolean; title: string; description: string; qq_group: string; qq_link?: string; wechat_group: string; qq_qr_url: string; wechat_qr_url: string; redeem_tutorial: string; free_highlight_label: string }
export interface AssistantStatus { enabled: boolean; model: string; models?: string[] }
export interface AssistantChatMessage { role: 'user' | 'assistant'; content: string }
export interface AssistantChatResponse { reply: string; intent: string; action: string; action_label: string; action_href: string; suggested_prompt: string; confidence: number; model: string }
export interface DomesticSellingPointSuggestionResponse { selling_points: string[]; text: string; source: string }
export interface CommercePlatformPreset {
  code: string
  name: string
  description: string
  enabled: boolean
  beta: boolean
  default_resolution: string
  default_size: string
  default_slots: string[]
  style_prompt: string
  language: string
  overlay_language: string
}
export interface CommerceImageSlot {
  code: string
  name: string
  description: string
  category: string
  default_size: string
  sort_order: number
}
export interface CommerceTemplatePreset {
  id: string
  platform: string
  slot: string
  name: string
  description: string
  prompt_template: string
  negative_prompt: string
  overlay_template: Record<string, any>
  default_size: string
  enabled: boolean
  sort_order: number
  provider_params: Record<string, any>
}
export interface CommerceCatalog {
  platforms: CommercePlatformPreset[]
  slots: CommerceImageSlot[]
  templates: CommerceTemplatePreset[]
  cost_rule: Record<string, number>
  updated_at: string
}
export interface CommerceProductInfo {
  product_name: string
  category: string
  brand: string
  selling_points: string[]
  material: string
  specs: string
  audience: string
  scenario: string
  price_text: string
  promo_text: string
  country: string
  language: string
}
export interface CommerceEstimateResponse {
  platform: string
  resolution: string
  image_count: number
  credits_cost: number
  slot_costs: Array<{ slot: string; name: string; size: string; credits_cost: number }>
}
export interface CommerceGenerateResponse {
  project_id: string
  project: ProjectDetail
  created_tasks: ImageTaskView[]
  credits_cost: number
  mock_mode: boolean
}
export interface ImageTranslationLanguage {
  code: string
  name: string
  native_name: string
  prompt_name: string
}
export interface ImageTranslationGenerateResponse {
  project_id: string
  project: ProjectDetail
  created_tasks: ImageTaskView[]
  target_language: ImageTranslationLanguage
  resolution: string
  credits_cost: number
}
export interface VideoTierConfig {
  enabled: boolean
  duration_seconds: number
  resolution: string
  price: number
}
export interface VideoVariantConfig {
  resolution: string
  duration_seconds: number
  price: number
  enabled: boolean
}
export interface VideoModelConfig {
  code: string
  name: string
  description: string
  provider: string
  model: string
  enabled: boolean
  default_resolution: string
  default_duration_seconds: number
  duration_selectable: boolean
  audio_toggle: boolean
  max_reference_images: number
  aspect_ratios: string[]
  resolutions: string[]
  durations: number[]
  variants: VideoVariantConfig[]
}
export interface VideoPublicConfig {
  enabled: boolean
  mock_mode: boolean
  aspect_ratios: string[]
  standard: VideoTierConfig
  quality: VideoTierConfig
  default_model: string
  model_configs: VideoModelConfig[]
}
export interface VideoScriptPreview {
  optimized_prompt: string
  mode: 'single_shot' | 'storyboard'
  source: 'ai' | 'fallback'
  summary: string
}
export interface VideoTaskView {
  id: string
  title: string
  prompt: string
  source_image_urls: string[]
  aspect_ratio: string
  duration_seconds: number
  requested_resolution: string
  mode: string
  status: string
  progress: number
  provider: string
  model: string
  routing_reason?: string
  queue_ahead?: number
  upstream_task_id?: string
  result_url?: string
  credits_cost: number
  metadata?: {
    mock_mode?: boolean
    selected_model_code?: string
    listed_price?: number
    generate_audio?: boolean
  }
  safe_error?: string
  refunded_at?: string
  queued_at?: string
  submitted_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}
export interface PageResult<T> { rows: T[]; total: number; page: number; page_size: number; total_pages: number }
export interface BillingPageQuery { page?: number; pageSize?: number; status?: string }

export class ApiError extends Error {
  code: string
  requestId: string

  constructor(message: string, code = 'REQUEST_FAILED', requestId = '') {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.requestId = requestId
  }
}

export const API_BASE_URL = ''
const TOKEN_KEY = 'openimage_token'
const USER_KEY = 'openimage_user'
const DEFAULT_API_TIMEOUT_MS = 130_000

export function getToken() { if (typeof window === 'undefined') return ''; return window.localStorage.getItem(TOKEN_KEY) || '' }
export function getStoredUser(): UserProfile | null { if (typeof window === 'undefined') return null; const raw = window.localStorage.getItem(USER_KEY); if (!raw) return null; try { return JSON.parse(raw) as UserProfile } catch { return null } }
export function saveSession(auth: AuthResponse) {
  window.localStorage.setItem(TOKEN_KEY, auth.access_token)
  saveStoredUser(auth.user)
}
export function saveStoredUser(user: UserProfile) {
  window.localStorage.setItem(USER_KEY, JSON.stringify(user))
  if (user.ui_language) {
    window.localStorage.setItem('openimage-language', user.ui_language)
    window.dispatchEvent(new Event('openimage:language-updated'))
  }
  window.dispatchEvent(new Event('openimage:user-updated'))
}
export function clearSession() {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(TOKEN_KEY)
  window.localStorage.removeItem(USER_KEY)
  window.dispatchEvent(new Event('openimage:user-updated'))
  window.dispatchEvent(new Event('openimage:auth-expired'))
}

function localizedAuthMessage() {
  if (typeof window === 'undefined') return 'Sign in required'
  const lang = window.localStorage.getItem('openimage-language') || getStoredUser()?.ui_language || 'zh-CN'
  if (lang === 'en-US') return 'Sign in required'
  if (lang === 'ja-JP') return 'ログインが必要です'
  return '请先登录'
}

function localizedApiErrorMessage(code: string, message: string) {
  if (typeof window === 'undefined') return message || 'Request failed'
  const lang = window.localStorage.getItem('openimage-language') || getStoredUser()?.ui_language || 'zh-CN'
  if (code === 'UPLOAD_TOO_LARGE') {
    if (lang === 'en-US') return 'The image exceeds the upload limit. Please compress it or use a smaller image.'
    if (lang === 'ja-JP') return '画像がアップロード上限を超えています。圧縮するか、より小さい画像を使用してください。'
    return '图片已超过上传限制，请压缩后上传或换一张更小的图片'
  }
  if (code === 'IMAGE_FORMAT_UNSUPPORTED') {
    if (lang === 'en-US') return 'This photo format cannot be read. Please choose a JPG, PNG, or WebP image. HEIC conversion is not available yet.'
    if (lang === 'ja-JP') return 'この写真形式は読み取れません。JPG、PNG、WebP画像を選択してください。HEIC 変換はまだ利用できません。'
    return '该照片格式暂时无法解析，请选择 JPG、PNG 或 WebP 图片。HEIC 转换尚未提供。'
  }
  if (code === 'IMAGE_CORRUPT') {
    if (lang === 'en-US') return 'The image is damaged or unreadable. Please choose another image.'
    if (lang === 'ja-JP') return '画像が破損しているか読み取れません。別の画像を選択してください。'
    return '图片文件已损坏或无法读取，请重新选择一张图片。'
  }
  if (code === 'IMAGE_PIXEL_LIMIT') {
    if (lang === 'en-US') return 'The image dimensions are too large. Please resize the image and try again.'
    if (lang === 'ja-JP') return '画像のピクセル数が大きすぎます。サイズを小さくして再試行してください。'
    return '图片像素过大，请缩小图片尺寸后再上传。'
  }
  if (code === 'INTERNAL_ERROR' && message === 'Service is temporarily unavailable') {
    if (lang === 'en-US') return 'Service is temporarily unavailable. Please try again later.'
    if (lang === 'ja-JP') return 'サービスが一時的に利用できません。しばらくしてから再試行してください。'
    return '服务暂时不可用，请稍后重试'
  }
  if (code === 'AI_WORKER_PROCESSING') {
    if (lang === 'en-US') return 'The local AI is still processing this request. Please wait a moment and try again.'
    if (lang === 'ja-JP') return 'ローカル AI がこのリクエストを処理中です。しばらく待ってから再試行してください。'
    return '本机 AI 仍在处理这次请求，请稍等片刻后再试'
  }
  if (code === 'REQUEST_TIMEOUT' || code === '502' || code === '503' || code === '504') {
    if (lang === 'en-US') return 'The request timed out and may still be processing. If you just submitted a generation task, check History before trying again.'
    if (lang === 'ja-JP') return 'リクエストがタイムアウトしましたが、処理が続いている可能性があります。生成を送信した場合は、再送信する前に履歴を確認してください。'
    return '请求超时，后台可能仍在处理；如果刚提交了生成任务，请先到历史记录确认，不要重复提交'
  }
  return message || 'Request failed'
}

function redirectToLogin() {
  if (typeof window === 'undefined') return
  const pathname = window.location.pathname
  if (pathname === '/login' || pathname === '/register') return
  const next = `${pathname}${window.location.search}`
  window.location.replace(`/login?next=${encodeURIComponent(next)}`)
}
export function assetUrl(url?: string) {
  if (!url) return ''
  if (url.startsWith('http://') || url.startsWith('https://')) return url
  return `${API_BASE_URL}${url}`
}
export function loginUrl(next?: string) { return next ? `/login?next=${encodeURIComponent(next)}` : '/login' }
export function requireLogin(next?: string) { if (typeof window === 'undefined') return false; if (getToken()) return true; window.location.replace(loginUrl(next || `${window.location.pathname}${window.location.search}`)); return false }

interface ApiFetchOptions extends RequestInit {
  preserveSessionOnUnauthorized?: boolean
  suppressAuthRedirect?: boolean
  timeoutMs?: number
}

export async function apiFetch<T>(path: string, init: ApiFetchOptions = {}) {
  await assertApiAllowed(path, init.body)
  const { preserveSessionOnUnauthorized = false, suppressAuthRedirect = false, timeoutMs = DEFAULT_API_TIMEOUT_MS, ...requestInit } = init
  const token = getToken()
  const headers: Record<string, string> = { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(requestInit.headers as Record<string, string> | undefined) }
  if (!(requestInit.body instanceof FormData)) headers['Content-Type'] = 'application/json'
  const controller = new AbortController()
  const originalSignal = requestInit.signal
  const abortFromOriginal = () => controller.abort()
  if (originalSignal?.aborted) controller.abort()
  else originalSignal?.addEventListener('abort', abortFromOriginal, { once: true })
  const timeout = setTimeout(() => controller.abort(), Math.max(1000, timeoutMs))
  let response: Response
  const method = String(requestInit.method || 'GET').toUpperCase()
  const mayRetryRead = method === 'GET' || method === 'HEAD'
  const request = () => fetch(`${API_BASE_URL}${path}`, { ...requestInit, headers, cache: 'no-store', signal: controller.signal })
  try {
    try {
      response = await request()
    } catch (error) {
      if (!mayRetryRead || controller.signal.aborted || originalSignal?.aborted) throw error
      await new Promise(resolve => setTimeout(resolve, 300))
      response = await request()
    }
    if (mayRetryRead && [502, 503, 504].includes(response.status) && !controller.signal.aborted) {
      await response.body?.cancel().catch(() => undefined)
      await new Promise(resolve => setTimeout(resolve, 300))
      response = await request()
    }
  } catch (error) {
    if (controller.signal.aborted && !originalSignal?.aborted) {
      throw new ApiError(localizedApiErrorMessage('REQUEST_TIMEOUT', ''), 'REQUEST_TIMEOUT')
    }
    if (originalSignal?.aborted) throw error
    const message = mayRetryRead
      ? '网络连接暂时中断，请检查网络后重试'
      : '网络连接中断，任务提交结果尚未确认；如果刚提交了生成任务，请先查看历史记录，不要重复提交'
    throw new ApiError(message, 'NETWORK_ERROR')
  } finally {
    clearTimeout(timeout)
    originalSignal?.removeEventListener('abort', abortFromOriginal)
  }
  if (response.status === 413) {
    const requestId = response.headers.get('x-request-id') || response.headers.get('x-openimage-request-id') || ''
    throw new ApiError(localizedApiErrorMessage('UPLOAD_TOO_LARGE', ''), 'UPLOAD_TOO_LARGE', requestId)
  }
  const text = await response.text()
  let payload: ApiResponse<T>
  try {
    payload = text ? JSON.parse(text) as ApiResponse<T> : { success: false, code: String(response.status), message: response.status === 401 ? localizedAuthMessage() : 'Request failed', data: null as T, request_id: '' }
  } catch {
    payload = { success: false, code: String(response.status), message: response.status === 401 ? localizedAuthMessage() : 'Service returned an unreadable response', data: null as T, request_id: '' }
  }
  if (!response.ok || !payload.success) {
    if (response.status === 401 || payload.code === 'UNAUTHORIZED' || payload.code === 'TOKEN_INVALID') {
      if (!preserveSessionOnUnauthorized) clearSession()
      if (!suppressAuthRedirect) redirectToLogin()
      throw new ApiError(payload.message || localizedAuthMessage(), payload.code || 'UNAUTHORIZED', payload.request_id)
    }
    throw new ApiError(localizedApiErrorMessage(payload.code || String(response.status), payload.message), payload.code || String(response.status), payload.request_id)
  }
  return payload.data
}

export function isAiDisabledError(error: unknown) {
  if (error instanceof ApiError && error.code === 'AI_DISABLED') return true
  const message = error instanceof Error ? error.message : String(error || '')
  const lower = message.toLowerCase()
  return message.includes('管理员禁用')
    || message.includes('未配置密钥')
    || lower.includes('admin-disabled')
    || lower.includes('ai_disabled')
    || lower.includes('disabled or not configured')
    || lower.includes('api key')
}

export async function login(email: string, password: string, rememberMe = false, turnstileToken = '') { return apiFetch<AuthResponse>('/api/auth/login', { method: 'POST', body: JSON.stringify({ email, password, ...(rememberMe ? { remember_me: true } : {}), ...(turnstileToken ? { turnstile_token: turnstileToken } : {}) }) }) }
export async function refreshSession() {
  await assertApiAllowed('/api/auth/session')
  // Cookie-only exchange: a cached Bearer token must not override the OAuth session.
  const auth = await publicRequest<AuthResponse>('/api/auth/session', { method: 'POST', body: '{}' })
  if (!auth || typeof auth.access_token !== 'string' || !auth.access_token || typeof auth.user?.id !== 'string' || !auth.user.id) {
    throw new ApiError('Invalid session response', 'INVALID_SESSION')
  }
  return auth
}
export async function logoutSession() {
  await apiFetch('/api/auth/logout', { method: 'POST' })
  clearSession()
}
export async function changePassword(currentPassword: string, newPassword: string, confirmPassword: string) {
  const result = await apiFetch<{ requires_login: boolean }>('/api/auth/change-password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword, confirm_password: confirmPassword }) })
  if (result.requires_login) clearSession()
  return result
}
export async function register(email: string, password: string, confirmPassword: string, verificationCode: string, turnstileToken = '') { return apiFetch<AuthResponse>('/api/auth/register', { method: 'POST', body: JSON.stringify({ email, password, confirm_password: confirmPassword, verification_code: verificationCode, turnstile_token: turnstileToken }) }) }
export async function sendEmailCode(email: string, purpose: 'register' | 'login' | 'reset_password', turnstileToken = '') { return apiFetch<EmailCodeSendResult>('/api/auth/email-code', { method: 'POST', body: JSON.stringify({ email, purpose, turnstile_token: turnstileToken }) }) }
export async function loginWithEmailCode(email: string, verificationCode: string, rememberMe = false, turnstileToken = '') { return apiFetch<AuthResponse>('/api/auth/login/email-code', { method: 'POST', body: JSON.stringify({ email, verification_code: verificationCode, remember_me: rememberMe, turnstile_token: turnstileToken }) }) }
export async function requestPasswordReset(email: string, note = '', turnstileToken = '') { return apiFetch<{ accepted: boolean; message: string }>('/api/auth/forgot-password', { method: 'POST', body: JSON.stringify({ email, note, turnstile_token: turnstileToken }) }) }
export async function confirmPasswordReset(email: string, verificationCode: string, password: string, confirmPassword: string, turnstileToken = '') {
  const result = await apiFetch<{ updated: boolean; requires_login: boolean; redirect_to: string; email_masked?: string }>('/api/auth/password-reset', { method: 'POST', body: JSON.stringify({ email, verification_code: verificationCode, password, confirm_password: confirmPassword, turnstile_token: turnstileToken }) })
  if (!result?.updated) throw new ApiError('Password reset was not confirmed', 'RESET_UNCONFIRMED')
  if (result.requires_login) clearSession()
  return result
}
export function githubLoginUrl(next?: string) {
  const safeNext = next && next.startsWith('/') && !next.startsWith('//') ? next : ''
  return `${API_BASE_URL}/api/auth/github${safeNext ? `?next=${encodeURIComponent(safeNext)}` : ''}`
}
export async function githubStatus() { return apiFetch<{ enabled: boolean }>('/api/auth/github/status') }
export async function getMe(options: { passive?: boolean } = {}) {
  return apiFetch<UserProfile>('/api/me', options.passive ? { preserveSessionOnUnauthorized: true, suppressAuthRedirect: true } : {})
}
export async function updateMe(input: { nickname: string; uiLanguage: string; theme: string }) { return apiFetch<UserProfile>('/api/me', { method: 'PATCH', body: JSON.stringify({ nickname: input.nickname, ui_language: input.uiLanguage, theme: input.theme }) }) }
export async function uploadAvatar(file: File) { const form = new FormData(); form.append('file', file); return apiFetch<UserProfile>('/api/me/avatar', { method: 'POST', body: form }) }
export async function uploadAsset(file: File) { const form = new FormData(); form.append('file', file); return apiFetch<ProductImageView>('/api/assets', { method: 'POST', body: form }) }
export async function listProjects(options: { timeoutMs?: number } = {}) { return apiFetch<ProjectSummary[]>('/api/projects', options) }
export async function getProject(projectId: string, options: { timeoutMs?: number } = {}) { return apiFetch<ProjectDetail>(`/api/projects/${projectId}`, options) }
export async function deleteProject(projectId: string) { return apiFetch<{ project_id: string; deleted: boolean }>(`/api/projects/${projectId}`, { method: 'DELETE' }) }
export async function createProject(input: { productName: string; brand: string; country: string; language: string; description: string; category: string; material: string; dimensions: string; visualPrompt?: string; keywords: string[]; title_keywords?: string[]; selling_point_keywords?: string[]; description_keywords?: string[]; includeAplus?: boolean; aplusModule?: string; aplusSize?: string; aplusPrompt?: string }) {
  return apiFetch<ProjectSummary>('/api/projects', {
    method: 'POST',
    body: JSON.stringify({
      product_name: input.productName,
      brand: input.brand,
      country: input.country,
      language: input.language,
      description: input.description,
      category: input.category,
      material: input.material,
      dimensions: input.dimensions,
      visual_prompt: input.visualPrompt || '',
      keywords: input.keywords,
      title_keywords: input.title_keywords || [],
      selling_point_keywords: input.selling_point_keywords || [],
      description_keywords: input.description_keywords || [],
      include_aplus: Boolean(input.includeAplus),
      aplus_module: input.aplusModule || '',
      aplus_size: input.aplusSize || '',
      aplus_prompt: input.aplusPrompt || ''
    })
  })
}
type ImageIntentInput = { intent?: string; textMode?: string; referencePolicy?: string; templateId?: string }

function singleImageBody(input: { productName: string; brand: string; prompt: string; size: string; language: string; module: string; reference?: File | null; references?: File[]; referenceUrl?: string } & ImageIntentInput) {
  const references = [
    ...(input.references || []),
    ...(input.reference ? [input.reference] : [])
  ].filter((file): file is File => Boolean(file))
  if (!references.length) return JSON.stringify(input)
  const form = new FormData()
  form.append('productName', input.productName)
  form.append('brand', input.brand)
  form.append('prompt', input.prompt)
  form.append('size', input.size)
  form.append('language', input.language)
  form.append('module', input.module)
  if (input.referenceUrl) form.append('referenceUrl', input.referenceUrl)
  if (input.intent) form.append('intent', input.intent)
  if (input.textMode) form.append('textMode', input.textMode)
  if (input.referencePolicy) form.append('referencePolicy', input.referencePolicy)
  if (input.templateId) form.append('templateId', input.templateId)
  references.forEach(file => form.append('references', file))
  return form
}
export async function createAplusImage(input: { productName: string; brand: string; prompt: string; size: string; language: string; module: string; reference?: File | null; references?: File[]; referenceUrl?: string } & ImageIntentInput) { return apiFetch<ProjectDetail>('/api/projects/aplus-images', { method: 'POST', body: singleImageBody(input) }) }
export async function createCreativeImage(input: { productName: string; brand: string; prompt: string; size: string; language: string; module: string; reference?: File | null; references?: File[]; referenceUrl?: string } & ImageIntentInput) { return apiFetch<ProjectDetail>('/api/projects/creative-images', { method: 'POST', body: singleImageBody(input) }) }
function creativeChatBody(input: { chatId?: string; message: string; style: string; size: string; language: string; reference?: File | null; references?: File[]; referenceUrl?: string; referenceUrls?: string[] } & ImageIntentInput) {
  const referenceUrls = Array.from(new Set((input.referenceUrls || []).map(url => url.trim()).filter(Boolean))).slice(0, 10)
  const references = [
    ...(input.references || []),
    ...(input.reference ? [input.reference] : [])
  ].filter((file): file is File => Boolean(file)).slice(0, Math.max(0, 10 - referenceUrls.length))
  if (!references.length) return JSON.stringify({ chat_id: input.chatId || '', message: input.message, style: input.style, size: input.size, language: input.language, reference_url: input.referenceUrl || '', reference_urls: referenceUrls, intent: input.intent || '', textMode: input.textMode || '', referencePolicy: input.referencePolicy || '', templateId: input.templateId || '' })
  const form = new FormData()
  if (input.chatId) form.append('chatId', input.chatId)
  form.append('message', input.message)
  form.append('style', input.style)
  form.append('size', input.size)
  form.append('language', input.language)
  if (input.referenceUrl) form.append('referenceUrl', input.referenceUrl)
  referenceUrls.forEach(url => form.append('referenceUrls', url))
  if (input.intent) form.append('intent', input.intent)
  if (input.textMode) form.append('textMode', input.textMode)
  if (input.referencePolicy) form.append('referencePolicy', input.referencePolicy)
  if (input.templateId) form.append('templateId', input.templateId)
  references.forEach(file => form.append('references', file))
  if (references[0]) form.append('reference', references[0])
  return form
}
export async function listCreativeChats() { return apiFetch<CreativeChatSummary[]>('/api/projects/creative-chats') }
export async function getCreativeChat(chatId: string) { return apiFetch<CreativeChatView>(`/api/projects/creative-chats/${chatId}`) }
export async function estimateCreativeChat(input: { message: string; size: string; language: string }) { return apiFetch<CreativeChatEstimateResponse>('/api/projects/creative-chats/estimate', { method: 'POST', body: JSON.stringify({ message: input.message, size: input.size, language: input.language }) }) }
export async function generateCreativeChat(input: { chatId?: string; message: string; style: string; size: string; language: string; reference?: File | null; references?: File[]; referenceUrl?: string; referenceUrls?: string[] } & ImageIntentInput) { return apiFetch<CreativeChatGenerateResponse>('/api/projects/creative-chats/generate', { method: 'POST', body: creativeChatBody(input) }) }
export async function createLinkedAplusImage(projectId: string, input: { prompt: string; size: string; language: string; module: string }) { return apiFetch<ProjectDetail>(`/api/projects/${projectId}/linked-aplus`, { method: 'POST', body: JSON.stringify(input) }) }
export async function generateCopy(projectId: string) { return apiFetch<GenerateCopyResponse>(`/api/projects/${projectId}/generate-copy`, { method: 'POST' }) }
export async function confirmFacts(projectId: string, confirmedFacts: Record<string, any>) { return apiFetch<ProjectSummary>(`/api/projects/${projectId}/confirm-facts`, { method: 'POST', body: JSON.stringify({ confirmedFacts }) }) }
export async function uploadProductImage(projectId: string, imageType: string, file: File) { const form = new FormData(); form.append('imageType', imageType); form.append('file', file); return apiFetch<ProductImageView>(`/api/projects/${projectId}/product-images?imageType=${encodeURIComponent(imageType)}`, { method: 'POST', body: form }) }
export async function linkProductImage(projectId: string, imageType: string, url: string) { return apiFetch<ProductImageView>(`/api/projects/${projectId}/product-images/link`, { method: 'POST', body: JSON.stringify({ image_type: imageType, url }) }) }
export async function enqueueImages(projectId: string) { return apiFetch<ImageTaskView[]>(`/api/projects/${projectId}/images`, { method: 'POST' }) }
export async function enqueueListingPackage(projectId: string, input: { includeAplus: boolean; aplusModule: string; aplusSize: string; aplusPrompt: string; language: string }) {
  return apiFetch<ListingPackageResponse>(`/api/projects/${projectId}/listing-package`, {
    method: 'POST',
    body: JSON.stringify({
      include_aplus: input.includeAplus,
      aplus_module: input.aplusModule,
      aplus_size: input.aplusSize,
      aplus_prompt: input.aplusPrompt,
      language: input.language
    })
  })
}
export async function listProjectImages(projectId: string) { return apiFetch<ImageTaskView[]>(`/api/projects/${projectId}/images`) }
export async function deleteProjectImage(projectId: string, taskId: string) { return apiFetch<{ project_id: string; task_id: string; hidden: boolean }>(`/api/projects/${projectId}/images/${taskId}`, { method: 'DELETE' }) }
export async function retryProjectImage(projectId: string, taskId: string, input: { referenceUrl?: string; referenceText?: string; reference?: File | null } = {}) {
  if (input.reference) {
    const form = new FormData()
    if (input.referenceUrl) form.append('referenceUrl', input.referenceUrl)
    if (input.referenceText) form.append('referenceText', input.referenceText)
    form.append('reference', input.reference)
    return apiFetch<ImageTaskView>(`/api/projects/${projectId}/images/${taskId}/retry`, { method: 'POST', body: form })
  }
  return apiFetch<ImageTaskView>(`/api/projects/${projectId}/images/${taskId}/retry`, { method: 'POST', body: JSON.stringify({ reference_url: input.referenceUrl || '', reference_text: input.referenceText || '' }) })
}
export async function optimizePrompt(input: { prompt: string; productName: string; marketplace: string; language: string }) { return apiFetch<{ optimized_prompt: string }>('/api/projects/optimize-prompt', { method: 'POST', body: JSON.stringify({ prompt: input.prompt, product_name: input.productName, marketplace: input.marketplace, language: input.language }) }) }
export async function generateCanvasText(input: { protocol?: string; model?: string; requestBody: Record<string, any>; feature?: string }) {
  return apiFetch<{ response: Record<string, any> }>('/api/projects/canvas-text', {
    method: 'POST',
    body: JSON.stringify({
      protocol: input.protocol || 'openai-chat-completions',
      model: input.model || '',
      request_body: input.requestBody,
      feature: input.feature || 'canvas_text'
    })
  })
}
export async function suggestDomesticSellingPoints(input: { productName: string; platform: string; usage: string; specs: string; audience: string; language: string }) {
  return apiFetch<DomesticSellingPointSuggestionResponse>('/api/projects/domestic-selling-points/suggest', {
    method: 'POST',
    body: JSON.stringify({
      product_name: input.productName,
      platform: input.platform,
      usage: input.usage,
      specs: input.specs,
      audience: input.audience,
      language: input.language
    })
  })
}
export async function getCommerceSuiteCatalog() { return apiFetch<CommerceCatalog>('/api/commerce-suite/catalog') }
export async function listCommerceSuitePlatforms() { return apiFetch<CommercePlatformPreset[]>('/api/commerce-suite/platforms') }
export async function listCommerceSuiteTemplates(platform?: string) {
  return apiFetch<CommerceTemplatePreset[]>(`/api/commerce-suite/templates${platform ? `?platform=${encodeURIComponent(platform)}` : ''}`)
}
export async function estimateCommerceSuite(input: { platform: string; selectedSlots: string[]; resolution: string }) {
  return apiFetch<CommerceEstimateResponse>('/api/commerce-suite/estimate', {
    method: 'POST',
    body: JSON.stringify({ platform: input.platform, selected_slots: input.selectedSlots, resolution: input.resolution })
  })
}
export async function generateCommerceSuite(input: {
  platform: string
  productInfo: CommerceProductInfo
  selectedSlots: string[]
  resolution: string
  reference?: File | null
  references?: File[]
  referenceUrl?: string
  overlayOptions?: Record<string, any>
}) {
  const references = [
    ...(input.references || []),
    ...(input.reference ? [input.reference] : [])
  ].filter((file): file is File => Boolean(file)).slice(0, 10)
  if (!references.length) {
    return apiFetch<CommerceGenerateResponse>('/api/commerce-suite/generate', {
      method: 'POST',
      timeoutMs: 90_000,
      body: JSON.stringify({
        platform: input.platform,
        product_info: input.productInfo,
        selected_slots: input.selectedSlots,
        resolution: input.resolution,
        reference_url: input.referenceUrl || '',
        overlay_options: input.overlayOptions || {}
      })
    })
  }
  const form = new FormData()
  form.append('platform', input.platform)
  form.append('product_info', JSON.stringify(input.productInfo))
  form.append('selected_slots', JSON.stringify(input.selectedSlots))
  form.append('resolution', input.resolution)
  if (input.referenceUrl) form.append('reference_url', input.referenceUrl)
  form.append('overlay_options', JSON.stringify(input.overlayOptions || {}))
  references.forEach(file => form.append('references', file))
  return apiFetch<CommerceGenerateResponse>('/api/commerce-suite/generate', { method: 'POST', body: form, timeoutMs: 90_000 })
}
export async function retryCommerceSuiteTask(taskId: string) {
  return apiFetch<ImageTaskView>(`/api/commerce-suite/tasks/${taskId}/retry`, { method: 'POST' })
}
export async function listImageTranslationLanguages() {
  return apiFetch<ImageTranslationLanguage[]>('/api/image-translation/languages')
}
export async function generateImageTranslation(input: { targetLanguage: string; resolution: string; images: File[] }) {
  const form = new FormData()
  form.append('target_language', input.targetLanguage)
  form.append('resolution', input.resolution)
  input.images.forEach(file => form.append('images', file))
  return apiFetch<ImageTranslationGenerateResponse>('/api/image-translation/generate', { method: 'POST', body: form, timeoutMs: 90_000 })
}
export async function retryImageTranslationTask(taskId: string) {
  return apiFetch<ImageTaskView>(`/api/image-translation/tasks/${taskId}/retry`, { method: 'POST' })
}
export async function getVideoConfig() {
  return apiFetch<VideoPublicConfig>('/api/videos/config')
}
export async function listVideoTasks() {
  return apiFetch<VideoTaskView[]>('/api/videos/tasks')
}
export async function getVideoTask(taskId: string) {
  return apiFetch<VideoTaskView>(`/api/videos/tasks/${encodeURIComponent(taskId)}`)
}
export async function previewVideoScript(input: {
  prompt: string
  modelCode: string
  durationSeconds: number
  resolution: string
  aspectRatio: string
  generateAudio: boolean
  referenceCount: number
}) {
  return apiFetch<VideoScriptPreview>('/api/videos/script-preview', {
    method: 'POST',
    body: JSON.stringify({
      prompt: input.prompt,
      model_code: input.modelCode,
      duration_seconds: input.durationSeconds,
      resolution: input.resolution,
      aspect_ratio: input.aspectRatio,
      generate_audio: input.generateAudio,
      reference_count: input.referenceCount
    })
  })
}
export async function createVideoTask(input: {
  idempotencyKey: string
  title: string
  prompt: string
  originalPrompt?: string
  aspectRatio: string
  modelCode: string
  durationSeconds: number
  resolution: string
  generateAudio: boolean
  seed?: number
  images: File[]
  imageUrls?: string[]
  maxReferenceImages?: number
}) {
  const form = new FormData()
  form.append('title', input.title)
  form.append('prompt', input.prompt)
  if (input.originalPrompt?.trim()) form.append('original_prompt', input.originalPrompt.trim())
  form.append('aspect_ratio', input.aspectRatio)
  form.append('model_code', input.modelCode)
  form.append('duration_seconds', String(input.durationSeconds))
  form.append('resolution', input.resolution)
  form.append('generate_audio', String(input.generateAudio))
  if (input.seed !== undefined) form.append('seed', String(input.seed))
  const maxReferenceImages = Math.max(1, input.maxReferenceImages || 3)
  input.images.slice(0, maxReferenceImages).forEach(file => form.append('images', file))
  ;(input.imageUrls || []).slice(0, Math.max(0, maxReferenceImages - input.images.length)).forEach(url => form.append('image_urls', url))
  return apiFetch<VideoTaskView>('/api/videos/tasks', {
    method: 'POST',
    body: form,
    headers: { 'Idempotency-Key': input.idempotencyKey },
    timeoutMs: 90_000
  })
}
export async function deleteVideoTask(taskId: string) {
  return apiFetch<{ task_id: string; hidden: boolean }>(`/api/videos/tasks/${encodeURIComponent(taskId)}`, { method: 'DELETE' })
}
export async function getBillingSummary(options: { passive?: boolean } = {}) { return apiFetch<BillingSummary>('/api/billing/summary', options.passive ? { preserveSessionOnUnauthorized: true, suppressAuthRedirect: true } : {}) }
export async function listRechargePlans() { return apiFetch<RechargePlan[]>('/api/billing/plans') }
export async function listPaymentChannels() { return apiFetch<PaymentChannel[]>('/api/billing/channels') }
function billingPageQuery(input: BillingPageQuery = {}) {
  const params = new URLSearchParams()
  if (input.page) params.set('page', String(input.page))
  if (input.pageSize) params.set('page_size', String(input.pageSize))
  if (input.status?.trim()) params.set('status', input.status.trim())
  const query = params.toString()
  return query ? `?${query}` : ''
}
function normalizePageResult<T>(data: PageResult<T> | T[], fallback: BillingPageQuery = {}): PageResult<T> {
  if (Array.isArray(data)) {
    const pageSize = fallback.pageSize || data.length || 5
    return { rows: data, total: data.length, page: fallback.page || 1, page_size: pageSize, total_pages: data.length ? Math.ceil(data.length / pageSize) : 0 }
  }
  return data
}
export async function listRechargeOrders(input: BillingPageQuery = {}) {
  const data = await apiFetch<PageResult<RechargeOrder> | RechargeOrder[]>(`/api/billing/orders${billingPageQuery(input)}`)
  return normalizePageResult(data, input)
}
export async function createRechargeOrder(input: { planId: string; channelCode: string; turnstileToken?: string }) { return apiFetch<CreateRechargeOrderResponse>('/api/billing/orders', { method: 'POST', body: JSON.stringify({ plan_id: input.planId, channel_code: input.channelCode, turnstile_token: input.turnstileToken || '' }) }) }
export async function syncRechargeOrder(orderId: string) { return apiFetch<SyncRechargeOrderResponse>(`/api/billing/orders/${encodeURIComponent(orderId)}/sync`, { method: 'POST' }) }
export async function cancelRechargeOrder(orderId: string) { return apiFetch<SyncRechargeOrderResponse>(`/api/billing/orders/${encodeURIComponent(orderId)}/cancel`, { method: 'POST' }) }
export async function listCreditLedger(input: BillingPageQuery = {}) {
  const data = await apiFetch<PageResult<CreditLedger> | CreditLedger[]>(`/api/billing/ledger${billingPageQuery(input)}`)
  return normalizePageResult(data, input)
}
export async function redeemCode(code: string) { return apiFetch<RedeemCodeResponse>('/api/billing/redeem', { method: 'POST', body: JSON.stringify({ code }) }) }
export async function listMessages(options: { passive?: boolean } = {}) { return apiFetch<StationMessage[]>('/api/messages', options.passive ? { preserveSessionOnUnauthorized: true, suppressAuthRedirect: true } : {}) }
export async function markMessageRead(id: string) { return apiFetch<{ message_id: string; read: boolean }>(`/api/messages/${id}/read`, { method: 'PATCH' }) }
export async function getBenefitConfig() { return apiFetch<BenefitConfig>('/api/public/benefit-config') }
export async function getAssistantStatus() { return apiFetch<AssistantStatus>('/api/assistant/status', { preserveSessionOnUnauthorized: true, suppressAuthRedirect: true }) }
export async function chatWithAssistant(input: { messages: AssistantChatMessage[]; pageContext?: Record<string, any>; locale?: string; model?: string }) {
  return apiFetch<AssistantChatResponse>('/api/assistant/chat', {
    method: 'POST',
    body: JSON.stringify({ messages: input.messages, page_context: input.pageContext || {}, locale: input.locale || 'zh-CN', model: input.model || '' })
  })
}
