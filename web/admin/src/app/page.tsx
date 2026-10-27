'use client'

import Link from 'next/link'
import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import ReactECharts from 'echarts-for-react'
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  BarChart3,
  CircleDollarSign,
  Clock3,
  CreditCard,
  Film,
  Filter,
  Image as ImageIcon,
  ReceiptText,
  ShieldAlert,
  Users,
  WalletCards,
  type LucideIcon
} from 'lucide-react'
import { useI18n, useLocale } from '@/components/i18n'
import { AdminAnalyticsFilters, DashboardMetrics, DashboardTrendPoint, getDashboard, getToken } from '@/lib/api'

type TrendPoint = DashboardTrendPoint
type NumericTrendField = 'recharge' | 'usage' | 'image_usage' | 'video_usage' | 'users' | 'revenue' | 'credits' | 'failures' | 'image_failures' | 'video_failures' | 'cost' | 'image_cost' | 'video_cost'

const COLORS = {
  blue: '#2563eb',
  teal: '#0f766e',
  purple: '#7c3aed',
  amber: '#d97706',
  red: '#dc2626',
  slate: '#64748b',
  green: '#059669'
}

function formatNumber(value: number, locale = 'zh-CN') {
  return new Intl.NumberFormat(locale).format(Math.round(value || 0))
}

function formatMoney(value: number, locale = 'zh-CN', digits = 0) {
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY', maximumFractionDigits: digits, minimumFractionDigits: digits }).format(Number(value || 0))
}

function formatPercent(value: number) {
  return `${Number.isFinite(value) ? value.toFixed(1) : '0.0'}%`
}

function trendValues(data: TrendPoint[], field: NumericTrendField) {
  return data.map((item) => {
    const value = item[field]
    if (value !== undefined && value !== null) return Number(value || 0)
    if (field === 'image_usage') return Number(item.usage || 0)
    if (field === 'image_failures') return Number(item.failures || 0)
    if (field === 'image_cost') return Number(item.cost || 0)
    return 0
  })
}

function totalTrend(data: TrendPoint[], field: NumericTrendField) {
  return trendValues(data, field).reduce((sum, value) => sum + value, 0)
}

function taskTotal(metrics: DashboardMetrics) {
  return Number(metrics.total_tasks ?? ((metrics.image_tasks || 0) + (metrics.video_tasks || 0)))
}

function completedTasks(metrics: DashboardMetrics) {
  return Math.max(taskTotal(metrics) - (metrics.active_tasks || 0) - (metrics.failed_tasks || 0), 0)
}

function percent(part: number, total: number) {
  return total > 0 ? (part / total) * 100 : 0
}

function chartTextColor() {
  if (typeof window === 'undefined') return '#667085'
  return document.documentElement.dataset.theme === 'dark' ? '#d0d5dd' : '#667085'
}

function chartTitleColor() {
  if (typeof window === 'undefined') return '#101828'
  return document.documentElement.dataset.theme === 'dark' ? '#f8fafc' : '#101828'
}

function chartLineColor() {
  if (typeof window === 'undefined') return 'rgba(102,112,133,.18)'
  return document.documentElement.dataset.theme === 'dark' ? 'rgba(152,162,179,.16)' : 'rgba(102,112,133,.18)'
}

function tooltipFormatter(params: unknown, locale: string, moneySeries = new Set<string>()) {
  const rows = Array.isArray(params) ? params : [params]
  const title = (rows[0] as { axisValueLabel?: string; name?: string } | undefined)?.axisValueLabel || (rows[0] as { name?: string } | undefined)?.name || ''
  const lines = rows.map((item) => {
    const point = item as { marker?: string; seriesName?: string; value?: number | string | Array<number | string> }
    const rawValue = Array.isArray(point.value) ? point.value[1] : point.value
    const value = Number(rawValue || 0)
    const formatted = moneySeries.has(point.seriesName || '') ? formatMoney(value, locale) : formatNumber(value, locale)
    return `${point.marker || ''}${point.seriesName || ''}: ${formatted}`
  })
  return [title, ...lines].filter(Boolean).join('<br/>')
}

function StatCard({ label, value, note, icon: Icon, tone }: { label: string; value: string | number; note: string; icon: LucideIcon; tone: string }) {
  return (
    <article className="report-kpi-card dashboard-stat-card" style={{ '--stat-tone': tone } as CSSProperties}>
      <div className="stat-card-head">
        <div className="metric-label">{label}</div>
        <span className="stat-card-icon"><Icon size={16} strokeWidth={2.2} /></span>
      </div>
      <div className="metric-value">{value}</div>
      <p className="metric-note">{note}</p>
      <span className="stat-card-line" />
    </article>
  )
}

function ChartPanel({ title, desc, type, children, className = '' }: { title: string; desc: string; type: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={`report-chart-card dashboard-chart-panel ${className}`}>
      <div className="report-chart-head">
        <div>
          <span className="chart-type-label">{type}</span>
          <h2>{title}</h2>
          <p>{desc}</p>
        </div>
      </div>
      <div className="report-chart-body">{children}</div>
    </section>
  )
}

function EmptyChart({ show, text }: { show: boolean; text: string }) {
  if (!show) return null
  return <div className="standard-chart-empty">{text}</div>
}

function RevenueLineChart({ data, locale, t }: { data: TrendPoint[]; locale: string; t: (key: string) => string }) {
  const labels = data.map((item) => item.label)
  const rechargeName = t('dashboard.rechargeLegend')
  const revenueName = t('dashboard.revenueLegend')
  const costName = t('dashboard.actualCost')
  const textColor = chartTextColor()
  const lineColor = chartLineColor()
  const option = {
    color: [COLORS.blue, COLORS.teal, COLORS.red],
    tooltip: {
      trigger: 'axis',
      confine: true,
      formatter: (params: unknown) => tooltipFormatter(params, locale, new Set([rechargeName, revenueName, costName]))
    },
    legend: {
      top: 0,
      right: 8,
      textStyle: { color: textColor, fontWeight: 700 }
    },
    grid: { left: 68, right: 28, top: 54, bottom: 48 },
    xAxis: {
      type: 'category',
      data: labels,
      axisLabel: { color: textColor },
      axisLine: { lineStyle: { color: lineColor } },
      axisTick: { alignWithLabel: true }
    },
    yAxis: {
      type: 'value',
      axisLabel: { color: textColor, formatter: (value: number) => `¥${value}` },
      splitLine: { lineStyle: { color: lineColor } }
    },
    series: [
      {
        name: rechargeName,
        type: 'line',
        smooth: true,
        symbolSize: 7,
        lineStyle: { width: 3 },
        areaStyle: { opacity: 0.08 },
        data: trendValues(data, 'recharge')
      },
      {
        name: revenueName,
        type: 'line',
        smooth: true,
        symbolSize: 7,
        lineStyle: { width: 3 },
        areaStyle: { opacity: 0.06 },
        data: trendValues(data, 'revenue')
      },
      {
        name: costName,
        type: 'line',
        smooth: true,
        symbolSize: 6,
        lineStyle: { width: 2, type: 'dashed' },
        data: trendValues(data, 'cost')
      }
    ]
  }
  return (
    <ChartPanel title={t('dashboard.businessPulse')} desc="充值、流水和真实成本放在同一张经营曲线上，先看利润压力再追明细。" type={t('dashboard.lineChart')} className="chart-panel-wide">
      <ReactECharts option={option} className="echarts-box" notMerge lazyUpdate />
      <EmptyChart show={!data.length} text={t('dashboard.noTrendData')} />
    </ChartPanel>
  )
}

function UsageBarChart({ data, locale, t }: { data: TrendPoint[]; locale: string; t: (key: string) => string }) {
  const imageUsageName = '图片任务'
  const videoUsageName = '视频任务'
  const usersName = t('dashboard.usersLegend')
  const textColor = chartTextColor()
  const lineColor = chartLineColor()
  const option = {
    color: [COLORS.teal, COLORS.blue, COLORS.purple],
    tooltip: {
      trigger: 'axis',
      confine: true,
      formatter: (params: unknown) => tooltipFormatter(params, locale)
    },
    legend: {
      top: 0,
      right: 8,
      textStyle: { color: textColor, fontWeight: 700 }
    },
    grid: { left: 60, right: 54, top: 54, bottom: 48 },
    xAxis: {
      type: 'category',
      data: data.map((item) => item.label),
      axisLabel: { color: textColor },
      axisLine: { lineStyle: { color: lineColor } },
      axisTick: { alignWithLabel: true }
    },
    yAxis: [
      {
        type: 'value',
        axisLabel: { color: textColor },
        splitLine: { lineStyle: { color: lineColor } }
      },
      {
        type: 'value',
        axisLabel: { color: textColor },
        splitLine: { show: false }
      }
    ],
    series: [
      {
        name: imageUsageName,
        type: 'bar',
        barMaxWidth: 18,
        itemStyle: { borderRadius: [6, 6, 0, 0] },
        data: trendValues(data, 'image_usage')
      },
      {
        name: videoUsageName,
        type: 'bar',
        barMaxWidth: 18,
        itemStyle: { borderRadius: [6, 6, 0, 0] },
        data: trendValues(data, 'video_usage')
      },
      {
        name: usersName,
        type: 'line',
        yAxisIndex: 1,
        smooth: true,
        symbolSize: 7,
        lineStyle: { width: 3 },
        data: trendValues(data, 'users')
      }
    ]
  }
  return (
    <ChartPanel title={t('dashboard.volumeMix')} desc="图片、视频生成量与新增用户并排看，方便判断需求和增长是否同步。" type={t('dashboard.comboChart')} className="chart-panel-wide">
      <ReactECharts option={option} className="echarts-box" notMerge lazyUpdate />
      <EmptyChart show={!data.length} text={t('dashboard.noTrendData')} />
    </ChartPanel>
  )
}

function TaskPieChart({ metrics, locale, t }: { metrics: DashboardMetrics; locale: string; t: (key: string) => string }) {
  const done = completedTasks(metrics)
  const queued = metrics.queued_tasks || 0
  const active = Math.max((metrics.active_tasks || 0) - queued, 0)
  const failed = metrics.failed_tasks || 0
  const textColor = chartTextColor()
  const option = {
    color: [COLORS.green, COLORS.blue, COLORS.amber, COLORS.red],
    tooltip: {
      trigger: 'item',
      confine: true,
      formatter: (params: { marker?: string; name?: string; value?: number; percent?: number }) => `${params.marker || ''}${params.name}: ${formatNumber(Number(params.value || 0), locale)} (${params.percent || 0}%)`
    },
    legend: {
      bottom: 0,
      left: 'center',
      textStyle: { color: textColor, fontWeight: 700 }
    },
    graphic: {
      type: 'text',
      left: 'center',
      top: '40%',
      style: {
        text: `${formatPercent(percent(done, Math.max(taskTotal(metrics), 1)))}\n完成率`,
        textAlign: 'center',
        fill: chartTitleColor(),
        fontSize: 14,
        fontWeight: 800,
        lineHeight: 22
      }
    },
    series: [
      {
        name: t('dashboard.taskHealth'),
        type: 'pie',
        radius: ['52%', '72%'],
        center: ['50%', '44%'],
        avoidLabelOverlap: true,
        label: { show: false },
        data: [
          { name: t('dashboard.completedTasks'), value: done },
          { name: '处理中', value: active },
          { name: t('dashboard.waiting'), value: queued },
          { name: t('dashboard.failedTasks'), value: failed }
        ]
      }
    ]
  }
  return (
    <ChartPanel title={t('dashboard.taskHealth')} desc="图片与视频任务的完成、处理、排队和失败结构。" type={t('dashboard.pieChart')}>
      <ReactECharts option={option} className="echarts-box echarts-box-small" notMerge lazyUpdate />
    </ChartPanel>
  )
}

function CostBarChart({ metrics, data, t }: { metrics: DashboardMetrics; data: TrendPoint[]; t: (key: string) => string }) {
  const totalRevenue = totalTrend(data, 'revenue')
  const totalTasks = Math.max(taskTotal(metrics), 1)
  const costRatio = totalRevenue ? (Number(metrics.actual_cost || 0) / totalRevenue) * 100 : 0
  const failureRate = percent(metrics.failed_tasks || 0, totalTasks)
  const queueRate = percent(metrics.queued_tasks || 0, totalTasks)
  const textColor = chartTextColor()
  const lineColor = chartLineColor()
  const option = {
    color: [COLORS.blue],
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: (params: unknown) => {
        const rows = Array.isArray(params) ? params : [params]
        const first = rows[0] as { name?: string; value?: number }
        return `${first.name}<br/>${t('dashboard.percentValue')}: ${Number(first.value || 0).toFixed(1)}%`
      }
    },
    grid: { left: 72, right: 38, top: 28, bottom: 36 },
    xAxis: {
      type: 'value',
      max: 100,
      axisLabel: { color: textColor, formatter: '{value}%' },
      splitLine: { lineStyle: { color: lineColor } }
    },
    yAxis: {
      type: 'category',
      data: [t('dashboard.costRatio'), t('dashboard.failureRate'), t('dashboard.queueRate')],
      axisLabel: { color: textColor, fontWeight: 700 },
      axisLine: { lineStyle: { color: lineColor } }
    },
    series: [
      {
        name: t('dashboard.operationsPulse'),
        type: 'bar',
        barWidth: 22,
        itemStyle: {
          borderRadius: [0, 6, 6, 0],
          color: (params: { dataIndex: number }) => [COLORS.blue, COLORS.red, COLORS.amber][params.dataIndex]
        },
        label: {
          show: true,
          position: 'right',
          formatter: ({ value }: { value: number }) => formatPercent(Number(value || 0)),
          color: textColor,
          fontWeight: 800
        },
        data: [costRatio, failureRate, queueRate]
      }
    ]
  }
  return (
    <ChartPanel title={t('dashboard.operationsPulse')} desc="成本、失败、排队三项压力并排看。" type={t('dashboard.barChart')}>
      <ReactECharts option={option} className="echarts-box echarts-box-small" notMerge lazyUpdate />
    </ChartPanel>
  )
}

function ExecutiveReport({ metrics, totalRevenue, locale, t }: { metrics: DashboardMetrics; totalRevenue: number; locale: string; t: (key: string) => string }) {
  const totalTasks = taskTotal(metrics)
  const done = completedTasks(metrics)
  const failureRate = percent(metrics.failed_tasks || 0, Math.max(totalTasks, 1))
  const queueRate = percent(metrics.queued_tasks || 0, Math.max(totalTasks, 1))
  const avgCost = totalTasks ? Number(metrics.actual_cost || 0) / totalTasks : 0
  const costRatio = totalRevenue ? (Number(metrics.actual_cost || 0) / totalRevenue) * 100 : 0
  const rows = [
    { label: '任务完成率', value: formatPercent(percent(done, Math.max(totalTasks, 1))), note: `${formatNumber(done, locale)} / ${formatNumber(totalTasks, locale)}`, status: failureRate <= 5 ? '正常' : '关注', tone: failureRate <= 5 ? 'good' : 'warn' },
    { label: '失败率', value: formatPercent(failureRate), note: `${t('dashboard.failedTasks')} ${formatNumber(metrics.failed_tasks || 0, locale)}`, status: failureRate <= 5 ? '正常' : failureRate <= 15 ? '关注' : '异常', tone: failureRate <= 5 ? 'good' : failureRate <= 15 ? 'warn' : 'bad' },
    { label: '排队率', value: formatPercent(queueRate), note: `${t('dashboard.queuedTasks')} ${formatNumber(metrics.queued_tasks || 0, locale)}`, status: metrics.queued_tasks ? '关注' : '正常', tone: metrics.queued_tasks ? 'warn' : 'good' },
    { label: '单任务成本', value: formatMoney(avgCost, locale, 2), note: `${t('dashboard.actualCost')} ${formatMoney(Number(metrics.actual_cost || 0), locale)}`, status: '追踪', tone: 'neutral' },
    { label: '成本/流水', value: formatPercent(costRatio), note: `流水 ${formatMoney(totalRevenue, locale)}`, status: costRatio <= 35 ? '正常' : '关注', tone: costRatio <= 35 ? 'good' : 'warn' },
    { label: '待确认订单', value: formatNumber(metrics.pending_orders || 0, locale), note: '人工充值和支付异常需要巡检', status: metrics.pending_orders ? '处理' : '正常', tone: metrics.pending_orders ? 'warn' : 'good' }
  ]

  return (
    <section className="admin-card report-table-card">
      <div className="section-head">
        <div>
          <h2>经营健康摘要</h2>
          <p>把老板最关心的任务、成本、队列和支付状态压成一张表。</p>
        </div>
      </div>
      <div className="report-table-wrap">
        <table className="data-table report-table">
          <thead>
            <tr>
              <th>指标</th>
              <th>当前值</th>
              <th>口径</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(row => (
              <tr key={row.label}>
                <td><strong>{row.label}</strong></td>
                <td className="report-number">{row.value}</td>
                <td>{row.note}</td>
                <td><span className={`report-status report-status-${row.tone}`}>{row.status}</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function SignalPanel({ metrics, totalRevenue, locale }: { metrics: DashboardMetrics; totalRevenue: number; locale: string }) {
  const totalTasks = Math.max(taskTotal(metrics), 1)
  const failureRate = percent(metrics.failed_tasks || 0, totalTasks)
  const queueRate = percent(metrics.queued_tasks || 0, totalTasks)
  const costRatio = totalRevenue ? (Number(metrics.actual_cost || 0) / totalRevenue) * 100 : 0
  const signals = [
    { label: '失败任务', value: `${formatNumber(metrics.failed_tasks || 0, locale)} 个`, hint: failureRate > 10 ? '失败率偏高，建议先看任务页错误原因。' : '失败率处于可观察区间。', tone: failureRate > 10 ? 'bad' : 'good' },
    { label: '队列压力', value: `${formatNumber(metrics.queued_tasks || 0, locale)} 个`, hint: queueRate > 5 ? '生成队列存在积压，需要检查 worker/provider。' : '暂无明显积压。', tone: queueRate > 5 ? 'warn' : 'good' },
    { label: '成本压力', value: formatPercent(costRatio), hint: costRatio > 35 ? '成本占流水偏高，建议查看成本页 Provider 排名。' : '成本占比暂未超过观察线。', tone: costRatio > 35 ? 'warn' : 'good' },
    { label: '待确认订单', value: `${formatNumber(metrics.pending_orders || 0, locale)} 笔`, hint: metrics.pending_orders ? '有订单需要人工确认或排查。' : '支付待办清空。', tone: metrics.pending_orders ? 'warn' : 'good' }
  ]

  return (
    <section className="admin-card report-side-panel">
      <div className="section-head">
        <div>
          <h2>异常信号</h2>
          <p>先看红黄项，再决定去哪一页处理。</p>
        </div>
      </div>
      <div className="report-signal-list">
        {signals.map(signal => (
          <div className={`report-signal-row report-signal-${signal.tone}`} key={signal.label}>
            <span className="signal-dot" aria-hidden="true" />
            <div>
              <strong>{signal.label}</strong>
              <p>{signal.hint}</p>
            </div>
            <b>{signal.value}</b>
          </div>
        ))}
      </div>
    </section>
  )
}

function QuickActionPanel() {
  const links = [
    { href: '/tasks', label: '任务队列', note: '看失败、超时、Provider 尝试链', icon: ImageIcon },
    { href: '/costs', label: '成本统计', note: '看 Provider 成本和失败率排名', icon: BarChart3 },
    { href: '/recharges', label: '充值流水', note: '查看到账记录并处理待确认订单', icon: CreditCard },
    { href: '/risk-events', label: '风控事件', note: '查看违禁词、原文和封禁状态', icon: ShieldAlert }
  ]

  return (
    <section className="admin-card report-side-panel">
      <div className="section-head">
        <div>
          <h2>快捷处理</h2>
          <p>把常见管理动作放到一屏内。</p>
        </div>
      </div>
      <div className="quick-action-list">
        {links.map(item => (
          <Link href={item.href} className="quick-action-row" key={item.href}>
            <span className="quick-action-icon"><item.icon size={17} /></span>
            <span>
              <strong>{item.label}</strong>
              <small>{item.note}</small>
            </span>
            <ArrowRight size={16} />
          </Link>
        ))}
      </div>
    </section>
  )
}

export default function AdminHomePage() {
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null)
  const [filters, setFilters] = useState<AdminAnalyticsFilters>({})
  const [message, setMessage] = useState('')
  const t = useI18n()
  const locale = useLocale()

  async function load() {
    try {
      const data = await getDashboard(filters)
      setMetrics(data)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('dashboard.loadFailed'))
    }
  }

  useEffect(() => {
    if (!getToken()) {
      window.location.href = '/login'
      return
    }
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function patchFilter(patch: Partial<AdminAnalyticsFilters>) {
    setFilters(current => ({ ...current, ...patch }))
  }

  const trend = useMemo(() => metrics?.trends || [], [metrics])
  const totalRecharge = totalTrend(trend, 'recharge')
  const totalImageUsage = totalTrend(trend, 'image_usage')
  const totalVideoUsage = totalTrend(trend, 'video_usage')
  const totalUsers = totalTrend(trend, 'users')
  const totalRevenue = totalTrend(trend, 'revenue')

  return (
    <>
      <div className="page-head dashboard-head report-page-head dashboard-screen-head dashboard-command-head">
        <div>
          <span className="eyebrow dashboard-command-eyebrow"><Activity size={13} />PicPilot Operations</span>
          <h1>运营总览</h1>
          <p>集中查看业务规模、任务健康度、收入成本和需要立即处理的异常。</p>
          <div className="dashboard-command-status" aria-label="当前运营状态">
            <span className="dashboard-live-status"><i aria-hidden="true" />数据已同步</span>
            <span>全部任务 <b>{metrics ? formatNumber(taskTotal(metrics), locale) : '--'}</b></span>
            <span>执行中 <b>{metrics ? formatNumber(metrics.active_tasks || 0, locale) : '--'}</b></span>
            <span>排队 <b>{metrics ? formatNumber(metrics.queued_tasks || 0, locale) : '--'}</b></span>
            <span>用户 <b>{metrics ? formatNumber(metrics.users || 0, locale) : '--'}</b></span>
          </div>
        </div>
        <div className="report-head-actions">
          <Link className="btn btn-secondary" href="/costs"><BarChart3 size={16} />成本统计</Link>
          <button className="btn btn-primary" onClick={load}><ReceiptText size={16} />{t('dashboard.refresh')}</button>
        </div>
      </div>

      {message || !metrics ? <div className="admin-card muted spacious-message">{message || t('dashboard.loading')}</div> : null}

      <section className="dashboard-filter-panel" aria-label="运营数据筛选">
        <div className="dashboard-filter-heading">
          <span className="dashboard-filter-icon"><Filter size={16} /></span>
          <div>
            <strong>数据筛选</strong>
            <small>按时间、任务和用户缩小统计范围</small>
          </div>
        </div>
        <div className="dashboard-filter-grid">
          <label className="dashboard-filter-field dashboard-filter-dates">
            <span>时间范围</span>
            <span className="dashboard-date-range">
              <input className="input" type="date" value={filters.from || ''} onChange={event => patchFilter({ from: event.target.value })} aria-label={t('filters.from')} />
              <i>至</i>
              <input className="input" type="date" value={filters.to || ''} onChange={event => patchFilter({ to: event.target.value })} aria-label={t('filters.to')} />
            </span>
          </label>
          <label className="dashboard-filter-field">
            <span>任务类型</span>
            <select className="select" value={filters.mediaType || ''} onChange={event => patchFilter({ mediaType: event.target.value })} aria-label="任务媒介">
              <option value="">全部任务</option>
              <option value="image">图片任务</option>
              <option value="video">视频任务</option>
            </select>
          </label>
          <label className="dashboard-filter-field">
            <span>上游与状态</span>
            <span className="dashboard-filter-pair">
              <input className="input" value={filters.provider || ''} onChange={event => patchFilter({ provider: event.target.value })} placeholder={t('filters.providerPlaceholder')} />
              <input className="input" value={filters.status || ''} onChange={event => patchFilter({ status: event.target.value })} placeholder={t('filters.statusPlaceholder')} />
            </span>
          </label>
          <label className="dashboard-filter-field">
            <span>任务与用户</span>
            <span className="dashboard-filter-pair">
              <input className="input" value={filters.imageType || ''} onChange={event => patchFilter({ imageType: event.target.value })} placeholder={t('filters.imageTypePlaceholder')} />
              <input className="input" value={filters.userKeyword || ''} onChange={event => patchFilter({ userKeyword: event.target.value })} placeholder={t('filters.userPlaceholder')} />
            </span>
          </label>
          <button className="btn btn-primary dashboard-filter-submit" onClick={load}><Filter size={15} />{t('dashboard.applyFilters')}</button>
        </div>
      </section>

      {metrics ? (
        <>
          <section className="dashboard-screen-board">
            <section className="dashboard-kpi-section">
              <div className="dashboard-group-heading">
                <div>
                  <span>Business volume</span>
                  <h2>业务规模</h2>
                </div>
                <p>充值、生成与用户增长</p>
              </div>
              <section className="dashboard-screen-kpis dashboard-primary-kpis">
                <StatCard icon={WalletCards} tone={COLORS.blue} label={t('dashboard.recharge7d')} value={formatMoney(totalRecharge, locale)} note={t('dashboard.paidOrdersNote')} />
                <StatCard icon={ImageIcon} tone={COLORS.teal} label="近 7 日图片任务" value={formatNumber(totalImageUsage, locale)} note={`累计图片任务 ${formatNumber(metrics.image_tasks ?? 0, locale)}`} />
                <StatCard icon={Film} tone={COLORS.blue} label="近 7 日视频任务" value={formatNumber(totalVideoUsage, locale)} note={`累计视频任务 ${formatNumber(metrics.video_tasks ?? 0, locale)} · 净收 ${formatNumber(metrics.video_credits_income ?? 0, locale)} 积分`} />
                <StatCard icon={Users} tone={COLORS.purple} label={t('dashboard.users')} value={formatNumber(metrics.users ?? 0, locale)} note={`${t('dashboard.newUsers7d')} ${formatNumber(totalUsers, locale)}`} />
              </section>
              <div className="dashboard-group-heading dashboard-group-heading-compact">
                <div>
                  <span>Operations health</span>
                  <h2>经营健康</h2>
                </div>
                <p>收入、成本与待处理任务</p>
              </div>
              <section className="dashboard-screen-kpis dashboard-ops-kpis">
                <StatCard icon={CircleDollarSign} tone={COLORS.amber} label={t('dashboard.revenue')} value={formatMoney(totalRevenue, locale)} note={`${t('dashboard.pendingOrders')} ${formatNumber(metrics.pending_orders ?? 0, locale)}`} />
                <StatCard icon={Clock3} tone={COLORS.amber} label={t('dashboard.queuedTasks')} value={formatNumber(metrics.queued_tasks ?? 0, locale)} note={`图片 ${formatNumber(metrics.queued_image_tasks ?? metrics.queued_tasks ?? 0, locale)} · 视频 ${formatNumber(metrics.queued_video_tasks ?? 0, locale)}`} />
                <StatCard icon={AlertTriangle} tone={COLORS.red} label={t('dashboard.failedTasks')} value={formatNumber(metrics.failed_tasks ?? 0, locale)} note={`图片 ${formatNumber(metrics.failed_image_tasks ?? metrics.failed_tasks ?? 0, locale)} · 视频 ${formatNumber(metrics.failed_video_tasks ?? 0, locale)}`} />
                <StatCard icon={ReceiptText} tone={COLORS.slate} label={t('dashboard.actualCost')} value={formatMoney(Number(metrics.actual_cost || 0), locale, 2)} note={`图片 ${formatMoney(Number(metrics.image_actual_cost ?? metrics.actual_cost ?? 0), locale, 2)} · 视频 ${formatMoney(Number(metrics.video_actual_cost || 0), locale, 2)}`} />
              </section>
            </section>

            <section className="dashboard-big-screen-grid">
              <div className="dashboard-main-stage">
                <RevenueLineChart data={trend} locale={locale} t={t} />
                <UsageBarChart data={trend} locale={locale} t={t} />
              </div>
              <aside className="dashboard-screen-aside">
                <TaskPieChart metrics={metrics} locale={locale} t={t} />
                <CostBarChart metrics={metrics} data={trend} t={t} />
                <SignalPanel metrics={metrics} totalRevenue={totalRevenue} locale={locale} />
              </aside>
            </section>

            <section className="dashboard-bottom-grid">
              <ExecutiveReport metrics={metrics} totalRevenue={totalRevenue} locale={locale} t={t} />
              <QuickActionPanel />
            </section>
          </section>
        </>
      ) : null}
    </>
  )
}
