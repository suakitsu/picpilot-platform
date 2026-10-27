'use client'

import Link from 'next/link'
import { ReactNode, useEffect, useLayoutEffect, useState } from 'react'
import { ApiError, clearSession, getMe, getStoredUser, getToken, saveStoredUser } from '@/lib/api'
import { useI18n } from '@/components/i18n'

interface AdminAuthGateProps {
  children: ReactNode
}

function isAdminRole(role?: string) {
  return role === 'admin' || role === 'super_admin'
}

const useIsomorphicLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect
const initialGateState = { isAllowed: false, checking: true }

function cachedGateState() {
  const cached = getStoredUser()
  return getToken() && isAdminRole(cached?.role)
    ? { isAllowed: true, checking: false }
    : { isAllowed: false, checking: true }
}

function sleep(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}

export function AdminAuthGate({ children }: AdminAuthGateProps) {
  const [gate, setGate] = useState(initialGateState)
  const t = useI18n()

  useIsomorphicLayoutEffect(() => {
    setGate(cachedGateState())
  }, [])

  useEffect(() => {
    let active = true
    async function verifySession() {
      const token = getToken()
      if (!token) {
        const next = `${window.location.pathname}${window.location.search}`
        window.location.replace(`/login?next=${encodeURIComponent(next)}`)
        return
      }
      const cached = getStoredUser()
      if (isAdminRole(cached?.role) && active) {
        setGate({ isAllowed: true, checking: false })
      }
      try {
        let user
        try {
          user = await getMe({ passive: true })
        } catch (firstError) {
          if (cached) {
            await sleep(1200)
            user = await getMe({ passive: true })
          } else {
            throw firstError
          }
        }
        if (active && isAdminRole(user.role)) {
          saveStoredUser(user)
          setGate({ isAllowed: true, checking: false })
        }
        if (active && !isAdminRole(user.role)) {
          clearSession()
          setGate({ isAllowed: false, checking: false })
          window.location.replace('/login')
        }
      } catch (error) {
        if (error instanceof ApiError && error.code === 'TOKEN_INVALID') {
          clearSession()
          if (active) setGate({ isAllowed: false, checking: false })
          return
        }
        if (error instanceof ApiError && error.code === 'UNAUTHORIZED') {
          clearSession()
          if (active) {
            setGate({ isAllowed: false, checking: false })
          }
          return
        }
        if (active) {
          setGate(isAdminRole(cached?.role) ? { isAllowed: true, checking: false } : { isAllowed: false, checking: false })
        }
      }
    }
    verifySession()
    const expire = () => {
      if (!active) return
      setGate({ isAllowed: false, checking: false })
    }
    window.addEventListener('openimage:auth-expired', expire)
    return () => {
      active = false
      window.removeEventListener('openimage:auth-expired', expire)
    }
  }, [])

  if (gate.checking) return <div className="auth-gate-transition auth-gate-full" aria-busy="true" aria-label={t('adminLoginRequired')} />

  if (!gate.isAllowed) {
    return (
      <main className="auth-page">
        <section className="auth-card">
          <div className="auth-brand">
            <span className="admin-mark">PP</span>
            <div>
              <h1>{t('adminLoginRequired')}</h1>
              <p>{t('adminLoginRequiredDesc')}</p>
            </div>
          </div>
          <Link className="btn btn-primary btn-block" href="/login">{t('goLogin')}</Link>
        </section>
      </main>
    )
  }

  return children
}
