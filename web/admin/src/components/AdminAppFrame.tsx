'use client'

import { ReactNode } from 'react'
import { usePathname } from 'next/navigation'
import { Shell } from '@/components/Shell'

const PUBLIC_ROUTES = new Set(['/login', '/register', '/setup'])

export function AdminAppFrame({ children }: { children: ReactNode }) {
  const pathname = usePathname()
  if (PUBLIC_ROUTES.has(pathname)) return children
  return <Shell>{children}</Shell>
}
