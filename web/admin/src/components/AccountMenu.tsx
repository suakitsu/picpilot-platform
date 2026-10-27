'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { LogOut } from 'lucide-react'
import { assetUrl, logoutSession, getStoredUser, type UserProfile } from '@/lib/api'
import { useI18n } from '@/components/i18n'

export function AccountMenu() {
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
      <div className="admin-actions">
        <Link href="/login" className="btn btn-primary btn-compact">{t('login')}</Link>
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
    <div className="admin-actions">
      <Link href="/profile" className="admin-user">
        {user.avatar_url ? <img className="avatar" src={assetUrl(user.avatar_url)} alt={t('profile')} /> : <span className="avatar">{user.email_masked.slice(0, 1).toUpperCase()}</span>}
        <span className="admin-user-copy"><b>{user.email_masked}</b><small>{user.role}</small></span>
      </Link>
      {logoutError ? <span role="alert">{logoutError}</span> : null}
      <button className="text-button admin-logout-button" type="button" aria-label={t('logout')} title={t('logout')} onClick={() => void logout()} disabled={loggingOut}>
        <LogOut size={15} aria-hidden="true" />
        <span className="logout-label">{t('logout')}</span>
      </button>
    </div>
  )
}
