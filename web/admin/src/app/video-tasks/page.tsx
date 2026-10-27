'use client'

import { RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useConfirm } from '@/components/ConfirmProvider'
import { Pagination } from '@/components/Pagination'
import { AdminVideoTask, deleteAdminVideoTask, listAdminVideoTasks } from '@/lib/api'

const ACTIVE = new Set(['queued', 'submitting', 'submitted', 'generating', 'upscaling', 'quality_check'])

function statusLabel(status: string) {
  if (status === 'queued') return '排队中'
  if (status === 'submitting') return '正在提交'
  if (status === 'submitted' || status === 'generating') return '生成中'
  if (status === 'upscaling') return '处理中'
  if (status === 'quality_check') return '质量检查'
  if (status === 'completed') return '已完成'
  if (status === 'failed') return '失败'
  if (status === 'timeout') return '超时'
  if (status === 'cancelled') return '已取消'
  return status
}

function statusClass(status: string) {
  if (status === 'completed') return 'status-good'
  if (['failed', 'timeout', 'cancelled'].includes(status)) return 'status-bad'
  return 'status-warn'
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '-'
}

function elapsedSeconds(start?: string, end?: string) {
  if (!start || !end) return '-'
  return `${Math.max(0, Math.round((new Date(end).getTime() - new Date(start).getTime()) / 1000))}s`
}

function modelChoiceLabel(mode: string) {
  if (mode === 'gemini-omni') return 'Gemini Omni'
  if (mode === 'seedance-mini') return 'Seedance Mini'
  if (mode === 'seedance-fast') return 'Seedance 2.0 Fast'
  if (mode === 'seedance-2') return 'Seedance 2.0'
  if (mode === 'quality') return '品质模式'
  if (mode === 'standard') return '标准模式'
  return mode || '-'
}

function videoCostDetailLabel(task: AdminVideoTask) {
  const details = task.cost_details
  if (!details || details.upstream_cost == null || details.conversion_rate == null) return ''
  if (details.upstream_cost_unit === 'APIMART_CREDIT') {
    return `上游 ${Number(details.upstream_cost).toFixed(3)} Credits × ¥${Number(details.conversion_rate).toFixed(6)}`
  }
  if (details.upstream_cost_unit === 'USD') {
    return `上游 $${Number(details.upstream_cost).toFixed(4)} × ¥${Number(details.conversion_rate).toFixed(6)}`
  }
  return ''
}

export default function Page() {
  const confirm = useConfirm()
  const [tasks, setTasks] = useState<AdminVideoTask[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [provider, setProvider] = useState('')
  const [message, setMessage] = useState('正在加载视频任务...')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const pageSize = 20

  async function load(nextPage = page) {
    try {
      const data = await listAdminVideoTasks({ keyword, status, provider, page: nextPage, pageSize })
      setTasks(data.rows)
      setPage(data.page)
      setTotal(data.total)
      setTotalPages(data.total_pages)
      setMessage(data.rows.length ? '' : '暂无符合条件的视频任务。')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '视频任务加载失败')
    }
  }

  useEffect(() => { load(1) }, [])
  useEffect(() => {
    if (!tasks.some(task => ACTIVE.has(task.status))) return
    const timer = window.setInterval(() => load(page), 5000)
    return () => window.clearInterval(timer)
  }, [tasks, page])

  async function remove(task: AdminVideoTask) {
    if (ACTIVE.has(task.status)) return
    if (!await confirm({ title: '删除视频任务', message: `确定由管理员删除视频任务 ${task.id}？此操作会同时从用户端和管理员后台移除记录。`, tone: 'danger' })) return
    try {
      await deleteAdminVideoTask(task.id)
      setTasks(current => current.filter(item => item.id !== task.id))
      setTotal(current => Math.max(0, current - 1))
      setMessage(`视频任务 ${task.id} 已由管理员删除。`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '视频任务删除失败')
    }
  }

  return (
    <>
      <div className="page-head">
        <div><h1>视频任务</h1><p>查看商品视频的用户、路由、生成状态、退款和上游实际结果。</p></div>
        <button className="btn btn-primary" type="button" onClick={() => load(page)}><RefreshCw size={16} />刷新</button>
      </div>

      <div className="filter-bar action-filter-bar admin-filter-grid">
        <select className="select" value={status} onChange={event => { setStatus(event.target.value); setPage(1) }}>
          <option value="">全部状态</option>
          <option value="queued">排队中</option>
          <option value="submitting">正在提交</option>
          <option value="generating">生成中</option>
          <option value="completed">已完成</option>
          <option value="failed">失败</option>
          <option value="timeout">超时</option>
        </select>
        <select className="select" value={provider} onChange={event => { setProvider(event.target.value); setPage(1) }}>
          <option value="">全部供应商</option>
          <option value="agnes">Agnes</option>
          <option value="gemini">Gemini Omni</option>
          <option value="seedance">Seedance</option>
        </select>
        <input className="input" value={keyword} onChange={event => { setKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder="任务 ID、名称或用户 ID" />
        <button className="btn btn-secondary" type="button" onClick={() => load(1)}>查询</button>
      </div>

      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}

      <section className="admin-card table-card operation-card">
        <div className="table-wrap">
          <table className="data-table">
            <thead><tr><th>任务 / 用户</th><th>用户选择</th><th>状态</th><th>供应商</th><th>请求规格</th><th>实际结果</th><th>积分 / 成本</th><th>排队 / 生成</th><th>时间</th><th>操作</th></tr></thead>
            <tbody>
              {tasks.map(task => (
                <tr key={task.id}>
                  <td><strong>{task.title || '未命名视频'}</strong><div className="table-subtext">{task.id}</div><div className="table-subtext">{task.user_account || task.user_id}</div>{task.hidden_by_user ? <div className="status-warn table-subtext">用户已隐藏</div> : null}{task.safe_error ? <div className="table-subtext status-bad">{task.safe_error}</div> : null}</td>
                  <td>{modelChoiceLabel(task.mode)}</td>
                  <td><span className={statusClass(task.status)}>{statusLabel(task.status)}</span><div className="table-subtext">{task.progress || 0}%</div>{task.refunded_at ? <div className="status-good">已退款</div> : null}</td>
                  <td>{task.provider || '-'}<div className="table-subtext">{task.model || '-'}</div><div className="table-subtext">{task.routing_reason || '-'}</div></td>
                  <td>{task.aspect_ratio} · {task.duration_seconds}s · {task.requested_resolution}</td>
                  <td>{task.actual_width && task.actual_height ? `${task.actual_width}×${task.actual_height}` : '-'}{task.actual_seconds ? <div className="table-subtext">{task.actual_seconds}s</div> : null}</td>
                  <td>
                    {task.credits_cost} 积分
                    <div className="table-subtext">成本 ¥{Number(task.actual_cost || 0).toFixed(6)}</div>
                    {videoCostDetailLabel(task) ? <div className="table-subtext">{videoCostDetailLabel(task)}</div> : null}
                  </td>
                  <td>{elapsedSeconds(task.queued_at || task.created_at, task.submitted_at)}<div className="table-subtext">{elapsedSeconds(task.submitted_at, task.completed_at)}</div></td>
                  <td>{formatDate(task.created_at)}<div className="table-subtext">更新 {formatDate(task.updated_at)}</div></td>
                  <td><button className="btn btn-danger" type="button" title={ACTIVE.has(task.status) ? '任务结束后可删除' : '管理员删除'} disabled={ACTIVE.has(task.status)} onClick={() => remove(task)}><Trash2 size={15} />删除</button></td>
                </tr>
              ))}
              {!tasks.length ? <tr><td colSpan={10} className="muted">暂无视频任务</td></tr> : null}
            </tbody>
          </table>
        </div>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={load} labels={{ previous: '上一页', next: '下一页', page: '页', total: '总数' }} />
      </section>
    </>
  )
}
