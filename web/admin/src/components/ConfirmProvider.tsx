'use client'

import { createContext, ReactNode, useCallback, useContext, useEffect, useState } from 'react'
import { AlertTriangle, CircleHelp, X } from 'lucide-react'
import { useI18n } from '@/components/i18n'

type ConfirmTone = 'default' | 'danger'

interface ConfirmOptions {
  title?: string
  message: string
  confirmText?: string
  cancelText?: string
  tone?: ConfirmTone
}

interface PendingConfirm extends ConfirmOptions {
  resolve: (value: boolean) => void
}

const ConfirmContext = createContext<(options: ConfirmOptions | string) => Promise<boolean>>(async () => false)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const t = useI18n()
  const [pending, setPending] = useState<PendingConfirm | null>(null)

  function confirm(options: ConfirmOptions | string) {
    return new Promise<boolean>(resolve => {
      setPending({
        message: typeof options === 'string' ? options : options.message,
        title: typeof options === 'string' ? t('confirm.defaultTitle') : options.title,
        confirmText: typeof options === 'string' ? t('confirm.confirm') : options.confirmText,
        cancelText: typeof options === 'string' ? t('confirm.cancel') : options.cancelText,
        tone: typeof options === 'string' ? 'default' : options.tone,
        resolve
      })
    })
  }

  const close = useCallback((value: boolean) => {
    setPending(current => {
      if (!current) return current
      current.resolve(value)
      return null
    })
  }, [])

  useEffect(() => {
    if (!pending) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [pending, close])

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending ? (
        <div className="confirm-backdrop" role="presentation" onMouseDown={() => close(false)}>
          <section className={`confirm-dialog ${pending.tone === 'danger' ? 'confirm-dialog-danger' : ''}`} role="dialog" aria-modal="true" aria-labelledby="confirm-title" onMouseDown={event => event.stopPropagation()}>
            <button className="confirm-close" type="button" aria-label={t('confirm.close')} onClick={() => close(false)}>
              <X size={18} strokeWidth={2.4} />
            </button>
            <div className={`confirm-icon ${pending.tone === 'danger' ? 'confirm-icon-danger' : ''}`} aria-hidden="true">
              {pending.tone === 'danger' ? <AlertTriangle size={24} strokeWidth={2.4} /> : <CircleHelp size={24} strokeWidth={2.4} />}
            </div>
            <div className="confirm-copy">
              <span className="confirm-kicker">{pending.tone === 'danger' ? t('confirm.dangerKicker') : t('confirm.defaultKicker')}</span>
              <h2 id="confirm-title">{pending.title || t('confirm.defaultTitle')}</h2>
              <p>{pending.message}</p>
            </div>
            <div className="confirm-actions">
              <button className="btn" type="button" onClick={() => close(false)}>{pending.cancelText || t('confirm.cancel')}</button>
              <button className={`btn ${pending.tone === 'danger' ? 'btn-danger' : 'btn-primary'}`} type="button" onClick={() => close(true)} autoFocus>{pending.confirmText || t('confirm.confirm')}</button>
            </div>
          </section>
        </div>
      ) : null}
    </ConfirmContext.Provider>
  )
}

export function useConfirm() {
  return useContext(ConfirmContext)
}
