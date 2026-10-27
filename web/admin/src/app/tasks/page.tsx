'use client'

import { useEffect, useState } from 'react'
import { ExternalLink, Eye, FileText, Image as ImageIcon, RotateCcw, Trash2, X } from 'lucide-react'
import { useI18n } from '@/components/i18n'
import { Pagination } from '@/components/Pagination'
import { useConfirm } from '@/components/ConfirmProvider'
import {
  AdminImageTaskDetail,
  AdminTaskRow,
  assetUrl,
  deleteImageTask,
  getImageTask,
  listImageTasks,
  retryImageTask
} from '@/lib/api'

const ACTIVE = new Set(['queued', 'running'])

function cls(status: string) {
  if (status === 'success') return 'status-good'
  if (status === 'failed') return 'status-bad'
  return 'status-warn'
}

function statusKey(status: string) {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'failed'
  if (status === 'queued') return 'queued'
  return status
}

function costLabel(task: AdminTaskRow, notReturned: string) {
  const value = Number(task.actual_cost || 0)
  if (!value) return notReturned
  return `¥${value.toFixed(4)}`
}

function costDetailLabel(task: AdminTaskRow) {
  const details = task.cost_details
  if (!details) return ''
  if (details.upstream_cost_unit === 'APIMART_CREDIT' && details.upstream_cost != null && details.conversion_rate != null) {
    const source = details.source === 'upstream_credits_cost' ? '上游实扣' : '档位兜底'
    return `${source} ${Number(details.upstream_cost).toFixed(3)} Credits × ¥${Number(details.conversion_rate).toFixed(6)}`
  }
  if (details.upstream_cost_unit === 'USD' && details.upstream_cost != null && details.conversion_rate != null) {
    return `上游实扣 $${Number(details.upstream_cost).toFixed(4)} × ¥${Number(details.conversion_rate).toFixed(6)}`
  }
  if (details.source === 'fixed_price') return '固定单价 / 张'
  if (details.source === 'local_worker_fixed' || details.source === 'local_worker') return '本机 Worker 固定单价 / 张'
  if (details.source === 'stored_actual_cost') return '任务落库成本'
  if (details.source === 'not_charged') return '未产生可确认成本'
  return details.source === 'provider_price_table' ? '供应商价格表' : ''
}

function providerAttemptLabel(task: AdminTaskRow, translate: (key: string) => string) {
  const attempts = task.provider_attempts || []
  if (ACTIVE.has(task.status)) {
    const current = attempts.length ? attempts[attempts.length - 1] : undefined
    const provider = current?.provider || task.provider || 'provider'
    return `${provider}: ${translate(task.status === 'queued' ? 'queued' : 'running')}`
  }
  if (!attempts.length) return ''
  return attempts.map(item => {
    const provider = item.provider || 'provider'
    const size = item.returnedSize || item.requestSize || ''
    const status = item.success || item.status === 'success'
      ? 'OK'
      : item.status === 'queued'
        ? translate('queued')
        : item.status === 'running'
          ? translate('running')
          : translate('failed')
    return `${provider}${size ? ` ${size}` : ''}: ${status}`
  }).join(' / ')
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatBytes(value?: number) {
  if (!value || value < 1) return '-'
  if (value < 1024 * 1024) return `${Math.ceil(value / 1024)} KB`
  return `${(value / 1024 / 1024).toFixed(2)} MB`
}

function userLabel(task: AdminTaskRow) {
  const account = task.user_email || task.user_account || task.user_email_masked || task.user_id || '-'
  return task.user_nickname ? `${task.user_nickname} · ${account}` : account
}

function prettyJson(value: unknown) {
  if (value == null) return '-'
  if (typeof value === 'string') return value || '-'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function hasProjectContext(detail: AdminImageTaskDetail) {
  const project = detail.project
  if (!project) return false
  return Boolean(project.product_name || project.brand || project.country || project.language
    || Object.keys((project.original_info as Record<string, unknown>) || {}).length
    || Object.keys((project.confirmed_facts as Record<string, unknown>) || {}).length)
}

export default function Page() {
  const confirm = useConfirm()
  const [tasks, setTasks] = useState<AdminTaskRow[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [provider, setProvider] = useState('')
  const [imageType, setImageType] = useState('')
  const [userKeyword, setUserKeyword] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [message, setMessage] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [detail, setDetail] = useState<AdminImageTaskDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState('')
  const [preview, setPreview] = useState<{ url: string; title: string } | null>(null)
  const pageSize = 20
  const t = useI18n()

  async function load(nextPage = page) {
    try {
      const data = await listImageTasks({ keyword, status, provider, imageType, userKeyword, from, to, page: nextPage, pageSize })
      setTasks(data.rows)
      setPage(data.page)
      setTotal(data.total)
      setTotalPages(data.total_pages)
      setMessage(data.rows.length ? '' : t('noTasks'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('taskLoadFailed'))
    }
  }

  useEffect(() => { load(1) }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') load(page)
    }, 10000)
    return () => window.clearInterval(timer)
  }, [page, keyword, status, provider, imageType, userKeyword, from, to])

  useEffect(() => {
    if (!detail && !preview) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (preview) setPreview(null)
      else setDetail(null)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', closeOnEscape)
    }
  }, [detail, preview])

  const failedTasks = tasks.filter(task => task.status === 'failed')

  async function handleRetry(taskId: string, skipConfirm = false) {
    if (!skipConfirm && !await confirm({ title: t('retry'), message: `${t('retry')} ${t('taskId')} ${taskId}？` })) return
    try {
      const data = await retryImageTask(taskId)
      setTasks(current => current.map(task => task.task_id === taskId ? { ...task, status: data.status, safe_error: '' } : task))
      setMessage(`${t('taskId')} ${taskId} ${t('taskRequeued')}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('taskRetryFailed'))
    }
  }

  async function retryFailed() {
    if (!failedTasks.length) {
      setMessage(t('noMatchedTasks'))
      return
    }
    if (!await confirm({ title: t('retryFailed'), message: `${t('retryFailed')} ${failedTasks.length} ${t('tasks')}？` })) return
    for (const task of failedTasks) await handleRetry(task.task_id, true)
    await load(page)
  }

  async function handleDelete(taskId: string) {
    if (!await confirm({ title: t('delete'), message: `${t('delete')} ${t('taskId')} ${taskId}？`, tone: 'danger' })) return
    try {
      await deleteImageTask(taskId)
      setTasks(current => current.filter(task => task.task_id !== taskId))
      if (detail?.task_id === taskId) setDetail(null)
      setMessage(`${t('taskId')} ${taskId} ${t('taskDeleted')}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('taskDeleteFailed'))
    }
  }

  async function openDetail(taskId: string) {
    setDetailLoading(taskId)
    try {
      setDetail(await getImageTask(taskId))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('taskDetailFailed'))
    } finally {
      setDetailLoading('')
    }
  }

  function openPreview(url: string | undefined, title: string) {
    if (!url) return
    setPreview({ url: assetUrl(url), title })
  }

  return (
    <>
      <div className="page-head">
        <div><h1>{t('taskTitle')}</h1><p>{t('taskDesc')}</p></div>
        <button className="btn btn-primary" onClick={retryFailed}><RotateCcw size={16} />{t('retryFailed')}</button>
      </div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      <div className="filter-bar action-filter-bar admin-filter-grid">
        <select className="select" value={status} onChange={event => { setStatus(event.target.value); setPage(1) }}><option value="">{t('allStatus')}</option><option value="queued">{t('queued')}</option><option value="running">{t('running')}</option><option value="success">{t('success')}</option><option value="failed">{t('failed')}</option></select>
        <input className="input" value={provider} onChange={event => { setProvider(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder={t('filters.providerPlaceholder')} />
        <input className="input" value={imageType} onChange={event => { setImageType(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder={t('filters.imageTypePlaceholder')} />
        <input className="input" value={userKeyword} onChange={event => { setUserKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder={t('filters.userPlaceholder')} />
        <input className="input" type="date" value={from} onChange={event => { setFrom(event.target.value); setPage(1) }} aria-label={t('filters.from')} />
        <input className="input" type="date" value={to} onChange={event => { setTo(event.target.value); setPage(1) }} aria-label={t('filters.to')} />
        <input className="input" value={keyword} onChange={event => { setKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder={t('searchTask')} />
        <button className="btn btn-secondary" onClick={() => load(1)}>{t('refresh')}</button>
      </div>

      <section className="admin-card table-card operation-card operation-sensitive">
        <table className="data-table admin-task-table admin-image-task-table">
          <thead><tr><th>{t('taskPreview')}</th><th>{t('taskId')}</th><th>{t('common.user')}</th><th>{t('taskTime')}</th><th>{t('imageType')}</th><th>{t('model')}</th><th>{t('supplierCost')}</th><th>{t('status')}</th><th>{t('actions')}</th></tr></thead>
          <tbody>
            {tasks.map(task => (
              <tr key={task.task_id}>
                <td className="admin-task-preview-cell">
                  {task.result_url ? (
                    <button className="admin-task-thumbnail" type="button" title={t('taskOpenPreview')} onClick={() => openPreview(task.result_url, task.task_id)}>
                      <img src={assetUrl(task.result_url)} alt="" />
                      <span><Eye size={15} /></span>
                    </button>
                  ) : <div className="admin-task-thumbnail admin-task-thumbnail-empty"><ImageIcon size={20} /><span>{t('taskNoResult')}</span></div>}
                </td>
                <td className="admin-task-identity-cell">
                  <strong>{task.task_id}</strong>
                  <span>{task.project_id}</span>
                  {task.hidden_by_user ? <span className="status-warn small-text">{t('taskHiddenByUser')}</span> : null}
                </td>
                <td className="admin-task-user-cell"><strong title={task.user_email || task.user_account}>{userLabel(task)}</strong><span>{task.user_id}</span></td>
                <td className="admin-task-time-cell"><strong>{formatDate(task.created_at)}</strong><span>{t('common.updatedAt')} {formatDate(task.updated_at)}</span></td>
                <td><strong>{task.image_type || '-'}</strong></td>
                <td className="admin-task-model-cell"><strong>{task.model || task.provider || '-'}</strong><span>{providerAttemptLabel(task, t) || task.provider || '-'}</span></td>
                <td><strong>{costLabel(task, t('notReturned'))}</strong>{costDetailLabel(task) ? <span className="muted small-text">{costDetailLabel(task)}</span> : null}</td>
                <td className="admin-task-status-cell"><span className={cls(task.status)}>{t(statusKey(task.status))}</span>{task.safe_error ? <span className="task-error-copy" title={task.safe_error}>{task.safe_error}</span> : null}</td>
                <td>
                  <div className="admin-actions row-action-group admin-task-actions">
                    <button className="btn btn-secondary" type="button" onClick={() => openDetail(task.task_id)} disabled={detailLoading === task.task_id}><FileText size={15} />{detailLoading === task.task_id ? t('common.loading') : t('common.details')}</button>
                    <button className="icon-button" type="button" title={t('taskOpenPreview')} disabled={!task.result_url} onClick={() => openPreview(task.result_url, task.task_id)}><Eye size={16} /></button>
                    <button className="icon-button" type="button" title={t('retry')} onClick={() => handleRetry(task.task_id)} disabled={task.status !== 'failed'}><RotateCcw size={16} /></button>
                    <button className="icon-button danger-action" type="button" title={ACTIVE.has(task.status) ? t('taskDeleteAfterFinish') : t('delete')} disabled={ACTIVE.has(task.status)} onClick={() => handleDelete(task.task_id)}><Trash2 size={16} /></button>
                  </div>
                </td>
              </tr>
            ))}
            {!tasks.length ? <tr><td colSpan={9} className="muted">{t('noMatchedTasks')}</td></tr> : null}
          </tbody>
        </table>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={load} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>

      {detail ? (
        <div className="admin-modal-backdrop admin-drawer-backdrop" onMouseDown={() => setDetail(null)}>
          <section className="modal-panel admin-action-modal admin-action-drawer admin-task-detail-drawer" role="dialog" aria-modal="true" aria-labelledby="task-detail-title" onMouseDown={event => event.stopPropagation()}>
            <div className="drawer-head">
              <div><span className="eyebrow"><FileText size={14} />{t('taskDetailKicker')}</span><h2 id="task-detail-title">{detail.task_id}</h2><p>{detail.project?.product_name || detail.image_type} · {detail.project_id}</p></div>
              <button className="icon-button drawer-close" type="button" aria-label={t('confirm.close')} onClick={() => setDetail(null)}><X size={18} /></button>
            </div>

            <div className="task-detail-result">
              {detail.result_url ? (
                <button type="button" onClick={() => openPreview(detail.result_url, detail.task_id)}>
                  <img src={assetUrl(detail.result_url)} alt={detail.image_type} />
                  <span><Eye size={16} />{t('taskOpenPreview')}</span>
                </button>
              ) : <div className="task-detail-result-empty"><ImageIcon size={28} /><span>{t('taskNoResult')}</span></div>}
              {detail.result_url ? <a className="btn btn-secondary" href={assetUrl(detail.result_url)} target="_blank" rel="noreferrer"><ExternalLink size={15} />{t('taskOpenOriginal')}</a> : null}
            </div>

            <div className="drawer-summary task-detail-summary">
              <div><span>{t('status')}</span><strong className={cls(detail.status)}>{t(statusKey(detail.status))}</strong></div>
              <div><span>{t('supplierCost')}</span><strong>{costLabel(detail, t('notReturned'))}</strong></div>
              <div><span>{t('taskCredits')}</span><strong>{detail.credits_cost ?? 0}</strong></div>
              <div><span>{t('model')}</span><strong>{detail.model || '-'}</strong></div>
            </div>

            <div className="panel-body task-detail-body">
              <section className="task-detail-section">
                <div className="task-detail-section-head"><h3>{t('taskUserAndTime')}</h3></div>
                <dl className="task-detail-metadata">
                  <div><dt>{t('common.email')}</dt><dd>{detail.user_email || detail.user_account || '-'}</dd></div>
                  <div><dt>User ID</dt><dd>{detail.user_id}</dd></div>
                  <div><dt>{t('common.createdAt')}</dt><dd>{formatDate(detail.created_at)}</dd></div>
                  <div><dt>{t('common.updatedAt')}</dt><dd>{formatDate(detail.updated_at)}</dd></div>
                  <div><dt>Provider</dt><dd>{detail.provider || '-'}</dd></div>
                  <div><dt>{t('imageType')}</dt><dd>{detail.image_type || '-'}</dd></div>
                </dl>
              </section>

              <section className="task-detail-section">
                <div className="task-detail-section-head"><h3>{t('taskInputDetails')}</h3></div>
                <pre className="admin-json-preview task-detail-json">{prettyJson(detail.prompt_details || detail.prompt_snapshot)}</pre>
              </section>

              {hasProjectContext(detail) ? (
                <section className="task-detail-section">
                  <div className="task-detail-section-head"><h3>{t('taskProjectContext')}</h3></div>
                  <dl className="task-detail-metadata">
                    <div><dt>{t('taskProductName')}</dt><dd>{detail.project?.product_name || '-'}</dd></div>
                    <div><dt>{t('taskBrand')}</dt><dd>{detail.project?.brand || '-'}</dd></div>
                    <div><dt>{t('taskCountry')}</dt><dd>{detail.project?.country || '-'}</dd></div>
                    <div><dt>{t('language')}</dt><dd>{detail.project?.language || '-'}</dd></div>
                  </dl>
                  <pre className="admin-json-preview task-detail-json">{prettyJson({ original_info: detail.project?.original_info, confirmed_facts: detail.project?.confirmed_facts })}</pre>
                </section>
              ) : null}

              <section className="task-detail-section">
                <div className="task-detail-section-head"><h3>{t('taskReferenceImages')}</h3><span>{detail.reference_images?.length || 0}</span></div>
                {detail.reference_images?.length ? (
                  <div className="task-reference-grid">
                    {detail.reference_images.map((image, index) => (
                      <button type="button" key={image.id || `${image.url}-${index}`} onClick={() => openPreview(image.url, image.original_filename || `${t('taskReferenceImages')} ${index + 1}`)}>
                        <img src={assetUrl(image.url)} alt={image.original_filename || ''} />
                        <span><strong>{image.original_filename || `${index + 1}`}</strong><small>{image.width && image.height ? `${image.width} × ${image.height}` : image.image_format || '-'} · {formatBytes(image.file_size)}</small></span>
                      </button>
                    ))}
                  </div>
                ) : <p className="muted">{t('taskNoReferences')}</p>}
              </section>

              {detail.safe_error ? (
                <section className="task-detail-section task-detail-error-section">
                  <div className="task-detail-section-head"><h3>{t('errorReason')}</h3></div>
                  <p>{detail.safe_error}</p>
                </section>
              ) : null}

              {detail.provider_attempts?.length ? (
                <section className="task-detail-section">
                  <div className="task-detail-section-head"><h3>{t('taskProviderAttempts')}</h3></div>
                  <pre className="admin-json-preview task-detail-json">{prettyJson(detail.provider_attempts)}</pre>
                </section>
              ) : null}
            </div>
          </section>
        </div>
      ) : null}

      {preview ? (
        <div className="admin-image-lightbox" role="dialog" aria-modal="true" aria-label={preview.title} onMouseDown={() => setPreview(null)}>
          <div className="admin-image-lightbox-toolbar"><strong>{preview.title}</strong><div><a className="icon-button" href={preview.url} target="_blank" rel="noreferrer" title={t('taskOpenOriginal')}><ExternalLink size={18} /></a><button className="icon-button" type="button" title={t('confirm.close')} onClick={() => setPreview(null)}><X size={20} /></button></div></div>
          <img src={preview.url} alt={preview.title} onMouseDown={event => event.stopPropagation()} />
        </div>
      ) : null}
    </>
  )
}
