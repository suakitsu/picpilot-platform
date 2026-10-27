'use client'

import { useEffect, useState } from 'react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { useI18n } from '@/components/i18n'
import { assetUrl, BenefitConfig, getBenefitConfig, listMessages, markMessageRead, requireLogin, StationMessage } from '@/lib/api'

export default function MessagesPage() {
  const t = useI18n()
  const [messages, setMessages] = useState<StationMessage[]>([])
  const [active, setActive] = useState<StationMessage | null>(null)
  const [notice, setNotice] = useState('')
  const [benefitConfig, setBenefitConfig] = useState<BenefitConfig | null>(null)

  async function load() {
    setNotice(t('messagesLoading'))
    try {
      const rows = await listMessages()
      const first = rows[0] || null
      setMessages(rows)
      setActive(first)
      setNotice(rows.length ? '' : t('messagesEmpty'))
      if (first && !first.read) markAsRead(first)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : t('messagesLoadFailed'))
    }
  }

  useEffect(() => { if (requireLogin()) load() }, [])
  useEffect(() => {
    if (!requireLogin()) return
    getBenefitConfig().then(setBenefitConfig).catch(() => setBenefitConfig(null))
  }, [])

  async function markAsRead(message: StationMessage) {
    await markMessageRead(message.id).catch(() => {})
    setMessages(rows => rows.map(row => row.id === message.id ? { ...row, read: true } : row))
    setActive(current => current?.id === message.id ? { ...current, read: true } : current)
    window.dispatchEvent(new Event('openimage:messages-updated'))
  }

  function openMessage(message: StationMessage) {
    setActive(message.read ? message : { ...message, read: true })
    if (!message.read) markAsRead(message)
  }

  const qqGroupValue = benefitConfig?.qq_group?.trim() || ''
  const legacyQqLink = /^https?:\/\//i.test(qqGroupValue) ? qqGroupValue : ''
  const qqJoinLink = benefitConfig?.qq_link?.trim() || legacyQqLink

  return (
    <Shell>
      <UserAuthGate title={t('loginMessagesTitle')} description={t('loginMessagesDesc')}>
        <div className="page-head"><div><h1>{t('messagesTitle')}</h1><p>{t('messagesDesc')}</p></div><button className="btn btn-primary" onClick={load}>{t('refresh')}</button></div>
        {benefitConfig?.enabled ? (
          <section className="panel message-benefit-card">
            <div>
              <span className="status status-warn">{t('benefitAttention')}</span>
              <h2>{benefitConfig.title || t('qqTitle')}</h2>
              <p>{benefitConfig.description || t('qqDesc')}</p>
              {benefitConfig.free_highlight_label ? <strong className="benefit-highlight">{benefitConfig.free_highlight_label}</strong> : null}
              {qqJoinLink ? <a className="btn btn-secondary" href={qqJoinLink} target="_blank" rel="noreferrer">QQ</a> : null}
              {!qqJoinLink && qqGroupValue ? <strong className="benefit-highlight">QQ {qqGroupValue}</strong> : null}
            </div>
            {(benefitConfig.qq_qr_url || benefitConfig.wechat_qr_url) ? (
              <div className="message-benefit-qr">
                {benefitConfig.qq_qr_url ? <img src={assetUrl(benefitConfig.qq_qr_url)} alt="QQ" /> : null}
                {benefitConfig.wechat_qr_url ? <img src={assetUrl(benefitConfig.wechat_qr_url)} alt="WeChat" /> : null}
              </div>
            ) : null}
          </section>
        ) : null}
        {notice ? <div className="notice">{notice}</div> : null}
        <div className="grid-2">
          <section className="panel">
            <div className="panel-head"><div><h2>{t('inbox')}</h2></div></div>
            <div className="message-list">
              {messages.map(message => (
                <button key={message.id} className={active?.id === message.id ? 'message-item message-item-active' : 'message-item'} onClick={() => openMessage(message)}>
                  <span className={message.read ? 'status' : 'status status-warn'}>{message.read ? t('read') : t('unread')}</span>
                  <strong>{message.title}</strong>
                  <small>{new Date(message.published_at).toLocaleString()}</small>
                </button>
              ))}
            </div>
          </section>
          <section className="panel">
            <div className="panel-head"><div><h2>{active?.title || t('selectMessage')}</h2><p>{active ? new Date(active.published_at).toLocaleString() : ''}</p></div></div>
            <div className="panel-body"><p className="message-content">{active?.content || t('selectMessageDesc')}</p></div>
          </section>
        </div>
      </UserAuthGate>
    </Shell>
  )
}
