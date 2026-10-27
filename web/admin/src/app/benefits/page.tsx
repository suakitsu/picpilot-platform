'use client'

import { useEffect, useMemo, useState } from 'react'
import { useI18n } from '@/components/i18n'
import { Pagination } from '@/components/Pagination'
import { useConfirm } from '@/components/ConfirmProvider'
import {
  assetUrl,
  BenefitConfig,
  createBenefitCardWave,
  createRedemptionCodes,
  getBenefitConfig,
  listBenefitRedemptionCodes,
  listRedemptionRecords,
  RedemptionCode,
  RedemptionRecord,
  updateBenefitConfig,
  uploadBenefitQr
} from '@/lib/api'

const defaultBenefitConfig: BenefitConfig = {
  enabled: false,
  title: '新人积分兑换码',
  description: '加入 QQ 群或微信群，联系管理员领取兑换码；到积分页兑换后即可用积分体验生成流程。',
  qq_group: '',
  qq_link: '',
  wechat_group: '',
  qq_qr_url: '',
  wechat_qr_url: '',
  redeem_tutorial: '1. 加入社群 2. 联系管理员领取兑换码 3. 到积分充值页兑换积分 4. 回到工作台生成图片',
  free_highlight_label: '兑换码会直接兑换成积分，生成仍按积分扣费'
}

function codeStatus(code: RedemptionCode, t: (key: string) => string) {
  const status = code.status || (code.enabled ? 'active' : 'disabled')
  if (status === 'active') return t('benefits.status.active')
  if (status === 'used_up') return t('benefits.status.usedUp')
  if (status === 'expired') return t('benefits.status.expired')
  if (status === 'disabled') return t('benefits.status.disabled')
  return status
}

function statusClass(code: RedemptionCode) {
  const status = code.status || (code.enabled ? 'active' : 'disabled')
  if (status === 'active') return 'status-good'
  if (status === 'used_up' || status === 'expired') return 'status-warn'
  return 'status-bad'
}

function parseWaveTierText(value: string) {
  return value.split(/\n+/)
    .map(line => line.trim())
    .filter(Boolean)
    .map(line => {
      const parts = line.replace(/[，,;；]/g, ' ').replace(/[xX*×]/g, ' ').split(/\s+/).filter(Boolean)
      return { credits: Number(parts[0] || 0), quantity: Number(parts[1] || 0), maxUses: 1 }
    })
    .filter(tier => tier.credits > 0 && tier.quantity > 0)
}

interface BatchFocus {
  id: string
  name?: string
}

interface LastWave {
  batchId: string
  batchName: string
  codes: RedemptionCode[]
}

interface LoadOptions {
  codeKeyword?: string
  recordKeyword?: string
  codeStatus?: string
}

export default function BenefitsPage() {
  const t = useI18n()
  const confirm = useConfirm()
  const [codes, setCodes] = useState<RedemptionCode[]>([])
  const [records, setRecords] = useState<RedemptionRecord[]>([])
  const [config, setConfig] = useState<BenefitConfig>(defaultBenefitConfig)
  const [credits, setCredits] = useState(20)
  const [maxUses, setMaxUses] = useState(1)
  const [quantity, setQuantity] = useState(1)
  const [validDays, setValidDays] = useState(7)
  const [benefitType, setBenefitType] = useState('')
  const [prefix, setPrefix] = useState('PP')
  const [note, setNote] = useState('')
  const [message, setMessage] = useState('')
  const [creating, setCreating] = useState(false)
  const [creatingWave, setCreatingWave] = useState(false)
  const [waveName, setWaveName] = useState('定时福利发卡')
  const [wavePrefix, setWavePrefix] = useState('FL')
  const [waveValidDays, setWaveValidDays] = useState(7)
  const [waveNote, setWaveNote] = useState('定时福利发卡，每人每轮限兑一张')
  const [waveTiers, setWaveTiers] = useState('1000 x 1\n500 x 3\n100 x 6')
  const [savingConfig, setSavingConfig] = useState(false)
  const [uploadingQr, setUploadingQr] = useState<'qq' | 'wechat' | ''>('')
  const [codeKeyword, setCodeKeyword] = useState('')
  const [codeStatusFilter, setCodeStatusFilter] = useState('')
  const [codePage, setCodePage] = useState(1)
  const [codeTotal, setCodeTotal] = useState(0)
  const [codeTotalPages, setCodeTotalPages] = useState(0)
  const [recordKeyword, setRecordKeyword] = useState('')
  const [recordPage, setRecordPage] = useState(1)
  const [recordTotal, setRecordTotal] = useState(0)
  const [recordTotalPages, setRecordTotalPages] = useState(0)
  const [activeBatch, setActiveBatch] = useState<BatchFocus | null>(null)
  const [lastWave, setLastWave] = useState<LastWave | null>(null)
  const [copyingBatch, setCopyingBatch] = useState(false)
  const pageSize = 20
  const quickDays = [1, 7, 30, 90]
  const benefitTypeValue = benefitType || t('benefits.defaultCommunityType')
  const noteValue = note || t('benefits.defaultCommunityNote')
  const parsedWaveTiers = useMemo(() => parseWaveTierText(waveTiers), [waveTiers])
  const waveTotalQuantity = parsedWaveTiers.reduce((sum, tier) => sum + tier.quantity, 0)
  const waveTotalCredits = parsedWaveTiers.reduce((sum, tier) => sum + tier.credits * tier.quantity, 0)

  async function load(nextCodePage = codePage, nextRecordPage = recordPage, options: LoadOptions = {}) {
    const nextCodeKeyword = options.codeKeyword ?? codeKeyword
    const nextRecordKeyword = options.recordKeyword ?? recordKeyword
    const nextCodeStatus = options.codeStatus ?? codeStatusFilter
    try {
      const [configRow, codeRows, recordRows] = await Promise.all([
        getBenefitConfig(),
        listBenefitRedemptionCodes({ keyword: nextCodeKeyword, status: nextCodeStatus, page: nextCodePage, pageSize }),
        listRedemptionRecords({ keyword: nextRecordKeyword, page: nextRecordPage, pageSize })
      ])
      setConfig({ ...defaultBenefitConfig, ...configRow })
      setCodes(codeRows.rows)
      setCodePage(codeRows.page)
      setCodeTotal(codeRows.total)
      setCodeTotalPages(codeRows.total_pages)
      setRecords(recordRows.rows)
      setRecordPage(recordRows.page)
      setRecordTotal(recordRows.total)
      setRecordTotalPages(recordRows.total_pages)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('benefits.loadFailed'))
    }
  }

  useEffect(() => { load() }, [])

  function updateConfigField<K extends keyof BenefitConfig>(key: K, value: BenefitConfig[K]) {
    setConfig(current => ({ ...current, [key]: value }))
  }

  async function saveConfig() {
    if (!await confirm({ title: t('benefits.configTitle'), message: t('benefits.confirmSaveConfig') })) return
    setSavingConfig(true)
    setMessage('')
    try {
      const saved = await updateBenefitConfig(config)
      setConfig({ ...defaultBenefitConfig, ...saved })
      setMessage(t('benefits.configSaved'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('benefits.configSaveFailed'))
    } finally {
      setSavingConfig(false)
    }
  }

  async function uploadQr(kind: 'qq' | 'wechat', file?: File) {
    if (!file) return
    if (!await confirm({ title: t('benefits.qrTitle'), message: t(kind === 'qq' ? 'benefits.confirmUploadQqQr' : 'benefits.confirmUploadWechatQr') })) return
    setUploadingQr(kind)
    setMessage('')
    try {
      const result = await uploadBenefitQr(kind, file)
      setConfig({ ...defaultBenefitConfig, ...result.config })
      setMessage(t(kind === 'qq' ? 'benefits.qqQrUploaded' : 'benefits.wechatQrUploaded'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('benefits.qrUploadFailed'))
    } finally {
      setUploadingQr('')
    }
  }

  async function generateCodes() {
    const summary = `${benefitTypeValue} / ${credits} ${t('common.credits')} / ${quantity} ${t('benefits.codes')}`
    if (!await confirm({ title: t('benefits.generateCodes'), message: `${t('benefits.confirmGenerate')} ${summary}？` })) return
    setCreating(true)
    setMessage('')
    try {
      const created = await createRedemptionCodes({ credits, maxUses, quantity, validDays, benefitType: benefitTypeValue, prefix, note: noteValue })
      setMessage(`${t('benefits.generated')}${created.length}`)
      await load(1, recordPage)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('benefits.generateFailed'))
    } finally {
      setCreating(false)
    }
  }

  function redemptionCodeText(rows: RedemptionCode[]) {
    return rows.map(code => code.code).filter(Boolean).join('\n')
  }

  async function copyCodes(rows: RedemptionCode[], label: string) {
    const text = redemptionCodeText(rows)
    if (!text) {
      setMessage('没有可复制的兑换码')
      return
    }
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        const textarea = document.createElement('textarea')
        textarea.value = text
        textarea.setAttribute('readonly', 'true')
        textarea.style.position = 'fixed'
        textarea.style.left = '-9999px'
        document.body.appendChild(textarea)
        textarea.select()
        document.execCommand('copy')
        document.body.removeChild(textarea)
      }
      setMessage(`${label}已按换行复制 ${rows.length} 个兑换码`)
    } catch (error) {
      setMessage(error instanceof Error ? `复制失败：${error.message}` : '复制失败，请手动复制')
    }
  }

  async function copyBatchCodes(batch: BatchFocus) {
    if (!batch.id || copyingBatch) return
    setCopyingBatch(true)
    try {
      let page = 1
      let totalPages = 1
      const allCodes: RedemptionCode[] = []
      do {
        const result = await listBenefitRedemptionCodes({ keyword: batch.id, page, pageSize: 100 })
        allCodes.push(...result.rows.filter(code => code.batch_id === batch.id))
        totalPages = result.total_pages || 1
        page += 1
      } while (page <= totalPages)
      await copyCodes(allCodes, `${batch.name || batch.id} 全部`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '批次兑换码复制失败')
    } finally {
      setCopyingBatch(false)
    }
  }

  async function applyBatchFilter(batch: BatchFocus) {
    if (!batch.id) return
    setActiveBatch(batch)
    setCodeKeyword(batch.id)
    setRecordKeyword(batch.id)
    setCodePage(1)
    setRecordPage(1)
    await load(1, 1, { codeKeyword: batch.id, recordKeyword: batch.id })
  }

  async function clearBatchFilter() {
    setActiveBatch(null)
    setCodeKeyword('')
    setRecordKeyword('')
    setCodePage(1)
    setRecordPage(1)
    await load(1, 1, { codeKeyword: '', recordKeyword: '' })
  }

  async function generateWave() {
    const tiers = parsedWaveTiers
    const total = tiers.reduce((sum, tier) => sum + tier.quantity, 0)
    if (!tiers.length) {
      setMessage('请按“积分 x 数量”填写至少一个档位，例如 1000 x 1')
      return
    }
    if (!await confirm({ title: '生成福利发卡波次', message: `将生成 ${total} 张卡，同一用户本轮只能兑换 1 张。确认生成？` })) return
    setCreatingWave(true)
    setMessage('')
    try {
      const created = await createBenefitCardWave({
        name: waveName.trim() || '定时福利发卡',
        prefix: wavePrefix.trim() || 'FL',
        validDays: waveValidDays,
        benefitType: '定时福利卡',
        note: waveNote.trim() || '定时福利发卡，每人每轮限兑一张',
        tiers
      })
      const nextWave = { batchId: created.batch_id, batchName: created.batch_name, codes: created.codes || [] }
      setLastWave(nextWave)
      await applyBatchFilter({ id: created.batch_id, name: created.batch_name })
      setMessage(`已生成福利波次 ${created.batch_name}，共 ${created.quantity} 张卡；已自动筛选本波，可一键换行复制`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '福利发卡波次生成失败')
    } finally {
      setCreatingWave(false)
    }
  }

  return (
    <>
      <div className="page-head"><div><h1>{t('benefits.title')}</h1><p>{t('benefits.desc')}</p></div><button className="btn btn-secondary" onClick={() => load(1, 1)}>{t('common.refresh')}</button></div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      <section className="admin-card operation-card">
        <div className="section-head">
          <div><h2>{t('benefits.configTitle')}</h2><p>{t('benefits.configDesc')}</p></div>
          <button className="btn btn-primary" disabled={savingConfig} onClick={saveConfig}>{savingConfig ? t('saving') : t('common.save')}</button>
        </div>
        <div className="benefit-config-grid">
          <label className="field"><span>{t('common.status')}</span><select className="select" value={config.enabled ? 'enabled' : 'disabled'} onChange={event => updateConfigField('enabled', event.target.value === 'enabled')}><option value="enabled">{t('common.enabled')}</option><option value="disabled">{t('common.disabled')}</option></select></label>
          <label className="field"><span>{t('common.title')}</span><input className="input" value={config.title} onChange={event => updateConfigField('title', event.target.value)} /></label>
          <label className="field"><span>{t('benefits.freeHighlightLabel')}</span><input className="input" value={config.free_highlight_label} onChange={event => updateConfigField('free_highlight_label', event.target.value)} /></label>
          <label className="field benefit-wide-field"><span>{t('common.content')}</span><textarea className="textarea" value={config.description} onChange={event => updateConfigField('description', event.target.value)} /></label>
          <label className="field"><span>{t('benefits.qqGroup')}</span><input className="input" value={config.qq_group} onChange={event => updateConfigField('qq_group', event.target.value)} placeholder={t('benefits.qqGroupPlaceholder')} /></label>
          <label className="field"><span>{t('benefits.qqLink')}</span><input className="input" value={config.qq_link || ''} onChange={event => updateConfigField('qq_link', event.target.value)} placeholder={t('benefits.qqLinkPlaceholder')} /></label>
          <label className="field"><span>{t('benefits.wechatGroup')}</span><input className="input" value={config.wechat_group} onChange={event => updateConfigField('wechat_group', event.target.value)} placeholder={t('benefits.wechatGroupPlaceholder')} /></label>
          <label className="field benefit-wide-field"><span>{t('benefits.tutorial')}</span><textarea className="textarea" value={config.redeem_tutorial} onChange={event => updateConfigField('redeem_tutorial', event.target.value)} /></label>
        </div>
        <div className="benefit-qr-grid">
          <div className="benefit-qr-card">
            <div><strong>{t('benefits.qqQr')}</strong><p>{t('benefits.qrDesc')}</p></div>
            <div className="qr-preview">{config.qq_qr_url ? <img src={assetUrl(config.qq_qr_url)} alt={t('benefits.qqQr')} /> : <span>{t('benefits.noQr')}</span>}</div>
            <label className="field"><span>{uploadingQr === 'qq' ? t('benefits.uploadingQr') : t('benefits.uploadQr')}</span><input className="input" type="file" accept="image/*" disabled={uploadingQr === 'qq'} onChange={event => uploadQr('qq', event.target.files?.[0])} /></label>
          </div>
          <div className="benefit-qr-card">
            <div><strong>{t('benefits.wechatQr')}</strong><p>{t('benefits.qrDesc')}</p></div>
            <div className="qr-preview">{config.wechat_qr_url ? <img src={assetUrl(config.wechat_qr_url)} alt={t('benefits.wechatQr')} /> : <span>{t('benefits.noQr')}</span>}</div>
            <label className="field"><span>{uploadingQr === 'wechat' ? t('benefits.uploadingQr') : t('benefits.uploadQr')}</span><input className="input" type="file" accept="image/*" disabled={uploadingQr === 'wechat'} onChange={event => uploadQr('wechat', event.target.files?.[0])} /></label>
          </div>
        </div>
      </section>
      <section className="admin-card operation-card operation-primary">
        <div className="section-head"><div><h2>{t('benefits.generate')}</h2><p>{t('benefits.generateDesc')}</p></div></div>
        <div className="admin-form-grid">
          <label className="field"><span>{t('benefits.type')}</span><input className="input" value={benefitTypeValue} onChange={event => setBenefitType(event.target.value)} placeholder={t('benefits.typePlaceholder')} /></label>
          <label className="field"><span>{t('common.credits')}</span><input className="input" type="number" min={1} value={credits} onChange={event => setCredits(Number(event.target.value))} /></label>
          <label className="field"><span>{t('benefits.validDays')}</span><input className="input" type="number" min={1} value={validDays} onChange={event => setValidDays(Number(event.target.value))} /></label>
          <label className="field"><span>{t('benefits.maxUses')}</span><input className="input" type="number" min={1} value={maxUses} onChange={event => setMaxUses(Number(event.target.value))} /></label>
          <label className="field"><span>{t('benefits.quantity')}</span><input className="input" type="number" min={1} max={200} value={quantity} onChange={event => setQuantity(Number(event.target.value))} /></label>
          <label className="field"><span>{t('benefits.prefix')}</span><input className="input" value={prefix} onChange={event => setPrefix(event.target.value)} /></label>
          <label className="field"><span>{t('common.remark')}</span><input className="input" value={noteValue} onChange={event => setNote(event.target.value)} placeholder={t('benefits.notePlaceholder')} /></label>
          <div className="field form-submit-field"><span>&nbsp;</span><button className="btn btn-primary" disabled={creating} onClick={generateCodes}>{creating ? t('benefits.generating') : t('benefits.generate')}</button></div>
        </div>
        <div className="quick-days action-chip-row">
          {quickDays.map(day => <button key={day} className={validDays === day ? 'btn btn-primary btn-compact' : 'btn btn-compact'} onClick={() => setValidDays(day)}>{day} {t('benefits.days')}</button>)}
        </div>
      </section>
      <section className="admin-card operation-card operation-primary benefit-wave-card">
        <div className="section-head">
          <div>
            <h2>定时福利发卡</h2>
            <p>一波活动可配置多个面额和数量，同一用户在同一波里只能兑换一张卡，下一波可继续兑换。</p>
          </div>
          <button className="btn btn-primary" disabled={creatingWave} onClick={generateWave}>{creatingWave ? '生成中...' : '生成一波福利卡'}</button>
        </div>
        <div className="admin-form-grid benefit-wave-grid">
          <label className="field"><span>波次名称</span><input className="input" value={waveName} onChange={event => setWaveName(event.target.value)} /></label>
          <label className="field"><span>前缀</span><input className="input" value={wavePrefix} onChange={event => setWavePrefix(event.target.value)} /></label>
          <label className="field"><span>有效天数</span><input className="input" type="number" min={1} value={waveValidDays} onChange={event => setWaveValidDays(Number(event.target.value))} /></label>
          <label className="field benefit-wide-field"><span>备注</span><input className="input" value={waveNote} onChange={event => setWaveNote(event.target.value)} /></label>
          <label className="field benefit-wide-field"><span>档位配置</span><textarea className="textarea benefit-wave-tiers" value={waveTiers} onChange={event => setWaveTiers(event.target.value)} /></label>
        </div>
        <div className="benefit-wave-preview">
          <div>
            <span>预计发卡</span>
            <strong>{waveTotalQuantity}</strong>
            <small>张</small>
          </div>
          <div>
            <span>总投放积分</span>
            <strong>{waveTotalCredits}</strong>
            <small>分</small>
          </div>
          <div>
            <span>档位明细</span>
            <strong>{parsedWaveTiers.length || '-'}</strong>
            <small>{parsedWaveTiers.map(tier => `${tier.credits}x${tier.quantity}`).join(' / ') || '请填写档位'}</small>
          </div>
        </div>
        <div className="quick-days action-chip-row">
          <span className="status-good">示例：1000 x 1 / 500 x 3 / 100 x 6</span>
          <span className="status-warn">同一 batch_id 每用户仅限兑换一次</span>
        </div>
        {lastWave ? (
          <div className="benefit-wave-result">
            <div className="benefit-wave-result-head">
              <div>
                <span className="eyebrow">最近生成波次</span>
                <h3>{lastWave.batchName}</h3>
                <p>{lastWave.batchId} · {lastWave.codes.length} 张，可按换行复制给运营发放。</p>
              </div>
              <div className="admin-actions">
                <button className="btn btn-secondary" type="button" onClick={() => applyBatchFilter({ id: lastWave.batchId, name: lastWave.batchName })}>筛选本波</button>
                <button className="btn btn-primary" type="button" onClick={() => copyCodes(lastWave.codes, '最近生成波次')}>换行复制全部</button>
              </div>
            </div>
            <textarea className="textarea benefit-code-copy-box" readOnly value={redemptionCodeText(lastWave.codes)} onFocus={event => event.currentTarget.select()} />
          </div>
        ) : null}
      </section>
      <section className="admin-card table-card">
        <div className="section-head"><div><h2>{t('benefits.codes')}</h2></div></div>
        {activeBatch ? (
          <div className="batch-filter-banner">
            <div>
              <strong>当前按批次筛选</strong>
              <span>{activeBatch.name || activeBatch.id} · {activeBatch.id}</span>
            </div>
              <div className="admin-actions">
                <button className="btn btn-secondary" type="button" disabled={copyingBatch} onClick={() => copyBatchCodes(activeBatch)}>{copyingBatch ? '复制中...' : '复制本批全部'}</button>
                <button className="btn" type="button" onClick={clearBatchFilter}>查看全部</button>
              </div>
          </div>
        ) : null}
        <div className="filter-bar action-filter-bar">
          <input className="input" value={codeKeyword} onChange={event => { setCodeKeyword(event.target.value); setCodePage(1); setActiveBatch(null) }} onKeyDown={event => { if (event.key === 'Enter') load(1, recordPage) }} placeholder="搜索兑换码 / 波次 / 备注" />
          <select className="select" value={codeStatusFilter} onChange={event => { setCodeStatusFilter(event.target.value); setCodePage(1) }}>
            <option value="">{t('allStatus')}</option>
            <option value="active">{t('benefits.status.active')}</option>
            <option value="used_up">{t('benefits.status.usedUp')}</option>
            <option value="expired">{t('benefits.status.expired')}</option>
            <option value="disabled">{t('benefits.status.disabled')}</option>
          </select>
          <button className="btn btn-secondary" type="button" onClick={() => copyCodes(codes, '当前页兑换码')}>换行复制当前页</button>
          <button className="btn btn-secondary" onClick={() => load(1, recordPage)}>{t('common.refresh')}</button>
        </div>
        <table className="data-table"><thead><tr><th>{t('benefits.code')}</th><th>{t('benefits.benefit')}</th><th>波次</th><th>{t('common.credits')}</th><th>{t('benefits.usedLimit')}</th><th>{t('common.status')}</th><th>{t('common.expiry')}</th><th>{t('common.remark')}</th></tr></thead><tbody>{codes.map(code => <tr key={code.id}><td><strong>{code.code}</strong></td><td>{code.benefit_type || t('benefits.defaultType')}</td><td>{code.batch_id ? <button className="batch-filter-button" type="button" onClick={() => applyBatchFilter({ id: code.batch_id || '', name: code.batch_name })}>{code.batch_name || code.batch_id}</button> : '-'}{code.batch_exclusive ? <div className="muted small-text">每人每波限兑 1 张</div> : null}</td><td>{code.credits}</td><td>{code.used_count}/{code.max_uses}</td><td><span className={statusClass(code)}>{codeStatus(code, t)}</span></td><td>{code.expires_at ? new Date(code.expires_at).toLocaleString() : t('common.longTerm')}</td><td>{code.note || '-'}</td></tr>)}{!codes.length ? <tr><td colSpan={8} className="muted">{t('benefits.noCodes')}</td></tr> : null}</tbody></table>
        <Pagination page={codePage} total={codeTotal} pageSize={pageSize} totalPages={codeTotalPages} onPageChange={nextPage => load(nextPage, recordPage)} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>
      <section className="admin-card table-card">
        <div className="section-head"><div><h2>{t('benefits.records')}</h2><p>{t('benefits.recordsDesc')}</p></div></div>
        <div className="filter-bar action-filter-bar">
          <input className="input" value={recordKeyword} onChange={event => { setRecordKeyword(event.target.value); setRecordPage(1); setActiveBatch(null) }} onKeyDown={event => { if (event.key === 'Enter') load(codePage, 1) }} placeholder="搜索兑换码 / 波次 / 用户" />
          <button className="btn btn-secondary" onClick={() => load(codePage, 1)}>{t('common.refresh')}</button>
        </div>
        <table className="data-table"><thead><tr><th>{t('common.time')}</th><th>{t('benefits.code')}</th><th>波次</th><th>{t('common.user')}</th><th>{t('nickname')}</th><th>{t('common.credits')}</th></tr></thead><tbody>{records.map(record => <tr key={record.id}><td>{new Date(record.redeemed_at).toLocaleString()}</td><td>{record.code}</td><td>{record.batch_id ? <button className="batch-filter-button" type="button" onClick={() => applyBatchFilter({ id: record.batch_id || '' })}>{record.batch_id}</button> : '-'}</td><td>{record.user_email_masked || record.user_id}</td><td>{record.user_nickname || '-'}</td><td>{record.credits}</td></tr>)}{!records.length ? <tr><td colSpan={6} className="muted">{t('benefits.noRecords')}</td></tr> : null}</tbody></table>
        <Pagination page={recordPage} total={recordTotal} pageSize={pageSize} totalPages={recordTotalPages} onPageChange={nextPage => load(codePage, nextPage)} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>
    </>
  )
}
