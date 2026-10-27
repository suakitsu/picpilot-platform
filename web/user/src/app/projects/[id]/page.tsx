'use client'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import Link from 'next/link'
import { useParams } from 'next/navigation'
import { useEffect, useMemo, useState } from 'react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { ImageReferenceInput } from '@/components/ImageReferenceInput'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { assetUrl, deleteProjectImage, getProject, ImageTaskView, isAiDisabledError, ProjectDetail, requireLogin, retryProjectImage } from '@/lib/api'
import { copyToClipboard } from '@/lib/clipboard'

const TASK_ORDER = ['main', 'scenario', 'features', 'dimensions', 'steps', 'comparison', 'package', 'aplus', 'creative']

function taskStatusKey(status: string) {
  if (status === 'queued') return 'queued'
  if (status === 'running') return 'generating'
  if (status === 'success') return 'success'
  if (status === 'failed') return 'failed'
  return status || 'draft'
}

function statusClass(status: string) {
  if (status === 'success') return 'status status-ok'
  if (status === 'failed') return 'status status-bad'
  if (status === 'queued' || status === 'running') return 'status status-warn'
  return 'status'
}

function textValue(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function projectEditorHref(detail: ProjectDetail) {
  const originalInfo = detail.original_info || {}
  const confirmedFacts = detail.confirmed_facts || {}
  const mode = textValue(confirmedFacts.mode) || textValue(originalInfo.mode)
  const keywordMode = Array.isArray(originalInfo.keywords) ? originalInfo.keywords.map(String).join(',') : ''
  const taskType = detail.image_tasks[0]?.image_type || ''
  if (mode === 'creative' || keywordMode.includes('creative') || taskType === 'creative') return `/ai-create?tab=free&projectId=${detail.project.id}`
  if (mode === 'aplus' || keywordMode.includes('aplus') || taskType === 'aplus') return `/commerce?tab=aplus&projectId=${detail.project.id}`
  return `/commerce?tab=listing&projectId=${detail.project.id}`
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

function isErrorMessage(message: string) {
  return isAiDisabledError(message)
}

export default function ProjectDetailPage() {
  const params = useParams<{ id: string }>()
  const { billingEnabled } = useRuntime()
  const t = useI18n()
  const confirm = useConfirm()
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [message, setMessage] = useState(t('loadingProjectDetail'))
  const [isLoading, setIsLoading] = useState(false)
  const [retryingTask, setRetryingTask] = useState<ImageTaskView | null>(null)
  const [retryReferenceFile, setRetryReferenceFile] = useState<File | null>(null)
  const [retryReferencePreview, setRetryReferencePreview] = useState('')
  const [retryReferenceUrl, setRetryReferenceUrl] = useState('')
  const [retryReferenceText, setRetryReferenceText] = useState('')
  const [manualCopyText, setManualCopyText] = useState('')

  async function load() {
    setIsLoading(true)
    try {
      const data = await getProject(params.id)
      setDetail(data)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('projectDetailLoadFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (!requireLogin(`/projects/${params.id}`)) return
    load()
  }, [params.id])

  useEffect(() => {
    if (!detail?.image_tasks.some(task => task.status === 'queued' || task.status === 'running')) return
    const timer = window.setInterval(load, 8000)
    return () => window.clearInterval(timer)
  }, [detail?.project.id, detail?.image_tasks])

  const stats = useMemo(() => {
    const tasks = detail?.image_tasks || []
    return {
      total: tasks.length,
      success: tasks.filter(task => task.status === 'success').length,
      failed: tasks.filter(task => task.status === 'failed').length,
      running: tasks.filter(task => task.status === 'queued' || task.status === 'running').length,
      credits: tasks.reduce((sum, task) => sum + (task.credits_cost || 0), 0)
    }
  }, [detail])

  async function copyText(value: string, label: string) {
    if (!value) return
    const copied = await copyToClipboard(value)
    if (copied) {
      setManualCopyText('')
      setMessage(label)
    } else {
      setManualCopyText(value)
      setMessage(t('manualCopyHint'))
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

  async function retry(taskId: string, input: { referenceUrl?: string; referenceText?: string; reference?: File | null } = {}) {
    if (!detail) return
    setIsLoading(true)
    try {
      const updated = await retryProjectImage(detail.project.id, taskId, input)
      setDetail({
        ...detail,
        image_tasks: detail.image_tasks.map(task => task.id === taskId ? updated : task)
      })
      closeRetry()
      setMessage(t('retryQueued'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('retryFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  async function removeFailedTask(task: ImageTaskView) {
    if (!detail) return
    if (!await confirm({ title: t('delete'), message: `确定删除失败记录「${task.image_name}」吗？删除后不会再反复出现。`, tone: 'danger' })) return
    setIsLoading(true)
    try {
      await deleteProjectImage(detail.project.id, task.id)
      const nextTasks = detail.image_tasks.filter(row => row.id !== task.id)
      if (!nextTasks.length) {
        setDetail(null)
        setMessage('失败记录已删除，该套图已无可展示任务')
        return
      }
      const remainingFailed = nextTasks.filter(row => row.status === 'failed')
      const latestFailed = remainingFailed.reduce<ImageTaskView | null>((latest, row) => {
        if (!latest) return row
        return row.created_at > latest.created_at ? row : latest
      }, null)
      setDetail({
        ...detail,
        image_tasks: nextTasks,
        project: {
          ...detail.project,
          failed_images: remainingFailed.length,
          total_images: nextTasks.length,
          latest_failure_reason: latestFailed?.safe_error || '',
          latest_failed_task_id: latestFailed?.id || ''
        }
      })
      setMessage('失败记录已删除')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('deleteFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Shell>
      <UserAuthGate title={t('projectDetailLoginTitle')} description={t('projectDetailLoginDesc')}>
        <div className="page-head">
          <div>
            <h1>{detail?.project.product_name || t('projectDetailTitle')}</h1>
            <p>{t(billingEnabled ? 'projectDetailDesc' : 'generationConfirmPersonal')}</p>
          </div>
          <div className="page-actions">
            <Link className="btn btn-secondary" href="/history">{t('backHistory')}</Link>
            <button className="btn btn-primary" disabled={isLoading} onClick={load}>{t('refreshStatus')}</button>
          </div>
        </div>

        {message ? <div className={isErrorMessage(message) ? 'notice notice-error' : 'notice'}>{message}</div> : null}
        {manualCopyText ? (
          <div className="manual-copy-box">
            <strong>{t('manualCopyTitle')}</strong>
            <p>{t('manualCopyDesc')}</p>
            <textarea className="textarea" readOnly value={manualCopyText} onFocus={event => event.currentTarget.select()} />
          </div>
        ) : null}

        {detail ? (
          <>
            <section className="panel">
              <div className="panel-head"><div><h2>{t('setOverview')}</h2><p>{detail.project.brand} | {detail.project.country} | {detail.project.language}</p></div><Link className="btn btn-secondary" href={projectEditorHref(detail)}>{t('continueEdit')}</Link></div>
              <div className="panel-body grid-4">
                <div className="result-box"><strong>{stats.total}</strong><p>{t('taskCount')}</p></div>
                <div className="result-box"><strong>{stats.success}</strong><p>{t('success')}</p></div>
                <div className="result-box"><strong>{stats.failed}</strong><p>{t('failed')}</p></div>
                <div className="result-box"><strong>{stats.running}</strong><p>{t('generating')}</p></div>
              </div>
              {detail.project.latest_failure_reason ? <div className="notice notice-error">{detail.project.latest_failure_reason}</div> : null}
            </section>

            <section className="panel mt-24" id="tasks">
              <div className="panel-head"><div><h2>{t('referenceImagesTitle')}</h2><p>{t('referenceImagesDesc')}</p></div></div>
              <div className="panel-body grid-4">
                {detail.product_images.length ? detail.product_images.map(image => (
                  <article className="feature-card" key={image.id}>
                    <img src={assetUrl(image.url)} alt={image.image_type} style={{ width: '100%', borderRadius: 12 }} />
                    <p>{image.image_type}</p>
                  </article>
                )) : <p className="muted">{t('noReferenceImages')}</p>}
              </div>
            </section>

            <section className="panel mt-24">
              <div className="panel-head"><div><h2>{t('singleTasks')}</h2><p>{t('singleTasksDesc')}</p></div></div>
              <div className="panel-body grid-3">
                {detail.image_tasks.length ? sortedTasks(detail.image_tasks).map(task => (
                  <article className="feature-card" id={`task-${task.id}`} key={task.id}>
                    <h3>{task.image_name}</h3>
                    <p><span className={statusClass(task.status)}>{t(taskStatusKey(task.status))}</span></p>
                    {task.result_url ? <img src={assetUrl(task.result_url)} alt={task.image_name} style={{ width: '100%', borderRadius: 12 }} /> : null}
                    {task.status === 'failed' ? <div className="notice notice-error">{t(billingEnabled ? 'failedRefundRetry' : 'generationFailedPersonal').replace('{credits}', String(task.credits_cost))}</div> : null}
                    {task.safe_error ? <p className="muted">{task.safe_error}</p> : null}
                    <BillingOnly><div className="muted">{task.credits_cost} {t('creditsLabel')}</div></BillingOnly>
                    <div className="generation-card-actions">
                      {task.result_url ? <a className="btn btn-secondary" href={assetUrl(task.result_url)} target="_blank">{t('viewDownload')}</a> : null}
                      {task.result_url ? <button className="btn btn-secondary" type="button" onClick={() => copyText(assetUrl(task.result_url), t('copyImageUrlDone'))}>{t('copyImageUrl')}</button> : null}
                      <button className="btn btn-secondary" type="button" onClick={() => copyText(task.prompt_snapshot, t('copyPromptDone'))}>{t('copyPrompt')}</button>
                      {task.status !== 'queued' && task.status !== 'running' ? <button className="btn btn-primary" disabled={isLoading} onClick={() => openRetry(task)}>{t('retryGenerate')}</button> : null}
                      {task.status === 'failed' ? <button className="btn btn-danger" disabled={isLoading} onClick={() => removeFailedTask(task)}>{t('delete')}</button> : null}
                    </div>
                  </article>
                )) : <p className="muted">{t('noTasks')}</p>}
              </div>
            </section>
          </>
        ) : null}
        {retryingTask ? (
          <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={t('retryWithReference')}>
            <div className="modal-panel generation-confirm-panel">
              <h2>{t('retryWithReference')}</h2>
              <p>{t(billingEnabled ? 'retryWithReferenceDesc' : 'generationConfirmPersonal')}</p>
              <ImageReferenceInput
                label={t('retryReferenceImage')}
                hint={t('retryReferenceImageHint')}
                disabled={isLoading}
                file={retryReferenceFile}
                url={retryReferenceUrl}
                previewUrl={retryReferencePreview || assetUrl(retryReferenceUrl)}
                onFile={selectRetryReferenceFile}
                onClearFile={clearRetryReferenceFile}
                onUrl={setRetryReferenceUrl}
                uploadLabel={t('chooseImage')}
                removeLabel={t('removeImage')}
                linkLabel={t('referenceUrlLabel')}
                placeholder={t('referenceUrlPlaceholder')}
              />
              <label className="field mt-16"><span>{t('referenceTextLabel')}</span><textarea className="textarea" value={retryReferenceText} onChange={event => setRetryReferenceText(event.target.value)} placeholder={t('referenceTextPlaceholder')} /></label>
              <div className="modal-actions">
                <button className="btn" type="button" onClick={closeRetry}>{t('cancel')}</button>
                <button className="btn btn-primary" type="button" disabled={isLoading} onClick={() => retry(retryingTask.id, { referenceUrl: retryReferenceUrl.trim(), referenceText: retryReferenceText.trim(), reference: retryReferenceFile })}>{t('confirmRetryGenerate')}</button>
              </div>
            </div>
          </div>
        ) : null}
      </UserAuthGate>
    </Shell>
  )
}
