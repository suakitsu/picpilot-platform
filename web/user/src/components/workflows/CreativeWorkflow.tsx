'use client'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { Shell } from '@/components/Shell'
import { currentLanguage, useI18n } from '@/components/i18n'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { UserAuthGate } from '@/components/UserAuthGate'
import { compressImageForUpload, MAX_UPLOAD_IMAGE_BYTES, TARGET_UPLOAD_IMAGE_BYTES } from '@/lib/imageCompression'
import { assetUrl, createCreativeImage, getOptionalBillingSummary, getProject, ImageTaskView, isAiDisabledError, ProjectSummary, requireLogin, retryProjectImage } from '@/lib/api'
import { IMAGE_SIZE_OPTIONS, imageCreditsForSize, imageSizeLabel } from '@/lib/imageSizes'
import { getTemplateById, templateIntentMeta } from '@/lib/templateCatalog'

const CREATIVE_STYLE_AUTO_VALUE = 'auto'
const CREATIVE_STYLE_CUSTOM_VALUE = 'custom'

const CREATIVE_STYLE_OPTIONS = [
  { value: CREATIVE_STYLE_AUTO_VALUE, labelKey: 'creativeStyleAuto' },
  { value: 'commercial-photography', labelKey: 'creativeStyleCommercial' },
  { value: 'ecommerce-poster', labelKey: 'creativeStylePoster' },
  { value: 'lifestyle', labelKey: 'creativeStyleLifestyle' },
  { value: 'minimal-white-background', labelKey: 'creativeStyleWhiteBg' },
  { value: '3d-render', labelKey: 'creativeStyle3d' },
  { value: CREATIVE_STYLE_CUSTOM_VALUE, labelKey: 'creativeStyleCustom' }
]

const CREATIVE_STYLE_PRESETS = [
  'creativeStyleQuickJapanese',
  'creativeStyleQuickKorean',
  'creativeStyleQuickLuxury',
  'creativeStyleQuickTransparent',
  'creativeStyleQuickLolita',
  'creativeStyleQuickFilm'
]

const MAX_REFERENCE_FILES = 4

function isErrorMessage(message: string, insufficientCredits: string) {
  return message === insufficientCredits || isAiDisabledError(message)
}

function referenceUploadMessage(key: 'compressing' | 'compressed' | 'tooLarge') {
  const lang = currentLanguage()
  if (lang === 'en-US') {
    if (key === 'compressing') return 'This image is large. Compressing it before upload...'
    if (key === 'compressed') return 'Image was compressed and uploaded'
    return 'The image is still above the upload limit after compression. Please use a smaller image.'
  }
  if (lang === 'ja-JP') {
    if (key === 'compressing') return '画像が大きいため、自動圧縮してアップロードしています...'
    if (key === 'compressed') return '画像を自動圧縮してアップロードしました'
    return '圧縮後もアップロード上限を超えています。より小さい画像を使用してください。'
  }
  if (key === 'compressing') return '图片较大，正在自动压缩后上传...'
  if (key === 'compressed') return '图片已自动压缩后上传'
  return '图片压缩后仍超过上传限制，请换一张更小的图片'
}

export function CreativeWorkflow({ embedded = false }: { embedded?: boolean } = {}) {
  const { billingEnabled } = useRuntime()
  const [prompt, setPrompt] = useState('')
  const [size, setSize] = useState('1024x1024')
  const [style, setStyle] = useState(CREATIVE_STYLE_OPTIONS[0].value)
  const [customStyle, setCustomStyle] = useState('')
  const [references, setReferences] = useState<File[]>([])
  const [referencePreviews, setReferencePreviews] = useState<string[]>([])
  const [referenceUrl, setReferenceUrl] = useState('')
  const [credits, setCredits] = useState<number | null>(null)
  const [project, setProject] = useState<ProjectSummary | null>(null)
  const [tasks, setTasks] = useState<ImageTaskView[]>([])
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const submittingRef = useRef(false)
  const previewUrlsRef = useRef<string[]>([])
  const t = useI18n()
  const estimatedCost = imageCreditsForSize(size)
  const resolvedStyle = resolveCreativeStyle()

  function resolveCreativeStyle() {
    if (style === CREATIVE_STYLE_CUSTOM_VALUE) {
      return customStyle.trim() || t('creativeStyleAuto')
    }
    const selectedStyle = CREATIVE_STYLE_OPTIONS.find(item => item.value === style) || CREATIVE_STYLE_OPTIONS[0]
    return t(selectedStyle.labelKey)
  }

  function applyPreset(labelKey: string) {
    setStyle(CREATIVE_STYLE_CUSTOM_VALUE)
    setCustomStyle(t(labelKey))
  }

  function createReferencePreview(file: File) {
    const url = URL.createObjectURL(file)
    previewUrlsRef.current.push(url)
    return url
  }

  function revokeReferencePreview(url: string) {
    if (!url.startsWith('blob:')) return
    URL.revokeObjectURL(url)
    previewUrlsRef.current = previewUrlsRef.current.filter(item => item !== url)
  }

  function replaceReferenceFiles(nextFiles: File[]) {
    previewUrlsRef.current.forEach(url => URL.revokeObjectURL(url))
    previewUrlsRef.current = []
    const limited = nextFiles.slice(0, MAX_REFERENCE_FILES)
    const previews = limited.map(file => createReferencePreview(file))
    setReferences(limited)
    setReferencePreviews(previews)
  }

  function appendReferences(files: File[]) {
    const next = [...references, ...files]
    replaceReferenceFiles(next)
  }

  async function handleReferenceFiles(files: File[]) {
    if (!files.length) return
    const filtered = files.slice(0, MAX_REFERENCE_FILES - references.length)
    if (!filtered.length) return
    const nextFiles: File[] = []
    let compressedCount = 0
    let droppedTooLarge = false
    for (const file of filtered) {
      if (file.size > TARGET_UPLOAD_IMAGE_BYTES) {
        setMessage(referenceUploadMessage('compressing'))
      }
      try {
        const compression = await compressImageForUpload(file)
        const compressedFile = compression.file.size > MAX_UPLOAD_IMAGE_BYTES ? null : compression.file
        if (!compressedFile) {
          droppedTooLarge = true
          continue
        }
        if (compression.compressed) compressedCount += 1
        nextFiles.push(compressedFile)
      } catch {
        if (file.size > MAX_UPLOAD_IMAGE_BYTES) {
          droppedTooLarge = true
          continue
        }
        nextFiles.push(file)
      }
    }
    appendReferences(nextFiles)
    if (droppedTooLarge) {
      setMessage(referenceUploadMessage('tooLarge'))
    } else if (compressedCount > 0) {
      setMessage(referenceUploadMessage('compressed'))
    }
  }

  function removeReference(index: number) {
    const nextFiles = references.filter((_, itemIndex) => itemIndex !== index)
    replaceReferenceFiles(nextFiles)
  }

  async function refreshCredits() {
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => setCredits(null))
  }

  useEffect(() => {
    if (requireLogin()) refreshCredits()
  }, [])

  useEffect(() => {
    const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
    if (template?.routeType === 'creative' && template.imagePreset) {
      const preset = template.imagePreset
      setStyle(CREATIVE_STYLE_CUSTOM_VALUE)
      setCustomStyle(preset.style || template.subtitle)
      if (preset.size) setSize(preset.size)
      if (preset.prompt) setPrompt(preset.prompt)
      setMessage(`已套用模板：${template.title}。可以继续补充商品和画面要求。`)
    }
  }, [])

  useEffect(() => {
    return () => {
      previewUrlsRef.current.forEach(url => URL.revokeObjectURL(url))
      previewUrlsRef.current = []
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
        const confirmedFacts = detail.confirmed_facts || {}
        const originalInfo = detail.original_info || {}
        const restoredSize = typeof confirmedFacts.size === 'string'
          ? confirmedFacts.size
          : (typeof originalInfo.dimensions === 'string' ? originalInfo.dimensions : '')
        setSize(restoredSize || '1024x1024')
        replaceReferenceFiles([])
        const restoredPrompt = typeof confirmedFacts.prompt === 'string'
          ? confirmedFacts.prompt
          : (typeof originalInfo.description === 'string' ? originalInfo.description : '')
        const restoredModule = typeof confirmedFacts.module === 'string' ? confirmedFacts.module : ''
        setPrompt(restoredPrompt)
        if (restoredModule) {
          const matchedStyle = CREATIVE_STYLE_OPTIONS.find(option => t(option.labelKey) === restoredModule || option.value === restoredModule)
          if (matchedStyle) {
            setStyle(matchedStyle.value)
            if (matchedStyle.value !== CREATIVE_STYLE_CUSTOM_VALUE) setCustomStyle('')
          } else {
            setStyle(CREATIVE_STYLE_CUSTOM_VALUE)
            setCustomStyle(restoredModule)
          }
        }
        const linkedReference = detail.product_images.filter(image => (image.image_type || '').includes('reference_link')).map(image => image.url).filter(Boolean)
        setReferenceUrl(linkedReference[0] || '')
        const failedTask = detail.image_tasks.find(task => task.status === 'failed')
        if (failedTask) setMessage(failedTask.safe_error || detail.project.latest_failure_reason || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
      })
      .catch(error => setMessage(error instanceof Error ? error.message : t('failed')))
      .finally(() => setLoading(false))
  }, [project?.id, t])

  useEffect(() => {
    if (!project || !tasks.some(task => task.status === 'queued' || task.status === 'running')) return
    const timer = window.setInterval(refresh, 8000)
    return () => window.clearInterval(timer)
  }, [project?.id, tasks])

  function requestSubmit() {
    if (!requireLogin() || !prompt.trim()) return
    if (credits !== null && credits < estimatedCost) {
      setMessage(t('insufficientCreditsGuide'))
      return
    }
    setConfirmOpen(true)
  }

  async function submit() {
    if (!requireLogin() || !prompt.trim()) return
    if (submittingRef.current) return
    submittingRef.current = true
    setLoading(true)
    setMessage('')
    try {
      const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
      const detail = await createCreativeImage({ productName: t('creativeTitle'), brand: resolvedStyle, prompt, size, language: currentLanguage(), module: resolvedStyle, references, referenceUrl: referenceUrl.trim(), ...(template ? templateIntentMeta(template) : {}) })
      setProject(detail.project)
      setTasks(detail.image_tasks)
      window.history.replaceState(null, '', `/ai-create?tab=free&projectId=${detail.project.id}`)
      setConfirmOpen(false)
      setMessage(t('startGenerate') + ' OK')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setConfirmOpen(false)
      setMessage(error instanceof Error ? error.message : t('failed'))
    } finally {
      submittingRef.current = false
      setLoading(false)
      refreshCredits()
    }
  }

  async function refresh() {
    if (!project) return
    const detail = await getProject(project.id)
    const hadFailure = !tasks.some(task => task.status === 'failed') && detail.image_tasks.some(task => task.status === 'failed')
    setProject(detail.project)
    setTasks(detail.image_tasks)
    if (hadFailure) setMessage(detail.project.latest_failure_reason || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
  }

  async function retry(taskId: string) {
    if (!project) return
    setLoading(true)
    try {
      const updated = await retryProjectImage(project.id, taskId)
      setTasks(rows => rows.map(task => task.id === taskId ? updated : task))
      setProject(current => current ? { ...current, status: 'images_queued' } : current)
      setMessage(t('retryQueued'))
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('retryFailed'))
    } finally {
      setLoading(false)
      refreshCredits()
    }
  }

  const content = (
    <UserAuthGate title={t('loginCreative')} description={t(billingEnabled ? 'loginCreativeDesc' : 'generationConfirmPersonal')}>
        <div className="page-head"><div><h1>{t('creativeTitle')}</h1><p>{t('creativeDesc')}</p></div>{project ? <div className="page-actions"><Link className="btn btn-primary" href={`/projects/${project.id}#tasks`}>{t('openProjectDetail')}</Link><button className="btn btn-secondary" onClick={refresh}>{t('refreshStatus')}</button></div> : null}</div>
        {message ? <div className={isErrorMessage(message, t('insufficientCreditsGuide')) ? 'notice notice-error' : 'notice'}>{message} {message === t('insufficientCreditsGuide') ? <BillingOnly><a href="/credits">{t('rechargeRedeem')}</a></BillingOnly> : null}</div> : null}
        <section className="panel">
          <div className="panel-head"><div><h2>{t('creativeSettings')}</h2><BillingOnly><p>{t('creativeCostHint')}</p></BillingOnly></div></div>
          <div className="panel-body form-grid">
            <div className="grid-2 creative-controls-grid">
              <div className="field creative-style-picker"><span>{t('style')}</span><select className="select" value={style} onChange={event => setStyle(event.target.value)}>{CREATIVE_STYLE_OPTIONS.map(option => <option value={option.value} key={option.value}>{t(option.labelKey)}</option>)}</select>
                <div className="creative-style-presets" aria-label={t('creativeStyleQuick')}>
                  {CREATIVE_STYLE_PRESETS.map(labelKey => <button className={customStyle === t(labelKey) ? 'creative-style-chip creative-style-chip-active' : 'creative-style-chip'} type="button" key={labelKey} onClick={() => applyPreset(labelKey)}>{t(labelKey)}</button>)}
                </div>
              </div>
              <label className="field creative-size-field"><span>{t('size')}</span><select className="select" value={size} onChange={event => setSize(event.target.value)}>{IMAGE_SIZE_OPTIONS.map(option => <option value={option.value} key={option.value}>{imageSizeLabel(option.value)}</option>)}</select></label>
            </div>
            {style === CREATIVE_STYLE_CUSTOM_VALUE ? <label className="field"><span>{t('creativeCustomStyle')}</span><input className="input" value={customStyle} maxLength={80} onChange={event => setCustomStyle(event.target.value)} placeholder={t('creativeCustomStylePlaceholder')} /></label> : null}
            <div className="creative-reference-input">
              <ImageReferenceInput
                label={t('referenceImage')}
                hint={t('referenceImageHint')}
                file={references[0] || null}
                files={references}
                previewUrls={referencePreviews}
                url={referenceUrl}
                previewUrl={referencePreviews[0] || referenceUrl}
                multiple
                onFile={file => handleReferenceFiles([file])}
                onFiles={handleReferenceFiles}
                onClearFile={() => replaceReferenceFiles([])}
                onRemoveFile={removeReference}
                onUrl={setReferenceUrl}
                uploadLabel={t('chooseImage')}
                removeLabel={t('removeImage')}
                linkLabel={t('referenceUrlLabel')}
                placeholder={t('referenceUrlPlaceholder')}
              />
            </div>
            <label className="field"><span>{t('prompt')}</span><textarea className="textarea" value={prompt} onChange={event => setPrompt(event.target.value)} placeholder="Describe the image you want. The AI prompt optimizer will refine subject, scene, composition, and constraints." /></label>
          </div>
          <div className="footer-actions"><BillingOnly><span className="muted">{t('estimatedCost')}: {estimatedCost} {t('creditsLabel')} · {t('currentCredits')}: {credits ?? '--'}</span></BillingOnly><button className="btn btn-primary" disabled={loading || !prompt.trim()} onClick={requestSubmit}>{loading ? t('submiting') : t('startGenerate')}</button></div>
        </section>
        <section className="panel mt-24">
          <div className="panel-head"><div><h2>{t('results')}</h2><p>{t('refreshHint')}</p></div></div>
          <div className="panel-body grid-3">{tasks.length ? tasks.map(task => <article className="panel feature-card" key={task.id}><h3>{task.image_name}</h3><p><span className={task.status === 'failed' ? 'status status-bad' : task.status === 'success' ? 'status status-ok' : 'status status-warn'}>{t(task.status === 'running' ? 'generating' : task.status)}</span></p>{task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} style={{ width: '100%', borderRadius: 10 }} /> : null}{task.status === 'failed' ? <div className="notice notice-error">{task.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal')}</div> : null}<BillingOnly><div className="muted">{task.credits_cost} {t('creditsLabel')}</div></BillingOnly><div className="generation-card-actions">{project ? <Link className="btn btn-primary" href={`/projects/${project.id}#task-${task.id}`}>{t('openProjectDetail')}</Link> : null}{task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">{t('viewDownload')}</a> : null}{task.status === 'failed' ? <button className="btn btn-primary" disabled={loading} onClick={() => retry(task.id)}>{t('retryGenerate')}</button> : null}</div></article>) : <p className="muted">{t('noTasks')}</p>}</div>
        </section>
        {confirmOpen ? (
          <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={t('confirmGenerate')}>
            <div className="modal-panel generation-confirm-panel">
              <h2>{t('confirmGenerate')}</h2>
              <p>{t(billingEnabled ? 'confirmCreativeDesc' : 'generationConfirmPersonal')}</p>
              <div className="confirm-summary-grid">
                <div className="result-box"><strong>{t('style')}</strong><p>{resolvedStyle}</p></div>
                <div className="result-box"><strong>{t('size')}</strong><p>{imageSizeLabel(size)}</p></div>
                <BillingOnly><div className="result-box"><strong>{t('estimatedCost')}</strong><p>{estimatedCost} {t('creditsLabel')}</p><p>{credits ?? '--'} {t('creditsLabel')}</p></div></BillingOnly>
              </div>
              {(referencePreviews.length || referenceUrl) ? (
                <div className="confirm-reference-grid">
                  {referencePreviews.map((item, index) => <img className="confirm-reference-preview" src={item} alt={`${t('referenceImage')} ${index + 1}`} key={`${item}-${index}`} />)}
                  {!referencePreviews.length && referenceUrl ? <img className="confirm-reference-preview" src={referenceUrl} alt={t('referenceImage')} /> : null}
                </div>
              ) : null}
              <div className="confirm-brief-list confirm-prompt-card">
                <strong>{t('prompt')}</strong>
                <span className="confirm-prompt-preview">{prompt}</span>
                <details className="confirm-prompt-details">
                  <summary>{t('viewFullPrompt')}</summary>
                  <pre>{prompt}</pre>
                </details>
              </div>
              <div className="modal-actions">
                <button className="btn" type="button" disabled={loading} onClick={() => setConfirmOpen(false)}>{t('cancel')}</button>
                <button className="btn btn-primary" type="button" disabled={loading} onClick={submit}>{loading ? t('submiting') : t('confirmSubmit')}</button>
              </div>
            </div>
          </div>
        ) : null}
    </UserAuthGate>
  )
  return embedded ? content : <Shell>{content}</Shell>
}
