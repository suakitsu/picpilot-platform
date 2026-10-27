'use client'

import { useEffect, useState } from 'react'
import { Pagination } from '@/components/Pagination'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import {
  cancelRechargeOrder,
  confirmRechargeOrder,
  createRedemptionCodes,
  listPaymentChannels,
  listRechargeOrders,
  listRechargePlans,
  listRedemptionCodes,
  PaymentChannel,
  RechargeOrder,
  RechargePlan,
  RedemptionCode,
  updatePaymentChannel,
  updateRechargePlan
} from '@/lib/api'

function money(cents: number, currency = 'CNY') {
  const symbol = currency === 'CNY' ? '¥' : `${currency} `
  return `${symbol}${(Number(cents || 0) / 100).toFixed(2)}`
}

function statusLabel(status: string, t: (key: string) => string) {
  if (status === 'pending') return t('billing.status.pending')
  if (status === 'paid') return t('billing.status.paid')
  if (status === 'failed') return t('billing.status.failed')
  if (status === 'cancelled') return t('billing.status.cancelled')
  if (status === 'channel_disabled') return t('billing.status.channelDisabled')
  return status
}

function statusClass(status: string) {
  if (status === 'paid') return 'status-good'
  if (status === 'pending') return 'status-warn'
  return 'status-bad'
}

function codeStatusLabel(status?: string) {
  if (status === 'active') return '可兑换'
  if (status === 'used_up') return '已用完'
  if (status === 'expired') return '已过期'
  if (status === 'disabled') return '已停用'
  return status || '-'
}

function codeStatusClass(status?: string) {
  if (status === 'active') return 'status-good'
  if (status === 'used_up' || status === 'expired') return 'status-warn'
  return 'status-bad'
}

export default function Page() {
  const confirm = useConfirm()
  const [plans, setPlans] = useState<RechargePlan[]>([])
  const [channels, setChannels] = useState<PaymentChannel[]>([])
  const [orders, setOrders] = useState<RechargeOrder[]>([])
  const [codes, setCodes] = useState<RedemptionCode[]>([])
  const [message, setMessage] = useState('')
  const [orderPage, setOrderPage] = useState(1)
  const [orderTotal, setOrderTotal] = useState(0)
  const [orderTotalPages, setOrderTotalPages] = useState(0)
  const [orderKeyword, setOrderKeyword] = useState('')
  const [orderStatus, setOrderStatus] = useState('')
  const [codePage, setCodePage] = useState(1)
  const [codeTotal, setCodeTotal] = useState(0)
  const [codeTotalPages, setCodeTotalPages] = useState(0)
  const [codeKeyword, setCodeKeyword] = useState('')
  const [codeStatus, setCodeStatus] = useState('')
  const [codeCredits, setCodeCredits] = useState(20)
  const [codeMaxUses, setCodeMaxUses] = useState(1)
  const [codeQuantity, setCodeQuantity] = useState(1)
  const [codeValidDays, setCodeValidDays] = useState(30)
  const [codePrefix, setCodePrefix] = useState('PP')
  const [codeNote, setCodeNote] = useState('财务后台生成')
  const [creatingCodes, setCreatingCodes] = useState(false)
  const orderPageSize = 20
  const t = useI18n()

  async function loadOrders(nextPage = orderPage, nextCodePage = codePage) {
    try {
      const [planRows, channelRows, orderRows, codeRows] = await Promise.all([
        listRechargePlans(),
        listPaymentChannels(),
        listRechargeOrders({ keyword: orderKeyword, status: orderStatus, page: nextPage, pageSize: orderPageSize }),
        listRedemptionCodes({ keyword: codeKeyword, status: codeStatus, page: nextCodePage, pageSize: orderPageSize })
      ])
      setPlans(planRows)
      setChannels(channelRows)
      setOrders(orderRows.rows)
      setOrderPage(orderRows.page)
      setOrderTotal(orderRows.total)
      setOrderTotalPages(orderRows.total_pages)
      setCodes(codeRows.rows)
      setCodePage(codeRows.page)
      setCodeTotal(codeRows.total)
      setCodeTotalPages(codeRows.total_pages)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('billing.loadFailed'))
    }
  }

  useEffect(() => { loadOrders(1) }, [])

  function patchPlan(id: string, patch: Partial<RechargePlan>) {
    setPlans(current => current.map(plan => plan.id === id ? { ...plan, ...patch } : plan))
  }

  function patchChannel(id: string, patch: Partial<PaymentChannel>) {
    setChannels(current => current.map(channel => channel.id === id ? { ...channel, ...patch } : channel))
  }

  async function savePlan(plan: RechargePlan) {
    if (!await confirm({ title: t('billing.plans'), message: `${t('billing.confirmSavePlan')} ${plan.name}？` })) return
    try {
      const saved = await updateRechargePlan(plan)
      patchPlan(saved.id, saved)
      setMessage(`${t('billing.planSaved')}${saved.name}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('billing.planSaveFailed'))
    }
  }

  async function saveChannel(channel: PaymentChannel) {
    if (!await confirm({ title: t('billing.channels'), message: `${t('billing.confirmSaveChannel')} ${channel.name}？` })) return
    try {
      const saved = await updatePaymentChannel(channel)
      patchChannel(saved.id, saved)
      setMessage(`${t('billing.channelSaved')}${saved.name}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('billing.channelSaveFailed'))
    }
  }

  async function confirmOrder(orderId: string) {
    if (!await confirm({ title: t('billing.confirmPaid'), message: `${t('billing.confirmPaid')} ${t('common.orderId')} ${orderId}？` })) return
    try {
      await confirmRechargeOrder(orderId)
      setMessage(`${t('billing.orderConfirmed')}${orderId}`)
      await loadOrders(orderPage)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('billing.orderConfirmFailed'))
    }
  }

  async function cancelOrder(orderId: string) {
    if (!await confirm({ title: t('billing.cancel'), message: `${t('billing.cancel')} ${t('common.orderId')} ${orderId}？`, tone: 'danger' })) return
    try {
      await cancelRechargeOrder(orderId)
      setMessage(`${t('billing.orderCancelled')}${orderId}`)
      await loadOrders(orderPage)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('billing.orderCancelFailed'))
    }
  }

  async function generateCodes() {
    if (!await confirm({ title: '生成兑换码', message: `生成 ${codeQuantity} 个，每个 ${codeCredits} 积分，有效 ${codeValidDays} 天？` })) return
    setCreatingCodes(true)
    setMessage('')
    try {
      const created = await createRedemptionCodes({
        credits: codeCredits,
        maxUses: codeMaxUses,
        quantity: codeQuantity,
        validDays: codeValidDays,
        prefix: codePrefix,
        note: codeNote,
        benefitType: '兑换码积分'
      })
      setMessage(`已生成 ${created.length} 个兑换码`)
      await loadOrders(orderPage, 1)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '兑换码生成失败')
    } finally {
      setCreatingCodes(false)
    }
  }

  return (
    <>
      <div className="page-head"><div><h1>{t('billing.title')}</h1><p>{t('billing.desc')}</p></div><button className="btn btn-secondary" onClick={() => loadOrders(1)}>{t('common.refresh')}</button></div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}

      <section className="admin-card table-card operation-card">
        <div className="section-head"><div><h2>{t('billing.plans')}</h2><p>{t('billing.plansDesc')}</p></div></div>
        <table className="data-table"><thead><tr><th>{t('common.name')}</th><th>{t('common.credits')}</th><th>{t('common.priceCents')}</th><th>{t('common.currency')}</th><th>{t('common.enabled')}</th><th>{t('common.sort')}</th><th>{t('common.actions')}</th></tr></thead><tbody>{plans.map(plan => <tr key={plan.id}><td><input className="input" value={plan.name} onChange={event => patchPlan(plan.id, { name: event.target.value })} /></td><td><input className="input" value={plan.credits} onChange={event => patchPlan(plan.id, { credits: Number(event.target.value) })} /></td><td><input className="input" value={plan.price_cents} onChange={event => patchPlan(plan.id, { price_cents: Number(event.target.value) })} /></td><td><input className="input" value={plan.currency} onChange={event => patchPlan(plan.id, { currency: event.target.value })} /></td><td><select className="select" value={plan.enabled ? 'true' : 'false'} onChange={event => patchPlan(plan.id, { enabled: event.target.value === 'true' })}><option value="true">{t('common.enabled')}</option><option value="false">{t('common.disabled')}</option></select></td><td><input className="input" value={plan.sort_order} onChange={event => patchPlan(plan.id, { sort_order: Number(event.target.value) })} /></td><td><button className="btn" onClick={() => savePlan(plan)}>{t('common.save')}</button></td></tr>)}{!plans.length ? <tr><td colSpan={7} className="muted">{t('billing.noPlans')}</td></tr> : null}</tbody></table>
      </section>

      <section className="admin-card table-card section operation-card">
        <div className="section-head"><div><h2>{t('billing.channels')}</h2><p>{t('billing.channelsDesc')}</p></div></div>
        <table className="data-table"><thead><tr><th>{t('common.name')}</th><th>{t('common.code')}</th><th>{t('common.visible')}</th><th>{t('common.available')}</th><th>{t('common.sort')}</th><th>{t('common.configJson')}</th><th>{t('common.actions')}</th></tr></thead><tbody>{channels.map(channel => <tr key={channel.id}><td><input className="input" value={channel.name} onChange={event => patchChannel(channel.id, { name: event.target.value })} /></td><td>{channel.code}</td><td><select className="select" value={channel.visible ? 'true' : 'false'} onChange={event => patchChannel(channel.id, { visible: event.target.value === 'true' })}><option value="true">{t('common.visible')}</option><option value="false">{t('common.hidden')}</option></select></td><td><select className="select" value={channel.enabled ? 'true' : 'false'} onChange={event => patchChannel(channel.id, { enabled: event.target.value === 'true' })}><option value="true">{t('common.available')}</option><option value="false">{t('common.closed')}</option></select></td><td><input className="input" value={channel.sort_order} onChange={event => patchChannel(channel.id, { sort_order: Number(event.target.value) })} /></td><td><input className="input" value={channel.config || '{}'} onChange={event => patchChannel(channel.id, { config: event.target.value })} /></td><td><button className="btn" onClick={() => saveChannel(channel)}>{t('common.save')}</button></td></tr>)}{!channels.length ? <tr><td colSpan={7} className="muted">{t('billing.noChannels')}</td></tr> : null}</tbody></table>
      </section>

      <section className="admin-card operation-card operation-primary">
        <div className="section-head"><div><h2>财务兑换码</h2><p>生成和查询普通积分兑换码，覆盖后端 billing/redemption-codes 管理接口。</p></div><button className="btn btn-primary" disabled={creatingCodes} onClick={generateCodes}>{creatingCodes ? '生成中...' : '生成兑换码'}</button></div>
        <div className="admin-form-grid">
          <label className="field"><span>积分</span><input className="input" type="number" min={1} value={codeCredits} onChange={event => setCodeCredits(Number(event.target.value))} /></label>
          <label className="field"><span>每码可用次数</span><input className="input" type="number" min={1} max={10000} value={codeMaxUses} onChange={event => setCodeMaxUses(Number(event.target.value))} /></label>
          <label className="field"><span>生成数量</span><input className="input" type="number" min={1} max={200} value={codeQuantity} onChange={event => setCodeQuantity(Number(event.target.value))} /></label>
          <label className="field"><span>有效天数</span><input className="input" type="number" min={1} value={codeValidDays} onChange={event => setCodeValidDays(Number(event.target.value))} /></label>
          <label className="field"><span>前缀</span><input className="input" value={codePrefix} onChange={event => setCodePrefix(event.target.value)} /></label>
          <label className="field"><span>备注</span><input className="input" value={codeNote} onChange={event => setCodeNote(event.target.value)} /></label>
        </div>
      </section>

      <section className="admin-card table-card section">
        <div className="section-head"><div><h2>兑换码记录</h2><p>按状态、关键词查询所有财务兑换码。</p></div></div>
        <div className="filter-bar action-filter-bar"><input className="input" value={codeKeyword} onChange={event => { setCodeKeyword(event.target.value); setCodePage(1) }} onKeyDown={event => { if (event.key === 'Enter') loadOrders(orderPage, 1) }} placeholder="兑换码 / 备注 / 类型" /><select className="select" value={codeStatus} onChange={event => { setCodeStatus(event.target.value); setCodePage(1) }}><option value="">全部状态</option><option value="active">可兑换</option><option value="used_up">已用完</option><option value="expired">已过期</option><option value="disabled">已停用</option></select><button className="btn btn-secondary" onClick={() => loadOrders(orderPage, 1)}>查询</button></div>
        <table className="data-table"><thead><tr><th>兑换码</th><th>积分</th><th>使用</th><th>状态</th><th>有效期</th><th>备注</th><th>创建时间</th></tr></thead><tbody>{codes.map(code => <tr key={code.id}><td><strong>{code.code}</strong><div className="muted small-text">{code.id}</div></td><td>{code.credits}</td><td>{code.used_count}/{code.max_uses}</td><td><span className={codeStatusClass(code.status)}>{codeStatusLabel(code.status)}</span></td><td>{code.expires_at ? new Date(code.expires_at).toLocaleString() : '长期'}</td><td>{code.note || '-'}</td><td>{new Date(code.created_at).toLocaleString()}</td></tr>)}{!codes.length ? <tr><td colSpan={7} className="muted">暂无兑换码</td></tr> : null}</tbody></table>
        <Pagination page={codePage} total={codeTotal} pageSize={orderPageSize} totalPages={codeTotalPages} onPageChange={nextPage => loadOrders(orderPage, nextPage)} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>

      <section className="admin-card table-card section operation-card operation-sensitive">
        <div className="section-head"><div><h2>{t('billing.orders')}</h2><p>{t('billing.ordersDesc')}</p></div></div>
        <div className="filter-bar action-filter-bar"><input className="input" value={orderKeyword} onChange={event => { setOrderKeyword(event.target.value); setOrderPage(1) }} onKeyDown={event => { if (event.key === 'Enter') loadOrders(1) }} placeholder={t('searchTask')} /><select className="select" value={orderStatus} onChange={event => { setOrderStatus(event.target.value); setOrderPage(1) }}><option value="">{t('allStatus')}</option><option value="pending">{t('billing.status.pending')}</option><option value="paid">{t('billing.status.paid')}</option><option value="failed">{t('billing.status.failed')}</option><option value="cancelled">{t('billing.status.cancelled')}</option></select><button className="btn btn-secondary" onClick={() => loadOrders(1)}>{t('common.refresh')}</button></div>
        <table className="data-table"><thead><tr><th>{t('common.orderId')}</th><th>{t('common.user')}</th><th>{t('common.createdAt')}</th><th>{t('common.channel')}</th><th>{t('common.amount')}</th><th>{t('common.credits')}</th><th>{t('common.status')}</th><th>{t('common.remark')}</th><th>{t('common.actions')}</th></tr></thead><tbody>{orders.map(order => <tr key={order.id}><td>{order.id}</td><td>{order.user_email_masked || order.user_id}</td><td>{new Date(order.created_at).toLocaleString()}</td><td>{order.channel_code}</td><td>{money(order.amount_cents, order.currency)}</td><td>{order.credits}</td><td><span className={statusClass(order.status)}>{statusLabel(order.status, t)}</span></td><td>{order.remark || '-'}</td><td>{order.status === 'pending' ? <div className="admin-actions row-action-group"><button className="btn btn-primary" onClick={() => confirmOrder(order.id)}>{t('billing.confirmPaid')}</button><button className="btn btn-danger" onClick={() => cancelOrder(order.id)}>{t('billing.cancel')}</button></div> : '-'}</td></tr>)}{!orders.length ? <tr><td colSpan={9} className="muted">{t('billing.noOrders')}</td></tr> : null}</tbody></table>
        <Pagination page={orderPage} total={orderTotal} pageSize={orderPageSize} totalPages={orderTotalPages} onPageChange={loadOrders} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>

    </>
  )
}
