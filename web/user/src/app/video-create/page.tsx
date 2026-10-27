'use client'

import { ChangeEvent, useEffect, useRef, useState } from 'react'
import { Check, Clock3, Film, ImagePlus, LoaderCircle, Play, Settings2, Sparkles, Trash2, Volume2, WalletCards, WandSparkles } from 'lucide-react'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'
import { useI18n } from '@/components/i18n'
import { assetUrl, createVideoTask, deleteVideoTask, getOptionalBillingSummary, getVideoConfig, listVideoTasks, previewVideoScript, requireLogin, VideoModelConfig, VideoPublicConfig, VideoTaskView } from '@/lib/api'
import { IMAGE_FILE_ACCEPT, imageUploadErrorMessage, imageUploadMessage, validateImageFile } from '@/lib/imageUpload'
import { videoModelLabel, videoSettlementLabel } from '@/lib/videoDisplay'

const ACTIVE_STATUSES = new Set(['queued', 'submitting', 'submission_unknown', 'submitted', 'generating', 'upscaling', 'quality_check'])

function statusText(status: string) {
  if (status === 'queued') return '排队中'
  if (status === 'submitting') return '正在提交上游'
  if (status === 'submission_unknown') return '提交状态待核验'
  if (status === 'submitted') return '生成中'
  if (status === 'generating') return '生成中'
  if (status === 'upscaling') return '清晰度处理中'
  if (status === 'quality_check') return '质量检查中'
  if (status === 'completed') return '已完成'
  if (status === 'failed') return '生成失败'
  if (status === 'timeout') return '生成超时'
  if (status === 'cancelled') return '已取消'
  return status
}

function statusClass(status: string) {
  if (status === 'completed') return 'status status-ok'
  if (status === 'failed' || status === 'timeout' || status === 'cancelled') return 'status status-bad'
  return 'status status-warn'
}

function queueNotice(task: VideoTaskView) {
  if (task.status !== 'queued' || task.provider !== 'agnes') return ''
  return `当前任务正在排队，前方还有 ${Math.max(0, task.queue_ahead || 0)} 个任务。`
}

function formatDate(value: string) {
  return value ? new Date(value).toLocaleString() : '-'
}

function isMock(task: VideoTaskView) {
  return task.provider === 'mock' || Boolean(task.metadata?.mock_mode)
}

function resolutionLabel(value: string) {
  return value.toLowerCase() === '4k' ? '4K' : value
}

function durationLabel(model: VideoModelConfig | undefined, duration: number) {
  if (!model) return '-'
  if (model.duration_selectable) return `${duration} 秒`
  return model.provider === 'agnes' ? `约 ${duration} 秒` : '约 3–10 秒'
}

export default function VideoCreatePage() {
  const { billingEnabled } = useRuntime()
  const t = useI18n()
  const submitKeyRef = useRef('')
  const submitFingerprintRef = useRef('')
  const [config, setConfig] = useState<VideoPublicConfig | null>(null)
  const [tasks, setTasks] = useState<VideoTaskView[]>([])
  const [title, setTitle] = useState('')
  const [prompt, setPrompt] = useState('')
  const [aspectRatio, setAspectRatio] = useState('9:16')
  const [modelCode, setModelCode] = useState('')
  const [resolution, setResolution] = useState('720p')
  const [duration, setDuration] = useState(10)
  const [generateAudio, setGenerateAudio] = useState(true)
  const [seedText, setSeedText] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [files, setFiles] = useState<File[]>([])
  const [imageUrl, setImageUrl] = useState('')
  const [previews, setPreviews] = useState<string[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [preparedPrompt, setPreparedPrompt] = useState('')
  const [scriptMode, setScriptMode] = useState<'single_shot' | 'storyboard'>('single_shot')
  const [scriptSource, setScriptSource] = useState<'ai' | 'fallback'>('fallback')
  const [scriptSummary, setScriptSummary] = useState('')
  const [processingImages, setProcessingImages] = useState(false)
  const [message, setMessage] = useState('')
  const [selectedTaskId, setSelectedTaskId] = useState('')
  const [credits, setCredits] = useState<number | null>(null)

  const activeTask = tasks.find(task => task.id === selectedTaskId) || tasks.find(task => ACTIVE_STATUSES.has(task.status)) || tasks[0]
  const hasActiveTask = tasks.some(task => ACTIVE_STATUSES.has(task.status))
  const models = config?.model_configs || []
  const selectedModel = models.find(model => model.code === modelCode)
  const selectedVariant = selectedModel?.variants.find(variant =>
    variant.resolution === resolution && variant.duration_seconds === duration
  )
  const maxReferenceImages = selectedModel?.max_reference_images || 3

  function applyModel(model: VideoModelConfig) {
    const variant = model.variants.find(item =>
      item.enabled
      && item.resolution === model.default_resolution
      && item.duration_seconds === model.default_duration_seconds
    ) || model.variants.find(item => item.enabled)
    setModelCode(model.code)
    setResolution(variant?.resolution || model.default_resolution)
    setDuration(variant?.duration_seconds || model.default_duration_seconds)
    setAspectRatio(model.aspect_ratios.includes(aspectRatio) ? aspectRatio : model.aspect_ratios[0])
    setGenerateAudio(model.audio_toggle)
    setPreparedPrompt('')
    setSeedText('')
    if (model.max_reference_images === 1 && files.length + (imageUrl.trim() ? 1 : 0) > 1) {
      if (files.length) {
        setFiles(current => current.slice(0, 1))
        setImageUrl('')
      }
      setMessage('Agnes V2.0 仅支持 1 张参考图，已保留第一张图片。')
    }
  }

  useEffect(() => {
    const next = files.map(file => URL.createObjectURL(file))
    setPreviews(next)
    return () => next.forEach(url => URL.revokeObjectURL(url))
  }, [files])

  useEffect(() => {
    const selected = new URLSearchParams(window.location.search).get('task') || ''
    setSelectedTaskId(selected)
    if (!requireLogin(`/video-create${window.location.search}`)) return
    Promise.all([getVideoConfig(), listVideoTasks(), getOptionalBillingSummary()])
      .then(([nextConfig, nextTasks, billing]) => {
        setConfig(nextConfig)
        setTasks(nextTasks)
        setCredits(billing?.credits ?? null)
        const defaultModel = nextConfig.model_configs.find(model =>
          model.code === nextConfig.default_model && model.enabled
        ) || nextConfig.model_configs.find(model => model.enabled)
        if (defaultModel) applyModel(defaultModel)
      })
      .catch(error => setMessage(error instanceof Error ? error.message : '商品视频加载失败'))
  }, [])

  useEffect(() => {
    if (!hasActiveTask) return
    const timer = window.setInterval(() => {
      listVideoTasks().then(setTasks).catch(() => undefined)
    }, 3000)
    return () => window.clearInterval(timer)
  }, [hasActiveTask])

  const selectedCount = files.length + (imageUrl.trim() ? 1 : 0)
  const canSubmit = Boolean(
    config?.enabled
    && selectedModel?.enabled
    && selectedVariant?.enabled
    && prompt.trim().length >= 4
    && selectedCount > 0
    && selectedCount <= maxReferenceImages
    && !submitting
    && !processingImages
  )
  const price = selectedVariant?.price || 0
  const chargedPrice = config?.mock_mode ? 0 : price
  const hasEnoughCredits = !billingEnabled || (credits !== null && credits >= chargedPrice)
  const balanceAfter = credits === null ? null : credits - chargedPrice

  function ensureSubmitKey() {
    const fingerprint = JSON.stringify({
      title,
      prompt,
      aspectRatio,
      modelCode,
      duration,
      resolution,
      generateAudio,
      seedText,
      imageUrl,
      files: files.map(file => [file.name, file.size, file.lastModified])
    })
    if (!submitKeyRef.current || submitFingerprintRef.current !== fingerprint) {
      submitKeyRef.current = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
        ? crypto.randomUUID()
        : `video-${Date.now()}-${Math.random().toString(36).slice(2)}`
      submitFingerprintRef.current = fingerprint
    }
    return submitKeyRef.current
  }

  async function selectFiles(event: ChangeEvent<HTMLInputElement>) {
    const incoming = Array.from(event.target.files || [])
    event.target.value = ''
    if (!incoming.length || processingImages) return
    setProcessingImages(true)
    setMessage(imageUploadMessage('PROCESSING'))
    try {
      const selected: File[] = []
      for (const file of incoming) {
        await validateImageFile(file)
        selected.push(file)
      }
      if (maxReferenceImages === 1) {
        setFiles(selected.slice(0, 1))
        setImageUrl('')
        setMessage(selected.length > 1 ? 'Agnes V2.0 仅支持 1 张商品参考图。' : '')
      } else {
        const available = Math.max(0, maxReferenceImages - (imageUrl.trim() ? 1 : 0))
        setFiles(current => [...current, ...selected].slice(0, available))
        setMessage(selected.length > available ? `商品参考图最多选择 ${maxReferenceImages} 张。` : '')
      }
    } catch (error) {
      setMessage(imageUploadErrorMessage(error))
    } finally {
      setProcessingImages(false)
    }
  }

  async function openConfirmation() {
    if (!canSubmit || confirmLoading) return
    setConfirmLoading(true)
    setMessage('')
    try {
      const [billing, script] = await Promise.all([
        getOptionalBillingSummary(),
        previewVideoScript({
          prompt,
          modelCode,
          durationSeconds: duration,
          resolution,
          aspectRatio,
          generateAudio,
          referenceCount: selectedCount
        })
      ])
      setCredits(billing?.credits ?? null)
      setPreparedPrompt(script.optimized_prompt)
      setScriptMode(script.mode)
      setScriptSource(script.source)
      setScriptSummary(script.summary)
      ensureSubmitKey()
      setConfirmOpen(true)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '视频脚本准备失败，请稍后重试')
    } finally {
      setConfirmLoading(false)
    }
  }

  async function submit() {
    if (!canSubmit) return
    const finalPrompt = preparedPrompt.trim()
    if (finalPrompt.length < 4) {
      setMessage('智能脚本至少需要 4 个字符。')
      return
    }
    if (billingEnabled && (credits === null || credits < chargedPrice)) {
      setMessage('积分不足，无法创建本次视频任务。')
      return
    }
    setConfirmOpen(false)
    setSubmitting(true)
    setMessage('')
    try {
      const created = await createVideoTask({
        idempotencyKey: ensureSubmitKey(),
        title,
        prompt: finalPrompt,
        originalPrompt: prompt,
        aspectRatio,
        modelCode,
        durationSeconds: duration,
        resolution,
        generateAudio,
        seed: seedText.trim() ? Number(seedText) : undefined,
        images: files,
        imageUrls: imageUrl.trim() ? [imageUrl.trim()] : [],
        maxReferenceImages
      })
      setTasks(current => [created, ...current.filter(task => task.id !== created.id)])
      setSelectedTaskId(created.id)
      submitKeyRef.current = ''
      submitFingerprintRef.current = ''
      window.history.replaceState(null, '', `/video-create?task=${encodeURIComponent(created.id)}`)
      setMessage(config?.mock_mode ? 'Mock 异步任务已提交，测试服不会调用付费上游。' : '视频任务已提交。')
      window.dispatchEvent(new Event('openimage:credits-updated'))
      getOptionalBillingSummary().then(billing => setCredits(billing?.credits ?? null)).catch(() => undefined)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '视频任务提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  async function removeTask(task: VideoTaskView) {
    if (ACTIVE_STATUSES.has(task.status)) return
    if (!window.confirm(`确定删除“${task.title}”吗？删除后将不再显示在历史记录中。`)) return
    try {
      await deleteVideoTask(task.id)
      setTasks(current => current.filter(item => item.id !== task.id))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '删除失败')
    }
  }

  return (
    <Shell>
      <UserAuthGate title="商品视频" description="上传商品参考图，生成适合电商投放的视频。">
        <div className="page-head video-page-head">
          <div>
            <h1>商品视频</h1>
            <p>商品图转视频；后台自动处理异步任务、质量检查和供应商路由。</p>
          </div>
          {config?.mock_mode ? <span className="video-env-badge"><Clock3 size={15} />测试服 Mock</span> : null}
        </div>

        {message ? <div className="notice video-notice">{message}</div> : null}
        {!config?.enabled && config ? <div className="notice notice-error">商品视频功能当前已由管理员关闭。</div> : null}

        <div className="video-workbench">
          <section className="panel video-form-panel">
            <div className="panel-head">
              <div><h2>商品资料</h2><p>当前模型最多使用 {maxReferenceImages} 张参考图。</p></div>
            </div>
            <div className="panel-body video-form-body">
              <label className="field">
                <span>任务名称</span>
                <input className="input" value={title} onChange={event => setTitle(event.target.value)} placeholder="例如：便携咖啡机 TikTok 商品视频" maxLength={120} />
              </label>

              <div className="field">
                <span>商品参考图 *</span>
                <div className="video-upload-row">
                  <label className="btn btn-secondary video-upload-button" htmlFor="video-images"><ImagePlus size={17} />{processingImages ? imageUploadMessage('PROCESSING') : '选择图片'}</label>
                  <input id="video-images" className="video-file-input" type="file" accept={IMAGE_FILE_ACCEPT} multiple={maxReferenceImages > 1} disabled={processingImages || submitting} onChange={event => void selectFiles(event)} />
                  <span className="muted">{selectedCount}/{maxReferenceImages} 张</span>
                </div>
                {previews.length ? (
                  <div className="video-reference-grid">
                    {previews.map((url, index) => (
                      <div className="video-reference-item" key={url}>
                        <img src={url} alt={`商品参考图 ${index + 1}`} />
                        <button type="button" aria-label="移除图片" onClick={() => setFiles(current => current.filter((_, fileIndex) => fileIndex !== index))}><Trash2 size={15} /></button>
                      </div>
                    ))}
                  </div>
                ) : null}
                <input className="input" value={imageUrl} onChange={event => {
                  const value = event.target.value
                  if (maxReferenceImages === 1 && value.trim()) setFiles([])
                  setImageUrl(value)
                }} placeholder="或填写商品图片 URL" />
              </div>

              <label className="field">
                <span>视频描述 *</span>
                <textarea className="textarea video-prompt" value={prompt} onChange={event => setPrompt(event.target.value)} maxLength={4000} placeholder="例如：镜头从商品包装缓慢推进，展示咖啡机装入胶囊并萃取咖啡，真实商业摄影，产品结构与参考图保持一致。" />
                <div className={`video-prompt-guidance ${prompt.trim().length > 0 && prompt.trim().length < 4 ? 'is-invalid' : ''}`}>
                  <small>至少 4 个字符<BillingOnly> · 当前规格 {price} 积分/次 · 生成失败自动退回</BillingOnly></small>
                  <small>{prompt.length}/4000</small>
                </div>
              </label>

              <div className="field">
                <span>视频模型</span>
                <div className="video-model-grid">
                  {models.map(model => (
                    <button type="button" className={`video-model-option ${modelCode === model.code ? 'active' : ''}`} disabled={!model.enabled} onClick={() => applyModel(model)} key={model.code}>
                      <span className="video-model-check">{modelCode === model.code ? <Check size={14} /> : null}</span>
                      <strong>{model.name}</strong>
                      <small>{model.description}</small>
                      {!model.enabled ? <em>暂不可用</em> : null}
                    </button>
                  ))}
                </div>
              </div>

              <div className="video-options-grid video-parameter-grid">
                <div className="field">
                  <span>分辨率</span>
                  <div className="video-segmented">
                    {(selectedModel?.resolutions || []).map(value => {
                      const available = selectedModel?.variants.some(variant => variant.enabled && variant.resolution === value && variant.duration_seconds === duration)
                      return <button type="button" className={resolution === value ? 'active' : ''} disabled={!available} onClick={() => setResolution(value)} key={value}>{resolutionLabel(value)}</button>
                    })}
                  </div>
                </div>
                <div className="field">
                  <span>时长</span>
                  {selectedModel?.duration_selectable ? <div className="video-segmented">
                    {(selectedModel?.durations || []).map(value => {
                      const available = selectedModel?.variants.some(variant => variant.enabled && variant.resolution === resolution && variant.duration_seconds === value)
                      return <button type="button" className={duration === value ? 'active' : ''} disabled={!available} onClick={() => setDuration(value)} key={value}>{value}s</button>
                    })}
                  </div> : <div className="video-fixed-value">{durationLabel(selectedModel, duration)}{selectedModel?.provider === 'gemini' ? '，由模型自动决定' : '，固定 24fps'}</div>}
                </div>
                <div className="field">
                  <span>画面比例</span>
                  <div className="video-segmented video-ratio-segmented">
                    {(selectedModel?.aspect_ratios || ['9:16', '16:9']).map(value => <button type="button" className={aspectRatio === value ? 'active' : ''} onClick={() => setAspectRatio(value)} key={value}>{value === 'adaptive' ? '自适应' : value}</button>)}
                  </div>
                </div>
                {selectedModel?.audio_toggle ? <label className="video-audio-toggle">
                  <span><Volume2 size={17} /><span><strong>生成音频</strong><small>同步生成音轨<BillingOnly>，当前不额外加价</BillingOnly></small></span></span>
                  <input type="checkbox" checked={generateAudio} onChange={event => setGenerateAudio(event.target.checked)} />
                </label> : null}
              </div>

              {selectedModel?.provider === 'seedance' ? <div className="video-advanced">
                <button type="button" onClick={() => setAdvancedOpen(value => !value)}><Settings2 size={16} />高级设置</button>
                {advancedOpen ? <label className="field"><span>随机种子（可选）</span><input className="input" type="number" min="0" max="2147483647" value={seedText} onChange={event => setSeedText(event.target.value)} placeholder="留空则随机生成" /></label> : null}
              </div> : null}
            </div>
          </section>

          <aside className="panel video-submit-panel">
            <div className="panel-head"><div><h2>交付规格</h2><BillingOnly><p>价格按所选参数实时计算</p></BillingOnly></div></div>
            <div className="panel-body video-submit-body">
              <div className="video-spec-row"><span>模型</span><strong>{selectedModel?.name || '请选择'}</strong></div>
              <div className="video-spec-row"><span>时长</span><strong>{durationLabel(selectedModel, duration)}</strong></div>
              <div className="video-spec-row"><span>清晰度</span><strong>{resolutionLabel(resolution)}</strong></div>
              <div className="video-spec-row"><span>比例</span><strong>{aspectRatio}</strong></div>
              {selectedModel?.audio_toggle ? <div className="video-spec-row"><span>音频</span><strong>{generateAudio ? '生成' : '关闭'}</strong></div> : null}
              <BillingOnly><div className="video-cost-block"><span>售价</span><strong>{price} 积分</strong><small>当前余额：{credits === null ? '读取中' : `${credits} 积分`}</small>{config?.mock_mode ? <small>测试服 Mock 展示售价但不扣积分</small> : null}</div></BillingOnly>
              <button className="btn btn-primary btn-block video-submit-button" type="button" disabled={!canSubmit || confirmLoading} onClick={openConfirmation}>
                {submitting || confirmLoading ? <LoaderCircle className="spin" size={18} /> : <WandSparkles size={18} />}{submitting ? '提交中...' : confirmLoading ? '智能优化中...' : '生成商品视频'}
              </button>
            </div>
          </aside>
        </div>

        {confirmOpen ? (
          <div className="modal-backdrop video-confirm-backdrop" role="dialog" aria-modal="true" aria-label="确认生成商品视频" onMouseDown={() => !submitting && setConfirmOpen(false)}>
            <div className="modal-panel video-generation-confirm" onMouseDown={event => event.stopPropagation()}>
              <div className="video-confirm-heading">
                <span><WalletCards size={19} /></span>
                <div><h2>确认生成商品视频</h2><p>{billingEnabled ? '确认规格与积分后，任务才会进入生成队列。' : t('generationConfirmPersonal')}</p></div>
              </div>
              <div className="video-confirm-specs">
                <div><span>视频模型</span><strong>{selectedModel?.name || '-'}</strong></div>
                <div><span>视频规格</span><strong>{aspectRatio} · {durationLabel(selectedModel, duration)} · {resolutionLabel(resolution)}</strong></div>
                <div><span>交付数量</span><strong>1 条视频</strong></div>
                <div><span>参考素材</span><strong>{selectedCount} 个文件</strong></div>
              </div>
              <BillingOnly><div className="video-confirm-balance">
                <div><span>当前积分</span><strong>{credits === null ? '-' : credits}</strong></div>
                <div><span>本次扣除</span><strong>-{chargedPrice}{config?.mock_mode ? '（Mock）' : ''}</strong></div>
                <div><span>生成后预计</span><strong className={hasEnoughCredits ? '' : 'is-insufficient'}>{balanceAfter === null ? '-' : Math.max(0, balanceAfter)}</strong></div>
              </div></BillingOnly>
              <div className="video-confirm-script">
                <div className="video-confirm-script-head">
                  <span><Sparkles size={15} />智能脚本</span>
                  <span className="video-script-badge">{scriptMode === 'storyboard' ? '分镜模式' : '单镜头模式'} · {scriptSource === 'ai' ? 'AI 优化' : '规则优化'}</span>
                </div>
                <textarea
                  value={preparedPrompt}
                  maxLength={4000}
                  onChange={event => setPreparedPrompt(event.target.value)}
                  aria-label="智能优化后的视频脚本"
                />
                <small>{scriptSummary || '已按商品连续性、镜头和合规要求整理，可直接编辑。'}</small>
              </div>
              <BillingOnly>{!hasEnoughCredits ? <div className="notice notice-error">当前积分不足，本次需要 {price} 积分。</div> : <div className="video-confirm-refund-note">任务生成失败时，本次扣除的积分会自动原路退回。</div>}</BillingOnly>
              <div className="modal-actions">
                <button className="btn btn-secondary" type="button" disabled={submitting} onClick={() => setConfirmOpen(false)}>取消</button>
                {!hasEnoughCredits ? <BillingOnly><a className="btn btn-primary" href="/credits">充值 / 兑换</a></BillingOnly> : (
                  <button className="btn btn-primary" type="button" disabled={submitting || preparedPrompt.trim().length < 4} onClick={submit}>
                    {submitting ? <LoaderCircle className="spin" size={18} /> : <WandSparkles size={18} />}{submitting ? '提交中...' : !billingEnabled ? t('confirmSubmit') : config?.mock_mode ? `确认 Mock · 展示价 ${price}` : `确认生成 · ${price} 积分`}
                  </button>
                )}
              </div>
            </div>
          </div>
        ) : null}

        <section className="panel mt-24 video-result-panel">
          <div className="panel-head"><div><h2>当前任务</h2><p>离开页面后任务仍会继续，可在历史记录中查看。</p></div></div>
          {activeTask ? (
            <div className="panel-body video-current-task">
              <div className={`video-stage ${activeTask.aspect_ratio === '16:9' ? 'video-stage-landscape' : ''}`}>
                {activeTask.result_url ? <video controls src={assetUrl(activeTask.result_url)} /> : activeTask.status === 'completed' && isMock(activeTask) ? (
                  <div className="video-mock-result"><span><Play size={26} /></span><strong>Mock 视频任务已完成</strong><p>付费上游关闭，未生成真实视频文件。</p></div>
                ) : (
                  <div className="video-task-poster">
                    {activeTask.source_image_urls?.[0] ? <img src={assetUrl(activeTask.source_image_urls[0])} alt="商品参考图" /> : null}
                    <div><Sparkles size={24} /><strong>{statusText(activeTask.status)}</strong></div>
                  </div>
                )}
              </div>
              <div className="video-task-detail">
                <div className="video-task-title"><div><span className={statusClass(activeTask.status)}>{statusText(activeTask.status)}</span><h3>{activeTask.title}</h3></div><span>{formatDate(activeTask.created_at)}</span></div>
                <p>{activeTask.prompt}</p>
                <div className="video-progress-track"><span style={{ width: `${Math.max(0, Math.min(100, activeTask.progress || 0))}%` }} /></div>
                <div className="video-progress-meta"><span>{activeTask.progress || 0}%</span><span>{videoModelLabel(activeTask.model, activeTask.provider)} · {activeTask.aspect_ratio} · {activeTask.duration_seconds} 秒 · {activeTask.requested_resolution}</span></div>
                {queueNotice(activeTask) ? <div className="notice video-queue-notice"><Clock3 size={16} />{queueNotice(activeTask)}</div> : null}
                {activeTask.safe_error ? <div className="notice notice-error">{activeTask.safe_error}</div> : null}
              </div>
            </div>
          ) : <div className="panel-body video-empty"><Film size={28} /><p>暂无视频任务</p></div>}
        </section>

        {tasks.length > 1 ? (
          <section className="panel mt-24">
            <div className="panel-head"><div><h2>最近视频</h2><p>最新 100 条视频任务会保留在历史记录中。</p></div></div>
            <div className="table-wrap"><table className="data-table video-recent-table"><thead><tr><th>任务</th><th>模型</th><th>规格</th><th>状态</th><BillingOnly><th>积分</th></BillingOnly><th>更新时间</th><th>操作</th></tr></thead><tbody>
              {tasks.map(task => <tr key={task.id}><td><strong>{task.title}</strong><div className="table-subtext">{task.prompt.slice(0, 64)}</div></td><td><span className="video-model-label">{videoModelLabel(task.model, task.provider)}</span></td><td>{task.aspect_ratio} · {task.duration_seconds}s</td><td className="video-status-cell"><span className={statusClass(task.status)}>{statusText(task.status)}</span>{queueNotice(task) ? <div className="table-subtext">{queueNotice(task)}</div> : null}</td><BillingOnly><td><div className="video-credit-cell"><span>{isMock(task) ? `${task.metadata?.listed_price || 0}（Mock）` : `${task.credits_cost} 积分`}</span>{task.refunded_at && task.status !== 'completed' ? <span className="video-settlement video-settlement-refund" title="本次扣除的积分已退回">{videoSettlementLabel(task.refunded_at)}</span> : null}</div></td></BillingOnly><td>{formatDate(task.updated_at)}</td><td><button className="icon-button" type="button" aria-label="删除视频任务" title={ACTIVE_STATUSES.has(task.status) ? '视频完成后可删除' : '删除视频任务'} disabled={ACTIVE_STATUSES.has(task.status)} onClick={() => removeTask(task)}><Trash2 size={16} /></button></td></tr>)}
            </tbody></table></div>
          </section>
        ) : null}
      </UserAuthGate>
    </Shell>
  )
}
