'use client'

import { useEffect, useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { BarChart3, Download, RefreshCw } from 'lucide-react'
import { useI18n, useLocale } from '@/components/i18n'
import { AdminAnalyticsFilters, CostSummary, getCostSummary } from '@/lib/api'

type CostRow = CostSummary['rows'][number]

const CHART_COLORS = ['#2563eb', '#0f766e', '#7c3aed', '#d97706', '#dc2626', '#475467', '#059669', '#0891b2']

function money(value: number, locale = 'zh-CN', digits = 2) {
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY', maximumFractionDigits: digits, minimumFractionDigits: digits }).format(Number(value || 0))
}

function numberText(value: number, locale = 'zh-CN') {
  return new Intl.NumberFormat(locale).format(Math.round(value || 0))
}

function percentText(value: number) {
  return `${Number(value || 0).toFixed(1)}%`
}

function safeDivide(part: number, total: number) {
  return total > 0 ? part / total : 0
}

function chartTextColor() {
  if (typeof window === 'undefined') return '#667085'
  return document.documentElement.dataset.theme === 'dark' ? '#d0d5dd' : '#667085'
}

function chartLineColor() {
  if (typeof window === 'undefined') return 'rgba(102,112,133,.18)'
  return document.documentElement.dataset.theme === 'dark' ? 'rgba(152,162,179,.16)' : 'rgba(102,112,133,.18)'
}

function csvCell(value: string | number) {
  const raw = String(value ?? '')
  return /[",\n]/.test(raw) ? `"${raw.replace(/"/g, '""')}"` : raw
}

function aggregateByProvider(rows: CostRow[]) {
  const map = new Map<string, { provider: string; calls: number; cost: number; failureWeight: number; taskTypes: Set<string> }>()
  rows.forEach(row => {
    const provider = row.provider || 'unknown'
    const current = map.get(provider) || { provider, calls: 0, cost: 0, failureWeight: 0, taskTypes: new Set<string>() }
    current.calls += Number(row.calls || 0)
    current.cost += Number(row.cost || 0)
    current.failureWeight += Number(row.failure_rate || 0) * Number(row.calls || 0)
    if (row.task_type) current.taskTypes.add(`${row.media_type === 'video' ? '视频' : '图片'} · ${row.task_type}`)
    map.set(provider, current)
  })
  return Array.from(map.values())
    .map(item => ({
      provider: item.provider,
      calls: item.calls,
      cost: item.cost,
      failureRate: item.calls ? item.failureWeight / item.calls : 0,
      taskTypes: Array.from(item.taskTypes).join(' / ') || '-'
    }))
    .sort((a, b) => b.cost - a.cost)
}

function ReportChart({ title, desc, children, wide = false }: { title: string; desc: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <section className={`report-chart-card cost-chart-card ${wide ? 'chart-panel-wide' : ''}`}>
      <div className="report-chart-head">
        <div>
          <span className="chart-type-label">ECharts</span>
          <h2>{title}</h2>
          <p>{desc}</p>
        </div>
      </div>
      <div className="report-chart-body">{children}</div>
    </section>
  )
}

function EmptyChart({ show }: { show: boolean }) {
  if (!show) return null
  return <div className="standard-chart-empty">暂无成本数据</div>
}

function ProviderCostChart({ rows, locale }: { rows: ReturnType<typeof aggregateByProvider>; locale: string }) {
  const topRows = rows.slice(0, 8).reverse()
  const option = {
    color: [CHART_COLORS[0]],
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: (params: unknown) => {
        const point = (Array.isArray(params) ? params[0] : params) as { name?: string; value?: number }
        return `${point.name || ''}<br/>成本: ${money(Number(point.value || 0), locale)}`
      }
    },
    grid: { left: 118, right: 36, top: 24, bottom: 28 },
    xAxis: {
      type: 'value',
      axisLabel: { color: chartTextColor(), formatter: (value: number) => `¥${value}` },
      splitLine: { lineStyle: { color: chartLineColor() } }
    },
    yAxis: {
      type: 'category',
      data: topRows.map(row => row.provider),
      axisLabel: { color: chartTextColor(), fontWeight: 700 },
      axisLine: { lineStyle: { color: chartLineColor() } }
    },
    series: [
      {
        name: '成本',
        type: 'bar',
        barWidth: 20,
        itemStyle: { borderRadius: [0, 6, 6, 0] },
        label: {
          show: true,
          position: 'right',
          color: chartTextColor(),
          fontWeight: 800,
          formatter: ({ value }: { value: number }) => money(Number(value || 0), locale)
        },
        data: topRows.map(row => Number(row.cost || 0))
      }
    ]
  }
  return (
    <ReportChart title="Provider 成本排行" desc="按实际成本排序，快速找到主要成本来源。" wide>
      <ReactECharts option={option} className="echarts-box" notMerge lazyUpdate />
      <EmptyChart show={!rows.length} />
    </ReportChart>
  )
}

function ProviderCallsChart({ rows, locale }: { rows: ReturnType<typeof aggregateByProvider>; locale: string }) {
  const topRows = rows.slice(0, 8)
  const option = {
    color: [CHART_COLORS[1]],
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: (params: unknown) => {
        const point = (Array.isArray(params) ? params[0] : params) as { name?: string; value?: number }
        return `${point.name || ''}<br/>调用: ${numberText(Number(point.value || 0), locale)}`
      }
    },
    grid: { left: 42, right: 22, top: 24, bottom: 58 },
    xAxis: {
      type: 'category',
      data: topRows.map(row => row.provider),
      axisLabel: { color: chartTextColor(), interval: 0, rotate: topRows.length > 4 ? 24 : 0 },
      axisLine: { lineStyle: { color: chartLineColor() } },
      axisTick: { alignWithLabel: true }
    },
    yAxis: {
      type: 'value',
      axisLabel: { color: chartTextColor() },
      splitLine: { lineStyle: { color: chartLineColor() } }
    },
    series: [
      {
        name: '调用量',
        type: 'bar',
        barWidth: 22,
        itemStyle: { borderRadius: [6, 6, 0, 0] },
        data: topRows.map(row => Number(row.calls || 0))
      }
    ]
  }
  return (
    <ReportChart title="调用量排行" desc="看每个 Provider 承担了多少请求。">
      <ReactECharts option={option} className="echarts-box echarts-box-small" notMerge lazyUpdate />
      <EmptyChart show={!rows.length} />
    </ReportChart>
  )
}

function FailureRateChart({ rows }: { rows: ReturnType<typeof aggregateByProvider> }) {
  const topRows = [...rows].sort((a, b) => b.failureRate - a.failureRate).slice(0, 8)
  const option = {
    color: [CHART_COLORS[4]],
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'shadow' },
      formatter: (params: unknown) => {
        const point = (Array.isArray(params) ? params[0] : params) as { name?: string; value?: number }
        return `${point.name || ''}<br/>失败率: ${percentText(Number(point.value || 0))}`
      }
    },
    grid: { left: 42, right: 24, top: 24, bottom: 58 },
    xAxis: {
      type: 'category',
      data: topRows.map(row => row.provider),
      axisLabel: { color: chartTextColor(), interval: 0, rotate: topRows.length > 4 ? 24 : 0 },
      axisLine: { lineStyle: { color: chartLineColor() } },
      axisTick: { alignWithLabel: true }
    },
    yAxis: {
      type: 'value',
      axisLabel: { color: chartTextColor(), formatter: '{value}%' },
      splitLine: { lineStyle: { color: chartLineColor() } }
    },
    series: [
      {
        name: '失败率',
        type: 'bar',
        barWidth: 22,
        itemStyle: { borderRadius: [6, 6, 0, 0] },
        label: {
          show: true,
          position: 'top',
          color: chartTextColor(),
          fontWeight: 800,
          formatter: ({ value }: { value: number }) => percentText(Number(value || 0))
        },
        data: topRows.map(row => Number(row.failureRate || 0))
      }
    ]
  }
  return (
    <ReportChart title="失败率排行" desc="按 Provider 聚合，异常时优先排查高失败项。" wide>
      <ReactECharts option={option} className="echarts-box echarts-box-small" notMerge lazyUpdate />
      <EmptyChart show={!rows.length} />
    </ReportChart>
  )
}

function CostShareChart({ rows, locale }: { rows: ReturnType<typeof aggregateByProvider>; locale: string }) {
  const option = {
    color: CHART_COLORS,
    tooltip: {
      trigger: 'item',
      confine: true,
      formatter: (params: { marker?: string; name?: string; value?: number; percent?: number }) => `${params.marker || ''}${params.name}: ${money(Number(params.value || 0), locale)} (${params.percent || 0}%)`
    },
    legend: {
      bottom: 0,
      left: 'center',
      textStyle: { color: chartTextColor(), fontWeight: 700 }
    },
    series: [
      {
        name: '成本占比',
        type: 'pie',
        radius: ['48%', '72%'],
        center: ['50%', '43%'],
        label: { show: false },
        data: rows.slice(0, 8).map(row => ({ name: row.provider, value: Number(row.cost || 0) }))
      }
    ]
  }
  return (
    <ReportChart title="成本占比" desc="看成本是否集中在少数 Provider。">
      <ReactECharts option={option} className="echarts-box echarts-box-small" notMerge lazyUpdate />
      <EmptyChart show={!rows.length} />
    </ReportChart>
  )
}

function CostKpi({ label, value, note, tone = 'neutral' }: { label: string; value: string | number; note: string; tone?: 'good' | 'warn' | 'bad' | 'neutral' }) {
  return (
    <article className={`report-kpi-card cost-kpi-card report-kpi-${tone}`}>
      <div className="metric-label">{label}</div>
      <div className="metric-value">{value}</div>
      <p className="metric-note">{note}</p>
    </article>
  )
}

export default function Page() {
  const [summary, setSummary] = useState<CostSummary | null>(null)
  const [filters, setFilters] = useState<AdminAnalyticsFilters>({})
  const [message, setMessage] = useState('')
  const t = useI18n()
  const locale = useLocale()

  async function load() {
    try {
      const data = await getCostSummary(filters)
      setSummary(data)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('costs.loadFailed'))
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function patchFilter(patch: Partial<AdminAnalyticsFilters>) {
    setFilters(current => ({ ...current, ...patch }))
  }

  function downloadCsv() {
    const rows = summary?.rows || []
    const header = ['媒介', t('common.provider'), t('common.taskType'), t('common.calls'), t('common.cost'), t('common.failureRate')]
    const csv = [header, ...rows.map(row => [row.media_type === 'video' ? '视频' : '图片', row.provider, row.task_type, row.calls, row.cost, `${row.failure_rate}%`])]
      .map(row => row.map(csvCell).join(','))
      .join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `picpilot-costs-${Date.now()}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  const rows = useMemo(() => summary?.rows || [], [summary])
  const providerRows = useMemo(() => aggregateByProvider(rows), [rows])
  const totalCost = Number(summary?.total_cost ?? rows.reduce((sum, row) => sum + Number(row.cost || 0), 0))
  const totalCalls = Number(summary?.task_count ?? rows.reduce((sum, row) => sum + Number(row.calls || 0), 0))
  const highestCostProvider = providerRows[0]
  const highestFailureProvider = useMemo(() => [...providerRows].sort((a, b) => b.failureRate - a.failureRate)[0], [providerRows])
  const costTableRows = rows
    .map(row => ({ ...row, costShare: safeDivide(Number(row.cost || 0), totalCost) * 100, avgCost: safeDivide(Number(row.cost || 0), Number(row.calls || 0)) }))
    .sort((a, b) => Number(b.cost || 0) - Number(a.cost || 0))

  return (
    <>
      <div className="page-head report-page-head">
        <div>
          <span className="eyebrow">Cost Control</span>
          <h1>{t('costs.title')}</h1>
          <p>统一统计图片与视频的 Provider、模型、调用量、失败率、积分和实际成本，方便查账和控成本。</p>
        </div>
        <div className="report-head-actions">
          <button className="btn btn-secondary" onClick={load}><RefreshCw size={16} />{t('common.refresh')}</button>
          <button className="btn btn-primary" onClick={downloadCsv}><Download size={16} />{t('costs.download')}</button>
        </div>
      </div>

      {message || !summary ? <div className="admin-card muted spacious-message">{message || t('costs.loading')}</div> : null}

      {summary?.cost_policy ? (
        <div className="filter-bar action-filter-bar cost-policy-bar" aria-label="成本核算口径">
          <strong>成本口径：人民币</strong>
          <span>rua.chat ¥{Number(summary.cost_policy.rua_chat_per_image_cny).toFixed(4)} / 张</span>
          <span>APIMart 1 Credit = ¥{Number(summary.cost_policy.apimart_credit_cny_rate).toFixed(6)}</span>
          <span>APIMart $1 = ¥{Number(summary.cost_policy.apimart_usd_cny_rate).toFixed(6)}</span>
          <span>
            APIMart 1K / 2K / 4K：
            {Number(summary.cost_policy.apimart_tier_credits['1k']).toFixed(3)} /
            {' '}{Number(summary.cost_policy.apimart_tier_credits['2k']).toFixed(3)} /
            {' '}{Number(summary.cost_policy.apimart_tier_credits['4k']).toFixed(3)} Credits
          </span>
          <small className="muted">优先采用上游 credits_cost；仅有 cost 时按美元换算，两个字段都缺失时才按图片分辨率档位兜底。</small>
        </div>
      ) : null}

      <div className="filter-bar action-filter-bar admin-filter-grid report-filter-bar">
        <input className="input" type="date" value={filters.from || ''} onChange={event => patchFilter({ from: event.target.value })} aria-label={t('filters.from')} />
        <input className="input" type="date" value={filters.to || ''} onChange={event => patchFilter({ to: event.target.value })} aria-label={t('filters.to')} />
        <select className="select" value={filters.mediaType || ''} onChange={event => patchFilter({ mediaType: event.target.value })} aria-label="任务媒介">
          <option value="">全部任务</option>
          <option value="image">图片任务</option>
          <option value="video">视频任务</option>
        </select>
        <input className="input" value={filters.provider || ''} onChange={event => patchFilter({ provider: event.target.value })} placeholder={t('filters.providerPlaceholder')} />
        <input className="input" value={filters.status || ''} onChange={event => patchFilter({ status: event.target.value })} placeholder={t('filters.statusPlaceholder')} />
        <input className="input" value={filters.imageType || ''} onChange={event => patchFilter({ imageType: event.target.value })} placeholder={t('filters.imageTypePlaceholder')} />
        <input className="input" value={filters.userKeyword || ''} onChange={event => patchFilter({ userKeyword: event.target.value })} placeholder={t('filters.userPlaceholder')} />
        <button className="btn btn-secondary" onClick={load}>{t('dashboard.applyFilters')}</button>
      </div>

      <section className="report-kpi-grid cost-kpi-grid">
        <CostKpi label="总成本" value={money(totalCost, locale)} note="当前筛选范围内实际成本汇总" />
        <CostKpi label={t('costs.today')} value={money(summary?.today_cost || 0, locale)} note={`图片 ${money(summary?.today_image_cost || 0, locale)} · 视频 ${money(summary?.today_video_cost || 0, locale)}`} />
        <CostKpi label="图片成本" value={money(summary?.image_cost || 0, locale)} note={`${numberText(summary?.image_task_count || 0, locale)} 个图片任务`} />
        <CostKpi label="视频成本" value={money(summary?.video_cost || 0, locale)} note={`${numberText(summary?.video_task_count || 0, locale)} 个视频任务`} />
        <CostKpi label="单任务平均成本" value={money(summary?.average_task_cost || 0, locale)} note={`按 ${numberText(totalCalls, locale)} 次任务均摊`} />
        <CostKpi label={t('common.failureRate')} value={percentText(summary?.failure_rate || 0)} note="当前筛选范围内任务失败占比" tone={Number(summary?.failure_rate || 0) > 10 ? 'bad' : 'good'} />
        <CostKpi label="最高成本 Provider" value={highestCostProvider?.provider || '-'} note={highestCostProvider ? money(highestCostProvider.cost, locale) : '暂无 Provider 数据'} tone={highestCostProvider ? 'warn' : 'neutral'} />
        <CostKpi label="最高失败 Provider" value={highestFailureProvider?.provider || '-'} note={highestFailureProvider ? percentText(highestFailureProvider.failureRate) : '暂无失败率数据'} tone={highestFailureProvider && highestFailureProvider.failureRate > 10 ? 'bad' : 'neutral'} />
      </section>

      <section className="report-chart-grid cost-chart-grid">
        <ProviderCostChart rows={providerRows} locale={locale} />
        <CostShareChart rows={providerRows} locale={locale} />
        <ProviderCallsChart rows={providerRows} locale={locale} />
        <FailureRateChart rows={providerRows} />
      </section>

      <section className="admin-card report-table-card">
        <div className="section-head">
          <div>
            <h2>Provider 成本报表</h2>
            <p>按成本倒序展示图片与视频，包含成本占比、单次均价和失败率。</p>
          </div>
          <span className="status-good"><BarChart3 size={14} /> {numberText(costTableRows.length, locale)} 行</span>
        </div>
        <div className="report-table-wrap">
          <table className="data-table report-table cost-report-table">
            <thead>
              <tr>
                <th>排名</th>
                <th>媒介</th>
                <th>{t('common.provider')}</th>
                <th>{t('common.taskType')}</th>
                <th>{t('common.calls')}</th>
                <th>{t('common.cost')}</th>
                <th>成本占比</th>
                <th>单次均价</th>
                <th>{t('common.failureRate')}</th>
              </tr>
            </thead>
            <tbody>
              {costTableRows.map((row, index) => (
                <tr key={`${row.media_type}-${row.provider}-${row.task_type}`}>
                  <td className="report-rank">#{index + 1}</td>
                  <td><span className={row.media_type === 'video' ? 'status-warn' : 'status-good'}>{row.media_type === 'video' ? '视频' : '图片'}</span></td>
                  <td><strong>{row.provider || '-'}</strong></td>
                  <td>{row.task_type || '-'}</td>
                  <td className="report-number">{numberText(row.calls, locale)}</td>
                  <td className="report-number">{money(row.cost, locale)}</td>
                  <td>{percentText(row.costShare)}</td>
                  <td>{money(row.avgCost, locale, 3)}</td>
                  <td><span className={`report-status ${Number(row.failure_rate || 0) > 10 ? 'report-status-bad' : 'report-status-good'}`}>{percentText(row.failure_rate)}</span></td>
                </tr>
              ))}
              {!costTableRows.length ? <tr><td colSpan={9} className="muted">{t('costs.noRows')}</td></tr> : null}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
