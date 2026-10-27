'use client'
import { useI18n } from '@/components/i18n'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { ArrowLeft, ArrowRight } from 'lucide-react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { TaskFlowSteps } from '@/components/TaskFlowSteps'
import { useLanguage } from '@/components/i18n'
import { assetUrl, createProject, enqueueListingPackage, generateCopy, getOptionalBillingSummary, getMe, getProject, ImageTaskView, isAiDisabledError, linkProductImage, optimizePrompt, ProductImageView, ProjectSummary, requireLogin, retryProjectImage, saveStoredUser, uploadProductImage } from '@/lib/api'
import { copyToClipboard } from '@/lib/clipboard'
import { compressImageForUpload, MAX_UPLOAD_IMAGE_BYTES, TARGET_UPLOAD_IMAGE_BYTES } from '@/lib/imageCompression'
import { APLUS_OUTPUT_SIZE_OPTIONS, normalizeAplusOutputSize } from '@/lib/imageSizes'
import { getTemplateById } from '@/lib/templateCatalog'

function taskStatusKey(status: string) {
  if (status === 'queued') return 'queued'
  if (status === 'running') return 'running'
  if (status === 'success') return 'success'
  if (status === 'failed') return 'failed'
  return status || 'pending'
}

function projectStatusKey(status: string) {
  if (status === 'draft') return 'draft'
  if (status === 'copy_generated') return 'copyGenerated'
  if (status === 'facts_confirmed') return 'factsConfirmed'
  if (status === 'images_queued') return 'running'
  if (status === 'images_completed') return 'imagesCompleted'
  if (status === 'images_failed') return 'failed'
  if (status === 'images_partial_failed') return 'partialFailed'
  return status || 'notCreated'
}

function statusClass(status: string) {
  if (status === 'success') return 'status status-ok'
  if (status === 'failed') return 'status status-bad'
  if (status === 'queued' || status === 'running') return 'status status-warn'
  return 'status'
}

function extractText(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function extractArray(value: unknown) {
  return Array.isArray(value) ? value.map(item => String(item)) : []
}

const APLUS_MODULE_OPTIONS = [
  { value: 'brand-story-banner', labelKey: 'aplusModuleBrandStory' },
  { value: 'key-feature-module', labelKey: 'aplusModuleFeatures' },
  { value: 'lifestyle-scene-module', labelKey: 'aplusModuleLifestyle' },
  { value: 'specs-structure-module', labelKey: 'aplusModuleSpecs' }
]

const LEGACY_APLUS_MODULES: Record<string, string> = {
  品牌故事横幅: 'brand-story-banner',
  核心卖点模块: 'key-feature-module',
  场景生活方式图: 'lifestyle-scene-module',
  '参数/结构说明图': 'specs-structure-module',
  'Brand story banner': 'brand-story-banner',
  'Key feature module': 'key-feature-module',
  'Lifestyle scene module': 'lifestyle-scene-module',
  'Specs / structure module': 'specs-structure-module',
  ブランドストーリーバナー: 'brand-story-banner',
  主要訴求モジュール: 'key-feature-module',
  ライフスタイルシーン: 'lifestyle-scene-module',
  '仕様 / 構造説明': 'specs-structure-module'
}

const TASK_ORDER = ['main', 'scenario', 'features', 'dimensions', 'steps', 'comparison', 'package', 'aplus', 'creative']

function firstMatch(text: string, patterns: RegExp[]) {
  for (const pattern of patterns) {
    const match = text.match(pattern)
    const value = match?.[1]?.trim()
    if (value) return value.replace(/^["'“”‘’]+|["'“”‘’。；;，,]+$/g, '').trim()
  }
  return ''
}

function normalizeMarketplace(value: string) {
  const text = value.trim().toLowerCase()
  if (!text) return ''
  if (/\b(us|usa|united states|america|美国|美站)\b/i.test(text)) return 'US'
  if (/\b(de|deutschland|germany|德国|德站)\b/i.test(text)) return 'DE'
  if (/\b(fr|france|法国|法站)\b/i.test(text)) return 'FR'
  return ''
}

function normalizeListingLanguage(value: string) {
  const text = value.trim().toLowerCase()
  if (!text) return ''
  if (/english|en-us|英语|英文/.test(text)) return 'en-US'
  if (/deutsch|german|de-de|德语|德文/.test(text)) return 'de-DE'
  if (/fran[cç]ais|french|fr-fr|法语|法文/.test(text)) return 'fr-FR'
  return ''
}

function splitKeywords(value: string) {
  return value
    .split(/[,\n，、;；/]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .slice(0, 8)
    .join(', ')
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function cleanStructuredValue(value: string) {
  return value.replace(/^[\s,，;；。:：]+|[\s,，;；。]+$/g, '').trim()
}

function structuredListingValues(text: string) {
  const fields = [
    { key: 'brand', labels: ['品牌名', '品牌', 'brand'] },
    { key: 'productName', labels: ['产品名称', '商品名称', '产品名', '商品名', 'product name', 'product'] },
    { key: 'country', labels: ['目标站点', '站点', '国家', 'marketplace', 'country'] },
    { key: 'language', labels: ['输出语言', '语言', 'language'] },
    { key: 'category', labels: ['产品类目', '商品类目', '类目', 'category'] },
    { key: 'titleKeywords', labels: ['标题关键词', '关键词', 'title keywords', 'keywords'] },
    { key: 'sellingKeywords', labels: ['卖点关键词', 'selling point keywords', 'bullet keywords'] },
    { key: 'descriptionKeywords', labels: ['描述关键词', 'description keywords'] },
    { key: 'material', labels: ['材质', 'material'] },
    { key: 'dimensions', labels: ['尺寸', '规格', 'size', 'dimensions'] },
    { key: 'extra', labels: ['其他信息', '使用场景', 'prompt', 'extra'] }
  ] as const
  const hits: { key: (typeof fields)[number]['key']; index: number; valueStart: number }[] = []
  const present = new Set<string>()
  for (const field of fields) {
    for (const label of field.labels) {
      const pattern = new RegExp(`(^|[\\s,，;；。\\n])(${escapeRegExp(label)})\\s*[:：]`, 'gi')
      let match: RegExpExecArray | null
      while ((match = pattern.exec(text)) !== null) {
        const prefixLength = match[1]?.length || 0
        hits.push({
          key: field.key,
          index: match.index + prefixLength,
          valueStart: match.index + match[0].length
        })
      }
    }
  }
  hits.sort((left, right) => left.index - right.index)
  const values = hits.reduce<Record<string, string>>((acc, hit, index) => {
    present.add(hit.key)
    if (Object.prototype.hasOwnProperty.call(acc, hit.key)) return acc
    const next = hits[index + 1]
    const raw = text.slice(hit.valueStart, next ? next.index : text.length)
    acc[hit.key] = cleanStructuredValue(raw)
    return acc
  }, {})
  return { values, present }
}

function structuredOrFallback(structured: ReturnType<typeof structuredListingValues>, key: string, fallback: () => string) {
  return structured.present.has(key) ? (structured.values[key] || '') : fallback()
}

function inferListingPrefill(prompt: string) {
  const text = prompt.replace(/\r/g, '').trim()
  const compact = text.replace(/\s+/g, ' ')
  const structured = structuredListingValues(text)
  const brand = structuredOrFallback(structured, 'brand', () => firstMatch(text, [
    /(?:品牌名?|brand)\s*[:：]\s*([^,，;；\n]+)/i,
    /(?:我的品牌是|品牌是)\s*([^,，;；\n]+)/i
  ]))
  const productName = structuredOrFallback(structured, 'productName', () => firstMatch(compact, [
    /(?:产品名称|商品名称|产品名|商品名|product(?:\s*name)?)\s*[:：]\s*([^,，;；\n]+)/i,
    /(?:生成|做|创建|制作|写)(?:一个|一套|一份)?(?:亚马逊|Amazon)?(?:Listing|listing|七图|文案)?(?:的|：|:)?\s*([^,，;；\n]{2,40})/i
  ]))
  const country = normalizeMarketplace(structuredOrFallback(structured, 'country', () => firstMatch(compact, [
    /(?:目标站点|站点|国家|marketplace|country)\s*[:：]\s*([^,，;；\n]+)/i,
    /(?:Amazon|亚马逊)\s*([A-Z]{2}|美国|德国|法国|美站|德站|法站|US|DE|FR)/i
  ])))
  const language = normalizeListingLanguage(structuredOrFallback(structured, 'language', () => firstMatch(compact, [
    /(?:输出语言|语言|language)\s*[:：]\s*([^,，;；\n]+)/i
  ])))
  const category = structuredOrFallback(structured, 'category', () => firstMatch(compact, [
    /(?:产品类目|商品类目|类目|category)\s*[:：]\s*([^,，;；\n]+)/i
  ]))
  const material = structuredOrFallback(structured, 'material', () => firstMatch(compact, [
    /(?:材质|material)\s*[:：]\s*([^,，;；\n]+)/i
  ]))
  const dimensions = structuredOrFallback(structured, 'dimensions', () => firstMatch(compact, [
    /(?:尺寸|规格|size|dimensions?)\s*[:：]\s*([^,，;；\n]+)/i
  ]))
  const titleKeywords = splitKeywords(structuredOrFallback(structured, 'titleKeywords', () => firstMatch(compact, [
    /(?:标题关键词|title\s*keywords?|关键词|keywords?)\s*[:：]\s*([^。\n]+)/i
  ])))
  const selling = splitKeywords(structuredOrFallback(structured, 'sellingKeywords', () => firstMatch(compact, [
    /(?:卖点关键词|selling\s*point\s*keywords?|bullet\s*keywords?)\s*[:：]\s*([^。\n]+)/i
  ])))
  const descKeywords = splitKeywords(structuredOrFallback(structured, 'descriptionKeywords', () => firstMatch(compact, [
    /(?:描述关键词|description\s*keywords?)\s*[:：]\s*([^。\n]+)/i
  ])))
  return {
    prompt: text,
    brand,
    productName,
    country,
    language,
    category,
    material,
    dimensions,
    titleKeywords,
    sellingKeywords: selling,
    descriptionKeywords: descKeywords
  }
}

function normalizeAplusModule(value: string) {
  return APLUS_MODULE_OPTIONS.some(option => option.value === value) ? value : (LEGACY_APLUS_MODULES[value] || APLUS_MODULE_OPTIONS[1].value)
}

function sortedTasks(tasks: ImageTaskView[]) {
  return [...tasks].sort((left, right) => {
    const leftIndex = TASK_ORDER.indexOf(left.image_type)
    const rightIndex = TASK_ORDER.indexOf(right.image_type)
    const normalizedLeft = leftIndex < 0 ? TASK_ORDER.length : leftIndex
    const normalizedRight = rightIndex < 0 ? TASK_ORDER.length : rightIndex
    if (normalizedLeft !== normalizedRight) return normalizedLeft - normalizedRight
    return new Date(left.created_at).getTime() - new Date(right.created_at).getTime()
  })
}

const listingText: Record<string, Record<string, string>> = {
  'zh-CN': {
    loginTitle: '登录后创建 Listing 七图', loginDesc: '项目、上传、文案生成、扣积分和图片结果都需要登录后才能操作。',
    title: '新建 Listing 七图', stepsDesc: '流程：先填写商品信息并生成 Listing 文案，再上传参考图并确认生成图片。', refreshProject: '刷新当前项目',
    stepProduct: '商品信息', stepUpload: '主图上传', stepOptimize: 'AI 优化', stepFacts: '确认事实', stepResult: '生成结果',
    productForm: '左：商品信息表单', productFormDesc: '先保存项目，再上传主图和生成文案。', brand: '品牌名', productName: '产品名称', targetMarket: '目标站点', outputLanguage: '输出语言', category: '产品类目', keywords: '标题关键词', sellingKeywords: '卖点关键词', descriptionKeywords: '描述关键词', material: '材质', dimensions: '尺寸', dimensionsPlaceholder: '未填写会影响尺寸图',
    promptLabel: '其他信息 / 使用场景 / Prompt', promptPlaceholder: '输入包装清单、适用型号、颜色、使用场景、画面想法等',
    includeAplus: '同时需要 A+ 图', includeAplusDesc: '运营经常 Listing 七图和 A+ 模块一起要；开启后生成 Listing 时会同步创建一个 A+ 图任务。', aplusModule: 'A+ 模块类型', aplusSize: 'A+ 尺寸', aplusExtra: 'A+ 画面补充要求', aplusExtraPlaceholder: '可选。不填时会根据当前商品名、品牌和 Listing Prompt 自动组织 A+ 画面要求。',
    aplusModuleBrandStory: '品牌故事横幅', aplusModuleFeatures: '核心卖点模块', aplusModuleLifestyle: '场景生活方式图', aplusModuleSpecs: '参数/结构说明图',
    uploadAngle: '多角度图', uploadDetail: '细节图', uploadPackage: '包装图', uploadVariant: '变体图', referenceUrlLabel: '参考图片 URL', referenceUrlPlaceholder: '粘贴图片 URL，可替代本地上传', linkReference: '链接参考图', submittedCost: '已提交扣费',
    optimizing: '优化中...', optimizePrompt: 'AI 优化 Prompt', saveAsNew: '新建项目并生成文案', saveProject: '创建项目并生成 Listing 文案',
    optimizedPromptTitle: '优化后的图像 Prompt', optimizedPromptDesc: '此内容只用于后续生图参考，不会写入商品事实。',
    referenceUpload: '右：参考图上传', referenceUploadDesc: '主商品图必填，多角度图和细节图可选。', mainImage: '主商品图', uploadMain: '点击上传，用于白底主图和商品一致性', saveFirst: '请先保存商品项目', generateCopyFirst: '请先生成 Listing 文案', uploadedImages: '已上传图片', currentProject: '当前项目', imageTasks: '图片任务', estimatedCost: '预计消耗', aplusLinked: 'A+ 关联', created: '已创建',
    aiReview: 'AI 优化 / 合规审查', aiReviewDesc: '生成标题、卖点和图片生成所需的文案依据。', generateCopy: '生成 Listing 文案', copyEmpty: '点击“创建项目并生成 Listing 文案”后，这里会显示 AI 优化和合规结果。',
    factsTitle: '用户确认商品事实', factsDesc: 'confirmed facts 是唯一事实源。未确认前不能创建七图任务。', confirmFacts: '确认事实',
    planTitle: '确认图片生成', planDesc: '上传主/参考图后即可生成 Listing 图片。', noPlan: '暂无七图方案', noPlanDesc: '请先创建商品并生成 Listing 文案。', clothingReferenceRequired: '服装类商品必须先上传至少 1 张主商品参考图；没有参考图无法承诺版型、颜色、图案和细节保真。', currentBalance: '当前余额', oneClickAplus: '一键生成 Listing + A+ 图', oneClickImages: '一键生成可用图片', addAplusOnly: '补充生成 A+ 图（6 积分）', confirmGenerate: '确认生成', confirmGenerateDesc: '请确认商品、参考图和积分消耗无误后再入队。', confirmAplusOnlyDesc: 'Listing 七图已经存在，本次只创建 1 张 A+ 图，不会重复生成七图。', insufficientCredits: '积分不足，无法创建本次生成任务。请先充值或兑换积分。', rechargeRedeem: '去充值/兑换', prevStep: '上一步', nextStep: '下一步', confirmSubmit: '确认并生成', submitting: '提交中...', cancel: '取消', previewProduct: '商品信息', previewReferences: '参考图', previewBriefs: '图片方案',
    resultDesc: 'Worker 会调用已配置的上游图片模型，失败时自动退回本张图片积分。', refreshTasks: '刷新任务状态', viewBrief: '查看 Brief', downloadView: '下载/查看', retryDeduct: '重新生成（扣 {credits} 积分）', noTasks: '暂无图片任务。', creditsUnit: '积分', copyImageUrl: '复制图片 URL', copyImageUrlDone: '图片 URL 已复制', copyPrompt: '复制 Prompt', copyPromptDone: 'Prompt 已复制', manualCopyHint: '浏览器限制了自动复制，请在下方手动全选复制。', manualCopyTitle: '手动复制', manualCopyDesc: '点击文本框会自动全选，可直接 Ctrl+C。', retryWithReference: '按当前坑位重新生成', retryWithReferenceDesc: '可粘贴图片 URL、选择本地图片、拖拽/粘贴图片，并补充文字；系统会保留这张图原来的坑位、图类型和积分扣费。', retryReferenceImage: '重生参考图', retryReferenceImageHint: '可选择文件、拖拽图片、粘贴截图或粘贴图片 URL。', referenceTextLabel: '参考文字', referenceTextPlaceholder: '粘贴要参考的卖点、构图、图片文字或修改要求', confirmRetryGenerate: '确认重新生成',
    restored: '已恢复历史项目，可继续上传参考图、刷新任务或查看结果', defaultDescription: '用户暂未填写商品描述', createdMsg: '项目已创建，Listing 文案正在生成...', uploadedPrefix: '图片已上传：', uploadCompressing: '图片较大，正在自动压缩后上传...', uploadCompressed: '图片已自动压缩后上传', uploadTooLarge: '图片压缩后仍超过上传限制，请换一张更小的图片', copyGeneratedMsg: 'AI 文案和合规检查已生成，请上传/确认主参考图后生成图片', copyFallbackMsg: 'Listing 文案已生成，但上游文案 AI 暂不可用，系统使用了本地安全兜底。请管理员到 AI 配置里做真实测试。', promptOptimized: 'Prompt 已按亚马逊运营专家策略优化', retryQueued: '失败图片已重新加入队列', factsConfirmedMsg: '商品事实已确认，现在可以创建七图任务',
    enqueueAplus: 'Listing 七图任务已进入队列，正在同步创建 A+ 图任务...', enqueueImages: '七图任务已进入 Worker 队列，稍后点击“刷新任务状态”查看生成结果', aplusQueued: 'Listing 七图和 A+ 图已同时入队，A+ 已复用当前套组参考图。A+ 项目：', openAplusResult: '进入 A+ 成品', continueAplusEdit: '编辑 A+ 图', aplusFailed: 'Listing 七图已入队，但 A+ 图创建失败：', aplusFallback: '请稍后到 A+ 图页面单独生成',
    loadFailed: '项目详情加载失败', createFailed: '项目创建失败', uploadFailed: '图片上传失败', linkFailed: '图片链接保存失败', copyFailed: '文案生成失败', promptFailed: 'Prompt 优化失败', retryFailed: '重新生成失败', factsFailed: '事实确认失败，请检查 JSON 格式', enqueueFailed: '图片任务创建失败',
    draft: '草稿', copyGenerated: '文案已生成', factsConfirmed: '事实已确认', running: '生成中', imagesCompleted: '图片已完成', partialFailed: '部分失败', notCreated: '未创建', queued: '排队中', success: '已完成', failed: '失败', pending: '待生成', generationFailedNotice: '有图片生成失败，积分已自动退回，可在结果区查看原因并重试。', failedRefund: '生成失败，本张图片积分已退回。可以点击重新生成，重试会重新扣除 {credits} 积分。'
  },
  'en-US': {
    loginTitle: 'Sign in to create a Listing set', loginDesc: 'Projects, uploads, copy generation, credit charges, and image results require sign in.',
    title: 'New Listing Set', stepsDesc: 'Flow: enter product info, generate Listing copy, then upload a reference image and confirm image generation.', refreshProject: 'Refresh project',
    stepProduct: 'Product info', stepUpload: 'Main image', stepOptimize: 'AI optimize', stepFacts: 'Confirm facts', stepResult: 'Results',
    productForm: 'Left: product form', productFormDesc: 'Save the project first, then upload images and generate copy.', brand: 'Brand', productName: 'Product name', targetMarket: 'Target marketplace', outputLanguage: 'Output language', category: 'Category', keywords: 'Title keywords', sellingKeywords: 'Bullet keywords', descriptionKeywords: 'Description keywords', material: 'Material', dimensions: 'Dimensions', dimensionsPlaceholder: 'Missing dimensions may affect dimension images',
    promptLabel: 'Other information / use cases / prompt', promptPlaceholder: 'Enter package contents, compatible models, colors, scenarios, and visual ideas',
    includeAplus: 'Also need an A+ image', includeAplusDesc: 'Operators often need Listing images and A+ modules together. When enabled, generating Listing images also creates an A+ task.', aplusModule: 'A+ module type', aplusSize: 'A+ size', aplusExtra: 'A+ extra requirements', aplusExtraPlaceholder: 'Optional. If empty, A+ requirements are built from product, brand, and Listing prompt.',
    aplusModuleBrandStory: 'Brand story banner', aplusModuleFeatures: 'Key feature module', aplusModuleLifestyle: 'Lifestyle scene module', aplusModuleSpecs: 'Specs / structure module',
    uploadAngle: 'Angle image', uploadDetail: 'Detail image', uploadPackage: 'Package image', uploadVariant: 'Variant image', referenceUrlLabel: 'Reference image URL', referenceUrlPlaceholder: 'Paste an image URL instead of uploading', linkReference: 'Link reference', submittedCost: 'Submitted charge',
    optimizing: 'Optimizing...', optimizePrompt: 'AI optimize prompt', saveAsNew: 'Create new project + copy', saveProject: 'Create project + generate Listing copy',
    optimizedPromptTitle: 'Optimized image prompt', optimizedPromptDesc: 'Used only as image-generation guidance. It is not saved as product facts.',
    referenceUpload: 'Right: reference upload', referenceUploadDesc: 'Main product image is required. Angle and detail images are optional.', mainImage: 'Main product image', uploadMain: 'Click to upload for white-background main image and product consistency', saveFirst: 'Save the product project first', generateCopyFirst: 'Generate Listing copy first', uploadedImages: 'Uploaded images', currentProject: 'Current project', imageTasks: 'Image tasks', estimatedCost: 'Estimated cost', aplusLinked: 'A+ link', created: 'Created',
    aiReview: 'AI optimization / compliance', aiReviewDesc: 'Generate title, bullets, and the copy basis for image generation.', generateCopy: 'Generate Listing copy', copyEmpty: 'Click "Create project + generate Listing copy" to show AI optimization and compliance results.',
    factsTitle: 'Confirm product facts', factsDesc: 'Confirmed facts are the single source of truth. Image tasks cannot be created before confirmation.', confirmFacts: 'Confirm facts',
    planTitle: 'Confirm image generation', planDesc: 'Upload a main/reference image, then generate the Listing images.', noPlan: 'No 7-image plan yet', noPlanDesc: 'Create a product and generate Listing copy first.', clothingReferenceRequired: 'Apparel products require at least one main product reference image. Without it, fit, color, pattern, and details cannot be promised as faithful.', currentBalance: 'Current balance', oneClickAplus: 'Generate Listing + A+ image', oneClickImages: 'Generate available images', addAplusOnly: 'Add A+ image (6 credits)', confirmGenerate: 'Confirm generation', confirmGenerateDesc: 'Review product, references, and credit cost before enqueueing.', confirmAplusOnlyDesc: 'The Listing images already exist. This creates one A+ image only and will not regenerate the Listing set.', insufficientCredits: 'Not enough credits for this generation. Top up or redeem credits first.', rechargeRedeem: 'Top up / redeem', prevStep: 'Previous', nextStep: 'Next', confirmSubmit: 'Confirm and generate', submitting: 'Submitting...', cancel: 'Cancel', previewProduct: 'Product info', previewReferences: 'References', previewBriefs: 'Image plan',
    resultDesc: 'The worker calls the configured upstream image model. Failed images are refunded automatically.', refreshTasks: 'Refresh task status', viewBrief: 'View brief', downloadView: 'Download / View', retryDeduct: 'Retry (charge {credits} credits)', noTasks: 'No image tasks.', creditsUnit: 'credits', copyImageUrl: 'Copy image URL', copyImageUrlDone: 'Image URL copied', copyPrompt: 'Copy prompt', copyPromptDone: 'Prompt copied', manualCopyHint: 'The browser blocked automatic copy. Select and copy the text below.', manualCopyTitle: 'Manual copy', manualCopyDesc: 'Click the text box to select all, then press Ctrl+C.', retryWithReference: 'Regenerate this slot', retryWithReferenceDesc: 'Paste an image URL, choose a local image, drag/drop or paste an image, and add notes. The system keeps this image slot, type, and credit charge.', retryReferenceImage: 'Retry reference image', retryReferenceImageHint: 'Choose a file, drag an image, paste a screenshot, or paste an image URL.', referenceTextLabel: 'Reference text', referenceTextPlaceholder: 'Paste selling points, layout notes, image text, or revision requests', confirmRetryGenerate: 'Confirm retry',
    restored: 'Historical project restored. You can upload references, refresh tasks, or view results.', defaultDescription: 'No product description provided yet', createdMsg: 'Project created. Generating Listing copy...', uploadedPrefix: 'Image uploaded: ', uploadCompressing: 'This image is large. Compressing it before upload...', uploadCompressed: 'Image was compressed and uploaded', uploadTooLarge: 'The image is still above the upload limit after compression. Please use a smaller image.', copyGeneratedMsg: 'AI copy and compliance checks are ready. Upload/confirm the main reference image, then generate images.', copyFallbackMsg: 'Listing copy is ready, but the upstream copy AI was unavailable, so PicPilot used a local safe fallback. Ask an admin to run a live AI config test.', promptOptimized: 'Prompt optimized with Amazon operator strategy', retryQueued: 'Failed image requeued', factsConfirmedMsg: 'Product facts confirmed. You can now create image tasks.',
    enqueueAplus: 'Listing tasks are queued. Creating linked A+ task...', enqueueImages: '7-image tasks are queued. Refresh task status later to see results.', aplusQueued: 'Listing and A+ tasks are queued. A+ reused current set references. A+ project: ', openAplusResult: 'Open A+ result', continueAplusEdit: 'Edit A+ image', aplusFailed: 'Listing tasks queued, but A+ creation failed: ', aplusFallback: 'Generate it later from the A+ page',
    loadFailed: 'Failed to load project details', createFailed: 'Failed to create project', uploadFailed: 'Image upload failed', linkFailed: 'Failed to save image link', copyFailed: 'Copy generation failed', promptFailed: 'Prompt optimization failed', retryFailed: 'Retry failed', factsFailed: 'Fact confirmation failed. Check JSON format.', enqueueFailed: 'Failed to create image tasks',
    draft: 'Draft', copyGenerated: 'Copy generated', factsConfirmed: 'Facts confirmed', running: 'Generating', imagesCompleted: 'Images completed', partialFailed: 'Partially failed', notCreated: 'Not created', queued: 'Queued', success: 'Completed', failed: 'Failed', pending: 'Pending', generationFailedNotice: 'Some image generation failed. Credits were refunded automatically. Check the result area for the reason and retry.', failedRefund: 'Generation failed. This image was refunded. Retry will charge {credits} credits again.'
  },
  'ja-JP': {
    loginTitle: 'ログイン後に Listing セットを作成', loginDesc: 'プロジェクト、アップロード、コピー生成、ポイント消費、画像結果の確認にはログインが必要です。',
    title: 'Listing 7枚を作成', stepsDesc: '流れ：商品情報を入力して Listing コピーを生成し、参考画像をアップロードして画像生成を確認します。', refreshProject: '現在のプロジェクトを更新',
    stepProduct: '商品情報', stepUpload: 'メイン画像', stepOptimize: 'AI 最適化', stepFacts: '事実確認', stepResult: '生成結果',
    productForm: '左：商品情報フォーム', productFormDesc: '先にプロジェクトを保存し、画像アップロードとコピー生成へ進みます。', brand: 'ブランド名', productName: '商品名', targetMarket: '対象サイト', outputLanguage: '出力言語', category: '商品カテゴリ', keywords: 'タイトルキーワード', sellingKeywords: '箇条書きキーワード', descriptionKeywords: '説明キーワード', material: '素材', dimensions: 'サイズ', dimensionsPlaceholder: '未入力の場合、サイズ画像に影響します',
    promptLabel: 'その他情報 / 利用シーン / Prompt', promptPlaceholder: '同梱物、対応モデル、カラー、利用シーン、画面アイデアなどを入力',
    includeAplus: 'A+ 画像も必要', includeAplusDesc: 'Listing 7枚と A+ モジュールを同時に必要とするケース向け。オンにすると Listing 生成時に A+ タスクも作成します。', aplusModule: 'A+ モジュールタイプ', aplusSize: 'A+ サイズ', aplusExtra: 'A+ 補足要件', aplusExtraPlaceholder: '任意。未入力時は商品名、ブランド、Listing Prompt から A+ 要件を作成します。',
    aplusModuleBrandStory: 'ブランドストーリーバナー', aplusModuleFeatures: '主要訴求モジュール', aplusModuleLifestyle: 'ライフスタイルシーン', aplusModuleSpecs: '仕様 / 構造説明',
    uploadAngle: '別角度画像', uploadDetail: '詳細画像', uploadPackage: 'パッケージ画像', uploadVariant: 'バリエーション画像', referenceUrlLabel: '参考画像URL', referenceUrlPlaceholder: 'アップロードの代わりに画像URLを貼り付け', linkReference: '参照をリンク', submittedCost: '送信済み消費',
    optimizing: '最適化中...', optimizePrompt: 'AI で Prompt 最適化', saveAsNew: '新規プロジェクト + コピー生成', saveProject: 'プロジェクト作成 + Listing コピー生成',
    optimizedPromptTitle: '最適化された画像 Prompt', optimizedPromptDesc: '画像生成の参考にのみ使用され、商品事実には保存されません。',
    referenceUpload: '右：参考画像アップロード', referenceUploadDesc: 'メイン商品画像は必須、多角度・詳細画像は任意です。', mainImage: 'メイン商品画像', uploadMain: 'クリックしてアップロード。白背景メイン画像と商品一貫性に利用します', saveFirst: '先に商品プロジェクトを保存してください', generateCopyFirst: '先に Listing コピーを生成してください', uploadedImages: 'アップロード済み画像', currentProject: '現在のプロジェクト', imageTasks: '画像タスク', estimatedCost: '予定消費', aplusLinked: 'A+ 連携', created: '作成済み',
    aiReview: 'AI 最適化 / コンプライアンス審査', aiReviewDesc: 'タイトル、箇条書き、画像生成に必要なコピー根拠を生成します。', generateCopy: 'Listing コピーを生成', copyEmpty: '「プロジェクト作成 + Listing コピー生成」を押すと、AI 最適化と審査結果が表示されます。',
    factsTitle: '商品事実を確認', factsDesc: 'confirmed facts が唯一の事実ソースです。確認前は画像タスクを作成できません。', confirmFacts: '事実を確認',
    planTitle: '画像生成を確認', planDesc: 'メイン/参考画像をアップロードしてから Listing 画像を生成します。', noPlan: '7枚プランはまだありません', noPlanDesc: '先に商品を作成し、Listing コピーを生成してください。', clothingReferenceRequired: 'アパレル商品は少なくとも1枚のメイン参考画像が必要です。参考画像なしでは型、色、柄、細部の忠実性を保証できません。', currentBalance: '現在残高', oneClickAplus: 'Listing + A+ を生成', oneClickImages: '利用可能な画像を生成', addAplusOnly: 'A+画像を追加（6ポイント）', confirmGenerate: '生成を確認', confirmGenerateDesc: '商品、参考画像、ポイント消費を確認してからキューに追加します。', confirmAplusOnlyDesc: 'Listing 7枚は作成済みです。今回はA+画像1枚のみを作成し、Listing画像は再生成しません。', insufficientCredits: 'ポイントが不足しています。先にチャージまたは交換してください。', rechargeRedeem: 'チャージ/交換へ', prevStep: '前へ', nextStep: '次へ', confirmSubmit: '確認して生成', submitting: '送信中...', cancel: 'キャンセル', previewProduct: '商品情報', previewReferences: '参考画像', previewBriefs: '画像プラン',
    resultDesc: 'Worker が設定済みの上流画像モデルを呼び出します。失敗した画像は自動返還されます。', refreshTasks: 'タスク状態を更新', viewBrief: 'Brief を表示', downloadView: 'ダウンロード / 表示', retryDeduct: '再生成（{credits} ポイント消費）', noTasks: '画像タスクはありません。', creditsUnit: 'ポイント', copyImageUrl: '画像URLをコピー', copyImageUrlDone: '画像URLをコピーしました', copyPrompt: 'Promptをコピー', copyPromptDone: 'Promptをコピーしました', manualCopyHint: 'ブラウザにより自動コピーが制限されました。下の内容を手動でコピーしてください。', manualCopyTitle: '手動コピー', manualCopyDesc: 'テキストボックスをクリックすると全選択できます。Ctrl+Cでコピーしてください。', retryWithReference: 'この枠を再生成', retryWithReferenceDesc: '画像URL、ローカル画像選択、ドラッグ/貼り付け画像、補足テキストを利用できます。画像枠、タイプ、消費ポイントは保持されます。', retryReferenceImage: '再生成の参考画像', retryReferenceImageHint: 'ファイル選択、ドラッグ、スクリーンショット貼り付け、画像URL貼り付けができます。', referenceTextLabel: '参考テキスト', referenceTextPlaceholder: '訴求点、構図、画像内テキスト、修正要望など', confirmRetryGenerate: '再生成を確認',
    restored: '履歴プロジェクトを復元しました。参考画像アップロード、タスク更新、結果確認を続けられます。', defaultDescription: '商品説明はまだ入力されていません', createdMsg: 'プロジェクトを作成しました。Listing コピーを生成しています...', uploadedPrefix: '画像をアップロードしました：', uploadCompressing: '画像が大きいため、自動圧縮してアップロードしています...', uploadCompressed: '画像を自動圧縮してアップロードしました', uploadTooLarge: '圧縮後もアップロード上限を超えています。より小さい画像を使用してください。', copyGeneratedMsg: 'AI コピーと審査が完了しました。メイン参考画像をアップロード/確認して画像を生成してください。', copyFallbackMsg: 'Listing コピーは生成済みですが、上流コピー AI が利用できないためローカル安全フォールバックを使用しました。管理者に AI 設定の実テストを依頼してください。', promptOptimized: 'Amazon 運用者向け戦略で Prompt を最適化しました', retryQueued: '失敗画像をキューに戻しました', factsConfirmedMsg: '商品事実を確認しました。画像タスクを作成できます。',
    enqueueAplus: 'Listing 7枚タスクをキューに追加しました。A+ タスクも作成中です...', enqueueImages: '7枚タスクを Worker キューに追加しました。後で状態を更新してください。', aplusQueued: 'Listing と A+ タスクを同時にキュー投入しました。A+ は現在セットの参考画像を再利用しました。A+ プロジェクト：', openAplusResult: 'A+ 完成を見る', continueAplusEdit: 'A+ を編集', aplusFailed: 'Listing はキュー投入済みですが、A+ 作成に失敗しました：', aplusFallback: '後で A+ ページから生成してください',
    loadFailed: 'プロジェクト詳細の読み込みに失敗しました', createFailed: 'プロジェクト作成に失敗しました', uploadFailed: '画像アップロードに失敗しました', linkFailed: '画像リンクの保存に失敗しました', copyFailed: 'コピー生成に失敗しました', promptFailed: 'Prompt 最適化に失敗しました', retryFailed: '再生成に失敗しました', factsFailed: '事実確認に失敗しました。JSON 形式を確認してください。', enqueueFailed: '画像タスク作成に失敗しました',
    draft: '下書き', copyGenerated: 'コピー生成済み', factsConfirmed: '事実確認済み', running: '生成中', imagesCompleted: '画像完了', partialFailed: '一部失敗', notCreated: '未作成', queued: '待機中', success: '完了', failed: '失敗', pending: '生成待ち', generationFailedNotice: '一部の画像生成に失敗しました。ポイントは自動返還済みです。結果エリアで理由を確認し、再試行できます。', failedRefund: '生成に失敗しました。この画像のポイントは返還済みです。再試行時は {credits} ポイントを再度消費します。'
  }
}

export function ListingWorkflow({ embedded = false }: { embedded?: boolean } = {}) {
  const t = useI18n()
  const { billingEnabled } = useRuntime()
  const lang = useLanguage()
  const originalCopy = listingText[lang] || listingText['zh-CN']
  const copy = billingEnabled ? originalCopy : { ...originalCopy, loginDesc: t('generationConfirmPersonal'), confirmGenerateDesc: t('generationConfirmPersonal'), addAplusOnly: t('generateAplus'), resultDesc: t('refreshHint'), retryDeduct: t('retryGenerate'), failedRefund: t('generationFailedPersonal'), retryWithReferenceDesc: t('generationConfirmPersonal') }
  const [productName, setProductName] = useState('')
  const [brand, setBrand] = useState('')
  const [country, setCountry] = useState('US')
  const [language, setLanguage] = useState('en-US')
  const [description, setDescription] = useState('')
  const [optimizedPrompt, setOptimizedPrompt] = useState('')
  const [keywords, setKeywords] = useState('')
  const [sellingKeywords, setSellingKeywords] = useState('')
  const [descriptionKeywords, setDescriptionKeywords] = useState('')
  const [category, setCategory] = useState('')
  const [material, setMaterial] = useState('')
  const [size, setSize] = useState('')
  const [project, setProject] = useState<ProjectSummary | null>(null)
  const [copyTitle, setCopyTitle] = useState('')
  const [copyBullets, setCopyBullets] = useState<string[]>([])
  const [candidateFacts, setCandidateFacts] = useState<Record<string, any>>({})
  const [factText, setFactText] = useState('{}')
  const [warnings, setWarnings] = useState<string[]>([])
  const [briefs, setBriefs] = useState<Array<any>>([])
  const [productImages, setProductImages] = useState<ProductImageView[]>([])
  const [tasks, setTasks] = useState<ImageTaskView[]>([])
  const [credits, setCredits] = useState<number | null>(null)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [optimizingPrompt, setOptimizingPrompt] = useState(false)
  const [includeAplus, setIncludeAplus] = useState(false)
  const [aplusModule, setAplusModule] = useState(APLUS_MODULE_OPTIONS[1].value)
  const [aplusSize, setAplusSize] = useState(APLUS_OUTPUT_SIZE_OPTIONS[0].value)
  const [aplusPrompt, setAplusPrompt] = useState('')
  const [aplusProject, setAplusProject] = useState<ProjectSummary | null>(null)
  const [linkedAplusProjectId, setLinkedAplusProjectId] = useState('')
  const [linkedAplusProjectName, setLinkedAplusProjectName] = useState('')
  const [referenceFiles, setReferenceFiles] = useState<Record<string, File | null>>({})
  const [referenceUrls, setReferenceUrls] = useState<Record<string, string>>({})
  const [referencePreviews, setReferencePreviews] = useState<Record<string, string>>({})
  const [workflowStep, setWorkflowStep] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const enqueueRef = useRef(false)
  const [retryingTask, setRetryingTask] = useState<ImageTaskView | null>(null)
  const [retryReferenceFile, setRetryReferenceFile] = useState<File | null>(null)
  const [retryReferencePreview, setRetryReferencePreview] = useState('')
  const [retryReferenceUrl, setRetryReferenceUrl] = useState('')
  const [retryReferenceText, setRetryReferenceText] = useState('')
  const [manualCopyText, setManualCopyText] = useState('')

  useEffect(() => {
    if (!requireLogin()) return
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => {})
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const template = getTemplateById(params.get('templateId'))
    if (template?.routeType === 'listing' && template.listingPreset) {
      const preset = template.listingPreset
      if (preset.brand !== undefined) setBrand(preset.brand)
      if (preset.productName !== undefined) setProductName(preset.productName)
      if (preset.country) setCountry(preset.country)
      if (preset.language) setLanguage(preset.language)
      if (preset.category) setCategory(preset.category)
      if (preset.keywords) setKeywords(preset.keywords)
      if (preset.sellingKeywords) setSellingKeywords(preset.sellingKeywords)
      if (preset.descriptionKeywords) setDescriptionKeywords(preset.descriptionKeywords)
      if (preset.material) setMaterial(preset.material)
      if (preset.dimensions) setSize(preset.dimensions)
      if (preset.description) setDescription(preset.description)
      if (typeof preset.includeAplus === 'boolean') setIncludeAplus(preset.includeAplus)
      if (preset.aplusModule) setAplusModule(normalizeAplusModule(preset.aplusModule))
      if (preset.aplusPrompt) setAplusPrompt(preset.aplusPrompt)
      setMessage(`已套用模板：${template.title}。请补充品牌、商品名和真实参数后生成文案。`)
    }
    const incomingPrompt = (params.get('prompt') || '').trim()
    if (incomingPrompt) {
      const prefill = inferListingPrefill(incomingPrompt)
      setDescription(prefill.prompt)
      if (prefill.brand) setBrand(prefill.brand)
      if (prefill.productName) setProductName(prefill.productName)
      if (prefill.country) setCountry(prefill.country)
      if (prefill.language) setLanguage(prefill.language)
      if (prefill.category) setCategory(prefill.category)
      if (prefill.material) setMaterial(prefill.material)
      if (prefill.dimensions) setSize(prefill.dimensions)
      if (prefill.titleKeywords) setKeywords(prefill.titleKeywords)
      if (prefill.sellingKeywords) setSellingKeywords(prefill.sellingKeywords)
      if (prefill.descriptionKeywords) setDescriptionKeywords(prefill.descriptionKeywords)
      setMessage('已从 AI 助手带入 Listing 需求，请检查商品信息后创建项目。')
    }
    const projectId = params.get('projectId')
    if (projectId) loadProject(projectId)
  }, [])

  useEffect(() => {
    if (!project || !tasks.some(task => task.status === 'queued' || task.status === 'running')) return
    const timer = window.setInterval(() => loadProject(project.id, { restoredMessage: false }), 8000)
    return () => window.clearInterval(timer)
  }, [project?.id, tasks])

  function hydrateProject(detail: Awaited<ReturnType<typeof getProject>>, options: { preserveAplusSelection?: boolean } = {}) {
    const originalInfo = detail.original_info || {}
    const generated = detail.generated_copy || {}
    const facts = Object.keys(detail.confirmed_facts || {}).length ? detail.confirmed_facts : (generated.confirmed_facts_candidate || {})

    setProject(detail.project)
    setBrand(detail.project.brand || '')
    setProductName(detail.project.product_name || '')
    setCountry(detail.project.country || 'US')
    setLanguage(detail.project.language || 'en-US')
    setDescription(extractText(originalInfo.description))
    setOptimizedPrompt(extractText(originalInfo.visual_prompt) || extractText(generated.visual_prompt))
    setCategory(extractText(originalInfo.category))
    setMaterial(extractText(originalInfo.material))
    setSize(extractText(originalInfo.dimensions))
    setKeywords(extractArray(originalInfo.title_keywords).length ? extractArray(originalInfo.title_keywords).join(', ') : extractArray(originalInfo.keywords).join(', '))
    setSellingKeywords(extractArray(originalInfo.selling_point_keywords).join(', '))
    setDescriptionKeywords(extractArray(originalInfo.description_keywords).join(', '))
    if (!options.preserveAplusSelection) {
      setIncludeAplus(Boolean(originalInfo.include_aplus))
      setAplusModule(normalizeAplusModule(extractText(originalInfo.aplus_module)))
      setAplusSize(normalizeAplusOutputSize(extractText(originalInfo.aplus_size)))
      setAplusPrompt(extractText(originalInfo.aplus_prompt))
    }
    setLinkedAplusProjectId(extractText(originalInfo.linked_aplus_project_id))
    setLinkedAplusProjectName(extractText(originalInfo.linked_aplus_product_name))
    setCopyTitle(generated.title || '')
    setCopyBullets(extractArray(generated.bullets))
    setCandidateFacts(facts)
    setFactText(JSON.stringify(facts, null, 2))
    const hasMainImage = (detail.product_images || []).some(image => (image.image_type || '').split(',')[0] === 'main')
    setWarnings(extractArray(generated.compliance?.warnings).filter(item => !(hasMainImage && item.includes('缺少主商品图'))))
    setBriefs(Array.isArray(generated.image_briefs) ? generated.image_briefs : [])
    setProductImages(detail.product_images || [])
    setTasks(detail.image_tasks || [])
  }

  async function loadProject(projectId: string, options: { restoredMessage?: boolean } = {}) {
    setIsLoading(true)
    setError('')
    try {
      const detail = await getProject(projectId)
      const hadFailure = !tasks.some(task => task.status === 'failed') && detail.image_tasks.some(task => task.status === 'failed')
      hydrateProject(detail, { preserveAplusSelection: options.restoredMessage === false })
      if (options.restoredMessage !== false) {
        setWorkflowStep(detail.image_tasks.length ? 2 : detail.project.status === 'draft' ? 0 : 1)
      }
      if (hadFailure) {
        setMessage(detail.project.latest_failure_reason || copy.generationFailedNotice)
      } else if (options.restoredMessage !== false) {
        setMessage(copy.restored)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.loadFailed)
    } finally {
      setIsLoading(false)
    }
  }

  function applyGeneratedCopy(data: any, baseProject: ProjectSummary) {
    const generated = data.generated_copy || {}
    setCopyTitle(generated.title || '')
    setCopyBullets(generated.bullets || [])
    const facts = generated.confirmed_facts_candidate || {}
    setCandidateFacts(facts)
    setFactText(JSON.stringify(facts, null, 2))
    setWarnings(extractArray(generated.compliance?.warnings))
    setBriefs(generated.image_briefs || [])
    setProject({ ...baseProject, status: data.status })
  }

  function copyReadyMessage(data: any) {
    const generated = data.generated_copy || {}
    return generated.ai_mode === 'local-safe-copy' ? copy.copyFallbackMsg : copy.copyGeneratedMsg
  }

  async function handleCreateProject() {
    if (!brand || !productName) return
    setIsLoading(true)
    setMessage('')
    setError('')
    let data: ProjectSummary | null = null
    try {
      data = await createProject({
        productName,
        brand,
        country,
        language,
        description: description.trim(),
        category,
        material,
        dimensions: size,
        visualPrompt: optimizedPrompt,
        keywords: keywords.split(',').map(item => item.trim()).filter(Boolean),
        title_keywords: keywords.split(',').map(item => item.trim()).filter(Boolean),
        selling_point_keywords: sellingKeywords.split(',').map(item => item.trim()).filter(Boolean),
        description_keywords: descriptionKeywords.split(',').map(item => item.trim()).filter(Boolean),
        includeAplus,
        aplusModule,
        aplusSize,
        aplusPrompt: aplusPrompt.trim()
      })
      setProject(data)
      setTasks([])
      setProductImages([])
      setReferenceFiles({})
      setReferenceUrls({})
      setReferencePreviews({})
      setAplusProject(null)
      setLinkedAplusProjectName('')
      setCopyTitle('')
      setCopyBullets([])
      setCandidateFacts({})
      setFactText('{}')
      setBriefs([])
      setWarnings([])
      window.history.replaceState(null, '', `/commerce?tab=listing&projectId=${data.id}`)
      setMessage(copy.createdMsg)
      setWorkflowStep(1)
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.createFailed)
      setIsLoading(false)
      return
    }
    try {
      const generatedCopy = await generateCopy(data.id)
      applyGeneratedCopy(generatedCopy, data)
      setMessage(copyReadyMessage(generatedCopy))
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.copyFailed)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleUpload(imageType: string, file?: File) {
    if (!project || !file) return
    setIsLoading(true)
    setError('')
    try {
      if (file.size > TARGET_UPLOAD_IMAGE_BYTES) {
        setMessage(copy.uploadCompressing)
      }
      const compression = await compressImageForUpload(file)
      if (compression.file.size > MAX_UPLOAD_IMAGE_BYTES) {
        clearReferenceFile(imageType)
        throw new Error(copy.uploadTooLarge)
      }
      if (compression.compressed) {
        setReferenceFile(imageType, compression.file)
      }
      const image = await uploadProductImage(project.id, imageType, compression.file)
      setProductImages(prev => [image, ...prev])
      const normalized = (image.image_type || '').split(',')[0]
      setUploadedReference(imageType, image)
      setMessage(copy.uploadedPrefix + imageTypeLabel(normalized || imageType) + (compression.compressed ? `. ${copy.uploadCompressed}` : ''))
      loadProject(project.id, { restoredMessage: false }).catch(() => {})
    } catch (err) {
      clearReferenceFile(imageType)
      setError(err instanceof Error ? err.message : copy.uploadFailed)
    } finally {
      setIsLoading(false)
    }
  }

  function setReferenceFile(imageType: string, file: File) {
    setReferenceFiles(prev => ({ ...prev, [imageType]: file }))
    setReferencePreviews(prev => {
      if (prev[imageType]?.startsWith('blob:')) URL.revokeObjectURL(prev[imageType])
      return { ...prev, [imageType]: URL.createObjectURL(file) }
    })
  }

  function clearReferenceFile(imageType: string) {
    setReferenceFiles(prev => ({ ...prev, [imageType]: null }))
    setReferencePreviews(prev => {
      if (prev[imageType]?.startsWith('blob:')) URL.revokeObjectURL(prev[imageType])
      const next = { ...prev }
      delete next[imageType]
      return next
    })
  }

  function setUploadedReference(imageType: string, image: ProductImageView) {
    setReferenceFiles(prev => ({ ...prev, [imageType]: null }))
    setReferencePreviews(prev => {
      if (prev[imageType]?.startsWith('blob:')) URL.revokeObjectURL(prev[imageType])
      return { ...prev, [imageType]: assetUrl(image.url) }
    })
  }

  async function handleLink(imageType: string) {
    if (!project) return
    const url = (referenceUrls[imageType] || '').trim()
    if (!url) return
    setIsLoading(true)
    setError('')
    try {
      const image = await linkProductImage(project.id, imageType, url)
      setProductImages(prev => [image, ...prev])
      setMessage(copy.uploadedPrefix + imageTypeLabel(image.image_type || imageType))
      loadProject(project.id, { restoredMessage: false }).catch(() => {})
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.linkFailed)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleGenerateCopy() {
    if (!project) return
    setIsLoading(true)
    setError('')
    try {
      const data = await generateCopy(project.id)
      applyGeneratedCopy(data, project)
      setMessage(copyReadyMessage(data))
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.copyFailed)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleOptimizePrompt() {
    setOptimizingPrompt(true)
    setError('')
    try {
      const data = await optimizePrompt({ prompt: description || productName, productName, marketplace: `Amazon ${country}`, language })
      setOptimizedPrompt(data.optimized_prompt)
      setMessage(copy.promptOptimized)
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.promptFailed)
    } finally {
      setOptimizingPrompt(false)
    }
  }

  async function copyText(value: string, label: string) {
    if (!value) return
    const copied = await copyToClipboard(value)
    if (copied) {
      setManualCopyText('')
      setMessage(label)
    } else {
      setManualCopyText(value)
      setMessage(copy.manualCopyHint)
    }
  }

  function clearRetryReferenceFile() {
    setRetryReferenceFile(null)
    setRetryReferencePreview(current => {
      if (current.startsWith('blob:')) URL.revokeObjectURL(current)
      return ''
    })
  }

  function selectRetryReferenceFile(file: File) {
    setRetryReferenceFile(file)
    setRetryReferencePreview(current => {
      if (current.startsWith('blob:')) URL.revokeObjectURL(current)
      return URL.createObjectURL(file)
    })
  }

  function closeRetry() {
    setRetryingTask(null)
    clearRetryReferenceFile()
    setRetryReferenceUrl('')
    setRetryReferenceText('')
  }

  function openRetry(task: ImageTaskView) {
    clearRetryReferenceFile()
    setRetryingTask(task)
    setRetryReferenceUrl(task.result_url || '')
    setRetryReferenceText('')
  }

  async function handleRetryTask(taskId: string, input: { referenceUrl?: string; referenceText?: string; reference?: File | null } = {}) {
    if (!project) return
    setIsLoading(true)
    setError('')
    try {
      const updated = await retryProjectImage(project.id, taskId, input)
      setTasks(rows => rows.map(row => row.id === taskId ? updated : row))
      setProject(current => current ? { ...current, status: 'images_queued' } : current)
      closeRetry()
      setMessage(copy.retryQueued)
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.retryFailed)
    } finally {
      setIsLoading(false)
    }
  }

  function requestEnqueueImages() {
    if (!project) return
    if (credits !== null && estimatedGenerationCost > credits) {
      setError(copy.insufficientCredits)
      return
    }
    setError('')
    setConfirmOpen(true)
  }

  function requestAplusRecovery() {
    if (!project || linkedAplusId) return
    if (credits !== null && credits < 6) {
      setError(copy.insufficientCredits)
      return
    }
    setIncludeAplus(true)
    setError('')
    setConfirmOpen(true)
  }

  async function handleEnqueueImages() {
    if (!project) return
    if (enqueueRef.current) return
    enqueueRef.current = true
    setConfirmOpen(false)
    setIsLoading(true)
    setError('')
    try {
      const result = await enqueueListingPackage(project.id, {
        includeAplus,
        aplusModule: copy[APLUS_MODULE_OPTIONS.find(item => item.value === aplusModule)?.labelKey || 'aplusModuleFeatures'],
        aplusSize,
        aplusPrompt: aplusPrompt.trim() || `${productName} Amazon A+ content module. Highlight key selling points, brand trust, usage scenario, and leave safe space for short text overlays.`,
        language
      })
      const data = result.image_tasks
      setTasks(data)
      setProject({ ...project, status: 'images_queued', total_images: data.length, credits_cost: data.reduce((sum, item) => sum + item.credits_cost, 0) })
      let nextMessage = copy.enqueueImages
      if (result.aplus_project) {
        setAplusProject(result.aplus_project.project)
        setLinkedAplusProjectId(result.aplus_project.project.id)
        setLinkedAplusProjectName(result.aplus_project.project.product_name)
        nextMessage = `${copy.aplusQueued}${result.aplus_project.project.product_name}`
      }
      setMessage(nextMessage)
      getOptionalBillingSummary().then(summary => {
        setCredits(summary?.credits ?? null)
        window.dispatchEvent(new Event('openimage:credits-updated'))
      }).catch(() => {})
      getMe({ passive: true }).then(saveStoredUser).catch(() => {})
    } catch (err) {
      setError(err instanceof Error ? err.message : copy.enqueueFailed)
    } finally {
      enqueueRef.current = false
      setIsLoading(false)
    }
  }

  const mainImage = productImages.find(image => (image.image_type || '').split(',')[0] === 'main')
  const clothingText = `${category} ${productName} ${description} ${material}`.toLowerCase()
  const isClothingListing = /服装|衣服|t恤|短袖|上衣|裙|裤|外套|衬衫|卫衣|apparel|clothing|shirt|t-shirt|dress|skirt|pants|hoodie|jacket/.test(clothingText)
  const canCreateImages = Boolean(copyTitle) || project?.status === 'copy_generated' || project?.status === 'facts_confirmed' || project?.status === 'images_queued' || project?.status === 'images_completed'
  const canUploadReferences = Boolean(project) && canCreateImages
  const listingCost = briefs.reduce((sum, item) => sum + (Number(item.credits_cost) || 0), 0)
  const hasExistingTasks = tasks.length > 0
  const linkedAplusName = aplusProject?.product_name || linkedAplusProjectName
  const linkedAplusId = aplusProject?.id || linkedAplusProjectId
  const listingCharge = hasExistingTasks ? 0 : (listingCost || (canCreateImages ? 42 : 0))
  const aplusCharge = includeAplus && !linkedAplusId ? 6 : 0
  const estimatedGenerationCost = listingCharge + aplusCharge
  const submittedListingCost = tasks.reduce((sum, item) => sum + (Number(item.credits_cost) || 0), 0)
  const submittedTotalCost = submittedListingCost + (linkedAplusId ? 6 : 0)
  const hasInsufficientCredits = credits !== null && estimatedGenerationCost > credits
  const listingWorkflowSteps = [copy.stepProduct, copy.stepUpload, copy.confirmGenerate]
  const aplusModuleLabel = copy[APLUS_MODULE_OPTIONS.find(item => item.value === aplusModule)?.labelKey || 'aplusModuleFeatures']
  const imageTypeLabels: Record<string, string> = {
    main: copy.mainImage,
    angle: copy.uploadAngle,
    detail: copy.uploadDetail,
    package: copy.uploadPackage,
    variant: copy.uploadVariant,
    reference: copy.referenceUpload
  }
  const imageTypeLabel = (type: string) => imageTypeLabels[(type || '').split(',')[0]] || type
  const uploadTypes = [
    { value: 'angle', label: copy.uploadAngle },
    { value: 'detail', label: copy.uploadDetail },
    { value: 'package', label: copy.uploadPackage },
    { value: 'variant', label: copy.uploadVariant }
  ]
  const content = (
    <UserAuthGate title={copy.loginTitle} description={copy.loginDesc}>
        <div className="page-head">
        <div>
          <h1>{copy.title}</h1>
          <p>{copy.stepsDesc}</p>
        </div>
        {project ? <div className="page-actions"><button className="btn btn-secondary" disabled={isLoading} onClick={() => loadProject(project.id)}>{copy.refreshProject}</button></div> : null}
      </div>

      {(message || error) ? <div className={error || isAiDisabledError(message) ? 'notice notice-error' : 'notice'}>{error || message}</div> : null}
      {manualCopyText ? (
        <div className="manual-copy-box">
          <strong>{copy.manualCopyTitle}</strong>
          <p>{copy.manualCopyDesc}</p>
          <textarea className="textarea" readOnly value={manualCopyText} onFocus={event => event.currentTarget.select()} />
        </div>
      ) : null}

      <TaskFlowSteps steps={listingWorkflowSteps} currentStep={workflowStep} ariaLabel={copy.stepsDesc} onStepChange={setWorkflowStep} />

      {workflowStep === 0 ? (
        <section className="panel task-flow-panel listing-flow-card listing-product-card">
          <div className="panel-head"><div><h2>{copy.productForm}</h2><p>{copy.productFormDesc}</p></div></div>
          <div className="panel-body form-grid">
            <div className="grid-2">
              <label className="field"><span>{copy.brand} <b className="required">*</b></span><input className="input" value={brand} onChange={event => setBrand(event.target.value)} placeholder="RAYAKT" /></label>
              <label className="field"><span>{copy.productName} <b className="required">*</b></span><input className="input" value={productName} onChange={event => setProductName(event.target.value)} placeholder="Helmet Chin Mount" /></label>
            </div>
            <div className="grid-2">
              <label className="field"><span>{copy.targetMarket} <b className="required">*</b></span><select className="select" value={country} onChange={event => setCountry(event.target.value)}><option value="DE">DE</option><option value="FR">FR</option><option value="US">US</option></select></label>
              <label className="field"><span>{copy.outputLanguage} <b className="required">*</b></span><select className="select" value={language} onChange={event => setLanguage(event.target.value)}><option value="en-US">English</option><option value="de-DE">Deutsch</option><option value="fr-FR">Français</option></select></label>
            </div>
            <div className="grid-2">
              <label className="field"><span>{copy.category}</span><input className="input" value={category} onChange={event => setCategory(event.target.value)} placeholder="Sports & Outdoors" /></label>
              <label className="field"><span>{copy.keywords}</span><input className="input" value={keywords} onChange={event => setKeywords(event.target.value)} placeholder="helmet mount, chin mount" /></label>
            </div>
            <div className="grid-2">
              <label className="field"><span>{copy.sellingKeywords}</span><input className="input" value={sellingKeywords} onChange={event => setSellingKeywords(event.target.value)} placeholder="easy installation, stable shooting" /></label>
              <label className="field"><span>{copy.descriptionKeywords}</span><input className="input" value={descriptionKeywords} onChange={event => setDescriptionKeywords(event.target.value)} placeholder="hands-free recording, outdoor cycling" /></label>
            </div>
            <div className="grid-2">
              <label className="field"><span>{copy.material}</span><input className="input" value={material} onChange={event => setMaterial(event.target.value)} placeholder="Plastic" /></label>
              <label className="field"><span>{copy.dimensions}</span><input className="input" value={size} onChange={event => setSize(event.target.value)} placeholder={copy.dimensionsPlaceholder} /></label>
            </div>
            <label className="field"><span>{copy.promptLabel}</span><textarea className="textarea" value={description} onChange={event => setDescription(event.target.value)} placeholder={copy.promptPlaceholder} /></label>
            <div className="linked-option-card">
              <label className="switch-row">
                <input type="checkbox" checked={includeAplus} onChange={event => setIncludeAplus(event.target.checked)} />
                <span><strong>{copy.includeAplus}</strong><small>{copy.includeAplusDesc}</small></span>
              </label>
              {includeAplus ? <div className="grid-2 linked-fields">
                <label className="field"><span>{copy.aplusModule}</span><select className="select" value={aplusModule} onChange={event => setAplusModule(event.target.value)}>{APLUS_MODULE_OPTIONS.map(option => <option value={option.value} key={option.value}>{copy[option.labelKey]}</option>)}</select></label>
                <label className="field"><span>{copy.aplusSize}</span><select className="select" value={aplusSize} onChange={event => setAplusSize(event.target.value)}>{APLUS_OUTPUT_SIZE_OPTIONS.map(option => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>
                <label className="field" style={{ gridColumn: '1 / -1' }}><span>{copy.aplusExtra}</span><textarea className="textarea" value={aplusPrompt} onChange={event => setAplusPrompt(event.target.value)} placeholder={copy.aplusExtraPlaceholder} /></label>
              </div> : null}
            </div>
            <div className="section-action-card">
              <div className="section-action-title"><span>{copy.aiReview}</span></div>
              <p className="section-action-copy">{copy.aiReviewDesc}</p>
              <button className="btn btn-secondary" type="button" disabled={!description.trim() || optimizingPrompt} onClick={handleOptimizePrompt}>{optimizingPrompt ? copy.optimizing : copy.optimizePrompt}</button>
              {optimizedPrompt ? <div className="optimized-prompt-preview"><strong>{copy.optimizedPromptTitle}</strong><p>{copy.optimizedPromptDesc}</p><pre>{optimizedPrompt}</pre></div> : null}
            </div>
          </div>
          <div className="task-flow-actions">
            <span className="muted">{project ? `${copy.currentProject}: ${project.product_name}` : copy.productFormDesc}</span>
            <div className="task-flow-action-buttons">
              {project && canCreateImages ? <button className="btn btn-secondary" disabled={isLoading} onClick={handleCreateProject}>{copy.saveAsNew}</button> : null}
              <button className="btn btn-primary" disabled={!brand || !productName || isLoading} onClick={project && canCreateImages ? () => setWorkflowStep(1) : handleCreateProject}>
                {project && canCreateImages ? <>{copy.nextStep}<ArrowRight size={16} /></> : copy.saveProject}
              </button>
            </div>
          </div>
        </section>
      ) : null}

      {workflowStep === 1 ? (
        <>
          <div className="grid-2 task-flow-content-grid">
            <section className="panel listing-flow-card listing-reference-card">
              <div className="panel-head"><div><h2>{copy.referenceUpload}</h2><p>{copy.referenceUploadDesc}</p></div></div>
              <div className="panel-body form-grid">
                <ImageReferenceInput label={`${copy.mainImage} *`} hint={!project ? copy.saveFirst : canUploadReferences ? copy.uploadMain : copy.generateCopyFirst} disabled={!canUploadReferences || isLoading} file={referenceFiles.main} url={referenceUrls.main || ''} previewUrl={referencePreviews.main || (mainImage ? assetUrl(mainImage.url) : '')} onFile={file => setReferenceFile('main', file)} onClearFile={() => clearReferenceFile('main')} onUpload={file => handleUpload('main', file)} onUrl={url => setReferenceUrls(prev => ({ ...prev, main: url }))} uploadLabel={copy.chooseImage || '选择图片'} removeLabel={copy.removeImage || '移除'} linkLabel={copy.linkReference} placeholder={copy.referenceUrlPlaceholder} />
                {canUploadReferences && referenceUrls.main?.trim() ? <div className="inline-link-action"><button className="btn btn-secondary" disabled={isLoading} onClick={() => handleLink('main')}>{copy.linkReference}</button></div> : null}
                <div className="grid-2">{uploadTypes.map(type => (
                  <div className="form-grid" key={type.value}>
                    <ImageReferenceInput label={type.label} hint={!project ? copy.saveFirst : canUploadReferences ? copy.referenceUrlPlaceholder : copy.generateCopyFirst} disabled={!canUploadReferences || isLoading} file={referenceFiles[type.value]} url={referenceUrls[type.value] || ''} previewUrl={referencePreviews[type.value]} onFile={file => setReferenceFile(type.value, file)} onClearFile={() => clearReferenceFile(type.value)} onUpload={file => handleUpload(type.value, file)} onUrl={url => setReferenceUrls(prev => ({ ...prev, [type.value]: url }))} uploadLabel={copy.chooseImage || '选择图片'} removeLabel={copy.removeImage || '移除'} linkLabel={copy.linkReference} placeholder={copy.referenceUrlPlaceholder} />
                    {canUploadReferences && referenceUrls[type.value]?.trim() ? <div className="inline-link-action"><button className="btn btn-secondary" disabled={isLoading} onClick={() => handleLink(type.value)}>{copy.linkReference}</button></div> : null}
                  </div>
                ))}</div>
                {productImages.length ? <div className="result-box"><strong>{copy.uploadedImages}</strong><p>{productImages.map(image => imageTypeLabel(image.image_type || '')).join(' / ')}</p></div> : null}
                {project ? <div className="result-box"><strong>{copy.currentProject}</strong><p>{project.product_name} | {project.country} | {copy[projectStatusKey(project.status)] || project.status}</p><p>{copy.imageTasks}: {project.total_images || tasks.length}/7</p><BillingOnly><p>{copy.estimatedCost}: Listing {project.credits_cost || 0}{linkedAplusId ? ` + A+ 6 = ${(project.credits_cost || 0) + 6}` : ''} {copy.creditsUnit}</p></BillingOnly>{includeAplus || linkedAplusId ? <p>{copy.aplusLinked}: {linkedAplusName ? `${copy.created} ${linkedAplusName}` : `${aplusModuleLabel} / ${aplusSize}`}</p> : null}{linkedAplusId ? <div className="mt-16"><Link className="btn btn-primary" href={`/projects/${linkedAplusId}#tasks`}>{copy.openAplusResult}</Link><Link className="btn btn-secondary" href={`/commerce?tab=aplus&projectId=${linkedAplusId}`}>{copy.continueAplusEdit}</Link></div> : null}</div> : null}
              </div>
            </section>
            <section className="panel listing-flow-card listing-copy-card">
              <div className="panel-head"><div><h2>{copy.aiReview}</h2><p>{copy.aiReviewDesc}</p></div></div>
              <div className="panel-body">
                {copyTitle ? <div className="copy-preview"><h3>{copyTitle}</h3><ul>{copyBullets.map(item => <li key={item}>{item}</li>)}</ul>{warnings.length ? <div className="notice notice-error">{warnings.map(item => <div key={item}>{item}</div>)}</div> : null}</div> : <p className="muted">{copy.copyEmpty}</p>}
              </div>
              <div className="footer-actions"><span className="muted">{copy.stepOptimize}</span><button className="btn btn-secondary" disabled={!project || isLoading} onClick={handleGenerateCopy}>{copy.generateCopy}</button></div>
            </section>
          </div>
          <div className="task-flow-actions task-flow-actions-standalone">
            <button className="btn btn-secondary" type="button" onClick={() => setWorkflowStep(0)}><ArrowLeft size={16} />{copy.prevStep}</button>
            <button className="btn btn-primary" type="button" disabled={!project || !canCreateImages || !mainImage || isLoading} onClick={() => setWorkflowStep(2)}>{copy.nextStep}<ArrowRight size={16} /></button>
          </div>
        </>
      ) : null}

      {workflowStep === 2 ? (
        <section className="panel task-flow-panel listing-flow-card listing-plan-card">
          <div className="panel-head"><div><h2>{copy.planTitle}</h2><p>{copy.planDesc}</p></div></div>
          <div className="panel-body">
            <div className="confirm-summary-grid">
              <div className="result-box"><strong>{copy.previewProduct}</strong><p>{brand || '-'} / {productName || '-'}</p><p>{country} · {language}</p></div>
              <div className="result-box"><strong>{copy.previewReferences}</strong><p>{productImages.length} / 5</p><p>{productImages.map(image => imageTypeLabel(image.image_type || '')).join(' / ') || '-'}</p></div>
              <BillingOnly><div className="result-box"><strong>{hasExistingTasks ? copy.submittedCost : copy.estimatedCost}</strong>{hasExistingTasks ? <p>Listing {submittedListingCost}{linkedAplusId ? ` + A+ 6 = ${submittedTotalCost}` : ''} {copy.creditsUnit}</p> : <p>{estimatedGenerationCost} {copy.creditsUnit}</p>}<p>{copy.currentBalance}: {credits !== null ? credits : '--'} {copy.creditsUnit}</p></div></BillingOnly>
            </div>
          </div>
          {hasInsufficientCredits ? <div className="notice notice-error"><span>{copy.insufficientCredits}</span> <BillingOnly><a href="/credits">{copy.rechargeRedeem}</a></BillingOnly></div> : null}
          {isClothingListing && !mainImage ? <div className="notice notice-error">{copy.clothingReferenceRequired}</div> : null}
          <div className="task-flow-actions">
            <button className="btn btn-secondary" type="button" onClick={() => setWorkflowStep(1)}><ArrowLeft size={16} />{copy.prevStep}</button>
            {hasExistingTasks && !linkedAplusId
              ? <button className="btn btn-primary" disabled={!project || isLoading || (credits !== null && credits < 6)} onClick={requestAplusRecovery}>{copy.addAplusOnly}</button>
              : !hasExistingTasks
                ? <button className="btn btn-primary" disabled={!project || !canCreateImages || !mainImage || isLoading || hasInsufficientCredits} onClick={requestEnqueueImages}>{includeAplus ? copy.oneClickAplus : copy.oneClickImages}</button>
                : null}
          </div>
        </section>
      ) : null}

      <section className="panel mt-24 listing-flow-card listing-result-card">
        <div className="panel-head"><div><h2>{copy.stepResult}</h2><p>{copy.resultDesc}</p></div>{project ? <button className="btn btn-secondary" disabled={isLoading} onClick={() => loadProject(project.id)}>{copy.refreshTasks}</button> : null}</div>
        <div className="panel-body grid-3">
          {tasks.length > 0 ? sortedTasks(tasks).map(task => (
            <article className="panel feature-card" key={task.id}>
              <h3>{task.image_name}</h3>
              <p><span className={statusClass(task.status)}>{copy[taskStatusKey(task.status)] || task.status}</span></p>
              {task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} style={{ width: '100%', borderRadius: 12 }} /> : null}
              {task.status === 'failed' ? <div className="notice notice-error">{copy.failedRefund.replace('{credits}', String(task.credits_cost))}{task.safe_error ? <div className="mt-16">{task.safe_error}</div> : null}</div> : null}
              <BillingOnly><div className="muted">{task.credits_cost} {copy.creditsUnit}</div></BillingOnly>
              <div className="generation-card-actions">
                <button className="btn btn-secondary" type="button" onClick={() => setMessage(task.prompt_snapshot)}>{copy.viewBrief}</button>
                {task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">{copy.downloadView}</a> : null}
                {task.result_url ? <button className="btn btn-secondary" type="button" onClick={() => copyText(assetUrl(task.result_url), copy.copyImageUrlDone)}>{copy.copyImageUrl}</button> : null}
                <button className="btn btn-secondary" type="button" onClick={() => copyText(task.prompt_snapshot, copy.copyPromptDone)}>{copy.copyPrompt}</button>
                {task.status !== 'queued' && task.status !== 'running' ? <button className="btn btn-primary" disabled={isLoading} onClick={() => openRetry(task)}>{copy.retryDeduct.replace('{credits}', String(task.credits_cost))}</button> : null}
              </div>
            </article>
          )) : <p className="muted">{copy.noTasks}</p>}
          {linkedAplusId ? <article className="panel feature-card">
            <h3>{linkedAplusName || `${productName} A+`}</h3>
            <p><span className="status status-warn">{copy.created}</span></p>
            <BillingOnly><div className="muted">A+ · 6 {copy.creditsUnit}</div></BillingOnly>
            <div className="generation-card-actions">
              <Link className="btn btn-primary" href={`/projects/${linkedAplusId}#tasks`}>{copy.openAplusResult}</Link>
              <Link className="btn btn-secondary" href={`/commerce?tab=aplus&projectId=${linkedAplusId}`}>{copy.continueAplusEdit}</Link>
            </div>
          </article> : null}
        </div>
      </section>
      {confirmOpen ? (
        <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={copy.confirmGenerate}>
          <div className="modal-panel generation-confirm-panel">
            <h2>{copy.confirmGenerate}</h2>
            <p>{hasExistingTasks ? copy.confirmAplusOnlyDesc : copy.confirmGenerateDesc}</p>
            <div className="confirm-summary-grid">
              <div className="result-box"><strong>{copy.previewProduct}</strong><p>{brand || '-'} / {productName || '-'}</p><p>{country} · {language}</p></div>
              <div className="result-box"><strong>{copy.previewReferences}</strong><p>{mainImage ? copy.mainImage : copy.saveFirst}</p><p>{productImages.map(image => imageTypeLabel(image.image_type || '')).join(' / ') || '-'}</p></div>
              <BillingOnly><div className="result-box"><strong>{copy.estimatedCost}</strong><p>{estimatedGenerationCost} {copy.creditsUnit}</p><p>{copy.currentBalance}: {credits !== null ? credits : '--'} {copy.creditsUnit}</p></div></BillingOnly>
            </div>
            {includeAplus ? <div className="notice">{copy.aplusLinked}: {aplusModuleLabel} / {aplusSize}</div> : null}
            <div className="modal-actions">
              <button className="btn" type="button" disabled={isLoading} onClick={() => setConfirmOpen(false)}>{copy.cancel}</button>
              <button className="btn btn-primary" type="button" disabled={isLoading} onClick={handleEnqueueImages}>{isLoading ? copy.submitting : copy.confirmSubmit}</button>
            </div>
          </div>
        </div>
      ) : null}
      {retryingTask ? (
        <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={copy.retryWithReference}>
          <div className="modal-panel generation-confirm-panel">
            <h2>{copy.retryWithReference}</h2>
            <p>{copy.retryWithReferenceDesc}</p>
            <ImageReferenceInput
              label={copy.retryReferenceImage}
              hint={copy.retryReferenceImageHint}
              disabled={isLoading}
              file={retryReferenceFile}
              url={retryReferenceUrl}
              previewUrl={retryReferencePreview || assetUrl(retryReferenceUrl)}
              onFile={selectRetryReferenceFile}
              onClearFile={clearRetryReferenceFile}
              onUrl={setRetryReferenceUrl}
              uploadLabel={copy.chooseImage || '选择图片'}
              removeLabel={copy.removeImage || '移除'}
              linkLabel={copy.referenceUrlLabel}
              placeholder={copy.referenceUrlPlaceholder}
            />
            <label className="field mt-16"><span>{copy.referenceTextLabel}</span><textarea className="textarea" value={retryReferenceText} onChange={event => setRetryReferenceText(event.target.value)} placeholder={copy.referenceTextPlaceholder} /></label>
            <div className="modal-actions">
              <button className="btn" type="button" onClick={closeRetry}>{copy.cancel}</button>
              <button className="btn btn-primary" type="button" disabled={isLoading} onClick={() => handleRetryTask(retryingTask.id, { referenceUrl: retryReferenceUrl.trim(), referenceText: retryReferenceText.trim(), reference: retryReferenceFile })}>{copy.confirmRetryGenerate}</button>
            </div>
          </div>
        </div>
      ) : null}
    </UserAuthGate>
  )
  return embedded ? content : <Shell>{content}</Shell>
}
