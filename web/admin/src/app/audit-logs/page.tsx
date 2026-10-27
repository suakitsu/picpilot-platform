'use client'

import { useEffect, useMemo, useState } from 'react'
import { Pagination } from '@/components/Pagination'
import { useI18n } from '@/components/i18n'
import { AdminAuditLog, getToken, listAuditLogs } from '@/lib/api'

function actionLabel(action: string, t: (key: string) => string) {
  const translated = t(`audit.action.${action}`)
  return translated === `audit.action.${action}` ? action : translated
}

export default function AuditLogsPage() {
  const [logs, setLogs] = useState<AdminAuditLog[]>([])
  const [message, setMessage] = useState('')
  const [keyword, setKeyword] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const pageSize = 20
  const t = useI18n()

  async function load(nextKeyword = keyword, nextPage = page) {
    try {
      const data = await listAuditLogs({ keyword: nextKeyword, page: nextPage, pageSize })
      setLogs(data.rows)
      setPage(data.page)
      setTotal(data.total)
      setTotalPages(data.total_pages)
      setMessage(data.rows.length ? '' : t('audit.empty'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('audit.loadFailed'))
    }
  }

  useEffect(() => {
    if (!getToken()) {
      window.location.href = '/login'
      return
    }
    load('', 1)
  }, [])

  const filtered = useMemo(() => {
    const value = keyword.trim().toLowerCase()
    if (!value) return logs
    return logs.filter(log => [
      log.action,
      actionLabel(log.action, t),
      log.admin_email_masked,
      log.target_type,
      log.target_id,
      log.summary
    ].some(item => String(item || '').toLowerCase().includes(value)))
  }, [keyword, logs, t])

  return (
    <>
      <div className="page-head">
        <div>
          <span className="eyebrow">Audit</span>
          <h1>{t('audit.title')}</h1>
          <p>{t('audit.desc')}</p>
        </div>
        <button className="btn btn-primary" onClick={() => load(keyword, 1)}>{t('audit.refresh')}</button>
      </div>

      <section className="admin-card">
        <div className="section-head">
          <div><h2>{t('audit.records')}</h2><p>{t('audit.recordsDesc')}</p></div>
          <input className="input page-search" value={keyword} onChange={event => { setKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(event.currentTarget.value, 1) }} placeholder={t('audit.search')} />
        </div>
        {message || !logs.length ? <div className="muted spacious-message">{message || t('audit.loading')}</div> : null}
        <div className="table-wrap">
          <table className="data-table">
            <thead><tr><th>{t('common.time')}</th><th>{t('audit.admin')}</th><th>{t('audit.action')}</th><th>{t('audit.target')}</th><th>{t('audit.summary')}</th><th>{t('common.details')}</th></tr></thead>
            <tbody>
              {filtered.map(log => (
                <tr key={log.id}>
                  <td>{new Date(log.created_at).toLocaleString()}</td>
                  <td>{log.admin_email_masked}</td>
                  <td><span className="status-good">{actionLabel(log.action, t)}</span></td>
                  <td>{log.target_type}<br /><small className="muted">{log.target_id}</small></td>
                  <td>{log.summary}</td>
                  <td><button className="btn" onClick={() => setExpanded(expanded === log.id ? null : log.id)}>{expanded === log.id ? t('common.collapse') : t('common.expand')}</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={nextPage => load(keyword, nextPage)} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
        {expanded ? (
          <pre className="admin-json-preview">
{JSON.stringify({
  before: safeJson(logs.find(log => log.id === expanded)?.before_json),
  after: safeJson(logs.find(log => log.id === expanded)?.after_json)
}, null, 2)}
          </pre>
        ) : null}
      </section>
    </>
  )
}

function safeJson(value?: string) {
  if (!value) return {}
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}
