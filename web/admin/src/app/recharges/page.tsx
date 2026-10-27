'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import { CalendarDays, CheckCircle2, CircleDollarSign, Clock3, CreditCard, RefreshCw, Search, Settings2, UserRound } from 'lucide-react'
import { Pagination } from '@/components/Pagination'
import { useConfirm } from '@/components/ConfirmProvider'
import { cancelRechargeOrder, confirmRechargeOrder, listPaymentChannels, listRechargeOrders, PaymentChannel, RechargeOrder } from '@/lib/api'

function money(cents: number, currency = 'CNY') {
  const symbol = currency === 'CNY' ? '¥' : `${currency} `
  return `${symbol}${(Number(cents || 0) / 100).toFixed(2)}`
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function statusLabel(status: string) {
  if (status === 'pending') return '待支付'
  if (status === 'paid') return '已到账'
  if (status === 'failed') return '支付失败'
  if (status === 'cancelled') return '已取消'
  if (status === 'channel_disabled') return '渠道已关闭'
  return status || '-'
}

function statusClass(status: string) {
  if (status === 'paid') return 'status-good'
  if (status === 'pending') return 'status-warn'
  return 'status-bad'
}

export default function RechargeOrdersPage() {
  const confirm = useConfirm()
  const [orders, setOrders] = useState<RechargeOrder[]>([])
  const [channels, setChannels] = useState<PaymentChannel[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [channel, setChannel] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState('')
  const requestSequence = useRef(0)
  const pageSize = 20

  const pageStats = useMemo(() => {
    const paid = orders.filter(order => order.status === 'paid')
    return {
      paidCount: paid.length,
      paidAmount: paid.reduce((sum, order) => sum + Number(order.amount_cents || 0), 0),
      paidCredits: paid.reduce((sum, order) => sum + Number(order.credits || 0), 0),
      pending: orders.filter(order => order.status === 'pending').length
    }
  }, [orders])

  async function load(nextPage = 1, override: Partial<{ keyword: string; status: string; channel: string; from: string; to: string }> = {}) {
    const requestId = ++requestSequence.current
    setLoading(true)
    try {
      const result = await listRechargeOrders({
        keyword: override.keyword ?? keyword,
        status: override.status ?? status,
        channel: override.channel ?? channel,
        from: override.from ?? from,
        to: override.to ?? to,
        page: nextPage,
        pageSize
      })
      if (requestId !== requestSequence.current) return
      setOrders(result.rows)
      setPage(result.page)
      setTotal(result.total)
      setTotalPages(result.total_pages)
      setMessage(result.rows.length ? '' : '暂无符合筛选条件的充值记录')
    } catch (error) {
      if (requestId !== requestSequence.current) return
      setMessage(error instanceof Error ? error.message : '充值流水加载失败')
    } finally {
      if (requestId === requestSequence.current) setLoading(false)
    }
  }

  useEffect(() => {
    listPaymentChannels().then(setChannels).catch(() => setChannels([]))
    const initialKeyword = new URLSearchParams(window.location.search).get('keyword') || ''
    if (initialKeyword) setKeyword(initialKeyword)
    load(1, { keyword: initialKeyword })
  }, [])

  async function confirmOrder(order: RechargeOrder) {
    if (!await confirm({ title: '确认充值到账', message: `确认订单 ${order.id} 已到账，并为用户增加 ${order.credits} 积分？` })) return
    try {
      await confirmRechargeOrder(order.id)
      await load(page)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '订单确认失败')
    }
  }

  async function cancelOrder(order: RechargeOrder) {
    if (!await confirm({ title: '取消充值订单', message: `确定取消订单 ${order.id}？`, tone: 'danger' })) return
    try {
      await cancelRechargeOrder(order.id)
      await load(page)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '订单取消失败')
    }
  }

  function resetFilters() {
    setKeyword('')
    setStatus('')
    setChannel('')
    setFrom('')
    setTo('')
  }

  return (
    <>
      <div className="page-head recharge-page-head">
        <div><span className="eyebrow"><CreditCard size={14} />财务运营</span><h1>充值流水</h1><p>直接查看谁在什么时间通过哪个渠道充值、到账多少积分，并处理待确认订单。</p></div>
        <div className="admin-actions"><Link className="btn btn-secondary" href="/billing"><Settings2 size={16} />支付配置</Link><button className="btn btn-primary" type="button" disabled={loading} onClick={() => load(page)}>{loading ? <span className="spin"><RefreshCw size={16} /></span> : <RefreshCw size={16} />}刷新流水</button></div>
      </div>

      <section className="admin-kpi-grid recharge-kpi-grid" aria-label="充值流水概览">
        <div className="admin-kpi-card"><span>匹配订单</span><strong>{total}</strong><small>按当前筛选范围</small></div>
        <div className="admin-kpi-card"><span>当前页到账</span><strong>{money(pageStats.paidAmount)}</strong><small>{pageStats.paidCount} 笔成功订单</small></div>
        <div className="admin-kpi-card"><span>当前页发放积分</span><strong>{pageStats.paidCredits}</strong><small>仅统计已到账订单</small></div>
        <div className="admin-kpi-card"><span>当前页待处理</span><strong>{pageStats.pending}</strong><small>需要确认或等待回调</small></div>
      </section>

      <section className="admin-card recharge-filter-panel">
        <div className="recharge-filter-grid">
          <label className="field recharge-filter-search"><span>订单或用户</span><div className="filter-input-with-icon"><Search size={16} /><input className="input" value={keyword} onChange={event => setKeyword(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder="邮箱 / 昵称 / 用户 ID / 订单号 / 上游单号" /></div></label>
          <label className="field"><span>订单状态</span><select className="select" value={status} onChange={event => setStatus(event.target.value)}><option value="">全部状态</option><option value="paid">已到账</option><option value="pending">待支付</option><option value="failed">支付失败</option><option value="cancelled">已取消</option><option value="channel_disabled">渠道已关闭</option></select></label>
          <label className="field"><span>支付渠道</span><select className="select" value={channel} onChange={event => setChannel(event.target.value)}><option value="">全部渠道</option>{channels.map(item => <option value={item.code} key={item.id}>{item.name} ({item.code})</option>)}</select></label>
          <label className="field recharge-date-range"><span>创建时间</span><span><input className="input" type="date" value={from} onChange={event => setFrom(event.target.value)} aria-label="开始日期" /><i>至</i><input className="input" type="date" value={to} onChange={event => setTo(event.target.value)} aria-label="结束日期" /></span></label>
          <div className="recharge-filter-actions"><button className="btn" type="button" onClick={resetFilters}>重置</button><button className="btn btn-primary" type="button" disabled={loading} onClick={() => load(1)}><Search size={15} />查询</button></div>
        </div>
      </section>

      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}

      <section className={loading ? 'admin-card table-card recharge-table-card admin-results-loading' : 'admin-card table-card recharge-table-card'} aria-busy={loading}>
        <table className="data-table recharge-table">
          <thead><tr><th>用户</th><th>订单</th><th>渠道</th><th>金额</th><th>积分</th><th>状态</th><th>时间</th><th>备注</th><th>操作</th></tr></thead>
          <tbody>
            {orders.map(order => (
              <tr key={order.id}>
                <td><strong className="table-account"><UserRound size={14} />{order.user_email_display || order.user_email_masked || order.user_id}</strong><div className="muted small-text">{order.user_nickname || order.user_id}</div></td>
                <td><strong>{order.id}</strong><div className="muted small-text">{order.external_trade_no || '暂无上游单号'}</div></td>
                <td><span className="recharge-channel"><CreditCard size={14} />{order.channel_code || '-'}</span></td>
                <td><strong>{money(order.amount_cents, order.currency)}</strong></td>
                <td>{order.credits}</td>
                <td><span className={statusClass(order.status)}>{statusLabel(order.status)}</span></td>
                <td><div className="table-time"><CalendarDays size={14} /><span>{formatDate(order.created_at)}</span></div>{order.paid_at ? <div className="muted small-text">到账 {formatDate(order.paid_at)}</div> : null}</td>
                <td className="recharge-remark-cell">{order.remark || '-'}</td>
                <td>{order.status === 'pending' ? <div className="admin-actions row-action-group"><button className="btn btn-primary btn-compact" type="button" onClick={() => confirmOrder(order)}><CheckCircle2 size={14} />确认到账</button><button className="btn btn-danger btn-compact" type="button" onClick={() => cancelOrder(order)}>取消</button></div> : order.status === 'paid' ? <span className="muted small-text"><CircleDollarSign size={13} /> 已结算</span> : <span className="muted small-text"><Clock3 size={13} /> 已结束</span>}</td>
              </tr>
            ))}
            {!orders.length ? <tr><td colSpan={9} className="muted">{loading ? '正在加载充值流水...' : '暂无充值流水'}</td></tr> : null}
          </tbody>
        </table>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={load} labels={{ previous: '上一页', next: '下一页', page: '页', total: '总数', jump: '跳至' }} />
      </section>
    </>
  )
}
