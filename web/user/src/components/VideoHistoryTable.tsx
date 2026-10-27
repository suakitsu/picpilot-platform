'use client'
import { BillingOnly } from '@/components/AppRuntime'

import Link from 'next/link'
import { Film, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { assetUrl, deleteVideoTask, listVideoTasks, VideoTaskView } from '@/lib/api'
import { videoModelLabel, videoSettlementLabel } from '@/lib/videoDisplay'

const ACTIVE = new Set(['queued', 'submitting', 'submission_unknown', 'submitted', 'generating', 'upscaling', 'quality_check'])

function label(status: string) {
  if (status === 'queued') return '排队中'
  if (status === 'submitting') return '正在提交'
  if (status === 'submission_unknown') return '提交状态待核验'
  if (status === 'submitted' || status === 'generating') return '生成中'
  if (status === 'upscaling') return '清晰度处理中'
  if (status === 'quality_check') return '质量检查中'
  if (status === 'completed') return '已完成'
  if (status === 'failed') return '失败'
  if (status === 'timeout') return '生成超时'
  if (status === 'cancelled') return '已取消'
  return status
}

function badge(status: string) {
  if (status === 'completed') return 'status status-ok'
  if (status === 'failed' || status === 'timeout' || status === 'cancelled') return 'status status-bad'
  return 'status status-warn'
}

function queueNotice(task: VideoTaskView) {
  if (task.status !== 'queued' || task.provider !== 'agnes') return ''
  return `当前任务正在排队，前方还有 ${Math.max(0, task.queue_ahead || 0)} 个任务。`
}

export function VideoHistoryTable() {
  const [tasks, setTasks] = useState<VideoTaskView[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('all')
  const [message, setMessage] = useState('正在加载视频历史...')

  function refresh() {
    listVideoTasks().then(rows => {
      setTasks(rows)
      setMessage(rows.length ? '' : '暂无视频历史，请先创建商品视频。')
    }).catch(error => setMessage(error instanceof Error ? error.message : '视频历史加载失败'))
  }

  useEffect(refresh, [])
  useEffect(() => {
    if (!tasks.some(task => ACTIVE.has(task.status))) return
    const timer = window.setInterval(refresh, 3000)
    return () => window.clearInterval(timer)
  }, [tasks])

  const filtered = tasks.filter(task => {
    if (status === 'active' && !ACTIVE.has(task.status)) return false
    if (status === 'completed' && task.status !== 'completed') return false
    if (status === 'failed' && !['failed', 'timeout', 'cancelled'].includes(task.status)) return false
    const query = keyword.trim().toLowerCase()
    return !query || `${task.title} ${task.prompt} ${task.aspect_ratio} ${label(task.status)}`.toLowerCase().includes(query)
  })

  async function remove(task: VideoTaskView) {
    if (ACTIVE.has(task.status)) return
    if (!window.confirm(`确定删除“${task.title}”吗？删除后将不再显示在历史记录中。`)) return
    try {
      await deleteVideoTask(task.id)
      setTasks(current => current.filter(item => item.id !== task.id))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '删除失败')
    }
  }

  return (
    <>
      <section className="panel">
        <div className="panel-head"><div><h2>视频筛选</h2><p>按状态或任务名称查找商品视频。</p></div></div>
        <div className="panel-body history-filter-body">
          <select className="select" value={status} onChange={event => setStatus(event.target.value)}>
            <option value="all">全部状态</option><option value="active">生成中</option><option value="completed">已完成</option><option value="failed">失败</option>
          </select>
          <input className="input" value={keyword} onChange={event => setKeyword(event.target.value)} placeholder="搜索任务名称或视频描述" />
        </div>
      </section>
      <section className="panel mt-16 history-table-panel">
        <div className="panel-head history-list-head"><div><h2>视频任务</h2><p>异步任务会自动更新，无需停留在生成页面。</p></div><Link className="btn btn-primary" href="/video-create">新建商品视频</Link></div>
        {message ? <div className="panel-body muted">{message}</div> : null}
        {filtered.length ? <div className="table-wrap"><table className="data-table data-table-history video-history-table"><thead><tr><th>预览</th><th>任务</th><th>规格 / 模型</th><th>状态</th><th>进度</th><BillingOnly><th>积分</th></BillingOnly><th>更新时间</th><th>操作</th></tr></thead><tbody>
          {filtered.map(task => <tr key={task.id}>
            <td><Link href={`/video-create?task=${encodeURIComponent(task.id)}`} aria-label={`查看${task.title}`}><div className="video-history-preview">{task.source_image_urls?.[0] ? <img src={assetUrl(task.source_image_urls[0])} alt="" /> : <span className="video-history-preview-empty"><Film size={18} /></span>}</div></Link></td>
            <td><div className="history-table-product"><strong>{task.title}</strong><span>{task.prompt.slice(0, 72)}</span></div>{task.safe_error ? <div className="table-subtext status-bad">{task.safe_error}</div> : null}</td>
            <td><div className="video-spec-model-cell"><span>{task.aspect_ratio} · {task.duration_seconds}s · {task.requested_resolution?.toLowerCase() === '4k' ? '4K' : task.requested_resolution}</span><span className="video-model-label">{videoModelLabel(task.model, task.provider)}</span></div></td>
            <td className="video-status-cell"><span className={badge(task.status)}>{label(task.status)}</span>{queueNotice(task) ? <div className="table-subtext">{queueNotice(task)}</div> : null}</td>
            <td className="video-progress-cell">{task.progress || 0}%</td>
            <BillingOnly><td><div className="video-credit-cell"><span>{task.metadata?.mock_mode ? `${task.metadata?.listed_price || 0}（Mock）` : `${task.credits_cost} 积分`}</span>{task.refunded_at && task.status !== 'completed' ? <span className="video-settlement video-settlement-refund" title="本次扣除的积分已退回">{videoSettlementLabel(task.refunded_at)}</span> : null}</div></td></BillingOnly>
            <td className="video-updated-cell">{new Date(task.updated_at).toLocaleString()}</td>
            <td><div className="table-actions"><Link className="btn btn-secondary" href={`/video-create?task=${encodeURIComponent(task.id)}`}>查看</Link><button className="icon-button" type="button" aria-label="删除视频任务" title={ACTIVE.has(task.status) ? '视频完成后可删除' : '删除视频任务'} disabled={ACTIVE.has(task.status)} onClick={() => remove(task)}><Trash2 size={16} /></button></div></td>
          </tr>)}
        </tbody></table></div> : null}
      </section>
    </>
  )
}
