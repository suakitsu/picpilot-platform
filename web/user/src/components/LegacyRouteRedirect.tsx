'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'

interface LegacyRouteRedirectProps {
  to: string
  tab: string
}

export function LegacyRouteRedirect({ to, tab }: LegacyRouteRedirectProps) {
  const router = useRouter()

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    params.set('tab', tab)
    router.replace(`${to}?${params.toString()}`)
  }, [router, tab, to])

  return null
}
