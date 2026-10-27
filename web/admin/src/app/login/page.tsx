'use client'

import { useSearchParams } from 'next/navigation'
import { Suspense, type FormEvent, useRef, useState } from 'react'
import { Activity, ArrowRight, Database, Eye, EyeOff, LoaderCircle, LockKeyhole, ShieldCheck, UserRound } from 'lucide-react'
import { adminLogin, saveSession } from '@/lib/api'
import { useI18n } from '@/components/i18n'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { BrandLogo } from '@/components/BrandLogo'
import { useRuntime } from '@/components/AppRuntime'
import { TurnstileWidget, type TurnstileHandle } from '@/components/TurnstileWidget'
import { safeNextPath } from '@/lib/navigation'

function AdminLoginForm() {
  const searchParams = useSearchParams()
  const t = useI18n()
  const { config } = useRuntime()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [turnstileToken, setTurnstileToken] = useState('')
  const turnstileRef = useRef<TurnstileHandle>(null)
  const turnstilePending = Boolean(config.turnstile_site_key) && !turnstileToken

  async function submit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault()
    if (!email || !password || loading) return
    if (turnstilePending) { setMessage(t('authHumanCheck')); return }
    setLoading(true)
    setMessage('')
    try {
      const auth = await adminLogin(email, password, turnstileToken)
      saveSession(auth)
      window.location.href = safeNextPath(searchParams.get('next'), '/')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('adminLoginFailed'))
      setTurnstileToken('')
      turnstileRef.current?.reset()
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="auth-page admin-login-page">
      <div className="auth-tools"><LanguageMenu /><ThemeToggle /></div>
      <section className="admin-login-shell">
        <aside className="admin-login-aside">
          <BrandLogo className="picpilot-brand-admin-login" subtitle="Admin Console" />
          <div className="admin-login-intro">
            <span className="admin-login-kicker"><ShieldCheck size={15} />受保护的运营入口</span>
            <h1>把运营、财务和生成任务放在一个清晰工作台里。</h1>
            <p>仅供 PicPilot 授权管理员使用。登录状态、敏感操作与配置变更均保留审计记录。</p>
          </div>
          <div className="admin-login-status-list" aria-label="控制台能力">
            <div><span><Activity size={17} /></span><strong>业务状态</strong><small>任务、队列与失败率集中查看</small></div>
            <div><span><Database size={17} /></span><strong>财务追踪</strong><small>充值流水、积分和成本可追溯</small></div>
            <div><span><LockKeyhole size={17} /></span><strong>权限隔离</strong><small>管理员会话与用户端完全分离</small></div>
          </div>
          <div className="admin-login-environment"><i aria-hidden="true" /><span>PicPilot Admin</span><b>Secure access</b></div>
        </aside>

        <section className="admin-login-panel">
          <div className="admin-login-panel-head">
            <span>ADMIN SIGN IN</span>
            <h2>{t('adminLoginTitle')}</h2>
            <p>{t('adminLoginDesc')}</p>
          </div>
          <form className="form-grid admin-login-form" onSubmit={submit} aria-busy={loading}>
            <label className="field">
              <span>{t('adminAccount')}</span>
              <div className="admin-login-input-wrap"><UserRound size={17} /><input className="input" autoComplete="username" inputMode="email" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={email} onChange={event => setEmail(event.target.value)} placeholder="admin@example.com" /></div>
            </label>
            <label className="field" htmlFor="admin-login-password">
              <span>{t('password')}</span>
              <div className="admin-login-input-wrap"><LockKeyhole size={17} /><input id="admin-login-password" aria-label={t('password')} className="input" type={showPassword ? 'text' : 'password'} autoComplete="current-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={password} onChange={event => setPassword(event.target.value)} placeholder={t('passwordPlaceholder')} /><button type="button" className="icon-button admin-password-toggle" aria-label={showPassword ? t('hidePassword') : t('showPassword')} title={showPassword ? t('hidePassword') : t('showPassword')} onClick={() => setShowPassword(value => !value)}>{showPassword ? <EyeOff size={17} /> : <Eye size={17} />}</button></div>
            </label>
            <TurnstileWidget ref={turnstileRef} action="admin-login" onToken={setTurnstileToken} />
            {searchParams.get('password_changed') === '1' ? <p role="status">{t('passwordChanged')}</p> : null}
            {message ? <div className="form-error" role="alert">{message}</div> : null}
            <button type="submit" className="btn btn-primary btn-block admin-login-submit" disabled={!email || !password || loading || turnstilePending}>{loading ? <><span className="spin"><LoaderCircle size={17} /></span>{t('loggingIn')}</> : <>{t('loginAdmin')}<ArrowRight size={17} /></>}</button>
          </form>
          <div className="admin-login-foot"><ShieldCheck size={14} /><span>{t('adminAccountHint')}</span></div>
        </section>
      </section>
    </main>
  )
}

export default function AdminLoginPage() {
  const t = useI18n()
  return (
    <Suspense fallback={<main className="auth-page admin-login-page"><section className="auth-card">{t('openingLogin')}</section></main>}>
      <AdminLoginForm />
    </Suspense>
  )
}
