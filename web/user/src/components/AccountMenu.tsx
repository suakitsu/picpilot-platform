'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { assetUrl, logoutSession, getStoredUser, type UserProfile } from '@/lib/api'
import { useI18n } from '@/components/i18n'
import { useRuntime } from '@/components/AppRuntime'

export function AccountMenu() {
  const { registrationEnabled } = useRuntime()
  const [user, setUser] = useState<UserProfile | null>(null)

  const t = useI18n()
  const [loggingOut, setLoggingOut] = useState(false)
  const [logoutError, setLogoutError] = useState('')

  useEffect(() => {
    const sync = () => setUser(getStoredUser())
    sync()

    window.addEventListener('openimage:user-updated', sync)
    window.addEventListener('storage', sync)
    return () => {
      window.removeEventListener('openimage:user-updated', sync)
      window.removeEventListener('storage', sync)
    }
  }, [])

  if (!user) {
    return (
      <div className="account-actions">
        <Link href="/login" className="btn">{t('login')}</Link>
        {registrationEnabled ? <Link href="/register" className="btn btn-primary">{t('register')}</Link> : null}
      </div>
    )
  }

  async function logout() {
    if (loggingOut) return
    setLoggingOut(true)
    setLogoutError('')
    try {
      await logoutSession()
      window.location.href = '/login'
    } catch (cause) {
      setLogoutError(cause instanceof Error ? cause.message : t('logoutFailed'))
    } finally { setLoggingOut(false) }
  }

  return (
    <div className="account-menu">
      <Link href="/profile" className="avatar-link" title={t('profile')}>
        {user.avatar_url ? <img className="avatar" src={assetUrl(user.avatar_url)} alt={t('profile')} /> : <span className="avatar">{(user.email_display || user.email_masked).slice(0, 1).toUpperCase()}</span>}
        <span className="account-copy"><b>{user.email_display || user.email_masked}</b><small>{t('profile')}</small></span>
      </Link>
      {logoutError ? <span role="alert">{logoutError}</span> : null}
      <button className="btn" onClick={() => void logout()} disabled={loggingOut}>{t('logout')}</button>
    </div>
  )
}
