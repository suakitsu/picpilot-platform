'use client'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from 'react'
import Link from 'next/link'
import { ArrowLeft, Check, CreditCard, ImagePlus, Menu, Plus, Quote, RefreshCw, Send, Sparkles, X } from 'lucide-react'
import { currentLanguage, useI18n } from '@/components/i18n'
import { UserAuthGate } from '@/components/UserAuthGate'
import {
  assetUrl,
  CreativeChatSummary,
  type CreativeChatMessageView,
  CreativeChatView,
  estimateCreativeChat,
  generateCreativeChat,
  getOptionalBillingSummary,
  getCreativeChat,
  getStoredUser,
  isAiDisabledError,
  listCreativeChats,
  requireLogin,
  type UserProfile
} from '@/lib/api'
import { compressImageForUpload, MAX_UPLOAD_IMAGE_BYTES, TARGET_UPLOAD_IMAGE_BYTES } from '@/lib/imageCompression'
import { IMAGE_FILE_ACCEPT, imageUploadErrorMessage, imageUploadMessage, validateImageFiles } from '@/lib/imageUpload'

const CHAT_VERSION_LIMIT = 10
const INPUT_LIMIT = 3000
const DEFAULT_AGENT_STYLE = 'Agent 自动判断'
const DEFAULT_AGENT_SIZE = '1024x1024'
const ONE_K_CREDITS = 6
const TWO_K_CREDITS = 24
const FOUR_K_CREDITS = 30
const DEFAULT_ESTIMATE = { size: '1024x1024', quality_label: '1K', credits_cost: ONE_K_CREDITS }
const MAX_AGENT_REFERENCE_IMAGES = 10
const ESTIMATE_TIMEOUT_MS = 5000
const LOST_RESPONSE_SYNC_WINDOW_MS = 120000

type QuotedReference = {
  url: string
  label: string
}

function orderCreativeChatMessages(messages: CreativeChatMessageView[]) {
  return [...messages].sort((left, right) => {
    const leftTime = Date.parse(left.created_at || '') || 0
    const rightTime = Date.parse(right.created_at || '') || 0
    if (leftTime !== rightTime) return leftTime - rightTime
    const leftVersion = left.version_no || 0
    const rightVersion = right.version_no || 0
    if (leftVersion !== rightVersion) return leftVersion - rightVersion
    const leftRole = left.role === 'user' ? 0 : 1
    const rightRole = right.role === 'user' ? 0 : 1
    if (leftRole !== rightRole) return leftRole - rightRole
    return (left.id || '').localeCompare(right.id || '')
  })
}

function referenceUrlsForVersion(version: { reference_url?: string; reference_urls?: string[] } | undefined) {
  const urls: string[] = []
  for (const item of version?.reference_urls || []) {
    if (item && !urls.includes(item)) urls.push(item)
  }
  if (version?.reference_url && !urls.includes(version.reference_url)) {
    urls.push(version.reference_url)
  }
  return urls
}

function estimateFromText(message: string) {
  const text = (message || '').toLowerCase()
  const compact = text.replace(/[\s_\-]+/g, '')
  if (
    compact.includes('8k') ||
    compact.includes('4k') ||
    text.includes('4096') ||
    text.includes('3840') ||
    text.includes('2880') ||
    compact.includes('4320p') ||
    compact.includes('2160p') ||
    compact.includes('uhd') ||
    compact.includes('ultrahd') ||
    text.includes('ultra hd') ||
    text.includes('ultra-high') ||
    text.includes('ultra high') ||
    text.includes('highest resolution') ||
    text.includes('maximum resolution') ||
    text.includes('最高分辨率') ||
    text.includes('超高清') ||
    text.includes('极清') ||
    text.includes('原生4k') ||
    text.includes('原生 4k') ||
    text.includes('四k')
  ) {
    return { size: '2880x2880', quality_label: '4K', credits_cost: FOUR_K_CREDITS }
  }
  if (
    compact.includes('2k') ||
    text.includes('2048') ||
    compact.includes('1440p') ||
    compact.includes('qhd') ||
    /\bhd\b/.test(text) ||
    text.includes('high resolution') ||
    text.includes('hi-res') ||
    text.includes('hires') ||
    text.includes('clear text') ||
    text.includes('sharp text') ||
    text.includes('readable text') ||
    text.includes('text rendering') ||
    text.includes('文字清晰') ||
    text.includes('文字清楚') ||
    text.includes('字体清晰') ||
    text.includes('文案清晰') ||
    text.includes('清晰度') ||
    text.includes('高清') ||
    text.includes('超清') ||
    text.includes('高分辨率') ||
    text.includes('大尺寸') ||
    text.includes('大图')
  ) {
    return { size: '2048x2048', quality_label: '2K', credits_cost: TWO_K_CREDITS }
  }
  return DEFAULT_ESTIMATE
}

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  let timer: ReturnType<typeof setTimeout>
  return Promise.race([
    promise.finally(() => clearTimeout(timer)),
    new Promise<T>((_, reject) => {
      timer = setTimeout(() => reject(new Error('timeout')), ms)
    })
  ])
}

function isCreditMessage(message: string, insufficientCredits: string) {
  const lower = message.toLowerCase()
  return message === insufficientCredits
    || message.includes('积分不足')
    || lower.includes('not enough credits')
    || lower.includes('insufficient credits')
}

function isErrorMessage(message: string, insufficientCredits: string) {
  return isCreditMessage(message, insufficientCredits)
    || isAiDisabledError(message)
    || message.includes('上限')
    || message.includes('过大')
    || message.includes('失败')
    || message.toLowerCase().includes('failed')
    || message.toLowerCase().includes('too large')
}

function statusText(status: string | undefined, t: (key: string) => string) {
  if (status === 'running') return t('generating')
  return t(status || 'queued')
}

function isWorkingStatus(status?: string, resultUrl?: string) {
  return status === 'queued' || status === 'running' || (!resultUrl && status !== 'failed')
}

function localAgentMessage(key: 'referenceType' | 'referenceSize' | 'referenceCompressing' | 'referenceCompressed' | 'referenceCompressFailed' | 'referenceNeedPrompt' | 'referenceLimit' | 'referenceGuidance' | 'chatLimit' | 'thinking' | 'failedRefunded' | 'generatedResult' | 'referenceImage' | 'addReferenceImage' | 'syncFailed' | 'quoteImage' | 'unquoteImage' | 'historyImage' | 'quotedImage') {
  const lang = currentLanguage()
  if (key === 'referenceType') {
    if (lang === 'en-US') return 'Please upload an image as the reference.'
    if (lang === 'ja-JP') return '参考画像として画像ファイルをアップロードしてください。'
    return '请上传图片作为参考图'
  }
  if (key === 'referenceSize') {
    if (lang === 'en-US') return 'The reference image is too large. Please compress it to 25MB or less.'
    if (lang === 'ja-JP') return '参考画像が大きすぎます。25MB以内に圧縮してください。'
    return '参考图过大，请压缩到 25MB 以内后再上传'
  }
  if (key === 'referenceCompressing') {
    if (lang === 'en-US') return 'Compressing reference image...'
    if (lang === 'ja-JP') return '参考画像を圧縮しています...'
    return '正在压缩参考图...'
  }
  if (key === 'referenceCompressed') {
    if (lang === 'en-US') return 'Reference image was compressed for the upstream image model.'
    if (lang === 'ja-JP') return '上流画像モデルに合わせて参考画像を圧縮しました。'
    return '已自动压缩参考图，避免超过上游限制'
  }
  if (key === 'referenceCompressFailed') {
    if (lang === 'en-US') return 'The reference image is still too large after compression. Please use a smaller image.'
    if (lang === 'ja-JP') return '圧縮後も参考画像が大きすぎます。より小さい画像を使用してください。'
    return '参考图压缩后仍过大，请换一张更小的图片'
  }
  if (key === 'referenceLimit') {
    if (lang === 'en-US') return `Up to ${MAX_AGENT_REFERENCE_IMAGES} reference images are supported. Extra images were not added.`
    if (lang === 'ja-JP') return `参考画像は最大${MAX_AGENT_REFERENCE_IMAGES}枚までです。超過分は追加されませんでした。`
    return `最多支持 ${MAX_AGENT_REFERENCE_IMAGES} 张参考图，已忽略多余图片`
  }
  if (key === 'referenceGuidance') {
    if (lang === 'en-US') return `Suggested 1-4 images, up to ${MAX_AGENT_REFERENCE_IMAGES}. More references can reduce focus; some HD fallback providers use the first 4 only.`
    if (lang === 'ja-JP') return `推奨は1-4枚、最大${MAX_AGENT_REFERENCE_IMAGES}枚です。多すぎると焦点が弱くなり、一部の高解像度フォールバックでは先頭4枚のみ使用されます。`
    return `建议 1-4 张，最多 ${MAX_AGENT_REFERENCE_IMAGES} 张；参考图越多越容易分散，部分高清兜底通道会优先使用前 4 张`
  }
  if (key === 'chatLimit') {
    if (lang === 'en-US') return 'This chat has reached the 10-generation limit. Start a new chat to continue.'
    if (lang === 'ja-JP') return 'このチャットは10回の生成上限に達しました。続けるには新しいチャットを開始してください。'
    return '当前对话已达到 10 轮上限，请新开一个对话继续生成'
  }
  if (key === 'thinking') {
    if (lang === 'en-US') return 'Thinking'
    if (lang === 'ja-JP') return '考えています'
    return '思考中'
  }
  if (key === 'failedRefunded') {
    if (lang === 'en-US') return 'Generation failed. Credits for this image were refunded. You can regenerate it.'
    if (lang === 'ja-JP') return '生成に失敗しました。この画像のポイントは返還済みです。再生成できます。'
    return '生成失败，积分已退回，可重新生成'
  }
  if (key === 'generatedResult') {
    if (lang === 'en-US') return 'Generated result'
    if (lang === 'ja-JP') return '生成結果'
    return '生成结果'
  }
  if (key === 'referenceImage') {
    if (lang === 'en-US') return 'Reference image'
    if (lang === 'ja-JP') return '参考画像'
    return '参考图'
  }
  if (key === 'addReferenceImage') {
    if (lang === 'en-US') return 'Add reference image'
    if (lang === 'ja-JP') return '参考画像を追加'
    return '添加参考图'
  }
  if (key === 'quoteImage') {
    if (lang === 'en-US') return 'Use as reference'
    if (lang === 'ja-JP') return '参考画像として使用'
    return '引用这张图'
  }
  if (key === 'unquoteImage') {
    if (lang === 'en-US') return 'Remove reference'
    if (lang === 'ja-JP') return '参照を解除'
    return '取消引用'
  }
  if (key === 'historyImage') {
    if (lang === 'en-US') return 'Chat image'
    if (lang === 'ja-JP') return '会話内の画像'
    return '历史图片'
  }
  if (key === 'quotedImage') {
    if (lang === 'en-US') return 'Referenced'
    if (lang === 'ja-JP') return '参照中'
    return '已引用'
  }
  if (key === 'syncFailed') {
    if (lang === 'en-US') return 'Status sync is temporarily unavailable. It will retry automatically.'
    if (lang === 'ja-JP') return '状態同期が一時的に利用できません。自動で再試行します。'
    return '状态同步暂时失败，系统会自动重试'
  }
  if (lang === 'en-US') return 'Reference image added. Add what you want to generate or revise.'
  if (lang === 'ja-JP') return '参考画像を追加しました。生成または修正したい内容を入力してください。'
  return '已添加参考图，请补充生成或修改需求'
}

export function ImageAgentExperience() {
  const { billingEnabled } = useRuntime()
  const [chatInput, setChatInput] = useState('')
  const [credits, setCredits] = useState<number | null>(null)
  const [chat, setChat] = useState<CreativeChatView | null>(null)
  const [chatList, setChatList] = useState<CreativeChatSummary[]>([])
  const [currentUser, setCurrentUser] = useState<UserProfile | null>(null)
  const [userAvatarFailed, setUserAvatarFailed] = useState(false)
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [compressingReference, setCompressingReference] = useState(false)
  const [pendingMessage, setPendingMessage] = useState('')
  const [pendingSubmittedAt, setPendingSubmittedAt] = useState(0)
  const [pendingTimedOut, setPendingTimedOut] = useState(false)
  const [referenceFiles, setReferenceFiles] = useState<File[]>([])
  const [referencePreviews, setReferencePreviews] = useState<string[]>([])
  const [quotedReferences, setQuotedReferences] = useState<QuotedReference[]>([])
  const [pendingReferencePreviews, setPendingReferencePreviews] = useState<string[]>([])
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [estimate, setEstimate] = useState(DEFAULT_ESTIMATE)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const mainRef = useRef<HTMLElement | null>(null)
  const threadRef = useRef<HTMLDivElement | null>(null)
  const previewUrlsRef = useRef<string[]>([])
  const estimateSeqRef = useRef(0)
  const submittingRef = useRef(false)
  const t = useI18n()

  const chatLimitReached = (chat?.chat.version_count || 0) >= CHAT_VERSION_LIMIT
  const orderedMessages = orderCreativeChatMessages(chat?.messages || [])
  const pendingEchoWindowStart = pendingSubmittedAt ? pendingSubmittedAt - 120000 : 0
  const pendingEchoedByServer = pendingMessage
    ? orderedMessages.some(item => item.role === 'user' && item.content === pendingMessage && ((Date.parse(item.created_at || '') || 0) >= pendingEchoWindowStart))
    : false
  const pendingAssistantEchoedByServer = pendingMessage
    ? orderedMessages.some(item => item.role === 'assistant' && ((Date.parse(item.created_at || '') || 0) >= pendingEchoWindowStart))
    : false
  const showPendingUserMessage = Boolean(pendingMessage && !pendingEchoedByServer)
  const showPendingAssistantMessage = Boolean(pendingMessage && !pendingAssistantEchoedByServer)
  const userInitial = ((currentUser?.nickname || currentUser?.email_display || currentUser?.email_masked || t('agentYou') || 'U').trim().slice(0, 1) || 'U').toUpperCase()
  const activeVersion = chat?.versions?.[0]
  const activeEstimate = chatInput.trim()
    ? estimate
    : activeVersion?.size
      ? estimateFromText(activeVersion.size)
      : chat?.chat.size
        ? estimateFromText(chat.chat.size)
        : estimate
  const totalReferenceCount = quotedReferences.length + referenceFiles.length
  const canGenerate = chatInput.trim().length > 0 && !loading && !compressingReference && (!chatLimitReached || activeEstimate.credits_cost === 0)
  const composerHint = !billingEnabled ? `${chatInput.length}/${INPUT_LIMIT}` : totalReferenceCount && !chatInput.trim()
    ? localAgentMessage('referenceNeedPrompt')
    : totalReferenceCount
      ? `${localAgentMessage('referenceGuidance')} · ${t('estimatedCost')}: ${activeEstimate.credits_cost ?? ONE_K_CREDITS} ${t('creditsLabel')} · ${chatInput.length}/${INPUT_LIMIT}`
      : `${t('estimatedCost')}: ${activeEstimate.credits_cost ?? ONE_K_CREDITS} ${t('creditsLabel')} · ${chatInput.length}/${INPUT_LIMIT}`

  function scrollThreadToBottom(behavior: ScrollBehavior = 'smooth') {
    window.requestAnimationFrame(() => {
      const target = mainRef.current || threadRef.current
      target?.scrollTo({ top: target.scrollHeight, behavior })
    })
  }

  async function refreshCredits() {
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => setCredits(null))
  }

  async function loadChats() {
    const rows = await listCreativeChats()
    setChatList(rows)
    return rows
  }

  async function loadChat(chatId: string) {
    const detail = await getCreativeChat(chatId)
    setChat(detail)
    setMessage('')
    setPendingMessage('')
    setPendingSubmittedAt(0)
    clearReferences()
    setSidebarOpen(false)
    const latest = detail.versions[0]
    window.history.replaceState(null, '', `/ai-create?tab=agent&chatId=${detail.chat.id}${latest?.project_id ? `&projectId=${latest.project_id}` : ''}`)
  }

  async function syncAfterLostResponse(chatId: string, submittedAt: number) {
    const rows = await loadChats()
    const fallback = rows.find(row => (Date.parse(row.updated_at || '') || 0) >= submittedAt - LOST_RESPONSE_SYNC_WINDOW_MS)?.id
    const target = chatId || fallback
    if (!target) return false
    const detail = await getCreativeChat(target)
    setChat(detail)
    setMessage('')
    setPendingMessage('')
    setPendingSubmittedAt(0)
    setPendingReferencePreviews([])
    setSidebarOpen(false)
    const latest = detail.versions[0]
    window.history.replaceState(null, '', `/ai-create?tab=agent&chatId=${detail.chat.id}${latest?.project_id ? `&projectId=${latest.project_id}` : ''}`)
    return true
  }

  async function openChat(chatId: string) {
    if (loading) return
    setLoading(true)
    setMessage('')
    try {
      await loadChat(chatId)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('failed'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!requireLogin()) return
    refreshCredits()
    loadChats().catch(() => undefined)
    const params = new URLSearchParams(window.location.search)
    const incomingPrompt = (params.get('prompt') || '').trim()
    if (incomingPrompt) {
      setChatInput(current => current || incomingPrompt.slice(0, INPUT_LIMIT))
      params.delete('prompt')
      params.set('tab', params.get('tab') || 'agent')
      const nextQuery = params.toString()
      window.history.replaceState(null, '', `/ai-create${nextQuery ? `?${nextQuery}` : '?tab=agent'}`)
    }
    const chatId = params.get('chatId')
    if (chatId) {
      setLoading(true)
      loadChat(chatId).catch(error => setMessage(error instanceof Error ? error.message : t('failed'))).finally(() => setLoading(false))
    }
  }, [t])

  useEffect(() => {
    const syncUser = () => {
      setCurrentUser(getStoredUser())
      setUserAvatarFailed(false)
    }
    syncUser()
    window.addEventListener('openimage:user-updated', syncUser)
    window.addEventListener('storage', syncUser)
    return () => {
      window.removeEventListener('openimage:user-updated', syncUser)
      window.removeEventListener('storage', syncUser)
    }
  }, [])

  useEffect(() => {
    if (!chat?.versions.some(version => version.status === 'queued' || version.status === 'running')) return
    const timer = window.setInterval(refresh, 8000)
    return () => window.clearInterval(timer)
  }, [chat?.chat.id, chat?.versions])

  useEffect(() => {
    const text = chatInput.trim()
    const seq = ++estimateSeqRef.current
    if (!text) {
      setEstimate(DEFAULT_ESTIMATE)
      return
    }
    setEstimate(estimateFromText(text))
    const timer = window.setTimeout(() => {
      withTimeout(estimateCreativeChat({ message: text.slice(0, INPUT_LIMIT), size: estimateFromText(text).size || DEFAULT_AGENT_SIZE, language: currentLanguage() }), ESTIMATE_TIMEOUT_MS)
        .then(nextEstimate => {
          if (seq === estimateSeqRef.current) setEstimate(nextEstimate)
        })
        .catch(() => {
          if (seq === estimateSeqRef.current) setEstimate(DEFAULT_ESTIMATE)
        })
    }, 350)
    return () => window.clearTimeout(timer)
  }, [chatInput])

  useEffect(() => {
    setPendingTimedOut(false)
    if (!pendingMessage) return
    const timer = window.setTimeout(() => setPendingTimedOut(true), 45000)
    return () => window.clearTimeout(timer)
  }, [pendingMessage, pendingSubmittedAt])

  useEffect(() => {
    scrollThreadToBottom()
  }, [chat?.messages.length, chat?.versions, pendingMessage, showPendingUserMessage, showPendingAssistantMessage])

  useEffect(() => {
    return () => {
      previewUrlsRef.current.forEach(url => URL.revokeObjectURL(url))
      previewUrlsRef.current = []
    }
  }, [])

  function createPreviewUrl(file: File) {
    const url = URL.createObjectURL(file)
    previewUrlsRef.current.push(url)
    return url
  }

  function revokePreviewUrl(url: string) {
    if (!url.startsWith('blob:')) return
    URL.revokeObjectURL(url)
    previewUrlsRef.current = previewUrlsRef.current.filter(item => item !== url)
  }

  async function pickReferences(input?: FileList | File[] | null) {
    const incoming = Array.from(input || [])
    if (!incoming.length || loading || compressingReference) return
    if (chatLimitReached) {
      setMessage(localAgentMessage('chatLimit'))
      return
    }
    const slots = Math.max(0, MAX_AGENT_REFERENCE_IMAGES - totalReferenceCount)
    if (slots <= 0) {
      setMessage(localAgentMessage('referenceLimit'))
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }
    const nextFiles: File[] = []
    let compressed = false
    try {
      setCompressingReference(true)
      setMessage(imageUploadMessage('PROCESSING'))
      const imageFiles = (await validateImageFiles(incoming, MAX_UPLOAD_IMAGE_BYTES)).map(item => item.file)
      const selected = imageFiles.slice(0, slots)
      const hasOverflow = imageFiles.length > selected.length
      if (selected.some(file => file.size > TARGET_UPLOAD_IMAGE_BYTES)) setMessage(localAgentMessage('referenceCompressing'))
      for (const file of selected) {
        let nextFile = file
        if (file.size > TARGET_UPLOAD_IMAGE_BYTES) {
          const compression = await compressImageForUpload(file)
          nextFile = compression.file
          compressed = compressed || compression.compressed
          if (nextFile.size > MAX_UPLOAD_IMAGE_BYTES) {
            setMessage(localAgentMessage('referenceCompressFailed'))
            return
          }
        }
        nextFiles.push(nextFile)
      }
      const nextPreviews = nextFiles.map(createPreviewUrl)
      setReferenceFiles(current => [...current, ...nextFiles].slice(0, MAX_AGENT_REFERENCE_IMAGES))
      setReferencePreviews(current => [...current, ...nextPreviews].slice(0, MAX_AGENT_REFERENCE_IMAGES))
      if (hasOverflow) {
        setMessage(localAgentMessage('referenceLimit'))
      } else {
        setMessage(compressed ? localAgentMessage('referenceCompressed') : '')
      }
    } catch (error) {
      setMessage(imageUploadErrorMessage(error))
      return
    } finally {
      setCompressingReference(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  function removeReference(index: number) {
    setReferenceFiles(current => current.filter((_, itemIndex) => itemIndex !== index))
    setReferencePreviews(current => {
      const removed = current[index]
      if (removed) revokePreviewUrl(removed)
      return current.filter((_, itemIndex) => itemIndex !== index)
    })
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  function toggleQuotedReference(url: string, label: string) {
    if (loading || compressingReference || chatLimitReached) return
    const normalizedUrl = url.trim()
    if (!normalizedUrl) return
    const selected = quotedReferences.some(item => item.url === normalizedUrl)
    if (!selected && totalReferenceCount >= MAX_AGENT_REFERENCE_IMAGES) {
      setMessage(localAgentMessage('referenceLimit'))
      return
    }
    setQuotedReferences(current => selected
      ? current.filter(item => item.url !== normalizedUrl)
      : [...current, { url: normalizedUrl, label }]
    )
    setMessage('')
  }

  function removeQuotedReference(url: string) {
    setQuotedReferences(current => current.filter(item => item.url !== url))
  }

  function isQuotedReference(url: string) {
    return quotedReferences.some(item => item.url === url)
  }

  function clearReferences() {
    referencePreviews.forEach(revokePreviewUrl)
    setReferenceFiles([])
    setReferencePreviews([])
    setQuotedReferences([])
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  function handleReferenceDrop(event: DragEvent<HTMLDivElement>) {
    const files = event.dataTransfer.files
    if (!files?.length) return
    event.preventDefault()
    void pickReferences(files)
  }

  function handleReferenceDragOver(event: DragEvent<HTMLDivElement>) {
    if (Array.from(event.dataTransfer.items || []).some(item => item.kind === 'file')) {
      event.preventDefault()
    }
  }

  function handleReferencePaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const images = Array.from(event.clipboardData.files || [])
    if (!images.length) return
    event.preventDefault()
    void pickReferences(images)
  }

  async function submitChat() {
    if (!requireLogin() || !chatInput.trim()) return
    if (submittingRef.current) return
    submittingRef.current = true
    const requestText = chatInput.trim().slice(0, INPUT_LIMIT)
    const requestReferences = referenceFiles
    const requestQuotedReferences = quotedReferences
    const requestReferencePreviews = [
      ...requestQuotedReferences.map(item => assetUrl(item.url)),
      ...referencePreviews
    ]
    const submittedAt = Date.now()
    setLoading(true)
    setMessage('')
    let submitEstimate = estimateFromText(requestText)
    try {
      submitEstimate = await withTimeout(estimateCreativeChat({ message: requestText, size: submitEstimate.size || DEFAULT_AGENT_SIZE, language: currentLanguage() }), ESTIMATE_TIMEOUT_MS)
      setEstimate(submitEstimate)
    } catch {
      setEstimate(submitEstimate)
    }
    if (chatLimitReached && submitEstimate.credits_cost > 0) {
      setMessage(localAgentMessage('chatLimit'))
      submittingRef.current = false
      setLoading(false)
      return
    }
    if (credits !== null && credits < submitEstimate.credits_cost) {
      setMessage(t('insufficientCreditsGuide'))
      submittingRef.current = false
      setLoading(false)
      return
    }
    setPendingMessage(requestText)
    setPendingSubmittedAt(Date.now())
    setPendingReferencePreviews(requestReferencePreviews)
    setChatInput('')
    setReferenceFiles([])
    setReferencePreviews([])
    setQuotedReferences([])
    try {
      const response = await generateCreativeChat({
        chatId: chat?.chat.id,
        message: requestText,
        style: DEFAULT_AGENT_STYLE,
        size: submitEstimate.size || DEFAULT_AGENT_SIZE,
        language: currentLanguage(),
        references: requestReferences,
        referenceUrl: '',
        referenceUrls: requestQuotedReferences.map(item => item.url)
      })
      setPendingMessage('')
      setPendingSubmittedAt(0)
      setPendingReferencePreviews([])
      setChat(response.chat)
      await loadChats()
      try {
        const detail = await getCreativeChat(response.chat.chat.id)
        setChat(detail)
      } catch {}
      window.history.replaceState(null, '', `/ai-create?tab=agent&chatId=${response.chat.chat.id}${response.project?.project?.id ? `&projectId=${response.project.project.id}` : ''}`)
      if (response.project?.project?.id) {
        window.dispatchEvent(new Event('openimage:credits-updated'))
      }
      requestReferencePreviews.forEach(revokePreviewUrl)
    } catch (error) {
      const synced = await syncAfterLostResponse(chat?.chat.id || '', submittedAt).catch(() => false)
      if (synced) {
        setMessage('任务已提交，已同步最新对话。')
        requestReferencePreviews.forEach(revokePreviewUrl)
        window.dispatchEvent(new Event('openimage:credits-updated'))
        return
      }
      setPendingMessage('')
      setPendingSubmittedAt(0)
      setPendingReferencePreviews([])
      setChatInput(requestText)
      setReferenceFiles(requestReferences)
      setReferencePreviews(referencePreviews)
      setQuotedReferences(requestQuotedReferences)
      setMessage(error instanceof Error ? error.message : t('failed'))
    } finally {
      submittingRef.current = false
      setLoading(false)
      refreshCredits()
    }
  }

  async function refresh() {
    try {
      await loadChats()
      if (!chat?.chat.id) return
      const detail = await getCreativeChat(chat.chat.id)
      setChat(detail)
    } catch (error) {
      console.warn(localAgentMessage('syncFailed'), error)
    }
  }

  function newChat() {
    if (loading) return
    setChat(null)
    setChatInput('')
    setMessage('')
    setPendingMessage('')
    setPendingSubmittedAt(0)
    setPendingReferencePreviews([])
    clearReferences()
    setSidebarOpen(false)
    window.history.replaceState(null, '', '/ai-create?tab=agent')
  }

  function userAvatar() {
    return (
      <div className="agent-message-avatar agent-message-avatar-user">
        {currentUser?.avatar_url && !userAvatarFailed ? (
          <img src={assetUrl(currentUser.avatar_url)} alt={t('agentYou')} onError={() => setUserAvatarFailed(true)} />
        ) : <span>{userInitial}</span>}
      </div>
    )
  }

  function agentAvatar() {
    return (
      <div className="agent-message-avatar agent-message-avatar-agent">
        <img src="/favicon.png" alt="PicPilot Agent" />
      </div>
    )
  }

  return (
    <div className="agent-standalone-shell">
      <UserAuthGate title={t('loginImageAgent')} description={t('loginImageAgentDesc')}>
        {sidebarOpen ? <button className="agent-sidebar-backdrop" type="button" aria-label={t('cancel')} onClick={() => setSidebarOpen(false)} /> : null}
        <aside className={sidebarOpen ? 'agent-standalone-sidebar agent-standalone-sidebar-open' : 'agent-standalone-sidebar'}>
          <div className="agent-sidebar-head">
            <Link className="agent-sidebar-brand" href="/workspace" title={t('workspace')}>
              <img src="/brand-picpilot-mark.png" alt="PicPilot" />
              <span>PicPilot Agent</span>
            </Link>
            <button className="icon-button agent-sidebar-close" type="button" aria-label={t('cancel')} onClick={() => setSidebarOpen(false)}><X size={18} /></button>
          </div>
          <div className="agent-sidebar-actions">
            <button className="btn btn-primary" type="button" disabled={loading} onClick={newChat}><Plus size={16} />{t('agentNewChat')}</button>
            <Link className="btn btn-secondary" href="/workspace"><ArrowLeft size={16} />{t('workspace')}</Link>
          </div>
          <div className="agent-history-heading">
            <strong>{t('agentHistory')}</strong>
            <span>{t('agentHistoryLimit')}</span>
          </div>
          <div className="agent-standalone-history">
            {chatList.length ? chatList.map(item => (
              <button className={chat?.chat.id === item.id ? 'agent-chat-row agent-chat-row-active' : 'agent-chat-row'} key={item.id} type="button" disabled={loading} onClick={() => openChat(item.id)}>
                <span className={`agent-chat-status-dot agent-chat-status-${item.latest_status || 'queued'}`} />
                <span className="agent-chat-row-copy">
                  <strong>{item.title || t('imageAgent')}</strong>
                  <small>{item.size || '1024x1024'} · {statusText(item.latest_status, t)}</small>
                </span>
              </button>
            )) : <p className="agent-no-history">{t('agentNoHistory')}</p>}
          </div>
        </aside>

        <main className="agent-standalone-main" ref={mainRef}>
          <header className="agent-standalone-topbar">
            <div className="agent-topbar-left">
              <button className="icon-button agent-mobile-menu" type="button" aria-label={t('agentHistory')} onClick={() => setSidebarOpen(true)}><Menu size={18} /></button>
              <div>
                <h1>{t('imageAgentTitle')}</h1>
                <p>{activeEstimate.quality_label || '1K'} {activeEstimate.size || DEFAULT_AGENT_SIZE} <BillingOnly> · {activeEstimate.credits_cost ?? ONE_K_CREDITS} {t('creditsLabel')}</BillingOnly></p>
              </div>
            </div>
            <div className="agent-topbar-actions">
              <div className="agent-create-switch">
                <Link href="/ai-create?tab=free">自由创作</Link>
                <Link href="/ai-create?tab=copy">文案 Agent</Link>
              </div>
              <BillingOnly><Link className="pill pill-strong agent-credit-pill" href="/credits" title={t('rechargeRedeem')}>
                <CreditCard size={15} />{credits ?? '--'} {t('creditsLabel')}
              </Link></BillingOnly>
              <button className="icon-button" type="button" disabled={loading} aria-label={t('refreshStatus')} onClick={refresh}><RefreshCw size={17} /></button>
              <button className="btn btn-secondary agent-desktop-new" type="button" disabled={loading} onClick={newChat}><Plus size={16} />{t('agentNewChat')}</button>
            </div>
          </header>

          <section className="agent-standalone-stage">
            {message ? <div className={isErrorMessage(message, t('insufficientCreditsGuide')) ? 'agent-alert agent-alert-error' : 'agent-alert'}>{message} {isCreditMessage(message, t('insufficientCreditsGuide')) ? <BillingOnly><a href="/credits">{t('rechargeRedeem')}</a></BillingOnly> : null}</div> : null}
            {chatLimitReached ? <div className="agent-alert agent-alert-error">{localAgentMessage('chatLimit')}</div> : null}
            <div className="agent-standalone-thread" ref={threadRef}>
              {orderedMessages.length || showPendingUserMessage || showPendingAssistantMessage ? (
                <>
                {orderedMessages.map(item => {
                const version = (chat?.versions || []).find(row => row.version_no === item.version_no)
                const isAssistant = item.role === 'assistant'
                const resultUrl = item.result_url || ''
                const isWorking = isAssistant && isWorkingStatus(item.status, resultUrl)
                const isFailed = isAssistant && item.status === 'failed'
                const isImageResult = isAssistant && !isWorking && !isFailed && Boolean(resultUrl)
                const isChatReply = isAssistant && item.status === 'chat'
                const referenceUrls = item.role === 'user' ? referenceUrlsForVersion(version) : []
                return (
                  <article className={`agent-standalone-message agent-standalone-message-${item.role}`} key={item.id}>
                    {item.role === 'user' ? userAvatar() : agentAvatar()}
                    <div className={isWorking && !isChatReply ? 'agent-message-bubble agent-message-thinking' : isImageResult ? 'agent-message-bubble agent-message-bubble-image' : 'agent-message-bubble'}>
                      {item.role === 'assistant' ? (
                        <>
                          {isChatReply ? (
                            <p>{item.content}</p>
                          ) : isWorking ? (
                            <div className="agent-thinking-line"><span>{localAgentMessage('thinking')}</span><span className="agent-thinking-dots"><i /><i /><i /></span></div>
                          ) : isFailed ? (
                            <p>{version?.safe_error || localAgentMessage('failedRefunded')}</p>
                          ) : resultUrl ? (
                            <div className={isQuotedReference(resultUrl) ? 'agent-history-image agent-history-image-selected' : 'agent-history-image'}>
                              <a className="agent-result-link" href={assetUrl(resultUrl)} target="_blank" rel="noreferrer">
                                <img className="agent-result-image" src={assetUrl(resultUrl)} alt={localAgentMessage('generatedResult')} onLoad={() => scrollThreadToBottom('auto')} />
                              </a>
                              <button
                                className="agent-history-quote-button"
                                type="button"
                                aria-pressed={isQuotedReference(resultUrl)}
                                aria-label={isQuotedReference(resultUrl) ? localAgentMessage('unquoteImage') : localAgentMessage('quoteImage')}
                                title={isQuotedReference(resultUrl) ? localAgentMessage('unquoteImage') : localAgentMessage('quoteImage')}
                                onClick={() => toggleQuotedReference(resultUrl, localAgentMessage('generatedResult'))}
                              >
                                {isQuotedReference(resultUrl) ? <Check size={16} /> : <Quote size={16} />}
                              </button>
                            </div>
                          ) : null}
                        </>
                      ) : (
                        <>
                          {referenceUrls.length ? (
                            <div className="agent-reference-thumb-grid">
                              {referenceUrls.map((url, index) => (
                                <div className={isQuotedReference(url) ? 'agent-history-reference agent-history-image-selected' : 'agent-history-reference'} key={`${url}-${index}`}>
                                  <a href={assetUrl(url)} target="_blank" rel="noreferrer">
                                    <img className="agent-reference-thumb" src={assetUrl(url)} alt={`${localAgentMessage('referenceImage')} ${index + 1}`} />
                                  </a>
                                  <button
                                    className="agent-history-quote-button agent-history-quote-button-small"
                                    type="button"
                                    aria-pressed={isQuotedReference(url)}
                                    aria-label={isQuotedReference(url) ? localAgentMessage('unquoteImage') : localAgentMessage('quoteImage')}
                                    title={isQuotedReference(url) ? localAgentMessage('unquoteImage') : localAgentMessage('quoteImage')}
                                    onClick={() => toggleQuotedReference(url, `${localAgentMessage('referenceImage')} ${index + 1}`)}
                                  >
                                    {isQuotedReference(url) ? <Check size={14} /> : <Quote size={14} />}
                                  </button>
                                </div>
                              ))}
                            </div>
                          ) : null}
                          <p>{item.content}</p>
                        </>
                      )}
                    </div>
                  </article>
                )
              })}
              {showPendingUserMessage || showPendingAssistantMessage ? (
                <>
                  {showPendingUserMessage ? (
                    <article className="agent-standalone-message agent-standalone-message-user">
                      {userAvatar()}
                      <div className="agent-message-bubble">
                        {pendingReferencePreviews.length ? (
                          <div className="agent-reference-thumb-grid">
                            {pendingReferencePreviews.map((preview, index) => (
                              <img className="agent-reference-thumb" src={preview} alt={`${localAgentMessage('referenceImage')} ${index + 1}`} key={`${preview}-${index}`} />
                            ))}
                          </div>
                        ) : null}
                        <p>{pendingMessage}</p>
                      </div>
                    </article>
                  ) : null}
                  {showPendingAssistantMessage ? (
                    <article className="agent-standalone-message agent-standalone-message-assistant">
                      {agentAvatar()}
                      <div className="agent-message-bubble agent-message-thinking">
                        <div className="agent-thinking-line"><span>{pendingTimedOut ? '任务可能已提交，正在同步结果...' : localAgentMessage('thinking')}</span><span className="agent-thinking-dots"><i /><i /><i /></span></div>
                      </div>
                    </article>
                  ) : null}
                </>
              ) : null}
                </>
              ) : (
                <div className="agent-empty-state">
                  <Sparkles size={26} />
                  <h2>PicPilot Agent</h2>
                  <p>{t('agentEmpty')}</p>
                </div>
              )}
            </div>

            <div className="agent-composer-bar agent-standalone-composer" onDrop={handleReferenceDrop} onDragOver={handleReferenceDragOver}>
              {totalReferenceCount ? (
                <div className="agent-attachment-preview-list">
                  {quotedReferences.map((reference, index) => (
                    <div className="agent-attachment-preview agent-attachment-preview-history" key={reference.url}>
                      <img src={assetUrl(reference.url)} alt={`${localAgentMessage('referenceImage')} ${index + 1}`} />
                      <span>{`${localAgentMessage('referenceImage')} ${index + 1} · ${localAgentMessage('historyImage')}`}</span>
                      <button className="icon-button" type="button" disabled={loading} aria-label={t('cancel')} onClick={() => removeQuotedReference(reference.url)}><X size={15} /></button>
                    </div>
                  ))}
                  {referencePreviews.map((preview, index) => (
                    <div className="agent-attachment-preview" key={`${preview}-${index}`}>
                      <img src={preview} alt={`${localAgentMessage('referenceImage')} ${quotedReferences.length + index + 1}`} />
                      <span>{`${localAgentMessage('referenceImage')} ${quotedReferences.length + index + 1} · ${referenceFiles[index]?.name || localAgentMessage('referenceImage')}`}</span>
                      <button className="icon-button" type="button" disabled={loading} aria-label={t('cancel')} onClick={() => removeReference(index)}><X size={15} /></button>
                    </div>
                  ))}
                  <p className="agent-attachment-guidance">{localAgentMessage('referenceGuidance')}</p>
                </div>
              ) : null}
              <textarea
                className="textarea creative-chat-input"
                value={chatInput}
                maxLength={INPUT_LIMIT}
                disabled={loading || compressingReference}
                onChange={event => setChatInput(event.target.value)}
                onPaste={handleReferencePaste}
                placeholder={t('agentInputPlaceholder')}
              />
              <div className="agent-composer-footer">
                <div className="agent-composer-tools">
                  <input ref={fileInputRef} type="file" accept={IMAGE_FILE_ACCEPT} multiple hidden disabled={loading || compressingReference || chatLimitReached} onChange={event => void pickReferences(event.target.files)} />
                  <button className="icon-button agent-attachment-button" type="button" disabled={loading || compressingReference || chatLimitReached} aria-label={localAgentMessage('addReferenceImage')} title={localAgentMessage('addReferenceImage')} onClick={() => fileInputRef.current?.click()}><ImagePlus size={17} /></button>
                  <span className={totalReferenceCount && !chatInput.trim() ? 'muted agent-reference-hint' : 'muted'}>{composerHint}</span>
                </div>
                <button className="btn btn-primary" disabled={!canGenerate} onClick={submitChat}><Send size={16} />{loading ? t('agentThinking') : t('agentSend')}</button>
              </div>
            </div>
          </section>
        </main>
      </UserAuthGate>
    </div>
  )
}
