'use client'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { Shell } from '@/components/Shell'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { deleteProject, getToken, listProjects, ProjectSummary } from '@/lib/api'

const features: Array<{ title: string; desc: string; action: string; href: string; meta: string }> = [
  { title: 'templates', desc: 'templateCenterFeatureDesc', action: 'start', href: '/templates', meta: 'opened' },
  { title: 'listingSetTitle', desc: 'listingSetDesc', action: 'start', href: '/commerce?tab=listing', meta: 'fullSet' },
  { title: 'aplusTitle', desc: 'aplusFeatureDesc', action: 'generate', href: '/commerce?tab=aplus', meta: 'opened' },
  { title: 'commerceSuite', desc: 'commerceSuiteFeatureDesc', action: 'create', href: '/commerce?tab=suite', meta: 'fullSet' },
  { title: 'domesticCommerce', desc: 'domesticCommerceFeatureDesc', action: 'create', href: '/commerce?tab=domestic', meta: 'opened' },
  { title: 'productVideo', desc: 'productVideoFeatureDesc', action: 'create', href: '/video-create', meta: 'productVideoMeta' },
  { title: 'imageAgentTitle', desc: 'imageAgentDesc', action: 'create', href: '/ai-create?tab=agent', meta: 'freePlay' },
  { title: 'copyAgent', desc: 'copyAgentFeatureDesc', action: 'start', href: '/ai-create?tab=copy', meta: 'opened' }
]

function statusLabel(status: string) {
  if (status === 'draft') return 'draft'
  if (status === 'copy_generated') return 'copyGenerated'
  if (status === 'facts_confirmed') return 'factsConfirmed'
  if (status === 'images_queued') return 'generating'
  if (status === 'images_failed') return 'failed'
  if (status === 'images_partial_failed') return 'partialFailed'
  if (status === 'images_completed') return 'completed'
  return status
}

function statusClass(status: string) {
  if (status === 'images_queued') return 'status status-warn'
  if (status === 'images_failed' || status === 'images_partial_failed') return 'status status-bad'
  if (status === 'images_completed' || status === 'copy_generated' || status === 'facts_confirmed') return 'status status-ok'
  return 'status'
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

function imageCountText(project: ProjectSummary, t: (key: string) => string) {
  if (isCreativeChatProject(project)) {
    return `${project.success_images}/${project.total_images} ${t('agentGeneration')}`
  }
  return `${project.success_images}/${project.total_images}`
}

export default function WorkspacePage() {
  const { billingEnabled } = useRuntime()
  const [projects, setProjects] = useState<ProjectSummary[]>([])
  const [message, setMessage] = useState('')
  const [isLoadingProjects, setIsLoadingProjects] = useState(true)
  const t = useI18n()
  const confirm = useConfirm()

  useEffect(() => {
    if (!getToken()) {
      setIsLoadingProjects(false)
      setMessage(t('loginToView'))
      return
    }
    setIsLoadingProjects(true)
    listProjects()
      .then(setProjects)
      .catch(error => setMessage(error instanceof Error ? error.message : t('loginToView')))
      .finally(() => setIsLoadingProjects(false))
  }, [t])

  const queuedCount = projects.filter(project => project.status === 'images_queued').length
  const failedCount = projects.filter(project => project.status === 'images_failed' || project.status === 'images_partial_failed').length

  async function removeProject(project: ProjectSummary) {
    if (!await confirm({ title: t('delete'), message: `${t('confirmDeletePrefix')}「${project.product_name}」${t('confirmDeleteSuffix')}`, tone: 'danger' })) return
    await deleteProject(project.id)
    setProjects(rows => rows.filter(row => row.id !== project.id))
  }

  return (
    <Shell>
      <div className="page-head">
        <div>
          <h1>{t('dashboardTitle')}</h1>
          <p>{t('dashboardDesc')}</p>
        </div>
        <Link href="/commerce?tab=listing" className="btn btn-primary">{t('newListing')}</Link>
      </div>

      <div className="summary-strip">
        <div>
          <div className="muted">{t('recentProjects')}</div>
          <div className="credit-value">{projects.length} {t('projectCount')}</div>
        </div>
        <div className="compact-action-bar">
          {!isLoadingProjects && !message && (failedCount > 0 ? <span className="status status-bad">{failedCount} {t('failed')}</span> : queuedCount > 0 ? <span className="status status-warn">{t('runningTasks')}</span> : <span className="status">{t('noActiveProjects')}</span>)}
          <BillingOnly><Link href="/credits" className="btn btn-secondary">{t('viewCredits')}</Link></BillingOnly>
        </div>
      </div>

      {message ? <div className="notice notice-error">{message}</div> : null}

      <section className="panel workspace-tutorial">
        <div className="panel-head">
          <div>
            <h2>{t('workspaceTutorialTitle')}</h2>
            <p>{t('workspaceTutorialDesc')}</p>
          </div>
        </div>
        <div className="panel-body tutorial-steps">
          {[t('workspaceStep1'), t('workspaceStep2'), t(billingEnabled ? 'workspaceStep3' : 'workspaceStep3Personal')].map((step, index) => (
            <div className="tutorial-step" key={step}>
              <span>{index + 1}</span>
              <strong>{step}</strong>
            </div>
          ))}
        </div>
      </section>

      <section className="panel mt-24">
        <div className="panel-head">
          <div>
            <h2>{t('coreFeatures')}</h2>
            <p>{t('featureHint')}</p>
          </div>
        </div>
        <div className="panel-body grid-3">
          {features.map(feature => (
            <article className={feature.href.includes('tab=listing') ? 'panel feature-card core-feature-primary' : 'panel feature-card'} key={feature.title}>
              <h3>{t(feature.title)}</h3>
              <p>{t(feature.desc)}</p>
              <div className="feature-meta">{t(feature.meta)}</div>
              <div className="mt-16">
                <Link href={feature.href} className="btn btn-primary">{t(feature.action)}</Link>
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="panel mt-24">
        <div className="panel-head">
          <div>
            <h2>{t('recentProjectTitle')}</h2>
            <p>{t('recentProjectDesc')}</p>
          </div>
        </div>
        <div className="table-wrap">
          <table className="data-table data-table-projects">
            <thead><tr><th>{t('productName')}</th><th>{t('marketplace')}</th><th>{t('status')}</th><th>{t('images')}</th><th>{t('actions')}</th></tr></thead>
            <tbody>
              {projects.slice(0, 8).map(project => (
                <tr key={project.id}>
                  <td>{project.product_name}{project.latest_failure_reason ? <div className="table-subtext status-bad">{project.latest_failure_reason}</div> : null}</td><td>{project.country}</td><td><span className={statusClass(project.status)}>{t(statusLabel(project.status))}</span></td><td>{imageCountText(project, t)}{project.failed_images ? <span className="table-subtext status-bad"> · {project.failed_images} {t('failed')}</span> : null}</td><td><div className="table-actions"><Link href={projectEntryHref(project)} className="btn btn-secondary">{t('continue')}</Link><button className="btn btn-danger" onClick={() => removeProject(project)}>{t('delete')}</button></div></td>
                </tr>
              ))}
              {isLoadingProjects ? <tr><td colSpan={5} className="muted">{t('loadingHistory')}</td></tr> : null}
              {!isLoadingProjects && !projects.length ? <tr><td colSpan={5} className="muted">{t('emptyProjects')}</td></tr> : null}
            </tbody>
          </table>
        </div>
      </section>
    </Shell>
  )
}
