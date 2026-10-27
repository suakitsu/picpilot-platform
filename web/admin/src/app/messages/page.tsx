'use client'

import { useEffect, useState } from 'react'
import { useI18n } from '@/components/i18n'
import { Pagination } from '@/components/Pagination'
import { useConfirm } from '@/components/ConfirmProvider'
import { listStationMessages, publishStationMessage, StationMessage } from '@/lib/api'

export default function AdminMessagesPage() {
  const [messages, setMessages] = useState<StationMessage[]>([])
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [message, setMessage] = useState('')
  const [publishing, setPublishing] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const pageSize = 20
  const t = useI18n()
  const confirm = useConfirm()

  async function load(nextPage = page) {
    try {
      const data = await listStationMessages({ keyword, page: nextPage, pageSize })
      setMessages(data.rows)
      setPage(data.page)
      setTotal(data.total)
      setTotalPages(data.total_pages)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('messages.loadFailed'))
    }
  }

  useEffect(() => { load() }, [])

  async function publish() {
    if (!await confirm({ title: t('messages.confirmPublish'), message: `${t('messages.confirmPublish')} ${title}？` })) return
    setPublishing(true)
    setMessage('')
    try {
      await publishStationMessage({ title, content, audience: 'all', expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined, enabled: true })
      setTitle('')
      setContent('')
      setExpiresAt('')
      setMessage(t('messages.published'))
      await load(1)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('messages.publishFailed'))
    } finally {
      setPublishing(false)
    }
  }

  return (
    <>
      <div className="page-head"><div><h1>{t('messages.title')}</h1><p>{t('messages.desc')}</p></div><button className="btn btn-secondary" onClick={() => load(1)}>{t('common.refresh')}</button></div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      <section className="admin-card operation-card operation-primary">
        <div className="section-head"><div><h2>{t('messages.compose')}</h2></div></div>
        <div className="admin-form-grid message-form">
          <label className="field"><span>{t('common.title')}</span><input className="input" value={title} onChange={event => setTitle(event.target.value)} /></label>
          <label className="field"><span>{t('common.expiry')}</span><input className="input" type="datetime-local" value={expiresAt} onChange={event => setExpiresAt(event.target.value)} /></label>
          <label className="field message-content-field"><span>{t('common.content')}</span><textarea className="textarea" value={content} onChange={event => setContent(event.target.value)} /></label>
          <div className="field form-submit-field"><span>&nbsp;</span><button className="btn btn-primary" disabled={publishing || !title || !content} onClick={publish}>{publishing ? t('common.publishing') : t('common.publish')}</button></div>
        </div>
      </section>
      <section className="admin-card table-card">
        <div className="section-head"><div><h2>{t('messages.log')}</h2></div></div>
        <div className="filter-bar action-filter-bar"><input className="input" value={keyword} onChange={event => { setKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder={t('messages.search')} /><button className="btn btn-secondary" onClick={() => load(1)}>{t('common.refresh')}</button></div>
        <table className="data-table"><thead><tr><th>{t('common.publishedAt')}</th><th>{t('common.title')}</th><th>{t('common.status')}</th><th>{t('common.readCount')}</th><th>{t('common.expiry')}</th><th>{t('common.content')}</th></tr></thead><tbody>{messages.map(item => <tr key={item.id}><td>{new Date(item.published_at).toLocaleString()}</td><td>{item.title}</td><td><span className={item.enabled ? 'status-good' : 'status-bad'}>{item.enabled ? t('messages.status.published') : t('messages.status.disabled')}</span></td><td>{item.read_count}</td><td>{item.expires_at ? new Date(item.expires_at).toLocaleString() : t('common.longTerm')}</td><td>{item.content}</td></tr>)}{!messages.length ? <tr><td colSpan={6} className="muted">{t('messages.noMessages')}</td></tr> : null}</tbody></table>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={load} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>
    </>
  )
}
