'use client'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { ArrowLeft, ArrowRight } from 'lucide-react'
import { Shell } from '@/components/Shell'
import { currentLanguage, useI18n } from '@/components/i18n'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { TaskFlowSteps } from '@/components/TaskFlowSteps'
import { UserAuthGate } from '@/components/UserAuthGate'
import { assetUrl, createAplusImage, getOptionalBillingSummary, getProject, ImageTaskView, isAiDisabledError, ProjectSummary, requireLogin, retryProjectImage } from '@/lib/api'
import { APLUS_OUTPUT_SIZE_OPTIONS } from '@/lib/imageSizes'
import { getTemplateById, templateIntentMeta } from '@/lib/templateCatalog'

function statusKey(status: string) {
  if (status === 'queued') return 'queued'
  if (status === 'running') return 'generating'
  if (status === 'success') return 'success'
  if (status === 'failed') return 'failed'
  return status
}

const APLUS_MODULE_OPTIONS = [
  { value: 'brand-story-banner', labelKey: 'aplusModuleBrandStory' },
  { value: 'key-feature-module', labelKey: 'aplusModuleFeatures' },
  { value: 'lifestyle-scene-module', labelKey: 'aplusModuleLifestyle' },
  { value: 'specs-structure-module', labelKey: 'aplusModuleSpecs' }
]

function textValue(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function isErrorMessage(message: string, insufficientCredits: string) {
  return message === insufficientCredits || isAiDisabledError(message)
}

export function AplusWorkflow({ embedded = false }: { embedded?: boolean } = {}) {
  const { billingEnabled } = useRuntime()
  const [productName, setProductName] = useState('')
  const [brand, setBrand] = useState('')
  const [module, setModule] = useState(APLUS_MODULE_OPTIONS[0].value)
  const [size, setSize] = useState(APLUS_OUTPUT_SIZE_OPTIONS[0].value)
  const [prompt, setPrompt] = useState('')
  const [reference, setReference] = useState<File | null>(null)
  const [referenceUrl, setReferenceUrl] = useState('')
  const [referencePreview, setReferencePreview] = useState('')
  const [credits, setCredits] = useState<number | null>(null)
  const [project, setProject] = useState<ProjectSummary | null>(null)
  const [tasks, setTasks] = useState<ImageTaskView[]>([])
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [workflowStep, setWorkflowStep] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const submittingRef = useRef(false)
  const t = useI18n()

  useEffect(() => {
    if (requireLogin()) refreshCredits()
  }, [])

  useEffect(() => {
    const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
    if (template?.routeType === 'aplus' && template.imagePreset) {
      const preset = template.imagePreset
      if (preset.aplusModule && APLUS_MODULE_OPTIONS.some(item => item.value === preset.aplusModule)) setModule(preset.aplusModule)
      if (preset.aplusSize && APLUS_OUTPUT_SIZE_OPTIONS.some(item => item.value === preset.aplusSize)) setSize(preset.aplusSize)
      if (preset.prompt) setPrompt(preset.prompt)
      setMessage(`已套用模板：${template.title}。请补充商品名、品牌和参考图。`)
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
        setBrand(detail.project.brand || '')
        const originalInfo = detail.original_info || {}
        const confirmedFacts = detail.confirmed_facts || {}
        setPrompt(textValue(confirmedFacts.prompt) || textValue(originalInfo.description))
        setSize(textValue(confirmedFacts.size) || textValue(originalInfo.dimensions) || APLUS_OUTPUT_SIZE_OPTIONS[0].value)
        const referenceImage = detail.product_images.find(image => (image.image_type || '').includes('reference'))
         if (referenceImage) setReferencePreview(assetUrl(referenceImage.url))
         const failedTask = detail.image_tasks.find(task => task.status === 'failed')
         if (failedTask) setMessage(failedTask.safe_error || detail.project.latest_failure_reason || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
         setWorkflowStep(2)
      })
      .catch(error => setMessage(error instanceof Error ? error.message : t('failed')))
      .finally(() => setLoading(false))
  }, [project?.id, t])

  useEffect(() => {
    if (!project || !tasks.some(task => task.status === 'queued' || task.status === 'running')) return
    const timer = window.setInterval(refresh, 8000)
    return () => window.clearInterval(timer)
  }, [project?.id, tasks])

  function selectReference(file: File) {
    setReference(file)
    if (referencePreview.startsWith('blob:')) URL.revokeObjectURL(referencePreview)
    setReferencePreview(URL.createObjectURL(file))
  }

  async function refreshCredits() {
    getOptionalBillingSummary().then(data => setCredits(data?.credits ?? null)).catch(() => setCredits(null))
  }

  function requestSubmit() {
    if (!requireLogin() || !prompt.trim()) return
    if (credits !== null && credits < 6) {
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
      const selectedModule = APLUS_MODULE_OPTIONS.find(item => item.value === module) || APLUS_MODULE_OPTIONS[0]
      const moduleLabel = t(selectedModule.labelKey)
      const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
      const detail = await createAplusImage({ productName: productName || moduleLabel, brand, prompt, size, language: currentLanguage(), module: moduleLabel, reference, referenceUrl: referenceUrl.trim(), ...(template ? templateIntentMeta(template) : {}) })
      setProject(detail.project)
      setTasks(detail.image_tasks)
      window.history.replaceState(null, '', `/commerce?tab=aplus&projectId=${detail.project.id}`)
      setConfirmOpen(false)
      setMessage(t('generateAplus') + ' OK')
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

  const flowLanguage = currentLanguage()
  const flowText = flowLanguage === 'en-US'
    ? { product: 'Product & reference', design: 'Module & brief', confirm: 'Review & submit', previous: 'Previous', nextDesign: 'Next: module settings', nextConfirm: 'Next: review' }
    : flowLanguage === 'ja-JP'
      ? { product: '商品・参考画像', design: 'モジュール・指示', confirm: '確認・送信', previous: '前へ', nextDesign: '次へ：モジュール設定', nextConfirm: '次へ：確認' }
      : { product: '商品与参考图', design: '模块与画面要求', confirm: '确认并提交', previous: '上一步', nextDesign: '下一步：模块设置', nextConfirm: '下一步：提交确认' }
  const aplusWorkflowSteps = [flowText.product, flowText.design, flowText.confirm]

  const content = (
    <UserAuthGate title={t('loginAplus')} description={t(billingEnabled ? 'loginAplusDesc' : 'generationConfirmPersonal')}>
        <div className="page-head"><div><h1>{t('aplusTitle')}</h1><p>{t('aplusDesc')}</p></div>{project ? <div className="page-actions"><Link className="btn btn-primary" href={`/projects/${project.id}#tasks`}>{t('openProjectDetail')}</Link><button className="btn btn-secondary" onClick={refresh}>{t('refreshStatus')}</button></div> : null}</div>
        {message ? <div className={isErrorMessage(message, t('insufficientCreditsGuide')) ? 'notice notice-error' : 'notice'}>{message} {message === t('insufficientCreditsGuide') ? <BillingOnly><a href="/credits">{t('rechargeRedeem')}</a></BillingOnly> : null}</div> : null}
        <TaskFlowSteps steps={aplusWorkflowSteps} currentStep={workflowStep} ariaLabel={t('aplusDesc')} onStepChange={setWorkflowStep} />
        <section className="panel task-flow-panel">
          <div className="panel-head">
            <div>
              <h2>{aplusWorkflowSteps[workflowStep]}</h2>
              <p>{workflowStep === 0 ? t('referenceImageHint') : workflowStep === 1 ? t('aplusDesc') : t(billingEnabled ? 'confirmAplusDesc' : 'generationConfirmPersonal')}</p>
            </div>
          </div>
          {workflowStep === 0 ? (
            <div className="panel-body form-grid">
              <div className="grid-2">
                <label className="field"><span>{t('productName')}</span><input className="input" value={productName} onChange={event => setProductName(event.target.value)} placeholder="Portable coffee maker" /></label>
                <label className="field"><span>{t('brand')}</span><input className="input" value={brand} onChange={event => setBrand(event.target.value)} placeholder={t('optional')} /></label>
              </div>
              <ImageReferenceInput label={t('referenceImage')} hint={t('referenceImageHint')} file={reference} url={referenceUrl} previewUrl={referencePreview || referenceUrl} onFile={selectReference} onClearFile={() => { setReference(null); setReferencePreview('') }} onUrl={setReferenceUrl} uploadLabel={t('chooseImage')} removeLabel={t('removeImage')} linkLabel={t('referenceUrlLabel')} placeholder={t('referenceUrlPlaceholder')} />
            </div>
          ) : null}
          {workflowStep === 1 ? (
            <div className="panel-body form-grid">
              <div className="grid-2">
                <label className="field"><span>{t('moduleType')}</span><select className="select" value={module} onChange={event => setModule(event.target.value)}>{APLUS_MODULE_OPTIONS.map(option => <option value={option.value} key={option.value}>{t(option.labelKey)}</option>)}</select></label>
                <label className="field"><span>{t('size')}</span><select className="select" value={size} onChange={event => setSize(event.target.value)}>{APLUS_OUTPUT_SIZE_OPTIONS.map(option => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>
              </div>
              <label className="field"><span>{t('aplusIdea')}</span><textarea className="textarea" value={prompt} onChange={event => setPrompt(event.target.value)} placeholder="Describe subject, scene, selling points, style, and text-safe space." /></label>
            </div>
          ) : null}
          {workflowStep === 2 ? (
            <div className="panel-body">
              <div className="confirm-summary-grid">
                <div className="result-box"><strong>{t('productName')}</strong><p>{productName || t(APLUS_MODULE_OPTIONS.find(item => item.value === module)?.labelKey || 'aplusModuleBrandStory')}</p><p>{brand || t('optional')}</p></div>
                <div className="result-box"><strong>{t('moduleType')}</strong><p>{t(APLUS_MODULE_OPTIONS.find(item => item.value === module)?.labelKey || 'aplusModuleBrandStory')}</p><p>{size}</p></div>
                <BillingOnly><div className="result-box"><strong>{t('estimatedCost')}</strong><p>6 {t('creditsLabel')}</p><p>{t('currentCredits')}: {credits ?? '--'}</p></div></BillingOnly>
              </div>
              <div className="task-flow-review-brief"><strong>{t('aplusIdea')}</strong><p>{prompt || '-'}</p></div>
            </div>
          ) : null}
          <div className="task-flow-actions">
            <div>{workflowStep > 0 ? <button className="btn btn-secondary" type="button" onClick={() => setWorkflowStep(step => Math.max(0, step - 1))}><ArrowLeft size={16} />{flowText.previous}</button> : null}</div>
            {workflowStep === 0 ? <button className="btn btn-primary" type="button" onClick={() => setWorkflowStep(1)}>{flowText.nextDesign}<ArrowRight size={16} /></button> : null}
            {workflowStep === 1 ? <button className="btn btn-primary" type="button" disabled={!prompt.trim()} onClick={() => setWorkflowStep(2)}>{flowText.nextConfirm}<ArrowRight size={16} /></button> : null}
            {workflowStep === 2 ? <button className="btn btn-primary" disabled={loading || !prompt.trim()} onClick={requestSubmit}>{loading ? t('submiting') : t('generateAplus')}</button> : null}
          </div>
        </section>
        <section className="panel mt-24">
          <div className="panel-head"><div><h2>{t('results')}</h2><p>{t('refreshHint')}</p></div></div>
          <div className="panel-body grid-3">{tasks.length ? tasks.map(task => <article className="panel feature-card" key={task.id}><h3>{task.image_name}</h3><p><span className={task.status === 'failed' ? 'status status-bad' : task.status === 'success' ? 'status status-ok' : 'status status-warn'}>{t(statusKey(task.status))}</span></p>{task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} style={{ width: '100%', borderRadius: 10 }} /> : null}{task.status === 'failed' ? <div className="notice notice-error">{task.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal')}</div> : null}<BillingOnly><div className="muted">{task.credits_cost} {t('creditsLabel')}</div></BillingOnly><div className="generation-card-actions">{project ? <Link className="btn btn-primary" href={`/projects/${project.id}#task-${task.id}`}>{t('openProjectDetail')}</Link> : null}{task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">{t('viewDownload')}</a> : null}{task.status === 'failed' ? <button className="btn btn-primary" disabled={loading} onClick={() => retry(task.id)}>{t('retryGenerate')}</button> : null}</div></article>) : <p className="muted">{t('noTasks')}</p>}</div>
        </section>
        {confirmOpen ? (
          <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={t('confirmGenerate')}>
            <div className="modal-panel generation-confirm-panel">
              <h2>{t('confirmGenerate')}</h2>
              <p>{t(billingEnabled ? 'confirmAplusDesc' : 'generationConfirmPersonal')}</p>
              <div className="confirm-summary-grid">
                <div className="result-box"><strong>{t('productName')}</strong><p>{productName || t(APLUS_MODULE_OPTIONS.find(item => item.value === module)?.labelKey || 'aplusModuleBrandStory')}</p><p>{brand || t('optional')}</p></div>
                <div className="result-box"><strong>{t('moduleType')}</strong><p>{t(APLUS_MODULE_OPTIONS.find(item => item.value === module)?.labelKey || 'aplusModuleBrandStory')}</p><p>{size}</p></div>
                <BillingOnly><div className="result-box"><strong>{t('estimatedCost')}</strong><p>6 {t('creditsLabel')}</p><p>{credits ?? '--'} {t('creditsLabel')}</p></div></BillingOnly>
              </div>
              {(referencePreview || referenceUrl) ? <img className="confirm-reference-preview" src={referencePreview || referenceUrl} alt={t('referenceImage')} /> : null}
              <div className="confirm-brief-list confirm-prompt-card">
                <strong>{t('aplusIdea')}</strong>
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
