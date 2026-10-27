'use client'

import { Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import { Shell } from '@/components/Shell'
import { CopyAgentPanel } from '@/components/CopyAgentPanel'
import { WorkflowTabNav } from '@/components/WorkflowTabNav'
import { CreativeWorkflow } from '@/components/workflows/CreativeWorkflow'
import { ImageAgentExperience } from '@/components/workflows/ImageAgentExperience'

const AI_CREATE_TABS = [
  { value: 'free', label: '自由创作' },
  { value: 'agent', label: '对话生图' },
  { value: 'copy', label: '文案 Agent' }
]

function normalizeTab(value: string | null) {
  if (value === 'agent' || value === 'copy') return value
  return 'free'
}

function AiCreatePageInner() {
  const searchParams = useSearchParams()
  const activeTab = normalizeTab(searchParams.get('tab'))

  if (activeTab === 'agent') {
    return <ImageAgentExperience />
  }

  const isCopyTab = activeTab === 'copy'

  return (
    <Shell chatSurface={isCopyTab}>
      <div className="page-head">
        <div>
          <h1>AI 创作</h1>
          <p>自由生图、对话式生图和文案 Agent 分开管理；想聊天生成图片时可进入独立对话页。</p>
        </div>
      </div>
      <WorkflowTabNav basePath="/ai-create" tabs={AI_CREATE_TABS} activeTab={activeTab} prefetch={false} />
      <div className="workflow-tab-panel">
        {isCopyTab ? <CopyAgentPanel /> : <CreativeWorkflow embedded />}
      </div>
    </Shell>
  )
}

export default function AiCreatePage() {
  return (
    <Suspense fallback={<Shell chatSurface><div className="page-head"><div><h1>AI 创作</h1><p>正在加载...</p></div></div></Shell>}>
      <AiCreatePageInner />
    </Suspense>
  )
}
