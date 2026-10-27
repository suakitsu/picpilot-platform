'use client'

import Link from 'next/link'
import { useSearchParams } from 'next/navigation'
import {
  ArrowRight,
  CheckCircle2,
  Eye,
  EyeOff,
  History,
  ImageIcon,
  Languages,
  LockKeyhole,
  Mail,
  ShieldCheck
} from 'lucide-react'
import { type FormEvent, useEffect, useRef, useState } from 'react'
import { LanguageMenu } from '@/components/LanguageMenu'
import { BrandLogo } from '@/components/BrandLogo'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useI18n } from '@/components/i18n'
import { useRuntime } from '@/components/AppRuntime'
import { validNewPassword } from '@/lib/password'
import { TurnstileWidget, type TurnstileHandle } from '@/components/TurnstileWidget'
import { safeNextPath } from '@/lib/navigation'
import { useEmailCodeCooldown } from '@/hooks/useEmailCodeCooldown'
import { githubLoginUrl, githubStatus, login, loginWithEmailCode, register, saveSession, sendEmailCode } from '@/lib/api'

export type AuthView = 'login' | 'register'
type LoginMode = 'password' | 'code'
type AuthMessage = { tone: 'error' | 'success'; text: string }

interface AuthExperienceProps {
  initialView: AuthView
}

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/


function GithubMark() {
  return (
    <svg className="auth-github-mark" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.44 9.8 8.2 11.39.6.11.82-.26.82-.58 0-.29-.01-1.04-.02-2.04-3.34.73-4.04-1.61-4.04-1.61-.55-1.4-1.34-1.77-1.34-1.77-1.09-.74.08-.73.08-.73 1.2.08 1.84 1.23 1.84 1.23 1.07 1.84 2.81 1.31 3.5 1 .11-.78.42-1.31.76-1.61-2.67-.3-5.47-1.34-5.47-5.93 0-1.31.47-2.38 1.23-3.22-.12-.3-.53-1.52.12-3.18 0 0 1-.02 3.3 1.23.96-.27 1.98-.4 3-.41 1.02 0 2.04.14 3 .41 2.29-1.56 3.29-1.23 3.29-1.23.65 1.66.24 2.88.12 3.18.77.84 1.23 1.91 1.23 3.22 0 4.61-2.81 5.62-5.49 5.92.43.37.81 1.1.81 2.22 0 1.61-.01 2.9-.01 3.29 0 .32.22.7.83.58A12 12 0 0 0 24 12C24 5.37 18.63 0 12 0Z"
      />
    </svg>
  )
}

export function AuthExperience({ initialView }: AuthExperienceProps) {
  const { registrationEnabled, config } = useRuntime()
  const searchParams = useSearchParams()
  const t = useI18n()
  const [view, setView] = useState<AuthView>(initialView)
  const [loginMode, setLoginMode] = useState<LoginMode>('password')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [verificationCode, setVerificationCode] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [rememberMe, setRememberMe] = useState(false)
  const [message, setMessage] = useState<AuthMessage | null>(null)
  const [loading, setLoading] = useState(false)
  const [sendingCode, setSendingCode] = useState(false)
  const [githubEnabled, setGithubEnabled] = useState(false)
  const [turnstileToken, setTurnstileToken] = useState('')
  const stageRef = useRef<HTMLDivElement | null>(null)
  const switchingRef = useRef(false)
  const turnstileRef = useRef<TurnstileHandle>(null)

  const emailValid = emailPattern.test(email.trim())
  const passwordValid = validNewPassword(password)
  const codeValid = /^\d{6}$/.test(verificationCode.trim())
  const passwordsMatch = password === confirmPassword
  const turnstilePending = Boolean(config.turnstile_site_key) && !turnstileToken
  const emailLoginEnabled = config.email_login_enabled
  const { cooldownActive, remainingSeconds, startCooldown } = useEmailCodeCooldown(email)

  useEffect(() => {
    if (config.edition !== 'platform') return
    githubStatus().then(data => setGithubEnabled(data.enabled)).catch(() => setGithubEnabled(false))
  }, [config.edition])

  useEffect(() => {
    document.title = `${view === 'login' ? t('login') : t('register')} | PicPilot`
  }, [t, view])

  function resetTurnstile() {
    setTurnstileToken('')
    turnstileRef.current?.reset()
  }

  function requireTurnstile() {
    if (!turnstilePending) return true
    setMessage({ tone: 'error', text: t('authHumanCheck') })
    return false
  }

  async function switchView(next: AuthView) {
    if (next === 'register' && !registrationEnabled) return
    if (next === view || switchingRef.current) return
    switchingRef.current = true
    const stage = stageRef.current
    const direction = next === 'register' ? 1 : -1
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    if (stage && !reducedMotion) {
      await stage.animate(
        [
          { opacity: 1, transform: 'translateX(0)' },
          { opacity: 0, transform: `translateX(${direction * -14}px)` }
        ],
        { duration: 150, easing: 'cubic-bezier(.4, 0, 1, 1)', fill: 'forwards' }
      ).finished.catch(() => undefined)
    }

    setView(next)
    setMessage(null)
    resetTurnstile()
    const nextUrl = next === 'login'
      ? `/login${window.location.search}`
      : '/register'
    window.history.replaceState(window.history.state, '', nextUrl)

    await new Promise<void>(resolve => window.requestAnimationFrame(() => resolve()))
    stage?.getAnimations().forEach(animation => animation.cancel())

    if (stage && !reducedMotion) {
      await stage.animate(
        [
          { opacity: 0, transform: `translateX(${direction * 18}px)` },
          { opacity: 1, transform: 'translateX(0)' }
        ],
        { duration: 260, easing: 'cubic-bezier(.22, .8, .3, 1)' }
      ).finished.catch(() => undefined)
    }

    switchingRef.current = false
  }

  function loginWithGithub() {
    window.location.href = githubLoginUrl(safeNextPath(searchParams.get('next')))
  }

  function registerWithGithub() {
    window.location.href = githubLoginUrl()
  }

  async function sendCode(purpose: 'login' | 'register') {
    if (!emailValid) {
      setMessage({ tone: 'error', text: t('emailInvalid') })
      return
    }
    if (!requireTurnstile() || sendingCode) return
    setSendingCode(true)
    setMessage(null)
    try {
      const result = await sendEmailCode(email.trim(), purpose, turnstileToken)
      startCooldown(result.cooldown_seconds)
      if (purpose === 'login') setLoginMode('code')
      setMessage({ tone: 'success', text: t('codeSent') })
    } catch (error) {
      setMessage({
        tone: 'error',
        text: error instanceof Error ? error.message : t(purpose === 'login' ? 'loginFailed' : 'registerFailed')
      })
    } finally {
      resetTurnstile()
      setSendingCode(false)
    }
  }

  async function submitLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!emailValid) {
      setMessage({ tone: 'error', text: t('emailInvalid') })
      return
    }
    if (loginMode === 'code' && !codeValid) {
      setMessage({ tone: 'error', text: t('codeInvalid') })
      return
    }
    if (!requireTurnstile() || loading) return
    setLoading(true)
    setMessage(null)
    try {
      const auth = loginMode === 'code'
        ? await loginWithEmailCode(email.trim(), verificationCode.trim(), rememberMe, turnstileToken)
        : await login(email.trim(), password, rememberMe, turnstileToken)
      saveSession(auth)
      window.location.href = safeNextPath(searchParams.get('next'))
    } catch (error) {
      setMessage({ tone: 'error', text: error instanceof Error ? error.message : t('loginFailed') })
      resetTurnstile()
    } finally {
      setLoading(false)
    }
  }

  async function submitRegister(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!emailValid) {
      setMessage({ tone: 'error', text: t('emailInvalid') })
      return
    }
    if (!codeValid) {
      setMessage({ tone: 'error', text: t('codeInvalid') })
      return
    }
    if (!passwordValid) {
      setMessage({ tone: 'error', text: t('passwordRule') })
      return
    }
    if (!passwordsMatch) {
      setMessage({ tone: 'error', text: t('passwordsMismatch') })
      return
    }
    if (!requireTurnstile() || loading) return
    setLoading(true)
    setMessage(null)
    try {
      const auth = await register(email.trim(), password, confirmPassword, verificationCode.trim(), turnstileToken)
      saveSession(auth)
      window.location.replace('/workspace')
    } catch (error) {
      setMessage({ tone: 'error', text: error instanceof Error ? error.message : t('registerFailed') })
      resetTurnstile()
    } finally {
      setLoading(false)
    }
  }

  const passwordToggleLabel = showPassword ? t('hidePassword') : t('showPassword')
  const sendCodeLabel = sendingCode
    ? t('codeSending')
    : cooldownActive
      ? t('codeResendCountdown').replace('{seconds}', String(remainingSeconds))
      : t('sendCode')

  return (
    <div className="auth-experience-page">
      <header className="auth-experience-header">
        <Link className="auth-experience-brand" href="/" aria-label={t('homeAria')}>
          <BrandLogo className="picpilot-brand-auth" subtitle={t('authBrandSubtitle')} />
        </Link>
        <div className="auth-experience-tools">
          <LanguageMenu />
          <ThemeToggle />
        </div>
      </header>

      <main className="auth-workspace">
        <aside className="auth-visual" aria-label={t('authVisualAria')}>
          <img className="auth-visual-image" src="/auth-visual-commerce-v3.png" alt={t('authVisualAria')} />
          <div className="auth-visual-copy">
            <div>
              <h2>{t('authVisualTitle')}</h2>
              <p>{t('authVisualDesc')}</p>
            </div>
            <ul className="auth-capability-list">
              <li><ImageIcon aria-hidden="true" /><span>{t('authCapabilityGenerate')}</span></li>
              <li><Languages aria-hidden="true" /><span>{t('authCapabilityLocalize')}</span></li>
              <li><History aria-hidden="true" /><span>{t('authCapabilityHistory')}</span></li>
            </ul>
          </div>
        </aside>

        <section className="auth-form-panel">
          <div className="auth-form-inner">
            <nav className="auth-account-tabs" aria-label={t('authAccountNav')}>
              <button type="button" className={view === 'login' ? 'active' : ''} onClick={() => switchView('login')}>{t('login')}</button>
              {registrationEnabled ? <button type="button" className={view === 'register' ? 'active' : ''} onClick={() => switchView('register')}>{t('register')}</button> : null}
            </nav>

            <div className="auth-form-stage" ref={stageRef}>
              {view === 'login' ? (
                <>
                  <div className="auth-form-heading">
                    <h1>{t('authWelcomeTitle')}</h1>
                    <p>{t('authWelcomeDesc')}</p>
                  </div>

                  {emailLoginEnabled ? <div className="auth-method-switch" role="group" aria-label={t('authLoginMethod')}>
                    <button type="button" className={loginMode === 'password' ? 'active' : ''} onClick={() => { setLoginMode('password'); setMessage(null) }}>{t('loginWithPassword')}</button>
                    <button type="button" className={loginMode === 'code' ? 'active' : ''} onClick={() => { setLoginMode('code'); setMessage(null) }}>{t('loginWithCode')}</button>
                  </div> : null}

                  <form className="auth-experience-form" onSubmit={submitLogin} noValidate>
                    <label className="auth-field">
                      <span>{t('emailLabel')}</span>
                      <span className="auth-input-shell">
                        <Mail aria-hidden="true" />
                        <input className="auth-input" type="email" autoComplete="email" inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={email} onChange={event => setEmail(event.target.value)} placeholder="you@example.com" />
                      </span>
                    </label>

                    {loginMode === 'password' ? (
                      <label className="auth-field">
                        <span className="auth-field-title">
                          <span>{t('passwordLabel')}</span>
                          {emailLoginEnabled ? <Link href="/forgot-password">{t('forgotPassword')}</Link> : null}
                        </span>
                        <span className="auth-input-shell">
                          <LockKeyhole aria-hidden="true" />
                          <input className="auth-input" type={showPassword ? 'text' : 'password'} autoComplete="current-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={password} onChange={event => setPassword(event.target.value)} placeholder={t('passwordPlaceholder')} />
                          <button className="auth-input-icon-button" type="button" onClick={() => setShowPassword(value => !value)} aria-label={passwordToggleLabel} title={passwordToggleLabel}>
                            {showPassword ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
                          </button>
                        </span>
                      </label>
                    ) : (
                      <label className="auth-field">
                        <span>{t('verificationCode')}</span>
                        <span className="auth-input-shell auth-code-shell">
                          <ShieldCheck aria-hidden="true" />
                          <input className="auth-input" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={verificationCode} onChange={event => setVerificationCode(event.target.value.replace(/\D/g, '').slice(0, 6))} placeholder={t('codeInputPlaceholder')} />
                          <button className="auth-code-button" type="button" disabled={!emailLoginEnabled || !emailValid || sendingCode || cooldownActive || turnstilePending} onClick={() => sendCode('login')}>{sendCodeLabel}</button>
                        </span>
                      </label>
                    )}

                    <label className="auth-checkbox-row">
                      <input type="checkbox" checked={rememberMe} onChange={event => setRememberMe(event.target.checked)} />
                      <span>{t('authRememberShort')}</span>
                    </label>

                    <TurnstileWidget key={`login-${loginMode}`} ref={turnstileRef} action="user-login" onToken={setTurnstileToken} />
                    {searchParams.get('password_changed') === '1' ? <p role="status">{t('passwordChanged')}</p> : null}
                    {message ? <div className={`auth-status auth-status-${message.tone}`}>{message.tone === 'success' ? <CheckCircle2 aria-hidden="true" /> : null}<span>{message.text}</span></div> : null}

                    <button className="auth-primary-button" type="submit" disabled={!emailValid || loading || turnstilePending || (loginMode === 'password' ? !password : !codeValid)}>
                      <span>{loading ? t('loggingIn') : t('login')}</span><ArrowRight aria-hidden="true" />
                    </button>

                    {githubEnabled ? (
                      <>
                        <div className="auth-divider"><span>{t('authOtherMethods')}</span></div>
                        <button className="auth-github-button" type="button" onClick={loginWithGithub}><GithubMark /><span>{t('authGithubLogin')}</span></button>
                      </>
                    ) : null}
                  </form>

                  {registrationEnabled ? <p className="auth-switch-prompt">{t('noAccount')} <button type="button" onClick={() => switchView('register')}>{t('createNow')}</button></p> : null}
                </>
              ) : (
                <>
                  <div className="auth-form-heading auth-register-heading">
                    <h1>{t('authCreateTitle')}</h1>
                    <p>{t('authCreateDesc')}</p>
                  </div>

                  <form className="auth-experience-form auth-register-form" onSubmit={submitRegister} noValidate>
                    {!emailLoginEnabled ? <p role="status">{t('emailAuthUnavailable')}</p> : null}
                    <label className="auth-field">
                      <span>{t('emailLabel')}</span>
                      <span className="auth-input-shell">
                        <Mail aria-hidden="true" />
                        <input className="auth-input" type="email" autoComplete="email" inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={email} onChange={event => setEmail(event.target.value)} placeholder="you@example.com" />
                      </span>
                    </label>

                    <label className="auth-field">
                      <span>{t('verificationCode')}</span>
                      <span className="auth-input-shell auth-code-shell">
                        <ShieldCheck aria-hidden="true" />
                        <input className="auth-input" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={verificationCode} onChange={event => setVerificationCode(event.target.value.replace(/\D/g, '').slice(0, 6))} placeholder={t('codeInputPlaceholder')} />
                        <button className="auth-code-button" type="button" disabled={!emailLoginEnabled || !emailValid || sendingCode || cooldownActive || turnstilePending} onClick={() => sendCode('register')}>{sendCodeLabel}</button>
                      </span>
                    </label>

                    <label className="auth-field">
                      <span>{t('passwordLabel')}</span>
                      <span className="auth-input-shell">
                        <LockKeyhole aria-hidden="true" />
                        <input className="auth-input" type={showPassword ? 'text' : 'password'} autoComplete="new-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={password} onChange={event => setPassword(event.target.value)} placeholder={t('authPasswordPlaceholder')} />
                        <button className="auth-input-icon-button" type="button" onClick={() => setShowPassword(value => !value)} aria-label={passwordToggleLabel} title={passwordToggleLabel}>
                          {showPassword ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
                        </button>
                      </span>
                      {password && !passwordValid ? <small className="auth-field-hint">{t('passwordRule')}</small> : null}
                    </label>

                    <label className="auth-field">
                      <span>{t('confirmPassword')}</span>
                      <span className="auth-input-shell">
                        <LockKeyhole aria-hidden="true" />
                        <input className="auth-input" type={showPassword ? 'text' : 'password'} autoComplete="new-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={confirmPassword} onChange={event => setConfirmPassword(event.target.value)} placeholder={t('authConfirmPlaceholder')} />
                      </span>
                    </label>

                    <TurnstileWidget key="register" ref={turnstileRef} action="user-register" onToken={setTurnstileToken} />
                    {message ? <div className={`auth-status auth-status-${message.tone}`}>{message.tone === 'success' ? <CheckCircle2 aria-hidden="true" /> : null}<span>{message.text}</span></div> : null}

                    <button className="auth-primary-button" type="submit" disabled={!emailLoginEnabled || !emailValid || !codeValid || !passwordValid || !passwordsMatch || loading || turnstilePending}>
                      <span>{loading ? t('registering') : t('authCreateAccount')}</span><ArrowRight aria-hidden="true" />
                    </button>

                    {githubEnabled ? (
                      <>
                        <div className="auth-divider"><span>{t('authOtherMethods')}</span></div>
                        <button className="auth-github-button" type="button" onClick={registerWithGithub}><GithubMark /><span>{t('authGithubRegister')}</span></button>
                      </>
                    ) : null}
                  </form>

                  <p className="auth-switch-prompt">{t('haveAccount')} <button type="button" onClick={() => switchView('login')}>{t('authBackToLogin')}</button></p>
                </>
              )}
            </div>
          </div>
        </section>
      </main>
    </div>
  )
}
