'use client'

import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { CheckCheck, KeyRound, Pencil, Plus, RefreshCw, Save, Trash2, X } from 'lucide-react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { useRuntime } from '@/components/AppRuntime'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { ApiError } from '@/lib/api'
import { checkProvider, deleteProvider, listProviders, saveProvider, type Capability, type ProviderCatalog, type ProviderConfig, type ProviderInput } from '@/lib/providers'

type Draft = ProviderInput & { has_api_key: boolean; api_key: string; existing: boolean }

function ProviderSettings() {
  const t = useI18n()
  const confirm = useConfirm()
  const [catalog, setCatalog] = useState<ProviderCatalog | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  function errorText(cause: unknown) {
    const message = cause instanceof Error ? cause.message : t('providersLoadFailed')
    const safe = draft?.api_key ? message.split(draft.api_key).join('[redacted]') : message
    return safe + (cause instanceof ApiError ? ' [' + cause.code + ']' + (cause.requestId ? ' / ' + cause.requestId : '') : '')
  }
  const refresh = useCallback(async () => {
    setLoading(true)
    try { setCatalog(await listProviders()) } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    let active = true
    listProviders().then(data => { if (active) setCatalog(data) }).catch(cause => {
      if (active) setError(cause instanceof Error ? cause.message : 'INVALID_PROVIDER_CONFIG')
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  function edit(provider?: ProviderConfig) {
    setError('')
    setNotice('')
    setDraft(provider ? { ...provider, api_key: '', existing: true } : {
      id: crypto.randomUUID(), name: '', capability: 'text', protocol: catalog?.allowed_protocols.text[0] || '',
      base_url: '', model: '', enabled: false, priority: 0, max_concurrency: 1, timeout_seconds: 120,
      has_api_key: false, execution_mode: 'http', api_key: '', existing: false
    })
  }
  function patch(values: Partial<Draft>) { setDraft(current => current ? { ...current, ...values } : null) }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!draft || busy) return
    setError('')
    if (!catalog?.allowed_protocols[draft.capability]?.includes(draft.protocol)) { setError(t('providerUnsupported')); return }
    if (draft.execution_mode !== 'pull_worker') try {
      const url = new URL(draft.base_url)
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error()
    } catch { setError(t('providerInvalidUrl')); return }
    if (!draft.name.trim() || !draft.model.trim() || !Number.isInteger(draft.priority) || Math.abs(draft.priority) > 10000
      || !Number.isInteger(draft.max_concurrency) || draft.max_concurrency < 1 || draft.max_concurrency > 100
      || !Number.isInteger(draft.timeout_seconds) || draft.timeout_seconds < 5 || draft.timeout_seconds > 3600) { setError(t('providerInvalid')); return }
    setBusy(true)
    try {
      await saveProvider(draft)
      setDraft(null)
      setNotice(t('providerSaved'))
      await refresh()
    } catch (cause) { setError(errorText(cause)) } finally { setBusy(false) }
  }
  async function remove(provider: ProviderConfig) {
    if (busy || !await confirm({ title: t('providerDelete'), message: t('providerDeleteConfirm') + ' ' + provider.name, tone: 'danger' })) return
    setBusy(true); setError(''); setNotice('')
    try {
      await deleteProvider(provider.id)
      setNotice(t('providerDeleted'))
      await refresh()
    } catch (cause) { setError(errorText(cause)) } finally { setBusy(false) }
  }
  async function check(provider: ProviderConfig) {
    if (busy) return
    if (draft) { setError(t('providerCheckUnsaved')); return }
    setBusy(true); setError(''); setNotice('')
    try {
      const result = await checkProvider(provider.id)
      if (result?.valid !== true || result?.errors?.length) throw new Error(result.errors?.join('; ') || result?.message || t('providerValidationFailed'))
      setNotice(t('providerChecked'))
    } catch (cause) { setError(errorText(cause)) } finally { setBusy(false) }
  }
  async function toggle(provider: ProviderConfig) {
    if (busy || draft) return
    setBusy(true); setError(''); setNotice('')
    setCatalog(current => current ? { ...current, providers: current.providers.map(row => row.id === provider.id ? { ...row, enabled: !provider.enabled } : row) } : current)
    try {
      await saveProvider({ ...provider, enabled: !provider.enabled })
      await refresh()
    } catch (cause) {
      setCatalog(current => current ? { ...current, providers: current.providers.map(row => row.id === provider.id ? provider : row) } : current)
      setError(errorText(cause))
    } finally { setBusy(false) }
  }
  const capabilityLabel = (value: string) => t(value === 'text' ? 'providerText' : value === 'image' ? 'providerImage' : 'providerVideo')

  return <>
    <div className="page-head"><div><h1>{t('providersTitle')}</h1><p>{t('providersDescription')}</p></div><div className="provider-actions">
      <button className="icon-button" type="button" title={t('providerRefresh')} aria-label={t('providerRefresh')} disabled={busy || loading || Boolean(draft)} onClick={() => { setError(''); void refresh().catch(cause => setError(errorText(cause))) }}><RefreshCw size={18} /></button>
      <button className="btn btn-primary" type="button" disabled={!catalog || busy || Boolean(draft)} onClick={() => edit()}><Plus size={16} />{t('providerAdd')}</button>
    </div></div>
    {error ? <div className="notice notice-error provider-error" role="alert">{error}</div> : null}
    {notice ? <div className="notice" role="status">{notice}</div> : null}
    {loading ? <p role="status">{t('providerLoading')}</p> : null}
    {!loading && catalog?.providers.length === 0 ? <p className="muted">{t('providersEmpty')}</p> : null}
    {draft ? <form className="provider-editor" onSubmit={save} aria-busy={busy}>
      <fieldset disabled={busy} className="provider-fields">
        <label className="field"><span>{t('providerName')}</span><input className="input" required maxLength={160} value={draft.name} onChange={e => patch({ name: e.target.value })} /></label>
        <label className="field"><span>{t('providerCapability')}</span><select className="select" value={draft.capability} onChange={e => { const capability = e.target.value as Capability; patch({ capability, protocol: catalog?.allowed_protocols[capability][0] || '', execution_mode: capability === 'video' ? 'http' : draft.execution_mode }) }}>{(['text','image','video'] as Capability[]).map(c => <option key={c} value={c}>{capabilityLabel(c)}</option>)}</select></label>
        <label className="field"><span>{t('providerProtocol')}</span><select className="select" required value={draft.protocol} onChange={e => patch({ protocol: e.target.value })}><option value="">{t('providerProtocol')}</option>{catalog?.allowed_protocols[draft.capability]?.map(p => <option key={p} value={p}>{p}</option>)}</select></label>
        <label className="field"><span>{t('providerExecutionMode')}</span><select className="select" value={draft.execution_mode || 'http'} onChange={e => patch({ execution_mode: e.target.value as 'http' | 'pull_worker', api_key: '' })}><option value="http">{t('providerHttp')}</option><option value="pull_worker" disabled={draft.capability === 'video' || draft.protocol === 'gemini_generate_content'}>{t('providerWorker')}</option></select></label>
        <label className="field"><span>{t('providerProfile')}</span><input className="input" value={draft.profile || ''} onChange={e => patch({ profile: e.target.value })} autoComplete="off" /></label>
        <label className="field provider-wide"><span>{t('providerBaseUrl')}</span><input className="input" type="url" required={draft.execution_mode !== 'pull_worker'} disabled={draft.execution_mode === 'pull_worker'} value={draft.base_url} autoComplete="off" spellCheck={false} onChange={e => patch({ base_url: e.target.value })} /></label>
        <label className="field"><span>{t('providerModel')}</span><input className="input" required maxLength={160} value={draft.model} placeholder={t('providerManual')} onChange={e => patch({ model: e.target.value })} /></label>
        <label className="field"><span>{t('providerKey')}</span><input className="input" type="password" disabled={draft.execution_mode === 'pull_worker'} value={draft.api_key} autoComplete="new-password" spellCheck={false} placeholder={t('providerKeyPlaceholder')} onChange={e => patch({ api_key: e.target.value })} /><small>{t(draft.has_api_key ? 'providerKeyStored' : 'providerKeyMissing')}</small></label>
        <label className="field"><span>{t('providerPriority')}</span><input className="input" type="number" required min={-10000} max={10000} step={1} value={draft.priority} onChange={e => patch({ priority: e.target.valueAsNumber })} /></label>
        <label className="field"><span>{t('providerConcurrency')}</span><input className="input" type="number" required min={1} max={100} step={1} value={draft.max_concurrency} onChange={e => patch({ max_concurrency: e.target.valueAsNumber })} /></label>
        <label className="field"><span>{t('providerTimeout')}</span><input className="input" type="number" required min={5} max={3600} step={1} value={draft.timeout_seconds} onChange={e => patch({ timeout_seconds: e.target.valueAsNumber })} /></label>
        <label className="provider-toggle"><input type="checkbox" checked={draft.enabled} onChange={e => patch({ enabled: e.target.checked })} /><span>{t('providerEnabled')}</span></label>
        <div className="provider-actions provider-wide"><button className="btn btn-primary" type="submit"><Save size={16} />{t(busy ? 'providerBusy' : 'providerSave')}</button><button className="btn" type="button" onClick={() => setDraft(null)}><X size={16} />{t('providerCancel')}</button></div>
      </fieldset>
    </form> : null}
    <div className="provider-list">
      {catalog?.providers.map(provider => <article className="provider-row" key={provider.id}>
        <div className="provider-summary"><h2>{provider.name}</h2><span className="muted">{capabilityLabel(provider.capability)} / {provider.protocol}</span><strong>{provider.model}</strong><span className="muted">{provider.base_url}</span><small><KeyRound size={14} />{t(provider.has_api_key ? 'providerKeyStored' : 'providerKeyMissing')}</small></div>
        <div className="provider-actions"><input type="checkbox" checked={provider.enabled} disabled={busy || Boolean(draft)} aria-label={t('providerEnabled') + ' ' + provider.name} onChange={() => void toggle(provider)} />
          <button className="icon-button" title={t('providerEdit')} aria-label={t('providerEdit') + ' ' + provider.name} disabled={busy || Boolean(draft)} onClick={() => edit(provider)}><Pencil size={16} /></button>
          <button className="icon-button" title={t('providerCheck')} aria-label={t('providerCheck') + ' ' + provider.name} disabled={busy || Boolean(draft)} onClick={() => void check(provider)}><CheckCheck size={16} /></button>
          <button className="icon-button" title={t('providerDelete')} aria-label={t('providerDelete') + ' ' + provider.name} disabled={busy || Boolean(draft)} onClick={() => void remove(provider)}><Trash2 size={16} /></button>
        </div>
      </article>)}
    </div>
    <p className="muted">{t('providerCheckHint')}</p>
  </>
}

export default function ProvidersPage() {
  const { config } = useRuntime()
  const t = useI18n()
  return <Shell><UserAuthGate>{config.edition === 'personal' ? <ProviderSettings /> : <p role="status">{t('providersPlatformManaged')}</p>}</UserAuthGate></Shell>
}
