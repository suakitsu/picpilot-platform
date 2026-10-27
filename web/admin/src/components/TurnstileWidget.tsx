'use client'

import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { useI18n } from '@/components/i18n'
import { useRuntime } from '@/components/AppRuntime'

export interface TurnstileHandle { reset: () => void }
interface TurnstileWidgetProps { action: string; onToken: (token: string) => void; className?: string }

declare global {
  interface Window {
    turnstile?: {
      render: (container: HTMLElement, options: Record<string, unknown>) => string
      reset: (widgetId?: string) => void
      remove: (widgetId?: string) => void
    }
  }
}

const SCRIPT_ID = 'cloudflare-turnstile-js'
let scriptPromise: Promise<void> | undefined

function loadTurnstileScript() {
  if (window.turnstile) return Promise.resolve()
  if (scriptPromise) return scriptPromise
  scriptPromise = new Promise<void>((resolve, reject) => {
    const existing = document.getElementById(SCRIPT_ID) as HTMLScriptElement | null
    const script = existing || document.createElement('script')
    const fail = () => { script.remove(); reject(new Error('Turnstile script failed')) }
    const timeout = window.setTimeout(fail, 15000)
    script.addEventListener('load', () => {
      window.clearTimeout(timeout)
      if (window.turnstile) resolve()
      else fail()
    }, { once: true })
    script.addEventListener('error', () => { window.clearTimeout(timeout); fail() }, { once: true })
    if (!existing) {
      script.id = SCRIPT_ID
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
      script.async = true
      script.defer = true
      document.head.appendChild(script)
    }
  }).catch(error => { scriptPromise = undefined; throw error })
  return scriptPromise
}

export const TurnstileWidget = forwardRef<TurnstileHandle, TurnstileWidgetProps>(function TurnstileWidget({ action, onToken, className }, ref) {
  const { config } = useRuntime()
  const siteKey = config.turnstile_site_key
  const t = useI18n()
  const containerRef = useRef<HTMLDivElement>(null)
  const widgetIdRef = useRef<string | undefined>(undefined)
  const onTokenRef = useRef(onToken)
  const [scriptFailed, setScriptFailed] = useState(false)

  useEffect(() => { onTokenRef.current = onToken }, [onToken])
  useImperativeHandle(ref, () => ({
    reset() {
      onTokenRef.current('')
      if (widgetIdRef.current && window.turnstile) window.turnstile.reset(widgetIdRef.current)
    }
  }), [])

  useEffect(() => {
    onTokenRef.current('')
    setScriptFailed(false)
    if (!siteKey) return
    let cancelled = false
    loadTurnstileScript().then(() => {
      if (cancelled || !containerRef.current || !window.turnstile || widgetIdRef.current) return
      widgetIdRef.current = window.turnstile.render(containerRef.current, {
        sitekey: siteKey,
        action,
        callback: (token: string) => { setScriptFailed(false); onTokenRef.current(token || '') },
        'expired-callback': () => onTokenRef.current(''),
        'error-callback': () => { onTokenRef.current(''); setScriptFailed(true) }
      })
    }).catch(() => { if (!cancelled) setScriptFailed(true) })
    return () => {
      cancelled = true
      if (widgetIdRef.current && window.turnstile) {
        window.turnstile.remove(widgetIdRef.current)
        widgetIdRef.current = undefined
      }
    }
  }, [action, siteKey])

  if (!siteKey) return null
  return (
    <div className={className || 'turnstile-box'}>
      <div ref={containerRef} />
      {scriptFailed ? <small className="field-hint" role="alert">{t('turnstileLoadFailed')}</small> : null}
    </div>
  )
})
