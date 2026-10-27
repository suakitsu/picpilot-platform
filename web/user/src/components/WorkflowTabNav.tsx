'use client'

import Link from 'next/link'
import { useSearchParams } from 'next/navigation'

export type WorkflowTab = {
  value: string
  label: string
  href?: string
}

export function WorkflowTabNav({ basePath, tabs, activeTab, prefetch }: { basePath: string; tabs: WorkflowTab[]; activeTab: string; prefetch?: boolean }) {
  const searchParams = useSearchParams()

  function hrefFor(tab: WorkflowTab) {
    if (tab.href) return tab.href
    const params = new URLSearchParams(searchParams.toString())
    params.set('tab', tab.value)
    return `${basePath}?${params.toString()}`
  }

  return (
    <nav className="workflow-tabs" aria-label="Workflow tabs">
      {tabs.map(tab => (
        <Link key={tab.value} className={activeTab === tab.value ? 'workflow-tab workflow-tab-active' : 'workflow-tab'} href={hrefFor(tab)} prefetch={prefetch}>
          {tab.label}
        </Link>
      ))}
    </nav>
  )
}
