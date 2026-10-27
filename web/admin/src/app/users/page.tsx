'use client'

import { validNewPassword } from '@/lib/password'
import { useI18n } from '@/components/i18n'

import { startTransition, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Link from 'next/link'
import { BadgeCheck, Ban, Coins, KeyRound, MoreHorizontal, ReceiptText, RefreshCw, RotateCcw, Search, ShieldAlert, ShieldCheck, SlidersHorizontal, UserRoundCheck, X } from 'lucide-react'
import { Pagination } from '@/components/Pagination'
import { adjustUserCredits, AdminUserRow, clearUserRiskEvents, listAdminUsers, resetUserPassword, updateUserStatus } from '@/lib/api'

type UserAction = '' | 'add_credits' | 'deduct_credits' | 'reset_password' | 'disable' | 'restore' | 'clear_risk'
type ActionMenuPosition = { top: number; left: number }

const ACTION_MENU_WIDTH = 184
const ACTION_MENU_ESTIMATED_HEIGHT = 198
const ACTION_MENU_GAP = 6
const ACTION_MENU_VIEWPORT_MARGIN = 12

function calculateActionMenuPosition(trigger: HTMLButtonElement, menuHeight = ACTION_MENU_ESTIMATED_HEIGHT): ActionMenuPosition {
  const rect = trigger.getBoundingClientRect()
  const maxLeft = Math.max(ACTION_MENU_VIEWPORT_MARGIN, window.innerWidth - ACTION_MENU_WIDTH - ACTION_MENU_VIEWPORT_MARGIN)
  const left = Math.min(Math.max(ACTION_MENU_VIEWPORT_MARGIN, rect.right - ACTION_MENU_WIDTH), maxLeft)
  const availableBelow = window.innerHeight - rect.bottom - ACTION_MENU_VIEWPORT_MARGIN
  const availableAbove = rect.top - ACTION_MENU_VIEWPORT_MARGIN
  const openAbove = availableBelow < menuHeight + ACTION_MENU_GAP && availableAbove > availableBelow
  const preferredTop = openAbove ? rect.top - menuHeight - ACTION_MENU_GAP : rect.bottom + ACTION_MENU_GAP
  const maxTop = Math.max(ACTION_MENU_VIEWPORT_MARGIN, window.innerHeight - menuHeight - ACTION_MENU_VIEWPORT_MARGIN)
  return {
    top: Math.min(Math.max(ACTION_MENU_VIEWPORT_MARGIN, preferredTop), maxTop),
    left
  }
}

function makeTempPassword() {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
  return Array.from(crypto.getRandomValues(new Uint8Array(24)), byte => alphabet[byte % alphabet.length]).join('')
}

function accountLabel(user?: AdminUserRow) {
  return user?.email_display || user?.email_masked || user?.id || '-'
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function statusClass(status?: string) {
  return status === 'active' ? 'status-good' : 'status-bad'
}

export default function Page() {
  const t = useI18n()
  const [users, setUsers] = useState<AdminUserRow[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [role, setRole] = useState('')
  const [minCredits, setMinCredits] = useState('')
  const [maxCredits, setMaxCredits] = useState('')
  const [sort, setSort] = useState('created_desc')
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [modalUser, setModalUser] = useState<AdminUserRow | null>(null)
  const [modalAction, setModalAction] = useState<UserAction>('')
  const [amount, setAmount] = useState('100')
  const [remark, setRemark] = useState('人工调整积分')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [openMenuUserId, setOpenMenuUserId] = useState('')
  const [actionMenuPosition, setActionMenuPosition] = useState<ActionMenuPosition | null>(null)
  const actionMenuTriggerRef = useRef<HTMLButtonElement | null>(null)
  const actionMenuRef = useRef<HTMLDivElement | null>(null)
  const requestSequence = useRef(0)
  const pageSize = 20
  const signedAmount = modalAction === 'deduct_credits' ? -Math.abs(Number(amount) || 0) : Math.abs(Number(amount) || 0)
  const pageStats = useMemo(() => ({
    active: users.filter(user => user.status === 'active').length,
    limited: users.filter(user => user.status !== 'active').length,
    credits: users.reduce((sum, user) => sum + Number(user.credits || 0), 0),
    risks: users.reduce((sum, user) => sum + Number(user.risk_count || 0), 0)
  }), [users])
  const actionTitle = useMemo(() => {
    if (modalAction === 'add_credits') return '增加积分'
    if (modalAction === 'deduct_credits') return '扣减积分'
    if (modalAction === 'reset_password') return '重置登录密码'
    if (modalAction === 'disable') return '禁用用户'
    if (modalAction === 'restore') return '恢复用户'
    if (modalAction === 'clear_risk') return '清空风控次数并解禁'
    return '用户操作'
  }, [modalAction])
  const openMenuUser = useMemo(
    () => users.find(user => user.id === openMenuUserId) || null,
    [openMenuUserId, users]
  )

  const closeActionMenu = useCallback(() => {
    setOpenMenuUserId('')
    setActionMenuPosition(null)
    actionMenuTriggerRef.current = null
  }, [])

  const updateActionMenuPosition = useCallback(() => {
    const trigger = actionMenuTriggerRef.current
    if (!trigger) return
    const rect = trigger.getBoundingClientRect()
    if (rect.bottom < 0 || rect.top > window.innerHeight || rect.right < 0 || rect.left > window.innerWidth) {
      closeActionMenu()
      return
    }
    setActionMenuPosition(calculateActionMenuPosition(trigger, actionMenuRef.current?.offsetHeight))
  }, [closeActionMenu])

  const loadUsers = useCallback(async (nextPage = 1) => {
    const requestId = ++requestSequence.current
    closeActionMenu()
    setLoading(true)
    try {
      const data = await listAdminUsers({
        keyword,
        status,
        role,
        minCredits: minCredits.trim() ? Number(minCredits) : undefined,
        maxCredits: maxCredits.trim() ? Number(maxCredits) : undefined,
        sort,
        page: nextPage,
        pageSize
      })
      if (requestId !== requestSequence.current) return
      startTransition(() => {
        setUsers(data.rows)
        setPage(data.page)
        setTotal(data.total)
        setTotalPages(data.total_pages)
        setMessage(data.rows.length ? '' : '暂无符合筛选条件的用户')
      })
    } catch (error) {
      if (requestId !== requestSequence.current) return
      setMessage(error instanceof Error ? error.message : '用户加载失败')
    } finally {
      if (requestId === requestSequence.current) setLoading(false)
    }
  }, [closeActionMenu, keyword, maxCredits, minCredits, role, sort, status])

  useEffect(() => {
    const timer = window.setTimeout(() => loadUsers(1), 300)
    return () => window.clearTimeout(timer)
  }, [loadUsers])

  function resetFilters() {
    setKeyword('')
    setStatus('')
    setRole('')
    setMinCredits('')
    setMaxCredits('')
    setSort('created_desc')
  }

  useEffect(() => {
    if (!openMenuUserId) return
    updateActionMenuPosition()
    const frame = window.requestAnimationFrame(updateActionMenuPosition)
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Node)) return
      if (actionMenuRef.current?.contains(target) || actionMenuTriggerRef.current?.contains(target)) return
      closeActionMenu()
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeActionMenu()
    }
    window.addEventListener('resize', updateActionMenuPosition)
    window.addEventListener('scroll', updateActionMenuPosition, true)
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      window.cancelAnimationFrame(frame)
      window.removeEventListener('resize', updateActionMenuPosition)
      window.removeEventListener('scroll', updateActionMenuPosition, true)
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [closeActionMenu, openMenuUserId, updateActionMenuPosition])

  function openAction(user: AdminUserRow, action: UserAction) {
    if (!action) return
    setModalUser(user)
    setModalAction(action)
    closeActionMenu()
    setAmount('100')
    setRemark(action === 'deduct_credits' ? '扣减积分' : '人工调整积分')
    setPassword(action === 'reset_password' ? makeTempPassword() : '')
    setShowPassword(false)
    setMessage('')
  }

  function closeModal() {
    if (submitting) return
    setModalUser(null)
    setModalAction('')
  }

  async function submitAction() {
    if (!modalUser || !modalAction || submitting) return
    setSubmitting(true)
    setMessage('')
    try {
      if (modalAction === 'add_credits' || modalAction === 'deduct_credits') {
        if (!signedAmount) throw new Error('积分数量不能为 0')
        const data = await adjustUserCredits({
          userId: modalUser.id,
          changeAmount: signedAmount,
          remark: remark.trim() || (signedAmount > 0 ? '人工调整积分' : '扣减积分'),
          requestToken: `rt_${Date.now()}`
        })
        setUsers(rows => rows.map(user => user.id === modalUser.id ? { ...user, credits: data.balance_after } : user))
        setMessage(`积分已调整，当前余额 ${data.balance_after}`)
      } else if (modalAction === 'reset_password') {
        if (!validNewPassword(password)) throw new Error(t('users.passwordRule'))
        await resetUserPassword({ userId: modalUser.id, password })
        setMessage('密码已更新')
        setPassword('')
      } else if (modalAction === 'disable' || modalAction === 'restore') {
        const status = modalAction === 'disable' ? 'disabled' : 'active'
        const data = await updateUserStatus({ userId: modalUser.id, status })
        setUsers(rows => rows.map(user => user.id === modalUser.id ? { ...user, status: data.status } : user))
        setMessage(status === 'active' ? '用户已恢复' : '用户已禁用')
      } else if (modalAction === 'clear_risk') {
        const data = await clearUserRiskEvents(modalUser.id)
        setUsers(rows => rows.map(user => user.id === modalUser.id ? { ...user, status: data.status, risk_count: 0 } : user))
        setMessage(`已清空 ${data.cleared} 条风控记录并解禁`)
      }
      setModalUser(null)
      setModalAction('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <div className="page-head user-management-head">
        <div>
          <h1>用户管理</h1>
          <p>按用户查看账户状态、积分、充值和风控记录；敏感操作统一在右侧抽屉完成。</p>
        </div>
        <Link className="btn btn-secondary" href="/recharges"><ReceiptText size={16} />查看充值流水</Link>
      </div>

      <section className="admin-card user-filter-panel" aria-label="用户筛选">
        <div className="user-filter-heading"><span><SlidersHorizontal size={17} /></span><div><strong>搜索与分类</strong><small>按账号、状态、角色、积分区间和活跃度定位用户</small></div></div>
        <div className="user-filter-grid">
          <label className="field user-filter-search"><span>用户账号</span><div className="user-search-box"><Search size={16} /><input className="input user-search-input" value={keyword} onChange={event => setKeyword(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') loadUsers(1) }} placeholder="邮箱 / GitHub / 昵称 / 用户 ID" /></div></label>
          <label className="field"><span>状态</span><select className="select" value={status} onChange={event => setStatus(event.target.value)}><option value="">全部状态</option><option value="active">正常</option><option value="disabled">已禁用</option><option value="banned">已封禁</option></select></label>
          <label className="field"><span>角色</span><select className="select" value={role} onChange={event => setRole(event.target.value)}><option value="">全部角色</option><option value="user">普通用户</option><option value="admin">管理员</option><option value="super_admin">超级管理员</option></select></label>
          <label className="field user-credit-range"><span>积分区间</span><span><input className="input" type="number" min={0} value={minCredits} onChange={event => setMinCredits(event.target.value)} placeholder="最低" /><i>至</i><input className="input" type="number" min={0} value={maxCredits} onChange={event => setMaxCredits(event.target.value)} placeholder="最高" /></span></label>
          <label className="field"><span>排序</span><select className="select" value={sort} onChange={event => setSort(event.target.value)}><option value="created_desc">最新注册</option><option value="created_asc">最早注册</option><option value="credits_desc">积分从高到低</option><option value="credits_asc">积分从低到高</option><option value="last_active_desc">最近活跃</option></select></label>
          <div className="user-filter-actions"><button className="btn" type="button" onClick={resetFilters}>重置</button><button className="btn btn-primary" type="button" disabled={loading} onClick={() => loadUsers(1)}>{loading ? <span className="spin"><RefreshCw size={15} /></span> : <RefreshCw size={15} />}{loading ? '查询中' : '查询'}</button></div>
        </div>
      </section>

      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}

      <section className="admin-kpi-grid user-kpi-grid" aria-label="用户概览">
        <div className="admin-kpi-card"><span>当前页用户</span><strong>{users.length}</strong><small>共 {total} 个匹配用户</small></div>
        <div className="admin-kpi-card"><span>Active</span><strong>{pageStats.active}</strong><small>当前页可正常使用</small></div>
        <div className="admin-kpi-card"><span>当前页积分</span><strong>{pageStats.credits}</strong><small>用于快速发现异常余额</small></div>
        <div className="admin-kpi-card"><span>受限用户</span><strong>{pageStats.limited}</strong><small>disabled / banned</small></div>
        <div className="admin-kpi-card"><span>风控计数</span><strong>{pageStats.risks}</strong><small>点击数字可看历史原文</small></div>
      </section>

      <section className={loading ? 'admin-card table-card admin-results-loading' : 'admin-card table-card'} aria-busy={loading}>
        <table className="data-table admin-user-table">
          <thead>
            <tr>
              <th>用户</th>
              <th>昵称</th>
              <th>角色</th>
              <th>状态</th>
              <th>积分</th>
              <th>项目</th>
              <th>充值</th>
              <th>风控</th>
              <th>注册 IP</th>
              <th>最近请求</th>
              <th>注册时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {users.map(user => (
              <tr key={user.id}>
                <td><strong>{accountLabel(user)}</strong><div className="muted small-text">{user.id}</div></td>
                <td>{user.nickname || '-'}</td>
                <td>{user.role}</td>
                <td><span className={statusClass(user.status)}>{user.status}</span></td>
                <td><strong>{user.credits}</strong></td>
                <td>{user.project_count}</td>
                <td>{user.recharge_count ? <Link className="status-good" href={`/recharges?keyword=${encodeURIComponent(user.id)}`}>{user.recharge_count}</Link> : 0}</td>
                <td>{user.risk_count ? <Link className="status-bad" href={`/risk-events?user_keyword=${encodeURIComponent(user.id)}`}>{user.risk_count}</Link> : 0}</td>
                <td>{user.register_ip || '-'}</td>
                <td>{user.last_request_ip || '-'}<div className="muted small-text">{formatDate(user.last_request_at)}</div></td>
                <td>{formatDate(user.created_at)}</td>
                <td>
                  <div className="row-action-menu">
                    <button
                      className="btn btn-compact row-action-trigger"
                      type="button"
                      aria-haspopup="menu"
                      aria-expanded={openMenuUserId === user.id}
                      aria-controls={openMenuUserId === user.id ? 'admin-user-action-menu' : undefined}
                      onClick={event => {
                        if (openMenuUserId === user.id) {
                          closeActionMenu()
                          return
                        }
                        actionMenuTriggerRef.current = event.currentTarget
                        setActionMenuPosition(calculateActionMenuPosition(event.currentTarget))
                        setOpenMenuUserId(user.id)
                      }}
                    >
                      <MoreHorizontal size={16} />操作
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {!users.length ? <tr><td colSpan={12} className="muted">{loading ? '正在加载用户...' : '暂无用户'}</td></tr> : null}
          </tbody>
        </table>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={loadUsers} labels={{ previous: '上一页', next: '下一页', page: '页', total: '总数', jump: '跳至' }} />
      </section>

      {openMenuUser && actionMenuPosition && typeof document !== 'undefined' ? createPortal(
        <div
          id="admin-user-action-menu"
          ref={actionMenuRef}
          className="row-action-popover row-action-popover-portal"
          role="menu"
          aria-label={`${accountLabel(openMenuUser)} 的操作`}
          style={{ top: actionMenuPosition.top, left: actionMenuPosition.left }}
        >
          <button role="menuitem" type="button" onClick={() => openAction(openMenuUser, 'add_credits')}><Coins size={15} />增加积分</button>
          <button role="menuitem" type="button" onClick={() => openAction(openMenuUser, 'deduct_credits')}><Coins size={15} />扣减积分</button>
          <button role="menuitem" type="button" onClick={() => openAction(openMenuUser, 'reset_password')}><KeyRound size={15} />重置密码</button>
          <button role="menuitem" type="button" onClick={() => openAction(openMenuUser, openMenuUser.status === 'active' ? 'disable' : 'restore')} className={openMenuUser.status === 'active' ? 'danger-action' : ''}>
            {openMenuUser.status === 'active' ? <Ban size={15} /> : <UserRoundCheck size={15} />}{openMenuUser.status === 'active' ? '禁用用户' : '恢复用户'}
          </button>
          <button role="menuitem" type="button" onClick={() => openAction(openMenuUser, 'clear_risk')}><RotateCcw size={15} />清空风控次数</button>
        </div>,
        document.body
      ) : null}

      {modalUser ? (
        <div className="admin-modal-backdrop admin-drawer-backdrop" onMouseDown={closeModal}>
          <section className="modal-panel admin-action-modal admin-action-drawer" role="dialog" aria-modal="true" onMouseDown={event => event.stopPropagation()}>
            <div className="drawer-head">
              <div>
                <span className="eyebrow">{modalAction === 'reset_password' ? <KeyRound size={14} /> : modalAction === 'clear_risk' ? <ShieldAlert size={14} /> : modalAction === 'restore' ? <BadgeCheck size={14} /> : <ShieldCheck size={14} />}{actionTitle}</span>
                <h2>{accountLabel(modalUser)}</h2>
                <p>{modalUser.id}</p>
              </div>
              <button className="icon-button drawer-close" type="button" aria-label="关闭" onClick={closeModal}><X size={18} /></button>
            </div>
            <div className="drawer-summary">
              <div><span>状态</span><strong>{modalUser.status}</strong></div>
              <div><span>当前积分</span><strong>{modalUser.credits}</strong></div>
              <div><span>风控次数</span><strong>{modalUser.risk_count || 0}</strong></div>
            </div>

            {(modalAction === 'add_credits' || modalAction === 'deduct_credits') ? (
              <div className="config-list panel-body">
                <label className="field"><span>积分数量</span><input className="input" type="number" min={1} value={amount} onChange={event => setAmount(event.target.value)} /></label>
                <label className="field"><span>实际变动</span><input className="input" readOnly value={signedAmount ? `${signedAmount > 0 ? '+' : ''}${signedAmount}` : ''} /></label>
                <label className="field"><span>备注</span><input className="input" value={remark} onChange={event => setRemark(event.target.value)} /></label>
              </div>
            ) : null}

            {modalAction === 'reset_password' ? (
              <div className="config-list panel-body">
                <label className="field">
                  <span>临时密码</span>
                  <div className="password-field">
                    <input className="input" type={showPassword ? 'text' : 'password'} value={password} onChange={event => setPassword(event.target.value)} />
                    <button className="btn btn-compact" type="button" onClick={() => setShowPassword(value => !value)}>{showPassword ? '隐藏' : '显示'}</button>
                  </div>
                  <small className="field-hint">{t('users.passwordRule')}</small>
                </label>
                <button className="btn btn-secondary" type="button" onClick={() => { setPassword(makeTempPassword()); setShowPassword(true) }}>重新生成临时密码</button>
              </div>
            ) : null}

            {(modalAction === 'disable' || modalAction === 'restore' || modalAction === 'clear_risk') ? (
              <div className="panel-body">
                <p className="muted">{modalAction === 'disable' ? '禁用后用户不能继续使用生成能力。' : modalAction === 'restore' ? '恢复后用户状态会变为 active。' : '会软删除该用户当前风控事件，并把用户状态恢复为 active。'}</p>
              </div>
            ) : null}

            <div className="form-action-bar">
              <button className="btn btn-secondary" type="button" onClick={closeModal}>取消</button>
              <button className={modalAction === 'deduct_credits' || modalAction === 'disable' || modalAction === 'clear_risk' ? 'btn btn-danger' : 'btn btn-primary'} type="button" disabled={submitting} onClick={submitAction}>{submitting ? '提交中...' : `确认${actionTitle}`}</button>
            </div>
          </section>
        </div>
      ) : null}
    </>
  )
}
