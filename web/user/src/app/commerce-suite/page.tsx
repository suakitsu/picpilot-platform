'use client'

import { Shell } from '@/components/Shell'
import { CommerceSuiteWorkflow } from '@/components/CommerceSuiteWorkflow'

export default function CommerceSuitePage() {
  return (
    <Shell>
      <div className="page-head">
        <div>
          <h1>电商套图</h1>
          <p>淘宝/天猫、拼多多、抖音、小红书、TikTok Shop 等平台统一模板生成。</p>
        </div>
      </div>
      <CommerceSuiteWorkflow />
    </Shell>
  )
}
