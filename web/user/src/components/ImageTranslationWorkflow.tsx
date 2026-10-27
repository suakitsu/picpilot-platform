'use client'
import { useI18n } from '@/components/i18n'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import { ChangeEvent, DragEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'next/navigation'
import { ArrowLeft, ArrowRight, Download, FileImage, Images, Languages, RefreshCw, Sparkles, Trash2, Upload, X } from 'lucide-react'
import { TaskFlowSteps } from '@/components/TaskFlowSteps'
import { UserAuthGate } from '@/components/UserAuthGate'
import {
  assetUrl,
  generateImageTranslation,
  getOptionalBillingSummary,
  getProject,
  ImageTaskView,
  ImageTranslationLanguage,
  listImageTranslationLanguages,
  ProductImageView,
  ProjectDetail,
  requireLogin,
  retryImageTranslationTask
} from '@/lib/api'
import { IMAGE_FILE_ACCEPT, imageUploadErrorMessage, imageUploadMessage, validateImageFile } from '@/lib/imageUpload'

const MAX_IMAGES = 10
const MAX_FILE_BYTES = 25 * 1024 * 1024
const MAX_TOTAL_BYTES = 28 * 1024 * 1024
const RESOLUTIONS = ['1K', '2K', '4K'] as const
const COSTS: Record<string, number> = { '1K': 6, '2K': 24, '4K': 30 }

interface TaskSnapshot {
  translationSourceImageId?: string
  sourceImageIndex?: number
  sourceFilename?: string
  targetLanguageName?: string
  resolution?: string
}

function parseSnapshot(task: ImageTaskView): TaskSnapshot {
  try {
    return JSON.parse(task.prompt_snapshot || '{}') as TaskSnapshot
  } catch {
    return {}
  }
}

function statusText(status: string) {
  if (status === 'queued') return '排队中'
  if (status === 'running') return '翻译中'
  if (status === 'success') return '已完成'
  if (status === 'failed') return '失败'
  return status
}

function statusClass(status: string) {
  if (status === 'success') return 'status status-ok'
  if (status === 'failed') return 'status status-bad'
  return 'status status-warn'
}

function safeDownloadName(value: string, index: number) {
  const base = (value || `image-${index + 1}`).replace(/\.[^.]+$/, '').replace(/[\\/:*?"<>|]+/g, '-').trim()
  return `${base || `image-${index + 1}`}-translated.png`
}

function sourceForTask(task: ImageTaskView, sources: ProductImageView[]) {
  const snapshot = parseSnapshot(task)
  return sources.find(source => source.id === snapshot.translationSourceImageId) || sources[snapshot.sourceImageIndex || 0]
}

export function ImageTranslationWorkflow() {
  const t = useI18n()
  const { billingEnabled } = useRuntime()
  const searchParams = useSearchParams()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const submittingRef = useRef(false)
  const initialProjectIdRef = useRef(searchParams.get('projectId'))
  const [languages, setLanguages] = useState<ImageTranslationLanguage[]>([])
  const [targetLanguage, setTargetLanguage] = useState('ms-MY')
  const [resolution, setResolution] = useState<(typeof RESOLUTIONS)[number]>('1K')
  const [files, setFiles] = useState<File[]>([])
  const [previews, setPreviews] = useState<string[]>([])
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [credits, setCredits] = useState<number | null>(null)
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [processingImages, setProcessingImages] = useState(false)
  const [workflowStep, setWorkflowStep] = useState(0)
  const [dragging, setDragging] = useState(false)
  const [downloading, setDownloading] = useState(false)

  const tasks = useMemo(
    () => [...(detail?.image_tasks || [])].sort((left, right) => (parseSnapshot(left).sourceImageIndex ?? 999) - (parseSnapshot(right).sourceImageIndex ?? 999)),
    [detail]
  )
  const sources = useMemo(
    () => (detail?.product_images || []).filter(image => image.image_type === 'image_translation_source'),
    [detail?.product_images]
  )
  const estimatedCost = files.length * COSTS[resolution]
  const running = tasks.filter(task => task.status === 'queued' || task.status === 'running').length
  const succeeded = tasks.filter(task => task.status === 'success').length
  const failed = tasks.filter(task => task.status === 'failed').length
  const completedTasks = tasks.filter(task => task.status === 'success' && task.result_url)

  useEffect(() => {
    if (requireLogin()) refreshCredits()
    listImageTranslationLanguages()
      .then(rows => {
        setLanguages(rows)
        setTargetLanguage(current => rows.length && !rows.some(item => item.code === current) ? rows[0].code : current)
      })
      .catch(error => setMessage(error instanceof Error ? error.message : '目标语言加载失败'))

    const projectId = initialProjectIdRef.current
    if (projectId) {
      getProject(projectId)
        .then(project => {
          if (project.original_info?.source_type !== 'image_translation') return
          setDetail(project)
          const code = String(project.original_info?.target_language || '')
          const tier = String(project.original_info?.resolution || '')
          if (code) setTargetLanguage(code)
          if (RESOLUTIONS.includes(tier as (typeof RESOLUTIONS)[number])) setResolution(tier as (typeof RESOLUTIONS)[number])
        })
        .catch(error => setMessage(error instanceof Error ? error.message : '翻译批次加载失败'))
    }
  }, [])

  useEffect(() => {
    const next = files.map(file => URL.createObjectURL(file))
    setPreviews(next)
    return () => next.forEach(url => URL.revokeObjectURL(url))
  }, [files])

  useEffect(() => {
    const projectId = detail?.project.id
    if (!projectId || running === 0) return
    const timer = window.setInterval(() => {
      getProject(projectId)
        .then(project => {
          setDetail(project)
          const taskFailed = project.image_tasks.find(task => task.status === 'failed')
          if (taskFailed) setMessage(taskFailed.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
        })
        .catch(error => setMessage(error instanceof Error ? error.message : '刷新失败'))
    }, 5000)
    return () => window.clearInterval(timer)
  }, [detail?.project.id, running])

  async function refreshCredits() {
    try {
      const summary = await getOptionalBillingSummary()
      setCredits(summary?.credits ?? null)
    } catch {
      setCredits(null)
    }
  }

  async function refreshProject(projectId = detail?.project.id) {
    if (!projectId) return
    try {
      const project = await getProject(projectId)
      setDetail(project)
      const taskFailed = project.image_tasks.find(task => task.status === 'failed')
      if (taskFailed) setMessage(taskFailed.safe_error || t(billingEnabled ? 'generationFailedNotice' : 'generationFailedPersonal'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '刷新失败')
    }
  }

  async function addFiles(incoming: File[]) {
    if (!incoming.length || processingImages) return
    setProcessingImages(true)
    const currentKeys = new Set(files.map(file => `${file.name}:${file.size}:${file.lastModified}`))
    const next = [...files]
    let totalBytes = next.reduce((sum, file) => sum + file.size, 0)
    let rejection = ''

    try {
      for (const file of incoming) {
        const key = `${file.name}:${file.size}:${file.lastModified}`
        if (currentKeys.has(key)) continue
        if (next.length >= MAX_IMAGES) {
          rejection = `一次最多选择 ${MAX_IMAGES} 张图片。`
          continue
        }
        if (totalBytes + file.size > MAX_TOTAL_BYTES) {
          rejection = '单张不能超过 25MB，全部图片合计不能超过 28MB。'
          continue
        }
        try {
          await validateImageFile(file, MAX_FILE_BYTES)
        } catch (error) {
          rejection = imageUploadErrorMessage(error)
          continue
        }
        currentKeys.add(key)
        next.push(file)
        totalBytes += file.size
      }
      setFiles(next)
      setMessage(rejection)
    } finally {
      setProcessingImages(false)
    }
  }

  async function onFileInput(event: ChangeEvent<HTMLInputElement>) {
    await addFiles(Array.from(event.target.files || []))
    event.target.value = ''
  }

  async function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    setDragging(false)
    await addFiles(Array.from(event.dataTransfer.files || []))
  }

  async function submit() {
    if (!requireLogin() || !files.length || loading || submittingRef.current) return
    if (credits !== null && credits < estimatedCost) {
      setMessage('积分不足，请先充值或兑换积分。')
      return
    }
    submittingRef.current = true
    setLoading(true)
    setMessage('')
    try {
      const response = await generateImageTranslation({ targetLanguage, resolution, images: files })
      setDetail(response.project)
      setFiles([])
      setWorkflowStep(0)
      window.history.replaceState(null, '', `/commerce?tab=translate&projectId=${response.project_id}`)
      setMessage(`已提交 ${response.created_tasks.length} 张图片，正在后台逐张翻译。`)
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '提交失败，请稍后重试。')
    } finally {
      submittingRef.current = false
      setLoading(false)
      refreshCredits()
    }
  }

  async function retry(task: ImageTaskView) {
    setLoading(true)
    try {
      const updated = await retryImageTranslationTask(task.id)
      setDetail(current => current ? {
        ...current,
        project: { ...current.project, status: 'images_queued' },
        image_tasks: current.image_tasks.map(item => item.id === task.id ? updated : item)
      } : current)
      setMessage('该图片已重新入队。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '重试失败')
    } finally {
      setLoading(false)
      refreshCredits()
    }
  }

  async function downloadTask(task: ImageTaskView, index: number) {
    if (!task.result_url) return
    const snapshot = parseSnapshot(task)
    try {
      const response = await fetch(assetUrl(task.result_url))
      if (!response.ok) throw new Error('download failed')
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = safeDownloadName(snapshot.sourceFilename || task.image_name, index)
      document.body.appendChild(link)
      link.click()
      link.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch {
      window.open(assetUrl(task.result_url), '_blank', 'noopener,noreferrer')
    }
  }

  async function downloadAll() {
    if (!completedTasks.length || downloading) return
    setDownloading(true)
    setMessage('正在准备下载...')
    for (let index = 0; index < completedTasks.length; index += 1) {
      await downloadTask(completedTasks[index], index)
      await new Promise(resolve => window.setTimeout(resolve, 180))
    }
    setMessage(`已开始下载 ${completedTasks.length} 张译图。`)
    setDownloading(false)
  }

  return (
    <UserAuthGate title="登录后使用图片翻译" description={t('generationConfirmPersonal')}>
      <div className="translation-page">
        {message ? <div className={message.includes('失败') || message.includes('不足') || message.includes('无法') || message.includes('损坏') || message.includes('过大') ? 'notice notice-error' : 'notice'}>{message}</div> : null}

        <TaskFlowSteps steps={['上传原图', '语言与输出确认']} currentStep={workflowStep} ariaLabel="图片翻译创建步骤" onStepChange={setWorkflowStep} />

        {workflowStep === 0 ? (
          <section className="panel task-flow-panel translation-input-panel">
            <div className="panel-head">
              <div><h2>上传原图</h2><p>JPG / PNG / WebP / HEIC · 最多 {MAX_IMAGES} 张 · 合计不超过 28MB</p></div>
              {files.length ? <button className="btn btn-secondary btn-compact" type="button" onClick={() => setFiles([])}><Trash2 size={15} />清空</button> : null}
            </div>
            <div className="panel-body translation-input-body">
              <div
                className={dragging ? 'translation-dropzone translation-dropzone-active' : 'translation-dropzone'}
                onDragEnter={() => setDragging(true)}
                onDragLeave={() => setDragging(false)}
                onDragOver={event => event.preventDefault()}
                onDrop={event => void onDrop(event)}
                onClick={() => { if (!processingImages) fileInputRef.current?.click() }}
                role="button"
                tabIndex={0}
                onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') fileInputRef.current?.click() }}
              >
                <Upload size={26} />
                <strong>{processingImages ? imageUploadMessage('PROCESSING') : (files.length ? '继续添加图片' : '选择或拖入图片')}</strong>
                <span>{files.length}/{MAX_IMAGES} 张</span>
                <input ref={fileInputRef} className="translation-file-input" type="file" accept={IMAGE_FILE_ACCEPT} multiple disabled={processingImages || loading} onChange={event => void onFileInput(event)} />
              </div>
              {files.length ? (
                <div className="translation-upload-list">
                  {files.map((file, index) => (
                    <article className="translation-upload-item" key={`${file.name}:${file.size}:${file.lastModified}`}>
                      <div className="translation-upload-preview"><img src={previews[index]} alt="" /></div>
                      <div className="translation-upload-copy"><strong>{index + 1}. {file.name}</strong><span>{(file.size / 1024 / 1024).toFixed(2)} MB</span></div>
                      <button type="button" className="icon-btn" title="移除图片" aria-label={`移除 ${file.name}`} onClick={() => setFiles(current => current.filter((_, itemIndex) => itemIndex !== index))}><X size={17} /></button>
                    </article>
                  ))}
                </div>
              ) : <div className="translation-empty"><Images size={28} /><span>尚未选择图片</span></div>}
            </div>
            <div className="task-flow-actions">
              <span className="muted">选择完成后设置目标语言和输出清晰度。</span>
              <button className="btn btn-primary" type="button" disabled={!files.length || processingImages} onClick={() => setWorkflowStep(1)}>下一步：翻译设置<ArrowRight size={16} /></button>
            </div>
          </section>
        ) : null}

        {workflowStep === 1 ? (
          <>
            <div className="translation-layout">
              <section className="panel translation-input-panel">
                <div className="panel-head">
                  <div><h2>待翻译图片</h2><p>已选择 {files.length} 张，返回上一步可继续添加或移除。</p></div>
                </div>
                <div className="panel-body translation-input-body">
                  {files.length ? (
                    <div className="translation-upload-list">
                      {files.map((file, index) => (
                        <article className="translation-upload-item" key={`${file.name}:${file.size}:${file.lastModified}`}>
                          <div className="translation-upload-preview"><img src={previews[index]} alt="" /></div>
                          <div className="translation-upload-copy"><strong>{index + 1}. {file.name}</strong><span>{(file.size / 1024 / 1024).toFixed(2)} MB</span></div>
                        </article>
                      ))}
                    </div>
                  ) : <div className="translation-empty"><Images size={28} /><span>请先返回上传图片</span></div>}
                </div>
              </section>
              <aside className="translation-options">
                <section className="panel">
                  <div className="panel-head"><div><h2>翻译设置</h2><p>自动识别原文</p></div><Languages size={20} /></div>
                  <div className="panel-body translation-option-body">
                    <label className="field">
                      <span>目标语言</span>
                      <select className="input" value={targetLanguage} onChange={event => setTargetLanguage(event.target.value)}>
                        {languages.map(language => <option key={language.code} value={language.code}>{language.name} / {language.native_name}</option>)}
                      </select>
                    </label>
                    <div className="field">
                      <span>输出分辨率</span>
                      <div className="translation-resolution" role="group" aria-label="输出分辨率">
                        {RESOLUTIONS.map(item => <button key={item} type="button" className={resolution === item ? 'translation-resolution-active' : ''} onClick={() => setResolution(item)}>{item}</button>)}
                      </div>
                    </div>
                    <div className="translation-cost">
                      <span>{files.length} 张</span>
                      <BillingOnly><strong>{estimatedCost} 积分</strong></BillingOnly>
                      <BillingOnly><small>当前余额：{credits ?? '--'} 积分</small></BillingOnly>
                    </div>
                    <button className="btn btn-primary btn-block" type="button" disabled={!files.length || loading || processingImages} onClick={submit}>
                      {loading ? <RefreshCw size={16} className="spin" /> : <Sparkles size={16} />}
                      {loading ? '正在提交...' : '确认并开始翻译'}
                    </button>
                  </div>
                </section>
              </aside>
            </div>
            <div className="task-flow-actions task-flow-actions-standalone">
              <button className="btn btn-secondary" type="button" onClick={() => setWorkflowStep(0)}><ArrowLeft size={16} />上一步</button>
              <span className="muted">原图与译图一一对应。<BillingOnly>失败仅退回对应图片积分。</BillingOnly></span>
            </div>
          </>
        ) : null}

        <section className="panel translation-results">
          <div className="panel-head">
            <div>
              <h2>翻译结果</h2>
              <p>{tasks.length ? `完成 ${succeeded}/${tasks.length} · 处理中 ${running} · 失败 ${failed}` : '每张原图对应一张译图'}</p>
            </div>
            {tasks.length ? (
              <div className="page-actions">
                <button className="btn btn-secondary" type="button" disabled={loading} onClick={() => refreshProject()}><RefreshCw size={15} />刷新</button>
                <button className="btn btn-primary" type="button" disabled={!completedTasks.length || downloading} onClick={downloadAll}><Download size={15} />{downloading ? '准备中...' : `批量下载 (${completedTasks.length})`}</button>
              </div>
            ) : null}
          </div>
          <div className="panel-body translation-result-list">
            {tasks.length ? tasks.map((task, index) => {
              const source = sourceForTask(task, sources)
              const snapshot = parseSnapshot(task)
              return (
                <article className="translation-result-item" key={task.id}>
                  <div className="translation-result-head">
                    <div><strong>{index + 1}. {snapshot.sourceFilename || source?.original_filename || task.image_name}</strong><span>{snapshot.targetLanguageName || String(detail?.original_info?.target_language_name || '')}</span></div>
                    <span className={statusClass(task.status)}>{statusText(task.status)}</span>
                  </div>
                  <div className="translation-pair">
                    <figure>
                      <figcaption>原图</figcaption>
                      <div className="translation-media">{source?.url ? <img src={assetUrl(source.url)} alt="原图" /> : <FileImage size={34} />}</div>
                    </figure>
                    <ArrowRight className="translation-pair-arrow" size={22} />
                    <figure>
                      <figcaption>译图</figcaption>
                      <div className="translation-media">
                        {task.result_url ? <img src={assetUrl(task.result_url)} alt="翻译结果" /> : task.status === 'failed' ? <span>生成失败</span> : <RefreshCw size={30} className={task.status === 'running' ? 'spin' : ''} />}
                      </div>
                    </figure>
                  </div>
                  {task.safe_error ? <p className="translation-error">{task.safe_error}</p> : null}
                  <div className="translation-result-actions">
                    <span><BillingOnly>{task.credits_cost} 积分 · </BillingOnly>{snapshot.resolution || resolution}</span>
                    <div>
                      {task.result_url ? <button className="btn btn-secondary btn-compact" type="button" onClick={() => downloadTask(task, index)}><Download size={14} />下载</button> : null}
                      {task.status === 'failed' ? <button className="btn btn-primary btn-compact" type="button" disabled={loading} onClick={() => retry(task)}><RefreshCw size={14} />重试</button> : null}
                    </div>
                  </div>
                </article>
              )
            }) : <div className="translation-empty translation-results-empty"><FileImage size={32} /><span>暂无翻译任务</span></div>}
          </div>
        </section>
      </div>
    </UserAuthGate>
  )
}
