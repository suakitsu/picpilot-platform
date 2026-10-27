'use client'
import { useI18n } from '@/components/i18n'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  Clock3,
  Image as ImageIcon,
  ImagePlus,
  Layers,
  LockKeyhole,
  RefreshCw,
  Sparkles,
  WandSparkles,
  X
} from 'lucide-react'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { currentLanguage } from '@/components/i18n'
import { UserAuthGate } from '@/components/UserAuthGate'
import {
  assetUrl,
  ApiError,
  CommerceCatalog,
  CommercePlatformPreset,
  CommerceTemplatePreset,
  generateCommerceSuite,
  getOptionalBillingSummary,
  getCommerceSuiteCatalog,
  getProject,
  ImageTaskView,
  listProjects,
  ProjectSummary,
  requireLogin,
  retryCommerceSuiteTask
} from '@/lib/api'
import { createImagePreviewUrl, revokeImagePreviewUrls } from '@/lib/imagePreview'
import { hasActiveImageTasks } from '@/lib/imageTaskStatus'

const RESOLUTION_OPTIONS = ['1K', '2K', '4K'] as const
const COST_BY_RESOLUTION: Record<string, number> = { '1K': 6, '2K': 24, '4K': 30 }
const MAX_REFERENCE_IMAGES = 10
const MAX_REFERENCE_FILE_BYTES = 25 * 1024 * 1024
const MAX_REFERENCE_TOTAL_BYTES = 28 * 1024 * 1024
const WORKFLOW_STEPS = ['商品与素材', '平台与套图', '输出确认']
const TIKTOK_MARKETS = [
  { code: 'US', name: '美国', language: 'en-US' },
  { code: 'GB', name: '英国', language: 'en-GB' },
  { code: 'DE', name: '德国', language: 'de-DE' },
  { code: 'FR', name: '法国', language: 'fr-FR' },
  { code: 'IT', name: '意大利', language: 'it-IT' },
  { code: 'ES', name: '西班牙', language: 'es-ES' },
  { code: 'IE', name: '爱尔兰', language: 'en-IE' },
  { code: 'NL', name: '荷兰', language: 'nl-NL' },
  { code: 'BE', name: '比利时', language: 'nl-BE' },
  { code: 'PL', name: '波兰', language: 'pl-PL' },
  { code: 'ID', name: '印尼', language: 'id-ID' },
  { code: 'TH', name: '泰国', language: 'th-TH' },
  { code: 'VN', name: '越南', language: 'vi-VN' },
  { code: 'MY', name: '马来西亚', language: 'ms-MY' },
  { code: 'PH', name: '菲律宾', language: 'en-PH' },
  { code: 'SG', name: '新加坡', language: 'en-SG' },
  { code: 'JP', name: '日本', language: 'ja-JP' },
  { code: 'MX', name: '墨西哥', language: 'es-MX' },
  { code: 'BR', name: '巴西', language: 'pt-BR' }
]

function splitPoints(value: string) {
  return value
    .split(/[\n,，;；、|｜]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .slice(0, 8)
}

function statusLabel(status: string) {
  if (status === 'queued') return '排队中'
  if (status === 'running') return '生成中'
  if (status === 'success') return '已完成'
  if (status === 'failed') return '失败'
  return status
}

function statusClass(status: string) {
  if (status === 'success') return 'status status-ok'
  if (status === 'failed') return 'status status-bad'
  return 'status status-warn'
}

function selectedPlatform(catalog: CommerceCatalog | null, code: string) {
  return catalog?.platforms.find(item => item.code === code) || catalog?.platforms[0] || null
}

function defaultCountryForPlatform(item: CommercePlatformPreset) {
  return item.code === 'tiktok_shop' ? TIKTOK_MARKETS[0].code : item.code
}

function defaultLanguageForPlatform(item: CommercePlatformPreset) {
  return item.code === 'tiktok_shop' ? TIKTOK_MARKETS[0].language : (item.language || currentLanguage())
}

export function CommerceSuiteWorkflow() {
  const t = useI18n()
  const { billingEnabled } = useRuntime()
  const [catalog, setCatalog] = useState<CommerceCatalog | null>(null)
  const [platformCode, setPlatformCode] = useState('')
  const [selectedSlots, setSelectedSlots] = useState<string[]>([])
  const [resolution, setResolution] = useState<'1K' | '2K' | '4K'>('1K')
  const [productName, setProductName] = useState('')
  const [category, setCategory] = useState('')
  const [brand, setBrand] = useState('')
  const [sellingPoints, setSellingPoints] = useState('')
  const [material, setMaterial] = useState('')
  const [specs, setSpecs] = useState('')
  const [audience, setAudience] = useState('')
  const [scenario, setScenario] = useState('')
  const [priceText, setPriceText] = useState('')
  const [promoText, setPromoText] = useState('')
  const [country, setCountry] = useState('')
  const [language, setLanguage] = useState('')
  const [references, setReferences] = useState<File[]>([])
  const [referenceUrl, setReferenceUrl] = useState('')
  const [referencePreviews, setReferencePreviews] = useState<string[]>([])
  const [referencePreviewsLoading, setReferencePreviewsLoading] = useState(false)
  const [credits, setCredits] = useState<number | null>(null)
  const [project, setProject] = useState<ProjectSummary | null>(null)
  const [tasks, setTasks] = useState<ImageTaskView[]>([])
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [workflowStep, setWorkflowStep] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [lockVisualIdentity, setLockVisualIdentity] = useState(true)
  const submittingRef = useRef(false)

  useEffect(() => {
    if (requireLogin()) refreshCredits()
    getCommerceSuiteCatalog()
      .then(data => {
        setCatalog(data)
        const first = data.platforms.find(item => item.enabled && !item.beta) || data.platforms.find(item => item.enabled) || data.platforms[0]
        if (first) {
          setPlatformCode(first.code)
          setSelectedSlots(first.default_slots || [])
          setResolution((first.default_resolution as '1K' | '2K' | '4K') || '1K')
          setLanguage(defaultLanguageForPlatform(first))
          setCountry(defaultCountryForPlatform(first))
        }
      })
      .catch(error => setMessage(error instanceof Error ? error.message : '套图模板加载失败'))
  }, [])

  const platform = useMemo(() => selectedPlatform(catalog, platformCode), [catalog, platformCode])
  const templates = useMemo(() => {
    if (!catalog || !platform) return []
    return catalog.templates
      .filter(item => item.platform === platform.code && item.enabled)
      .sort((left, right) => (left.sort_order || 0) - (right.sort_order || 0))
  }, [catalog, platform])
  const slotMap = useMemo(() => new Map((catalog?.slots || []).map(slot => [slot.code, slot])), [catalog])
  const estimate = selectedSlots.length * (catalog?.cost_rule?.[resolution] || COST_BY_RESOLUTION[resolution])
  const hasActiveTask = hasActiveImageTasks(tasks)
  const canSubmit = Boolean(productName.trim() && selectedSlots.length && !loading && !hasActiveTask)
  const referenceCount = references.length + (referenceUrl.trim() ? 1 : 0)
  const primaryPreview = referencePreviews.find(Boolean) || referenceUrl.trim()
  const isFinalStep = workflowStep === WORKFLOW_STEPS.length - 1
  const canAdvance = workflowStep === 0
    ? Boolean(productName.trim() && !loading)
    : workflowStep === 1
      ? Boolean(selectedSlots.length && !loading)
      : canSubmit
  const primaryActionLabel = workflowStep === 0
    ? '下一步：平台与套图'
    : workflowStep === 1
      ? '下一步：输出确认'
      : hasActiveTask ? '当前任务生成中' : '确认并生成'

  useEffect(() => {
    if (!project || !hasActiveTask) return
    const timer = window.setInterval(refreshProject, 7000)
    return () => window.clearInterval(timer)
  }, [project?.id, hasActiveTask])

  useEffect(() => {
    if (hasActiveTask) setConfirmOpen(false)
  }, [hasActiveTask])

  useEffect(() => {
    let cancelled = false
    const previews = Array.from({ length: references.length }, () => '')
    const createdUrls: string[] = []
    setReferencePreviews(previews)
    setReferencePreviewsLoading(references.length > 0)

    async function buildPreviews() {
      let cursor = 0
      const worker = async () => {
        while (!cancelled) {
          const index = cursor
          cursor += 1
          if (index >= references.length) return
          const url = await createImagePreviewUrl(references[index])
          if (cancelled) {
            if (url) URL.revokeObjectURL(url)
            return
          }
          previews[index] = url
          if (url) createdUrls.push(url)
          setReferencePreviews([...previews])
        }
      }
      await Promise.all([worker(), worker()])
      if (!cancelled) setReferencePreviewsLoading(false)
    }

    void buildPreviews()
    return () => {
      cancelled = true
      revokeImagePreviewUrls(createdUrls)
    }
  }, [references])

  useEffect(() => {
    if (!confirmOpen) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !loading) setConfirmOpen(false)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [confirmOpen, loading])

  function choosePlatform(item: CommercePlatformPreset) {
    setPlatformCode(item.code)
    setSelectedSlots(item.default_slots || [])
    setResolution((item.default_resolution as '1K' | '2K' | '4K') || '1K')
    setLanguage(defaultLanguageForPlatform(item))
    setCountry(defaultCountryForPlatform(item))
  }

  function chooseTikTokMarket(code: string) {
    const market = TIKTOK_MARKETS.find(item => item.code === code) || TIKTOK_MARKETS[0]
    setCountry(market.code)
    setLanguage(market.language)
  }

  function toggleSlot(slot: string) {
    setSelectedSlots(current => current.includes(slot) ? current.filter(item => item !== slot) : [...current, slot])
  }

  function resetRecommendedPlan() {
    if (!platform) return
    setSelectedSlots(platform.default_slots?.length ? platform.default_slots : templates.map(item => item.slot))
    setMessage('已按当前平台恢复推荐图种。')
  }

  function goToNextStep() {
    if (workflowStep === 0 && !productName.trim()) {
      setMessage('请先填写商品名称。')
      return
    }
    if (workflowStep === 1 && !selectedSlots.length) {
      setMessage('请至少选择一种输出图片。')
      return
    }
    setMessage('')
    setWorkflowStep(step => Math.min(WORKFLOW_STEPS.length - 1, step + 1))
  }

  function runPrimaryAction() {
    if (isFinalStep) {
      requestConfirmation()
      return
    }
    goToNextStep()
  }

  function requestConfirmation() {
    if (!productName.trim()) {
      setMessage('请先填写商品名称。')
      return
    }
    if (!selectedSlots.length) {
      setMessage('请至少选择一种输出图片。')
      return
    }
    if (hasActiveTask) {
      setMessage('当前套图仍在排队或生成中，请完成后再创建新任务。')
      return
    }
    if (credits !== null && credits < estimate) {
      setMessage('积分不足，请先充值或兑换积分。')
      return
    }
    setMessage('')
    setConfirmOpen(true)
  }

  function selectReferences(files: File[]) {
    const existing = new Set(references.map(file => `${file.name}:${file.size}:${file.lastModified}`))
    const next = [...references]
    let totalBytes = next.reduce((sum, file) => sum + file.size, 0)
    let rejectedByCount = false
    let rejectedBySize = false
    files.forEach(file => {
      const key = `${file.name}:${file.size}:${file.lastModified}`
      if (existing.has(key)) return
      existing.add(key)
      if (next.length >= MAX_REFERENCE_IMAGES) {
        rejectedByCount = true
        return
      }
      if (file.size > MAX_REFERENCE_FILE_BYTES || totalBytes + file.size > MAX_REFERENCE_TOTAL_BYTES) {
        rejectedBySize = true
        return
      }
      next.push(file)
      totalBytes += file.size
    })
    setReferences(next)
    if (rejectedByCount) {
      setMessage(`商品参考图最多上传 ${MAX_REFERENCE_IMAGES} 张，超出的图片未加入。`)
    } else if (rejectedBySize) {
      setMessage('单张图片不能超过 25MB，全部参考图合计不能超过 28MB。')
    }
  }

  function removeReference(index: number) {
    setReferences(current => current.filter((_, itemIndex) => itemIndex !== index))
  }

  async function refreshCredits() {
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => setCredits(null))
  }

  async function submit() {
    if (!requireLogin() || !platform || !confirmOpen || !canSubmit || submittingRef.current) return
    if (credits !== null && credits < estimate) {
      setMessage('积分不足，请先充值或兑换积分。')
      return
    }
    setConfirmOpen(false)
    submittingRef.current = true
    setLoading(true)
    setMessage('')
    const submittedAt = Date.now()
    try {
      const response = await generateCommerceSuite({
        platform: platform.code,
        productInfo: {
          product_name: productName.trim(),
          category: category.trim(),
          brand: brand.trim(),
          selling_points: splitPoints(sellingPoints),
          material: material.trim(),
          specs: specs.trim(),
          audience: audience.trim(),
          scenario: scenario.trim(),
          price_text: priceText.trim(),
          promo_text: promoText.trim(),
          country: country.trim() || platform.code,
          language: language.trim() || platform.language
        },
        selectedSlots,
        resolution,
        references,
        referenceUrl: referenceUrl.trim(),
        overlayOptions: { preserve_visual_identity: lockVisualIdentity }
      })
      setProject(response.project.project)
      setTasks(response.project.image_tasks)
      window.history.replaceState(null, '', `/commerce?tab=suite&projectId=${response.project_id}`)
      setMessage(response.mock_mode ? 'MOCK_AI 已生成套图占位结果。' : '套图任务已提交，正在生成。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      if (error instanceof ApiError && ['REQUEST_TIMEOUT', '502', '503', '504'].includes(error.code)) {
        setMessage('提交响应超时，正在确认后台是否已创建任务，请勿重复提交。')
        const recovered = await recoverTimedOutSubmit(productName.trim(), submittedAt)
        if (!recovered) setMessage(error.message)
      } else {
        setMessage(error instanceof Error ? error.message : '提交失败，请稍后重试。')
      }
    } finally {
      submittingRef.current = false
      setLoading(false)
      refreshCredits()
    }
  }

  async function recoverTimedOutSubmit(expectedProductName: string, submittedAt: number) {
    for (let attempt = 0; attempt < 4; attempt += 1) {
      try {
        const rows = await listProjects({ timeoutMs: 5_000 })
        const candidate = rows
          .filter(item => item.product_name === expectedProductName && new Date(item.created_at).getTime() >= submittedAt - 60_000)
          .sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime())[0]
        if (candidate) {
          const detail = await getProject(candidate.id, { timeoutMs: 5_000 })
          setProject(detail.project)
          setTasks(detail.image_tasks)
          window.history.replaceState(null, '', `/commerce?tab=suite&projectId=${candidate.id}`)
          setMessage('提交响应虽已超时，但后台任务已找到，正在生成，请勿重复提交。')
          window.dispatchEvent(new Event('openimage:credits-updated'))
          return true
        }
      } catch {
        // Keep polling briefly because the timed-out transaction may still be committing.
      }
      if (attempt < 3) await new Promise(resolve => window.setTimeout(resolve, 5000))
    }
    return false
  }

  async function refreshProject() {
    if (!project) return
    try {
      const detail = await getProject(project.id)
      setProject(detail.project)
      setTasks(detail.image_tasks)
      const failed = detail.image_tasks.find(task => task.status === 'failed')
      if (failed) setMessage(failed.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '刷新失败')
    }
  }

  async function retry(task: ImageTaskView) {
    setLoading(true)
    try {
      const updated = await retryCommerceSuiteTask(task.id)
      setTasks(rows => rows.map(item => item.id === task.id ? updated : item))
      setProject(current => current ? { ...current, status: 'images_queued' } : current)
      setMessage('已按原平台和图种重新入队。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '重试失败')
    } finally {
      setLoading(false)
      refreshCredits()
    }
  }

  return (
    <UserAuthGate title="登录后生成电商套图" description={t('generationConfirmPersonal')}>
      <div className="suite-page suite-flow-page">
        {message ? <div className={message.includes('失败') || message.includes('不足') ? 'notice notice-error' : 'notice'}>{message}</div> : null}
        <section className="suite-flow-intro">
          <div>
            <span className="suite-flow-kicker"><WandSparkles size={15} />商品套图</span>
            <h2>三步完成一套平台商品图</h2>
            <p>先整理商品与参考素材，再选择平台和图种，最后核对输出规格。</p>
          </div>
          <span className="suite-flow-saved"><CheckCircle2 size={16} />3 步创建 · 内容实时保留</span>
        </section>

        <div className="suite-flow-layout">
          <section className="panel suite-flow-main">
            <header className="suite-flow-toolbar">
              <div>
                <strong>{productName.trim() || '新商品套图'}</strong>
                <span>{platform?.name || '正在加载平台'} · {selectedSlots.length} 张输出</span>
              </div>
              <span className="suite-step-count">第 {workflowStep + 1} / {WORKFLOW_STEPS.length} 步</span>
            </header>

            <div className="suite-professional">
              <nav className="suite-professional-steps suite-workflow-steps" aria-label="商品套图创建步骤">
                {WORKFLOW_STEPS.map((label, index) => (
                  <button key={label} type="button" className={workflowStep === index ? 'active' : workflowStep > index ? 'done' : ''} onClick={() => setWorkflowStep(index)}>
                    <span>{workflowStep > index ? <Check size={13} /> : index + 1}</span>{label}
                  </button>
                ))}
              </nav>

              <div className="suite-professional-body">
                {workflowStep === 0 ? (
                  <>
                    <header className="suite-flow-section-head compact">
                      <span>01 / 商品与素材</span>
                      <h3>先把商品主体和真实信息说清楚</h3>
                      <p>参考图用于保持商品结构、颜色和材质一致；不确定的信息可以留空。</p>
                    </header>
                    <div className="form-grid">
                      <div className="grid-2">
                        <label className="field"><span>商品名称 *</span><input className="input" value={productName} onChange={event => setProductName(event.target.value)} placeholder="例如：轻量折叠晴雨伞" /></label>
                        <label className="field"><span>类目</span><input className="input" value={category} onChange={event => setCategory(event.target.value)} placeholder="例如：家居日用 / 雨具 / 折叠伞" /></label>
                      </div>
                      <div className="suite-reference-zone">
                        <ImageReferenceInput
                          label="商品参考图"
                          hint={`最多 ${MAX_REFERENCE_IMAGES} 张、合计不超过 28MB；建议包含主图、不同角度和细节图`}
                          multiple
                          file={references[0] || null}
                          files={references}
                          previewUrls={referencePreviews}
                          previewProcessing={referencePreviewsLoading}
                          url={referenceUrl}
                          previewUrl={!referencePreviews.length ? referenceUrl : ''}
                          onFile={file => selectReferences([file])}
                          onFiles={selectReferences}
                          onRemoveFile={removeReference}
                          onClearFile={() => setReferences([])}
                          onUrl={setReferenceUrl}
                          uploadLabel={references.length ? '继续添加' : '选择图片'}
                          removeLabel="清空"
                          linkLabel="图片链接"
                          placeholder="https://example.com/product.jpg"
                        />
                      </div>
                      <div className="suite-subsection-head">
                        <strong>完善商品信息</strong>
                        <span>这些内容会直接影响卖点、场景和细节图的准确度。</span>
                      </div>
                      <div className="grid-2">
                        <label className="field"><span>品牌</span><input className="input" value={brand} onChange={event => setBrand(event.target.value)} placeholder="可选" /></label>
                        <label className="field"><span>材质</span><input className="input" value={material} onChange={event => setMaterial(event.target.value)} placeholder="只填写确认过的材质" /></label>
                      </div>
                      <label className="field"><span>核心卖点</span><textarea className="textarea" value={sellingPoints} onChange={event => setSellingPoints(event.target.value)} placeholder="每行一个卖点，例如：晴雨两用 / 轻量折叠 / 加固伞骨" /></label>
                      <div className="grid-2">
                        <label className="field"><span>规格/参数</span><input className="input" value={specs} onChange={event => setSpecs(event.target.value)} placeholder="只填已确认参数" /></label>
                        <label className="field"><span>适用人群</span><input className="input" value={audience} onChange={event => setAudience(event.target.value)} placeholder="例如：通勤人群、户外用户" /></label>
                      </div>
                      <label className="field"><span>使用场景</span><input className="input" value={scenario} onChange={event => setScenario(event.target.value)} placeholder="例如：雨天街头、地铁口、通勤收纳" /></label>
                    </div>
                  </>
                ) : null}

                {workflowStep === 1 ? (
                  <>
                    <header className="suite-flow-section-head compact">
                      <span>02 / 平台与套图</span>
                      <h3>选择上架平台和这次需要的图片</h3>
                      <p>切换平台会同步推荐图种、默认尺寸和文案语言，图片仍可按需增删。</p>
                    </header>
                    <div className="suite-platform-picker suite-platform-picker-block" aria-label="目标平台">
                      <span>目标平台</span>
                      {(catalog?.platforms || []).map(item => (
                        <button key={item.code} type="button" className={platformCode === item.code ? 'active' : ''} disabled={!item.enabled} onClick={() => choosePlatform(item)}>
                          {item.name}{item.beta ? ' Beta' : ''}
                        </button>
                      ))}
                    </div>
                    <div className="grid-2 suite-market-fields">
                      {platform?.code === 'tiktok_shop' ? (
                        <label className="field">
                          <span>目标国家</span>
                          <select className="input" value={country} onChange={event => chooseTikTokMarket(event.target.value)}>
                            {TIKTOK_MARKETS.map(item => <option key={item.code} value={item.code}>{item.name} / {item.language}</option>)}
                          </select>
                        </label>
                      ) : (
                        <label className="field"><span>国家/站点</span><input className="input" value={country} onChange={event => setCountry(event.target.value)} placeholder={platform?.code || 'CN'} /></label>
                      )}
                      <label className="field"><span>图中文字语言</span><input className="input" value={language} onChange={event => setLanguage(event.target.value)} readOnly={platform?.code === 'tiktok_shop'} placeholder={platform?.language || currentLanguage()} /></label>
                    </div>
                    <section className="suite-plan suite-plan-unified">
                      <header>
                        <span><Sparkles size={17} /><strong>{platform?.name || '平台'}推荐套图</strong></span>
                        <em>{selectedSlots.length} 张已选</em>
                      </header>
                      <div className="suite-output-grid">
                        {templates.length ? templates.map((template: CommerceTemplatePreset) => {
                          const slot = slotMap.get(template.slot)
                          const checked = selectedSlots.includes(template.slot)
                          return (
                            <button key={template.id} type="button" className={checked ? 'selected' : ''} onClick={() => toggleSlot(template.slot)}>
                              <span className="suite-slot-check">{checked ? <Check size={14} /> : <ImageIcon size={14} />}</span>
                              <span><strong>{slot?.name || template.name}</strong><small>{slot?.description || template.description}</small></span>
                            </button>
                          )
                        }) : <p className="muted">正在加载平台图种...</p>}
                      </div>
                      <footer>
                        <button className="btn btn-secondary" type="button" onClick={resetRecommendedPlan}><RefreshCw size={15} />恢复平台推荐</button>
                        <BillingOnly><span className="muted">积分按实际选择的图片数量计算</span></BillingOnly>
                      </footer>
                    </section>
                  </>
                ) : null}

                {workflowStep === 2 ? (
                  <>
                    <header className="suite-flow-section-head compact">
                      <span>03 / 输出确认</span>
                      <h3>确认清晰度、文案和本次消耗</h3>
                      <p>价格和促销文字只会在明确填写时进入任务，提交前仍可返回修改。</p>
                    </header>
                    <div className="form-grid">
                      <div className="field">
                        <span>输出清晰度</span>
                        <div className="suite-resolution">{RESOLUTION_OPTIONS.map(item => <button key={item} type="button" className={resolution === item ? 'suite-res-active' : ''} onClick={() => setResolution(item)}>{item}</button>)}</div>
                      </div>
                      <div className="grid-2">
                        <label className="field"><span>价格文字</span><input className="input" value={priceText} onChange={event => setPriceText(event.target.value)} placeholder="可选，必须真实合规" /></label>
                        <label className="field"><span>促销/角标</span><input className="input" value={promoText} onChange={event => setPromoText(event.target.value)} placeholder="例如：新品、限量、组合装" /></label>
                      </div>
                      <label className="suite-identity-option"><input type="checkbox" checked={lockVisualIdentity} onChange={event => setLockVisualIdentity(event.target.checked)} /><span><strong>统一主体与材质表达</strong><small>整套图片共享同一商品视觉约束</small></span></label>
                      <div className="suite-final-review">
                        <div><span>商品</span><strong>{productName.trim() || '未填写'}</strong></div>
                        <div><span>目标平台</span><strong>{platform?.name || '未选择'}</strong></div>
                        <div><span>参考素材</span><strong>{referenceCount} 张</strong></div>
                        <div><span>输出图片</span><strong>{selectedSlots.length} 张</strong></div>
                        <div><span>分辨率</span><strong>{resolution}</strong></div>
                        <BillingOnly><div className="cost"><span>预计消耗</span><strong>{estimate} 积分</strong></div></BillingOnly>
                      </div>
                    </div>
                  </>
                ) : null}
              </div>

              <footer className="suite-professional-actions">
                <button className="btn btn-secondary" type="button" disabled={workflowStep === 0} onClick={() => setWorkflowStep(step => Math.max(0, step - 1))}><ArrowLeft size={16} />上一步</button>
                <button className="btn btn-primary" type="button" disabled={!canAdvance} onClick={runPrimaryAction}>
                  {isFinalStep ? <Sparkles size={16} /> : null}
                  {primaryActionLabel}
                  {!isFinalStep ? <ArrowRight size={16} /> : null}
                </button>
              </footer>
            </div>
          </section>

          <aside className="panel suite-flow-summary" aria-label="生成摘要">
            <div className={primaryPreview ? 'suite-summary-preview has-image' : 'suite-summary-preview'}>
              {primaryPreview ? <img src={primaryPreview} alt="商品预览" decoding="async" /> : <span className="suite-summary-placeholder"><ImagePlus size={28} />{referencePreviewsLoading ? '正在生成轻量预览' : references.length ? '图片将在上传后处理' : '上传商品图后显示预览'}</span>}
              <em><LockKeyhole size={12} />主体约束</em>
            </div>
            <div className="suite-summary-product">
              <strong>{productName.trim() || '尚未填写商品名称'}</strong>
              <span>{platform?.name || '目标平台'} · {language || '跟随平台语言'}</span>
            </div>
            <div className="suite-summary-status">
              <span className={productName.trim() ? 'ready' : ''}><CheckCircle2 size={15} />{productName.trim() ? '商品信息已准备' : '等待填写商品名称'}</span>
              <span className={referenceCount ? 'ready' : ''}><CheckCircle2 size={15} />{referenceCount ? `${referenceCount} 张参考素材` : '尚未添加参考素材'}</span>
              <span className={selectedSlots.length ? 'ready' : ''}><CheckCircle2 size={15} />{selectedSlots.length} 张输出已规划</span>
            </div>
            <div className="suite-summary-resolution">
              <span>输出清晰度</span>
              <div className="suite-resolution">{RESOLUTION_OPTIONS.map(item => <button key={item} type="button" className={resolution === item ? 'suite-res-active' : ''} onClick={() => setResolution(item)}>{item}</button>)}</div>
            </div>
            <BillingOnly><div className="suite-cost-card">
              <span>本次预计消耗</span>
              <strong>{estimate} 积分</strong>
              <small>{selectedSlots.length} 张图片 · 当前余额 {credits ?? '--'} 积分</small>
            </div></BillingOnly>
            <button className="btn btn-primary btn-block suite-generate-button" disabled={!canAdvance} onClick={runPrimaryAction}>
              {loading ? <RefreshCw size={16} /> : isFinalStep ? <Sparkles size={16} /> : <ArrowRight size={16} />}
              {loading ? '提交中...' : primaryActionLabel}
            </button>
            {workflowStep > 0 ? <button className="btn btn-secondary btn-block" type="button" onClick={() => setWorkflowStep(step => Math.max(0, step - 1))}><ArrowLeft size={16} />返回上一步</button> : null}
            <p className="suite-summary-note"><Clock3 size={13} />当前第 {workflowStep + 1} 步；提交后可离开页面。<BillingOnly>失败单张自动退款。</BillingOnly></p>
          </aside>
        </div>

        <section className="panel mt-24">
          <div className="panel-head">
            <div>
              <h2>生成结果</h2>
              <p>{project ? `${project.product_name} · ${tasks.length} 张任务` : '提交后会按图种展示状态和预览结果。'}</p>
            </div>
            {project ? <div className="page-actions"><Link className="btn btn-secondary" href={`/projects/${project.id}#tasks`}>进入详情</Link><button className="btn btn-secondary" disabled={loading} onClick={refreshProject}>刷新</button></div> : null}
          </div>
          <div className="panel-body suite-result-grid">
            {tasks.length ? tasks.map(task => {
              return (
                <article className="suite-result-card" key={task.id} id={`task-${task.id}`}>
                  <div className="suite-result-media">
                    {task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} /> : <Layers size={34} />}
                  </div>
                  <div className="suite-result-body">
                    <div className="suite-result-title"><strong>{task.image_name}</strong><span className={statusClass(task.status)}>{statusLabel(task.status)}</span></div>
                    {task.safe_error ? <p className="suite-error">{task.safe_error}</p> : null}
                    <div className="muted"><BillingOnly>{task.credits_cost} 积分 · </BillingOnly>{task.image_type}</div>
                    <div className="generation-card-actions">
                      {task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">查看/下载</a> : null}
                      {task.status === 'failed' ? <button className="btn btn-primary" disabled={loading} onClick={() => retry(task)}>重试</button> : null}
                    </div>
                  </div>
                </article>
              )
            }) : <p className="muted">暂无套图任务。</p>}
          </div>
        </section>

        {confirmOpen ? (
          <div className="suite-confirm-backdrop" role="presentation" onMouseDown={event => {
            if (event.currentTarget === event.target && !loading) setConfirmOpen(false)
          }}>
            <section className="suite-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="suite-confirm-title">
              <header>
                <span><Sparkles size={20} /></span>
                <div><h2 id="suite-confirm-title">确认提交商品套图</h2><p>任务会在后台异步生成，提交后可以离开页面。</p></div>
                <button type="button" aria-label="关闭确认弹窗" disabled={loading} onClick={() => setConfirmOpen(false)}><X size={18} /></button>
              </header>
              <div className="suite-confirm-summary">
                <div><span>商品</span><strong>{productName.trim()}</strong></div>
                <div><span>目标平台</span><strong>{platform?.name || platformCode}</strong></div>
                <div><span>输出</span><strong>{selectedSlots.length} 张商品图</strong></div>
                <div><span>分辨率</span><strong>{resolution}</strong></div>
                <BillingOnly><div className="cost"><span>预计消耗</span><strong>{estimate} 积分</strong></div></BillingOnly>
              </div>
              <footer>
                <button className="btn btn-secondary" type="button" disabled={loading} onClick={() => setConfirmOpen(false)}>返回修改</button>
                <button className="btn btn-primary" type="button" disabled={loading} onClick={() => void submit()}>{loading ? <RefreshCw size={16} /> : <Sparkles size={16} />}{loading ? '提交中...' : '确认提交'}</button>
              </footer>
            </section>
          </div>
        ) : null}
      </div>
    </UserAuthGate>
  )
}
