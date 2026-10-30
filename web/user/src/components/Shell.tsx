'use client'

import Link from 'next/link'
import { ReactNode, useCallback, useEffect, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import {
  ArrowUp,
  Bell,
  Clapperboard,
  CreditCard,
  History,
  Images,
  LayoutDashboard,
  LayoutTemplate,
  MessageCircle,
  Settings2,
  UserRound,
  Users,
  WandSparkles,
  Workflow
} from 'lucide-react'
import { AccountMenu } from '@/components/AccountMenu'
import { BrandLogo } from '@/components/BrandLogo'
import { useRuntime } from '@/components/AppRuntime'
import { GlobalAssistant } from '@/components/GlobalAssistant'
import { useI18n } from '@/components/i18n'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { assetUrl, BenefitConfig, getBenefitConfig, getBillingSummary, getStoredUser, getToken, listMessages, saveStoredUser } from '@/lib/api'

interface ShellProps {
  children: ReactNode
  chatSurface?: boolean
  workspaceSurface?: boolean
}

const canvasLabEnabled = process.env.NEXT_PUBLIC_CANVAS_LAB_ENABLED !== 'false'

const primaryNav = [
  { label: 'workspace', href: '/workspace', Icon: LayoutDashboard },
  { label: 'templates', href: '/templates', Icon: LayoutTemplate },
  { label: 'commerce', href: '/commerce', Icon: Images },
  { label: 'productVideo', href: '/video-create', Icon: Clapperboard },
  { label: 'aiCreate', href: '/ai-create', Icon: WandSparkles },
  ...(canvasLabEnabled ? [{ label: 'workflows', href: '/workflows', Icon: Workflow }] : []),
  { label: 'history', href: '/history', Icon: History }
]

const mobileNav = primaryNav.filter(item => item.label !== 'templates')

export function Shell({ children, chatSurface = false, workspaceSurface = false }: ShellProps) {
  const { config, billingEnabled } = useRuntime()
  const platform = config.edition === 'platform'
  const pathname = usePathname()
  const router = useRouter()
  const [credits, setCredits] = useState<number | null>(null)
  const [hasUnreadMessages, setHasUnreadMessages] = useState(false)
  const [benefitConfig, setBenefitConfig] = useState<BenefitConfig | null>(null)
  const [showBackToTop, setShowBackToTop] = useState(false)
  const [routePending, setRoutePending] = useState(false)
  const t = useI18n()

  const prefetchRoute = useCallback((href: string) => {
    if (typeof window !== 'undefined') void router.prefetch(href)
  }, [router])

  useEffect(() => {
    if (!billingEnabled) return
    const refreshCredits = () => {
      if (!getToken()) {
        setCredits(null)
        return
      }
      getBillingSummary({ passive: true }).then(data => {
      setCredits(data.credits)
      const storedUser = getStoredUser()
      if (storedUser && storedUser.credits !== data.credits) {
        saveStoredUser({ ...storedUser, credits: data.credits })
      }
      }).catch(() => setCredits(null))
    }
    refreshCredits()
    window.addEventListener('openimage:credits-updated', refreshCredits)
    return () => window.removeEventListener('openimage:credits-updated', refreshCredits)
  }, [billingEnabled])

  useEffect(() => {
    if (!billingEnabled) return
    getBenefitConfig().then(setBenefitConfig).catch(() => setBenefitConfig(null))
  }, [billingEnabled])

  useEffect(() => {
    if (!platform) return
    const refreshMessages = () => {
      if (!getToken()) {
        setHasUnreadMessages(false)
        return
      }
      listMessages({ passive: true }).then(rows => setHasUnreadMessages(rows.some(row => !row.read))).catch(() => setHasUnreadMessages(false))
    }
    refreshMessages()
    window.addEventListener('openimage:messages-updated', refreshMessages)
    return () => window.removeEventListener('openimage:messages-updated', refreshMessages)
  }, [platform])

  useEffect(() => {
    const updateBackToTop = () => setShowBackToTop(window.scrollY > 480)
    updateBackToTop()
    window.addEventListener('scroll', updateBackToTop, { passive: true })
    return () => window.removeEventListener('scroll', updateBackToTop)
  }, [])

  useEffect(() => {
    const routeResetTimer = window.setTimeout(() => setRoutePending(false), 180)
    let resetTimer: number | null = null
    const startRouteTransition = () => {
      setRoutePending(true)
      if (resetTimer !== null) window.clearTimeout(resetTimer)
      resetTimer = window.setTimeout(() => setRoutePending(false), 4500)
    }
    const onDocumentClick = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      if (!(event.target instanceof Element)) return
      const link = event.target.closest('a[href]') as HTMLAnchorElement | null
      if (!link || link.target === '_blank' || link.hasAttribute('download')) return
      const next = new URL(link.href, window.location.href)
      if (next.origin !== window.location.origin || (next.pathname === pathname && next.search === window.location.search)) return
      startRouteTransition()
    }
    const onPopState = () => startRouteTransition()
    document.addEventListener('click', onDocumentClick, true)
    window.addEventListener('popstate', onPopState)
    return () => {
      window.clearTimeout(routeResetTimer)
      if (resetTimer !== null) window.clearTimeout(resetTimer)
      document.removeEventListener('click', onDocumentClick, true)
      window.removeEventListener('popstate', onPopState)
    }
  }, [pathname])

  function isActive(href: string) {
    if (href === '/') return pathname === '/'
    return pathname === href || pathname.startsWith(`${href}/`)
  }

  const showCommunityDock = Boolean(benefitConfig?.enabled) && !chatSurface && !workspaceSurface
  const qqGroupValue = benefitConfig?.qq_group?.trim() || ''
  const legacyQqLink = /^https?:\/\//i.test(qqGroupValue) ? qqGroupValue : ''
  const qqJoinLink = benefitConfig?.qq_link?.trim() || legacyQqLink
  const qqGroupLabel = legacyQqLink ? '' : qqGroupValue
  const hasCommunityInfo = Boolean(benefitConfig?.qq_qr_url || benefitConfig?.wechat_qr_url || qqJoinLink || qqGroupLabel || benefitConfig?.wechat_group)
  const messageLinkClass = [
    'icon-button labeled-icon-button',
    isActive('/messages') ? 'top-tool-active' : '',
    hasUnreadMessages ? 'top-tool-attention' : ''
  ].filter(Boolean).join(' ')
  const activePrimaryNav = primaryNav.find(item => isActive(item.href))
  const currentSection = activePrimaryNav
    ? t(activePrimaryNav.label)
    : isActive('/messages')
      ? t('messages')
      : isActive('/profile')
        ? t('profile')
        : isActive('/credits')
          ? t('creditsLabel')
          : 'PicPilot'

  const shellClassName = [
    'app-shell user-shell',
    chatSurface ? 'user-shell-chat' : '',
    workspaceSurface ? 'user-shell-workflow' : ''
  ].filter(Boolean).join(' ')

  return (
    <div className={shellClassName}>
      <div className={routePending ? 'user-route-progress is-active' : 'user-route-progress'} aria-hidden="true" />
      <aside className="user-sidebar" aria-label={t('userNavAria')}>
        <div className="user-sidebar-inner">
          <Link href="/workspace" className="user-sidebar-brand" aria-label={t('homeAria')}>
            <BrandLogo className="picpilot-brand-sidebar" subtitle="AI COMMERCE STUDIO" />
          </Link>
          <nav className="user-side-nav" aria-label={t('userNavAria')}>
            {primaryNav.map(({ label, href, Icon }) => (
              <Link
                key={label}
                className={isActive(href) ? 'user-side-nav-active' : ''}
                href={href}
                prefetch
                onMouseEnter={() => prefetchRoute(href)}
                onFocus={() => prefetchRoute(href)}
                title={t(label)}
                aria-current={isActive(href) ? 'page' : undefined}
              >
                <Icon size={18} />
                <span>{t(label)}</span>
              </Link>
            ))}
          </nav>
          <div className="user-sidebar-spacer" />
          <nav className="user-sidebar-utility" aria-label={t('profile')}>
            {config.edition === 'personal' ? <Link href="/providers" prefetch onMouseEnter={() => prefetchRoute('/providers')} onFocus={() => prefetchRoute('/providers')} className={isActive('/providers') ? 'user-side-nav-active' : ''} title={t('providersTitle')}><Settings2 size={18} /><span>{t('providersTitle')}</span></Link> : null}
            {platform ? <Link
              className={isActive('/messages') ? 'user-side-nav-active' : ''}
              href="/messages"
              prefetch
              onMouseEnter={() => prefetchRoute('/messages')}
              onFocus={() => prefetchRoute('/messages')}
              title={t('messages')}
              aria-current={isActive('/messages') ? 'page' : undefined}
            >
              <Bell size={18} />
              <span>{t('messages')}</span>
              {hasUnreadMessages ? <i className="user-sidebar-unread" aria-hidden="true" /> : null}
            </Link> : null}
            <Link
              className={isActive('/profile') ? 'user-side-nav-active' : ''}
              href="/profile"
              prefetch
              onMouseEnter={() => prefetchRoute('/profile')}
              onFocus={() => prefetchRoute('/profile')}
              title={t('profile')}
              aria-current={isActive('/profile') ? 'page' : undefined}
            >
              <UserRound size={18} />
              <span>{t('profile')}</span>
            </Link>
          </nav>
        </div>
      </aside>
      <header className="topbar user-topbar">
        <div className="topbar-inner">
          <div className="user-topbar-leading">
            <Link href="/workspace" className="user-mobile-brand" aria-label={t('homeAria')}>
              <BrandLogo className="picpilot-brand-shell" />
            </Link>
            <div className="user-topbar-context">
              <strong>{currentSection}</strong>
              <span>{t(config.edition === 'personal' ? 'personalEdition' : 'platformEdition')}</span>
            </div>
          </div>
          <div className="topbar-actions">
            {config.edition === 'personal' ? <Link className="icon-button" href="/providers" title={t('providersTitle')} aria-label={t('providersTitle')}><Settings2 size={16} /></Link> : null}
            {billingEnabled ? <Link className="pill pill-strong top-credit-link" href="/credits" title={t('rechargeRedeem')} aria-label={t('rechargeRedeem')}>
              <span className="credit-pill-main"><CreditCard size={15} /><span>{credits ?? '--'} {t('creditsLabel')}</span></span>
            </Link> : null}
            {platform ? <Link className={messageLinkClass} href="/messages" title={t('messages')} aria-label={t('messages')}><Bell size={16} /><span className="tool-label">{t('messages')}</span>{hasUnreadMessages ? <span className="attention-mark">!</span> : null}</Link> : null}
            <LanguageMenu />
            <ThemeToggle />
            <AccountMenu />
          </div>
        </div>
      </header>
      <main className={workspaceSurface ? 'user-main user-main-workflow' : 'user-main'}>
        <div className="user-route-stage" key={pathname}>{children}</div>
      </main>
      <button
        className={showBackToTop ? 'user-back-to-top is-visible' : 'user-back-to-top'}
        type="button"
        title="回到顶部"
        aria-label="回到顶部"
        aria-hidden={!showBackToTop}
        tabIndex={showBackToTop ? 0 : -1}
        onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
      >
        <ArrowUp size={18} />
      </button>
      {chatSurface || pathname === '/providers' ? null : <GlobalAssistant />}
      {showCommunityDock ? (
        <aside className="community-float" aria-label={t('communityFloatAria')}>
          <Link className="community-float-button" href="/messages" title={t('messages')}>
            <Bell size={20} />
            {hasUnreadMessages ? <span className="community-float-badge">!</span> : null}
          </Link>
          <div className="community-float-item">
            <button className="community-float-button" type="button" aria-label={t('communityFloatAria')}>
              <Users size={20} />
            </button>
            <div className="community-popover" role="tooltip">
              <div className="community-popover-head">
                <strong>{benefitConfig?.title || t('qqTitle')}</strong>
                <p>{t('communityFloatHint')}</p>
              </div>
              {hasCommunityInfo ? (
                <div className="community-qr-grid">
                  {benefitConfig?.qq_qr_url || qqJoinLink || qqGroupLabel ? (
                    <div>
                      {benefitConfig?.qq_qr_url ? <img src={assetUrl(benefitConfig.qq_qr_url)} alt="QQ" /> : null}
                      {qqJoinLink ? <a className="btn btn-secondary btn-block" href={qqJoinLink} target="_blank" rel="noreferrer">QQ</a> : null}
                      {qqGroupLabel ? <strong>QQ {qqGroupLabel}</strong> : null}
                    </div>
                  ) : null}
                  {benefitConfig?.wechat_qr_url ? (
                    <div>
                      <img src={assetUrl(benefitConfig.wechat_qr_url)} alt="WeChat" />
                      <strong>WeChat{benefitConfig.wechat_group ? ` ${benefitConfig.wechat_group}` : ''}</strong>
                    </div>
                  ) : null}
                </div>
              ) : (
                <p className="community-no-qr">{[qqJoinLink ? `QQ: ${qqJoinLink}` : (qqGroupLabel ? `QQ: ${qqGroupLabel}` : ''), benefitConfig?.wechat_group ? `WeChat: ${benefitConfig.wechat_group}` : ''].filter(Boolean).join(' · ') || t('communityNoQr')}</p>
              )}
              <Link className="btn btn-primary btn-block" href="/messages"><MessageCircle size={16} />{t('messages')}</Link>
            </div>
          </div>
        </aside>
      ) : null}
      <nav className="user-mobile-dock" aria-label={t('userNavAria')}>
        {mobileNav.map(({ label, href, Icon }) => (
          <Link
            key={label}
            className={isActive(href) ? 'user-mobile-dock-active' : ''}
            href={href}
            aria-current={isActive(href) ? 'page' : undefined}
          >
            <Icon size={18} />
            <span>{t(label)}</span>
          </Link>
        ))}
      </nav>
    </div>
  )
}
