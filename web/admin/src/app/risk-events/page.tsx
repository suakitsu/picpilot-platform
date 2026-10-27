'use client'

import { useEffect, useState } from 'react'
import { Eye, ShieldAlert, X } from 'lucide-react'
import { Pagination } from '@/components/Pagination'
import { useConfirm } from '@/components/ConfirmProvider'
import { addRiskKeyword, deleteRiskKeyword, getRiskKeywords, listRiskEvents, RiskEventRow, RiskKeywordConfig, updateRiskConfig } from '@/lib/api'

type RiskLevel = 'high' | 'medium' | 'low'
type RiskConfigDraft = Pick<RiskKeywordConfig, 'enabled' | 'ban_threshold' | 'block_levels' | 'warn_levels'>

const riskLevels: Array<{ value: RiskLevel; label: string; note: string }> = [
  { value: 'high', label: 'High', note: '直接拦截，严重违规' },
  { value: 'medium', label: 'Medium', note: '直接拦截，侵权/真人/功效等' },
  { value: 'low', label: 'Low', note: '记录提醒，低风险观察词' }
]

const defaultRiskConfigDraft: RiskConfigDraft = {
  enabled: true,
  ban_threshold: 5,
  block_levels: ['high', 'medium'],
  warn_levels: ['low']
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function userLabel(event: RiskEventRow) {
  return event.user_account || event.user_email_masked || event.user_id || '-'
}

export default function Page() {
  const confirm = useConfirm()
  const [rows, setRows] = useState<RiskEventRow[]>([])
  const [keywordConfig, setKeywordConfig] = useState<RiskKeywordConfig | null>(null)
  const [configDraft, setConfigDraft] = useState<RiskConfigDraft>(defaultRiskConfigDraft)
  const [keywordSearch, setKeywordSearch] = useState('')
  const [newKeywordLevel, setNewKeywordLevel] = useState<RiskLevel>('medium')
  const [newKeyword, setNewKeyword] = useState('')
  const [savingKeyword, setSavingKeyword] = useState(false)
  const [savingConfig, setSavingConfig] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [riskLevel, setRiskLevel] = useState('')
  const [userKeyword, setUserKeyword] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [message, setMessage] = useState('')
  const [selectedEvent, setSelectedEvent] = useState<RiskEventRow | null>(null)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalPages, setTotalPages] = useState(0)
  const pageSize = 20

  async function loadKeywords() {
    try {
      const data = await getRiskKeywords()
      setKeywordConfig(data)
      setConfigDraft(configDraftFrom(data))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '风控词库加载失败')
    }
  }

  async function load(nextPage = page) {
    try {
      const data = await listRiskEvents({ keyword, riskLevel, userKeyword, from, to, page: nextPage, pageSize })
      setRows(data.rows)
      setPage(data.page)
      setTotal(data.total)
      setTotalPages(data.total_pages)
      setMessage(data.rows.length ? '' : '暂无匹配的风控事件')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '风控事件加载失败')
    }
  }

  useEffect(() => {
    loadKeywords()
    const params = new URLSearchParams(window.location.search)
    const queryUser = params.get('user_keyword') || ''
    if (queryUser) {
      setUserKeyword(queryUser)
      listRiskEvents({ keyword, riskLevel, userKeyword: queryUser, from, to, page: 1, pageSize })
        .then(data => {
          setRows(data.rows)
          setPage(data.page)
          setTotal(data.total)
          setTotalPages(data.total_pages)
          setMessage(data.rows.length ? '' : '暂无匹配的风控事件')
        })
        .catch(error => setMessage(error instanceof Error ? error.message : '风控事件加载失败'))
      return
    }
    load(1)
  }, [])

  useEffect(() => {
    if (!selectedEvent) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setSelectedEvent(null)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [selectedEvent])

  function filteredKeywords(level: RiskLevel) {
    const rows = keywordConfig?.keywords?.[level] || []
    const search = keywordSearch.trim().toLowerCase()
    if (!search) return rows
    return rows.filter(item => item.toLowerCase().includes(search))
  }

  function configDraftFrom(config: RiskKeywordConfig): RiskConfigDraft {
    return {
      enabled: config.enabled !== false,
      ban_threshold: Number(config.ban_threshold || 5),
      block_levels: riskLevels.map(item => item.value).filter(level => (config.block_levels || []).includes(level)),
      warn_levels: riskLevels.map(item => item.value).filter(level => (config.warn_levels || []).includes(level))
    }
  }

  function toggleConfigLevel(field: 'block_levels' | 'warn_levels', level: RiskLevel) {
    const otherField = field === 'block_levels' ? 'warn_levels' : 'block_levels'
    setConfigDraft(current => {
      const values = new Set(current[field])
      const enabled = !values.has(level)
      if (enabled) {
        values.add(level)
      } else {
        values.delete(level)
      }
      const next = { ...current, [field]: riskLevels.map(item => item.value).filter(item => values.has(item)) }
      if (enabled) {
        next[otherField] = current[otherField].filter(item => item !== level)
      }
      return next
    })
  }

  async function saveRiskConfig() {
    const threshold = Number(configDraft.ban_threshold)
    if (!Number.isFinite(threshold) || threshold < 1 || threshold > 100) {
      setMessage('封禁阈值必须在 1-100 次之间')
      return
    }
    if (!await confirm({ title: '保存风控配置', message: `确认更新风控配置？封禁阈值将变为 ${threshold} 次。` })) return
    setSavingConfig(true)
    setMessage('')
    try {
      const data = await updateRiskConfig({ ...configDraft, ban_threshold: threshold })
      setKeywordConfig(data)
      setConfigDraft(configDraftFrom(data))
      setMessage('风控配置已保存')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '风控配置保存失败')
    } finally {
      setSavingConfig(false)
    }
  }

  async function addKeyword() {
    const keywordValue = newKeyword.trim()
    if (!keywordValue || savingKeyword) return
    setSavingKeyword(true)
    setMessage('')
    try {
      const data = await addRiskKeyword({ level: newKeywordLevel, keyword: keywordValue })
      setKeywordConfig(data)
      setNewKeyword('')
      setMessage(`已添加风控词：${keywordValue}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '风控词添加失败')
    } finally {
      setSavingKeyword(false)
    }
  }

  async function removeKeyword(level: RiskLevel, keywordValue: string) {
    if (!await confirm({ title: '删除风控词', message: `确认删除 ${level} 级风控词「${keywordValue}」？` })) return
    setSavingKeyword(true)
    setMessage('')
    try {
      const data = await deleteRiskKeyword({ level, keyword: keywordValue })
      setKeywordConfig(data)
      setMessage(`已删除风控词：${keywordValue}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '风控词删除失败')
    } finally {
      setSavingKeyword(false)
    }
  }

  return (
    <>
      <div className="page-head"><div><h1>风控事件</h1><p>查看关键词命中、上游风控错误和封禁记录</p></div><button className="btn btn-primary" onClick={() => load(1)}>刷新</button></div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      <section className="admin-card operation-card risk-keyword-card">
        <div className="section-head">
          <div>
            <h2>违禁词库</h2>
            <p>管理员可查看、搜索、添加和删除风控关键词；保存后会立即影响后端 provider 前置拦截。</p>
          </div>
          <button className="btn btn-secondary" type="button" onClick={loadKeywords}>刷新词库</button>
        </div>
        <div className="risk-config-grid">
          <label className="field">
            <span>风控状态</span>
            <select className="select" value={configDraft.enabled ? 'enabled' : 'disabled'} onChange={event => setConfigDraft(current => ({ ...current, enabled: event.target.value === 'enabled' }))}>
              <option value="enabled">开启</option>
              <option value="disabled">关闭</option>
            </select>
          </label>
          <label className="field">
            <span>封禁阈值</span>
            <input className="input" type="number" min={1} max={100} value={configDraft.ban_threshold} onChange={event => setConfigDraft(current => ({ ...current, ban_threshold: Number(event.target.value) || 1 }))} />
          </label>
          <div className="field risk-level-control">
            <span>拦截等级</span>
            <div className="risk-level-checks">
              {riskLevels.map(level => (
                <label key={`block-${level.value}`}>
                  <input type="checkbox" checked={configDraft.block_levels.includes(level.value)} onChange={() => toggleConfigLevel('block_levels', level.value)} />
                  {level.label}
                </label>
              ))}
            </div>
          </div>
          <div className="field risk-level-control">
            <span>提醒等级</span>
            <div className="risk-level-checks">
              {riskLevels.map(level => (
                <label key={`warn-${level.value}`}>
                  <input type="checkbox" checked={configDraft.warn_levels.includes(level.value)} onChange={() => toggleConfigLevel('warn_levels', level.value)} />
                  {level.label}
                </label>
              ))}
            </div>
          </div>
          <button className="btn btn-primary" type="button" disabled={savingConfig} onClick={saveRiskConfig}>{savingConfig ? '保存中...' : '保存配置'}</button>
        </div>
        <div className="risk-keyword-toolbar">
          <input className="input" value={keywordSearch} onChange={event => setKeywordSearch(event.target.value)} placeholder="搜索违禁词" />
          <select className="select" value={newKeywordLevel} onChange={event => setNewKeywordLevel(event.target.value as RiskLevel)}>
            {riskLevels.map(level => <option key={level.value} value={level.value}>{level.label}</option>)}
          </select>
          <input className="input" value={newKeyword} onChange={event => setNewKeyword(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') addKeyword() }} placeholder="新增违禁词" />
          <button className="btn btn-primary" disabled={savingKeyword || !newKeyword.trim()} type="button" onClick={addKeyword}>{savingKeyword ? '保存中...' : '添加'}</button>
        </div>
        <div className="risk-keyword-summary">
          <span>状态：{keywordConfig?.enabled === false ? '关闭' : '开启'}</span>
          <span>封禁阈值：{keywordConfig?.ban_threshold || 5} 次</span>
          <span>拦截等级：{(keywordConfig?.block_levels || []).join(', ') || '-'}</span>
          <span>提醒等级：{(keywordConfig?.warn_levels || []).join(', ') || '-'}</span>
        </div>
        <div className="risk-keyword-grid">
          {riskLevels.map(level => {
            const rows = filteredKeywords(level.value)
            const total = keywordConfig?.keywords?.[level.value]?.length || 0
            return (
              <section className={`risk-keyword-level risk-keyword-${level.value}`} key={level.value}>
                <div className="risk-keyword-level-head">
                  <div>
                    <strong>{level.label}</strong>
                    <span>{level.note}</span>
                  </div>
                  <b>{rows.length}/{total}</b>
                </div>
                <div className="risk-keyword-tags">
                  {rows.map(item => (
                    <span className="risk-keyword-tag" key={`${level.value}-${item}`}>
                      {item}
                      <button type="button" disabled={savingKeyword} onClick={() => removeKeyword(level.value, item)} aria-label={`删除 ${item}`}>×</button>
                    </span>
                  ))}
                  {!rows.length ? <span className="muted small-text">暂无匹配关键词</span> : null}
                </div>
              </section>
            )
          })}
        </div>
      </section>
      <div className="filter-bar action-filter-bar admin-filter-grid">
        <select className="select" value={riskLevel} onChange={event => { setRiskLevel(event.target.value); setPage(1) }}><option value="">全部等级</option><option value="high">high</option><option value="medium">medium</option><option value="low">low</option></select>
        <input className="input" value={userKeyword} onChange={event => { setUserKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder="邮箱 / user_id" />
        <input className="input" value={keyword} onChange={event => { setKeyword(event.target.value); setPage(1) }} onKeyDown={event => { if (event.key === 'Enter') load(1) }} placeholder="关键词 / 模块 / 命中文本" />
        <input className="input" type="date" value={from} onChange={event => { setFrom(event.target.value); setPage(1) }} aria-label="开始日期" />
        <input className="input" type="date" value={to} onChange={event => { setTo(event.target.value); setPage(1) }} aria-label="结束日期" />
        <button className="btn btn-secondary" onClick={() => load(1)}>查询</button>
      </div>
      <section className="admin-card table-card operation-card operation-sensitive">
        <table className="data-table risk-event-table">
          <thead><tr><th>时间</th><th>用户</th><th>风险</th><th>模块 / 来源</th><th>动作</th><th>计数状态</th><th>项目 / 任务</th><th>命中内容</th></tr></thead>
          <tbody>
            {rows.map(event => (
              <tr key={event.id}>
                <td className="risk-event-time-cell">{formatDate(event.created_at)}</td>
                <td className="risk-event-user-cell"><strong title={userLabel(event)}>{userLabel(event)}</strong><div className="muted small-text">{event.user_id}</div></td>
                <td><div className="risk-event-cell-stack"><span className={event.risk_level === 'low' ? 'status-warn' : 'status-bad'}>{event.risk_level}</span><strong title={event.keyword}>{event.keyword || '-'}</strong></div></td>
                <td><div className="risk-event-cell-stack"><strong>{event.feature || '-'}</strong><span className="muted small-text">{event.source || '-'}</span></div></td>
                <td>{event.action || '-'}</td>
                <td><span className={event.cleared ? 'status-warn' : 'status-good'}>{event.cleared ? '已清零' : '计入中'}</span></td>
                <td className="risk-event-task-cell"><strong>{event.project_id || '-'}</strong><div className="muted small-text">{event.task_id || ''}</div></td>
                <td className="risk-event-content-cell">
                  <p className="risk-event-content-preview">{event.matched_text || '-'}</p>
                  <button className="btn btn-secondary risk-event-view-button" type="button" onClick={() => setSelectedEvent(event)}><Eye size={14} />查看完整</button>
                </td>
              </tr>
            ))}
            {!rows.length ? <tr><td colSpan={8} className="muted">暂无匹配的风控事件</td></tr> : null}
          </tbody>
        </table>
        <Pagination page={page} total={total} pageSize={pageSize} totalPages={totalPages} onPageChange={load} labels={{ previous: '上一页', next: '下一页', page: '页', total: '总数' }} />
      </section>

      {selectedEvent ? (
        <div className="admin-modal-backdrop admin-drawer-backdrop" onMouseDown={() => setSelectedEvent(null)}>
          <section className="modal-panel admin-action-modal admin-action-drawer risk-event-detail-drawer" role="dialog" aria-modal="true" aria-labelledby="risk-event-detail-title" onMouseDown={event => event.stopPropagation()}>
            <div className="drawer-head">
              <div><span className="eyebrow"><ShieldAlert size={14} />风控事件详情</span><h2 id="risk-event-detail-title">{selectedEvent.keyword || '上游风控事件'}</h2><p>{formatDate(selectedEvent.created_at)} · {selectedEvent.id}</p></div>
              <button className="icon-button drawer-close" type="button" aria-label="关闭" onClick={() => setSelectedEvent(null)}><X size={18} /></button>
            </div>
            <div className="drawer-summary risk-event-detail-summary">
              <div><span>等级</span><strong className={selectedEvent.risk_level === 'low' ? 'status-warn' : 'status-bad'}>{selectedEvent.risk_level}</strong></div>
              <div><span>计数状态</span><strong className={selectedEvent.cleared ? 'status-warn' : 'status-good'}>{selectedEvent.cleared ? '已清零' : '计入中'}</strong></div>
              <div><span>模块</span><strong>{selectedEvent.feature || '-'}</strong></div>
              <div><span>来源 / 动作</span><strong>{selectedEvent.source || '-'} / {selectedEvent.action || '-'}</strong></div>
            </div>
            <div className="panel-body risk-event-detail-body">
              <section className="task-detail-section">
                <div className="task-detail-section-head"><h3>用户与任务</h3></div>
                <dl className="task-detail-metadata risk-event-detail-metadata">
                  <div><dt>用户</dt><dd>{userLabel(selectedEvent)}</dd></div>
                  <div><dt>User ID</dt><dd>{selectedEvent.user_id || '-'}</dd></div>
                  <div><dt>项目 ID</dt><dd>{selectedEvent.project_id || '-'}</dd></div>
                  <div><dt>任务 ID</dt><dd>{selectedEvent.task_id || '-'}</dd></div>
                </dl>
              </section>
              <section className="task-detail-section">
                <div className="task-detail-section-head"><h3>用户原文 / 命中文本</h3></div>
                <pre className="risk-event-detail-text">{selectedEvent.matched_text || '-'}</pre>
              </section>
            </div>
          </section>
        </div>
      ) : null}
    </>
  )
}
