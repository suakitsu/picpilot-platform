'use client'
import { BillingOnly } from '@/components/AppRuntime'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { VideoHistoryTable } from '@/components/VideoHistoryTable'
import { assetUrl, deleteProject, listProjects, ProjectSummary, requireLogin } from '@/lib/api'

function statusLabel(status: string, t: (key: string) => string) {
  if (status === 'draft') return t('draft')
  if (status === 'copy_generated') return t('copyGenerated')
  if (status === 'facts_confirmed') return t('factsConfirmed')
  if (status === 'images_queued') return t('generating')
  if (status === 'images_failed') return t('failed')
  if (status === 'images_partial_failed') return t('partialFailed')
  if (status === 'images_completed') return t('completed')
  return status
}

function statusClass(status: string) {
  if (status === 'images_queued') return 'status status-warn'
  if (status === 'images_failed' || status === 'images_partial_failed') return 'status status-bad'
  if (status === 'images_completed' || status === 'copy_generated' || status === 'facts_confirmed') return 'status status-ok'
  return 'status'
}

function formatDate(value: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function imageProgress(project: ProjectSummary) {
  const total = project.total_images || 0
  if (!total) return '0%'
  return `${Math.round(((project.success_images || 0) / total) * 100)}%`
}

function projectPreviewUrls(project: ProjectSummary) {
  const urls: string[] = []
  for (const item of project.preview_urls || []) {
    if (item && !urls.includes(item)) urls.push(item)
  }
  if (project.cover_url && !urls.includes(project.cover_url)) {
    urls.unshift(project.cover_url)
  }
  return urls.slice(0, 4)
}

function HistoryPreview({ project }: { project: ProjectSummary }) {
  const previews = projectPreviewUrls(project)
  if (!previews.length) {
    return <span className="history-table-empty">{project.product_name.slice(0, 2).toUpperCase()}</span>
  }
  return (
    <div className="history-preview-stack" title={previews.length > 1 ? `${previews.length} 张预览` : ''}>
      {previews.map((url, index) => (
        <img className="history-table-thumb" src={assetUrl(url)} alt="" loading="lazy" decoding="async" key={`${url}-${index}`} />
      ))}
      {previews.length > 1 ? <span className="history-preview-count">{previews.length}张</span> : null}
    </div>
  )
}

function isCreativeChatProject(project: ProjectSummary) {
  return project.source_type === 'creative_chat' && Boolean(project.source_chat_id)
}

function projectEntryHref(project: ProjectSummary) {
  if (isCreativeChatProject(project)) {
    return `/ai-create?tab=agent&chatId=${encodeURIComponent(project.source_chat_id || '')}&projectId=${encodeURIComponent(project.id)}`
  }
  return `/projects/${project.id}#tasks`
}

function projectProgressText(project: ProjectSummary, t: (key: string) => string) {
  if (isCreativeChatProject(project)) {
    return `${project.success_images}/${project.total_images} ${t('agentGeneration')} · ${imageProgress(project)}`
  }
  return `${project.success_images}/${project.total_images} ${t('images')} · ${imageProgress(project)}`
}

function imageCountText(project: ProjectSummary, t: (key: string) => string) {
  if (isCreativeChatProject(project)) {
    return `${project.success_images}/${project.total_images} ${t('agentGeneration')}`
  }
  return `${project.success_images}/${project.total_images}`
}

export default function Page() {
  const [historyKind, setHistoryKind] = useState<'images' | 'videos'>('images')
  const [projects, setProjects] = useState<ProjectSummary[]>([])
  const [keyword, setKeyword] = useState('')
  const [message, setMessage] = useState('')
  const t = useI18n()
  const confirm = useConfirm()

  const [filterStatus, setFilterStatus] = useState('all')
  const [filterCountry, setFilterCountry] = useState('all')
  const [dateRange, setDateRange] = useState('30')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  useEffect(() => {
    if (!requireLogin()) return
    listProjects()
      .then(data => {
        setProjects(data)
        setMessage(data.length ? '' : t('emptyHistory'))
      })
      .catch(error => setMessage(error instanceof Error ? error.message : t('historyLoadFailed')))
  }, [t])

  const filtered = projects.filter(project => {
    const normalizedKeyword = keyword.trim().toLowerCase()
    if (filterCountry !== 'all' && project.country !== filterCountry) return false
    if (filterStatus === 'completed' && project.status !== 'images_completed') return false
    if (filterStatus === 'generating' && !['images_queued', 'facts_confirmed', 'copy_generated'].includes(project.status)) return false
    if (filterStatus === 'failed' && !['images_failed', 'images_partial_failed'].includes(project.status)) return false
    if (filterStatus === 'draft' && project.status !== 'draft') return false
    if (dateRange !== 'all') {
      const updatedAt = new Date(project.updated_at).getTime()
      const days = Number(dateRange)
      if (!Number.isNaN(days) && Date.now() - updatedAt > days * 24 * 60 * 60 * 1000) return false
    }
    if (normalizedKeyword) {
      const searchable = [
        project.product_name,
        project.brand,
        project.country,
        project.language,
        statusLabel(project.status, t),
      ].filter(Boolean).join(' ').toLowerCase()
      if (!searchable.includes(normalizedKeyword)) return false
    }
    return true
  })

  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const safePage = Math.min(page, pageCount)
  const pageStart = (safePage - 1) * pageSize
  const pageEnd = Math.min(pageStart + pageSize, filtered.length)
  const pagedProjects = filtered.slice(pageStart, pageEnd)

  useEffect(() => {
    setPage(1)
  }, [keyword, filterStatus, filterCountry, dateRange, pageSize, projects.length])

  async function removeProject(project: ProjectSummary) {
    if (!await confirm({ title: t('delete'), message: `${t('deleteProjectConfirmPrefix')}${project.product_name}${t('deleteProjectConfirmSuffix')}`, tone: 'danger' })) return
    try {
      await deleteProject(project.id)
      setProjects(rows => rows.filter(row => row.id !== project.id))
      setMessage(t('projectDeleted'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('deleteFailed'))
    }
  }

  if (historyKind === 'videos') {
    return (
      <Shell>
        <UserAuthGate title="历史记录" description="查看图片套组和商品视频任务。">
          <div className="history-kind-tabs" role="tablist" aria-label="历史记录类型">
            <button type="button" role="tab" aria-selected={false} onClick={() => setHistoryKind('images')}>图片任务</button>
            <button type="button" role="tab" aria-selected className="active">视频任务</button>
          </div>
          <VideoHistoryTable />
        </UserAuthGate>
      </Shell>
    )
  }

  return (
    <Shell>
      <UserAuthGate title={t('historyTitle')} description={t('historyDesc')}>
      <div className="history-kind-tabs" role="tablist" aria-label="历史记录类型">
        <button type="button" role="tab" aria-selected className="active">图片任务</button>
        <button type="button" role="tab" aria-selected={false} onClick={() => setHistoryKind('videos')}>视频任务</button>
      </div>
      <section className="panel">
        <div className="panel-head"><div><h2>{t('filters')}</h2><p>{t('filtersDesc')} · {filtered.length}/{projects.length}</p></div></div>
        <div className="panel-body history-filter-body"><select className="select" value={filterCountry} onChange={event => setFilterCountry(event.target.value)}><option value="all">{t('allMarkets')}</option><option value="DE">DE</option><option value="FR">FR</option><option value="US">US</option></select><select className="select" value={filterStatus} onChange={event => setFilterStatus(event.target.value)}><option value="all">{t('allStatuses')}</option><option value="completed">{t('completed')}</option><option value="generating">{t('generating')}</option><option value="failed">{t('failed')}</option><option value="draft">{t('draft')}</option></select><select className="select" value={dateRange} onChange={event => setDateRange(event.target.value)}><option value="30">{t('last30Days')}</option><option value="7">{t('last7Days')}</option><option value="all">{t('allTime')}</option></select><input className="input" value={keyword} onChange={event => setKeyword(event.target.value)} placeholder={t('searchProduct')} /></div>
      </section>
      <section className="panel mt-16 history-table-panel">
        <div className="panel-head history-list-head"><div><h2>{t('historyTableTitle')}</h2><p>{t('historyTableDesc')}</p></div>{filtered.length ? <div className="history-pagination history-pagination-top"><label>{t('pageSize')}<select className="select" value={pageSize} onChange={event => setPageSize(Number(event.target.value))}><option value="10">10</option><option value="20">20</option><option value="50">50</option></select></label><span>{filtered.length ? `${pageStart + 1}-${pageEnd} / ${filtered.length}` : '0 / 0'}</span><button className="btn btn-secondary" disabled={safePage <= 1} onClick={() => setPage(value => Math.max(1, value - 1))}>{t('prevPage')}</button><button className="btn btn-secondary" disabled={safePage >= pageCount} onClick={() => setPage(value => Math.min(pageCount, value + 1))}>{t('nextPage')}</button></div> : null}</div>
        {message || !projects.length ? <div className="panel-body muted">{message || t('loadingHistory')}</div> : null}
        {filtered.length ? <><div className="table-wrap"><table className="data-table data-table-history"><thead><tr><th>{t('historyPreview')}</th><th>{t('setProductName')}</th><th>{t('marketplace')}</th><th>{t('status')}</th><th>{t('imageCount')}</th><BillingOnly><th>{t('creditsCost')}</th></BillingOnly><th>{t('updatedAt')}</th><th>{t('actions')}</th></tr></thead><tbody>{pagedProjects.map(project => { const href = projectEntryHref(project); return <tr key={project.id}><td><HistoryPreview project={project} /></td><td><div className="history-table-product"><strong>{project.product_name}</strong><span>{project.brand || '-'} · {project.language}</span><span>{projectProgressText(project, t)}</span></div>{project.latest_failure_reason ? <div className="table-subtext status-bad">{project.latest_failure_reason}</div> : null}</td><td>{project.country}</td><td><span className={statusClass(project.status)}>{statusLabel(project.status, t)}</span></td><td>{imageCountText(project, t)}{project.failed_images ? <span className="table-subtext status-bad"> · {project.failed_images} {t('failed')}</span> : null}</td><BillingOnly><td>{project.credits_cost}</td></BillingOnly><td>{formatDate(project.updated_at)}</td><td><div className="table-actions"><Link className="btn btn-secondary" href={href}>{t('enterSet')}</Link><button className="btn btn-danger" onClick={() => removeProject(project)}>{t('delete')}</button></div></td></tr> })}</tbody></table></div><div className="history-pagination history-pagination-bottom"><span>{filtered.length ? `${pageStart + 1}-${pageEnd} / ${filtered.length}` : '0 / 0'}</span><button className="btn btn-secondary" disabled={safePage <= 1} onClick={() => setPage(value => Math.max(1, value - 1))}>{t('prevPage')}</button><button className="btn btn-secondary" disabled={safePage >= pageCount} onClick={() => setPage(value => Math.min(pageCount, value + 1))}>{t('nextPage')}</button></div></> : projects.length ? <div className="panel-body muted">{t('emptyHistory')}</div> : null}
      </section>
      </UserAuthGate>
    </Shell>
  )
}
