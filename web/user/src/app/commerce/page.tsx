'use client'

import { Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import { Shell } from '@/components/Shell'
import { WorkflowTabNav } from '@/components/WorkflowTabNav'
import { CommerceSuiteWorkflow } from '@/components/CommerceSuiteWorkflow'
import { DomesticCommerceWorkflow } from '@/components/DomesticCommerceWorkflow'
import { ImageTranslationWorkflow } from '@/components/ImageTranslationWorkflow'
import { AplusWorkflow } from '@/components/workflows/AplusWorkflow'
import { ListingWorkflow } from '@/components/workflows/ListingWorkflow'

const COMMERCE_TABS = [
  { value: 'listing', label: '亚马逊 Listing' },
  { value: 'aplus', label: '亚马逊 A+' },
  { value: 'suite', label: '平台套图' },
  { value: 'translate', label: '图片翻译' },
  { value: 'domestic', label: '国内电商' }
]

function normalizeTab(value: string | null) {
  if (value === 'aplus' || value === 'domestic' || value === 'suite' || value === 'translate') return value
  return 'listing'
}

function CommercePageInner() {
  const searchParams = useSearchParams()
  const activeTab = normalizeTab(searchParams.get('tab'))

  return (
    <Shell>
      <div className="page-head">
        <div>
          <h1>电商制图</h1>
          <p>集中管理亚马逊 Listing、A+ 模块图、平台套图、图片翻译和国内电商图片。</p>
        </div>
      </div>
      <WorkflowTabNav basePath="/commerce" tabs={COMMERCE_TABS} activeTab={activeTab} />
      <div className="workflow-tab-panel">
        {activeTab === 'aplus' ? <AplusWorkflow embedded /> : activeTab === 'suite' ? <CommerceSuiteWorkflow /> : activeTab === 'translate' ? <ImageTranslationWorkflow /> : activeTab === 'domestic' ? <DomesticCommerceWorkflow /> : <ListingWorkflow embedded />}
      </div>
    </Shell>
  )
}

export default function CommercePage() {
  return (
    <Suspense fallback={<Shell><div className="page-head"><div><h1>电商制图</h1><p>正在加载...</p></div></div></Shell>}>
      <CommercePageInner />
    </Suspense>
  )
}
