'use client'

import { Suspense } from 'react'
import { AuthExperience } from '@/components/AuthExperience'
import { useI18n } from '@/components/i18n'

export default function LoginPage() {
  const t = useI18n()
  return (
    <Suspense fallback={<main className="auth-page"><section className="auth-card">{t('openingLogin')}</section></main>}>
      <AuthExperience initialView="login" />
    </Suspense>
  )
}
