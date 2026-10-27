'use client'

import Link from 'next/link'
import { useMemo, useState } from 'react'
import { ArrowRight, BookOpenText, Filter, ImageIcon, Layers3, Search, Sparkles, X } from 'lucide-react'
import { Shell } from '@/components/Shell'
import { TEMPLATE_CATALOG, TEMPLATE_SOURCE_NOTES, TemplateCatalogItem, TemplateImageGroup, TemplateKind, TemplateMarket, templateHref } from '@/lib/templateCatalog'

const KIND_LABEL: Record<TemplateKind, string> = {
  image: '图片模板',
  copy: '文案模板'
}

const MARKET_LABEL: Record<TemplateMarket | 'all', string> = {
  all: '全部市场',
  domestic: '国内',
  overseas: '国外'
}

const IMAGE_GROUP_LABEL: Record<TemplateImageGroup | 'all', string> = {
  all: '全部图片',
  domestic: '国内图片',
  overseas: '国外图片',
  creative: '自由创作'
}

const PAGE_SIZE = 12

function unique(values: string[]) {
  return Array.from(new Set(values)).filter(Boolean)
}

function defaultImageGroup(item: TemplateCatalogItem): TemplateImageGroup {
  if (item.imageGroup) return item.imageGroup
  if (item.kind === 'image' && item.routeType === 'creative') return 'creative'
  if (item.kind !== 'image') return 'domestic'
  return item.market
}

function previewClass(item: TemplateCatalogItem) {
  return `template-preview ${item.previewImage ? 'template-preview-image' : `template-preview-${item.previewTone}`}`
}

function TemplatePreview({ item }: { item: TemplateCatalogItem }) {
  return (
    <>
      {item.previewImage ? (
        <img src={item.previewImage} alt={`${item.title} 案例图`} loading="eager" fetchPriority="high" decoding="sync" />
      ) : (
        <>
          <span className="template-preview-band" />
          <span className="template-preview-product" />
          <span className="template-preview-lines"><i /><i /><i /></span>
        </>
      )}
    </>
  )
}

function routeLabel(item: TemplateCatalogItem) {
  if (item.routeType === 'domestic') return '国内电商'
  if (item.routeType === 'listing') return '亚马逊 Listing'
  if (item.routeType === 'aplus') return '亚马逊 A+'
  if (item.routeType === 'creative') return '自由创作'
  return '文案 Agent'
}

function matchesTemplateFilters(
  item: TemplateCatalogItem,
  filters: {
    kind: TemplateKind | 'all'
    market: TemplateMarket | 'all'
    imageGroup: TemplateImageGroup | 'all'
    platform?: string
    category?: string
    query?: string
  }
) {
  if (filters.kind !== 'all' && item.kind !== filters.kind) return false
  if (filters.imageGroup !== 'all') {
    if (item.kind !== 'image') return false
    if (defaultImageGroup(item) !== filters.imageGroup) return false
  }
  if (filters.market !== 'all' && item.market !== filters.market) return false
  if (filters.platform && filters.platform !== 'all' && item.platform !== filters.platform) return false
  if (filters.category && filters.category !== 'all' && item.category !== filters.category) return false
  const normalizedQuery = filters.query?.trim().toLowerCase()
  if (!normalizedQuery) return true
  const haystack = [item.title, item.subtitle, item.description, item.platform, item.category, ...item.tags].join(' ').toLowerCase()
  return haystack.includes(normalizedQuery)
}

function resultLabel(kind: TemplateKind | 'all', imageGroup: TemplateImageGroup | 'all', market: TemplateMarket | 'all') {
  if (kind === 'copy') return `${KIND_LABEL.copy} / ${MARKET_LABEL[market]}`
  if (kind === 'image' || imageGroup !== 'all') return `${KIND_LABEL.image} / ${IMAGE_GROUP_LABEL[imageGroup]}`
  return `图片 + 文案 / ${MARKET_LABEL[market]}`
}

function FilterContent({
  kind,
  market,
  imageGroup,
  platform,
  category,
  platforms,
  categories,
  onKind,
  onMarket,
  onImageGroup,
  onPlatform,
  onCategory
}: {
  kind: TemplateKind | 'all'
  market: TemplateMarket | 'all'
  imageGroup: TemplateImageGroup | 'all'
  platform: string
  category: string
  platforms: string[]
  categories: string[]
  onKind: (value: TemplateKind | 'all') => void
  onMarket: (value: TemplateMarket | 'all') => void
  onImageGroup: (value: TemplateImageGroup | 'all') => void
  onPlatform: (value: string) => void
  onCategory: (value: string) => void
}) {
  return (
    <div className="template-filter-content">
      <div className="template-filter-group">
        <span>模板类型</span>
        <div className="template-segment">
          {(['all', 'image', 'copy'] as const).map(item => (
            <button key={item} type="button" className={kind === item ? 'active' : ''} onClick={() => onKind(item)}>
              {item === 'all' ? '全部' : KIND_LABEL[item]}
            </button>
          ))}
        </div>
      </div>
      {kind !== 'copy' ? (
        <div className="template-filter-group">
          <span>图片分组</span>
          <div className="template-segment">
            {(['all', 'domestic', 'overseas', 'creative'] as const).map(item => (
              <button key={item} type="button" className={imageGroup === item ? 'active' : ''} onClick={() => onImageGroup(item)}>
                {IMAGE_GROUP_LABEL[item]}
              </button>
            ))}
          </div>
        </div>
      ) : null}
      <div className="template-filter-group">
        <span>市场</span>
        <div className="template-segment">
          {(['all', 'domestic', 'overseas'] as const).map(item => (
            <button key={item} type="button" className={market === item ? 'active' : ''} onClick={() => onMarket(item)}>
              {MARKET_LABEL[item]}
            </button>
          ))}
        </div>
      </div>
      <label className="field">
        <span>平台</span>
        <select className="select" value={platform} onChange={event => onPlatform(event.target.value)}>
          <option value="all">全部平台</option>
          {platforms.map(item => <option key={item} value={item}>{item}</option>)}
        </select>
      </label>
      <label className="field">
        <span>用途</span>
        <select className="select" value={category} onChange={event => onCategory(event.target.value)}>
          <option value="all">全部用途</option>
          {categories.map(item => <option key={item} value={item}>{item}</option>)}
        </select>
      </label>
    </div>
  )
}

function TemplateCard({ item, onPreview }: { item: TemplateCatalogItem; onPreview: (item: TemplateCatalogItem) => void }) {
  return (
    <article className="template-card">
      <button type="button" className={previewClass(item)} onClick={() => onPreview(item)} aria-label={`查看 ${item.title}`}>
        <TemplatePreview item={item} />
      </button>
      <div className="template-card-body">
        <div className="template-card-meta">
          <span>{KIND_LABEL[item.kind]}</span>
          <span>{item.kind === 'image' ? IMAGE_GROUP_LABEL[defaultImageGroup(item)] : MARKET_LABEL[item.market]}</span>
        </div>
        <h2>{item.title}</h2>
        <p>{item.subtitle}</p>
        <div className="template-card-tags">
          <em>{item.platform}</em>
          <em>{item.category}</em>
        </div>
      </div>
      <div className="template-card-actions">
        <button type="button" className="btn btn-secondary" onClick={() => onPreview(item)}>预览</button>
        <Link className="btn btn-primary" href={templateHref(item)} prefetch={false}>使用模板<ArrowRight size={15} /></Link>
      </div>
    </article>
  )
}

export default function TemplateCenterPage() {
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState<TemplateKind | 'all'>('all')
  const [market, setMarket] = useState<TemplateMarket | 'all'>('all')
  const [imageGroup, setImageGroup] = useState<TemplateImageGroup | 'all'>('all')
  const [platform, setPlatform] = useState('all')
  const [category, setCategory] = useState('all')
  const [page, setPage] = useState(1)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [selected, setSelected] = useState<TemplateCatalogItem | null>(null)

  const platforms = useMemo(() => unique(TEMPLATE_CATALOG
    .filter(item => matchesTemplateFilters(item, { kind, market, imageGroup, category }))
    .map(item => item.platform)), [category, imageGroup, kind, market])
  const categories = useMemo(() => unique(TEMPLATE_CATALOG
    .filter(item => matchesTemplateFilters(item, { kind, market, imageGroup, platform }))
    .map(item => item.category)), [imageGroup, kind, market, platform])
  const filtered = useMemo(() => {
    return TEMPLATE_CATALOG.filter(item => matchesTemplateFilters(item, { kind, market, imageGroup, platform, category, query }))
  }, [category, imageGroup, kind, market, platform, query])

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const currentPage = Math.min(page, pageCount)
  const paged = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const imageCount = TEMPLATE_CATALOG.filter(item => item.kind === 'image').length
  const copyCount = TEMPLATE_CATALOG.filter(item => item.kind === 'copy').length

  function resetFilters() {
    setQuery('')
    setKind('all')
    setMarket('all')
    setImageGroup('all')
    setPlatform('all')
    setCategory('all')
    setPage(1)
  }

  function updateKind(value: TemplateKind | 'all') {
    setKind(value)
    if (value === 'copy') setImageGroup('all')
    setPlatform('all')
    setCategory('all')
    setPage(1)
  }

  function updateMarket(value: TemplateMarket | 'all') {
    setMarket(value)
    setPlatform('all')
    setCategory('all')
    setPage(1)
  }

  function updateImageGroup(value: TemplateImageGroup | 'all') {
    setImageGroup(value)
    if (value === 'creative' || value === 'all') {
      setMarket('all')
    } else {
      setMarket(value)
    }
    setPlatform('all')
    setCategory('all')
    setPage(1)
  }

  function updatePlatform(value: string) {
    setPlatform(value)
    setPage(1)
  }

  function updateCategory(value: string) {
    setCategory(value)
    setPage(1)
  }

  return (
    <Shell>
      <div className="page-head template-page-head">
        <div>
          <h1>模板中心</h1>
          <p>先选模板，再进入生成流程。国内电商、TikTok、Amazon 和文案 Agent 都收在这里。</p>
        </div>
        <div className="template-head-actions">
          <Link className="btn btn-secondary" href="/ai-create?tab=agent" prefetch={false}><Sparkles size={16} />生图助手</Link>
          <Link className="btn btn-primary" href="/commerce?tab=domestic" prefetch={false}><Layers3 size={16} />直接制图</Link>
        </div>
      </div>

      <section className="template-hero">
        <div className="template-hero-main">
          <strong>PicPilot 官方模板库</strong>
          <h2>把复杂表单收起来，让用户从“我要做什么图/文案”开始。</h2>
          <p>模板会预填平台、用途、构图、图内文案和 Agent 提示词。案例图里的文字由 AI 直接生成在图片内。</p>
        </div>
        <div className="template-hero-stats">
          <div><ImageIcon size={18} /><span>{imageCount}</span><small>图片模板</small></div>
          <div><BookOpenText size={18} /><span>{copyCount}</span><small>文案模板</small></div>
        </div>
      </section>

      <div className="template-toolbar">
        <div className="template-search">
          <Search size={17} />
          <input value={query} onChange={event => { setQuery(event.target.value); setPage(1) }} placeholder="搜索平台、用途、模板名，例如 TikTok、小红书、主图、直播话术" />
        </div>
        <button className="btn btn-secondary" type="button" onClick={() => setDrawerOpen(true)}><Filter size={16} />筛选</button>
        <button className="btn" type="button" onClick={resetFilters}>重置</button>
      </div>

      <div className="template-layout">
        <aside className="template-filter-rail panel">
          <div className="panel-head"><div><h2>筛选</h2><p>按模板类型、市场、平台和用途缩小范围。</p></div></div>
          <div className="panel-body">
            <FilterContent kind={kind} market={market} imageGroup={imageGroup} platform={platform} category={category} platforms={platforms} categories={categories} onKind={updateKind} onMarket={updateMarket} onImageGroup={updateImageGroup} onPlatform={updatePlatform} onCategory={updateCategory} />
          </div>
        </aside>
        <section className="template-results">
          <div className="template-result-head">
            <span>共 {filtered.length} 个模板</span>
            <div>{resultLabel(kind, imageGroup, market)}</div>
          </div>
          <div className="template-card-grid">
            {paged.map(item => <TemplateCard item={item} key={item.id} onPreview={setSelected} />)}
            {!filtered.length ? <div className="notice">没有匹配模板，换个关键词或清空筛选。</div> : null}
          </div>
          {filtered.length > PAGE_SIZE ? (
            <div className="template-pagination">
              <button className="btn btn-secondary" type="button" disabled={currentPage <= 1} onClick={() => setPage(value => Math.max(1, value - 1))}>上一页</button>
              <span>第 {currentPage} / {pageCount} 页</span>
              <button className="btn btn-secondary" type="button" disabled={currentPage >= pageCount} onClick={() => setPage(value => Math.min(pageCount, value + 1))}>下一页</button>
            </div>
          ) : null}
        </section>
      </div>

      <section className="panel mt-24">
        <div className="panel-head"><div><h2>模板来源策略</h2><p>参考优秀公开项目的结构，但沉淀成 PicPilot 自有模板。</p></div></div>
        <div className="panel-body template-source-list">
          {TEMPLATE_SOURCE_NOTES.map(item => <p key={item}>{item}</p>)}
        </div>
      </section>

      {drawerOpen ? (
        <div className="template-drawer-backdrop" role="dialog" aria-modal="true" aria-label="模板筛选">
          <aside className="template-filter-drawer">
            <div className="template-drawer-head">
              <strong>筛选模板</strong>
              <button className="icon-button" type="button" onClick={() => setDrawerOpen(false)} aria-label="关闭"><X size={17} /></button>
            </div>
            <FilterContent kind={kind} market={market} imageGroup={imageGroup} platform={platform} category={category} platforms={platforms} categories={categories} onKind={updateKind} onMarket={updateMarket} onImageGroup={updateImageGroup} onPlatform={updatePlatform} onCategory={updateCategory} />
            <button className="btn btn-primary btn-block" type="button" onClick={() => setDrawerOpen(false)}>查看 {filtered.length} 个模板</button>
          </aside>
        </div>
      ) : null}

      {selected ? (
        <div className="template-drawer-backdrop template-detail-backdrop" role="dialog" aria-modal="true" aria-label="模板详情">
          <aside className="template-detail-drawer">
            <div className="template-drawer-head">
              <strong>模板详情</strong>
              <button className="icon-button" type="button" onClick={() => setSelected(null)} aria-label="关闭"><X size={17} /></button>
            </div>
            <div className={previewClass(selected)}>
              <TemplatePreview item={selected} />
            </div>
            <div className="template-detail-body">
              <div className="template-card-meta">
                <span>{KIND_LABEL[selected.kind]}</span>
                <span>{selected.platform}</span>
                <span>{selected.category}</span>
              </div>
              <h2>{selected.title}</h2>
              <p>{selected.description}</p>
              <div className="template-detail-section">
                <strong>使用后进入</strong>
                <span>{routeLabel(selected)}</span>
              </div>
              <div className="template-detail-section">
                <strong>适合场景</strong>
                <div className="template-card-tags">{selected.tags.map(tag => <em key={tag}>{tag}</em>)}</div>
              </div>
              {selected.routeType === 'copy' && selected.copyPreset ? (
                <div className="template-detail-section">
                  <strong>文案需要的信息</strong>
                  <ul>{selected.copyPreset.requiredInfo.map(item => <li key={item}>{item}</li>)}</ul>
                </div>
              ) : null}
              <Link className="btn btn-primary btn-block" href={templateHref(selected)} prefetch={false}>使用模板<ArrowRight size={15} /></Link>
            </div>
          </aside>
        </div>
      ) : null}
    </Shell>
  )
}
