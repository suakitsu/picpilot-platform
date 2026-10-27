'use client'

import { useState, type FormEvent } from 'react'
import { KeyRound } from 'lucide-react'
import { ApiError, changePassword } from '@/lib/api'
import { validNewPassword } from '@/lib/password'
import { useI18n } from './i18n'

export function PasswordChangeForm() {
  const t = useI18n()
  const [current, setCurrent] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setError('')
    setSaved(false)
    if (!validNewPassword(password)) { setError(t('passwordRule')); return }
    if (password !== confirmation) { setError(t('setupMismatch')); return }
    setBusy(true)
    try {
      const result = await changePassword(current, password, confirmation)
      setCurrent(''); setPassword(''); setConfirmation('')
      if (result.requires_login) window.location.replace('/login?password_changed=1')
      else setSaved(true)
    } catch (cause) {
      let message = cause instanceof Error ? cause.message : t('saveFailed')
      for (const secret of [current, password, confirmation]) if (secret) message = message.split(secret).join('[redacted]')
      setError(message + (cause instanceof ApiError ? ' [' + cause.code + ']' + (cause.requestId ? ' / ' + cause.requestId : '') : ''))
    } finally { setBusy(false) }
  }

  return <section className="password-settings">
    <h2>{t('changePasswordTitle')}</h2>
    <p className="muted">{t('changePasswordNotice')}</p>
    <form onSubmit={submit} aria-busy={busy}>
      <fieldset className="provider-fields" disabled={busy}>
        <label className="field"><span>{t('currentPassword')}</span><input className="input" type="password" required autoComplete="current-password" value={current} onChange={e => setCurrent(e.target.value)} /></label>
        <label className="field"><span>{t('newPassword')}</span><input className="input" type="password" required autoComplete="new-password" value={password} onChange={e => setPassword(e.target.value)} /><small>{t('passwordRule')}</small></label>
        <label className="field"><span>{t('setupConfirmPassword')}</span><input className="input" type="password" required autoComplete="new-password" value={confirmation} onChange={e => setConfirmation(e.target.value)} /></label>
        {error ? <p className="provider-error provider-wide" role="alert">{error}</p> : null}
        {saved ? <p role="status">{t('passwordChanged')}</p> : null}
        <div className="provider-wide"><button className="btn btn-primary" type="submit"><KeyRound size={16} />{t(busy ? 'saving' : 'changePasswordTitle')}</button></div>
      </fieldset>
    </form>
  </section>
}
