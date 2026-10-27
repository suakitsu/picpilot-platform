'use client'

import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { type FormEvent, useEffect, useRef, useState } from 'react'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useI18n } from '@/components/i18n'
import { TurnstileWidget, type TurnstileHandle } from '@/components/TurnstileWidget'
import { useEmailCodeCooldown } from '@/hooks/useEmailCodeCooldown'
import { clearSession, confirmPasswordReset, sendEmailCode } from '@/lib/api'
import { validNewPassword } from '@/lib/password'
import { useRuntime } from '@/components/AppRuntime'

export default function ForgotPasswordPage() {
  const { registrationEnabled, config } = useRuntime()
  const router = useRouter()
  const t = useI18n()
  const [email, setEmail] = useState('')
  const [verificationCode, setVerificationCode] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [message, setMessage] = useState('')
  const [sendingCode, setSendingCode] = useState(false)
  const [loading, setLoading] = useState(false)
  const [resetComplete, setResetComplete] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [turnstileToken, setTurnstileToken] = useState('')
  const turnstileRef = useRef<TurnstileHandle>(null)
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())
  const codeValid = /^\d{6}$/.test(verificationCode.trim())
  const passwordValid = validNewPassword(password)
  const turnstileRequired = Boolean(config.turnstile_site_key)
  const turnstilePending = turnstileRequired && !turnstileToken
  const { cooldownActive, remainingSeconds, startCooldown } = useEmailCodeCooldown(email)

  useEffect(() => {
    if (!resetComplete) return
    const redirectTimer = window.setTimeout(() => router.replace('/login'), 1000)
    return () => window.clearTimeout(redirectTimer)
  }, [resetComplete, router])

  function requireTurnstile() {
    if (!turnstilePending) return true
    setMessage(t('authHumanCheck'))
    return false
  }

  function resetTurnstile() {
    setTurnstileToken('')
    turnstileRef.current?.reset()
  }

  async function sendCode() {
    if (!config.email_login_enabled) { setMessage(t('emailAuthUnavailable')); return }
    if (!emailValid) {
      setMessage(t('emailInvalid'))
      return
    }
    if (!requireTurnstile()) return
    if (sendingCode) return
    setSendingCode(true)
    setMessage('')
    try {
      const result = await sendEmailCode(email.trim(), 'reset_password', turnstileToken)
      startCooldown(result.cooldown_seconds)
      setMessage(t('forgotPasswordSubmitted'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('forgotPasswordFailed'))
    } finally {
      resetTurnstile()
      setSendingCode(false)
    }
  }

  async function submit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault()
    if (!config.email_login_enabled) { setMessage(t('emailAuthUnavailable')); return }
    if (!emailValid) {
      setMessage(t('emailInvalid'))
      return
    }
    if (!codeValid) {
      setMessage(t('codeInvalid'))
      return
    }
    if (!passwordValid) {
      setMessage(t('passwordRule'))
      return
    }
    if (password !== confirm) {
      setMessage(t('passwordsMismatch'))
      return
    }
    if (!requireTurnstile()) return
    if (loading || resetComplete) return
    setLoading(true)
    setMessage('')
    try {
      await confirmPasswordReset(email.trim(), verificationCode.trim(), password, confirm, turnstileToken)
      clearSession()
      setResetComplete(true)
      setMessage(t('resetPasswordSuccess'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('resetPasswordFailed'))
      resetTurnstile()
    } finally {
      setLoading(false)
    }
  }

  const sendCodeLabel = sendingCode
    ? t('codeSending')
    : cooldownActive
      ? t('codeResendCountdown').replace('{seconds}', String(remainingSeconds))
      : t('sendCode')

  return (
    <main className="auth-page">
      <div className="auth-tools"><LanguageMenu /><ThemeToggle /></div>
      <section className="auth-card">
        <div className="auth-brand"><span className="brand-mark">PP</span><div><h1>{t('forgotPasswordTitle')}</h1><p>{t('forgotPasswordDesc')}</p></div></div>
        <form className="form-grid" onSubmit={submit} noValidate>
          {!config.email_login_enabled ? <p role="status">{t('emailAuthUnavailable')}</p> : null}
          <label className="field"><span>{t('emailLabel')}</span><div className="password-field"><input className="input" type="email" autoComplete="email" inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false} required value={email} onChange={event => setEmail(event.target.value)} placeholder="you@example.com" /><button type="button" className="btn btn-compact" disabled={!config.email_login_enabled || !emailValid || sendingCode || cooldownActive || turnstilePending} onClick={sendCode}>{sendCodeLabel}</button></div></label>
          <label className="field"><span>{t('verificationCode')}</span><input className="input" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={verificationCode} onChange={event => setVerificationCode(event.target.value.replace(/\D/g, '').slice(0, 6))} placeholder={t('codeInputPlaceholder')} /></label>
          <label className="field" htmlFor="reset-password"><span>{t('newPassword')}</span><div className="password-field"><input id="reset-password" aria-label={t('newPassword')} className="input" type={showPassword ? 'text' : 'password'} autoComplete="new-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} required value={password} onChange={event => setPassword(event.target.value)} placeholder={t('passwordRule')} /><button type="button" className="btn btn-compact" aria-label={showPassword ? t('hidePassword') : t('showPassword')} onClick={() => setShowPassword(value => !value)}>{showPassword ? t('hidePassword') : t('showPassword')}</button></div><small className="field-hint">{t('passwordRule')}</small></label>
          <label className="field"><span>{t('confirmNewPassword')}</span><input className="input" type={showPassword ? 'text' : 'password'} autoComplete="new-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} required value={confirm} onChange={event => setConfirm(event.target.value)} placeholder={t('passwordAgain')} /></label>
          <TurnstileWidget ref={turnstileRef} action={codeValid ? 'reset-password' : 'send-reset-code'} onToken={setTurnstileToken} />
          <div className="notice">{t('forgotPasswordGuide')}</div>
          {message ? <div className={message === t('forgotPasswordSubmitted') || message === t('resetPasswordSuccess') ? 'notice' : 'form-error'}>{message}</div> : null}
          <button type="submit" className="btn btn-primary btn-block" disabled={!config.email_login_enabled || !email || !verificationCode || !password || !confirm || loading || resetComplete || turnstilePending}>{loading ? t('loggingIn') : resetComplete ? t('resetPasswordSuccess') : t('resetPasswordSubmit')}</button>
        </form>
        <div className="auth-footer auth-link-panel">
          <div className="auth-footer-links">
            <Link href="/login">{t('backLogin')}</Link>
            {registrationEnabled ? <Link href="/register">{t('createNow')}</Link> : null}
            <Link href="/">{t('backHome')}</Link>
          </div>
        </div>
      </section>
    </main>
  )
}
