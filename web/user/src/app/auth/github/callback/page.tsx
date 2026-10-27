'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { RefreshCw } from 'lucide-react'
import { useI18n } from '@/components/i18n'
import { refreshSession, saveSession, type AuthResponse } from '@/lib/api'
import { safeNextPath } from '@/lib/navigation'

export default function GithubCallbackPage() {
  const [message, setMessage] = useState('')
  const [attempt, setAttempt] = useState(0)
  const exchange = useRef<Promise<AuthResponse> | null>(null)
  const destination = useRef<string | null>(null)
  const t = useI18n()

  useEffect(() => {
    if (destination.current === null) destination.current = safeNextPath(new URLSearchParams(window.location.search).get('next'))
    // Legacy token/user query values are never credentials and must leave the address bar.
    window.history.replaceState(window.history.state, '', window.location.pathname)
    let active = true
    if (!exchange.current) exchange.current = refreshSession()
    exchange.current.then(auth => {
      if (!active) return
      saveSession(auth)
      window.location.replace(destination.current || '/workspace')
    }).catch(error => {
      if (active) setMessage(error instanceof Error ? error.message : t('githubCallbackParseFailed'))
    })
    return () => { active = false }
  }, [attempt, t])

  function retry() {
    exchange.current = null
    setMessage('')
    setAttempt(value => value + 1)
  }

  return (
    <main className="auth-page">
      <section className="auth-card">
        <div className="auth-brand"><span className="brand-mark">PP</span><div><h1>{t('githubCallbackTitle')}</h1><p role={message ? 'alert' : 'status'}>{message || t('githubCallbackLoading')}</p></div></div>
        {message ? <button className="btn" onClick={retry}><RefreshCw size={16} />{t('retryRuntime')}</button> : null}
        <div className="auth-footer"><Link href="/login">{t('backLogin')}</Link></div>
      </section>
    </main>
  )
}
