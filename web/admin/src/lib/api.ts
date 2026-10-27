import { assertApiAllowed, publicRequest } from './runtime'

export interface ApiResponse<T> {
  success: boolean
  code: string
  message: string
  data: T
  request_id: string
}

export interface UserProfile {
  id: string
  email_masked: string
  email_display?: string
  nickname: string
  role: string
  credits: number
  ui_language: string
  theme: string
  avatar_url?: string
}

export interface AuthResponse {
  user: UserProfile
  access_token: string
}

export interface DashboardTrendPoint {
  date: string
  label: string
  recharge: number
  usage: number
  image_usage: number
  video_usage: number
  users: number
  revenue: number
  credits: number
  failures: number
  image_failures: number
  video_failures: number
  cost: number
  image_cost: number
  video_cost: number
}

export interface DashboardMetrics {
  users: number
  projects: number
  image_tasks: number
  video_tasks: number
  total_tasks: number
  queued_image_tasks: number
  queued_video_tasks: number
  queued_tasks: number
  active_image_tasks: number
  active_video_tasks: number
  active_tasks: number
  failed_image_tasks: number
  failed_video_tasks: number
  failed_tasks: number
  credits: number
  video_credits_income: number
  video_refunded_credits: number
  pending_orders: number
  image_actual_cost: number
  video_actual_cost: number
  actual_cost: number
  trends: DashboardTrendPoint[]
}

export interface AdminUserRow {
  id: string
  email_masked: string
  email_display?: string
  nickname?: string
  auth_provider?: string
  github_login?: string
  role: string
  status: string
  credits: number
  project_count: number
  recharge_count: number
  risk_count?: number
  register_ip?: string
  last_request_ip?: string
  last_request_at?: string
  created_at: string
  updated_at?: string
}

export interface RiskEventRow {
  id: string
  user_id: string
  user_email_masked?: string
  user_account?: string
  project_id?: string
  task_id?: string
  feature: string
  risk_level: string
  keyword: string
  matched_text?: string
  source: string
  action: string
  cleared?: boolean
  created_at: string
}

export interface RiskKeywordConfig {
  enabled: boolean
  ban_threshold: number
  block_levels: string[]
  warn_levels: string[]
  keywords: Record<'high' | 'medium' | 'low', string[]>
}

export interface AdminTaskRow {
  task_id: string
  user_id: string
  user_email?: string
  user_email_masked?: string
  user_account?: string
  user_nickname?: string
  project_id: string
  image_type: string
  status: string
  credits_cost: number
  actual_cost: number
  cost_details?: ImageCostDetails
  provider: string
  model: string
  safe_error: string
  result_url?: string
  provider_attempts?: ProviderAttempt[]
  hidden_by_user?: boolean
  created_at: string
  updated_at?: string
}

export interface AdminTextCallRow {
  id: string
  user_id: string
  user_email: string
  user_nickname: string
  attributed: boolean
  feature: string
  target_type: string
  provider: string
  model: string
  status: string
  http_status?: number
  duration_ms: number
  input_chars: number
  output_chars: number
  error_type: string
  created_at: string
}

export interface AdminTextCallSummary {
  calls: number
  users: number
  success: number
  failed: number
  queued: number
  success_rate: number
  avg_duration_ms: number
}

export interface AdminTextCallPage extends PageResult<AdminTextCallRow> {
  summary: AdminTextCallSummary
}

export interface AdminImageReference {
  id: string
  image_type: string
  original_filename: string
  url: string
  content_type: string
  image_format: string
  file_size?: number
  width?: number
  height?: number
  created_at?: string
}

export interface AdminImageTaskDetail extends AdminTaskRow {
  task_set_id?: string
  prompt_snapshot: string
  prompt_details: unknown
  reference_images: AdminImageReference[]
  project?: {
    id?: string
    product_name?: string
    brand?: string
    country?: string
    language?: string
    original_info?: unknown
    confirmed_facts?: unknown
  }
  refunded_at?: string
}

export interface ProviderAttempt {
  provider: string
  model: string
  routing_reason?: string
  requestSize?: string
  returnedSize?: string
  elapsedMs?: number
  status: string
  errorType?: string
  errorMessage?: string
  success?: boolean
  notes?: string
  actualCostCny?: number
  upstreamCost?: number
  upstreamCostUnit?: string
  conversionRate?: number
  costSource?: string
}

export interface ImageCostDetails {
  currency: 'CNY'
  source: 'upstream_response' | 'tier_fallback' | 'historical_tier_fallback' | 'fixed_price' | 'provider_price_table' | string
  actual_cost_cny: number
  upstream_cost?: number
  upstream_cost_unit?: string
  conversion_rate?: number
}

export interface RechargePlan {
  id: string
  name: string
  credits: number
  price_cents: number
  currency: string
  enabled: boolean
  sort_order: number
}

export interface PaymentChannel {
  id: string
  name: string
  code: string
  visible: boolean
  enabled: boolean
  sort_order: number
  config: string
}

export interface RechargeOrder {
  id: string
  user_id: string
  user_email_masked: string
  user_email_display?: string
  user_nickname?: string
  plan_id: string
  channel_code: string
  amount_cents: number
  credits: number
  currency: string
  status: string
  remark: string
  external_trade_no?: string
  paid_at?: string
  created_at: string
  updated_at?: string
}

export interface RedemptionCode {
  id: string
  code: string
  credits: number
  max_uses: number
  used_count: number
  record_count: number
  expires_at?: string
  enabled: boolean
  benefit_type?: string
  batch_id?: string
  batch_name?: string
  batch_exclusive?: boolean
  status?: string
  note: string
  created_at: string
}

export interface RedemptionRecord {
  id: string
  code: string
  code_id: string
  batch_id?: string
  user_id: string
  user_email_masked: string
  user_nickname: string
  credits: number
  redeemed_at: string
}

export interface BenefitConfig {
  enabled: boolean
  title: string
  description: string
  qq_group: string
  qq_link?: string
  wechat_group: string
  qq_qr_url: string
  wechat_qr_url: string
  redeem_tutorial: string
  free_highlight_label: string
}

export interface BenefitQrUploadResult {
  kind: 'qq' | 'wechat'
  url: string
  config: BenefitConfig
}

export interface StationMessage {
  id: string
  title: string
  content: string
  audience: string
  enabled: boolean
  published_by?: string
  published_at: string
  expires_at?: string
  read_count: number
}

export interface ImageTypeConfig {
  id: string
  image_type: string
  image_name: string
  enabled: boolean
  credits_cost: number
  default_model: string
  fallback_model: string
  default_size: string
  required_fields: string
  sort_order: number
}

export interface PromptTemplate {
  id: string
  name: string
  scene: string
  version: number
  content: string
  enabled: boolean
  updated_at: string
}

export interface CostSummary {
  today_cost: number
  today_image_cost?: number
  today_video_cost?: number
  total_cost?: number
  image_cost?: number
  video_cost?: number
  average_image_cost: number
  average_video_cost?: number
  average_task_cost?: number
  failure_rate: number
  image_failure_rate?: number
  video_failure_rate?: number
  credits_cost: number
  net_video_credits?: number
  actual_cost?: number
  task_count?: number
  image_task_count?: number
  video_task_count?: number
  cost_policy?: {
    currency: 'CNY'
    rua_chat_per_image_cny: number
    apimart_credit_cny_rate: number
    apimart_usd_cny_rate: number
    apimart_tier_credits: Record<'1k' | '2k' | '4k', number>
    apimart_source_priority: string[]
  }
  rows: Array<{ media_type?: 'image' | 'video'; provider: string; task_type: string; calls: number; cost: number; failure_rate: number }>
}

export interface AdminAuditLog {
  id: string
  admin_id?: string
  admin_email_masked: string
  action: string
  target_type: string
  target_id: string
  summary: string
  before_json: string
  after_json: string
  created_at: string
}

export interface PageResult<T> {
  rows: T[]
  total: number
  page: number
  page_size: number
  total_pages: number
}

export interface PageQuery {
  keyword?: string
  status?: string
  role?: string
  channel?: string
  sort?: string
  minCredits?: number
  maxCredits?: number
  provider?: string
  model?: string
  feature?: string
  imageType?: string
  mediaType?: string
  riskLevel?: string
  userKeyword?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

export interface AdminAnalyticsFilters {
  keyword?: string
  status?: string
  provider?: string
  imageType?: string
  mediaType?: string
  userKeyword?: string
  from?: string
  to?: string
}

export interface AiProviderConfig {
  id?: string
  name?: string
  enabled: boolean
  base_url: string
  model: string
  api_key?: string
  api_key_set?: boolean
  api_key_masked?: string
  provider?: string
  env_key?: string
  priority?: number
  timeout_seconds?: number
  execution_mode?: 'http' | 'pull_worker'
  local_wait_seconds?: number
  offline_action?: 'queue' | 'fallback'
  lease_seconds?: number
  max_concurrency?: number
  actual_cost?: number
  supported_sizes?: string
  notes?: string
}

export interface AiWorkerTokenView {
  id: string
  name: string
  token_prefix: string
  enabled: boolean
  last_used_at?: string
  revoked_at?: string
  created_at?: string
}

export interface AiWorkerInstanceView {
  worker_id: string
  capabilities: string[]
  online: boolean
  last_heartbeat_at?: string
  active_tasks: number
  max_concurrency: number
  current_jobs: string[]
}

export interface AiWorkerProviderStatus {
  online: boolean
  online_workers: number
  active_tasks: number
  max_concurrency: number
}

export interface AiWorkerAdminState {
  tokens: AiWorkerTokenView[]
  instances: AiWorkerInstanceView[]
  providers: Record<string, AiWorkerProviderStatus>
  offline_after_seconds: number
}

export interface AiRuntimeConfig {
  image: AiProviderConfig
  text: AiProviderConfig
  image_providers?: AiProviderConfig[]
  text_providers?: AiProviderConfig[]
  can_rollback: boolean
  updated_at?: string
  workers?: AiWorkerAdminState
}

export interface AiProbeResult {
  ok: boolean
  kind: 'image' | 'text'
  provider: string
  model: string
  endpoint: string
  message: string
  latency_ms?: number
}

export interface AdminVideoTask {
  id: string
  user_id: string
  user_account: string
  title: string
  mode: string
  status: string
  progress: number
  provider: string
  model: string
  agnes_key_id?: string
  routing_reason?: string
  aspect_ratio: string
  duration_seconds: number
  requested_resolution: string
  credits_cost: number
  actual_cost?: number
  cost_details?: ImageCostDetails
  actual_width?: number
  actual_height?: number
  actual_seconds?: number
  refunded_at?: string
  safe_error?: string
  hidden_by_user?: boolean
  queued_at?: string
  submitted_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface AdminVideoVariantConfig {
  resolution: string
  duration_seconds: number
  price: number
  enabled: boolean
}

export interface AdminVideoModelConfig {
  code: string
  name: string
  description: string
  provider: 'gemini' | 'seedance' | 'agnes'
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
  variants: AdminVideoVariantConfig[]
}

export interface AgnesKeyConfig {
  id: string
  label: string
  enabled: boolean
  concurrency: number
  configured: boolean
  fingerprint: string
  source: 'admin' | 'environment' | 'mock'
  error: boolean
  active_count: number
  today_count: number
  success_count: number
  failure_count: number
  api_key?: string
  clear_api_key?: boolean
}

export interface VideoModelMetric {
  provider: string
  model: string
  call_count: number
  success_count: number
  failure_count: number
  average_queue_seconds: number
  average_generation_seconds: number
  average_cost: number
  total_cost: number
}

export interface VideoAdminMetrics {
  summary: {
    agnes_queued?: number
    agnes_active?: number
    agnes_fallback_gemini?: number
    average_queue_seconds?: number
    average_generation_seconds?: number
    daily_total_cost?: number
    credits_income?: number
    upstream_total_cost?: number
    income_cost_difference?: number
  }
  models: VideoModelMetric[]
}

export interface VideoHfsyMapping {
  model_code: string
  model_name: string
  resolution: string
  hfsy_model: string
  cost_cny: number
  minimum_credits: number
  aspect_ratios: string[]
}

export interface VideoAdminConfig {
  enabled: boolean
  paid_provider_requested: boolean
  paid_provider_env_enabled: boolean
  paid_provider_effective: boolean
  gemini_enabled: boolean
  gemini_effective: boolean
  seedance_enabled: boolean
  seedance_effective: boolean
  hfsy_enabled: boolean
  hfsy_effective: boolean
  agnes_route_enabled: boolean
  agnes_env_enabled: boolean
  agnes_effective: boolean
  apimart_key_set: boolean
  apimart_stored_key_set: boolean
  apimart_key_source: 'admin' | 'environment' | 'invalid' | 'none'
  apimart_key_error: boolean
  hfsy_key_set: boolean
  hfsy_stored_key_set: boolean
  hfsy_key_source: 'admin' | 'environment' | 'invalid' | 'none'
  hfsy_key_error: boolean
  hfsy_mappings: VideoHfsyMapping[]
  agnes_key_set: boolean
  agnes_stored_key_set: boolean
  agnes_key_source: 'admin' | 'environment' | 'invalid' | 'none'
  agnes_key_error: boolean
  agnes_keys: AgnesKeyConfig[]
  agnes_model: string
  standard_enabled: boolean
  standard_duration_seconds: number
  standard_resolution: '720p'
  standard_price: number
  quality_enabled: boolean
  quality_duration_seconds: number
  quality_resolution: '480p' | '720p'
  quality_price: number
  agnes_route_percent: number
  agnes_daily_limit: number
  agnes_concurrency: number
  agnes_circuit_failures: number
  poll_seconds: number
  timeout_minutes: number
  mock_mode: boolean
  mock_env_enabled: boolean
  models: Record<string, string>
  model_configs: AdminVideoModelConfig[]
  metrics?: VideoAdminMetrics
  recent_tasks?: AdminVideoTask[]
}

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

const API_BASE_URL = ''
const TOKEN_KEY = 'openimage_admin_token'
const USER_KEY = 'openimage_admin_user'

export function getToken() {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(TOKEN_KEY) || ''
}

export function getStoredUser(): UserProfile | null {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as UserProfile
  } catch {
    return null
  }
}

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
  if (typeof window === 'undefined') return 'Admin sign in required'
  const lang = window.localStorage.getItem('openimage-language') || getStoredUser()?.ui_language || 'zh-CN'
  if (lang === 'en-US') return 'Admin sign in required'
  if (lang === 'ja-JP') return '管理者ログインが必要です'
  return '需要管理员登录'
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

interface ApiFetchOptions extends RequestInit {
  preserveSessionOnUnauthorized?: boolean
  suppressAuthRedirect?: boolean
  activity?: boolean
}

export async function apiFetch<T>(path: string, init: ApiFetchOptions = {}) {
  await assertApiAllowed(path, init.body)
  const { preserveSessionOnUnauthorized = false, suppressAuthRedirect = false, activity = true, ...requestInit } = init
  const token = getToken()
  const headers: Record<string, string> = {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(requestInit.headers as Record<string, string> | undefined)
  }
  if (!(requestInit.body instanceof FormData)) headers['Content-Type'] = 'application/json'
  const activityId = `admin_${Date.now()}_${Math.random().toString(36).slice(2)}`
  if (activity && typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('picpilot:admin-request-start', { detail: { id: activityId, path } }))
  }
  try {
    const response = await fetch(`${API_BASE_URL}${path}`, {
      ...requestInit,
      headers,
      cache: 'no-store'
    })
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
      throw new ApiError(payload.message || 'Request failed', payload.code || String(response.status), payload.request_id)
    }
    return payload.data
  } finally {
    if (activity && typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent('picpilot:admin-request-end', { detail: { id: activityId, path } }))
    }
  }
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

export async function adminLogin(email: string, password: string, turnstileToken = '') {
  return apiFetch<AuthResponse>('/api/auth/admin/login', { method: 'POST', body: JSON.stringify({ email, password, ...(turnstileToken ? { turnstile_token: turnstileToken } : {}) }) })
}
export async function refreshSession() {
  await assertApiAllowed('/api/auth/admin/login')
  const auth = await publicRequest<AuthResponse>('/api/auth/session', { method: 'POST', body: '{}' })
  if (!auth || typeof auth.access_token !== 'string' || !auth.access_token || typeof auth.user?.id !== 'string' || !auth.user.id) {
    throw new ApiError('Invalid session response', 'INVALID_SESSION')
  }
  if (!['admin', 'super_admin'].includes(auth.user.role)) throw new ApiError('Administrator access is required', 'FORBIDDEN')
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

export async function getMe(options: { passive?: boolean } = {}) {
  return apiFetch<UserProfile>('/api/me', options.passive ? { preserveSessionOnUnauthorized: true, suppressAuthRedirect: true, activity: false } : {})
}
export async function updateMe(input: { nickname: string; uiLanguage: string; theme: string }) { return apiFetch<UserProfile>('/api/me', { method: 'PATCH', body: JSON.stringify({ nickname: input.nickname, ui_language: input.uiLanguage, theme: input.theme }) }) }
export async function uploadAvatar(file: File) { const form = new FormData(); form.append('file', file); return apiFetch<UserProfile>('/api/me/avatar', { method: 'POST', body: form }) }

function analyticsQuery(input: AdminAnalyticsFilters = {}) {
  const params = new URLSearchParams()
  if (input.keyword?.trim()) params.set('keyword', input.keyword.trim())
  if (input.status?.trim()) params.set('status', input.status.trim())
  if (input.provider?.trim()) params.set('provider', input.provider.trim())
  if (input.imageType?.trim()) params.set('image_type', input.imageType.trim())
  if (input.mediaType?.trim()) params.set('media_type', input.mediaType.trim())
  if (input.userKeyword?.trim()) params.set('user_keyword', input.userKeyword.trim())
  if (input.from?.trim()) params.set('from', input.from.trim())
  if (input.to?.trim()) params.set('to', input.to.trim())
  const query = params.toString()
  return query ? `?${query}` : ''
}

export async function getDashboard(input: AdminAnalyticsFilters = {}) { return apiFetch<DashboardMetrics>(`/api/admin/dashboard${analyticsQuery(input)}`) }
function pageQuery(input: PageQuery = {}) {
  const params = new URLSearchParams()
  if (input.keyword?.trim()) params.set('keyword', input.keyword.trim())
  if (input.status?.trim()) params.set('status', input.status.trim())
  if (input.role?.trim()) params.set('role', input.role.trim())
  if (input.channel?.trim()) params.set('channel', input.channel.trim())
  if (input.sort?.trim()) params.set('sort', input.sort.trim())
  if (input.minCredits !== undefined) params.set('min_credits', String(input.minCredits))
  if (input.maxCredits !== undefined) params.set('max_credits', String(input.maxCredits))
  if (input.provider?.trim()) params.set('provider', input.provider.trim())
  if (input.model?.trim()) params.set('model', input.model.trim())
  if (input.feature?.trim()) params.set('feature', input.feature.trim())
  if (input.imageType?.trim()) params.set('image_type', input.imageType.trim())
  if (input.mediaType?.trim()) params.set('media_type', input.mediaType.trim())
  if (input.riskLevel?.trim()) params.set('risk_level', input.riskLevel.trim())
  if (input.userKeyword?.trim()) params.set('user_keyword', input.userKeyword.trim())
  if (input.from?.trim()) params.set('from', input.from.trim())
  if (input.to?.trim()) params.set('to', input.to.trim())
  if (input.page) params.set('page', String(input.page))
  if (input.pageSize) params.set('page_size', String(input.pageSize))
  const query = params.toString()
  return query ? `?${query}` : ''
}

export async function listAuditLogs(input: PageQuery = {}) {
  return apiFetch<PageResult<AdminAuditLog>>(`/api/admin/audit-logs${pageQuery(input)}`)
}
export async function listAdminUsers(input: PageQuery | string = {}) {
  const query = typeof input === 'string' ? pageQuery({ keyword: input }) : pageQuery(input)
  return apiFetch<PageResult<AdminUserRow>>(`/api/admin/users${query}`)
}
export async function adjustUserCredits(input: { userId: string; changeAmount: number; remark: string; requestToken: string }) {
  return apiFetch<{ user_id: string; balance_after: number; ledger_id: string }>(`/api/admin/users/${input.userId}/credits`, { method: 'POST', body: JSON.stringify({ change_amount: input.changeAmount, remark: input.remark, request_token: input.requestToken }) })
}
export async function updateUserStatus(input: { userId: string; status: string }) {
  return apiFetch<{ user_id: string; status: string }>(`/api/admin/users/${input.userId}/status`, { method: 'PATCH', body: JSON.stringify({ status: input.status }) })
}
export async function listRiskEvents(input: PageQuery = {}) {
  return apiFetch<PageResult<RiskEventRow>>(`/api/admin/risk-events${pageQuery(input)}`)
}
export async function getRiskKeywords() {
  return apiFetch<RiskKeywordConfig>('/api/admin/risk-keywords')
}
export async function updateRiskConfig(input: Pick<RiskKeywordConfig, 'enabled' | 'ban_threshold' | 'block_levels' | 'warn_levels'>) {
  return apiFetch<RiskKeywordConfig>('/api/admin/risk-config', { method: 'PATCH', body: JSON.stringify(input) })
}
export async function addRiskKeyword(input: { level: 'high' | 'medium' | 'low'; keyword: string }) {
  return apiFetch<RiskKeywordConfig>('/api/admin/risk-keywords', { method: 'POST', body: JSON.stringify(input) })
}
export async function deleteRiskKeyword(input: { level: 'high' | 'medium' | 'low'; keyword: string }) {
  return apiFetch<RiskKeywordConfig>('/api/admin/risk-keywords/delete', { method: 'POST', body: JSON.stringify(input) })
}
export async function clearUserRiskEvents(userId: string) {
  return apiFetch<{ user_id: string; status: string; cleared: number }>(`/api/admin/users/${userId}/risk-events/clear`, { method: 'POST' })
}
export async function resetUserPassword(input: { userId: string; password: string }) {
  return apiFetch<{ user_id: string; updated: boolean }>(`/api/admin/users/${input.userId}/password`, { method: 'POST', body: JSON.stringify({ password: input.password }) })
}
export async function listImageTasks(input: PageQuery = {}) { return apiFetch<PageResult<AdminTaskRow>>(`/api/admin/image-tasks${pageQuery(input)}`) }
export async function listTextCalls(input: PageQuery = {}) { return apiFetch<AdminTextCallPage>(`/api/admin/text-calls${pageQuery(input)}`) }
export async function getImageTask(taskId: string) { return apiFetch<AdminImageTaskDetail>(`/api/admin/image-tasks/${encodeURIComponent(taskId)}`) }
export async function retryImageTask(taskId: string) { return apiFetch<{ task_id: string; status: string }>(`/api/admin/image-tasks/${taskId}/retry`, { method: 'PATCH' }) }
export async function deleteImageTask(taskId: string) { return apiFetch<{ task_id: string; deleted: boolean }>(`/api/admin/image-tasks/${taskId}/delete`, { method: 'PATCH' }) }
export async function listRechargePlans() { return apiFetch<RechargePlan[]>('/api/admin/billing/plans') }
export async function listPaymentChannels() { return apiFetch<PaymentChannel[]>('/api/admin/billing/channels') }
export async function listRechargeOrders(input: PageQuery = {}) { return apiFetch<PageResult<RechargeOrder>>(`/api/admin/billing/orders${pageQuery(input)}`) }
export async function updateRechargePlan(plan: RechargePlan) {
  return apiFetch<RechargePlan>(`/api/admin/billing/plans/${plan.id}`, { method: 'POST', body: JSON.stringify({ name: plan.name, credits: plan.credits, price_cents: plan.price_cents, currency: plan.currency, enabled: plan.enabled, sort_order: plan.sort_order }) })
}
export async function updatePaymentChannel(channel: PaymentChannel) {
  return apiFetch<PaymentChannel>(`/api/admin/billing/channels/${channel.id}`, { method: 'POST', body: JSON.stringify({ name: channel.name, visible: channel.visible, enabled: channel.enabled, sort_order: channel.sort_order, config: channel.config }) })
}
export async function confirmRechargeOrder(orderId: string) {
  return apiFetch<{ order_id: string; status: string; balance_after: number; ledger_id?: string }>(`/api/admin/billing/orders/${orderId}/confirm`, { method: 'PATCH' })
}
export async function cancelRechargeOrder(orderId: string) {
  return apiFetch<{ order_id: string; status: string }>(`/api/admin/billing/orders/${orderId}/cancel`, { method: 'PATCH' })
}
export async function listRedemptionCodes(input: PageQuery = {}) {
  return apiFetch<PageResult<RedemptionCode>>(`/api/admin/billing/redemption-codes${pageQuery(input)}`)
}
export async function createRedemptionCodes(input: { credits: number; maxUses: number; quantity: number; expiresAt?: string; validDays?: number; benefitType?: string; prefix: string; note: string }) {
  return apiFetch<RedemptionCode[]>('/api/admin/billing/redemption-codes', { method: 'POST', body: JSON.stringify({ credits: input.credits, max_uses: input.maxUses, quantity: input.quantity, expires_at: input.expiresAt || null, valid_days: input.validDays || null, benefit_type: input.benefitType || '兑换码积分', prefix: input.prefix, note: input.note }) })
}
export async function createBenefitCardWave(input: { name: string; prefix: string; validDays?: number; expiresAt?: string; benefitType?: string; note: string; tiers: Array<{ credits: number; quantity: number; maxUses?: number }> }) {
  return apiFetch<{ batch_id: string; batch_name: string; quantity: number; codes: RedemptionCode[] }>('/api/admin/benefits/card-waves', { method: 'POST', body: JSON.stringify({ name: input.name, prefix: input.prefix, valid_days: input.validDays || null, expires_at: input.expiresAt || null, benefit_type: input.benefitType || '定时福利卡', note: input.note, tiers: input.tiers.map(tier => ({ credits: tier.credits, quantity: tier.quantity, max_uses: tier.maxUses || 1 })) }) })
}
export async function listBenefitRedemptionCodes(input: PageQuery = {}) {
  return apiFetch<PageResult<RedemptionCode>>(`/api/admin/benefits/redemption-codes${pageQuery(input)}`)
}
export async function listRedemptionRecords(input: PageQuery = {}) {
  return apiFetch<PageResult<RedemptionRecord>>(`/api/admin/benefits/redemption-records${pageQuery(input)}`)
}
export async function getBenefitConfig() { return apiFetch<BenefitConfig>('/api/admin/benefits/config') }
export async function updateBenefitConfig(config: BenefitConfig) {
  return apiFetch<BenefitConfig>('/api/admin/benefits/config', { method: 'POST', body: JSON.stringify(config) })
}
export async function uploadBenefitQr(kind: 'qq' | 'wechat', file: File) {
  const form = new FormData()
  form.append('kind', kind)
  form.append('file', file)
  return apiFetch<BenefitQrUploadResult>('/api/admin/benefits/qr', { method: 'POST', body: form })
}
export async function listStationMessages(input: PageQuery = {}) {
  return apiFetch<PageResult<StationMessage>>(`/api/admin/messages${pageQuery(input)}`)
}
export async function publishStationMessage(input: { title: string; content: string; audience: string; expiresAt?: string; enabled: boolean }) {
  return apiFetch<StationMessage>('/api/admin/messages', { method: 'POST', body: JSON.stringify({ title: input.title, content: input.content, audience: input.audience, expires_at: input.expiresAt || null, enabled: input.enabled }) })
}
export async function listImageConfigs() { return apiFetch<ImageTypeConfig[]>('/api/admin/image-configs') }
export async function updateImageConfig(config: ImageTypeConfig) {
  return apiFetch<ImageTypeConfig>(`/api/admin/image-configs/${config.id}`, { method: 'POST', body: JSON.stringify({ enabled: config.enabled, credits_cost: config.credits_cost, default_model: config.default_model, fallback_model: config.fallback_model, default_size: config.default_size, required_fields: config.required_fields }) })
}
export async function listPromptTemplates() { return apiFetch<PromptTemplate[]>('/api/admin/prompts') }
export async function createPromptTemplate(input: { name: string; scene: string; content: string }) {
  return apiFetch<PromptTemplate>('/api/admin/prompts', { method: 'POST', body: JSON.stringify(input) })
}
export async function updatePromptTemplate(prompt: PromptTemplate) {
  return apiFetch<PromptTemplate>(`/api/admin/prompts/${prompt.id}`, { method: 'POST', body: JSON.stringify({ name: prompt.name, scene: prompt.scene, content: prompt.content, enabled: prompt.enabled }) })
}
export async function getCostSummary(input: AdminAnalyticsFilters = {}) { return apiFetch<CostSummary>(`/api/admin/costs${analyticsQuery(input)}`) }
export async function getAiRuntimeConfig() { return apiFetch<AiRuntimeConfig>('/api/admin/ai-config') }
export async function validateAiRuntimeConfig(config: AiRuntimeConfig) { return apiFetch<{ ok: boolean; message: string }>('/api/admin/ai-config/validate', { method: 'POST', body: JSON.stringify(config) }) }
export async function probeAiRuntimeConfig(kind: 'image' | 'text', config: AiRuntimeConfig) { return apiFetch<AiProbeResult>(`/api/admin/ai-config/probe/${kind}`, { method: 'POST', body: JSON.stringify(config) }) }
export async function updateAiRuntimeConfig(config: AiRuntimeConfig) { return apiFetch<AiRuntimeConfig>('/api/admin/ai-config', { method: 'PATCH', body: JSON.stringify(config) }) }
export async function rollbackAiRuntimeConfig() { return apiFetch<AiRuntimeConfig>('/api/admin/ai-config/rollback', { method: 'POST' }) }
export async function getAiWorkerState() { return apiFetch<AiWorkerAdminState>('/api/admin/ai-config/workers') }
export async function createAiWorkerToken(name: string) {
  return apiFetch<{ token: string; worker_token: AiWorkerTokenView; shown_once: boolean }>('/api/admin/ai-config/worker-tokens', {
    method: 'POST',
    body: JSON.stringify({ name })
  })
}
export async function regenerateAiWorkerToken(tokenId: string) {
  return apiFetch<{ token: string; worker_token: AiWorkerTokenView; shown_once: boolean }>(`/api/admin/ai-config/worker-tokens/${encodeURIComponent(tokenId)}/regenerate`, { method: 'POST' })
}
export async function revokeAiWorkerToken(tokenId: string) {
  return apiFetch<AiWorkerTokenView>(`/api/admin/ai-config/worker-tokens/${encodeURIComponent(tokenId)}/revoke`, { method: 'POST' })
}
export async function getVideoAdminConfig() { return apiFetch<VideoAdminConfig>('/api/admin/video-config') }
export async function listAdminVideoTasks(input: PageQuery = {}) {
  return apiFetch<PageResult<AdminVideoTask>>(`/api/admin/video-tasks${pageQuery(input)}`)
}
export async function deleteAdminVideoTask(taskId: string) {
  return apiFetch<{ task_id: string; deleted: boolean }>(`/api/admin/video-tasks/${encodeURIComponent(taskId)}`, { method: 'DELETE' })
}
export async function updateVideoAdminConfig(
  config: VideoAdminConfig,
  secret: {
    agnesApiKey?: string
    clearAgnesApiKey?: boolean
    apimartApiKey?: string
    clearApimartApiKey?: boolean
    hfsyApiKey?: string
    clearHfsyApiKey?: boolean
    saveAgnesKeys?: boolean
  } = {}
) {
  return apiFetch<VideoAdminConfig>('/api/admin/video-config', {
    method: 'PATCH',
    body: JSON.stringify({
      enabled: config.enabled,
      paid_provider_requested: config.paid_provider_requested,
      gemini_enabled: config.gemini_enabled,
      seedance_enabled: config.seedance_enabled,
      hfsy_enabled: config.hfsy_enabled,
      agnes_route_enabled: config.agnes_route_enabled,
      standard_enabled: config.standard_enabled,
      standard_duration_seconds: config.standard_duration_seconds,
      standard_resolution: config.standard_resolution,
      standard_price: config.standard_price,
      quality_enabled: config.quality_enabled,
      quality_duration_seconds: config.quality_duration_seconds,
      quality_resolution: config.quality_resolution,
      quality_price: config.quality_price,
      agnes_route_percent: 0,
      agnes_daily_limit: config.agnes_daily_limit,
      agnes_concurrency: config.agnes_concurrency,
      agnes_circuit_failures: config.agnes_circuit_failures,
      poll_seconds: config.poll_seconds,
        timeout_minutes: config.timeout_minutes,
        model_configs: config.model_configs.map(model => ({
          code: model.code,
          enabled: model.enabled,
          variants: model.variants.map(variant => ({
            resolution: variant.resolution,
            duration_seconds: variant.duration_seconds,
            price: variant.price,
            enabled: variant.enabled
          }))
        })),
        agnes_model: config.agnes_model,
      agnes_api_key: secret.agnesApiKey || undefined,
      clear_agnes_api_key: secret.clearAgnesApiKey || false,
      agnes_keys: secret.saveAgnesKeys ? config.agnes_keys.filter(key => key.source !== 'environment').map(key => ({
        id: key.id,
        label: key.label,
        enabled: key.enabled,
        concurrency: key.concurrency,
        api_key: key.api_key || undefined,
        clear_api_key: key.clear_api_key || false
      })) : undefined,
      apimart_api_key: secret.apimartApiKey || undefined,
      clear_apimart_api_key: secret.clearApimartApiKey || false,
      hfsy_api_key: secret.hfsyApiKey || undefined,
      clear_hfsy_api_key: secret.clearHfsyApiKey || false
    })
  })
}
export async function getAdminCommerceSuiteCatalog() { return apiFetch<CommerceCatalog>('/api/admin/commerce-suite/catalog') }
export async function saveAdminCommerceSuiteCatalog(catalog: CommerceCatalog) {
  return apiFetch<CommerceCatalog>('/api/admin/commerce-suite/catalog', { method: 'POST', body: JSON.stringify(catalog) })
}
export async function listAdminCommerceSuitePlatforms() { return apiFetch<CommercePlatformPreset[]>('/api/admin/commerce-suite/platforms') }
export async function listAdminCommerceSuiteTemplates(platform?: string) {
  return apiFetch<CommerceTemplatePreset[]>(`/api/admin/commerce-suite/templates${platform ? `?platform=${encodeURIComponent(platform)}` : ''}`)
}
