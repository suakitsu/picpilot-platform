'use client'

import { useEffect, useState } from 'react'
import { useI18n } from '@/components/i18n'
import { useConfirm } from '@/components/ConfirmProvider'
import { Pagination } from '@/components/Pagination'
import { createPromptTemplate, ImageTypeConfig, listImageConfigs, listPromptTemplates, PromptTemplate, updateImageConfig, updatePromptTemplate } from '@/lib/api'
import { TEMPLATE_CATALOG_STATS } from '@/lib/templateCatalogStats'

const LISTING_OUTPUT_SIZE_OPTIONS = ['1024x1024', '1600x1600']
const PROMPT_SCENE_OPTIONS = ['copy/listing', 'copy/title', 'image/brief', 'image/aplus', 'image/creative', 'image/single', 'prompt/optimize', 'prompt/creative-chat']
const CONFIG_GROUPS = ['all', 'amazon', 'domestic', 'creative', 'system'] as const
const PROMPT_GROUPS = ['all', 'copy', 'image', 'agent', 'system'] as const

type ConfigGroup = typeof CONFIG_GROUPS[number]
type PromptGroup = typeof PROMPT_GROUPS[number]
type CatalogTemplate = typeof TEMPLATE_CATALOG_STATS.items[number]

function textOf(...values: Array<string | undefined>) {
  return values.filter(Boolean).join(' ').toLowerCase()
}

function configGroup(row: ImageTypeConfig): ConfigGroup {
  const text = textOf(row.image_type, row.image_name, row.required_fields)
  if (text.includes('domestic') || text.includes('taobao') || text.includes('jd') || text.includes('pdd') || text.includes('douyin') || text.includes('xhs') || text.includes('1688') || text.includes('国内') || text.includes('淘宝') || text.includes('京东') || text.includes('拼多多') || text.includes('抖音') || text.includes('小红书')) return 'domestic'
  if (text.includes('creative') || text.includes('free') || text.includes('自由')) return 'creative'
  if (
    text.includes('aplus') || text.includes('listing') || text.includes('amazon') || text.includes('七图') || text.includes('a+') ||
    text.includes('main') || text.includes('scenario') || text.includes('features') || text.includes('dimensions') || text.includes('steps') || text.includes('comparison') || text.includes('package') ||
    text.includes('主图') || text.includes('场景') || text.includes('卖点') || text.includes('尺寸') || text.includes('步骤') || text.includes('对比') || text.includes('包装')
  ) return 'amazon'
  return 'system'
}

function promptGroup(row: PromptTemplate): PromptGroup {
  const text = textOf(row.scene, row.name)
  if (text.startsWith('copy/') || text.includes('listing') || text.includes('title')) return 'copy'
  if (text.startsWith('image/')) return 'image'
  if (text.includes('creative-chat') || text.includes('agent') || text.includes('optimize')) return 'agent'
  return 'system'
}

function templateImageCount(group: ConfigGroup) {
  return TEMPLATE_CATALOG_STATS.image[group] ?? 0
}

function templatePromptCount(group: PromptGroup) {
  return TEMPLATE_CATALOG_STATS.prompt[group] ?? 0
}

function catalogConfigGroup(item: CatalogTemplate): ConfigGroup {
  if (item.kind !== 'image') return 'system'
  if (item.imageGroup === 'domestic') return 'domestic'
  if (item.imageGroup === 'creative') return 'creative'
  if (item.imageGroup === 'overseas') return 'amazon'
  return 'system'
}

function catalogPromptGroup(item: CatalogTemplate): PromptGroup {
  if (item.kind === 'copy') return 'copy'
  if (item.kind === 'image') return 'image'
  return 'system'
}

export default function Page() {
  const [configs, setConfigs] = useState<ImageTypeConfig[]>([])
  const [prompts, setPrompts] = useState<PromptTemplate[]>([])
  const [message, setMessage] = useState('')
  const [newName, setNewName] = useState('')
  const [newScene, setNewScene] = useState('')
  const [newContent, setNewContent] = useState('')
  const [dirtyConfigIds, setDirtyConfigIds] = useState<string[]>([])
  const [configFilter, setConfigFilter] = useState<ConfigGroup>('all')
  const [promptFilter, setPromptFilter] = useState<PromptGroup>('all')
  const [catalogPage, setCatalogPage] = useState(1)
  const [search, setSearch] = useState('')
  const t = useI18n()
  const confirm = useConfirm()

  async function load() {
    try {
      const [configRows, promptRows] = await Promise.all([listImageConfigs(), listPromptTemplates()])
      setConfigs(configRows)
      setPrompts(promptRows)
      setMessage('')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('prompts.loadFailed'))
    }
  }

  useEffect(() => { load() }, [])
  useEffect(() => { setCatalogPage(1) }, [configFilter, promptFilter, search])

  function patchConfig(id: string, patch: Partial<ImageTypeConfig>) {
    setConfigs(current => current.map(item => item.id === id ? { ...item, ...patch } : item))
    setDirtyConfigIds(current => current.includes(id) ? current : [...current, id])
  }

  function patchPrompt(id: string, patch: Partial<PromptTemplate>) {
    setPrompts(current => current.map(item => item.id === id ? { ...item, ...patch } : item))
  }

  async function saveConfig(config: ImageTypeConfig) {
    if (!await confirm({ title: t('prompts.confirmSaveConfig'), message: `${t('prompts.confirmSaveConfig')} ${config.image_name}？` })) return
    try {
      const saved = await updateImageConfig(config)
      setConfigs(current => current.map(item => item.id === saved.id ? saved : item))
      setDirtyConfigIds(current => current.filter(id => id !== saved.id))
      setMessage(`${t('prompts.saved')}${saved.image_name}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('prompts.saveFailed'))
    }
  }

  async function createPrompt() {
    if (!newName || !newScene || !newContent) {
      setMessage(t('prompts.missingFields'))
      return
    }
    if (!await confirm({ title: t('prompts.confirmCreatePrompt'), message: `${t('prompts.confirmCreatePrompt')} ${newName}？` })) return
    try {
      await createPromptTemplate({ name: newName, scene: newScene, content: newContent })
      setNewName('')
      setNewScene('')
      setNewContent('')
      setMessage(t('prompts.created'))
      await load()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('prompts.createFailed'))
    }
  }

  async function saveAll() {
    try {
      const changed = configs.filter(config => dirtyConfigIds.includes(config.id))
      if (!changed.length) {
        setMessage(t('prompts.noDirtyConfigs'))
        return
      }
      if (!await confirm({ title: t('prompts.confirmSaveAll'), message: `${t('prompts.confirmSaveAll')} ${changed.length}？` })) return
      await Promise.all(changed.map(config => updateImageConfig(config)))
      setMessage(t('prompts.allSaved'))
      await load()
      setDirtyConfigIds([])
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('prompts.batchSaveFailed'))
    }
  }

  async function savePrompt(prompt: PromptTemplate) {
    if (!await confirm({ title: t('prompts.confirmSavePrompt'), message: `${t('prompts.confirmSavePrompt')} ${prompt.name}？` })) return
    try {
      const saved = await updatePromptTemplate(prompt)
      patchPrompt(saved.id, saved)
      setMessage(`${t('prompts.promptSaved')}${saved.name}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : t('prompts.promptSaveFailed'))
    }
  }

  const normalizedSearch = search.trim().toLowerCase()
  const configGroupCounts = CONFIG_GROUPS.reduce((acc, group) => {
    acc[group] = group === 'all' ? configs.length : configs.filter(row => configGroup(row) === group).length
    return acc
  }, {} as Record<ConfigGroup, number>)
  const promptGroupCounts = PROMPT_GROUPS.reduce((acc, group) => {
    acc[group] = group === 'all' ? prompts.length : prompts.filter(row => promptGroup(row) === group).length
    return acc
  }, {} as Record<PromptGroup, number>)
  const visibleConfigs = configs.filter(row => {
    const matchesGroup = configFilter === 'all' || configGroup(row) === configFilter
    const matchesSearch = !normalizedSearch || textOf(row.image_type, row.image_name, row.required_fields, row.default_model, row.fallback_model).includes(normalizedSearch)
    return matchesGroup && matchesSearch
  })
  const visiblePrompts = prompts.filter(row => {
    const matchesGroup = promptFilter === 'all' || promptGroup(row) === promptFilter
    const matchesSearch = !normalizedSearch || textOf(row.name, row.scene, row.content).includes(normalizedSearch)
    return matchesGroup && matchesSearch
  })
  const catalogTemplates = TEMPLATE_CATALOG_STATS.items.filter(item => {
    const matchesConfigGroup = configFilter === 'all' || catalogConfigGroup(item) === configFilter
    const matchesPromptGroup = promptFilter === 'all' || catalogPromptGroup(item) === promptFilter
    const matchesSearch = !normalizedSearch || textOf(item.id, item.title, item.subtitle, item.platform, item.category, item.routeType, item.market).includes(normalizedSearch)
    return matchesConfigGroup && matchesPromptGroup && matchesSearch
  })
  const catalogPageSize = 12
  const catalogTotalPages = Math.ceil(catalogTemplates.length / catalogPageSize)
  const catalogRows = catalogTemplates.slice((catalogPage - 1) * catalogPageSize, catalogPage * catalogPageSize)
  const noConfigText = configFilter !== 'all' && templateImageCount(configFilter)
    ? t('prompts.noConfigsButTemplates').replace('{count}', String(templateImageCount(configFilter)))
    : t('prompts.noConfigs')

  return (
    <>
      <div className="page-head"><div><h1>{t('prompts.title')}</h1><p>{t('prompts.desc')}</p></div><button className="btn btn-primary" onClick={saveAll}>{t('prompts.saveAll')}</button></div>
      {message ? <div className="admin-card muted spacious-message">{message}</div> : null}
      <div className="filter-bar action-filter-bar prompt-admin-filter">
        <input className="input" value={search} onChange={event => setSearch(event.target.value)} placeholder={t('prompts.searchPlaceholder')} />
        <div className="segmented-control prompt-segments">
          <span className="segmented-control-title">{t('prompts.configFilterTitle')}</span>
          {CONFIG_GROUPS.map(group => <button key={group} className={configFilter === group ? 'segment-active' : ''} type="button" title={`${t(`prompts.configGroup.${group}`)}: ${t('prompts.templateCountLabel')} ${templateImageCount(group)} / ${t('prompts.configCountLabel')} ${configGroupCounts[group]}`} disabled={group !== 'all' && !templateImageCount(group) && !configGroupCounts[group]} onClick={() => setConfigFilter(group)}><span className="segment-label">{t(`prompts.configGroup.${group}`)}</span><span className="segment-count">{templateImageCount(group)}</span><span className="segment-subcount">{t('prompts.configCountLabel')} {configGroupCounts[group]}</span></button>)}
        </div>
        <div className="segmented-control prompt-segments">
          <span className="segmented-control-title">{t('prompts.promptFilterTitle')}</span>
          {PROMPT_GROUPS.map(group => <button key={group} className={promptFilter === group ? 'segment-active' : ''} type="button" title={`${t(`prompts.promptGroup.${group}`)}: ${t('prompts.templateCountLabel')} ${templatePromptCount(group)} / ${t('prompts.promptCountLabel')} ${promptGroupCounts[group]}`} disabled={group !== 'all' && !templatePromptCount(group) && !promptGroupCounts[group]} onClick={() => setPromptFilter(group)}><span className="segment-label">{t(`prompts.promptGroup.${group}`)}</span><span className="segment-count">{templatePromptCount(group)}</span><span className="segment-subcount">{t('prompts.promptCountLabel')} {promptGroupCounts[group]}</span></button>)}
        </div>
      </div>
      <section className="admin-card table-card config-section operation-card">
        <div className="section-head">
          <div><h2>{t('prompts.imageTemplates')}</h2><p>{t('prompts.imageTemplatesDesc')}</p></div>
        </div>
        <table className="data-table"><thead><tr><th>{t('prompts.group')}</th><th>{t('prompts.imageType')}</th><th>{t('prompts.switch')}</th><th>{t('prompts.creditCost')}</th><th>{t('prompts.defaultModel')}</th><th>{t('common.size')}</th><th>{t('prompts.fallbackModel')}</th><th>{t('prompts.requiredFields')}</th><th>{t('common.actions')}</th></tr></thead><tbody>{visibleConfigs.map(row => <tr key={row.id}><td><span className="status-good">{t(`prompts.configGroup.${configGroup(row)}`)}</span></td><td>{row.image_name}<div className="muted small-text">{row.image_type}</div></td><td><select className="select config-select" value={row.enabled ? 'true' : 'false'} onChange={event => patchConfig(row.id, { enabled: event.target.value === 'true' })}><option value="true">{t('common.on')}</option><option value="false">{t('common.off')}</option></select></td><td><input className="input config-number" value={row.credits_cost} onChange={event => patchConfig(row.id, { credits_cost: Number(event.target.value) })} /></td><td><input className="input config-model" value={row.default_model} onChange={event => patchConfig(row.id, { default_model: event.target.value })} /></td><td><select className="select config-model" value={LISTING_OUTPUT_SIZE_OPTIONS.includes(row.default_size || '') ? row.default_size : '1024x1024'} onChange={event => patchConfig(row.id, { default_size: event.target.value })}>{LISTING_OUTPUT_SIZE_OPTIONS.map(size => <option value={size} key={size}>{size.replace('x', '×')}</option>)}</select></td><td><input className="input config-model" value={row.fallback_model} onChange={event => patchConfig(row.id, { fallback_model: event.target.value })} /></td><td><input className="input config-fields" value={row.required_fields} onChange={event => patchConfig(row.id, { required_fields: event.target.value })} /></td><td><button className="btn" onClick={() => saveConfig(row)}>{t('common.save')}</button></td></tr>)}{!visibleConfigs.length ? <tr><td colSpan={9} className="muted">{noConfigText}</td></tr> : null}</tbody></table>
      </section>
      <section className="admin-card table-card section config-section operation-card">
        <div className="section-head"><div><h2>{t('prompts.catalogTitle')}</h2><p>{t('prompts.catalogDesc').replace('{count}', String(TEMPLATE_CATALOG_STATS.total))}</p></div><span className="status-good">{t('prompts.catalogReadonly')}</span></div>
        <table className="data-table catalog-template-table"><thead><tr><th>{t('prompts.catalogTemplate')}</th><th>{t('prompts.catalogKind')}</th><th>{t('prompts.catalogMarket')}</th><th>{t('prompts.catalogPlatform')}</th><th>{t('prompts.catalogCategory')}</th><th>{t('prompts.catalogRoute')}</th></tr></thead><tbody>{catalogRows.map(item => <tr key={item.id}><td><strong>{item.title}</strong><div className="muted small-text">{item.subtitle}</div><div className="muted small-text template-id">{item.id}</div></td><td><span className="status-good">{item.kind === 'copy' ? t('prompts.promptGroup.copy') : t('prompts.promptGroup.image')}</span></td><td>{item.market === 'domestic' ? t('prompts.market.domestic') : t('prompts.market.overseas')}</td><td>{item.platform}</td><td>{item.category}</td><td>{t(`prompts.route.${item.routeType}`)}</td></tr>)}{!catalogRows.length ? <tr><td colSpan={6} className="muted">{t('prompts.catalogNoRows')}</td></tr> : null}</tbody></table>
        <Pagination page={catalogPage} total={catalogTemplates.length} pageSize={catalogPageSize} totalPages={catalogTotalPages} onPageChange={setCatalogPage} labels={{ previous: t('common.previous'), next: t('common.next'), page: t('common.page'), total: t('common.total') }} />
      </section>
      <section className="admin-card table-card section config-section operation-card operation-primary">
        <div className="section-head"><div><h2>{t('prompts.runtimePromptTitle')}</h2><p>{t('prompts.runtimePromptDesc')}</p></div><button className="btn btn-primary" onClick={createPrompt}>{t('prompts.newTemplate')}</button></div>
        <div className="filter-bar prompt-form"><input className="input" placeholder={t('prompts.namePlaceholder')} value={newName} onChange={event => setNewName(event.target.value)} /><input className="input" list="prompt-scenes" placeholder={t('prompts.scenePlaceholder')} value={newScene} onChange={event => setNewScene(event.target.value)} /><textarea className="textarea prompt-content-input prompt-content-textarea" placeholder={t('prompts.contentPlaceholder')} value={newContent} onChange={event => setNewContent(event.target.value)} /></div>
        <datalist id="prompt-scenes">{PROMPT_SCENE_OPTIONS.map(scene => <option value={scene} key={scene} />)}</datalist>
        <table className="data-table prompt-table"><thead><tr><th>{t('prompts.group')}</th><th>{t('common.name')}</th><th>{t('prompts.scene')}</th><th>{t('prompts.promptContent')}</th><th>{t('common.version')}</th><th>{t('common.status')}</th><th>{t('common.updatedAt')}</th><th>{t('common.actions')}</th></tr></thead><tbody>{visiblePrompts.map(row => <tr key={row.id}><td><span className="status-good">{t(`prompts.promptGroup.${promptGroup(row)}`)}</span></td><td><input className="input config-model" value={row.name} onChange={event => patchPrompt(row.id, { name: event.target.value })} /></td><td><input className="input config-model" list="prompt-scenes" value={row.scene} onChange={event => patchPrompt(row.id, { scene: event.target.value })} /></td><td><textarea className="textarea prompt-content-input prompt-content-textarea" value={row.content || ''} onChange={event => patchPrompt(row.id, { content: event.target.value })} /></td><td>v{row.version}</td><td><select className="select config-select" value={row.enabled ? 'true' : 'false'} onChange={event => patchPrompt(row.id, { enabled: event.target.value === 'true' })}><option value="true">{t('common.enabled')}</option><option value="false">{t('common.disabled')}</option></select></td><td>{row.updated_at ? new Date(row.updated_at).toLocaleString() : '-'}</td><td><button className="btn" onClick={() => savePrompt(row)}>{t('common.save')}</button></td></tr>)}{!visiblePrompts.length ? <tr><td colSpan={8} className="muted">{t('prompts.noPrompts')}</td></tr> : null}</tbody></table>
      </section>
    </>
  )
}
