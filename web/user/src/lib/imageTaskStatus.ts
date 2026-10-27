import type { ImageTaskView } from '@/lib/api'

const ACTIVE_IMAGE_TASK_STATUSES = new Set([
  'queued',
  'running',
  'submitting',
  'submitted',
  'generating',
  'processing',
  'in_progress'
])

export function isActiveImageTaskStatus(status: string | null | undefined) {
  return ACTIVE_IMAGE_TASK_STATUSES.has((status || '').trim().toLowerCase())
}

export function hasActiveImageTasks(tasks: ImageTaskView[]) {
  return tasks.some(task => isActiveImageTaskStatus(task.status))
}
