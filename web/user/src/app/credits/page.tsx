'use client'

import { useEffect, useState } from 'react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { useConfirm } from '@/components/ConfirmProvider'
import { useI18n } from '@/components/i18n'
import {
  cancelRechargeOrder,
  createRechargeOrder,
  getBillingSummary,
  listCreditLedger,
  listPaymentChannels,
  listRechargeOrders,
  listRechargePlans,
  syncRechargeOrder,
  CreditLedger,
  CreateRechargeOrderResponse,
  PageResult,
  PaymentChannel,
  PaymentLink,
  RechargeOrder,
  RechargePlan,
  redeemCode,
  requireLogin
} from '@/lib/api'

interface PendingPayment {
  orderId: string
  status: string
  hint?: string
  links: PaymentLink[]
  createdAt?: number
}

type BillingTab = 'orders' | 'ledger'

const PENDING_PAYMENT_KEY = 'openimage_pending_payment'
const PAYMENT_POLL_INTERVAL_MS = 8000
const PAYMENT_POLL_LIMIT_MS = 10 * 60 * 1000
const BILLING_PAGE_SIZE = 5

function money(cents: number, currency = 'CNY') {
  const symbol = currency === 'CNY' ? '¥' : currency + ' '
  return `${symbol}${(cents / 100).toFixed(2)}`
}

function statusText(status: string, t: (key: string) => string) {
  if (status === 'pending') return t('orderPending')
  if (status === 'paid') return t('orderPaid')
  if (status === 'failed') return t('orderFailed')
  if (status === 'cancelled') return t('orderCancelled')
  if (status === 'channel_disabled') return t('orderChannelDisabled')
  return status
}

function ledgerTypeText(type: string, t: (key: string) => string) {
  if (type === 'recharge_paid') return t('ledgerRechargePaid')
  if (type === 'redeem_code') return t('ledgerRedeemCode')
  if (type === 'creative_generate' || type === 'listing_generate' || type === 'aplus_generate') return t('ledgerGenerateCost')
  if (type === 'image_refund') return t('ledgerImageRefund')
  if (type === 'manual_adjust') return t('ledgerManualAdjust')
  return type || '-'
}

function allowedPlanIds(channel?: PaymentChannel) {
  if (!channel?.config) return { configured: false, ids: [] as string[] }
  try {
    const parsed = JSON.parse(channel.config) as { allowed_plan_ids?: unknown }
    if (!('allowed_plan_ids' in parsed)) return { configured: false, ids: [] as string[] }
    return {
      configured: true,
      ids: Array.isArray(parsed.allowed_plan_ids) ? parsed.allowed_plan_ids.filter((id): id is string => typeof id === 'string' && !!id) : []
    }
  } catch {
    return { configured: false, ids: [] as string[] }
  }
}

function channelFallbackName(code: string, t: (key: string) => string) {
  if (code === 'ykmcn_alipay' || code === 'ykmcn') return t('alipayPayment')
  if (code === 'ykmcn_wxpay') return t('wechatPayment')
  if (code === 'manual') return t('manualPayment')
  if (code === 'afdian') return t('onlinePayment')
  return code || '-'
}

function readPendingPayment() {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(PENDING_PAYMENT_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as PendingPayment
    return parsed?.orderId ? parsed : null
  } catch {
    window.localStorage.removeItem(PENDING_PAYMENT_KEY)
    return null
  }
}

function savePendingPayment(payment: PendingPayment | null) {
  if (typeof window === 'undefined') return
  if (!payment) {
    window.localStorage.removeItem(PENDING_PAYMENT_KEY)
    return
  }
  window.localStorage.setItem(PENDING_PAYMENT_KEY, JSON.stringify(payment))
}

function emptyPage<T>(page = 1): PageResult<T> {
  return { rows: [], total: 0, page, page_size: BILLING_PAGE_SIZE, total_pages: 0 }
}

export default function Page() {
  const [plans, setPlans] = useState<RechargePlan[]>([])
  const [channels, setChannels] = useState<PaymentChannel[]>([])
  const [ordersPage, setOrdersPage] = useState<PageResult<RechargeOrder>>(emptyPage<RechargeOrder>())
  const [ledgerPage, setLedgerPage] = useState<PageResult<CreditLedger>>(emptyPage<CreditLedger>())
  const [credits, setCredits] = useState(0)
  const [selectedChannel, setSelectedChannel] = useState('manual')
  const [message, setMessage] = useState('')
  const [loadingPlan, setLoadingPlan] = useState('')
  const [pendingPayment, setPendingPayment] = useState<PendingPayment | null>(null)
  const [syncingOrder, setSyncingOrder] = useState('')
  const [cancellingOrder, setCancellingOrder] = useState('')
  const [autoSyncing, setAutoSyncing] = useState(false)
  const [redeemInput, setRedeemInput] = useState('')
  const [redeeming, setRedeeming] = useState(false)
  const [showRedeem, setShowRedeem] = useState(false)
  const [activeTab, setActiveTab] = useState<BillingTab>('orders')
  const [orderStatus, setOrderStatus] = useState('all')
  const t = useI18n()
  const confirm = useConfirm()
  const selectedChannelInfo = channels.find(channel => channel.code === selectedChannel)
  const selectedChannelName = selectedChannelInfo?.name || t('selectPaymentFirst')
  const selectedAllowedPlanIds = allowedPlanIds(selectedChannelInfo)
  const visiblePlans = selectedAllowedPlanIds.configured ? plans.filter(plan => selectedAllowedPlanIds.ids.includes(plan.id)) : plans

  function paymentLinks(data: CreateRechargeOrderResponse, channelName: string) {
    const links = (data.payment_links || []).filter(link => link.url)
    if (data.payment_url && !links.some(link => link.url === data.payment_url)) {
      links.unshift({ label: channelName || t('payNow'), url: data.payment_url })
    }
    return links.map((link, index) => ({ ...link, label: link.label || (index === 0 ? channelName || t('payNow') : t('paymentLink')) }))
  }

  function primaryPaymentUrl(links: PaymentLink[]) {
    return links.find(link => !!link.url)?.url || ''
  }

  function orderChannelName(code: string) {
    return channels.find(channel => channel.code === code)?.name || channelFallbackName(code, t)
  }

  async function loadBase() {
    const [summary, planRows, channelRows] = await Promise.all([getBillingSummary(), listRechargePlans(), listPaymentChannels()])
    setCredits(summary.credits)
    setPlans(planRows)
    setChannels(channelRows)
    setSelectedChannel(current => {
      const stillAvailable = channelRows.find(item => item.code === current && item.enabled)
      return stillAvailable?.code || channelRows.find(item => item.enabled)?.code || channelRows[0]?.code || 'manual'
    })
  }

  async function loadOrders(page = ordersPage.page || 1, status = orderStatus) {
    const rows = await listRechargeOrders({ page, pageSize: BILLING_PAGE_SIZE, status: status === 'all' ? '' : status })
    setOrdersPage(rows)
  }

  async function loadLedger(page = ledgerPage.page || 1) {
    const rows = await listCreditLedger({ page, pageSize: BILLING_PAGE_SIZE })
    setLedgerPage(rows)
  }

  async function load(options: { resetRecords?: boolean } = {}) {
    try {
      const nextOrdersPage = options.resetRecords ? 1 : ordersPage.page || 1
      const nextLedgerPage = options.resetRecords ? 1 : ledgerPage.page || 1
      await Promise.all([loadBase(), loadOrders(nextOrdersPage), loadLedger(nextLedgerPage)])
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('creditsLoadFailed'))
    }
  }

  useEffect(() => {
    if (!requireLogin()) return
    const params = new URLSearchParams(window.location.search)
    const returnedOrderId = params.get('order_id') || params.get('out_trade_no')
    const stored = readPendingPayment()
    if (returnedOrderId) {
      setPendingPayment(stored?.orderId === returnedOrderId ? stored : { orderId: returnedOrderId, status: 'pending', hint: t('paymentDefaultHint'), links: [], createdAt: Date.now() })
    } else if (stored) {
      setPendingPayment(stored)
    }
    load({ resetRecords: true })
  }, [])

  useEffect(() => {
    if (!requireLogin()) return
    loadOrders(1, orderStatus).catch(error => setMessage(error instanceof Error ? error.message : t('creditsLoadFailed')))
  }, [orderStatus])

  useEffect(() => {
    if (!pendingPayment || pendingPayment.status !== 'pending') return
    savePendingPayment(pendingPayment)
    const startedAt = pendingPayment.createdAt || Date.now()
    const run = () => {
      if (Date.now() - startedAt > PAYMENT_POLL_LIMIT_MS) {
        syncPayment(pendingPayment.orderId, true)
        return
      }
      syncPayment(pendingPayment.orderId, true)
    }
    const timer = window.setInterval(run, PAYMENT_POLL_INTERVAL_MS)
    const first = window.setTimeout(run, 3000)
    const focus = () => run()
    const visibility = () => { if (!document.hidden) run() }
    window.addEventListener('focus', focus)
    document.addEventListener('visibilitychange', visibility)
    return () => {
      window.clearInterval(timer)
      window.clearTimeout(first)
      window.removeEventListener('focus', focus)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [pendingPayment?.orderId, pendingPayment?.status])

  async function choose(plan: RechargePlan) {
    const confirmed = await confirm({
      title: t('confirmPaymentTitle'),
      message: t('confirmPaymentMessage')
        .replace('{channel}', selectedChannelInfo?.name || selectedChannelName)
        .replace('{credits}', String(plan.credits))
        .replace('{amount}', money(plan.price_cents, plan.currency)),
      confirmText: t('confirmAndPay'),
      cancelText: t('reviewAgain')
    })
    if (!confirmed) return
    setLoadingPlan(plan.id)
    setMessage('')
    try {
      const data = await createRechargeOrder({ planId: plan.id, channelCode: selectedChannel, turnstileToken: '' })
      const links = paymentLinks(data, selectedChannelInfo?.name || selectedChannelName)
      const nextPending = links.length ? { orderId: data.order_id, status: data.status, hint: data.payment_hint || data.message, links, createdAt: Date.now() } : null
      setPendingPayment(nextPending)
      savePendingPayment(nextPending)
      await loadBase()
      await loadOrders(1, orderStatus)
      setActiveTab('orders')
      const payUrl = primaryPaymentUrl(links)
      setMessage(`${t('orderCreated')} · ${t('orderId')}: ${data.order_id} · ${payUrl ? t('paymentOpeningNotice') : nextPending ? t('paymentAutoSyncHint') : statusText(data.status, t)}`)
      if (payUrl) {
        window.location.assign(payUrl)
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('createOrderFailed'))
    } finally {
      setLoadingPlan('')
    }
  }

  async function syncPayment(orderId: string, silent = false) {
    if (silent) {
      setAutoSyncing(true)
    } else {
      setSyncingOrder(orderId)
      setMessage('')
    }
    try {
      const data = await syncRechargeOrder(orderId)
      await loadBase()
      await Promise.all([loadOrders(ordersPage.page || 1, orderStatus), loadLedger(ledgerPage.page || 1)])
      if (data.status === 'paid') {
        setPendingPayment(null)
        savePendingPayment(null)
        setMessage(`${t('paymentSuccessNotice')} · ${t('balance')}: ${data.balance_after ?? '--'}`)
        window.dispatchEvent(new Event('openimage:credits-updated'))
      } else if (data.status === 'failed') {
        setPendingPayment(null)
        savePendingPayment(null)
        setMessage(t('paymentFailedNotice'))
      } else {
        if (!silent) setMessage(`${t('syncOrderSuccess')} · ${t('status')}: ${statusText(data.status, t)}`)
        setPendingPayment(current => {
          if (current?.orderId !== orderId) return current
          const updated = { ...current, status: data.status }
          savePendingPayment(updated)
          return updated
        })
      }
    } catch (error) {
      if (!silent) setMessage(error instanceof Error ? error.message : t('syncOrderFailed'))
    } finally {
      if (silent) setAutoSyncing(false)
      else setSyncingOrder('')
    }
  }

  async function cancelPayment(orderId: string) {
    const confirmed = await confirm({
      title: t('cancelPaymentTitle'),
      message: t('cancelPaymentMessage'),
      confirmText: t('cancelPayment'),
      cancelText: t('reviewAgain'),
      tone: 'danger'
    })
    if (!confirmed) return
    setCancellingOrder(orderId)
    setMessage('')
    try {
      const data = await cancelRechargeOrder(orderId)
      await loadBase()
      await loadOrders(ordersPage.page || 1, orderStatus)
      if (pendingPayment?.orderId === orderId) {
        setPendingPayment(null)
        savePendingPayment(null)
      }
      if (data.status === 'paid') {
        setMessage(`${t('paymentSuccessNotice')} · ${t('balance')}: ${data.balance_after ?? '--'}`)
        window.dispatchEvent(new Event('openimage:credits-updated'))
      } else {
        setMessage(data.message || t('paymentCancelledNotice'))
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('cancelPaymentFailed'))
    } finally {
      setCancellingOrder('')
    }
  }

  async function submitRedeem() {
    setRedeeming(true)
    setMessage('')
    try {
      const data = await redeemCode(redeemInput)
      setRedeemInput('')
      setCredits(data.balance_after)
      setMessage(`${t('redeemSuccessPrefix')}${data.credits} ${t('creditsLabel')}`)
      window.dispatchEvent(new Event('openimage:credits-updated'))
      await Promise.all([loadBase(), loadLedger(1)])
      setActiveTab('ledger')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('redeemFailed'))
    } finally {
      setRedeeming(false)
    }
  }

  return (
    <Shell>
      <UserAuthGate title={t('creditsTitle')} description={t('creditsDesc')}>
        <div className="page-head">
          <div>
            <h1>{t('creditsTitle')}</h1>
            <p>{t('creditsDesc')}</p>
          </div>
          <div className="page-actions"><button className="btn btn-secondary" onClick={() => load({ resetRecords: true })}>{t('refreshData')}</button></div>
        </div>
        {message ? <div className="notice">{message}</div> : null}

        <section className="billing-hero panel">
          <div className="billing-balance">
            <span>{t('currentCredits')}</span>
            <strong>{credits}</strong>
            <button className="text-button" type="button" onClick={() => setShowRedeem(value => !value)}>{t('redeemCodeTitle')}</button>
          </div>
          <div className="billing-purchase">
            <div className="billing-mini-head">
              <div>
                <h2>{t('paymentChannels')}</h2>
                <p>{t('paymentPickHint')}</p>
              </div>
            </div>
            <div className="billing-channel-row">
              {channels.map(channel => (
                <button key={channel.code} disabled={!channel.enabled} className={selectedChannel === channel.code ? 'btn btn-primary' : 'btn btn-secondary'} onClick={() => setSelectedChannel(channel.code)}>
                  {channel.name}
                </button>
              ))}
            </div>
            <div className="billing-plan-grid">
              {visiblePlans.map(plan => (
                <article className="billing-plan-card" key={plan.id}>
                  <div>
                    <span>{plan.name}</span>
                    <strong>{plan.credits} {t('creditsLabel')}</strong>
                    <small>{money(plan.price_cents, plan.currency)}</small>
                  </div>
                  <button className="btn btn-primary" disabled={!!loadingPlan || !selectedChannelInfo?.enabled} onClick={() => choose(plan)}>
                    {loadingPlan === plan.id ? t('submiting') : `${t('payNow')} · ${selectedChannelName}`}
                  </button>
                </article>
              ))}
              {!visiblePlans.length ? <div className="empty-state">{t('noPlansForPayment')}</div> : null}
            </div>
            {showRedeem ? (
              <div className="billing-redeem-box">
                <input className="input" value={redeemInput} onChange={event => setRedeemInput(event.target.value)} placeholder={t('redeemPlaceholder')} />
                <button className="btn btn-secondary" disabled={redeeming || !redeemInput.trim()} onClick={submitRedeem}>{redeeming ? t('redeeming') : t('redeemNow')}</button>
              </div>
            ) : null}
          </div>
        </section>

        {pendingPayment ? (
          <section className="payment-panel billing-payment-strip" aria-live="polite">
            <div>
              <span className={pendingPayment.status === 'paid' ? 'status status-ok' : 'status status-warn'}>{statusText(pendingPayment.status, t)}</span>
              <strong>{t('paymentPanelTitle')}</strong>
              <p>{pendingPayment.hint || t('paymentDefaultHint')}</p>
              <small>{t('orderId')}: {pendingPayment.orderId}{autoSyncing ? ` · ${t('paymentAutoSyncing')}` : ''}</small>
            </div>
            <div className="payment-actions">
              {pendingPayment.links.length ? pendingPayment.links.map((link, index) => (
                <a key={`${link.url}-${index}`} className={index === 0 ? 'btn btn-primary payment-button' : 'btn btn-secondary payment-button'} href={link.url} target="_blank" rel="noreferrer">
                  {index === 0 ? `${t('goPay')} ${link.label || t('payNow')}` : link.label}
                </a>
              )) : <span className="muted payment-no-link">{t('paymentPanelNoLink')}</span>}
              <button className="btn btn-secondary payment-button" disabled={syncingOrder === pendingPayment.orderId || cancellingOrder === pendingPayment.orderId} onClick={() => syncPayment(pendingPayment.orderId)}>
                {syncingOrder === pendingPayment.orderId ? t('syncingPaymentStatus') : t('syncPaymentStatus')}
              </button>
              <button className="btn btn-danger payment-button" disabled={cancellingOrder === pendingPayment.orderId || syncingOrder === pendingPayment.orderId} onClick={() => cancelPayment(pendingPayment.orderId)}>
                {cancellingOrder === pendingPayment.orderId ? t('cancelingPayment') : t('cancelPayment')}
              </button>
            </div>
          </section>
        ) : null}

        <section className="panel billing-record-panel">
          <div className="billing-record-tabs">
            <button className={activeTab === 'orders' ? 'billing-tab-active' : ''} onClick={() => setActiveTab('orders')} type="button">{t('rechargeOrders')} <span>{ordersPage.total}</span></button>
            <button className={activeTab === 'ledger' ? 'billing-tab-active' : ''} onClick={() => setActiveTab('ledger')} type="button">{t('creditLedger')} <span>{ledgerPage.total}</span></button>
          </div>
          {activeTab === 'orders' ? (
            <div className="billing-record-body">
              <div className="billing-filter-row">
                {['all', 'pending', 'paid', 'failed'].map(status => (
                  <button key={status} className={orderStatus === status ? 'btn btn-primary' : 'btn btn-secondary'} type="button" onClick={() => setOrderStatus(status)}>
                    {status === 'all' ? t('allStatuses') : statusText(status, t)}
                  </button>
                ))}
              </div>
              <div className="billing-record-list">
                {ordersPage.rows.map(order => (
                  <article className="billing-record-card" key={order.id}>
                    <div className="billing-record-main">
                      <span>{orderChannelName(order.channel_code)}</span>
                      <strong>{order.credits} {t('creditsLabel')} · {money(order.amount_cents, order.currency)}</strong>
                      <small>{new Date(order.created_at).toLocaleString()}</small>
                      <small className="billing-record-id">{order.id}</small>
                    </div>
                    <div className="billing-record-side">
                      <span className={order.status === 'paid' ? 'status status-ok' : order.status === 'failed' || order.status === 'cancelled' ? 'status status-bad' : 'status status-warn'}>{statusText(order.status, t)}</span>
                      {order.status === 'pending' && pendingPayment?.orderId !== order.id ? (
                        <div className="billing-row-actions">
                          <button className="btn btn-secondary" disabled={syncingOrder === order.id || cancellingOrder === order.id} onClick={() => syncPayment(order.id)}>
                            {syncingOrder === order.id ? t('syncingPaymentStatus') : t('syncPaymentStatus')}
                          </button>
                          <button className="btn btn-danger" disabled={cancellingOrder === order.id || syncingOrder === order.id} onClick={() => cancelPayment(order.id)}>
                            {cancellingOrder === order.id ? t('cancelingPayment') : t('cancelPayment')}
                          </button>
                        </div>
                      ) : null}
                      {order.status === 'pending' && pendingPayment?.orderId === order.id ? <small className="muted">{t('pendingPaymentAbove')}</small> : null}
                    </div>
                  </article>
                ))}
                {!ordersPage.rows.length ? <div className="empty-state">{t('noOrders')}</div> : null}
              </div>
              <BillingPager page={ordersPage.page} totalPages={ordersPage.total_pages} onPrev={() => loadOrders(Math.max(1, ordersPage.page - 1), orderStatus)} onNext={() => loadOrders(Math.min(Math.max(1, ordersPage.total_pages), ordersPage.page + 1), orderStatus)} t={t} />
            </div>
          ) : (
            <div className="billing-record-body">
              <div className="billing-record-list">
                {ledgerPage.rows.map(row => (
                  <article className="billing-record-card" key={row.id}>
                    <div className="billing-record-main">
                      <span>{ledgerTypeText(row.biz_type, t)}</span>
                      <strong>{row.remark || row.biz_id}</strong>
                      <small>{new Date(row.created_at).toLocaleString()}</small>
                    </div>
                    <div className="billing-record-side">
                      <span className={row.change_amount >= 0 ? 'billing-change billing-change-plus' : 'billing-change billing-change-minus'}>{row.change_amount > 0 ? '+' : ''}{row.change_amount}</span>
                      <small>{t('balance')}: {row.balance_after}</small>
                    </div>
                  </article>
                ))}
                {!ledgerPage.rows.length ? <div className="empty-state">{t('noLedger')}</div> : null}
              </div>
              <BillingPager page={ledgerPage.page} totalPages={ledgerPage.total_pages} onPrev={() => loadLedger(Math.max(1, ledgerPage.page - 1))} onNext={() => loadLedger(Math.min(Math.max(1, ledgerPage.total_pages), ledgerPage.page + 1))} t={t} />
            </div>
          )}
        </section>
      </UserAuthGate>
    </Shell>
  )
}

function BillingPager({ page, totalPages, onPrev, onNext, t }: { page: number; totalPages: number; onPrev: () => void; onNext: () => void; t: (key: string) => string }) {
  if (totalPages <= 1) return null
  return (
    <div className="billing-pager">
      <button className="btn btn-secondary" type="button" disabled={page <= 1} onClick={onPrev}>{t('prevPage')}</button>
      <span>{page} / {totalPages}</span>
      <button className="btn btn-secondary" type="button" disabled={page >= totalPages} onClick={onNext}>{t('nextPage')}</button>
    </div>
  )
}
