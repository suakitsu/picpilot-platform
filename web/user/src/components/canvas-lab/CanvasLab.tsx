'use client'

import { AlertCircle, CheckCircle2, Coins, Expand, Image as ImageIcon, LoaderCircle, RefreshCw, Video } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'
import { useI18n } from '@/components/i18n'
import {
  assetUrl,
  createCreativeImage,
  createVideoTask,
  generateCanvasText,
  getAiCapabilities,
  getMe,
  getToken,
  getOptionalBillingSummary,
  getProject,
  getVideoConfig,
  getVideoTask,
  previewVideoScript
} from '@/lib/api'
import type { VideoModelConfig, VideoPublicConfig } from '@/lib/api'
import { IMAGE_SIZE_OPTIONS, imageCreditsForSize } from '@/lib/imageSizes'
import { forwardNovaRequest } from './bridge-host'
import { PROTOCOL, type NovaRequest, type Runtime as BridgeRuntime } from '../../../../../canvas-lab/bridge/protocol'

const BRIDGE_PROTOCOL = PROTOCOL
const BRIDGE_TASK_STORAGE = 'picpilot:canvas:task-map'
const BRIDGE_CANCELLED = 'PICPILOT_REQUEST_CANCELLED'
const ACTIVE_IMAGE_STATUSES = new Set(['pending', 'queued', 'submitting', 'submitted', 'processing', 'running'])
const ACTIVE_VIDEO_STATUSES = new Set(['queued', 'submitting', 'submitted', 'generating', 'upscaling', 'quality_check'])
const TERMINAL_VIDEO_STATUSES = new Set(['completed', 'failed', 'cancelled', 'timeout'])

type CanvasOption = {
  id: 'nova' | 'infinite'
  label: string
  shortLabel: string
  sourceUrl: string
  basePath: string
  initialPath: string
}

type BridgeEnvelope = {
  protocol?: string
  type?: string
  app?: 'nova' | 'infinite'
  requestId?: string
  action?: string
  payload?: Record<string, any>
}

type ConfirmationState = {
  title: string
  description: string
  facts: Array<{ label: string; value: string }>
  cost: number
  balance: number | null
  busyLabel: string
}

type RuntimeConfig = {
  credits: number | null
  image: {
    enabled: boolean
    model: string
    prices: Record<string, number>
  }
  text: {
    enabled: boolean
    model: string
    price: number
  }
  video: {
    enabled: boolean
    models: VideoModelConfig[]
  }
}

const options: CanvasOption[] = [
  {
    id: 'nova',
    label: 'PicPilot 创作工作台',
    shortLabel: '创作工作台',
    sourceUrl: 'https://github.com/tianjiangqiji/nova-image-studio',
    basePath: '/canvas/nova/',
    initialPath: ''
  },
  {
    id: 'infinite',
    label: 'PicPilot 无限画布',
    shortLabel: '自由画布',
    sourceUrl: 'https://github.com/basketikun/infinite-canvas',
    basePath: '/canvas/infinite/',
    initialPath: 'canvas'
  }
]

function normalizeUrl(value: string) {
  return value.endsWith('/') ? value : `${value}/`
}

function runtimeUrl(option: CanvasOption, runtime: RuntimeConfig | null) {
  return option.basePath + option.initialPath
}

function numberValue(value: unknown, fallback: number) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function parseRatio(value: string) {
  const normalized = value.replace('×', 'x').trim()
  const dimension = normalized.match(/^(\d+)x(\d+)$/i)
  if (dimension) return Number(dimension[1]) / Number(dimension[2])
  const ratio = normalized.match(/^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/)
  if (ratio) return Number(ratio[1]) / Number(ratio[2])
  return 1
}

function qualityFromParams(params: Record<string, any>) {
  const explicit = String(params.outputSize || '').toUpperCase()
  if (explicit === '1K' || explicit === '2K' || explicit === '4K') return explicit
  const quality = String(params.quality || '').toLowerCase()
  if (quality === 'high' || quality === '4k') return '4K'
  if (quality === 'medium' || quality === 'hd' || quality === '2k') return '2K'
  const dimensions = String(params.customSize || params.size || '').match(/^(\d+)x(\d+)$/i)
  if (dimensions) {
    const edge = Math.max(Number(dimensions[1]), Number(dimensions[2]))
    if (edge >= 2800) return '4K'
    if (edge >= 1800) return '2K'
  }
  return '1K'
}

function imageSizeFromParams(params: Record<string, any>) {
  const quality = qualityFromParams(params)
  const source = String(params.customSize || params.size || params.aspectRatio || '1:1')
  const targetRatio = parseRatio(source)
  const candidates = IMAGE_SIZE_OPTIONS.filter(option => option.quality === quality)
  return candidates.reduce((best, option) => {
    const currentGap = Math.abs(Math.log(parseRatio(option.ratio) / targetRatio))
    const bestGap = Math.abs(Math.log(parseRatio(best.ratio) / targetRatio))
    return currentGap < bestGap ? option : best
  }, candidates[0]).value
}

function imageFileName(mime: string, index: number) {
  const extension = mime.includes('jpeg') ? 'jpg' : mime.includes('webp') ? 'webp' : 'png'
  return `canvas-reference-${index + 1}.${extension}`
}

async function sourceFiles(values: unknown, limit: number) {
  const sources = Array.isArray(values) ? values.filter(value => typeof value === 'string' && value.trim()).slice(0, limit) as string[] : []
  const files: File[] = []
  for (let index = 0; index < sources.length; index += 1) {
    const response = await fetch(sources[index])
    if (!response.ok) throw new Error(`第 ${index + 1} 张参考图无法读取`)
    const blob = await response.blob()
    if (!blob.type.startsWith('image/')) throw new Error(`第 ${index + 1} 个参考素材不是图片`)
    if (blob.size > 28 * 1024 * 1024) throw new Error(`第 ${index + 1} 张参考图超过 28MB`)
    files.push(new File([blob], imageFileName(blob.type, index), { type: blob.type, lastModified: Date.now() }))
  }
  return files
}

function nearestValue(values: number[], requested: number, fallback: number) {
  if (!values.length) return fallback
  return values.reduce((best, value) => Math.abs(value - requested) < Math.abs(best - requested) ? value : best, values[0])
}

function normalizeVideoResolution(value: unknown, model: VideoModelConfig) {
  const requested = String(value || '').toLowerCase().replace('p', '')
  return model.resolutions.find(item => item.toLowerCase().replace('p', '') === requested)
    || model.default_resolution
    || model.resolutions[0]
}

function normalizeVideoRatio(value: unknown, model: VideoModelConfig) {
  const requested = String(value || '').trim()
  return model.aspect_ratios.includes(requested) ? requested : (model.aspect_ratios[0] || '9:16')
}

function statusError(value: unknown, fallback: string) {
  return value instanceof Error ? value.message : fallback
}

function sleep(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}

export function CanvasLab() {
  const { billingEnabled, config: publicConfig } = useRuntime()
  const t = useI18n()
  const [selected, setSelected] = useState<CanvasOption['id']>('nova')
  const [mountedFrames, setMountedFrames] = useState<Record<CanvasOption['id'], boolean>>({ nova: true, infinite: false })
  const [loadedFrames, setLoadedFrames] = useState<Record<CanvasOption['id'], boolean>>({ nova: false, infinite: false })
  const [runtime, setRuntime] = useState<RuntimeConfig | null>(null)
  const [runtimeError, setRuntimeError] = useState('')
  const [confirmation, setConfirmation] = useState<ConfirmationState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const frameRefs = useRef<Record<CanvasOption['id'], HTMLIFrameElement | null>>({ nova: null, infinite: null })
  const confirmationResolver = useRef<((confirmed: boolean) => void) | null>(null)
  const frameReadyFallbacks = useRef<Partial<Record<CanvasOption['id'], number>>>({})
  const bridgeTasks = useRef(new Map<string, string[]>())
  const bridgeRequests = useRef(new Map<string, Promise<any>>())
  const bridgeHandler = useRef<((message: BridgeEnvelope) => Promise<any>) | null>(null)
  const active = useMemo(() => options.find(option => option.id === selected) || options[0], [selected])

  const refreshRuntime = useCallback(async () => {
    setRuntimeError('')
    try {
      const [billing, capabilities, videoResult] = await Promise.all([getOptionalBillingSummary(), getAiCapabilities(), getVideoConfig().then(value => ({ value, error: '' })).catch(error => ({ value: null, error: statusError(error, 'Video configuration unavailable') }))])
      if (videoResult.error) setRuntimeError(videoResult.error)
      const next: RuntimeConfig = {
        credits: billing?.credits ?? null,
        image: {
          enabled: capabilities.image.enabled,
          model: capabilities.image.default_model,
          prices: { '1K': imageCreditsForSize('1024x1024'), '2K': imageCreditsForSize('2048x2048'), '4K': imageCreditsForSize('2880x2880') }
        },
        text: { enabled: capabilities.text.enabled, model: capabilities.text.default_model, price: 0 },
        video: { enabled: Boolean(videoResult.value?.enabled), models: videoResult.value?.model_configs.filter(model => model.enabled) || [] }
      }
      setRuntime(next)
      return next
    } catch (error) {
      setRuntime(null)
      setRuntimeError(statusError(error, t('canvasConfigLoadFailed')))
      throw error
    }
  }, [t])

  useEffect(() => {
    void refreshRuntime().catch(() => undefined)
  }, [refreshRuntime])

  useEffect(() => {
    const root = document.documentElement
    const viewport = window.visualViewport
    const syncViewport = () => {
      root.style.setProperty('--canvas-visual-viewport-top', `${viewport?.offsetTop ?? 0}px`)
      root.style.setProperty('--canvas-visual-viewport-left', `${viewport?.offsetLeft ?? 0}px`)
      root.style.setProperty('--canvas-visual-viewport-width', `${viewport?.width ?? window.innerWidth}px`)
      root.style.setProperty('--canvas-visual-viewport-height', `${viewport?.height ?? window.innerHeight}px`)
    }

    syncViewport()
    window.addEventListener('resize', syncViewport)
    viewport?.addEventListener('resize', syncViewport)
    viewport?.addEventListener('scroll', syncViewport)
    return () => {
      window.removeEventListener('resize', syncViewport)
      viewport?.removeEventListener('resize', syncViewport)
      viewport?.removeEventListener('scroll', syncViewport)
      root.style.removeProperty('--canvas-visual-viewport-top')
      root.style.removeProperty('--canvas-visual-viewport-left')
      root.style.removeProperty('--canvas-visual-viewport-width')
      root.style.removeProperty('--canvas-visual-viewport-height')
    }
  }, [])

  useEffect(() => {
    if (!confirmation) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previousOverflow
    }
  }, [confirmation])

  const frameUrls = useMemo<Record<CanvasOption['id'], string>>(() => ({
    nova: runtimeUrl(options[0], runtime),
    infinite: runtimeUrl(options[1], runtime)
  // Runtime credits are not encoded in either URL, so a balance refresh cannot reload a canvas.
  }), [runtime])
  const previousFrameUrls = useRef(frameUrls)

  useEffect(() => {
    const changed = options.filter(option => previousFrameUrls.current[option.id] !== frameUrls[option.id])
    if (changed.length) {
      changed.forEach(option => {
        const fallback = frameReadyFallbacks.current[option.id]
        if (fallback !== undefined) window.clearTimeout(fallback)
        delete frameReadyFallbacks.current[option.id]
      })
      setLoadedFrames(current => {
        const next = { ...current }
        changed.forEach(option => { next[option.id] = false })
        return next
      })
      previousFrameUrls.current = frameUrls
    }
  }, [frameUrls])

  useEffect(() => {
    if (!mountedFrames[selected] || loadedFrames[selected] || !frameUrls[selected]) return
    if (frameReadyFallbacks.current[selected] !== undefined) return

    // A few embedded canvas builds keep the document request open while their
    // runtime warms up, so the iframe load event can arrive very late. Do not
    // leave an opaque loading veil over an otherwise usable workspace forever.
    frameReadyFallbacks.current[selected] = window.setTimeout(() => markFrameReady(selected), 12_000)
  }, [selected, mountedFrames, loadedFrames, frameUrls])

  useEffect(() => () => {
    confirmationResolver.current?.(false)
    confirmationResolver.current = null
    Object.values(frameReadyFallbacks.current).forEach(timeout => {
      if (timeout !== undefined) window.clearTimeout(timeout)
    })
  }, [])

  function markFrameReady(frameId: CanvasOption['id']) {
    const fallback = frameReadyFallbacks.current[frameId]
    if (fallback !== undefined) {
      window.clearTimeout(fallback)
      delete frameReadyFallbacks.current[frameId]
    }
    setLoadedFrames(current => current[frameId] ? current : { ...current, [frameId]: true })
  }

  useEffect(() => {
    if (!confirmation) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !confirmBusy) resolveConfirmation(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [confirmation, confirmBusy])

  function requestConfirmation(next: ConfirmationState) {
    if (confirmationResolver.current) return Promise.reject(new Error(BRIDGE_CANCELLED))
    setConfirmation(next)
    return new Promise<boolean>(resolve => {
      confirmationResolver.current = resolve
    })
  }

  function resolveConfirmation(confirmed: boolean) {
    if (confirmed) setConfirmBusy(true)
    confirmationResolver.current?.(confirmed)
    confirmationResolver.current = null
    if (!confirmed) setConfirmation(null)
  }

  async function currentBalance() {
    const billing = await getOptionalBillingSummary()
    setRuntime(current => current ? { ...current, credits: billing?.credits ?? null } : current)
    window.dispatchEvent(new Event('openimage:credits-updated'))
    return billing?.credits ?? null
  }

  function rememberBridgeTask(taskId: string, projectIds: string[]) {
    bridgeTasks.current.set(taskId, projectIds)
    try {
      const stored = JSON.parse(sessionStorage.getItem(BRIDGE_TASK_STORAGE) || '{}') as Record<string, string[]>
      const entries = Object.entries({ ...stored, [taskId]: projectIds }).slice(-50)
      sessionStorage.setItem(BRIDGE_TASK_STORAGE, JSON.stringify(Object.fromEntries(entries)))
    } catch {
      // In-memory tracking still keeps the active page functional when storage is unavailable.
    }
  }

  function bridgeProjectIds(taskId: string) {
    const activeIds = bridgeTasks.current.get(taskId)
    if (activeIds?.length) return activeIds
    try {
      const stored = JSON.parse(sessionStorage.getItem(BRIDGE_TASK_STORAGE) || '{}') as Record<string, string[]>
      const restored = stored[taskId]
      if (Array.isArray(restored) && restored.length) {
        bridgeTasks.current.set(taskId, restored)
        return restored
      }
    } catch {
      // A missing task map is handled below as a normal not-found state.
    }
    return taskId.startsWith('prod_') ? [taskId] : []
  }

  async function submitImages(payload: Record<string, any>) {
    const prompt = String(payload.prompt || '').trim()
    if (!prompt) throw new Error('请输入图片生成提示词')
    const params = payload.params && typeof payload.params === 'object' ? payload.params : {}
    const size = imageSizeFromParams(params)
    const quality = qualityFromParams(params)
    const count = Math.max(1, Math.min(4, Math.floor(numberValue(params.count, 1))))
    const unitCost = imageCreditsForSize(size)
    const cost = unitCost * count
    const balance = await currentBalance()
    if (billingEnabled && (balance === null || balance < cost)) throw new Error(`积分不足，本次生成需要 ${cost} 积分`)
    const confirmed = await requestConfirmation({
      title: '确认生成图片',
      description: billingEnabled ? '任务将进入 PicPilot 图片队列，失败时仍按原有规则自动退款。' : t('generationConfirmPersonal'),
      facts: [
        { label: '规格', value: `${quality} · ${size.replace('x', '×')}` },
        { label: '数量', value: `${count} 张` },
        { label: '参考图', value: `${Array.isArray(payload.images) ? Math.min(payload.images.length, 10) : 0} 张` }
      ],
      cost,
      balance,
      busyLabel: '正在提交图片任务'
    })
    if (!confirmed) throw new Error(BRIDGE_CANCELLED)

    try {
      const references = await sourceFiles(payload.images, 10)
      const projectIds: string[] = []
      let partialError = ''
      for (let index = 0; index < count; index += 1) {
        try {
          const detail = await createCreativeImage({
            productName: count > 1 ? `画布创作 ${index + 1}/${count}` : '画布创作',
            brand: 'PicPilot Canvas',
            prompt,
            size,
            language: 'zh-CN',
            module: count > 1 ? `画布节点 ${index + 1}/${count}` : '画布节点',
            references,
            intent: 'canvas_generation',
            referencePolicy: references.length ? 'ordered_references' : 'none'
          })
          projectIds.push(detail.project.id)
        } catch (error) {
          partialError = statusError(error, '部分图片任务提交失败')
          break
        }
      }
      if (!projectIds.length) throw new Error(partialError || '图片任务提交失败')
      const taskId = `canvas_img_${crypto.randomUUID?.() || `${Date.now()}_${Math.random().toString(36).slice(2)}`}`
      rememberBridgeTask(taskId, projectIds)
      await currentBalance().catch(() => undefined)
      return {
        taskId,
        submittedCount: projectIds.length,
        warning: partialError ? `已成功提交 ${projectIds.length}/${count} 张；其余未扣费。${partialError}` : undefined
      }
    } finally {
      setConfirmBusy(false)
      setConfirmation(null)
    }
  }

  async function imageStatus(taskId: string) {
    const projectIds = bridgeProjectIds(taskId)
    if (!projectIds.length) throw new Error('未找到对应的图片任务')
    const projects = await Promise.all(projectIds.map(projectId => getProject(projectId)))
    const tasks = projects.flatMap(project => project.image_tasks)
    const images = tasks.filter(task => task.status === 'success' && task.result_url).map(task => `URL:${assetUrl(task.result_url)}`)
    const activeTasks = tasks.filter(task => ACTIVE_IMAGE_STATUSES.has(task.status))
    const failures = tasks.filter(task => task.status === 'failed')
    if (!tasks.length || activeTasks.length) {
      return { task: { id: taskId, status: tasks.some(task => task.status === 'running') ? 'processing' : 'queued', result: { images } } }
    }
    if (images.length) {
      return {
        task: {
          id: taskId,
          status: 'completed',
          result: { images },
          warning: failures.length ? (billingEnabled ? `${failures.length} 张生成失败并已按规则退款` : t('generationFailedPersonal')) : undefined
        }
      }
    }
    return { task: { id: taskId, status: 'failed', result: { images: [] }, error: failures[0]?.safe_error || '图片生成失败' } }
  }

  async function waitForImages(taskId: string) {
    let consecutiveReadFailures = 0
    for (let attempt = 0; attempt < 300; attempt += 1) {
      let status: Awaited<ReturnType<typeof imageStatus>>
      try {
        status = await imageStatus(taskId)
        consecutiveReadFailures = 0
      } catch {
        consecutiveReadFailures += 1
        if (consecutiveReadFailures <= 8) {
          await sleep(Math.min(10_000, 2_000 + consecutiveReadFailures * 1_000))
          continue
        }
        throw new Error('任务状态同步暂时中断，任务仍可能在后台生成；请到历史记录确认，不要重复提交')
      }
      if (status.task.status === 'completed') {
        return { images: (status.task.result?.images || []).map((value: string) => value.replace(/^URL:/, '')) }
      }
      if (status.task.status === 'failed') throw new Error(status.task.error || '图片生成失败')
      await sleep(3000)
    }
    throw new Error('图片仍在后台生成，请到历史记录继续查看')
  }

  async function generateText(payload: Record<string, any>) {
    const rawBody = payload.requestBody && typeof payload.requestBody === 'object'
      ? payload.requestBody
      : {
          model: payload.model || runtime?.text.model || '',
          messages: Array.isArray(payload.messages) ? payload.messages : [],
          temperature: payload.params?.temperature,
          max_tokens: payload.params?.maxTokens || payload.params?.max_tokens
        }
    const messages = Array.isArray(rawBody.messages) ? rawBody.messages : []
    if (!messages.length) throw new Error('请输入需要处理的文本')
    const systemText = JSON.stringify(messages[0]?.content || '')
    const feature = Array.isArray(rawBody.tools) && rawBody.tools.length
      ? 'canvas_agent'
      : systemText.includes('反推提示词')
        ? 'canvas_reverse_prompt'
        : 'canvas_prompt_optimize'
    const result = await generateCanvasText({
      protocol: String(payload.protocol || 'openai-chat-completions'),
      model: String(payload.model || rawBody.model || ''),
      requestBody: rawBody,
      feature
    })
    const message = result.response?.choices?.[0]?.message || {}
    const content = typeof message.content === 'string'
      ? message.content
      : Array.isArray(message.content)
        ? message.content.filter((part: any) => part?.type === 'text').map((part: any) => String(part.text || '')).join('')
        : ''
    return { response: result.response, text: content }
  }

  function selectVideoModel(config: VideoPublicConfig, payload: Record<string, any>) {
    const requested = String(payload.modelCode || '').trim()
    return config.model_configs.find(model => model.code === requested && model.enabled)
      || config.model_configs.find(model => model.code === config.default_model && model.enabled)
      || config.model_configs.find(model => model.enabled)
  }

  async function generateVideo(payload: Record<string, any>, requestId: string, submitOnly = false) {
    const prompt = String(payload.prompt || '').trim()
    if (prompt.length < 4) throw new Error('视频提示词至少需要 4 个字符')
    const config = await getVideoConfig()
    if (!config.enabled) throw new Error('管理员暂未开启视频供应商')
    const model = selectVideoModel(config, payload)
    if (!model) throw new Error('当前没有可用的视频模型')
    const params = payload.params && typeof payload.params === 'object' ? payload.params : {}
    const duration = nearestValue(model.durations, numberValue(params.seconds, model.default_duration_seconds), model.default_duration_seconds)
    const resolution = normalizeVideoResolution(params.resolution || params.vquality, model)
    const aspectRatio = normalizeVideoRatio(params.ratio || params.size, model)
    const enabledVariants = model.variants.filter(item => item.enabled)
    const sameResolution = enabledVariants.filter(item => item.resolution === resolution)
    const variantPool = sameResolution.length ? sameResolution : enabledVariants
    const variant = variantPool.reduce<VideoModelConfig['variants'][number] | undefined>((best, item) => {
      if (!best) return item
      return Math.abs(item.duration_seconds - duration) < Math.abs(best.duration_seconds - duration) ? item : best
    }, undefined)
    if (!variant) throw new Error('该视频模型当前没有可用规格')
    const generateAudio = model.audio_toggle && String(params.generateAudio ?? 'true') !== 'false'
    const balance = await currentBalance()
    const chargedPrice = config.mock_mode ? 0 : variant.price
    if (billingEnabled && (balance === null || balance < chargedPrice)) throw new Error(`积分不足，本次生成需要 ${chargedPrice} 积分`)
    const script = await previewVideoScript({
      prompt,
      modelCode: model.code,
      durationSeconds: variant.duration_seconds,
      resolution: variant.resolution,
      aspectRatio,
      generateAudio,
      referenceCount: Array.isArray(payload.images) ? Math.min(payload.images.length, model.max_reference_images) : 0
    })
    const confirmed = await requestConfirmation({
      title: '确认生成视频',
      description: script.summary || '脚本已按商品连续性规则优化，任务将进入 PicPilot 视频队列。',
      facts: [
        { label: '模型', value: model.name },
        { label: '规格', value: `${aspectRatio} · ${variant.resolution} · ${variant.duration_seconds} 秒` },
        { label: '音轨', value: generateAudio ? '同步生成' : '不生成' }
      ],
      cost: chargedPrice,
      balance,
      busyLabel: '正在提交视频任务'
    })
    if (!confirmed) throw new Error(BRIDGE_CANCELLED)

    try {
      const references = await sourceFiles(payload.images, model.max_reference_images)
      const created = await createVideoTask({
        idempotencyKey: `canvas-${requestId}`,
        title: `画布视频 · ${model.name}`,
        prompt: script.optimized_prompt,
        originalPrompt: prompt,
        aspectRatio,
        modelCode: model.code,
        durationSeconds: variant.duration_seconds,
        resolution: variant.resolution,
        generateAudio,
        images: references,
        maxReferenceImages: model.max_reference_images
      })
      await currentBalance().catch(() => undefined)
      if (submitOnly) return { id: created.id }
      for (let attempt = 0; attempt < 600; attempt += 1) {
        const task = attempt === 0 ? created : await getVideoTask(created.id)
        if (task.status === 'completed' && task.result_url) return { url: assetUrl(task.result_url), taskId: task.id }
        if (TERMINAL_VIDEO_STATUSES.has(task.status) && task.status !== 'completed') {
          throw new Error(task.safe_error || (billingEnabled ? '视频生成失败，积分将按原有规则退款' : t('generationFailedPersonal')))
        }
        if (!ACTIVE_VIDEO_STATUSES.has(task.status) && task.status !== 'completed') {
          throw new Error(task.safe_error || `视频任务状态异常：${task.status}`)
        }
        await sleep(3000)
      }
      throw new Error('视频仍在后台生成，请到视频历史记录继续查看')
    } finally {
      setConfirmBusy(false)
      setConfirmation(null)
    }
  }

  async function processBridgeRequest(message: BridgeEnvelope) {
    const payload = message.payload || {}
    if (message.action === 'runtime.read') {
      const [me, capabilities, video, billing] = await Promise.all([getMe(), getAiCapabilities(), getVideoConfig(), getOptionalBillingSummary()])
      const value: BridgeRuntime = { userScope: me.id, edition: publicConfig.edition, billingEnabled, credits: billing?.credits ?? null, capabilities, video: { enabled: video.enabled, model_configs: video.model_configs.map(model => ({ ...model })) } }
      return value
    }
    if (message.action === 'nova.request') return forwardNovaRequest(payload as NovaRequest)
    if (message.action === 'video.submit') {
      if (!/^[\x21-\x7e]{1,100}$/.test(String(payload.idempotencyKey || ''))) throw new Error('A stable video submission key is required')
      return generateVideo(payload, payload.idempotencyKey, true)
    }
    if (message.action === 'video.status') return getVideoTask(String(payload.taskId || ''))
    if (message.action === 'settings.open') { window.location.assign(publicConfig.edition === 'personal' ? '/providers' : '/profile'); return null }
    throw new Error('该画布操作尚未接入 PicPilot')
  }

  bridgeHandler.current = processBridgeRequest

  useEffect(() => {
    function sendConfig(target: Window, origin: string, value: RuntimeConfig) {
      target.postMessage({ protocol: BRIDGE_PROTOCOL, type: 'picpilot:canvas:config', config: value }, origin)
    }

    function onMessage(event: MessageEvent<BridgeEnvelope>) {
      const frameOption = options.find(option => frameRefs.current[option.id]?.contentWindow === event.source)
      if (!frameOption) return
      const frameWindow = frameRefs.current[frameOption.id]?.contentWindow
      const frameUrl = frameUrls[frameOption.id]
      if (!frameWindow || !frameUrl) return
      const allowedOrigin = window.location.origin
      if (!allowedOrigin || event.origin !== allowedOrigin) return
      const message = event.data
      if (!message || message.protocol !== BRIDGE_PROTOCOL) return
      if (message.type === 'picpilot:canvas:ready') {
        if (runtime) sendConfig(frameWindow, allowedOrigin, runtime)
        else void refreshRuntime().then(value => sendConfig(frameWindow, allowedOrigin, value)).catch(() => undefined)
        return
      }
      if (message.type === 'picpilot:canvas:ui-ready') {
        markFrameReady(frameOption.id)
        return
      }
      if (message.type !== 'request' || !message.requestId || message.app !== frameOption.id) return
      if (!/^[a-zA-Z0-9_-]{1,128}$/.test(message.requestId)) return
      markFrameReady(frameOption.id)
      const requestId = message.requestId
      const key = `${frameOption.id}:${requestId}`
      const session = getToken()
      let running = bridgeRequests.current.get(key)
      if (!running) {
        if (bridgeRequests.current.size > 256) return
        const handler = bridgeHandler.current
        if (!handler) return
        running = handler(message)
        bridgeRequests.current.set(key, running)
        const cleanup = () => window.setTimeout(() => bridgeRequests.current.delete(key), 60_000)
        void running.then(cleanup, cleanup)
      }
      void running
        .then(data => { if (session !== getToken()) throw new Error('PicPilot session changed'); frameWindow.postMessage({ protocol: BRIDGE_PROTOCOL, type: 'response', app: frameOption.id, requestId, ok: true, data }, allowedOrigin) })
        .catch(error => frameWindow.postMessage({ protocol: BRIDGE_PROTOCOL, type: 'response', app: frameOption.id, requestId, ok: false, error: statusError(error, '任务处理失败') }, allowedOrigin))
    }

    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [frameUrls, refreshRuntime, runtime])

  useEffect(() => {
    if (!runtime) return
    options.forEach(option => {
      const frameWindow = frameRefs.current[option.id]?.contentWindow
      const frameUrl = frameUrls[option.id]
      if (!loadedFrames[option.id] || !frameWindow || !frameUrl) return
      frameWindow.postMessage({ protocol: BRIDGE_PROTOCOL, type: 'config', config: runtime }, window.location.origin)
    })
  }, [frameUrls, loadedFrames, runtime])

  return (
    <section className="canvas-lab" aria-label="AI 画布工作区">
      <header className="canvas-lab-toolbar">
        <div className="canvas-lab-heading">
          <strong>AI 画布</strong>
          <span>实验功能</span>
        </div>
        <div className="canvas-lab-switch" role="tablist" aria-label="选择画布实现">
          {options.map(option => (
            <button
              key={option.id}
              type="button"
              role="tab"
              aria-selected={selected === option.id}
              className={selected === option.id ? 'is-active' : ''}
              onClick={() => {
                setMountedFrames(current => current[option.id] ? current : { ...current, [option.id]: true })
                setSelected(option.id)
              }}
            >
              {option.shortLabel}
            </button>
          ))}
        </div>
        <div className="canvas-lab-actions">
          <span className="canvas-lab-capability" title="图片任务使用 PicPilot 统一路由"><ImageIcon size={14} />图片</span>
          <span className="canvas-lab-capability" title="视频模型由管理员后台启停"><Video size={14} />{runtime?.video.enabled ? '视频' : '视频未开启'}</span>
          <BillingOnly><a className="canvas-lab-balance" href="/credits" title="查看积分明细"><Coins size={15} />{runtime?.credits ?? '--'}</a></BillingOnly>
          <button type="button" onClick={() => void refreshRuntime().catch(() => undefined)} title={t('providerRefresh')}><RefreshCw size={17} /></button>
          <button type="button" onClick={() => document.querySelector('.canvas-lab-frame-wrap')?.requestFullscreen()} title="全屏"><Expand size={17} /></button>
        </div>
      </header>
      <div className="canvas-lab-frame-wrap">
        {runtimeError ? <div className="canvas-lab-runtime-error"><AlertCircle size={16} />{runtimeError}</div> : null}
        {!loadedFrames[selected] ? (
          <div className="canvas-lab-loading" aria-live="polite">
            <LoaderCircle size={24} />
            <strong>正在打开 {active.label}</strong>
          </div>
        ) : null}
        {options.map(option => mountedFrames[option.id] && frameUrls[option.id] ? (
          <iframe
            ref={element => { frameRefs.current[option.id] = element }}
            key={option.id}
            className={`canvas-lab-frame${selected === option.id ? ' is-active' : ' is-hidden'}${loadedFrames[option.id] ? ' is-ready' : ''}`}
            src={frameUrls[option.id]}
            title={option.label}
            aria-hidden={selected !== option.id}
            referrerPolicy="strict-origin"
            sandbox="allow-scripts allow-same-origin allow-forms allow-downloads allow-modals"
            allow="clipboard-read; clipboard-write; fullscreen"
          />
        ) : null)}
      </div>
      {confirmation && typeof document !== 'undefined' ? createPortal((
        <div
          className="canvas-confirm-backdrop"
          role="presentation"
          onMouseDown={event => {
            if (event.target === event.currentTarget && !confirmBusy) resolveConfirmation(false)
          }}
        >
          <section className="canvas-confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="canvas-confirm-title">
            <div className="canvas-confirm-head">
              <div className="canvas-confirm-icon"><CheckCircle2 size={21} /></div>
              <div>
                <h2 id="canvas-confirm-title">{confirmation.title}</h2>
                <p>{confirmation.description}</p>
              </div>
            </div>
            <div className="canvas-confirm-facts">
              {confirmation.facts.map(item => <div key={item.label}><span>{item.label}</span><strong>{item.value}</strong></div>)}
            </div>
            <BillingOnly><div className="canvas-confirm-billing">
              <div><span>本次预计扣除</span><strong>{confirmation.cost} 积分</strong></div>
              <div><span>当前余额</span><strong>{confirmation.balance} 积分</strong></div>
              <div><span>预计剩余</span><strong>{confirmation.balance === null ? '--' : Math.max(0, confirmation.balance - confirmation.cost)} 积分</strong></div>
            </div></BillingOnly>
            <BillingOnly><p className="canvas-confirm-note">失败任务沿用 PicPilot 现有退款规则；重复回调不会重复扣费或退款。</p></BillingOnly>
            <div className="canvas-confirm-actions">
              <button type="button" className="btn btn-secondary" disabled={confirmBusy} onClick={() => resolveConfirmation(false)}>取消</button>
              <button type="button" className="btn btn-primary" disabled={confirmBusy} onClick={() => resolveConfirmation(true)}>
                {confirmBusy ? <><LoaderCircle size={16} />{confirmation.busyLabel}</> : billingEnabled ? `确认并扣除 ${confirmation.cost} 积分` : t('confirmSubmit')}
              </button>
            </div>
          </section>
        </div>
      ), document.body) : null}
    </section>
  )
}
