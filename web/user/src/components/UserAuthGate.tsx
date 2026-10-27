'use client'

import Link from 'next/link'
import { ReactNode, useEffect, useLayoutEffect, useState } from 'react'
import { ApiError, clearSession, getMe, getStoredUser, getToken, loginUrl, saveStoredUser } from '@/lib/api'
import { useI18n } from '@/components/i18n'
import { useRuntime } from '@/components/AppRuntime'

interface UserAuthGateProps {
  children: ReactNode
  title?: string
  description?: string
}

const useIsomorphicLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect
const initialGateState = { isAllowed: false, checking: true }

function cachedGateState() {
  return getToken() && getStoredUser()
    ? { isAllowed: true, checking: false }
    : { isAllowed: false, checking: true }
}

function sleep(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}

export function UserAuthGate({ children, title, description }: UserAuthGateProps) {
  const { registrationEnabled } = useRuntime()
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
        window.location.replace(loginUrl(next))
        return
      }
      const cached = getStoredUser()
      if (cached && active) {
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
        if (active) {
          saveStoredUser(user)
          setGate({ isAllowed: true, checking: false })
        }
      } catch (error) {
        if (error instanceof ApiError && error.code === 'TOKEN_INVALID') {
          clearSession()
          if (active) setGate({ isAllowed: false, checking: false })
          return
        }
        if (error instanceof ApiError && error.code === 'UNAUTHORIZED') {
          clearSession()
          if (active) setGate({ isAllowed: false, checking: false })
          return
        }
        if (active) setGate(cached ? { isAllowed: true, checking: false } : { isAllowed: false, checking: false })
      }
    }
    verifySession()
    const expire = () => active && setGate({ isAllowed: false, checking: false })
    window.addEventListener('openimage:auth-expired', expire)
    return () => {
      active = false
      window.removeEventListener('openimage:auth-expired', expire)
    }
  }, [])

  if (gate.checking) {
    return <section className="auth-gate-transition" aria-busy="true" aria-label={t('openingLogin')} />
  }

  if (!gate.isAllowed) {
    return (
      <section className="panel auth-required-panel">
        <div className="panel-head">
          <div>
            <h1>{title || t('authRequiredTitle')}</h1>
            <p>{description || t('authRequiredDesc')}</p>
          </div>
        </div>
        <div className="panel-body">
          <Link className="btn btn-primary" href="/login">{t('goLogin')}</Link>
          {registrationEnabled ? <Link className="btn" href="/register">{t('registerAccount')}</Link> : null}
        </div>
      </section>
    )
  }

  return children
}
