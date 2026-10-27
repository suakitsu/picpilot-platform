'use client'

import { useState, type FormEvent } from 'react'
import { KeyRound, ShieldCheck } from 'lucide-react'
import { BrandLogo } from '@/components/BrandLogo'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useI18n } from '@/components/i18n'
import { useRuntime } from '@/components/AppRuntime'
import { completeSetup, loadRuntime, RuntimeError } from '@/lib/runtime'
import { validNewPassword } from '@/lib/password'

export default function SetupPage() {
  const t = useI18n()
  const { reload } = useRuntime()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [setupToken, setSetupToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    if (!validNewPassword(password)) { setError(t('passwordRule')); return }
    if (password !== confirmPassword) { setError(t('setupMismatch')); return }
    setBusy(true)
    setError('')
    try {
      await completeSetup({ email: email.trim(), password, confirm_password: confirmPassword, setup_token: setupToken })
      setPassword('')
      setConfirmPassword('')
      setSetupToken('')
      const state = await loadRuntime()
      if (state.needsSetup) { setError(t('setupUnconfirmed')); return }
      await reload()
    } catch (cause) {
      setError(cause instanceof RuntimeError ? cause.message + ' [' + cause.code + ']' + (cause.requestId ? ' / ' + cause.requestId : '') : t('setupFailed'))
    } finally { setBusy(false) }
  }

  return <main className="runtime-page">
    <section className="setup-form">
      <div className="setup-tools"><BrandLogo /><LanguageMenu /><ThemeToggle /></div>
      <h1><ShieldCheck size={24} />{t('setupTitle')}</h1>
      <p className="muted">{t('setupDescription')}</p>
      <form className="form-grid" onSubmit={submit} aria-busy={busy}>
        <fieldset disabled={busy} className="setup-fields">
          <label className="field"><span>{t('setupEmail')}</span><input className="input" type="email" name="email" autoComplete="username" required value={email} onChange={e => setEmail(e.target.value)} /></label>
          <label className="field"><span>{t('setupPassword')}</span><input className="input" type="password" name="password" autoComplete="new-password" required value={password} onChange={e => setPassword(e.target.value)} /></label>
          <label className="field"><span>{t('setupConfirmPassword')}</span><input className="input" type="password" name="confirm_password" autoComplete="new-password" required value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} /></label>
          <label className="field"><span>{t('setupToken')}</span><input className="input" type="password" name="setup_token" autoComplete="off" required value={setupToken} onChange={e => setSetupToken(e.target.value)} /></label>
          {error ? <div className="notice notice-error" role="alert">{error}</div> : null}
          <button className="btn btn-primary" type="submit"><KeyRound size={16} />{t(busy ? 'setupBusy' : 'setupSubmit')}</button>
        </fieldset>
      </form>
    </section>
  </main>
}

