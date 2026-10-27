'use client'

import { useEffect, useState } from 'react'
import { Copy, KeyRound, X } from 'lucide-react'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { VideoProviderConfigPanel } from '@/components/VideoProviderConfigPanel'
import {
  AiProviderConfig,
  AiRuntimeConfig,
  createAiWorkerToken,
  getAiRuntimeConfig,
  getAiWorkerState,
  probeAiRuntimeConfig,
  regenerateAiWorkerToken,
  revokeAiWorkerToken,
  rollbackAiRuntimeConfig,
  updateAiRuntimeConfig,
  validateAiRuntimeConfig
} from '@/lib/api'

type ProviderKind = 'image' | 'text'
const DEFAULT_GPT_TEXT_MODEL = 'gpt-5.5'
const GPT_TEXT_MODELS = [DEFAULT_GPT_TEXT_MODEL, 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna']

function newProvider(kind: ProviderKind): AiProviderConfig {
  const now = Date.now()
  return {
    id: `${kind}_${now}`,
    name: kind === 'image' ? '作图 AI' : '文案 AI',
    provider: '',
    enabled: false,
    base_url: '',
    model: '',
    priority: kind === 'image' ? 1 : 1,
    timeout_seconds: kind === 'image' ? 240 : 45,
    execution_mode: 'http',
    local_wait_seconds: 25,
    offline_action: 'fallback',
    lease_seconds: kind === 'image' ? 180 : 120,
    max_concurrency: 1,
    supported_sizes: kind === 'image' ? '1K,2K,4K' : '',
    env_key: '',
    notes: '',
    api_key: ''
  }
}

function emptyConfig(): AiRuntimeConfig {
  return {
    image: { enabled: false, base_url: '', model: '', api_key: '' },
    text: { enabled: false, base_url: '', model: '', api_key: '' },
    image_providers: [newProvider('image')],
    text_providers: [newProvider('text')],
    can_rollback: false
  }
}

function normalizeConfig(data: AiRuntimeConfig): AiRuntimeConfig {
  const imageProviders = data.image_providers?.length ? data.image_providers : [data.image || newProvider('image')]
  const textProviders = data.text_providers?.length ? data.text_providers : [data.text || newProvider('text')]
  const normalizedImageProviders = normalizeProviderOrder('image', imageProviders.map((item, index) => ({ ...newProvider('image'), ...item, id: item.id || `image_${index}`, api_key: '' })))
  const normalizedTextProviders = normalizeProviderOrder('text', textProviders.map((item, index) => ({ ...newProvider('text'), ...item, id: item.id || `text_${index}`, api_key: '' })))
  return {
    ...data,
    image_providers: normalizedImageProviders,
    text_providers: normalizedTextProviders,
    image: { ...data.image, api_key: '' },
    text: { ...data.text, api_key: '' }
  }
}

function configured(provider: AiProviderConfig) {
  return isPullWorker(provider) || Boolean((provider.api_key || '').trim() || provider.api_key_set)
}

function keySource(provider: AiProviderConfig) {
  if (isPullWorker(provider)) return 'worker'
  if ((provider.api_key || '').trim()) return 'pending'
  if (provider.api_key_set && provider.env_key) return 'env'
  if (provider.api_key_set) return 'saved'
  return 'missing'
}

function isPullWorker(provider: AiProviderConfig) {
  return provider.execution_mode === 'pull_worker'
}

function isLocalWorkerProvider(provider: AiProviderConfig) {
  return (provider.id || '').startsWith('local-cliproxy')
}

function isLocalImageProvider(kind: ProviderKind, provider: AiProviderConfig) {
  return kind === 'image' && provider.id === 'local-cliproxy-image'
}

function sortedProviders(kind: ProviderKind, rows: AiProviderConfig[]) {
  return [...rows].sort((left, right) => (left.priority ?? 100) - (right.priority ?? 100))
}

function isGptTextProvider(kind: ProviderKind, provider: AiProviderConfig) {
  if (kind !== 'text') return false
  if (provider.id === 'local-cliproxy-text') return true
  const text = `${provider.provider || ''} ${provider.base_url || ''}`.toLowerCase()
  return text.includes('chatgpt') || text.includes('98pro') || text.includes('openai') || text.includes('ai98pro') || text.includes('otokapi')
}

function isOpenImageProvider(kind: ProviderKind, provider: AiProviderConfig) {
  if (kind !== 'image') return false
  const text = `${provider.provider || ''} ${provider.base_url || ''}`.toLowerCase()
  return text.includes('xin-zhi-zhu') || text.includes('xinzhi') || text.includes('xinzhizhu') || text.includes('rua.chat') || text.includes('hfsyapi') || text.includes('aishuch') || text.includes('yunfei') || text.includes('img.yunfei.best')
}

function isYunfeiProvider(kind: ProviderKind, provider: AiProviderConfig) {
  if (kind !== 'image') return false
  const text = `${provider.id || ''} ${provider.name || ''} ${provider.provider || ''} ${provider.base_url || ''}`.toLowerCase()
  return text.includes('yunfei') || text.includes('img.yunfei.best')
}

function yunfeiEnvPlaceholder(provider: AiProviderConfig) {
  const text = `${provider.id || ''} ${provider.name || ''} ${provider.provider || ''} ${provider.supported_sizes || ''}`.toLowerCase()
  return text.includes('hires') || text.includes('2k') || text.includes('4k')
    ? 'OPENIMAGE_YUNFEI_HIRES_API_KEY'
    : 'OPENIMAGE_YUNFEI_1K_API_KEY'
}

function envKeyPlaceholder(kind: ProviderKind, provider: AiProviderConfig) {
  if (isYunfeiProvider(kind, provider)) return yunfeiEnvPlaceholder(provider)
  return kind === 'image' ? 'XIN_ZHI_ZHU_API_KEY' : 'OPENIMAGE_OTOKAPI_API_KEY'
}

function normalizeProviderOrder(kind: ProviderKind, rows: AiProviderConfig[]) {
  return sortedProviders(kind, rows).map((item, index) => ({ ...item, priority: index + 1 }))
}

function preserveProviderOrder(rows: AiProviderConfig[]) {
  return rows.map((item, index) => ({ ...item, priority: index + 1 }))
}

export default function Page() {
  const t = useI18n()
  const confirm = useConfirm()
  const [config, setConfig] = useState<AiRuntimeConfig>(emptyConfig())
  const [message, setMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const [probing, setProbing] = useState<ProviderKind | ''>('')
  const [workerTokenName, setWorkerTokenName] = useState('PicPilot Local Worker')
  const [workerTokenBusy, setWorkerTokenBusy] = useState(false)
  const [oneTimeToken, setOneTimeToken] = useState('')

  async function load() {
    try {
      const data = await getAiRuntimeConfig()
      setConfig(normalizeConfig(data))
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.loadFailed'))
    }
  }

  useEffect(() => {
    load()
    const timer = window.setInterval(() => {
      getAiWorkerState()
        .then(workers => setConfig(current => ({ ...current, workers })))
        .catch(() => undefined)
    }, 15000)
    return () => window.clearInterval(timer)
  }, [])

  function providers(kind: ProviderKind) {
    return kind === 'image' ? (config.image_providers || []) : (config.text_providers || [])
  }

  function setProviders(kind: ProviderKind, rows: AiProviderConfig[]) {
    setConfig(current => ({ ...current, [kind === 'image' ? 'image_providers' : 'text_providers']: rows }))
  }

  function patchProvider(kind: ProviderKind, id: string | undefined, patch: Partial<AiProviderConfig>) {
    setProviders(kind, providers(kind).map(item => item.id === id ? { ...item, ...patch } : item))
  }

  function toggleProvider(kind: ProviderKind, id: string | undefined) {
    const selected = providers(kind).find(item => item.id === id)
    const nextEnabled = !selected?.enabled
    setProviders(kind, providers(kind).map(item => item.id === id ? { ...item, enabled: nextEnabled } : item))
  }

  function disableAll(kind: ProviderKind) {
    setProviders(kind, providers(kind).map(item => ({ ...item, enabled: false })))
  }

  function addProvider(kind: ProviderKind) {
    setProviders(kind, preserveProviderOrder([...sortedProviders(kind, providers(kind)), newProvider(kind)]))
  }

  function removeProvider(kind: ProviderKind, id: string | undefined) {
    const rows = providers(kind)
    if (rows.length <= 1) {
      disableAll(kind)
      return
    }
    setProviders(kind, preserveProviderOrder(sortedProviders(kind, rows).filter(item => item.id !== id)))
  }

  function moveProvider(kind: ProviderKind, id: string | undefined, direction: -1 | 1) {
    const rows = sortedProviders(kind, providers(kind))
    const index = rows.findIndex(item => item.id === id)
    const nextIndex = index + direction
    if (index < 0 || nextIndex < 0 || nextIndex >= rows.length) return
    const nextRows = [...rows]
    const [item] = nextRows.splice(index, 1)
    nextRows.splice(nextIndex, 0, item)
    setProviders(kind, preserveProviderOrder(nextRows))
  }

  async function validate() {
    try {
      const result = await validateAiRuntimeConfig(config)
      setMessage(result.message || t('aiConfig.validated'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.validateFailed'))
    }
  }

  async function probe(kind: ProviderKind) {
    const enabled = sortedProviders(kind, providers(kind).filter(item => item.enabled))
    const probeTarget = enabled.find(configured)
    if (!enabled.length) {
      setMessage(t(kind === 'image' ? 'aiConfig.imageUsersDisabled' : 'aiConfig.textUsersDisabled'))
      return
    }
    if (!probeTarget) {
      setMessage(t('aiConfig.enabledButNoKey'))
      return
    }
    if (!isPullWorker(probeTarget)
      && !await confirm({ title: t('aiConfig.probe'), message: t(kind === 'image' ? 'aiConfig.confirmProbeImage' : 'aiConfig.confirmProbeText') })) return
    setProbing(kind)
    setMessage('')
    try {
      const result = await probeAiRuntimeConfig(kind, config)
      setMessage(`${result.message || t('aiConfig.probePassed')}${typeof result.latency_ms === 'number' ? ` 延迟：${result.latency_ms}ms` : ''}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.probeFailed'))
    } finally {
      setProbing('')
    }
  }

  async function probeProvider(kind: ProviderKind, id: string | undefined) {
    const selected = providers(kind).find(item => item.id === id)
    if (!selected) return
    if (!configured(selected)) {
      setMessage(t('aiConfig.enabledButNoKey'))
      return
    }
    if (!isPullWorker(selected)
      && !await confirm({ title: t('aiConfig.probe'), message: t(kind === 'image' ? 'aiConfig.confirmProbeImage' : 'aiConfig.confirmProbeText') })) return
    setProbing(kind)
    setMessage('')
    try {
      const scopedProviders = providers(kind).map(item => ({ ...item, enabled: item.id === selected.id }))
      const result = await probeAiRuntimeConfig(kind, {
        ...config,
        [kind === 'image' ? 'image_providers' : 'text_providers']: scopedProviders
      })
      setMessage(`${selected.name || selected.provider || selected.model || t('aiConfig.providerName')}：${result.message || t('aiConfig.probePassed')}${typeof result.latency_ms === 'number' ? ` 延迟：${result.latency_ms}ms` : ''}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.probeFailed'))
    } finally {
      setProbing('')
    }
  }

  async function save() {
    const imageEnabled = providers('image').some(item => item.enabled)
    const textEnabled = providers('text').some(item => item.enabled)
    const disabledMessage = !imageEnabled || !textEnabled
      ? `${t('aiConfig.confirmSave')}\n\n${!imageEnabled ? t('aiConfig.imageDisabledWarning') : ''}${!imageEnabled && !textEnabled ? '\n' : ''}${!textEnabled ? t('aiConfig.textDisabledWarning') : ''}`
      : t('aiConfig.confirmSave')
    if (!await confirm({ title: t('aiConfig.title'), message: disabledMessage, tone: !imageEnabled || !textEnabled ? 'danger' : 'default' })) return
    setSaving(true)
    try {
      const payload = {
        ...config,
        image_providers: normalizeProviderOrder('image', providers('image')),
        text_providers: normalizeProviderOrder('text', providers('text'))
      }
      const saved = await updateAiRuntimeConfig(payload)
      setConfig(normalizeConfig(saved))
      setMessage(t('aiConfig.saved'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function rollback() {
    if (!await confirm({ title: t('aiConfig.rollback'), message: t('aiConfig.confirmRollback'), tone: 'danger' })) return
    setSaving(true)
    try {
      const rolledBack = await rollbackAiRuntimeConfig()
      setConfig(normalizeConfig(rolledBack))
      setMessage(t('aiConfig.rolledBack'))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.rollbackFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function createWorkerToken() {
    setWorkerTokenBusy(true)
    setMessage('')
    try {
      const created = await createAiWorkerToken(workerTokenName)
      setOneTimeToken(created.token)
      await load()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.workerTokenFailed'))
    } finally {
      setWorkerTokenBusy(false)
    }
  }

  async function regenerateWorkerToken(tokenId: string) {
    if (!await confirm({
      title: t('aiConfig.workerTokenRegenerate'),
      message: t('aiConfig.workerTokenRegenerateConfirm'),
      tone: 'danger'
    })) return
    setWorkerTokenBusy(true)
    try {
      const created = await regenerateAiWorkerToken(tokenId)
      setOneTimeToken(created.token)
      await load()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.workerTokenFailed'))
    } finally {
      setWorkerTokenBusy(false)
    }
  }

  async function revokeWorkerToken(tokenId: string) {
    if (!await confirm({
      title: t('aiConfig.workerTokenRevoke'),
      message: t('aiConfig.workerTokenRevokeConfirm'),
      tone: 'danger'
    })) return
    setWorkerTokenBusy(true)
    try {
      await revokeAiWorkerToken(tokenId)
      await load()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('aiConfig.workerTokenFailed'))
    } finally {
      setWorkerTokenBusy(false)
    }
  }

  async function copyOneTimeToken() {
    if (!oneTimeToken) return
    await navigator.clipboard.writeText(oneTimeToken)
    setMessage(t('aiConfig.workerTokenCopied'))
  }

  function workerPanel() {
    const workers = config.workers
    const tokens = workers?.tokens || []
    const instances = workers?.instances || []
    return (
      <section className="admin-card operation-card ai-worker-panel">
        <div className="section-head ai-provider-head">
          <div>
            <h2>{t('aiConfig.workerTitle')}</h2>
            <p>{t('aiConfig.workerDesc')}</p>
          </div>
          <div className="ai-worker-create">
            <input
              className="input"
              value={workerTokenName}
              onChange={event => setWorkerTokenName(event.target.value)}
              placeholder={t('aiConfig.workerTokenName')}
            />
            <button className="btn btn-primary" type="button" disabled={workerTokenBusy} onClick={createWorkerToken}>
              {t('aiConfig.workerTokenCreate')}
            </button>
          </div>
        </div>
        {oneTimeToken ? (
          <div className="ai-worker-token-once" role="status" aria-live="polite">
            <div className="ai-worker-token-once-head">
              <span className="ai-worker-token-once-icon" aria-hidden="true">
                <KeyRound size={18} />
              </span>
              <div className="ai-worker-token-once-copy">
                <strong>{t('aiConfig.workerTokenOnce')}</strong>
                <span>{t('aiConfig.workerTokenOnceHint')}</span>
              </div>
              <button
                className="icon-button ai-worker-token-dismiss"
                type="button"
                aria-label={t('aiConfig.workerTokenDismiss')}
                title={t('aiConfig.workerTokenDismiss')}
                onClick={() => setOneTimeToken('')}
              >
                <X size={17} />
              </button>
            </div>
            <div className="ai-worker-token-credential">
              <code>{oneTimeToken}</code>
              <button className="btn btn-primary" type="button" onClick={copyOneTimeToken}>
                <Copy size={15} />
                {t('aiConfig.workerTokenCopy')}
              </button>
            </div>
          </div>
        ) : null}
        <div className="ai-worker-summary-grid">
          <div>
            <span>{t('aiConfig.workerOnline')}</span>
            <strong>{instances.filter(item => item.online).length}</strong>
          </div>
          <div>
            <span>{t('aiConfig.workerActiveTasks')}</span>
            <strong>{instances.reduce((total, item) => total + (item.active_tasks || 0), 0)}</strong>
          </div>
          <div>
            <span>{t('aiConfig.workerTokens')}</span>
            <strong>{tokens.filter(item => item.enabled).length}</strong>
          </div>
        </div>
        <div className="ai-worker-lists">
          <div>
            <h3>{t('aiConfig.workerInstances')}</h3>
            {instances.length ? instances.map(instance => (
              <div className="ai-worker-row" key={instance.worker_id}>
                <div>
                  <strong>{instance.worker_id}</strong>
                  <span>{instance.capabilities.join(' / ') || '-'}</span>
                </div>
                <span className={instance.online ? 'status-good' : 'status-bad'}>
                  {instance.online ? t('aiConfig.workerOnlineState') : t('aiConfig.workerOfflineState')}
                </span>
                <span>{instance.active_tasks}/{instance.max_concurrency}</span>
                <span>{instance.last_heartbeat_at ? new Date(instance.last_heartbeat_at).toLocaleString() : '-'}</span>
              </div>
            )) : <p className="muted">{t('aiConfig.workerNoInstances')}</p>}
          </div>
          <div>
            <h3>{t('aiConfig.workerTokens')}</h3>
            {tokens.length ? tokens.map(token => (
              <div className="ai-worker-row" key={token.id}>
                <div>
                  <strong>{token.name}</strong>
                  <span>{token.token_prefix}...</span>
                </div>
                <span className={token.enabled ? 'status-good' : 'status-bad'}>
                  {token.enabled ? t('aiConfig.workerTokenActive') : t('aiConfig.workerTokenRevoked')}
                </span>
                <div className="admin-actions">
                  <button className="btn btn-secondary" type="button" disabled={workerTokenBusy} onClick={() => regenerateWorkerToken(token.id)}>{t('aiConfig.workerTokenRegenerate')}</button>
                  <button className="btn btn-danger" type="button" disabled={workerTokenBusy || !token.enabled} onClick={() => revokeWorkerToken(token.id)}>{t('aiConfig.workerTokenRevoke')}</button>
                </div>
              </div>
            )) : <p className="muted">{t('aiConfig.workerNoTokens')}</p>}
          </div>
        </div>
      </section>
    )
  }

  function providerSection(kind: ProviderKind, title: string, desc: string) {
    const rows = providers(kind)
    const enabled = sortedProviders(kind, rows.filter(item => item.enabled))
    const incompleteEnabled = enabled.some(item => !configured(item))
    const probeTarget = enabled.find(configured)
    const enabledLabel = enabled.map(item => item.name || item.provider || item.model).join(' -> ')
    return (
      <section className="admin-card operation-card ai-provider-section">
        <div className="section-head ai-provider-head">
          <div>
            <h2>{title}</h2>
            <p>{desc}</p>
            <div className={enabled.length && !incompleteEnabled ? 'status-good ai-provider-state' : 'status-bad ai-provider-state'}>
              {enabled.length
                ? `${t('aiConfig.providerChain')}：${enabledLabel}${incompleteEnabled ? ` · ${t('aiConfig.enabledButNoKey')}` : ''}`
                : t('aiConfig.allDisabled')}
            </div>
          </div>
          <div className="admin-actions row-action-group">
            <button className="btn btn-secondary" type="button" onClick={() => addProvider(kind)}>{t('aiConfig.addProvider')}</button>
            <button className="btn btn-secondary" type="button" disabled={!probeTarget || Boolean(incompleteEnabled) || probing === kind} onClick={() => probe(kind)}>{probing === kind ? t('aiConfig.probing') : t('aiConfig.probe')}</button>
            <button className="btn btn-danger" type="button" disabled={!rows.some(item => item.enabled)} onClick={() => disableAll(kind)}>{t('aiConfig.disableAll')}</button>
          </div>
        </div>
        <div className={enabled.length && !incompleteEnabled ? 'ai-provider-service-note' : 'ai-provider-service-note ai-provider-service-note-danger'}>
          {enabled.length && !incompleteEnabled
            ? t(kind === 'image' ? 'aiConfig.imageUsersAvailable' : 'aiConfig.textUsersAvailable')
            : t(kind === 'image' ? 'aiConfig.imageUsersDisabled' : 'aiConfig.textUsersDisabled')}
        </div>
        <div className="ai-provider-list">
          {sortedProviders(kind, rows).map((item, index) => providerCard(kind, item, index, rows.length))}
        </div>
      </section>
    )
  }

  function providerCard(kind: ProviderKind, provider: AiProviderConfig, index: number, total: number) {
    const pullWorker = isPullWorker(provider)
    const workerStatus = provider.id ? config.workers?.providers?.[provider.id] : undefined
    return (
      <article className={`ai-provider-card ${provider.enabled ? 'ai-provider-card-active' : ''}`} key={provider.id}>
        <div className="ai-provider-card-head">
          <div>
            <strong>{`${t('aiConfig.orderLabel')}${index + 1} · `}{provider.name || provider.provider || provider.model || t('aiConfig.providerName')}</strong>
            <span>{provider.enabled ? (configured(provider) ? t('aiConfig.enabledForUsers') : t('aiConfig.enabledButNoKey')) : t('aiConfig.disabledForUsers')}</span>
          </div>
          <div className="admin-actions row-action-group">
            <button className="btn btn-secondary" type="button" disabled={index === 0} onClick={() => moveProvider(kind, provider.id, -1)}>{t('aiConfig.moveUp')}</button>
            <button className="btn btn-secondary" type="button" disabled={index >= total - 1} onClick={() => moveProvider(kind, provider.id, 1)}>{t('aiConfig.moveDown')}</button>
            <button className="btn btn-secondary" type="button" disabled={!configured(provider) || probing === kind} onClick={() => probeProvider(kind, provider.id)}>{probing === kind ? t('aiConfig.probing') : t('aiConfig.probe')}</button>
            <button className={provider.enabled ? 'btn btn-secondary' : 'btn'} type="button" onClick={() => toggleProvider(kind, provider.id)}>{provider.enabled ? t('aiConfig.disableProvider') : t('aiConfig.enableProvider')}</button>
            <button className="btn btn-danger" type="button" disabled={provider.id?.startsWith('local-cliproxy')} onClick={() => removeProvider(kind, provider.id)}>{t('aiConfig.removeProvider')}</button>
          </div>
        </div>
        <div className="ai-provider-meta-row">
          <span>{pullWorker ? t('aiConfig.pullWorkerMode') : (provider.base_url || t('aiConfig.noBaseUrl'))}</span>
          <span>{provider.model || t('aiConfig.noModel')}</span>
          <span>
            {pullWorker
              ? (workerStatus?.online ? t('aiConfig.workerOnlineState') : t('aiConfig.workerOfflineState'))
              : keySource(provider) === 'env'
                ? `${t('aiConfig.keyConfigured')} · ${provider.env_key}`
                : configured(provider) ? t('aiConfig.keyConfigured') : t('aiConfig.keyMissing')}
          </span>
        </div>
        <div className="admin-form-grid ai-config-grid">
          <label className="field">
            <span>{t('common.name')}</span>
            <input className="input" value={provider.name || ''} onChange={event => patchProvider(kind, provider.id, { name: event.target.value })} placeholder={kind === 'image' ? 'HFSY 作图' : 'DeepSeek 文案'} />
          </label>
          <label className="field">
            <span>{t('common.provider')}</span>
            <input className="input" value={provider.provider || ''} disabled={isLocalWorkerProvider(provider)} onChange={event => patchProvider(kind, provider.id, { provider: event.target.value })} placeholder={kind === 'image' ? 'hfsyapi' : 'deepseek'} />
          </label>
          <label className="field">
            <span>{t('aiConfig.executionMode')}</span>
            <select
              className="input"
              value={provider.execution_mode || 'http'}
              disabled={isLocalWorkerProvider(provider)}
              onChange={event => patchProvider(kind, provider.id, { execution_mode: event.target.value as 'http' | 'pull_worker' })}
            >
              <option value="http">HTTP</option>
              <option value="pull_worker">Pull Worker</option>
            </select>
          </label>
          <label className="field">
            <span>{t('aiConfig.priority')}</span>
            <input className="input" type="number" value={provider.priority ?? 100} onChange={event => patchProvider(kind, provider.id, { priority: Number(event.target.value || 100) })} />
          </label>
          {!pullWorker ? (
          <label className="field">
            <span>{t('aiConfig.baseUrl')}</span>
            <input className="input" value={provider.base_url || ''} onChange={event => patchProvider(kind, provider.id, { base_url: event.target.value })} placeholder="https://api.example.com" />
          </label>
          ) : null}
          <label className="field">
            <span>{t('aiConfig.model')}</span>
            {isLocalImageProvider(kind, provider) ? (
              <input className="input" value="gpt-image-2" disabled />
            ) : isYunfeiProvider(kind, provider) ? (
              <input className="input" value="gpt-image-2" disabled />
            ) : isGptTextProvider(kind, provider) ? (
              <select className="input" value={GPT_TEXT_MODELS.includes(provider.model || '') ? provider.model : DEFAULT_GPT_TEXT_MODEL} onChange={event => patchProvider(kind, provider.id, { model: event.target.value })}>
                {GPT_TEXT_MODELS.map(model => <option key={model} value={model}>{model}</option>)}
              </select>
            ) : isOpenImageProvider(kind, provider) ? (
              <input className="input" value={provider.model || ''} onChange={event => patchProvider(kind, provider.id, { model: event.target.value })} placeholder="gpt-image-2" />
            ) : (
              <input className="input" value={provider.model || ''} onChange={event => patchProvider(kind, provider.id, { model: event.target.value })} placeholder={kind === 'image' ? 'gpt-image-2' : 'deepseek-v4-flash'} />
            )}
          </label>
          {!pullWorker ? (
          <label className="field ai-key-field">
            <span>{t('aiConfig.apiKey')}</span>
            <input className="input" type="password" value={provider.api_key || ''} onChange={event => patchProvider(kind, provider.id, { api_key: event.target.value })} placeholder={provider.api_key_set ? t('aiConfig.keepExistingKey') : t('aiConfig.enterApiKey')} />
            <small className="field-hint">{provider.api_key_set ? `${t('aiConfig.currentKey')}${provider.api_key_masked || '****'}` : t('aiConfig.noKey')}</small>
          </label>
          ) : null}
          {!pullWorker ? (
          <label className="field">
            <span>{t('aiConfig.envKey')}</span>
            <input className="input" value={provider.env_key || ''} onChange={event => patchProvider(kind, provider.id, { env_key: event.target.value })} placeholder={envKeyPlaceholder(kind, provider)} />
          </label>
          ) : null}
          {!pullWorker ? (
          <label className="field">
            <span>{t('aiConfig.timeoutSeconds')}</span>
            <input className="input" type="number" value={provider.timeout_seconds ?? (kind === 'image' ? 240 : 45)} onChange={event => patchProvider(kind, provider.id, { timeout_seconds: Number(event.target.value || (kind === 'image' ? 240 : 45)) })} />
          </label>
          ) : (
            <>
              <label className="field">
                <span>{t('aiConfig.localWaitSeconds')}</span>
                <input className="input" type="number" min={1} max={300} value={provider.local_wait_seconds ?? 25} onChange={event => patchProvider(kind, provider.id, { local_wait_seconds: Number(event.target.value || 25) })} />
              </label>
              <label className="field">
                <span>{t('aiConfig.offlineAction')}</span>
                <select className="input" value={provider.offline_action || 'fallback'} onChange={event => patchProvider(kind, provider.id, { offline_action: event.target.value as 'queue' | 'fallback' })}>
                  <option value="fallback">{t('aiConfig.offlineFallback')}</option>
                  <option value="queue">{t('aiConfig.offlineQueue')}</option>
                </select>
              </label>
              <label className="field">
                <span>{t('aiConfig.leaseSeconds')}</span>
                <input className="input" type="number" min={30} max={1800} value={provider.lease_seconds ?? 120} onChange={event => patchProvider(kind, provider.id, { lease_seconds: Number(event.target.value || 120) })} />
              </label>
              <label className="field">
                <span>{t('aiConfig.maxConcurrency')}</span>
                <input className="input" type="number" min={1} max={32} value={provider.max_concurrency ?? 1} onChange={event => patchProvider(kind, provider.id, { max_concurrency: Number(event.target.value || 1) })} />
              </label>
              {isLocalImageProvider(kind, provider) ? (
                <label className="field">
                  <span>{t('aiConfig.localImageCost')}</span>
                  <input className="input" type="number" min={0} max={1000} step="0.0001" value={provider.actual_cost ?? 0.001} onChange={event => patchProvider(kind, provider.id, { actual_cost: Number(event.target.value || 0) })} />
                  <small className="field-hint">{t('aiConfig.localImageCostHint')}</small>
                </label>
              ) : null}
              <div className="field ai-worker-provider-status">
                <span>{t('aiConfig.workerProviderStatus')}</span>
                <strong className={workerStatus?.online ? 'status-good' : 'status-bad'}>
                  {workerStatus?.online ? t('aiConfig.workerOnlineState') : t('aiConfig.workerOfflineState')}
                </strong>
                <small className="field-hint">
                  {t('aiConfig.workerActiveTasks')} {workerStatus?.active_tasks || 0}/{workerStatus?.max_concurrency || provider.max_concurrency || 1}
                </small>
              </div>
            </>
          )}
          {kind === 'image' ? (
            <label className="field">
              <span>{t('aiConfig.supportedSizes')}</span>
              <input className="input" value={provider.supported_sizes || ''} onChange={event => patchProvider(kind, provider.id, { supported_sizes: event.target.value })} placeholder="1K,2K,4K" />
            </label>
          ) : null}
          <label className="field">
            <span>{t('aiConfig.notes')}</span>
            <input className="input" value={provider.notes || ''} onChange={event => patchProvider(kind, provider.id, { notes: event.target.value })} placeholder={kind === 'image' ? '上游说明/价格备注' : '用途说明/稳定性备注'} />
          </label>
        </div>
      </article>
    )
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>{t('aiConfig.title')}</h1>
          <p>{t('aiConfig.desc')}</p>
        </div>
        <div className="admin-actions row-action-group">
          <button className="btn btn-secondary" onClick={validate} disabled={saving}>{t('aiConfig.validate')}</button>
          <button className="btn btn-primary" onClick={save} disabled={saving}>{saving ? t('common.submitting') : t('common.save')}</button>
          <button className="btn btn-danger" onClick={rollback} disabled={saving || !config.can_rollback}>{t('aiConfig.rollback')}</button>
        </div>
      </div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      {workerPanel()}
      {providerSection('image', t('aiConfig.imageTitle'), t('aiConfig.imageDesc'))}
      {providerSection('text', t('aiConfig.textTitle'), t('aiConfig.textDesc'))}
      <VideoProviderConfigPanel />
    </>
  )
}
