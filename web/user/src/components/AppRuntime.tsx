'use client'

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { LoaderCircle, RefreshCw } from 'lucide-react'
import { BrandLogo } from './BrandLogo'
import { LanguageMenu } from './LanguageMenu'
import { useI18n } from './i18n'
import { hasBilling, hasRegistration, loadRuntime, resetRuntime, RuntimeError, type RuntimeState } from '@/lib/runtime'

type ContextValue = RuntimeState & { billingEnabled: boolean; registrationEnabled: boolean; reload: () => Promise<void> }
const RuntimeContext = createContext<ContextValue | null>(null)

export function useRuntime() {
  const value = useContext(RuntimeContext)
  if (!value) throw new Error('AppRuntime is required')
  return value
}

export function BillingOnly({ children }: { children: ReactNode }) {
  return useRuntime().billingEnabled ? children : null
}

export function AppRuntime({ children, admin = false }: { children: ReactNode; admin?: boolean }) {
  const [runtime, setRuntime] = useState<RuntimeState | null>(null)
  const [error, setError] = useState<RuntimeError | null>(null)
  const pathname = usePathname()
  const router = useRouter()
  const t = useI18n()
  const reload = useCallback(async () => {
    setRuntime(null)
    setError(null)
    resetRuntime()
    try { setRuntime(await loadRuntime()) } catch (cause) {
      setError(cause instanceof RuntimeError ? cause : new RuntimeError('Configuration unavailable'))
    }
  }, [])

  useEffect(() => {
    let active = true
    loadRuntime().then(value => { if (active) setRuntime(value) }).catch(cause => { if (active) setError(cause) })
    const refresh = () => { void reload() }
    window.addEventListener('picpilot:runtime-refresh', refresh)
    return () => { active = false; window.removeEventListener('picpilot:runtime-refresh', refresh) }
  }, [reload])

  const billingEnabled = runtime ? hasBilling(runtime.config) : false
  const registrationEnabled = runtime ? hasRegistration(runtime.config) : false
  const redirect = runtime?.needsSetup && pathname !== '/setup' ? '/setup'
    : runtime && !runtime.needsSetup && pathname === '/setup' ? '/login'
    : runtime && !admin && pathname === '/register' && !registrationEnabled ? '/login'
    : runtime && !admin && pathname === '/credits' && !billingEnabled ? (runtime.config.edition === 'personal' ? '/providers' : '/workspace')
    : runtime && !admin && runtime.config.edition === 'personal' && pathname === '/messages' ? '/workspace'
    : runtime && !admin && pathname === '/download' ? '/workspace'
    : null

  useEffect(() => { if (redirect) router.replace(redirect) }, [redirect, router])
  useEffect(() => {
    if (!runtime) return
    document.documentElement.dataset.edition = runtime.config.edition
    document.documentElement.dataset.billingEnabled = String(billingEnabled)
  }, [runtime, billingEnabled])

  if (error || !runtime || redirect) return (
    <main className="runtime-page">
      <div className="runtime-state" role={error ? 'alert' : 'status'}>
        <BrandLogo />
        <LanguageMenu />
        <p>{error ? t('runtimeUnavailable') : t('runtimeLoading')}</p>
        {error ? <><small>{error.code}{error.requestId ? ' / ' + error.requestId : ''}</small>
          <button className="btn btn-primary" onClick={() => void reload()}><RefreshCw size={16} />{t('retryRuntime')}</button></>
          : <LoaderCircle size={20} className="spin" aria-hidden="true" />}
      </div>
    </main>
  )

  if (admin && runtime.config.edition === 'personal' && !runtime.needsSetup) return (
    <main className="runtime-page"><section className="runtime-state"><BrandLogo /><LanguageMenu /><h1>{t('personalEdition')}</h1><p>{t('adminUnavailable')}</p></section></main>
  )

  return <RuntimeContext.Provider value={{ ...runtime, billingEnabled, registrationEnabled, reload }}>
    {runtime.config.demo_mode ? <div className="runtime-demo" role="status">{t('demoMode')}</div> : null}
    {children}
  </RuntimeContext.Provider>
}

