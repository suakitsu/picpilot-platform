'use client'

import Link from 'next/link'
import { useI18n } from '@/components/i18n'

export default function AdminRegisterDisabledPage() {
  const t = useI18n()

  return (
    <main className="auth-page">
      <section className="auth-card">
        <div className="auth-brand"><span className="admin-mark">PP</span><div><h1>{t('registerClosedTitle')}</h1><p>{t('registerClosedDesc')}</p></div></div>
        <div className="notice notice-error">{t('registerClosedNotice')}</div>
        <div className="auth-footer"><Link href="/login">{t('goLogin')}</Link> <Link href="/">{t('backAdmin')}</Link></div>
      </section>
    </main>
  )
}
