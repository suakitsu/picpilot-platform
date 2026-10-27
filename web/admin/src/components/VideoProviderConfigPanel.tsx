'use client'

import { Activity, Film, KeyRound, Plus, Save, ShieldCheck, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { getVideoAdminConfig, updateVideoAdminConfig, VideoAdminConfig } from '@/lib/api'

function defaultModelConfigs(): VideoAdminConfig['model_configs'] {
  const variants = (rows: Array<[string, number, number]>) => rows.map(([resolution, duration_seconds, price]) => ({ resolution, duration_seconds, price, enabled: true }))
  return [
    {
      code: 'gemini-omni', name: 'Gemini Omni', description: '自然运动与商品镜头理解', provider: 'gemini',
      model: 'gemini-omni-flash-preview', enabled: true, default_resolution: '720p', default_duration_seconds: 10,
      duration_selectable: false, audio_toggle: false, max_reference_images: 3, aspect_ratios: ['9:16', '16:9'], resolutions: ['720p'], durations: [10],
      variants: variants([['720p', 10, 950]])
    },
    {
      code: 'agnes-v2', name: 'Agnes V2.0', description: '单图排队生成，适合轻量商品动态', provider: 'agnes',
      model: 'agnes-video-v2.0', enabled: true, default_resolution: '720p', default_duration_seconds: 10,
      duration_selectable: false, audio_toggle: false, max_reference_images: 1, aspect_ratios: ['9:16', '16:9', '1:1', '3:4', '4:3'], resolutions: ['720p'], durations: [10],
      variants: variants([['720p', 10, 100]])
    },
    {
      code: 'seedance-mini', name: 'Seedance Mini', description: '轻量快速，适合批量商品短视频', provider: 'seedance',
      model: 'doubao-seedance-2.0-mini', enabled: true, default_resolution: '720p', default_duration_seconds: 10,
      duration_selectable: true, audio_toggle: true, max_reference_images: 3, aspect_ratios: ['9:16', '16:9', '1:1', '3:4', '4:3', 'adaptive'], resolutions: ['480p', '720p'], durations: [5, 8, 10, 15],
      variants: variants([['480p', 5, 150], ['480p', 8, 230], ['480p', 10, 290], ['480p', 15, 430], ['720p', 5, 310], ['720p', 8, 490], ['720p', 10, 610], ['720p', 15, 920]])
    },
    {
      code: 'seedance-fast', name: 'Seedance 2.0 Fast', description: '速度与质量均衡，适合常规投放', provider: 'seedance',
      model: 'doubao-seedance-2.0-fast', enabled: true, default_resolution: '720p', default_duration_seconds: 10,
      duration_selectable: true, audio_toggle: true, max_reference_images: 3, aspect_ratios: ['9:16', '16:9', '1:1', '3:4', '4:3', 'adaptive'], resolutions: ['480p', '720p'], durations: [5, 8, 10, 15],
      variants: variants([['480p', 5, 290], ['480p', 8, 460], ['480p', 10, 570], ['480p', 15, 850], ['720p', 5, 610], ['720p', 8, 980], ['720p', 10, 1220], ['720p', 15, 1830]])
    },
    {
      code: 'seedance-2', name: 'Seedance 2.0', description: '完整质量档，支持最高 4K', provider: 'seedance',
      model: 'doubao-seedance-2.0', enabled: true, default_resolution: '720p', default_duration_seconds: 10,
      duration_selectable: true, audio_toggle: true, max_reference_images: 3, aspect_ratios: ['9:16', '16:9', '1:1', '3:4', '4:3', 'adaptive'], resolutions: ['480p', '720p', '1080p', '4k'], durations: [5, 8, 10, 15],
      variants: variants([['480p', 5, 360], ['480p', 8, 570], ['480p', 10, 710], ['480p', 15, 1060], ['720p', 5, 760], ['720p', 8, 1220], ['720p', 10, 1520], ['720p', 15, 2280], ['1080p', 5, 1890], ['1080p', 8, 3030], ['1080p', 10, 3780], ['1080p', 15, 5670], ['4k', 5, 3850], ['4k', 8, 6160], ['4k', 10, 7700], ['4k', 15, 11550]])
    }
  ]
}

function emptyConfig(): VideoAdminConfig {
  return {
    enabled: false,
    paid_provider_requested: false,
    paid_provider_env_enabled: false,
    paid_provider_effective: false,
    gemini_enabled: false,
    gemini_effective: false,
    seedance_enabled: false,
    seedance_effective: false,
    hfsy_enabled: false,
    hfsy_effective: false,
    agnes_route_enabled: false,
    agnes_env_enabled: false,
    agnes_effective: false,
    apimart_key_set: false,
    apimart_stored_key_set: false,
    apimart_key_source: 'none',
    apimart_key_error: false,
    hfsy_key_set: false,
    hfsy_stored_key_set: false,
    hfsy_key_source: 'none',
    hfsy_key_error: false,
    hfsy_mappings: [],
    agnes_key_set: false,
    agnes_stored_key_set: false,
    agnes_key_source: 'none',
    agnes_key_error: false,
    agnes_keys: [],
    agnes_model: 'agnes-video-v2.0',
    standard_enabled: true,
    standard_duration_seconds: 10,
    standard_resolution: '720p',
    standard_price: 900,
    quality_enabled: true,
    quality_duration_seconds: 10,
    quality_resolution: '720p',
    quality_price: 1200,
    agnes_route_percent: 0,
    agnes_daily_limit: 40,
    agnes_concurrency: 1,
    agnes_circuit_failures: 3,
    poll_seconds: 5,
    timeout_minutes: 30,
    mock_mode: false,
    mock_env_enabled: false,
    models: {},
    model_configs: defaultModelConfigs(),
    metrics: { summary: {}, models: [] }
  }
}

export function VideoProviderConfigPanel() {
  const [config, setConfig] = useState<VideoAdminConfig>(emptyConfig())
  const [message, setMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const [apimartApiKey, setApimartApiKey] = useState('')
  const [clearApimartApiKey, setClearApimartApiKey] = useState(false)
  const [hfsyApiKey, setHfsyApiKey] = useState('')
  const [clearHfsyApiKey, setClearHfsyApiKey] = useState(false)
  const [agnesKeysDirty, setAgnesKeysDirty] = useState(false)

  useEffect(() => {
    getVideoAdminConfig()
      .then(value => setConfig({ ...value, agnes_route_percent: 0, model_configs: value.model_configs?.length ? value.model_configs : defaultModelConfigs() }))
      .catch(error => setMessage(error instanceof Error ? error.message : '视频配置加载失败'))
  }, [])

  function patch(value: Partial<VideoAdminConfig>) {
    setConfig(current => ({ ...current, ...value }))
  }

  function patchModel(code: string, value: Partial<VideoAdminConfig['model_configs'][number]>) {
    setConfig(current => ({
      ...current,
      model_configs: current.model_configs.map(model => model.code === code ? { ...model, ...value } : model)
    }))
  }

  function patchVariant(code: string, resolution: string, duration: number, value: { price?: number; enabled?: boolean }) {
    setConfig(current => ({
      ...current,
      model_configs: current.model_configs.map(model => model.code !== code ? model : {
        ...model,
        variants: model.variants.map(variant =>
          variant.resolution === resolution && variant.duration_seconds === duration
            ? { ...variant, ...value }
            : variant
        )
      })
    }))
  }

  async function save() {
    setSaving(true)
    setMessage('')
    try {
      const saved = await updateVideoAdminConfig(config, {
        apimartApiKey: apimartApiKey.trim() || undefined,
        clearApimartApiKey,
        hfsyApiKey: hfsyApiKey.trim() || undefined,
        clearHfsyApiKey,
        saveAgnesKeys: agnesKeysDirty
      })
      setConfig(current => ({ ...saved, recent_tasks: current.recent_tasks, metrics: current.metrics }))
      setAgnesKeysDirty(false)
      setApimartApiKey('')
      setClearApimartApiKey(false)
      setHfsyApiKey('')
      setClearHfsyApiKey(false)
      setMessage('商品视频配置已保存。')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '商品视频配置保存失败')
    } finally {
      setSaving(false)
    }
  }

  function patchAgnesKey(id: string, value: Partial<VideoAdminConfig['agnes_keys'][number]>) {
    setConfig(current => ({
      ...current,
      agnes_keys: (current.agnes_keys || []).map(key => key.id === id ? { ...key, ...value } : key)
    }))
    setAgnesKeysDirty(true)
  }

  function addAgnesKey() {
    const suffix = typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID().replaceAll('-', '')
      : `${Date.now()}${Math.random().toString(16).slice(2)}`
    const index = (config.agnes_keys || []).filter(key => key.source !== 'environment').length + 1
    setConfig(current => ({
      ...current,
      agnes_keys: [...(current.agnes_keys || []), {
        id: `agnes-${suffix}`,
        label: `Agnes Key ${index}`,
        enabled: false,
        concurrency: 1,
        configured: false,
        fingerprint: '',
        source: 'admin',
        error: false,
        active_count: 0,
        today_count: 0,
        success_count: 0,
        failure_count: 0,
        api_key: ''
      }]
    }))
    setAgnesKeysDirty(true)
  }

  function removeAgnesKey(id: string) {
    const key = (config.agnes_keys || []).find(item => item.id === id)
    if (key?.active_count) {
      setMessage(`${key.label} 仍有 ${key.active_count} 个生成中任务，暂时不能删除。`)
      return
    }
    setConfig(current => ({ ...current, agnes_keys: (current.agnes_keys || []).filter(item => item.id !== id) }))
    setAgnesKeysDirty(true)
  }

  const recent = config.recent_tasks || []
  const metrics = config.metrics || { summary: {}, models: [] }
  const summary = metrics.summary || {}
  const realWillApply = !config.mock_mode && (config.gemini_effective || config.seedance_effective || config.agnes_effective || config.hfsy_effective)
  const minimumPrice = realWillApply ? 1 : 0
  const apimartKeySource = config.apimart_key_source === 'admin'
    ? '后台加密配置'
    : config.apimart_key_source === 'environment'
      ? '服务器环境变量'
      : config.apimart_key_source === 'invalid'
        ? '后台配置无法解密'
        : '未配置'
  const hfsyKeySource = config.hfsy_key_source === 'admin'
    ? '后台加密配置'
    : config.hfsy_key_source === 'environment'
      ? '服务器环境变量'
      : config.hfsy_key_source === 'invalid'
        ? '后台配置无法解密'
        : '未配置'
  return (
    <section className="admin-card operation-card ai-provider-section video-provider-config">
      <div className="section-head ai-provider-head">
        <div>
          <h2><Film size={19} />视频供应商</h2>
          <p>独立控制 APIMart、HFSY 和 Agnes；用户端价格来自下方规格矩阵。</p>
          <div className={config.mock_mode || !realWillApply ? 'status-warn ai-provider-state' : 'status-good ai-provider-state'}>
            {config.mock_mode ? '当前为 Mock 模式，不会创建真实视频' : realWillApply ? '真实视频模型按开关路由' : '当前没有真正可用的视频模型'}
          </div>
        </div>
        <button className="btn btn-primary" type="button" onClick={save} disabled={saving}><Save size={16} />{saving ? '保存中...' : '保存视频配置'}</button>
      </div>

      {message ? <div className="ai-provider-service-note">{message}</div> : null}
      <div className="video-config-safety">
        <ShieldCheck size={18} />
        <div><strong>供应商状态</strong><span>Mock：{config.mock_env_enabled ? '已开启' : '已关闭'} · APIMart：{config.paid_provider_env_enabled ? '已允许' : '已关闭'} · HFSY：{config.hfsy_effective ? '已生效' : '未生效'} · Agnes：{config.agnes_env_enabled ? '已允许' : '已关闭'} · APIMart Key：{config.apimart_key_set ? '已配置' : '未配置'} · HFSY Key：{config.hfsy_key_set ? '已配置' : '未配置'} · Agnes Key：{config.agnes_key_set ? '已配置' : '未配置'}</span></div>
      </div>

      <div className="admin-form-grid ai-config-grid video-config-grid video-global-config">
        <label className="field video-config-toggle">
          <span>用户端商品视频</span>
          <input type="checkbox" checked={config.enabled} onChange={event => patch({ enabled: event.target.checked })} />
          <small>关闭后不能创建新任务，已有任务仍可查询。</small>
        </label>
        <label className="field video-config-toggle">
          <span>启用 APIMart</span>
          <input type="checkbox" checked={config.paid_provider_requested} onChange={event => patch({ paid_provider_requested: event.target.checked })} />
          <small>仍受 VIDEO_PAID_PROVIDER_ENABLED 和 APIMart Key 限制。</small>
        </label>
        <label className="field video-config-toggle">
          <span>开放 Gemini Omni</span>
          <input type="checkbox" checked={config.gemini_enabled} onChange={event => patch({ gemini_enabled: event.target.checked })} />
          <small>关闭后，用户端 Gemini 模型不可选。</small>
        </label>
        <label className="field video-config-toggle">
          <span>开放 Seedance 系列</span>
          <input type="checkbox" checked={config.seedance_enabled} onChange={event => patch({ seedance_enabled: event.target.checked })} />
          <small>统一控制 Mini、Fast 和 Seedance 2.0。</small>
        </label>
        <label className="field video-config-toggle">
          <span>启用 HFSY 固定路由</span>
          <input type="checkbox" checked={config.hfsy_enabled} onChange={event => patch({ hfsy_enabled: event.target.checked })} />
          <small>仅接管下方明确映射的 Mini / Fast / 2.0 规格；任务创建后不跨供应商。</small>
        </label>
        <label className="field">
          <span>APIMart API Key</span>
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            value={apimartApiKey}
            placeholder={config.apimart_key_set ? '已配置，留空不修改' : '填写 APIMart API Key'}
            onChange={event => {
              setApimartApiKey(event.target.value)
              if (event.target.value) setClearApimartApiKey(false)
            }}
          />
          <small>Gemini Omni 与 Seedance 2.0 Fast 共用 · 当前来源：{apimartKeySource}</small>
        </label>
        {config.apimart_stored_key_set ? <label className="field video-config-toggle">
          <span>清除后台保存的 APIMart Key</span>
          <input type="checkbox" checked={clearApimartApiKey} onChange={event => {
            setClearApimartApiKey(event.target.checked)
            if (event.target.checked) setApimartApiKey('')
          }} />
          <small>清除后仍可使用服务器环境变量中的 Key。</small>
        </label> : null}
        <label className="field">
          <span>HFSY API Key</span>
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            value={hfsyApiKey}
            placeholder={config.hfsy_key_set ? '已配置，留空不修改' : '填写 HFSY API Key'}
            onChange={event => {
              setHfsyApiKey(event.target.value)
              if (event.target.value) setClearHfsyApiKey(false)
            }}
          />
          <small>当前来源：{hfsyKeySource}。后台加密保存，不回显明文。</small>
        </label>
        {config.hfsy_stored_key_set ? <label className="field video-config-toggle">
          <span>清除后台保存的 HFSY Key</span>
          <input type="checkbox" checked={clearHfsyApiKey} onChange={event => {
            setClearHfsyApiKey(event.target.checked)
            if (event.target.checked) setHfsyApiKey('')
          }} />
          <small>仍有 HFSY 任务时，服务端会拒绝清除。</small>
        </label> : null}
      </div>

      <div className="video-hfsy-routing">
        <div className="video-hfsy-routing-head">
          <ShieldCheck size={18} />
          <div>
            <strong>HFSY 固定映射</strong>
            <span>映射、成本保护与实际任务路由均来自后端同一份配置。其他规格在任务创建前固定给 APIMart，失败后不会跨供应商重生成。</span>
          </div>
        </div>
        <div className="table-wrap">
          <table className="data-table video-hfsy-routing-table">
            <thead><tr><th>用户模型</th><th>分辨率</th><th>HFSY 模型</th><th>固定成本</th><th>最低安全售价</th><th>支持比例</th></tr></thead>
            <tbody>
              {(config.hfsy_mappings || []).map(mapping => <tr key={`${mapping.model_code}-${mapping.resolution}`}>
                <td><strong>{mapping.model_name}</strong></td>
                <td>{mapping.resolution}</td>
                <td><code>{mapping.hfsy_model}</code></td>
                <td>¥{Number(mapping.cost_cny || 0).toFixed(2)} / 次</td>
                <td>{mapping.minimum_credits} 积分</td>
                <td>{mapping.aspect_ratios.join('、')}</td>
              </tr>)}
            </tbody>
          </table>
        </div>
      </div>

      <div className="video-model-pricing">
        <div className="video-model-pricing-head">
          <div><strong>用户端模型与售价</strong><small>模型参数白名单由服务端固定；这里只开放规格并设置积分售价。</small></div>
          <span>100 积分 = ¥0.99</span>
        </div>
        {(config.model_configs || []).map(model => (
          <fieldset className="video-model-price-card" key={model.code}>
            <legend>{model.name}</legend>
            <div className="video-model-price-head">
              <div><strong>{model.model}</strong><small>{model.description} · {model.provider === 'agnes' ? 'Agnes AI' : model.provider === 'gemini' ? 'APIMart / Gemini' : 'HFSY / APIMart 固定选路'} · 最多 {model.max_reference_images} 张参考图</small></div>
              <label className="video-model-enable"><input type="checkbox" checked={model.enabled} onChange={event => patchModel(model.code, { enabled: event.target.checked })} /><span>用户可选</span></label>
            </div>
            <div className="table-wrap video-price-matrix-wrap">
              <table className="data-table video-price-matrix">
                <thead><tr><th>分辨率</th>{model.durations.map(duration => <th key={duration}>{model.duration_selectable ? `${duration} 秒` : model.provider === 'agnes' ? `约 ${duration} 秒` : '模型自动时长'}</th>)}</tr></thead>
                <tbody>
                  {model.resolutions.map(resolution => <tr key={resolution}>
                    <td><strong>{resolution.toLowerCase() === '4k' ? '4K' : resolution}</strong></td>
                    {model.durations.map(duration => {
                      const variant = model.variants.find(item => item.resolution === resolution && item.duration_seconds === duration)
                      return <td key={duration}>{variant ? <div className="video-price-cell">
                        <input type="number" min={minimumPrice} max="100000" value={variant.price} onChange={event => patchVariant(model.code, resolution, duration, { price: Number(event.target.value || 0) })} aria-label={`${model.name} ${resolution} ${duration}秒售价`} />
                        <label title="是否开放此规格"><input type="checkbox" checked={variant.enabled} onChange={event => patchVariant(model.code, resolution, duration, { enabled: event.target.checked })} /><span>启用</span></label>
                      </div> : '-'}</td>
                    })}
                  </tr>)}
                </tbody>
              </table>
            </div>
          </fieldset>
        ))}
      </div>

      <fieldset className="video-route-config">
        <legend><KeyRound size={16} />Agnes 供应商与全局队列</legend>
        <div className="agnes-key-pool">
          <div className="agnes-key-pool-head">
            <div><strong><KeyRound size={16} />Agnes Key 池</strong><small>全站总并发由下方控制；每把 Key 还有独立并发上限。密钥保存后只显示指纹。</small></div>
            <button className="btn btn-secondary" type="button" onClick={addAgnesKey}><Plus size={16} />添加 Key</button>
          </div>
          {(config.agnes_keys || []).length ? <div className="agnes-key-list">
            {(config.agnes_keys || []).map(key => {
              const environment = key.source === 'environment'
              return <div className="agnes-key-row" key={key.id}>
                <div className="agnes-key-row-head">
                  <div>
                    <strong>{key.label}</strong>
                    <span>{environment ? '环境变量' : key.configured ? `指纹 ${key.fingerprint}` : '尚未填写密钥'} · 编号 {key.id}</span>
                  </div>
                  <div className="agnes-key-row-actions">
                    <label title={key.active_count ? '仍有任务生成中，完成后可停用' : '启用此 Key'}>
                      <input type="checkbox" checked={key.enabled} disabled={environment || key.active_count > 0} onChange={event => patchAgnesKey(key.id, { enabled: event.target.checked })} />
                      <span>启用</span>
                    </label>
                    {!environment ? <button className="icon-button" type="button" aria-label={`删除 ${key.label}`} title="删除 Key" onClick={() => removeAgnesKey(key.id)}><Trash2 size={15} /></button> : null}
                  </div>
                </div>
                <div className="agnes-key-fields">
                  <label className="field"><span>名称</span><input className="input" type="text" maxLength={40} disabled={environment} value={key.label} onChange={event => patchAgnesKey(key.id, { label: event.target.value })} /></label>
                  <label className="field"><span>API Key</span><input className="input" type="password" autoComplete="new-password" spellCheck={false} disabled={environment} value={key.api_key || ''} placeholder={key.configured ? '已配置，留空不修改' : '填写新的 Agnes API Key'} onChange={event => patchAgnesKey(key.id, { api_key: event.target.value, clear_api_key: false })} /></label>
                  <label className="field"><span>单 Key 并发</span><input className="input" type="number" min="1" max="100" disabled={environment} value={key.concurrency} onChange={event => patchAgnesKey(key.id, { concurrency: Number(event.target.value || 1) })} /></label>
                </div>
                <div className="agnes-key-stats">
                  <span>生成中 <strong>{key.active_count || 0}</strong></span>
                  <span>今日任务 <strong>{key.today_count || 0}</strong></span>
                  <span>今日成功 <strong>{key.success_count || 0}</strong></span>
                  <span>今日失败 <strong>{key.failure_count || 0}</strong></span>
                </div>
              </div>
            })}
          </div> : <div className="agnes-key-empty">尚未配置 Agnes Key。添加后填写名称、密钥和并发，再保存视频配置。</div>}
        </div>
        <div className="admin-form-grid ai-config-grid">
          <label className="field">
            <span>Agnes 模型 ID</span>
            <input className="input" type="text" maxLength={128} spellCheck={false} value={config.agnes_model} onChange={event => patch({ agnes_model: event.target.value })} />
            <small>默认 agnes-video-v2.0</small>
          </label>
          <label className="field video-config-toggle"><span>开放 Agnes V2.0</span><input type="checkbox" checked={config.agnes_route_enabled} onChange={event => patch({ agnes_route_enabled: event.target.checked, agnes_route_percent: 0 })} /><small>用户主动选择后进入 Agnes 全局队列，不会随机接管 Gemini 任务。</small></label>
          <label className="field"><span>每日任务上限</span><input className="input" type="number" min="0" max="10000" value={config.agnes_daily_limit} onChange={event => patch({ agnes_daily_limit: Number(event.target.value || 0) })} /><small>10 秒任务默认 40 个，相当于最多约 400 秒/日。</small></label>
          <label className="field"><span>全站总并发</span><input className="input" type="number" min="1" max="100" value={config.agnes_concurrency} onChange={event => patch({ agnes_concurrency: Number(event.target.value || 1) })} /><small>所有 Agnes Key 合计不能超过此数量。</small></label>
          <label className="field"><span>熔断失败次数</span><input className="input" type="number" min="1" max="100" value={config.agnes_circuit_failures} onChange={event => patch({ agnes_circuit_failures: Number(event.target.value || 3) })} /></label>
          <label className="field"><span>轮询间隔（秒）</span><input className="input" type="number" min="2" max="60" value={config.poll_seconds} onChange={event => patch({ poll_seconds: Number(event.target.value || 5) })} /></label>
          <label className="field"><span>任务超时（分钟）</span><input className="input" type="number" min="5" max="180" value={config.timeout_minutes} onChange={event => patch({ timeout_minutes: Number(event.target.value || 30) })} /></label>
        </div>
      </fieldset>

      <div className="video-metrics-section">
        <div className="video-metrics-head"><h3><Activity size={17} />近 30 天模型统计</h3><span>今日上游成本 {Number(summary.daily_total_cost || 0).toFixed(4)}</span></div>
        <div className="video-metrics-summary">
          <span><small>Agnes 排队</small><strong>{summary.agnes_queued || 0}</strong></span>
          <span><small>Agnes 活动</small><strong>{summary.agnes_active || 0}</strong></span>
          <span><small>历史遗留回退</small><strong>{summary.agnes_fallback_gemini || 0}</strong></span>
          <span><small>平均排队</small><strong>{Number(summary.average_queue_seconds || 0).toFixed(1)}s</strong></span>
          <span><small>平均生成</small><strong>{Number(summary.average_generation_seconds || 0).toFixed(1)}s</strong></span>
          <span><small>积分收入</small><strong>{summary.credits_income || 0}</strong></span>
          <span><small>上游成本</small><strong>{Number(summary.upstream_total_cost || 0).toFixed(4)}</strong></span>
          <span><small>收入成本差额</small><strong>{Number(summary.income_cost_difference || 0).toFixed(4)}</strong></span>
        </div>
        {metrics.models?.length ? <div className="table-wrap"><table className="data-table"><thead><tr><th>模型</th><th>调用</th><th>成功 / 失败</th><th>成功率</th><th>平均排队</th><th>平均生成</th><th>平均 / 总成本</th></tr></thead><tbody>
          {metrics.models.map(row => {
            const calls = Number(row.call_count || 0)
            const successes = Number(row.success_count || 0)
            const rate = calls ? `${((successes / calls) * 100).toFixed(1)}%` : '-'
            return <tr key={`${row.provider}-${row.model}`}><td><strong>{row.provider}</strong><div className="table-subtext">{row.model}</div></td><td>{calls}</td><td>{successes} / {Number(row.failure_count || 0)}</td><td>{rate}</td><td>{Number(row.average_queue_seconds || 0).toFixed(1)}s</td><td>{Number(row.average_generation_seconds || 0).toFixed(1)}s</td><td>{Number(row.average_cost || 0).toFixed(4)} / {Number(row.total_cost || 0).toFixed(4)}</td></tr>
          })}
        </tbody></table></div> : <div className="video-metrics-empty">暂无已提交的视频模型调用。</div>}
      </div>

      {recent.length ? <div className="table-wrap mt-16"><table className="data-table"><thead><tr><th>最近视频任务</th><th>用户选择</th><th>状态</th><th>供应商</th><th>规格</th><th>积分</th><th>时间</th></tr></thead><tbody>
        {recent.map(task => <tr key={task.id}>
          <td><strong>{task.title}</strong><div className="table-subtext">{task.user_account || task.user_id}</div>{task.hidden_by_user ? <div className="status-warn table-subtext">用户已隐藏</div> : null}{task.safe_error ? <div className="table-subtext status-bad">{task.safe_error}</div> : null}</td>
          <td>{config.model_configs.find(model => model.code === task.mode)?.name || (task.mode === 'quality' ? '旧版品质模式' : task.mode === 'standard' ? '旧版标准模式' : task.mode)}</td>
          <td>{task.status} · {task.progress}%{task.refunded_at ? <div><span className="status status-ok">已退款</span></div> : null}</td>
          <td>{task.provider || '-'}<div className="table-subtext">{task.model || '-'}</div>{task.agnes_key_id ? <div className="table-subtext">Key：{task.agnes_key_id}</div> : null}{task.routing_reason ? <div className="table-subtext">{task.routing_reason}</div> : null}</td>
          <td>{task.aspect_ratio} · {task.duration_seconds}s · {task.requested_resolution}{task.actual_width && task.actual_height ? <div className="table-subtext">实际 {task.actual_width}×{task.actual_height}{task.actual_seconds ? ` · ${task.actual_seconds}s` : ''}</div> : null}</td>
          <td>{task.credits_cost}</td>
          <td>{new Date(task.updated_at).toLocaleString()}</td>
        </tr>)}
      </tbody></table></div> : null}
    </section>
  )
}
