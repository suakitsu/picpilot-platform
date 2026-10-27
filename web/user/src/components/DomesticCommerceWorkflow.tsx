'use client'
import { useI18n } from '@/components/i18n'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import { ArrowLeft, ArrowRight, RefreshCw, Sparkles, X } from 'lucide-react'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { TaskFlowSteps } from '@/components/TaskFlowSteps'
import { currentLanguage } from '@/components/i18n'
import { UserAuthGate } from '@/components/UserAuthGate'
import { assetUrl, createCreativeImage, getOptionalBillingSummary, getProject, ImageTaskView, ProjectSummary, requireLogin, retryProjectImage, suggestDomesticSellingPoints } from '@/lib/api'
import { IMAGE_SIZE_OPTIONS, imageCreditsForSize, imageSizeLabel } from '@/lib/imageSizes'
import { hasActiveImageTasks } from '@/lib/imageTaskStatus'
import { getTemplateById, templateIntentMeta } from '@/lib/templateCatalog'

const PLATFORM_OPTIONS = ['淘宝/天猫', '1688', '京东', '拼多多', '抖音电商', '快手电商', '小红书']
const USAGE_OPTIONS = ['主图', '卖点图', '场景图', '详情页模块', '直播封面', '短视频首帧', '小红书种草图']

const PLATFORM_PROMPT_PRESETS: Record<string, string[]> = {
  '淘宝/天猫': [
    '平台风格：淘宝/天猫品牌店视觉，干净精修、货架感强、商品主视觉突出，适合店铺首页、新品首发、活动承接页和详情页头图。',
    '视觉表达：偏品牌化、质感化与审美转化，背景可用柔和渐变、轻奢材质、生活化道具、季节氛围或棚拍精修感，但不要做成海外 Amazon Listing 风格。',
    '平台化表达：突出“上新感、品质感、氛围感、收藏欲、礼赠感、搭配感”，画面要像品牌店铺主视觉，而不是简单白底产品图。',
    '版面倾向：把主标题、核心利益点、活动角标、3 个卖点胶囊直接排版进画面，画面层次要适合手机端快速扫读。'
  ],
  '1688': [
    '平台风格：1688 偏批发采购、源头工厂、档口供货和 B2B 询盘转化，画面要直接、可信、货源感强，适合工厂店、档口货盘、源头厂家和批量采购场景。',
    '视觉表达：可使用工厂、仓储、打包、货架、档口、批量陈列、细节样品或多款同框，但商品主体必须清楚，不能做成低质杂乱的批发广告。',
    '平台化表达：突出“源头货、现货感、批量采购、款式齐全、供货稳定、工厂直供、拿样参考”，但不要生成未提供的价格、起订量、认证、包邮或承诺。',
    '版面倾向：把主标题、供货卖点、款式/规格提示和 2-3 个采购理由直接排版进画面，适合采购商快速判断是否询盘。'
  ],
  '京东': [
    '平台风格：京东偏可信、理性、参数清晰、品质感强，商品应显得可靠、清爽、专业，适合数码、家电、日百、工具和品质消费。',
    '视觉表达：可使用白底、浅灰、蓝白科技感、质检感、参数卡片区、干净棚拍布光或专业评测感，突出真实材质和规格信息承载位。',
    '平台化表达：突出“正品感、专业感、可靠感、参数清晰、细节可信、到手即用”，但只用画面表达，不生成未经确认的承诺。',
    '版面倾向：把参数、核心卖点、服务说明类文字直接排版进画面，但不要生成认证章、平台 UI、虚假保障、服务承诺或未经确认的数据。'
  ],
  '拼多多': [
    '平台风格：拼多多转化导向更强，画面更直接、更醒目、更接地气，强调一眼看懂商品优势和购买理由。',
    '视觉表达：可以更高饱和、更强对比、更明确的卖点区块、福利感区域和大面积留白，但保持合规，不做夸大功效或低质杂乱感。',
    '平台化表达：突出“实用、划算、家庭囤货、同款对比、直观看懂、强缩略图识别”，但不要生成未提供的价格、补贴、包邮或限时促销。',
    '版面倾向：给大标题、价格/福利、3 个核心利益点和醒目角标留大块可读空间，适合小屏列表缩略图。'
  ],
  '抖音电商': [
    '平台风格：抖音电商偏短视频封面和直播间转化，动态感、场景感、人物/使用情境冲击力更强，像视频暂停帧而不是静态货架图。',
    '视觉表达：适合竖屏封面、直播间背景、达人上身/上手场景、强光影、近景商品展示、前景动作和高停留率构图。',
    '平台化表达：突出“开播感、试用感、达人推荐感、场景冲突、三秒吸引、近距离质感”，但不要生成平台按钮、真实账号、二维码或水印。',
    '版面倾向：把直播标题、福利点、口播卖点、弹幕式短语或行动提示直接排版进画面。'
  ],
  '快手电商': [
    '平台风格：快手电商偏真实、实在、源头货和信任感，画面可以更接地气，但要保持商品专业和干净。',
    '视觉表达：适合产地、仓库、工厂、摊位、家庭使用或真人手持场景，突出货品规模、细节和可信氛围。',
    '平台化表达：突出“源头好货、真实展示、实用耐看、货品看得见、亲近可信”，不要生成未提供的价格、补贴、承诺或平台 UI。',
    '版面倾向：标题直接，卖点短句清晰，主体和场景都要易读，避免低质杂乱和夸张噱头。'
  ],
  '小红书': [
    '平台风格：小红书偏生活方式、真实种草、杂志封面、笔记首图和审美氛围，少一点硬广，多一点使用场景和情绪价值。',
    '视觉表达：自然光、生活化陈列、手作/穿搭/家居/旅行/开箱氛围、轻胶片或杂志感，画面像优质笔记封面而不是传统货架图。',
    '平台化表达：突出“种草感、松弛感、真实使用、氛围审美、收藏欲、拍照好看、生活方式”，避免过度促销和硬广大字报。',
    '版面倾向：把封面标题、副标题、标签贴纸和 2-3 个轻量卖点直接排版进画面，整体要清爽、精致、有分享欲。'
  ]
}

const USAGE_PROMPT_PRESETS: Record<string, string[]> = {
  '主图': [
    '图片用途：主图。商品必须是绝对主角，轮廓清楚，第一眼能识别品类、颜色、材质和核心卖点。',
    '构图建议：主体占比高，背景干净但有平台风格，避免堆太多道具，标题或角标直接排版进画面。',
    '转化目标：在搜索结果缩略图里也能看清商品，不依赖小字说明就能理解卖什么。'
  ],
  '卖点图': [
    '图片用途：卖点图。画面要围绕 2-3 个真实卖点组织视觉信息，使用分区、局部特写、对比场景或功能暗示。',
    '构图建议：每个卖点都需要有清楚的视觉锚点，并把短文案直接排版在对应区域。',
    '转化目标：让用户无需阅读长文案也能理解“为什么值得买”，但不要视觉化未提供的具体参数或功效。'
  ],
  '场景图': [
    '图片用途：场景图。把商品放进真实使用情境，体现目标人群、生活方式和购买理由。',
    '构图建议：场景服务商品，不要让人物、背景或道具抢走主体；参考图商品颜色和结构优先。',
    '转化目标：让用户代入“我会在什么地方、什么状态下使用它”，场景要真实可信。'
  ],
  '详情页模块': [
    '图片用途：详情页模块。适合上下滚动浏览，要有模块化信息层级、细节展示和参数承载区域。',
    '构图建议：可做横向/纵向分区、细节放大、材质局部、步骤说明，并把文字直接排版进模块。',
    '转化目标：像详情页中的一屏模块，画面同时承担质感、场景和信息说明。'
  ],
  '直播封面': [
    '图片用途：直播封面。要有强吸引力、强主体、强氛围，适合直播间入口和短视频封面。',
    '构图建议：把大标题、福利点、直播主题或角标直接排版进画面，画面要明亮、有冲击力，但不要生成平台 UI。',
    '转化目标：像直播封面的首帧，能让用户停一下，但不直接绘制倒计时、按钮、二维码或账号元素。'
  ],
  '短视频首帧': [
    '图片用途：短视频首帧。要像视频刚暂停的一帧，有动作感、场景冲击力和明确商品主体。',
    '构图建议：优先竖版信息流，人物上手、近景细节、使用前后或场景痛点都可以，但不要做成静态白底货架图。',
    '转化目标：3 秒内让用户知道商品是什么、解决什么问题、为什么值得继续看。'
  ],
  '小红书种草图': [
    '图片用途：小红书种草图。画面像真实笔记首图，重氛围、审美、生活方式和收藏欲。',
    '构图建议：适合 3:4 或竖版思路，把封面标题、标签和轻量卖点贴纸直接排版进画面，不要做成硬广大字报。',
    '转化目标：像用户愿意收藏的笔记封面，画面同时传达审美、使用理由和关键文字信息。'
  ]
}

const TEXT_LAYER_POLICY = [
  '中文字规则：AI 必须把下方中文文案直接绘制在图片上，作为最终可下载海报的一部分。',
  '文字需要清晰、端正、符合国内电商主图/详情图/封面排版，不能出现乱码、错别字、伪字、随机英文或 JSON。',
  '只渲染用户提供的文案，不要自行添加价格、活动词、认证章、平台按钮、二维码、水印或未确认承诺。',
  '文字排版要服务商品转化：标题醒目，卖点短促，角标清楚，避免遮挡商品主体。',
  '如果用户没有提供某项文字，就不要虚构该项文字。'
]

function statusKey(status: string) {
  if (status === 'running') return '生成中'
  if (status === 'queued') return '排队中'
  if (status === 'success') return '已完成'
  if (status === 'failed') return '失败'
  return status
}

function statusClass(status: string) {
  if (status === 'success') return 'status status-ok'
  if (status === 'failed') return 'status status-bad'
  return 'status status-warn'
}

function fieldList(values: string[]) {
  return values.map(item => item.trim()).filter(Boolean)
}

function splitSellingPoints(value: string) {
  return value
    .split(/[\n,，;；、|｜]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .slice(0, 3)
}

export function DomesticCommerceWorkflow() {
  const t = useI18n()
  const { billingEnabled } = useRuntime()
  const [platform, setPlatform] = useState(PLATFORM_OPTIONS[0])
  const [usage, setUsage] = useState(USAGE_OPTIONS[0])
  const [productName, setProductName] = useState('')
  const [brand, setBrand] = useState('')
  const [sellingPoints, setSellingPoints] = useState('')
  const [specs, setSpecs] = useState('')
  const [audience, setAudience] = useState('')
  const [forbiddenWords, setForbiddenWords] = useState('')
  const [size, setSize] = useState('1024x1024')
  const [enableTextLayer, setEnableTextLayer] = useState(true)
  const [title, setTitle] = useState('')
  const [subtitle, setSubtitle] = useState('')
  const [point1, setPoint1] = useState('')
  const [point2, setPoint2] = useState('')
  const [point3, setPoint3] = useState('')
  const [priceText, setPriceText] = useState('')
  const [badge, setBadge] = useState('')
  const [reference, setReference] = useState<File | null>(null)
  const [referenceUrl, setReferenceUrl] = useState('')
  const [referencePreview, setReferencePreview] = useState('')
  const [project, setProject] = useState<ProjectSummary | null>(null)
  const [tasks, setTasks] = useState<ImageTaskView[]>([])
  const [credits, setCredits] = useState<number | null>(null)
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [suggestingSellingPoints, setSuggestingSellingPoints] = useState(false)
  const [workflowStep, setWorkflowStep] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const submittingRef = useRef(false)

  const cost = imageCreditsForSize(size)
  const hasActiveTask = hasActiveImageTasks(tasks)
  const overlayJson = useMemo(() => {
    const fallbackPoints = splitSellingPoints(sellingPoints)
    const points = fieldList([point1, point2, point3])
    return {
      enabled: enableTextLayer,
      platform,
      usage,
      text_layer: {
        title: title.trim() || productName.trim(),
        subtitle: subtitle.trim() || audience.trim(),
        selling_points: points.length ? points : fallbackPoints,
        price_text: priceText.trim(),
        badge: badge.trim()
      }
    }
  }, [audience, badge, enableTextLayer, platform, point1, point2, point3, priceText, productName, sellingPoints, subtitle, title, usage])

  const visibleTextLines = useMemo(() => {
    if (!enableTextLayer) return []
    return [
      overlayJson.text_layer.title ? `主标题：${overlayJson.text_layer.title}` : '',
      overlayJson.text_layer.subtitle ? `副标题：${overlayJson.text_layer.subtitle}` : '',
      overlayJson.text_layer.selling_points.length ? `卖点：${overlayJson.text_layer.selling_points.join(' / ')}` : '',
      overlayJson.text_layer.price_text ? `价格/促销词：${overlayJson.text_layer.price_text}` : '',
      overlayJson.text_layer.badge ? `角标：${overlayJson.text_layer.badge}` : ''
    ].filter(Boolean)
  }, [enableTextLayer, overlayJson])

  const prompt = useMemo(() => {
    const platformPreset = PLATFORM_PROMPT_PRESETS[platform] || []
    const usagePreset = USAGE_PROMPT_PRESETS[usage] || []
    const textLayer = enableTextLayer
      ? [
        ...TEXT_LAYER_POLICY,
        '图片上必须出现的中文文字：',
        ...visibleTextLines
      ].join('\n')
      : '文字规则：不要在图片中生成任何文字、价格、标签、角标或平台 UI，保留干净商品画面。'
    return [
      `为${platform}生成${usage}。`,
      ...platformPreset,
      ...usagePreset,
      productName ? `商品名：${productName}` : '',
      brand ? `品牌：${brand}` : '',
      sellingPoints ? `卖点：${sellingPoints}` : '',
      specs ? `价格/规格/参数：${specs}` : '',
      audience ? `适用人群：${audience}` : '',
      forbiddenWords ? `禁用词与规避：${forbiddenWords}` : '',
      '参考图规则：如果上传了主商品图或参考图，参考图里的商品颜色、形状、材质、结构和款式是最高优先级，不要擅自改成其他变体。',
      '统一画面要求：国内电商转化导向，商品主体清晰，平台风格明显，构图适合移动端浏览，避免夸大功效、虚假认证、未经提供的具体参数、平台 UI、二维码、水印和无关品牌标识。',
      textLayer
    ].filter(Boolean).join('\n')
  }, [audience, brand, enableTextLayer, forbiddenWords, platform, productName, sellingPoints, specs, usage, visibleTextLines])

  useEffect(() => {
    if (requireLogin()) refreshCredits()
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const template = getTemplateById(params.get('templateId'))
    if (template?.routeType === 'domestic' && template.imagePreset) {
      const preset = template.imagePreset
      if (preset.platform && PLATFORM_OPTIONS.includes(preset.platform)) setPlatform(preset.platform)
      if (preset.usage && USAGE_OPTIONS.includes(preset.usage)) setUsage(preset.usage)
      if (preset.productName !== undefined) setProductName(preset.productName)
      if (preset.brand !== undefined) setBrand(preset.brand)
      if (preset.sellingPoints) setSellingPoints(preset.sellingPoints)
      if (preset.specs) setSpecs(preset.specs)
      if (preset.audience) setAudience(preset.audience)
      if (preset.forbiddenWords) setForbiddenWords(preset.forbiddenWords)
      if (preset.size) setSize(preset.size)
      if (typeof preset.enableTextLayer === 'boolean') setEnableTextLayer(preset.enableTextLayer)
      if (preset.title) setTitle(preset.title)
      if (preset.subtitle) setSubtitle(preset.subtitle)
      if (preset.points?.[0]) setPoint1(preset.points[0])
      if (preset.points?.[1]) setPoint2(preset.points[1])
      if (preset.points?.[2]) setPoint3(preset.points[2])
      if (preset.priceText) setPriceText(preset.priceText)
      if (preset.badge) setBadge(preset.badge)
      setMessage(`已套用模板：${template.title}。请补充商品名和真实参数后生成。`)
    }
  }, [])

  useEffect(() => {
    const projectId = new URLSearchParams(window.location.search).get('projectId')
    if (!projectId || project?.id === projectId || !requireLogin()) return
    setLoading(true)
    getProject(projectId)
      .then(detail => {
        setProject(detail.project)
        setTasks(detail.image_tasks)
         setProductName(detail.project.product_name || '')
         const referenceImage = detail.product_images.find(image => (image.image_type || '').includes('reference'))
         if (referenceImage) setReferencePreview(assetUrl(referenceImage.url))
         setWorkflowStep(2)
      })
      .catch(error => setMessage(error instanceof Error ? error.message : '项目加载失败'))
      .finally(() => setLoading(false))
  }, [project?.id])

  useEffect(() => {
    if (!project || !hasActiveTask) return
    const timer = window.setInterval(refresh, 8000)
    return () => window.clearInterval(timer)
  }, [project?.id, hasActiveTask])

  useEffect(() => {
    if (!confirmOpen) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !loading) setConfirmOpen(false)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [confirmOpen, loading])

  useEffect(() => {
    if (hasActiveTask) setConfirmOpen(false)
  }, [hasActiveTask])

  function selectReference(file: File) {
    setReference(file)
    if (referencePreview.startsWith('blob:')) URL.revokeObjectURL(referencePreview)
    setReferencePreview(URL.createObjectURL(file))
  }

  async function refreshCredits() {
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => setCredits(null))
  }

  async function suggestSellingPoints() {
    if (!requireLogin() || !productName.trim()) {
      setMessage('请先填写商品名称。')
      return
    }
    setSuggestingSellingPoints(true)
    try {
      const data = await suggestDomesticSellingPoints({
        productName: productName.trim(),
        platform,
        usage,
        specs: specs.trim(),
        audience: audience.trim(),
        language: currentLanguage()
      })
      const suggestions = (data.selling_points || []).slice(0, 6)
      const joined = suggestions.join('、')
      setSellingPoints(joined)
      setPoint1(suggestions[0] || '')
      setPoint2(suggestions[1] || '')
      setPoint3(suggestions[2] || '')
      if (!title.trim()) {
        setTitle(suggestions[0] || productName.trim())
      }
      setMessage('已生成卖点建议，可继续修改。')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '卖点建议生成失败，请稍后重试。')
    } finally {
      setSuggestingSellingPoints(false)
    }
  }

  async function submit() {
    if (!requireLogin() || !productName.trim() || !confirmOpen) return
    if (submittingRef.current) return
    if (hasActiveTask) {
      setConfirmOpen(false)
      setMessage('当前任务仍在排队或生成中，请完成后再创建新任务。')
      return
    }
    submittingRef.current = true
    if (credits !== null && credits < cost) {
      setMessage('积分不足，请先充值或兑换积分。')
      submittingRef.current = false
      return
    }
    setConfirmOpen(false)
    setLoading(true)
    setMessage('')
    try {
      const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
      const detail = await createCreativeImage({
        productName: productName.trim(),
        brand: brand.trim() || platform,
        prompt,
        size,
        language: currentLanguage(),
        module: `国内电商 / ${platform} / ${usage}`,
        reference,
        referenceUrl: referenceUrl.trim(),
        ...(template ? templateIntentMeta(template) : {})
      })
      setProject(detail.project)
      setTasks(detail.image_tasks)
      window.history.replaceState(null, '', `/commerce?tab=domestic&projectId=${detail.project.id}`)
      setMessage('任务已提交，正在生成。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '提交失败，请稍后重试。')
    } finally {
      submittingRef.current = false
      setLoading(false)
      refreshCredits()
    }
  }

  async function refresh() {
    if (!project) return
    const detail = await getProject(project.id)
    setProject(detail.project)
    setTasks(detail.image_tasks)
    const failedTask = detail.image_tasks.find(task => task.status === 'failed')
    if (failedTask) setMessage(failedTask.safe_error || detail.project.latest_failure_reason || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
  }

  async function retry(taskId: string) {
    if (!project) return
    setLoading(true)
    try {
      const updated = await retryProjectImage(project.id, taskId)
      setTasks(rows => rows.map(task => task.id === taskId ? updated : task))
      setProject(current => current ? { ...current, status: 'images_queued' } : current)
      setMessage('已重新加入队列。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '重新生成失败')
    } finally {
      setLoading(false)
      refreshCredits()
    }
  }

  function runPrimaryAction() {
    if (workflowStep === 0) {
      if (!productName.trim()) {
        setMessage('请先填写商品名称。')
        return
      }
      setMessage('')
      setWorkflowStep(1)
      return
    }
    if (workflowStep === 1) {
      setMessage('')
      setWorkflowStep(2)
      return
    }
    requestSubmissionConfirmation()
  }

  function requestSubmissionConfirmation() {
    if (hasActiveTask) {
      setMessage('当前任务仍在排队或生成中，请完成后再创建新任务。')
      return
    }
    if (credits !== null && credits < cost) {
      setMessage('积分不足，请先充值或兑换积分。')
      return
    }
    setMessage('')
    setConfirmOpen(true)
  }

  const domesticWorkflowSteps = ['商品与素材', '平台与画面', '费用与提交']

  return (
    <UserAuthGate title="登录后使用国内电商制图" description={t('generationConfirmPersonal')}>
      <div className="page-head domestic-page-head">
        <div>
          <h1>国内电商</h1>
          <p>按平台风格、图片用途、商品信息和图片内中文文案生成淘宝/1688/京东/拼多多/抖音/小红书图片。</p>
        </div>
        {project ? <div className="page-actions"><Link className="btn btn-primary" href={`/projects/${project.id}#tasks`}>进入成品详情</Link><button className="btn btn-secondary" disabled={loading} onClick={refresh}>刷新状态</button></div> : null}
      </div>
      {message ? <div className={message.includes('失败') || message.includes('不足') ? 'notice notice-error' : 'notice'}>{message}</div> : null}
      <TaskFlowSteps steps={domesticWorkflowSteps} currentStep={workflowStep} ariaLabel="国内电商图片创建步骤" onStepChange={setWorkflowStep} />
      <div className="domestic-workflow-grid">
        <section className="panel domestic-workflow-main">
          <div className="panel-head"><div><h2>{domesticWorkflowSteps[workflowStep]}</h2><p>{workflowStep === 0 ? '先整理商品事实与参考素材，不确定的信息可以留空。' : workflowStep === 1 ? '选择平台、用途、输出规格和需要排入画面的真实文字。' : '最后核对商品、平台和图片文字，再提交后台生成。'}</p></div></div>
          {workflowStep === 0 ? (
            <div className="panel-body form-grid">
              <div className="grid-2">
                <label className="field"><span>商品名 *</span><input className="input" value={productName} onChange={event => setProductName(event.target.value)} placeholder="例如：兔子玩偶包" /></label>
                <label className="field"><span>品牌</span><input className="input" value={brand} onChange={event => setBrand(event.target.value)} placeholder="可选" /></label>
              </div>
              <div className="field">
                <div className="field-title-row">
                  <span>卖点</span>
                  <button type="button" className="btn btn-secondary btn-compact" disabled={suggestingSellingPoints || loading || !productName.trim()} onClick={suggestSellingPoints}>
                    <Sparkles size={14} />
                    {suggestingSellingPoints ? '生成中...' : 'AI 生成卖点'}
                  </button>
                </div>
                <textarea className="textarea" value={sellingPoints} onChange={event => setSellingPoints(event.target.value)} placeholder="面料、容量、风格、场景、适用人群等，越真实越好。" />
              </div>
              <div className="grid-2">
                <label className="field"><span>价格/规格/参数</span><input className="input" value={specs} onChange={event => setSpecs(event.target.value)} placeholder="只填真实已确认的信息" /></label>
                <label className="field"><span>适用人群</span><input className="input" value={audience} onChange={event => setAudience(event.target.value)} placeholder="例如：Lolita 爱好者、学生、通勤人群" /></label>
              </div>
              <label className="field"><span>禁用词</span><input className="input" value={forbiddenWords} onChange={event => setForbiddenWords(event.target.value)} placeholder="不希望出现的词、违规词或品牌词" /></label>
              <ImageReferenceInput label="参考图" hint="可上传主商品图、细节图、竞品参考或风格参考。" file={reference} url={referenceUrl} previewUrl={referencePreview || referenceUrl} onFile={selectReference} onClearFile={() => { setReference(null); setReferencePreview('') }} onUrl={setReferenceUrl} uploadLabel="选择图片" removeLabel="移除" linkLabel="图片链接" placeholder="https://example.com/product.jpg" />
            </div>
          ) : null}
          {workflowStep === 1 ? (
            <div className="panel-body form-grid">
              <div className="domestic-option-grid">
                <div className="field"><span>平台风格</span><div className="domestic-chip-grid">{PLATFORM_OPTIONS.map(item => <button key={item} type="button" className={platform === item ? 'domestic-chip domestic-chip-active' : 'domestic-chip'} onClick={() => setPlatform(item)}>{item}</button>)}</div></div>
                <div className="field"><span>图片用途</span><div className="domestic-chip-grid">{USAGE_OPTIONS.map(item => <button key={item} type="button" className={usage === item ? 'domestic-chip domestic-chip-active' : 'domestic-chip'} onClick={() => setUsage(item)}>{item}</button>)}</div></div>
              </div>
              <div className="grid-2">
                <label className="field"><span>清晰度 / 比例</span><select className="select" value={size} onChange={event => setSize(event.target.value)}>{IMAGE_SIZE_OPTIONS.map(option => <option value={option.value} key={`${option.quality}-${option.ratio}-${option.value}`}>{imageSizeLabel(option.value)}</option>)}</select></label>
                <label className="switch-row domestic-switch">
                  <input type="checkbox" checked={enableTextLayer} onChange={event => setEnableTextLayer(event.target.checked)} />
                  <span><strong>AI 直接生成中文文字</strong><small>让模型把标题、卖点、角标直接画进成品图。</small></span>
                </label>
              </div>
              {enableTextLayer ? (
                <div className="domestic-overlay-fields">
                  <div className="grid-2">
                    <label className="field"><span>主标题</span><input className="input" value={title} onChange={event => setTitle(event.target.value)} placeholder="例如：软萌兔包" /></label>
                    <label className="field"><span>副标题</span><input className="input" value={subtitle} onChange={event => setSubtitle(event.target.value)} placeholder="例如：出街通勤都好搭" /></label>
                  </div>
                  <div className="grid-3">
                    <label className="field"><span>卖点 1</span><input className="input" value={point1} onChange={event => setPoint1(event.target.value)} /></label>
                    <label className="field"><span>卖点 2</span><input className="input" value={point2} onChange={event => setPoint2(event.target.value)} /></label>
                    <label className="field"><span>卖点 3</span><input className="input" value={point3} onChange={event => setPoint3(event.target.value)} /></label>
                  </div>
                  <div className="grid-2">
                    <label className="field"><span>价格/促销词</span><input className="input" value={priceText} onChange={event => setPriceText(event.target.value)} placeholder="可选，注意真实合规" /></label>
                    <label className="field"><span>角标</span><input className="input" value={badge} onChange={event => setBadge(event.target.value)} placeholder="例如：新品、热卖" /></label>
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}
          {workflowStep === 2 ? (
            <div className="panel-body domestic-final-review">
              <div className="confirm-summary-grid">
                <div className="result-box"><strong>商品</strong><p>{productName || '-'}</p><p>{brand || '未填写品牌'}</p></div>
                <div className="result-box"><strong>平台与用途</strong><p>{platform} / {usage}</p><p>{imageSizeLabel(size)}</p></div>
                <div className="result-box"><strong>参考素材</strong><p>{reference || referenceUrl.trim() || referencePreview ? '已添加' : '未添加'}</p><p>参考图为可选项</p></div>
              </div>
              <div className="result-box domestic-copy-preview">
                <strong>图片内文字</strong>
                {enableTextLayer ? (
                  <div className="domestic-copy-preview-body">
                    <b>{overlayJson.text_layer.title || '主标题未填写'}</b>
                    {overlayJson.text_layer.subtitle ? <span>{overlayJson.text_layer.subtitle}</span> : null}
                    <div className="domestic-copy-tags">
                      {overlayJson.text_layer.selling_points.map((item, index) => <em key={`${item}-${index}`}>{item}</em>)}
                      {overlayJson.text_layer.badge ? <em key="badge">{overlayJson.text_layer.badge}</em> : null}
                    </div>
                  </div>
                ) : <p>未启用图内文字，本次只生成无文字商品图。</p>}
              </div>
            </div>
          ) : null}
          <div className="task-flow-actions">
            <div>{workflowStep > 0 ? <button className="btn btn-secondary" type="button" onClick={() => setWorkflowStep(step => Math.max(0, step - 1))}><ArrowLeft size={16} />上一步</button> : null}</div>
            <span className="muted">第 {workflowStep + 1} / {domesticWorkflowSteps.length} 步</span>
          </div>
        </section>
        <aside className="panel domestic-preview-panel">
          <div className="panel-head"><div><h2>创建摘要</h2><p>内容实时保留，最后一步才会提交。</p></div></div>
          <div className="panel-body domestic-preview-body">
            <BillingOnly><div className="result-box"><strong>预计扣费</strong><p>{cost} 积分 / 张</p><p>当前余额：{credits ?? '--'} 积分</p></div></BillingOnly>
            <div className="result-box"><strong>当前选择</strong><p>{platform} / {usage}</p><p>{imageSizeLabel(size)}</p></div>
            <div className="result-box domestic-copy-preview">
              <strong>图片内文字</strong>
              {enableTextLayer ? (
                <div className="domestic-copy-preview-body">
                  <b>{overlayJson.text_layer.title || '主标题未填写'}</b>
                  {overlayJson.text_layer.subtitle ? <span>{overlayJson.text_layer.subtitle}</span> : null}
                  <div className="domestic-copy-tags">
                    {overlayJson.text_layer.selling_points.map((item, index) => <em key={`${item}-${index}`}>{item}</em>)}
                    {overlayJson.text_layer.badge ? <em key="badge">{overlayJson.text_layer.badge}</em> : null}
                  </div>
                  <small>AI 会直接把这些文字画进图片里。</small>
                </div>
              ) : <p>未启用图内文字，本次只生成无文字商品图。</p>}
            </div>
            <button className="btn btn-primary btn-block" disabled={loading || !productName.trim() || (workflowStep === 2 && hasActiveTask)} onClick={runPrimaryAction}>
              {loading ? '提交中...' : workflowStep === 2 && hasActiveTask ? '当前任务生成中' : workflowStep === 0 ? <>下一步：平台与画面<ArrowRight size={16} /></> : workflowStep === 1 ? <>下一步：费用确认<ArrowRight size={16} /></> : '确认并开始生成'}
            </button>
          </div>
        </aside>
      </div>
      <section className="panel mt-24">
        <div className="panel-head"><div><h2>生成结果</h2><p>点刷新查看队列状态，失败会按原规则退款并可重新生成。</p></div></div>
        <div className="panel-body grid-3">
          {tasks.length ? tasks.map(task => (
            <article className="panel feature-card" key={task.id}>
              <h3>{task.image_name}</h3>
              <p><span className={statusClass(task.status)}>{statusKey(task.status)}</span></p>
              {task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} /> : null}
              {task.status === 'failed' ? <div className="notice notice-error">{task.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal')}</div> : null}
              <BillingOnly><div className="muted">{task.credits_cost} 积分</div></BillingOnly>
              <div className="generation-card-actions">
                {project ? <Link className="btn btn-primary" href={`/projects/${project.id}#task-${task.id}`}>进入成品详情</Link> : null}
                {task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">查看/下载</a> : null}
                {task.status === 'failed' ? <button className="btn btn-primary" disabled={loading} onClick={() => retry(task.id)}>重新生成</button> : null}
              </div>
            </article>
          )) : <p className="muted">暂无任务。</p>}
        </div>
      </section>
      {confirmOpen ? (
        <div className="suite-confirm-backdrop" role="presentation" onMouseDown={event => {
          if (event.currentTarget === event.target && !loading) setConfirmOpen(false)
        }}>
          <section className="suite-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="domestic-confirm-title">
            <header>
              <span><Sparkles size={20} /></span>
              <div><h2 id="domestic-confirm-title">确认生成国内电商图</h2><p>确认后将立即创建任务。</p></div>
              <button type="button" aria-label="关闭确认弹窗" disabled={loading} onClick={() => setConfirmOpen(false)}><X size={18} /></button>
            </header>
            <div className="suite-confirm-summary">
              <div><span>商品</span><strong>{productName.trim()}</strong></div>
              <div><span>平台与用途</span><strong>{platform} / {usage}</strong></div>
              <div><span>输出</span><strong>1 张商品图</strong></div>
              <div><span>清晰度</span><strong>{imageSizeLabel(size)}</strong></div>
              <BillingOnly><div className="cost"><span>本次扣除</span><strong>{cost} 积分</strong></div></BillingOnly>
              <BillingOnly><div><span>当前余额</span><strong>{credits ?? '--'} 积分</strong></div></BillingOnly>
            </div>
            <footer>
              <button className="btn btn-secondary" type="button" disabled={loading} onClick={() => setConfirmOpen(false)}>返回修改</button>
              <button className="btn btn-primary" type="button" disabled={loading || hasActiveTask} onClick={() => void submit()}>{loading ? <RefreshCw size={16} /> : <Sparkles size={16} />}{loading ? '提交中...' : '确认并扣费生成'}</button>
            </footer>
          </section>
        </div>
      ) : null}
    </UserAuthGate>
  )
}
