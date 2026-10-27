'use client'

import { useEffect, useMemo, useState } from 'react'
import { useConfirm } from '@/components/ConfirmProvider'
import { CommerceCatalog, getAdminCommerceSuiteCatalog, saveAdminCommerceSuiteCatalog } from '@/lib/api'

function formatJson(value: unknown) {
  return JSON.stringify(value, null, 2)
}

export default function AdminCommerceSuitePage() {
  const [catalog, setCatalog] = useState<CommerceCatalog | null>(null)
  const [draft, setDraft] = useState('')
  const [message, setMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const confirm = useConfirm()

  async function load() {
    try {
      const data = await getAdminCommerceSuiteCatalog()
      setCatalog(data)
      setDraft(formatJson(data))
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '电商套图配置加载失败')
    }
  }

  useEffect(() => { load() }, [])

  const platformRows = useMemo(() => catalog?.platforms || [], [catalog])
  const templateRows = useMemo(() => catalog?.templates || [], [catalog])
  const enabledTemplateCount = templateRows.filter(item => item.enabled).length
  const templateByPlatform = useMemo(() => {
    const result = new Map<string, number>()
    templateRows.forEach(template => result.set(template.platform, (result.get(template.platform) || 0) + 1))
    return result
  }, [templateRows])

  async function save() {
    let parsed: CommerceCatalog
    try {
      parsed = JSON.parse(draft) as CommerceCatalog
    } catch {
      setMessage('JSON 格式不正确，请先修正。')
      return
    }
    if (!await confirm({ title: '保存电商套图配置', message: '保存后用户侧平台和 Prompt 模板会立即生效。' })) return
    setSaving(true)
    try {
      const saved = await saveAdminCommerceSuiteCatalog(parsed)
      setCatalog(saved)
      setDraft(formatJson(saved))
      setMessage('电商套图配置已保存')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>电商套图</h1>
          <p>统一维护平台 preset、图种 slots、Prompt 模板、overlay JSON 和费用规则。</p>
        </div>
        <div className="page-actions">
          <button className="btn btn-secondary" onClick={load}>刷新</button>
          <button className="btn btn-primary" disabled={saving} onClick={save}>{saving ? '保存中...' : '保存配置'}</button>
        </div>
      </div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}

      <section className="admin-card section operation-card">
        <div className="summary-grid">
          <div><span>平台</span><strong>{platformRows.length}</strong></div>
          <div><span>启用平台</span><strong>{platformRows.filter(item => item.enabled).length}</strong></div>
          <div><span>模板</span><strong>{templateRows.length}</strong></div>
          <div><span>启用模板</span><strong>{enabledTemplateCount}</strong></div>
        </div>
      </section>

      <div className="commerce-admin-grid">
        <section className="admin-card table-card operation-card">
          <div className="section-head">
            <div>
              <h2>平台与模板</h2>
              <p>Beta 平台可展示并生成，方便后续逐步调整 Prompt。</p>
            </div>
          </div>
          <table className="data-table">
            <thead><tr><th>平台</th><th>状态</th><th>默认图种</th><th>模板数</th></tr></thead>
            <tbody>
              {platformRows.map(platform => (
                <tr key={platform.code}>
                  <td><strong>{platform.name}</strong><div className="muted small-text">{platform.code}</div></td>
                  <td><span className={platform.enabled ? 'status-good' : 'status-bad'}>{platform.enabled ? (platform.beta ? 'Beta' : '启用') : '停用'}</span></td>
                  <td>{platform.default_slots?.length || 0}</td>
                  <td>{templateByPlatform.get(platform.code) || 0}</td>
                </tr>
              ))}
              {!platformRows.length ? <tr><td colSpan={4} className="muted">暂无平台配置</td></tr> : null}
            </tbody>
          </table>
          <div className="commerce-template-list">
            {templateRows.slice(0, 14).map(template => (
              <div key={template.id} className="commerce-template-row">
                <strong>{template.name}</strong>
                <span>{template.platform} / {template.slot} / {template.default_size}</span>
              </div>
            ))}
          </div>
        </section>

        <section className="admin-card operation-card">
          <div className="section-head">
            <div>
              <h2>Catalog JSON</h2>
              <p>可直接修改平台、slot、prompt_template、overlay_template、enabled、sort_order。</p>
            </div>
          </div>
          <textarea className="textarea commerce-catalog-editor" value={draft} onChange={event => setDraft(event.target.value)} spellCheck={false} />
        </section>
      </div>
    </>
  )
}
