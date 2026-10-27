'use client'

import Link from 'next/link'
import { ReactNode, useEffect, useMemo, useState } from 'react'
import { usePathname } from 'next/navigation'
import { ArrowUp, BarChart3, Bell, Bot, ChevronDown, CreditCard, FileText, Film, Gift, History, Image, Layers, LayoutDashboard, Mail, Menu, ReceiptText, RotateCw, Settings2, ShieldCheck, ShieldUser, Users, X } from 'lucide-react'
import { AccountMenu } from '@/components/AccountMenu'
import { AdminAuthGate } from '@/components/AdminAuthGate'
import { AdminRequestProgress } from '@/components/AdminRequestProgress'
import { BrandLogo } from '@/components/BrandLogo'
import { useI18n } from '@/components/i18n'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useRuntime } from '@/components/AppRuntime'

interface ShellProps { children: ReactNode }

const navGroups = [
  {
    title: 'overview',
    items: [
      { label: 'dashboard', href: '/', icon: LayoutDashboard }
    ]
  },
  {
    title: 'userOps',
    items: [
      { label: 'users', href: '/users', icon: Users },
      { label: 'recharges', href: '/recharges', icon: ReceiptText },
      { label: 'riskEvents', href: '/risk-events', icon: ShieldCheck },
      { label: 'benefits', href: '/benefits', icon: Gift },
      { label: 'messages', href: '/messages', icon: Mail }
    ]
  },
  {
    title: 'generationOps',
    items: [
      { label: 'tasks', href: '/tasks', icon: Image },
      { label: 'textCalls', href: '/text-calls', icon: FileText },
      { label: 'videoTasks', href: '/video-tasks', icon: Film },
      { label: 'costs', href: '/costs', icon: BarChart3 }
    ]
  },
  {
    title: 'systemConfig',
    items: [
      { label: 'providersTitle', href: '/providers', icon: Settings2 },
      { label: 'aiConfig', href: '/ai-config', icon: Bot },
      { label: 'commerceSuite', href: '/commerce-suite', icon: Layers },
      { label: 'prompts', href: '/prompts', icon: Settings2 },
      { label: 'billing', href: '/billing', icon: CreditCard },
      { label: 'auditLogs', href: '/audit-logs', icon: History }
    ]
  },
  {
    title: 'account',
    items: [
      { label: 'profile', href: '/profile', icon: ShieldUser }
    ]
  }
]

function isActive(pathname: string, href: string) {
  if (href === '/') return pathname === '/'
  return pathname.startsWith(href)
}

function currentTitle(pathname: string) {
  const item = navGroups.flatMap(group => group.items).find(navItem => isActive(pathname, navItem.href))
  return item?.label || 'adminConsole'
}

export function Shell({ children }: ShellProps) {
  const { billingEnabled } = useRuntime()
  const pathname = usePathname()
  const t = useI18n()
  const title = t(currentTitle(pathname))
  const activeGroup = useMemo(() => navGroups.find(group => group.items.some(item => isActive(pathname, item.href)))?.title || 'overview', [pathname])
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [showBackToTop, setShowBackToTop] = useState(false)

  useEffect(() => {
    const updateBackToTop = () => setShowBackToTop(window.scrollY > 480)
    updateBackToTop()
    window.addEventListener('scroll', updateBackToTop, { passive: true })
    return () => window.removeEventListener('scroll', updateBackToTop)
  }, [])

  useEffect(() => {
    const retryKey = 'picpilot_admin_asset_retry'
    const recoverAsset = () => {
      const lastRetry = Number(window.sessionStorage.getItem(retryKey) || 0)
      if (Date.now() - lastRetry < 15_000) return
      window.sessionStorage.setItem(retryKey, String(Date.now()))
      window.location.reload()
    }
    const handleAssetError = (event: Event) => {
      const target = event.target
      if (target instanceof HTMLScriptElement && target.src.includes('/_next/static/')) recoverAsset()
      if (target instanceof HTMLLinkElement && target.href.includes('/_next/static/')) recoverAsset()
    }
    const handleRejectedAsset = (event: PromiseRejectionEvent) => {
      const message = event.reason instanceof Error ? event.reason.message : String(event.reason || '')
      if (/ChunkLoadError|Loading chunk|Failed to fetch dynamically imported module/i.test(message)) recoverAsset()
    }
    window.addEventListener('error', handleAssetError, true)
    window.addEventListener('unhandledrejection', handleRejectedAsset)
    return () => {
      window.removeEventListener('error', handleAssetError, true)
      window.removeEventListener('unhandledrejection', handleRejectedAsset)
    }
  }, [])

  function toggleGroup(groupTitle: string) {
    setExpandedGroups(prev => {
      const current = prev[groupTitle] ?? groupTitle === activeGroup
      return { ...prev, [groupTitle]: !current }
    })
  }

  return (
    <AdminAuthGate>
      <div className="admin-shell">
        <AdminRequestProgress />
        <aside className={mobileNavOpen ? 'admin-sidebar fixed-sidebar admin-sidebar-mobile-open' : 'admin-sidebar fixed-sidebar'}>
        <div className="admin-sidebar-head">
          <Link href="/" className="admin-brand side-brand" onClick={() => setMobileNavOpen(false)}>
            <BrandLogo className="picpilot-brand-admin" subtitle="Admin Console" />
          </Link>
          <button
            className="icon-button mobile-nav-toggle"
            type="button"
            aria-label={t('adminMenu')}
            aria-expanded={mobileNavOpen}
            onClick={() => setMobileNavOpen(open => !open)}
          >
            {mobileNavOpen ? <X size={18} /> : <Menu size={18} />}
          </button>
        </div>

        <nav className="admin-nav" aria-label={t('adminMenu')}>
          {navGroups.map(group => {
            const expanded = expandedGroups[group.title] ?? group.title === activeGroup
            return (
              <div className={group.title === activeGroup ? 'nav-group nav-group-active' : 'nav-group'} key={group.title}>
                <button
                  className="admin-nav-title nav-group-trigger"
                  type="button"
                  aria-expanded={expanded}
                  onClick={() => toggleGroup(group.title)}
                >
                  <span>{t(group.title)}</span>
                  <ChevronDown size={14} aria-hidden="true" />
                </button>
                {expanded ? (
                  <div className="nav-group-items">
                    {group.items.filter(item => billingEnabled || !['/billing', '/recharges', '/benefits'].includes(item.href)).map(item => (
                      <Link className={isActive(pathname, item.href) ? 'nav-item nav-item-active' : 'nav-item'} key={`${group.title}-${item.label}`} href={item.href} onClick={() => setMobileNavOpen(false)}>
                        <span className="nav-icon" aria-hidden="true"><item.icon size={16} strokeWidth={2.1} /></span>
                        <span>{t(item.label)}</span>
                      </Link>
                    ))}
                  </div>
                ) : null}
              </div>
            )
          })}
        </nav>

        <div className="sidebar-footer">
          <Link href="/profile" className="side-mode"><ShieldUser size={16} /><span>{t('profile')}</span></Link>
        </div>
        </aside>

        <div className="admin-workspace">
        <header className="admin-topbar">
          <div className="admin-topbar-inner">
            <div className="topbar-context" aria-label={title}>
              <span>{t('adminConsole')}</span>
              <span className="topbar-context-separator">/</span>
              <strong>{title}</strong>
            </div>
            <div className="topbar-tools">
              <Link className="icon-button labeled-icon-button" href={pathname} aria-label={t('refresh')} title={t('refresh')}><RotateCw size={16} /><span className="tool-label">{t('refresh')}</span></Link>
              <Link className="icon-button labeled-icon-button" href="/messages" aria-label={t('messages')} title={t('messages')}><Bell size={16} /><span className="tool-label">{t('messages')}</span></Link>
              <LanguageMenu />
              <ThemeToggle />
              <AccountMenu />
            </div>
          </div>
        </header>

        <main className="admin-main">
          <div className="admin-content-card">
            <div className="admin-route-stage" data-route={pathname} key={pathname}>{children}</div>
          </div>
        </main>
        </div>
        <button
          className={showBackToTop ? 'back-to-top back-to-top-visible' : 'back-to-top'}
          type="button"
          aria-label={t('backToTop')}
          title={t('backToTop')}
          onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
        >
          <ArrowUp size={18} strokeWidth={2.2} />
        </button>
      </div>
    </AdminAuthGate>
  )
}
