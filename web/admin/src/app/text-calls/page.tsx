'use client'

import { useEffect, useState } from 'react'
import { RefreshCw, Search } from 'lucide-react'
import { Pagination } from '@/components/Pagination'
import { useI18n } from '@/components/i18n'
import { AdminTextCallRow, AdminTextCallSummary, getToken, listTextCalls } from '@/lib/api'

interface Filters {
  keyword: string
  userKeyword: string
  provider: string
  model: string
  feature: string
  status: string
  from: string
  to: string
}

const EMPTY_FILTERS: Filters = {
  keyword: '',
  userKeyword: '',
  provider: '',
  model: '',
  feature: '',
  status: '',
  from: '',
  to: ''
}

const EMPTY_SUMMARY: AdminTextCallSummary = {
  calls: 0,
  users: 0,
  success: 0,
  failed: 0,
  queued: 0,
  success_rate: 0,
  avg_duration_ms: 0
}

function statusClass(status: string) {
  if (status === 'success') return 'status-good'
  if (status === 'failed' || status === 'empty') return 'status-bad'
  return 'status-warn'
}

function duration(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '-'
  if (value < 1000) return `${Math.round(value)} ms`
  return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} s`
}

function featureName(feature: string, t: (key: string) => string) {
  const key = `textCalls.feature.${feature}`
  const translated = t(key)
  return translated === key ? feature || '-' : translated
}

function statusName(status: string, t: (key: string) => string) {
  const key = `textCalls.status.${status}`
  const translated = t(key)
  return translated === key ? status || '-' : translated
}

export default function TextCallsPage() {
  const t = useI18n()
  const [rows, setRows] = useState<AdminTextCallRow[]>([])
  const [summary, setSummary] = useState<AdminTextCallSummary>(EMPTY_SUMMARY)
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState('')
  const pageSize = 20

  async function load(nextPage = page, nextFilters = filters) {
    setLoading(true)
    try {
      const result = await listTextCalls({ ...nextFilters, page: nextPage, pageSize })
      setRows(result.rows)
      setSummary(result.summary || EMPTY_SUMMARY)
      setPage(result.page)
      setTotal(result.total)
      setTotalPages(result.total_pages)
      setMessage(result.rows.length ? '' : t('textCalls.empty'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('textCalls.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!getToken()) {
      window.location.href = '/login'
      return
    }
    load(1, EMPTY_FILTERS)
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') load(page, filters)
    }, 15000)
    return () => window.clearInterval(timer)
  }, [page, filters])

  function patch(key: keyof Filters, value: string) {
    setFilters(current => ({ ...current, [key]: value }))
  }

  function reset() {
    setFilters(EMPTY_FILTERS)
    load(1, EMPTY_FILTERS)
  }

  return (
    <>
      <div className="page-head">
        <div>
          <span className="eyebrow">AI Usage</span>
          <h1>{t('textCalls.title')}</h1>
          <p>{t('textCalls.desc')}</p>
        </div>
        <button className="btn btn-primary" type="button" onClick={() => load(page, filters)} disabled={loading}>
          <RefreshCw size={16} />{loading ? t('textCalls.loading') : t('refresh')}
        </button>
      </div>

      <div className="metric-grid text-call-metric-grid">
        <article className="metric-card"><span className="metric-label">{t('textCalls.totalCalls')}</span><strong className="metric-value">{summary.calls}</strong></article>
        <article className="metric-card"><span className="metric-label">{t('textCalls.users')}</span><strong className="metric-value">{summary.users}</strong></article>
        <article className="metric-card"><span className="metric-label">{t('textCalls.successRate')}</span><strong className="metric-value">{Number(summary.success_rate || 0).toFixed(2)}%</strong><small className="muted">{summary.success} / {summary.calls}</small></article>
        <article className="metric-card"><span className="metric-label">{t('textCalls.avgDuration')}</span><strong className="metric-value">{duration(summary.avg_duration_ms)}</strong><small className="muted">{t('textCalls.filteredScope')}</small></article>
      </div>

      <section className="admin-card">
        <div className="filter-bar text-call-filter-bar">
          <input className="input" value={filters.userKeyword} onChange={event => patch('userKeyword', event.target.value)} placeholder={t('textCalls.userSearch')} />
          <input className="input" value={filters.provider} onChange={event => patch('provider', event.target.value)} placeholder="Provider" />
          <input className="input" value={filters.model} onChange={event => patch('model', event.target.value)} placeholder={t('model')} />
          <input className="input" value={filters.feature} onChange={event => patch('feature', event.target.value)} placeholder={t('textCalls.feature')} />
          <select className="select" value={filters.status} onChange={event => patch('status', event.target.value)} aria-label={t('status')}>
            <option value="">{t('allStatus')}</option>
            <option value="success">{t('textCalls.status.success')}</option>
            <option value="failed">{t('textCalls.status.failed')}</option>
            <option value="queued">{t('textCalls.status.queued')}</option>
            <option value="empty">{t('textCalls.status.empty')}</option>
          </select>
          <input className="input" type="date" value={filters.from} onChange={event => patch('from', event.target.value)} aria-label={t('textCalls.from')} />
          <input className="input" type="date" value={filters.to} onChange={event => patch('to', event.target.value)} aria-label={t('textCalls.to')} />
          <input className="input" value={filters.keyword} onChange={event => patch('keyword', event.target.value)} onKeyDown={event => { if (event.key === 'Enter') load(1, filters) }} placeholder={t('textCalls.keyword')} />
          <button className="btn btn-primary" type="button" onClick={() => load(1, filters)}><Search size={16} />{t('textCalls.search')}</button>
          <button className="btn" type="button" onClick={reset}>{t('textCalls.reset')}</button>
        </div>

        {message || (loading && !rows.length) ? <div className="muted spacious-message">{message || t('textCalls.loading')}</div> : null}
        <div className="table-wrap">
          <table className="data-table text-call-table">
            <thead><tr><th>{t('textCalls.time')}</th><th>{t('textCalls.user')}</th><th>{t('textCalls.feature')}</th><th>Provider / {t('model')}</th><th>{t('status')}</th><th>{t('textCalls.duration')}</th><th>{t('textCalls.characters')}</th></tr></thead>
            <tbody>
              {rows.map(row => (
                <tr key={row.id}>
                  <td><strong>{new Date(row.created_at).toLocaleString()}</strong><span className="muted small-text text-call-id">{row.id}</span></td>
                  <td className="text-call-user-cell">
                    {row.attributed ? <><strong>{row.user_nickname ? `${row.user_nickname} · ${row.user_email || row.user_id}` : row.user_email || row.user_id}</strong><span>{row.user_id}</span></> : <strong>{t('textCalls.systemUser')}</strong>}
                  </td>
                  <td><strong>{featureName(row.feature, t)}</strong><span className="muted small-text">{row.feature}{row.target_type ? ` · ${row.target_type}` : ''}</span></td>
                  <td><strong>{row.provider || '-'}</strong><span className="muted small-text">{row.model || '-'}</span></td>
                  <td><span className={statusClass(row.status)}>{statusName(row.status, t)}</span>{row.error_type ? <span className="task-error-copy">{row.error_type}{row.http_status ? ` · HTTP ${row.http_status}` : ''}</span> : null}</td>
                  <td>{duration(row.duration_ms)}</td>
                  <td><strong>{row.input_chars}</strong><span className="muted text-call-char-arrow">→</span><strong>{row.output_chars}</strong></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={next => load(next, filters)} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>
    </>
  )
}
