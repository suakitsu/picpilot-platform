import './canvas-lab.css'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { CanvasLab } from '@/components/canvas-lab/CanvasLab'

export default function WorkflowsPage() {
  return (
    <Shell workspaceSurface>
      <UserAuthGate title="登录后使用 AI 画布" description="组合提示词、参考图与生成步骤，持续沉淀可复用的创作结果。">
        <CanvasLab />
      </UserAuthGate>
    </Shell>
  )
}
