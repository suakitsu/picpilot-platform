export type TemplateKind = 'image' | 'copy'
export type TemplateMarket = 'domestic' | 'overseas'
export type TemplateImageGroup = 'domestic' | 'overseas' | 'creative'
export type TemplateRouteType = 'domestic' | 'listing' | 'aplus' | 'creative' | 'copy'
export type TemplateImageIntent = 'reference_preserve' | 'background_edit' | 'poster_layout' | 'free_create'
export type TemplateTextMode = 'ai_short_text' | 'no_text'
export type TemplateReferencePolicy = 'preserve' | 'style_reference' | 'none'

export type TemplateIntentMeta = {
  templateId?: string
  intent: TemplateImageIntent
  textMode: TemplateTextMode
  referencePolicy: TemplateReferencePolicy
}

export type TemplateCatalogItem = {
  id: string
  kind: TemplateKind
  market: TemplateMarket
  imageGroup?: TemplateImageGroup
  platform: string
  category: string
  title: string
  subtitle: string
  description: string
  routeType: TemplateRouteType
  tags: string[]
  previewTone: 'red' | 'blue' | 'green' | 'pink' | 'gold' | 'purple' | 'dark' | 'clean'
  previewImage?: string
  sourceNote?: string
  intent?: TemplateImageIntent
  textMode?: TemplateTextMode
  referencePolicy?: TemplateReferencePolicy
  imagePreset?: {
    platform?: string
    usage?: string
    productName?: string
    brand?: string
    sellingPoints?: string
    specs?: string
    audience?: string
    forbiddenWords?: string
    size?: string
    enableTextLayer?: boolean
    title?: string
    subtitle?: string
    points?: string[]
    priceText?: string
    badge?: string
    style?: string
    prompt?: string
    aplusModule?: string
    aplusSize?: string
  }
  listingPreset?: {
    brand?: string
    productName?: string
    country?: string
    language?: string
    category?: string
    keywords?: string
    sellingKeywords?: string
    descriptionKeywords?: string
    material?: string
    dimensions?: string
    description?: string
    includeAplus?: boolean
    aplusModule?: string
    aplusPrompt?: string
  }
  copyPreset?: {
    opening: string
    outputType: string
    requiredInfo: string[]
  }
}

export const TEMPLATE_SOURCE_NOTES = [
  'PicPilot 官方模板：按真实电商工作流重写，参考公开优秀项目的分类思路，不直接复制第三方模板原文。',
  '可参考方向：抖音/快手/小红书脚本结构、电商图片套件分类、GPT-image 案例分类、Amazon Listing 合规框架。',
  '后续后台模板库可补充 source_url、license、预览图、启停、排序和团队私有模板。'
]

export const TEMPLATE_CATALOG: TemplateCatalogItem[] = [
  {
    id: 'img-cn-taobao-main-clean',
    previewImage: '/template-previews/img-cn-taobao-main-clean.webp',
    kind: 'image',
    market: 'domestic',
    platform: '淘宝/天猫',
    category: '主图',
    title: '淘宝天猫清爽主图',
    subtitle: '品牌店质感 + 商品强主体',
    description: '适合新品首发、店铺货架主图和活动承接图，保留标题、3 个卖点和角标文字位。',
    routeType: 'domestic',
    tags: ['主图', '品牌感', 'AI 图内中文', '手机端'],
    previewTone: 'clean',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '主图',
      productName: '',
      sellingPoints: '商品主体清晰、材质质感、适合移动端缩略图',
      size: '1024x1024',
      enableTextLayer: true,
      title: '新品主推',
      subtitle: '一眼看懂核心卖点',
      points: ['主体清晰', '质感精修', '手机端醒目'],
      badge: '新品'
    }
  },
  {
    id: 'img-cn-taobao-campaign',
    previewImage: '/template-previews/img-cn-taobao-campaign.webp',
    kind: 'image',
    market: 'domestic',
    platform: '淘宝/天猫',
    category: '活动图',
    title: '淘宝活动承接图',
    subtitle: '大标题 + 促销区 + 商品陈列',
    description: '适合 618、双 11、新品季等活动视觉，但不自动生成未填写的价格和促销承诺。',
    routeType: 'domestic',
    tags: ['活动承接', '大标题', '促销区', '淘宝'],
    previewTone: 'red',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '详情页模块',
      sellingPoints: '活动氛围、商品卖点、店铺视觉统一',
      size: '1280x720',
      title: '店铺活动主视觉',
      subtitle: '商品利益点集中展示',
      points: ['上新氛围', '卖点清晰', '适合承接页'],
      priceText: '填写真实活动信息',
      badge: '活动视觉'
    }
  },
  {
    id: 'img-cn-jd-spec',
    previewImage: '/template-previews/img-cn-jd-spec.webp',
    kind: 'image',
    market: 'domestic',
    platform: '京东',
    category: '卖点图',
    title: '京东参数卖点图',
    subtitle: '理性可信 + 参数卡片',
    description: '适合数码、家电、日百、工具类商品，用分区表达参数、结构和真实卖点。',
    routeType: 'domestic',
    tags: ['京东', '参数', '卖点图', '可信感'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '参数清晰、结构说明、专业可信',
      size: '1280x720',
      title: '核心参数一屏看懂',
      subtitle: '只展示已确认数据',
      points: ['参数卡片', '细节特写', '专业排版'],
      badge: '品质感'
    }
  },
  {
    id: 'img-cn-pdd-conversion',
    previewImage: '/template-previews/img-cn-pdd-conversion.webp',
    kind: 'image',
    market: 'domestic',
    platform: '拼多多',
    category: '主图',
    title: '拼多多强转化主图',
    subtitle: '缩略图醒目 + 一眼懂卖点',
    description: '适合搜索流和推荐流，强调主体、利益点和短句，不生成虚假低价或补贴承诺。',
    routeType: 'domestic',
    tags: ['拼多多', '转化', '醒目', '强主体'],
    previewTone: 'gold',
    imagePreset: {
      platform: '拼多多',
      usage: '主图',
      sellingPoints: '强识别、强对比、实用卖点',
      size: '1024x1024',
      title: '一眼看懂',
      subtitle: '核心卖点放大',
      points: ['主体大', '卖点短', '列表醒目'],
      badge: '热卖款'
    }
  },
  {
    id: 'img-cn-douyin-live-cover',
    previewImage: '/template-previews/img-cn-douyin-live-cover.webp',
    kind: 'image',
    market: 'domestic',
    platform: '抖音电商',
    category: '直播封面',
    title: '抖音直播封面',
    subtitle: '短视频停留感 + 直播间入口',
    description: '适合直播间封面和短视频首帧，强调人物/上手/近景冲击力和清晰大标题。',
    routeType: 'domestic',
    tags: ['抖音', '直播', '竖版', '封面'],
    previewTone: 'dark',
    imagePreset: {
      platform: '抖音电商',
      usage: '直播封面',
      sellingPoints: '直播主题、商品近景、强停留率',
      size: '720x1280',
      title: '今晚主推',
      subtitle: '商品亮点 3 秒看懂',
      points: ['近景冲击', '标题醒目', '直播氛围'],
      badge: '直播中'
    }
  },
  {
    id: 'img-cn-xhs-cover',
    previewImage: '/template-previews/img-cn-xhs-cover.webp',
    kind: 'image',
    market: 'domestic',
    platform: '小红书',
    category: '种草封面',
    title: '小红书种草封面',
    subtitle: '生活方式 + 笔记首图',
    description: '适合家居、穿搭、美妆、礼物和开箱类商品，画面像真实优质笔记。',
    routeType: 'domestic',
    tags: ['小红书', '种草', '封面', '氛围感'],
    previewTone: 'pink',
    imagePreset: {
      platform: '小红书',
      usage: '小红书种草图',
      sellingPoints: '真实使用、氛围审美、收藏欲',
      size: '768x1024',
      title: '最近爱用',
      subtitle: '真实体验感封面',
      points: ['自然光', '生活感', '收藏欲'],
      badge: '种草'
    }
  },
  {
    id: 'img-cn-xhs-comparison',
    previewImage: '/template-previews/img-cn-xhs-comparison.webp',
    kind: 'image',
    market: 'domestic',
    platform: '小红书',
    category: '对比图',
    title: '小红书前后对比图',
    subtitle: '轻评测 + 真实体验',
    description: '适合收纳、清洁、美妆、穿搭等对比类笔记，用左右分屏和短标签表达变化。',
    routeType: 'domestic',
    tags: ['小红书', '对比', '评测', '分屏'],
    previewTone: 'green',
    imagePreset: {
      platform: '小红书',
      usage: '卖点图',
      sellingPoints: '使用前后对比、真实体验、轻评测感',
      size: '768x1024',
      title: '用前用后对比',
      subtitle: '真实场景轻评测',
      points: ['对比清楚', '不夸大', '好收藏'],
      badge: '实测'
    }
  },
  {
    id: 'img-cn-beauty-ingredient-panel',
    previewImage: '/template-previews/img-cn-beauty-ingredient-panel-real.jpg',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '成分说明',
    title: '淘宝美妆成分图',
    subtitle: '成分 / 质地 / 说明区',
    description: '适合美妆、护肤和清洁类商品，把成分和质地信息做成清晰的说明模块。',
    routeType: 'domestic',
    tags: ['淘宝', '美妆', '成分', '说明图'],
    previewTone: 'pink',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '卖点图',
      sellingPoints: '成分区清晰、质地说明位、信息更好读',
      size: '1024x1024',
      enableTextLayer: true,
      title: '成分与质地',
      subtitle: '只写已确认信息',
      points: ['成分区', '质地区', '说明区'],
      badge: '美妆'
    }
  },
  {
    id: 'img-cn-clothing-size-guide',
    previewImage: '/template-previews/img-cn-clothing-size-guide-real.jpg',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '尺码说明',
    title: '服饰尺码试穿图',
    subtitle: '尺码 / 版型 / 选购提示',
    description: '适合服饰、鞋帽和配件类商品，用尺码和版型信息降低退换风险。',
    routeType: 'domestic',
    tags: ['淘宝', '服饰', '尺码', '版型'],
    previewTone: 'blue',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '详情页模块',
      sellingPoints: '尺码表清楚、版型可读、试穿提示明确',
      size: '1024x1024',
      enableTextLayer: true,
      title: '尺码与版型',
      subtitle: '按真实尺寸填写',
      points: ['尺码表', '版型图', '试穿提示'],
      badge: '尺码'
    }
  },
  {
    id: 'img-cn-home-storage-before-after',
    previewImage: '/template-previews/img-cn-home-storage-before-after-real.jpg',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '小红书',
    category: '前后对比',
    title: '收纳前后对比图',
    subtitle: '前后 / 变化 / 真实场景',
    description: '适合收纳、整理、清洁和家居改造内容，用前后变化表达效果。',
    routeType: 'domestic',
    tags: ['小红书', '收纳', '对比', '变化'],
    previewTone: 'green',
    imagePreset: {
      platform: '小红书',
      usage: '对比图',
      sellingPoints: '前后变化明显、画面统一、真实好懂',
      size: '1024x1024',
      enableTextLayer: true,
      title: '变化一眼看懂',
      subtitle: '只表达真实对比',
      points: ['前后分屏', '变化对照', '真实结果'],
      badge: '整理'
    }
  },
  {
    id: 'img-cn-electronics-feature-grid',
    previewImage: '/template-previews/img-cn-electronics-feature-grid-real.jpg',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '功能图',
    title: '京东数码功能图',
    subtitle: '功能 / 参数 / 细节',
    description: '适合数码、家电和工具类商品，用功能网格承载参数和细节说明。',
    routeType: 'domestic',
    tags: ['京东', '数码', '功能', '参数'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '功能模块清楚、参数可读、细节可信',
      size: '1024x1024',
      enableTextLayer: true,
      title: '功能一屏看懂',
      subtitle: '参数只写真实信息',
      points: ['功能区', '参数区', '细节区'],
      badge: '数码'
    }
  },
  {
    id: 'img-cn-pet-lifestyle-scene',
    previewImage: '/template-previews/img-cn-pet-lifestyle-scene-real.jpg',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '小红书',
    category: '宠物生活',
    title: '宠物用品生活图',
    subtitle: '宠物 / 居家 / 使用感',
    description: '适合宠物用品、喂食、清洁和居家陪伴类商品，强化真实互动感。',
    routeType: 'domestic',
    tags: ['小红书', '宠物', '生活方式', '场景'],
    previewTone: 'gold',
    imagePreset: {
      platform: '小红书',
      usage: '场景图',
      sellingPoints: '宠物互动自然、居家感真实、主体清晰',
      size: '1024x1024',
      enableTextLayer: true,
      title: '宠物和家都适用',
      subtitle: '真实生活场景',
      points: ['宠物互动', '生活感', '清晰主体'],
      badge: '宠物'
    }
  },
  {
    id: 'img-overseas-amazon-listing',
    previewImage: '/template-previews/img-overseas-amazon-listing.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'Amazon',
    category: 'Listing 七图',
    title: 'Amazon Listing 七图',
    subtitle: '主图 / 场景 / 卖点 / 尺寸',
    description: '适合从商品信息开始生成 Amazon 文案和整套 Listing 图片，遵守站点语言和参数真实性。',
    routeType: 'listing',
    tags: ['Amazon', 'Listing', '七图', '合规'],
    previewTone: 'clean',
    listingPreset: {
      country: 'US',
      language: 'en-US',
      category: 'Please fill in product category',
      keywords: 'core keyword, product type, use case',
      sellingKeywords: 'main benefit, compatible model, target audience',
      descriptionKeywords: 'feature, specification, package included',
      description: 'Please fill in product material, dimensions, compatible models, package contents, target audience, usage scenarios, color and any confirmed constraints. Do not invent parameters.'
    }
  },
  {
    id: 'img-overseas-amazon-aplus-brand',
    previewImage: '/template-previews/img-overseas-amazon-aplus-brand.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'Amazon',
    category: 'A+ 图',
    title: 'Amazon A+ 品牌横幅',
    subtitle: '品牌故事 + 场景横幅',
    description: '适合品牌故事、卖点横幅和场景模块，给商品图、文案和安全留白位。',
    routeType: 'aplus',
    tags: ['Amazon', 'A+', '品牌故事', '横幅'],
    previewTone: 'blue',
    imagePreset: {
      aplusModule: 'brand-story-banner',
      aplusSize: '970x600',
      prompt: 'Create an Amazon A+ brand story banner. Show the product as the hero, use a clean premium ecommerce composition, leave safe space for short copy, keep all product parameters faithful to the provided information, avoid badges, platform UI, logos, fake certifications, and unsupported claims.'
    }
  },
  {
    id: 'img-overseas-amazon-aplus-comparison',
    previewImage: '/template-previews/img-overseas-amazon-aplus-comparison-real.jpg',
    kind: 'image',
    market: 'overseas',
    platform: 'Amazon',
    category: 'A+ 图',
    title: 'Amazon A+ 对比模块',
    subtitle: '为什么选我 / 对比证明位',
    description: '适合把产品和常见替代方案做并排对比，强化购买理由和差异化卖点。',
    routeType: 'aplus',
    tags: ['Amazon', 'A+', '对比', '模块'],
    previewTone: 'blue',
    imagePreset: {
      aplusModule: 'comparison-module',
      aplusSize: '970x600',
      prompt: 'Create an Amazon A+ comparison module. Show the product with a clean side-by-side comparison layout, clear benefit columns, generous whitespace for short copy, and a premium ecommerce feel. Keep all details faithful to the provided product information and avoid fake badges, certifications, or unsupported claims.'
    }
  },
  {
    id: 'img-overseas-amazon-aplus-faq',
    previewImage: '/template-previews/img-overseas-amazon-aplus-faq-real.jpg',
    kind: 'image',
    market: 'overseas',
    platform: 'Amazon',
    category: 'A+ 图',
    title: 'Amazon A+ FAQ 模块',
    subtitle: '兼容 / 安装 / 发货 / 售后',
    description: '适合承接购买前的高频疑问，减少犹豫并补齐信息说明位。',
    routeType: 'aplus',
    tags: ['Amazon', 'A+', 'FAQ', '保障'],
    previewTone: 'clean',
    imagePreset: {
      aplusModule: 'faq-module',
      aplusSize: '970x600',
      prompt: 'Create an Amazon A+ FAQ module. Arrange the product with clear question-and-answer blocks for compatibility, shipping, installation, and after-sales information. Keep the composition clean and trustworthy, with safe copy space and no platform UI, logos, badges, or unsupported claims.'
    }
  },
  {
    id: 'img-overseas-tiktok-shop-cover',
    previewImage: '/template-previews/img-overseas-tiktok-shop-cover.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '商品封面',
    title: 'TikTok Shop 商品封面',
    subtitle: '短视频货架 + 高停留封面',
    description: '适合 TikTok Shop 商品卡和短视频封面，强调竖版构图、使用场景和强视觉钩子。',
    routeType: 'creative',
    tags: ['TikTok Shop', '竖版', '封面', 'UGC'],
    previewTone: 'dark',
    imagePreset: {
      style: 'TikTok Shop high-retention ecommerce cover',
      size: '720x1280',
      prompt: 'Create a TikTok Shop product cover with a vertical short-video composition. Put the product in a clear hero position, use a lively creator-style scene, strong lighting, close-up details, and include only short readable in-image text if the user provides it. Do not add platform UI, watermarks, QR codes, fake discounts, or unverified claims.'
    }
  },
  {
    id: 'img-overseas-tiktok-shop-product-card',
    previewImage: '/template-previews/img-overseas-tiktok-shop-product-card.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '商品卡',
    title: 'TikTok Shop 商品卡',
    subtitle: '货架主视觉 + 商品卖点',
    description: '适合 TikTok Shop 商品卡和橱窗主图，商品清楚、卖点短、适合移动端快速扫读。',
    routeType: 'creative',
    tags: ['TikTok Shop', '商品卡', '主图', '海外'],
    previewTone: 'clean',
    sourceNote: '参考公开 TikTok Shop 商品内容工作流分类，PicPilot 重写为官方模板。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok Shop product card main image',
      size: '1024x1024',
      prompt: 'Create a TikTok Shop product card main image. Keep the referenced product accurate, centered, and easy to recognize on a mobile shopping feed. Use a clean ecommerce background, 2-3 short benefit zones only if the user provides text, and strong product clarity. Do not add platform UI, fake prices, fake discounts, ratings, badges, QR codes, watermarks, or unverified claims.'
    }
  },
  {
    id: 'img-overseas-tiktok-shop-pdp-gallery',
    previewImage: '/template-previews/img-overseas-tiktok-shop-pdp-gallery.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '详情图',
    title: 'TikTok Shop 详情卖点图',
    subtitle: 'PDP Gallery / Feature Detail',
    description: '适合商品详情页轮播图，突出材质、使用方式、包装或核心卖点，不编造参数。',
    routeType: 'creative',
    tags: ['TikTok Shop', 'PDP', '详情图', '卖点'],
    previewTone: 'blue',
    sourceNote: '参考公开 TikTok Shop PDP/UGC 商品内容结构，模板文案为 PicPilot 原创。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok Shop PDP gallery feature image',
      size: '1024x1024',
      prompt: 'Create a TikTok Shop product detail gallery image. Preserve the referenced product and show one clear feature, material detail, usage scenario, package detail, or size context based only on user-provided facts. Use clean ecommerce layout and short readable labels only when supplied. Do not invent specs, prices, discounts, certifications, ratings, platform UI, QR codes, or watermarks.'
    }
  },
  {
    id: 'img-overseas-tiktok-ugc-scene',
    previewImage: '/template-previews/img-overseas-tiktok-ugc-scene.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok',
    category: 'UGC 场景图',
    title: 'TikTok UGC 场景图',
    subtitle: '达人开箱 / 上手 / 使用瞬间',
    description: '适合海外短视频素材，画面像真实用户拍摄但保持商品质感和清晰度。',
    routeType: 'creative',
    tags: ['TikTok', 'UGC', '场景', '海外'],
    previewTone: 'purple',
    imagePreset: {
      style: 'authentic TikTok UGC lifestyle scene',
      size: '720x1280',
      prompt: 'Create an authentic TikTok UGC-style product scene. Show a creator holding or using the product naturally, with clear product shape, color, material, and scale. The frame should look like a polished short-video still, not a studio catalog image. Leave clean space for captions, and avoid platform UI or fake text.'
    }
  },
  {
    id: 'img-overseas-tiktok-shop-live-cover',
    previewImage: '/template-previews/img-overseas-tiktok-shop-live-cover.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '直播封面',
    title: 'TikTok Shop Live 封面',
    subtitle: '直播间入口 + 商品近景',
    description: '适合 TikTok Shop Live 入口图，突出主播视角、商品近景和清晰主题，不生成虚假折扣。',
    routeType: 'creative',
    tags: ['TikTok Shop', 'Live', '直播', '封面'],
    previewTone: 'dark',
    sourceNote: '参考 GitHub UGC ads / TikTok prompt 公开项目的场景分类，PicPilot 重写为官方模板。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok Shop live commerce cover',
      size: '720x1280',
      prompt: 'Create a vertical TikTok Shop Live cover. Show the referenced product as the hero in a creator/live-shopping setup with strong close-up framing, lively lighting, and a clear product demonstration mood. Render only short user-provided text if supplied. Do not add platform UI, fake sale badges, fake prices, watermarks, QR codes, or unverified claims.'
    }
  },
  {
    id: 'img-overseas-tiktok-unboxing-first-frame',
    previewImage: '/template-previews/img-overseas-tiktok-unboxing-first-frame.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok',
    category: '开箱首帧',
    title: 'TikTok 开箱首帧',
    subtitle: '手持开箱 + 停留感',
    description: '适合达人开箱、种草和 Spark Ads 素材首帧，画面像真实创作者拍摄但商品足够清楚。',
    routeType: 'creative',
    tags: ['TikTok', 'Unboxing', '首帧', 'UGC'],
    previewTone: 'purple',
    sourceNote: '参考 GitHub UGC/product-demo prompt 项目的工作流分类，模板文案为 PicPilot 原创。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok creator unboxing first frame',
      size: '720x1280',
      prompt: 'Create a TikTok creator-style unboxing first frame. Keep the referenced product accurate and recognizable, show hands or a creator naturally opening or revealing the product, use casual home-studio lighting, and leave clean caption space. Avoid platform UI, fake reviews, fake discounts, QR codes, watermarks, and unrelated props.'
    }
  },
  {
    id: 'img-overseas-tiktok-spark-ad-hook',
    previewImage: '/template-previews/img-overseas-tiktok-spark-ad-hook.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok Ads',
    category: '广告钩子图',
    title: 'TikTok Spark Ads 钩子图',
    subtitle: '痛点开场 + 商品解决方案',
    description: '适合 Spark Ads 和短视频广告首帧，用一个痛点场景承接商品，避免夸张功效和虚假证明。',
    routeType: 'creative',
    tags: ['TikTok Ads', 'Spark Ads', 'Hook', '广告'],
    previewTone: 'blue',
    sourceNote: '参考 GitHub short-form ads / UGC hook 分类思路，未复制第三方 prompt。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok Spark Ads hook frame',
      size: '720x1280',
      prompt: 'Create a TikTok Spark Ads hook frame. Start with one clear relatable problem scene and make the referenced product the obvious solution without exaggerating results. Use creator-native framing, high-retention composition, strong product clarity, and only exact user-provided text if any. No platform UI, fake statistics, fake testimonials, fake discounts, watermarks, or QR codes.'
    }
  },
  {
    id: 'img-overseas-tiktok-before-after-demo',
    previewImage: '/template-previews/img-overseas-tiktok-before-after-demo.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'TikTok',
    category: '演示对比图',
    title: 'TikTok 前后演示图',
    subtitle: 'Before / After / Demo',
    description: '适合清洁、收纳、美妆、工具类商品的短视频演示首帧，只表达用户提供的真实变化。',
    routeType: 'creative',
    tags: ['TikTok', 'Demo', 'Before After', '演示'],
    previewTone: 'green',
    sourceNote: '参考 GitHub product-demo / UGC ads 公开分类，PicPilot 重写为合规模板。',
    intent: 'poster_layout',
    textMode: 'ai_short_text',
    referencePolicy: 'preserve',
    imagePreset: {
      style: 'TikTok product demo before-after frame',
      size: '720x1280',
      prompt: 'Create a TikTok product demo before-and-after frame. Preserve the referenced product, show a clear demonstration setup with a simple before/after or step-by-step visual contrast, and keep claims tied to user-provided facts only. Use short readable user-provided text if supplied. Do not invent results, test data, prices, platform UI, QR codes, watermarks, or certifications.'
    }
  },
  {
    id: 'img-overseas-shopify-hero',
    previewImage: '/template-previews/img-overseas-shopify-hero.webp',
    kind: 'image',
    market: 'overseas',
    platform: 'Shopify/DTC',
    category: '官网 Hero',
    title: 'DTC 官网首屏图',
    subtitle: '品牌官网 Hero + 商品情绪价值',
    description: '适合独立站首屏和广告落地页，商品真实、品牌感强、预留标题按钮区域。',
    routeType: 'creative',
    tags: ['DTC', 'Shopify', 'Hero', '品牌'],
    previewTone: 'green',
    imagePreset: {
      style: 'premium DTC landing page hero',
      size: '1280x720',
      prompt: 'Create a premium DTC ecommerce hero image. Feature the product in a real lifestyle setting with brand-level lighting, strong visual hierarchy, and clean negative space for headline and CTA. Preserve the reference product exactly and avoid fake logos, unreadable text, or unsupported claims.'
    }
  },
  {
    id: 'img-overseas-shopify-trust-strip',
    previewImage: '/template-previews/img-overseas-shopify-trust-strip-real.jpg',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'Shopify/DTC',
    category: '信任条',
    title: 'Shopify 信任条模块',
    subtitle: 'shipping / returns / warranty',
    description: '适合独立站首屏下方的信任条和说明带，用于承接发货、退换、保修类信息。',
    routeType: 'creative',
    tags: ['Shopify', 'DTC', '信任', '保障'],
    previewTone: 'green',
    imagePreset: {
      style: 'clean Shopify trust bar ecommerce asset',
      size: '1280x240',
      prompt: 'Create a clean Shopify trust strip module for an ecommerce website. Use a horizontal layout with three or four trust blocks such as shipping, returns, warranty, and support. Keep the visual minimal, premium, and easy to read, with strong spacing and no fake logos or unsupported claims.'
    }
  },
  {
    id: 'img-cn-tmall-34-mobile-main',
    previewImage: '/template-previews/img-cn-tmall-34-mobile-main.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '竖版主图',
    title: '天猫 3:4 移动端主图',
    subtitle: '商品大主体 + 手机端强识别',
    description: '适合天猫、淘宝移动端货架和推荐流，主体占比更高，保留标题和短卖点文字层。',
    routeType: 'domestic',
    tags: ['天猫', '3:4', '移动端', '主图'],
    previewTone: 'clean',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '主图',
      sellingPoints: '手机端缩略图醒目、商品主体大、品牌店质感',
      size: '768x1024',
      enableTextLayer: true,
      title: '移动端主推',
      subtitle: '一屏突出核心卖点',
      points: ['主体更大', '竖版货架', '品牌质感'],
      badge: '主推'
    }
  },
  {
    id: 'img-cn-taobao-white-bg-fifth',
    previewImage: '/template-previews/img-cn-taobao-white-bg-fifth.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '白底图',
    title: '淘宝白底合规图',
    subtitle: '纯白背景 + 完整商品',
    description: '适合商品白底、第五张图和平台素材位，保持商品完整、干净、无文字干扰。',
    routeType: 'domestic',
    tags: ['白底图', '合规', '商品完整', '素材位'],
    previewTone: 'clean',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '主图',
      sellingPoints: '纯白背景、商品完整、边缘干净',
      size: '1024x1024',
      enableTextLayer: false,
      title: '',
      subtitle: '',
      points: ['纯白背景', '无文字', '商品完整']
    }
  },
  {
    id: 'img-cn-taobao-detail-scene',
    previewImage: '/template-previews/img-cn-taobao-detail-scene.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '详情页场景',
    title: '淘宝详情页使用场景',
    subtitle: '问题场景 + 卖点解释',
    description: '适合详情页模块，把商品放到真实使用场景里，后续用系统文字层解释卖点。',
    routeType: 'domestic',
    tags: ['详情页', '场景图', '使用痛点', '文字层'],
    previewTone: 'green',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '详情页模块',
      sellingPoints: '真实使用场景、问题解决、卖点解释位',
      size: '1280x720',
      enableTextLayer: true,
      title: '真实使用场景',
      subtitle: '把卖点放进生活里',
      points: ['场景明确', '卖点好讲', '详情页友好'],
      badge: '场景'
    }
  },
  {
    id: 'img-cn-jd-compare-panel',
    previewImage: '/template-previews/img-cn-jd-compare-panel.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '对比图',
    title: '京东对比参数图',
    subtitle: '理性蓝白 + 对比模块',
    description: '适合数码、家电、工具、耗材等商品，用对比区域表达真实参数和差异。',
    routeType: 'domestic',
    tags: ['京东', '对比', '参数', '理性决策'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '对比清楚、参数可信、适合理性决策',
      size: '1280x720',
      enableTextLayer: true,
      title: '对比一眼懂',
      subtitle: '只展示已确认参数',
      points: ['参数对比', '细节说明', '专业可信'],
      badge: '对比'
    }
  },
  {
    id: 'img-cn-jd-compatibility',
    previewImage: '/template-previews/img-cn-jd-compatibility.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '适配说明',
    title: '京东适配型号图',
    subtitle: '配件适配 + 型号矩阵',
    description: '适合数码配件、耗材和替换件，用商品、设备轮廓和型号矩阵说明适配范围。',
    routeType: 'domestic',
    tags: ['京东', '适配', '型号', '配件'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '适配范围清楚、型号区域可编辑、细节可信',
      size: '1280x720',
      enableTextLayer: true,
      title: '适配范围',
      subtitle: '型号请按真实信息填写',
      points: ['型号矩阵', '配件说明', '不编造适配'],
      badge: '适配'
    }
  },
  {
    id: 'img-cn-pdd-sku-grid',
    previewImage: '/template-previews/img-cn-pdd-sku-grid.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '拼多多',
    category: 'SKU 图',
    title: '拼多多多规格一屏看懂',
    subtitle: 'SKU 网格 + 短卖点',
    description: '适合多规格、多色、多组合商品，一屏展示款式差异，不自动生成未提供低价。',
    routeType: 'domestic',
    tags: ['拼多多', 'SKU', '多规格', '强转化'],
    previewTone: 'gold',
    imagePreset: {
      platform: '拼多多',
      usage: '主图',
      sellingPoints: '多规格展示、强对比、短句卖点',
      size: '1024x1024',
      enableTextLayer: true,
      title: '多规格可选',
      subtitle: '一屏看懂款式差异',
      points: ['规格清楚', '颜色直观', '列表醒目'],
      badge: '多选'
    }
  },
  {
    id: 'img-cn-douyin-short-cover',
    previewImage: '/template-previews/img-cn-douyin-short-cover.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '抖音电商',
    category: '短视频首帧',
    title: '抖音带货首帧图',
    subtitle: '近景冲击 + 3 秒停留',
    description: '适合短视频封面和首帧，强调上手、近景、灯光和可读标题空间。',
    routeType: 'domestic',
    tags: ['抖音', '首帧', '带货', '高停留'],
    previewTone: 'dark',
    imagePreset: {
      platform: '抖音电商',
      usage: '短视频首帧',
      sellingPoints: '近景冲击、上手展示、标题空间',
      size: '720x1280',
      enableTextLayer: true,
      title: '3 秒看懂',
      subtitle: '短视频首帧更抓人',
      points: ['近景', '上手', '强光影'],
      badge: '首帧'
    }
  },
  {
    id: 'img-cn-kuaishou-factory-cover',
    previewImage: '/template-previews/img-cn-kuaishou-factory-cover.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '快手电商',
    category: '信任封面',
    title: '快手源头好货封面',
    subtitle: '产地/仓库感 + 实在可信',
    description: '适合源头工厂、农货、日百和批量供货场景，突出真实、实在和货品规模感。',
    routeType: 'domestic',
    tags: ['快手', '源头', '信任', '封面'],
    previewTone: 'red',
    imagePreset: {
      platform: '快手电商',
      usage: '直播封面',
      sellingPoints: '源头氛围、货品规模、真实可信',
      size: '720x1280',
      enableTextLayer: true,
      title: '源头好货',
      subtitle: '真实货品看得见',
      points: ['实拍感', '货量感', '可信任'],
      badge: '源头'
    }
  },
  {
    id: 'img-overseas-amazon-main-white',
    previewImage: '/template-previews/img-overseas-amazon-main-white.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '白底主图',
    title: 'Amazon 白底主图',
    subtitle: '纯白背景 + 售卖物本身',
    description: '适合 Amazon 主图规范预览，突出商品主体，不加入文字、徽章、道具或水印。',
    routeType: 'creative',
    tags: ['Amazon', '主图', '白底', '合规'],
    previewTone: 'clean',
    imagePreset: {
      style: 'Amazon compliant white-background main image',
      size: '1024x1024',
      prompt: 'Create an Amazon-compliant main product image. Pure white background, product only, full product visible, centered, professional lighting, realistic soft shadow. No props, no text, no badges, no logos, no watermark.'
    }
  },
  {
    id: 'img-overseas-amazon-infographic',
    previewImage: '/template-previews/img-overseas-amazon-infographic.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '信息图副图',
    title: 'Amazon 信息图副图',
    subtitle: '商品特写 + 3 个卖点位',
    description: '适合 Listing 第二、第三张图，用商品特写和 callout 区域承载已确认卖点。',
    routeType: 'creative',
    tags: ['Amazon', '信息图', '副图', 'Callout'],
    previewTone: 'blue',
    imagePreset: {
      style: 'Amazon listing infographic secondary image',
      size: '1024x1024',
      prompt: 'Create an Amazon listing infographic-style secondary image. Show the product hero, detail close-ups, and three clean callout areas for short copy. Use only user-provided claims, avoid fake badges, unreadable text, platform UI, and unsupported certifications.'
    }
  },
  {
    id: 'img-overseas-amazon-dimension-scale',
    previewImage: '/template-previews/img-overseas-amazon-dimension-scale.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '尺寸比例图',
    title: 'Amazon 尺寸比例图',
    subtitle: '尺寸线 + 场景比例',
    description: '适合需要解释尺寸、容量和使用比例的商品，尺寸数字必须由用户后续填写。',
    routeType: 'creative',
    tags: ['Amazon', '尺寸', '比例', '合规'],
    previewTone: 'clean',
    imagePreset: {
      style: 'Amazon dimension and scale image',
      size: '1024x1024',
      prompt: 'Create a clean Amazon dimension and scale image. Show front and side product views, dimension-line placeholders, and a realistic scale reference. Do not invent measurements, numbers, or model names. No logos or watermarks.'
    }
  },
  {
    id: 'img-overseas-amazon-comparison-chart',
    previewImage: '/template-previews/img-overseas-amazon-comparison-chart.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '对比选购图',
    title: 'Amazon 对比选购图',
    subtitle: '本品 vs 普通方案',
    description: '适合解释为什么选择本品，避免出现竞品商标和未确认参数。',
    routeType: 'creative',
    tags: ['Amazon', '对比', '选购', '副图'],
    previewTone: 'purple',
    imagePreset: {
      style: 'Amazon comparison chart image',
      size: '1024x1024',
      prompt: 'Create an Amazon comparison chart visual. Compare the product with a generic alternative using clean table-like blocks and product imagery. Do not use competitor logos, fake numbers, or unsupported claims. Leave copy areas editable.'
    }
  },
  {
    id: 'img-overseas-walmart-main-clean',
    previewImage: '/template-previews/img-overseas-walmart-main-clean.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Walmart',
    category: '商品主图',
    title: 'Walmart 干净货架主图',
    subtitle: '专业目录感 + 轻灰背景',
    description: '适合 Walmart Marketplace 商品主图和目录图，干净、真实、少干扰。',
    routeType: 'creative',
    tags: ['Walmart', '主图', '目录', '海外'],
    previewTone: 'clean',
    imagePreset: {
      style: 'Walmart marketplace clean product image',
      size: '1024x1024',
      prompt: 'Create a clean Walmart Marketplace product image. Square white or very light gray background, centered product, professional catalog lighting, accurate product shape, no promotional text, no props, no watermark.'
    }
  },
  {
    id: 'img-overseas-ebay-resale-angle',
    previewImage: '/template-previews/img-overseas-ebay-resale-angle.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'eBay',
    category: '多角度图',
    title: 'eBay 多角度转售图',
    subtitle: '正侧细节 + 成色展示',
    description: '适合二手、翻新、收藏和复古商品，把正面、侧面和细节区域放在同一套风格中。',
    routeType: 'creative',
    tags: ['eBay', '二手', '多角度', '细节'],
    previewTone: 'dark',
    imagePreset: {
      style: 'eBay multi-angle resale product image',
      size: '1024x1024',
      prompt: 'Create a neutral eBay multi-angle product image. Show front, side, detail, and condition close-up areas in one clean composition. Keep it honest and realistic, no readable labels, no watermark, no fake condition claims.'
    }
  },
  {
    id: 'img-overseas-etsy-handmade-lifestyle',
    previewImage: '/template-previews/img-overseas-etsy-handmade-lifestyle.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Etsy',
    category: '手作场景',
    title: 'Etsy 手作生活方式图',
    subtitle: '自然光 + 手作质感',
    description: '适合手工、香氛、家居、礼物类商品，让画面更像真实小店高质量图片。',
    routeType: 'creative',
    tags: ['Etsy', '手作', '生活方式', '自然光'],
    previewTone: 'gold',
    imagePreset: {
      style: 'Etsy handmade lifestyle product image',
      size: '1024x1024',
      prompt: 'Create an Etsy handmade lifestyle product image. Use natural window light, handcrafted texture, cozy table setting, and authentic small-shop mood. Keep the product clear, no readable text, no logo, no watermark.'
    }
  },
  {
    id: 'img-overseas-shopify-pdp-gallery',
    previewImage: '/template-previews/img-overseas-shopify-pdp-gallery.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Shopify/DTC',
    category: 'PDP 图库',
    title: 'DTC 商品图库套图',
    subtitle: '白底 / 场景 / 细节 / 包装',
    description: '适合独立站商品详情页，把同一商品的多个图库视角整理成一致品牌风格。',
    routeType: 'creative',
    tags: ['Shopify', 'DTC', 'PDP', '图库'],
    previewTone: 'green',
    imagePreset: {
      style: 'DTC product detail page gallery set',
      size: '1024x1024',
      prompt: 'Create a DTC product gallery preview with four consistent mini scenes: white-background product, lifestyle scene, material detail, and packaging detail. Unified lighting and brand mood, no readable text or logos.'
    }
  },
  {
    id: 'img-free-studio-macro',
    previewImage: '/template-previews/img-free-studio-macro.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '棚拍微距',
    title: '棚拍质感微距',
    subtitle: '材质细节 + 高级布光',
    description: '适合美妆、香氛、食品、数码配件等商品，生成质感强的局部特写和氛围图。',
    routeType: 'creative',
    tags: ['自由创作', '棚拍', '微距', '质感'],
    previewTone: 'clean',
    imagePreset: {
      style: 'studio macro product photography',
      size: '1024x1024',
      prompt: 'Create a clean studio macro product photography scene. Emphasize premium material texture, controlled soft lighting, crisp detail, and commercial-safe composition. No readable text, no logos, no watermark.'
    }
  },
  {
    id: 'img-free-lifestyle-home',
    previewImage: '/template-previews/img-free-lifestyle-home.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '生活方式',
    title: '居家生活方式图',
    subtitle: '自然光 + 真实使用感',
    description: '适合家居、杯壶、收纳、日用品，把商品放到温暖真实的生活空间中。',
    routeType: 'creative',
    tags: ['自由创作', '家居', '生活方式', '自然光'],
    previewTone: 'green',
    imagePreset: {
      style: 'cozy home lifestyle product scene',
      size: '1024x1024',
      prompt: 'Create a cozy home lifestyle product scene. Show the product naturally in use near a bright window, with realistic shadows, calm brand mood, and clean negative space. No readable text, no logo, no watermark.'
    }
  },
  {
    id: 'img-free-fashion-model',
    previewImage: '/template-previews/img-free-fashion-model.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '服饰模特',
    title: '服饰配件模特图',
    subtitle: '成人模特 + 自然上身',
    description: '适合服饰、包袋、帽子、饰品等创意图，强调成人模特和商品可见度。',
    routeType: 'creative',
    tags: ['自由创作', '服饰', '模特', '穿搭'],
    previewTone: 'dark',
    imagePreset: {
      style: 'commercial fashion accessory image',
      size: '1024x1024',
      prompt: 'Create a commercial fashion product image with an adult model wearing or holding the product naturally. Keep the product clearly visible, preserve color and texture, no readable text, no logo, no watermark.'
    }
  },
  {
    id: 'img-free-holiday-gift',
    previewImage: '/template-previews/img-free-holiday-gift.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '节日礼物',
    title: '节日礼物商品海报',
    subtitle: '季节氛围 + 留白区',
    description: '适合圣诞、新年、情人节、母亲节等节日图，背景有氛围但不抢商品主体。',
    routeType: 'creative',
    tags: ['自由创作', '节日', '礼物', '海报'],
    previewTone: 'red',
    imagePreset: {
      style: 'seasonal ecommerce campaign image',
      size: '1024x1024',
      prompt: 'Create a seasonal ecommerce campaign image. Put the product at the center, use tasteful holiday atmosphere, and leave safe space for title and CTA overlays. No readable text, no logo, no fake discounts.'
    }
  },
  {
    id: 'img-free-tech-launch',
    previewImage: '/template-previews/img-free-tech-launch.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '科技广告',
    title: '科技新品发布图',
    subtitle: '暗色质感 + 光效层次',
    description: '适合数码、智能硬件和电子配件，生成带发布感的广告视觉。',
    routeType: 'creative',
    tags: ['自由创作', '科技', '广告', '新品'],
    previewTone: 'dark',
    imagePreset: {
      style: 'futuristic tech product launch visual',
      size: '1024x1024',
      prompt: 'Create a futuristic tech product launch visual. Use precise premium lighting, subtle light streaks, clean negative space, and a dramatic reflective surface. No readable text, no logos, no watermark.'
    }
  },
  {
    id: 'img-free-social-flatlay',
    previewImage: '/template-previews/img-free-social-flatlay.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '社媒俯拍',
    title: '社媒 Flat Lay 构图',
    subtitle: '俯拍陈列 + 创作者感',
    description: '适合小红书、Pinterest、Instagram 风格内容，商品和道具平衡陈列。',
    routeType: 'creative',
    tags: ['自由创作', 'Flat Lay', '社媒', '俯拍'],
    previewTone: 'pink',
    imagePreset: {
      style: 'social media flat lay product composition',
      size: '1024x1024',
      prompt: 'Create a social media flat lay product composition. Arrange the main product with tasteful props on a soft background, balanced square composition, polished creator style, no readable text or logos.'
    }
  },
  {
    id: 'img-free-packaging-mockup',
    previewImage: '/template-previews/img-free-packaging-mockup.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '包装展示',
    title: '包装盒与瓶身展示',
    subtitle: '包装组合 + 材质真实',
    description: '适合美妆、食品、香氛、保健品等包装展示，生成盒、瓶、标签同框的高质感预览。',
    routeType: 'creative',
    tags: ['自由创作', '包装', 'Mockup', '品牌'],
    previewTone: 'gold',
    imagePreset: {
      style: 'premium packaging mockup',
      size: '1024x1024',
      prompt: 'Create a premium packaging mockup with a box and product container together. Front-facing packaging, realistic paper and glass materials, clean studio background. No readable brand text, no logo, no watermark.'
    }
  },
  {
    id: 'img-free-exploded-feature-view',
    previewImage: '/template-previews/img-free-exploded-feature-view.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '结构说明',
    title: '产品结构爆炸图',
    subtitle: '部件层级 + 功能说明位',
    description: '适合数码、家电、户外装备和工具类商品，展示结构感但不编造内部参数。',
    routeType: 'creative',
    tags: ['自由创作', '结构', '爆炸图', '功能'],
    previewTone: 'blue',
    imagePreset: {
      style: 'semi-realistic exploded feature view',
      size: '1024x1024',
      prompt: 'Create a semi-realistic exploded feature view of a product. Show floating component layers, clean technical lighting, and editable callout areas. Do not invent labels, numbers, or internal claims.'
    }
  },
  {
    id: 'img-cn-taobao-detail-module',
    previewImage: '/template-previews/img-cn-taobao-detail-module.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '详情页模块',
    title: '淘宝详情页模块图',
    subtitle: '首屏标题 + 卖点分栏',
    description: '适合淘宝天猫详情页首屏和模块图，用更清晰的分栏方式表达标题、卖点和场景。',
    routeType: 'domestic',
    tags: ['淘宝', '详情页', '模块图', '卖点分栏'],
    previewTone: 'gold',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '详情页模块',
      sellingPoints: '模块分区清楚、卖点层级明确、适合移动端浏览',
      size: '1280x720',
      enableTextLayer: true,
      title: '详情页核心模块',
      subtitle: '让卖点更好读',
      points: ['标题分区', '卖点卡片', '场景说明'],
      badge: '模块'
    }
  },
  {
    id: 'img-cn-taobao-ingredient-panel',
    previewImage: '/template-previews/img-cn-taobao-ingredient-panel.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '成分说明',
    title: '淘宝成分说明图',
    subtitle: '成分 / 材质 / 说明位',
    description: '适合美妆、食品、家居清洁和日百商品，把成分和材质信息做成可编辑说明区。',
    routeType: 'domestic',
    tags: ['淘宝', '成分', '材质', '说明图'],
    previewTone: 'clean',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '卖点图',
      sellingPoints: '成分信息清晰、说明位充足、视觉干净',
      size: '1024x1024',
      enableTextLayer: true,
      title: '材质与成分',
      subtitle: '只写已确认信息',
      points: ['成分区', '材质区', '注意事项'],
      badge: '说明'
    }
  },
  {
    id: 'img-cn-taobao-package-showcase',
    previewImage: '/template-previews/img-cn-taobao-package-showcase.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '包装展示',
    title: '淘宝包装展示图',
    subtitle: '盒装 / 套装 / 开箱感',
    description: '适合礼盒、套装、组合装和包装卖点展示，强调商品与包装的整体质感。',
    routeType: 'domestic',
    tags: ['淘宝', '包装', '套装', '开箱'],
    previewTone: 'pink',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '场景图',
      sellingPoints: '包装层次丰富、适合礼赠、组合关系清晰',
      size: '1024x1024',
      enableTextLayer: true,
      title: '包装与内容物',
      subtitle: '组合装也一眼看懂',
      points: ['礼盒感', '套装感', '开箱感'],
      badge: '包装'
    }
  },
  {
    id: 'img-cn-jd-unbox-compare',
    previewImage: '/template-previews/img-cn-jd-unbox-compare.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '开箱对比',
    title: '京东开箱对比图',
    subtitle: '包装 / 主体 / 配件一屏看懂',
    description: '适合数码、家电、工具和配件商品，用开箱和对比结构表达真实配置。',
    routeType: 'domestic',
    tags: ['京东', '开箱', '对比', '配件'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '开箱层次清楚、配置一屏可见、理性可信',
      size: '1280x720',
      enableTextLayer: true,
      title: '开箱即见配置',
      subtitle: '主体和配件都清楚',
      points: ['开箱结构', '配件说明', '对比展示'],
      badge: '开箱'
    }
  },
  {
    id: 'img-cn-jd-spec-grid',
    previewImage: '/template-previews/img-cn-jd-spec-grid.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '参数网格',
    title: '京东参数网格图',
    subtitle: '型号 / 规格 / 参数卡片',
    description: '适合多参数、多规格和配置型商品，用网格排版提高理性决策效率。',
    routeType: 'domestic',
    tags: ['京东', '参数', '网格', '配置'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '卖点图',
      sellingPoints: '参数排版规整、型号清晰、便于对照',
      size: '1280x720',
      enableTextLayer: true,
      title: '参数一屏对照',
      subtitle: '请按真实规格填写',
      points: ['型号卡', '参数卡', '对照区'],
      badge: '参数'
    }
  },
  {
    id: 'img-cn-pdd-combo-grid',
    previewImage: '/template-previews/img-cn-pdd-combo-grid.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '拼多多',
    category: '组合装',
    title: '拼多多组合装图',
    subtitle: '多件组合 + 清楚识别',
    description: '适合组合装、家庭装和多规格商品，强调套餐关系和一眼看懂的购买理由。',
    routeType: 'domestic',
    tags: ['拼多多', '组合装', '多件装', '套组'],
    previewTone: 'gold',
    imagePreset: {
      platform: '拼多多',
      usage: '主图',
      sellingPoints: '组合关系明确、主体醒目、缩略图友好',
      size: '1024x1024',
      enableTextLayer: true,
      title: '组合装更划算',
      subtitle: '按真实套装信息展示',
      points: ['套装关系', '组合标识', '主体放大'],
      badge: '套组'
    }
  },
  {
    id: 'img-cn-pdd-benefit-banner',
    previewImage: '/template-previews/img-cn-pdd-benefit-banner.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '拼多多',
    category: '利益点横幅',
    title: '拼多多利益点横幅',
    subtitle: '直给利益点 + 大字区',
    description: '适合拼多多货架和活动主图，强化利益点表达但不生成未确认价格与补贴。',
    routeType: 'domestic',
    tags: ['拼多多', '利益点', '横幅', '大字图'],
    previewTone: 'red',
    imagePreset: {
      platform: '拼多多',
      usage: '活动图',
      sellingPoints: '利益点直接、视觉醒目、适合列表页',
      size: '1280x720',
      enableTextLayer: true,
      title: '直给核心利益点',
      subtitle: '信息集中更好扫读',
      points: ['大标题', '卖点短句', '活动留白'],
      badge: '热销'
    }
  },
  {
    id: 'img-cn-douyin-hook-card',
    previewImage: '/template-previews/img-cn-douyin-hook-card.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '抖音电商',
    category: '开头钩子图',
    title: '抖音开头钩子图',
    subtitle: '强钩子 / 强情绪 / 强主体',
    description: '适合短视频前 3 秒封面和首帧，用强钩子表达问题、结果或场景冲突。',
    routeType: 'domestic',
    tags: ['抖音', '钩子', '首帧', '情绪'],
    previewTone: 'dark',
    imagePreset: {
      platform: '抖音电商',
      usage: '短视频首帧',
      sellingPoints: '开头冲击力强、适合视频停帧、文字可读',
      size: '720x1280',
      enableTextLayer: true,
      title: '先抓住停留',
      subtitle: '钩子表达更直接',
      points: ['问题感', '结果感', '动作感'],
      badge: '钩子'
    }
  },
  {
    id: 'img-cn-douyin-demo-closeup',
    previewImage: '/template-previews/img-cn-douyin-demo-closeup.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '抖音电商',
    category: '使用演示',
    title: '抖音使用演示图',
    subtitle: '上手 / 细节 / 结果展示',
    description: '适合抖音电商短视频和直播间素材，突出商品实际使用过程和效果展示位。',
    routeType: 'domestic',
    tags: ['抖音', '演示', '上手', '细节'],
    previewTone: 'red',
    imagePreset: {
      platform: '抖音电商',
      usage: '卖点图',
      sellingPoints: '上手动作明确、细节可见、结果感强',
      size: '720x1280',
      enableTextLayer: true,
      title: '上手就能懂',
      subtitle: '把演示做得更直观',
      points: ['演示动作', '局部特写', '结果区'],
      badge: '演示'
    }
  },
  {
    id: 'img-cn-kuaishou-source-floor',
    previewImage: '/template-previews/img-cn-kuaishou-source-floor.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '快手电商',
    category: '源头货架',
    title: '快手源头货架图',
    subtitle: '仓库 / 货量 / 实在感',
    description: '适合源头工厂、仓储出货和批量供货场景，表达真实货量和可信氛围。',
    routeType: 'domestic',
    tags: ['快手', '源头', '仓库', '货架'],
    previewTone: 'green',
    imagePreset: {
      platform: '快手电商',
      usage: '直播封面',
      sellingPoints: '源头真实、货量感足、适合信任转化',
      size: '720x1280',
      enableTextLayer: true,
      title: '源头货真价实',
      subtitle: '真实仓储场景更可信',
      points: ['仓库感', '货量感', '批发感'],
      badge: '源头'
    }
  },
  {
    id: 'img-cn-xhs-review-banner',
    previewImage: '/template-previews/img-cn-xhs-review-banner.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '小红书',
    category: '评测封面',
    title: '小红书评测封面图',
    subtitle: '真实体验 / 轻评测 / 封面感',
    description: '适合真实测评、使用体验和开箱笔记，画面更像可收藏的评测封面。',
    routeType: 'domestic',
    tags: ['小红书', '评测', '封面', '体验'],
    previewTone: 'pink',
    imagePreset: {
      platform: '小红书',
      usage: '小红书种草图',
      sellingPoints: '体验感真实、标题位清楚、适合封面',
      size: '768x1024',
      enableTextLayer: true,
      title: '真实测评来了',
      subtitle: '把体验感做出来',
      points: ['真实评价', '标题位', '收藏感'],
      badge: '评测'
    }
  },
  {
    id: 'img-cn-xhs-before-after',
    previewImage: '/template-previews/img-cn-xhs-before-after.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '小红书',
    category: '前后对比',
    title: '小红书前后对比封面',
    subtitle: '变化感 / 对比感 / 易收藏',
    description: '适合清洁、收纳、美妆和家居改造类内容，用前后对比表达变化。',
    routeType: 'domestic',
    tags: ['小红书', '前后对比', '变化', '改造'],
    previewTone: 'green',
    imagePreset: {
      platform: '小红书',
      usage: '对比图',
      sellingPoints: '前后变化明显、画面统一、信息好读',
      size: '768x1024',
      enableTextLayer: true,
      title: '变化一眼看懂',
      subtitle: '只表达真实对比',
      points: ['前后分屏', '变化对照', '真实结果'],
      badge: '对比'
    }
  },
  {
    id: 'img-cn-tmall-size-guide',
    previewImage: '/template-previews/img-cn-tmall-size-guide.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '尺寸指南',
    title: '天猫尺寸指南图',
    subtitle: '尺码 / 规格 / 选购提醒',
    description: '适合服饰、收纳、家具和配件类商品，帮助用户理解尺寸关系和购买范围。',
    routeType: 'domestic',
    tags: ['天猫', '尺寸', '指南', '规格'],
    previewTone: 'clean',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '详情页模块',
      sellingPoints: '尺寸关系清楚、选购更安心、信息层级明确',
      size: '768x1024',
      enableTextLayer: true,
      title: '尺寸选购指南',
      subtitle: '请按真实尺寸填写',
      points: ['尺码表', '比例图', '提醒区'],
      badge: '尺寸'
    }
  },
  {
    id: 'img-cn-taobao-lifestyle-lens',
    previewImage: '/template-previews/img-cn-taobao-lifestyle-lens.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '淘宝/天猫',
    category: '生活方式图',
    title: '淘宝生活方式图',
    subtitle: '品牌感 / 使用感 / 情绪价值',
    description: '适合家居、礼物、日用品和审美型商品，画面更像品牌店铺的生活方式主图。',
    routeType: 'domestic',
    tags: ['淘宝', '生活方式', '品牌感', '主图'],
    previewTone: 'pink',
    imagePreset: {
      platform: '淘宝/天猫',
      usage: '场景图',
      sellingPoints: '生活感自然、品牌调性强、适合首屏',
      size: '1024x1024',
      enableTextLayer: true,
      title: '把商品放进生活里',
      subtitle: '品牌调性更统一',
      points: ['生活场景', '情绪价值', '品牌感'],
      badge: '生活'
    }
  },
  {
    id: 'img-cn-jd-install-step',
    previewImage: '/template-previews/img-cn-jd-install-step.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '京东',
    category: '安装步骤',
    title: '京东安装步骤图',
    subtitle: '步骤清晰 / 适配说明 / 使用安心',
    description: '适合家电、工具、收纳和装配型商品，用步骤分镜表达安装、组合和使用流程。',
    routeType: 'domestic',
    tags: ['京东', '安装', '步骤', '说明图'],
    previewTone: 'blue',
    imagePreset: {
      platform: '京东',
      usage: '详情页模块',
      sellingPoints: '步骤分明、操作直观、降低理解成本',
      size: '1280x720',
      enableTextLayer: true,
      title: '安装步骤一眼看懂',
      subtitle: '让使用门槛更低',
      points: ['步骤 1-3', '部件说明', '安装提示'],
      badge: '步骤'
    }
  },
  {
    id: 'img-cn-1688-source-factory',
    previewImage: '/template-previews/img-cn-1688-source-factory.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '源头工厂',
    title: '1688 源头工厂图',
    subtitle: '工厂实力 / 现货感 / 采购信任',
    description: '适合工厂店、源头厂家和供应链型商品，突出真实货源、生产/仓储氛围和采购可信度。',
    routeType: 'domestic',
    tags: ['1688', '源头工厂', 'B2B', '采购'],
    previewTone: 'blue',
    sourceNote: '参考公开 1688 采集/商品分析项目的货源字段思路，重写为 PicPilot 官方模板，不复制第三方素材。',
    imagePreset: {
      platform: '1688',
      usage: '主图',
      sellingPoints: '源头工厂、现货供应、批量采购、货源稳定',
      size: '1024x1024',
      enableTextLayer: true,
      title: '源头工厂 现货供应',
      subtitle: '适合批量采购询盘',
      points: ['工厂实拍感', '货源稳定', '支持拿样'],
      badge: '源头'
    }
  },
  {
    id: 'img-cn-1688-wholesale-main',
    previewImage: '/template-previews/img-cn-1688-wholesale-main.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '批发主图',
    title: '1688 批发主图',
    subtitle: '主体清楚 / 多件陈列 / 快速询盘',
    description: '适合批发商品首图，用清楚主体、多件陈列和采购卖点帮助买家快速判断是否询盘。',
    routeType: 'domestic',
    tags: ['1688', '批发', '主图', '询盘'],
    previewTone: 'clean',
    imagePreset: {
      platform: '1688',
      usage: '主图',
      sellingPoints: '主体清晰、多件陈列、适合采购商快速判断',
      size: '1024x1024',
      enableTextLayer: true,
      title: '批发采购看这里',
      subtitle: '主体清楚 货品齐全',
      points: ['多件展示', '现货感强', '询盘友好'],
      badge: '批发'
    }
  },
  {
    id: 'img-cn-1688-sku-matrix',
    previewImage: '/template-previews/img-cn-1688-sku-matrix.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '货盘多款',
    title: '1688 多款货盘图',
    subtitle: '款式齐全 / SKU 分区 / 采购对比',
    description: '适合多颜色、多规格、多款式商品，用整齐货盘陈列体现可选范围和批量采购效率。',
    routeType: 'domestic',
    tags: ['1688', '货盘', 'SKU', '多款'],
    previewTone: 'green',
    imagePreset: {
      platform: '1688',
      usage: '卖点图',
      sellingPoints: '款式齐全、规格分区、采购对比清楚',
      size: '1280x720',
      enableTextLayer: true,
      title: '多款货盘 一图看清',
      subtitle: '颜色/规格请按实填写',
      points: ['款式分区', '规格清楚', '批量选款'],
      badge: '货盘'
    }
  },
  {
    id: 'img-cn-1688-moq-tier',
    previewImage: '/template-previews/img-cn-1688-moq-tier.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '阶梯采购',
    title: '1688 阶梯采购图',
    subtitle: '拿样 / 起订 / 批量区间',
    description: '适合需要说明拿样、起订和批量采购逻辑的商品，只保留文字位，不自动编造价格和起订量。',
    routeType: 'domestic',
    tags: ['1688', '阶梯价', 'MOQ', '拿样'],
    previewTone: 'gold',
    imagePreset: {
      platform: '1688',
      usage: '详情页模块',
      sellingPoints: '拿样说明、起订提示、批量采购路径清楚',
      size: '1280x720',
      enableTextLayer: true,
      title: '拿样到批量采购',
      subtitle: '价格/起订量按真实信息填写',
      points: ['拿样参考', '批量区间', '询盘沟通'],
      priceText: '填写真实阶梯信息',
      badge: '采购'
    }
  },
  {
    id: 'img-cn-1688-spec-detail',
    previewImage: '/template-previews/img-cn-1688-spec-detail.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '参数详情',
    title: '1688 参数详情图',
    subtitle: '规格 / 材质 / 包装 / 细节',
    description: '适合详情页参数模块，把材质、尺寸、包装、适配等真实信息放进清晰分区，降低采购沟通成本。',
    routeType: 'domestic',
    tags: ['1688', '参数', '详情页', '材质'],
    previewTone: 'blue',
    imagePreset: {
      platform: '1688',
      usage: '详情页模块',
      sellingPoints: '参数清楚、细节放大、采购信息完整',
      size: '1280x720',
      enableTextLayer: true,
      title: '参数细节清楚展示',
      subtitle: '只填写真实规格',
      points: ['材质细节', '尺寸信息', '包装说明'],
      badge: '参数'
    }
  },
  {
    id: 'img-cn-1688-pack-ship',
    previewImage: '/template-previews/img-cn-1688-pack-ship.webp',
    kind: 'image',
    market: 'domestic',
    imageGroup: 'domestic',
    platform: '1688',
    category: '包装发货',
    title: '1688 包装发货图',
    subtitle: '仓储 / 打包 / 发货流程',
    description: '适合工厂、档口和仓储型商品，用打包、箱规和发货流程建立采购信任，不生成未确认承诺。',
    routeType: 'domestic',
    tags: ['1688', '包装', '发货', '仓储'],
    previewTone: 'dark',
    imagePreset: {
      platform: '1688',
      usage: '详情页模块',
      sellingPoints: '打包流程、仓储现货、发货信息承载位',
      size: '1280x720',
      enableTextLayer: true,
      title: '包装发货流程清楚',
      subtitle: '箱规/时效按真实信息填写',
      points: ['仓储现货', '打包展示', '发货流程'],
      badge: '发货'
    }
  },
  {
    id: 'img-overseas-amazon-listing-7-grid',
    previewImage: '/template-previews/img-overseas-amazon-listing-7-grid.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: 'Listing 七图',
    title: 'Amazon 七图强化版',
    subtitle: '主图 / 场景 / 卖点 / 比例',
    description: '适合更完整的 Amazon Listing 套图，强调主图、场景图、卖点图和尺寸说明的统一风格。',
    routeType: 'listing',
    tags: ['Amazon', 'Listing', '七图', '完整套组'],
    previewTone: 'clean',
    listingPreset: {
      country: 'US',
      language: 'en-US',
      category: 'Please fill in product category',
      keywords: 'core keyword, product type, use case, material',
      sellingKeywords: 'main benefit, compatibility, feature, audience',
      descriptionKeywords: 'feature, specification, package included, use case',
      material: 'Please fill in verified material',
      dimensions: 'Please fill in verified dimensions',
      description: 'Please fill in product facts, material, dimensions, package contents, compatibility, and usage scenarios. Do not invent numbers, certifications, or guarantees.'
    }
  },
  {
    id: 'img-overseas-amazon-listing-bundle',
    previewImage: '/template-previews/img-overseas-amazon-listing-bundle.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: 'Listing 套组',
    title: 'Amazon Listing 套组',
    subtitle: '变体 / 配件 / 组合展示',
    description: '适合带变体和配件的 Listing 套组，帮助前置梳理组合关系与关键词。',
    routeType: 'listing',
    tags: ['Amazon', 'Listing', '套组', '变体'],
    previewTone: 'purple',
    listingPreset: {
      country: 'US',
      language: 'en-US',
      category: 'Please fill in product category',
      keywords: 'bundle, variant, product type, use case',
      sellingKeywords: 'bundle value, compatibility, included items, audience',
      descriptionKeywords: 'bundle, component, specification, package included',
      material: 'Please fill in verified material',
      dimensions: 'Please fill in verified dimensions',
      description: 'Please provide bundle structure, variant differences, package contents, and confirmed specs. Keep all claims factual and avoid unsupported comparison language.',
      includeAplus: true,
      aplusModule: 'key-feature-module',
      aplusPrompt: 'Create an Amazon A+ key feature module for a bundle listing. Use the provided facts only, show the main bundle and component hierarchy clearly, and leave editable copy space.'
    }
  },
  {
    id: 'img-overseas-amazon-aplus-feature',
    previewImage: '/template-previews/img-overseas-amazon-aplus-feature.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: 'A+ 图',
    title: 'Amazon A+ 核心卖点模块',
    subtitle: '特性分区 + 可编辑文案位',
    description: '适合核心卖点模块，突出功能、材料和使用场景的组合表达。',
    routeType: 'aplus',
    tags: ['Amazon', 'A+', '卖点', '模块'],
    previewTone: 'blue',
    imagePreset: {
      aplusModule: 'key-feature-module',
      aplusSize: '970x600',
      prompt: 'Create an Amazon A+ key feature module. Show one clear hero product and three supporting feature zones with editable copy space. Keep all claims factual, avoid platform UI, fake seals, unsupported certifications, and any invented numbers.'
    }
  },
  {
    id: 'img-overseas-amazon-aplus-lifestyle',
    previewImage: '/template-previews/img-overseas-amazon-aplus-lifestyle.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: 'A+ 图',
    title: 'Amazon A+ 场景模块',
    subtitle: '品牌感 / 生活方式 / 叙事',
    description: '适合生活方式模块和品牌故事延展，用场景表达品牌气质和使用感受。',
    routeType: 'aplus',
    tags: ['Amazon', 'A+', '场景', '品牌'],
    previewTone: 'green',
    imagePreset: {
      aplusModule: 'lifestyle-scene-module',
      aplusSize: '970x600',
      prompt: 'Create an Amazon A+ lifestyle scene module. Present the product in a premium real-world setting with clean copy-safe areas, consistent brand mood, and no exaggerated claims. Avoid logos, badges, platform UI, and unreadable text.'
    }
  },
  {
    id: 'img-overseas-amazon-main-angle',
    previewImage: '/template-previews/img-overseas-amazon-main-angle.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '主图角度',
    title: 'Amazon 主图角度图',
    subtitle: '主图 / 侧角 / 清晰轮廓',
    description: '适合展示产品主体角度与结构感，方便后续进入 Listing 或 DTC 视觉流程。',
    routeType: 'creative',
    tags: ['Amazon', '主图', '角度', '海外'],
    previewTone: 'clean',
    imagePreset: {
      style: 'Amazon compliant main angle product photo',
      size: '1024x1024',
      prompt: 'Create a clean Amazon-style main angle product photo with a white or near-white background, clear product silhouette, accurate color and material, and subtle realistic shadow. No readable text, logos, props, badges, or watermark.'
    }
  },
  {
    id: 'img-overseas-amazon-packaging-lineup',
    previewImage: '/template-previews/img-overseas-amazon-packaging-lineup.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Amazon',
    category: '包装陈列',
    title: 'Amazon 包装陈列图',
    subtitle: '包装 / 配件 / 套装',
    description: '适合展示包装内容、配件组合和套装结构，强调清晰的购买预期。',
    routeType: 'creative',
    tags: ['Amazon', '包装', '配件', '套装'],
    previewTone: 'purple',
    imagePreset: {
      style: 'Amazon packaging and accessories lineup',
      size: '1024x1024',
      prompt: 'Create an Amazon product packaging lineup image. Show the main product, packaging, and included accessories in a clean commercial layout. Preserve factual packaging contents, keep copy areas editable, and avoid invented badges or unsupported claims.'
    }
  },
  {
    id: 'img-overseas-walmart-pdp-grid',
    previewImage: '/template-previews/img-overseas-walmart-pdp-grid.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Walmart',
    category: 'PDP 图',
    title: 'Walmart PDP 网格图',
    subtitle: '目录感 / 信息块 / 产品清晰',
    description: '适合 Walmart PDP 详情图和目录图，用清晰网格承载卖点和细节。',
    routeType: 'creative',
    tags: ['Walmart', 'PDP', '网格', '目录'],
    previewTone: 'clean',
    imagePreset: {
      style: 'Walmart PDP grid product image',
      size: '1024x1024',
      prompt: 'Create a Walmart PDP grid-style product image. Use a clean catalog look with the product centered, supporting detail zones, and clear information blocks. No logos, no watermark, no platform UI, and no invented claims.'
    }
  },
  {
    id: 'img-overseas-ebay-condition-card',
    previewImage: '/template-previews/img-overseas-ebay-condition-card.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'eBay',
    category: '成色卡',
    title: 'eBay 成色说明图',
    subtitle: '状态 / 细节 / 真实感',
    description: '适合二手、翻新和收藏品展示，把成色和细节说明得更直观。',
    routeType: 'creative',
    tags: ['eBay', '成色', '二手', '细节'],
    previewTone: 'dark',
    imagePreset: {
      style: 'eBay condition showcase product image',
      size: '1024x1024',
      prompt: 'Create an honest eBay condition showcase image. Show the product from a few angles with visible wear or detail areas if appropriate, while keeping the product accurate and well lit. No fake grading labels, no unreadable text, no watermark.'
    }
  },
  {
    id: 'img-overseas-shopify-dtc-hero',
    previewImage: '/template-previews/img-overseas-shopify-dtc-hero.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'overseas',
    platform: 'Shopify/DTC',
    category: '首屏 Hero',
    title: 'DTC 首屏 Hero 图',
    subtitle: '品牌感 / 留白 / 转化位',
    description: '适合 Shopify 和 DTC 首页首屏，强调品牌感和文案留白空间。',
    routeType: 'creative',
    tags: ['DTC', 'Shopify', 'Hero', '品牌'],
    previewTone: 'green',
    imagePreset: {
      style: 'premium DTC ecommerce hero image',
      size: '1280x720',
      prompt: 'Create a premium DTC ecommerce hero image. Feature the product in a real lifestyle setting with strong brand-level lighting, generous negative space for headline and CTA, and a polished commercial mood. Avoid logos, watermarks, and unsupported claims.'
    }
  },
  {
    id: 'img-free-studio-glass',
    previewImage: '/template-previews/img-free-studio-glass.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '玻璃质感',
    title: '棚拍玻璃质感图',
    subtitle: '反光控制 / 透明材质 / 质感',
    description: '适合玻璃、液体、香氛和高反光材质商品，突出材质表现和商业灯光。',
    routeType: 'creative',
    tags: ['自由创作', '棚拍', '玻璃', '质感'],
    previewTone: 'clean',
    imagePreset: {
      style: 'premium studio glass product photography',
      size: '1024x1024',
      prompt: 'Create a premium studio product image featuring glass or transparent materials. Control reflections, show realistic highlights, and keep the composition clean and commercial. No readable text, logos, or watermark.'
    }
  },
  {
    id: 'img-free-material-detail',
    previewImage: '/template-previews/img-free-material-detail.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '材质特写',
    title: '材质细节特写图',
    subtitle: '纹理 / 表面 / 近景',
    description: '适合材质、表面和局部纹理表达，让商品更有真实的触感和品质感。',
    routeType: 'creative',
    tags: ['自由创作', '材质', '特写', '质感'],
    previewTone: 'gold',
    imagePreset: {
      style: 'macro material detail product photo',
      size: '1024x1024',
      prompt: 'Create a macro material detail product photo. Emphasize surface texture, edge detail, and premium close-up lighting. Keep the product factual and avoid logos, readable text, and watermark.'
    }
  },
  {
    id: 'img-free-unboxing-table',
    previewImage: '/template-previews/img-free-unboxing-table.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '开箱桌面',
    title: '开箱桌面图',
    subtitle: '包装 / 配件 / 使用感',
    description: '适合开箱、产品展示和配件组合，用桌面构图提升真实感。',
    routeType: 'creative',
    tags: ['自由创作', '开箱', '桌面', '配件'],
    previewTone: 'pink',
    imagePreset: {
      style: 'lifestyle unboxing tabletop scene',
      size: '1024x1024',
      prompt: 'Create a polished unboxing tabletop scene. Show the main product, packaging, and accessories arranged naturally on a clean table with soft daylight. No readable text, no logos, no watermark.'
    }
  },
  {
    id: 'img-free-gifting-scene',
    previewImage: '/template-previews/img-free-gifting-scene.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '礼赠场景',
    title: '礼赠场景图',
    subtitle: '送礼 / 节日 / 情绪价值',
    description: '适合礼物、节日和送礼场景，强化情绪价值和开箱仪式感。',
    routeType: 'creative',
    tags: ['自由创作', '礼物', '节日', '场景'],
    previewTone: 'red',
    imagePreset: {
      style: 'giftable ecommerce lifestyle scene',
      size: '1024x1024',
      prompt: 'Create a gift-ready lifestyle product scene. Present the product as a thoughtful present with tasteful wrapping cues and warm emotional lighting. Keep the product central and avoid text, logos, and watermark.'
    }
  },
  {
    id: 'img-free-kitchen-use-case',
    previewImage: '/template-previews/img-free-kitchen-use-case.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '厨房场景',
    title: '厨房使用场景图',
    subtitle: '真实操作 / 家用感 / 细节',
    description: '适合厨房工具、餐厨用品和家用电器，表达真实使用状态和便捷感。',
    routeType: 'creative',
    tags: ['自由创作', '厨房', '使用场景', '家用'],
    previewTone: 'green',
    imagePreset: {
      style: 'realistic kitchen use-case product scene',
      size: '1024x1024',
      prompt: 'Create a realistic kitchen use-case product scene. Show the product in active use on a clean kitchen counter with natural light and clear function visibility. Avoid logos, readable text, and watermark.'
    }
  },
  {
    id: 'img-free-office-desk-setup',
    previewImage: '/template-previews/img-free-office-desk-setup.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '办公桌搭配',
    title: '办公桌搭配图',
    subtitle: '桌面 / 效率 / 品质感',
    description: '适合桌面配件、办公用品和数码小物，做成干净的高效办公场景。',
    routeType: 'creative',
    tags: ['自由创作', '办公', '桌面', '数码'],
    previewTone: 'blue',
    imagePreset: {
      style: 'minimal office desk product scene',
      size: '1024x1024',
      prompt: 'Create a minimal office desk scene featuring the product naturally in use. Use tidy composition, premium materials, and soft daylight. No readable text, no logos, and no watermark.'
    }
  },
  {
    id: 'img-free-travel-gear',
    previewImage: '/template-previews/img-free-travel-gear.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '旅行装备',
    title: '旅行装备图',
    subtitle: '出行 / 便携 / 轻量',
    description: '适合旅行收纳、便携电子、户外小物和通勤装备，强调易携带与使用便利。',
    routeType: 'creative',
    tags: ['自由创作', '旅行', '便携', '装备'],
    previewTone: 'purple',
    imagePreset: {
      style: 'travel gear product lifestyle image',
      size: '1024x1024',
      prompt: 'Create a travel gear lifestyle product image. Show the product packed or in use for travel with clean composition, practical realism, and subtle adventure mood. No logos, no readable text, no watermark.'
    }
  },
  {
    id: 'img-free-sport-action',
    previewImage: '/template-previews/img-free-sport-action.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '运动动感',
    title: '运动动感图',
    subtitle: '动态 / 活力 / 商品表现',
    description: '适合运动、户外和功能型商品，用动感场景强化活力与使用感。',
    routeType: 'creative',
    tags: ['自由创作', '运动', '动感', '户外'],
    previewTone: 'dark',
    imagePreset: {
      style: 'dynamic sports product scene',
      size: '1024x1024',
      prompt: 'Create a dynamic sports product scene with motion energy, clear product visibility, and a premium commercial feel. Keep the product accurate and avoid logos, readable text, and watermark.'
    }
  },
  {
    id: 'img-free-premium-drop-shadow',
    previewImage: '/template-previews/img-free-premium-drop-shadow.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '高级留白',
    title: '高级留白图',
    subtitle: '极简 / 留白 / 商业感',
    description: '适合品牌首页、广告封面和简洁商品海报，保留大面积留白给后期排版。',
    routeType: 'creative',
    tags: ['自由创作', '极简', '留白', '海报'],
    previewTone: 'clean',
    imagePreset: {
      style: 'premium minimal product composition',
      size: '1024x1024',
      prompt: 'Create a premium minimal product composition with generous negative space, soft drop shadows, and a refined commercial look. Keep the product the visual focus and avoid any readable text, logos, or watermark.'
    }
  },
  {
    id: 'img-free-ambient-shadow',
    previewImage: '/template-previews/img-free-ambient-shadow.webp',
    kind: 'image',
    market: 'overseas',
    imageGroup: 'creative',
    platform: 'PicPilot Free',
    category: '氛围阴影',
    title: '氛围阴影图',
    subtitle: '光影 / 层次 / 质感',
    description: '适合强调光影、层次和高级气氛的商品视觉，兼顾可读性和商业质感。',
    routeType: 'creative',
    tags: ['自由创作', '光影', '氛围', '质感'],
    previewTone: 'gold',
    imagePreset: {
      style: 'ambient shadow product art direction',
      size: '1024x1024',
      prompt: 'Create an ambient shadow product composition with elegant lighting, layered shadows, and a premium editorial-commercial feel. Keep the product clearly readable and avoid text, logos, and watermark.'
    }
  },
  {
    id: 'copy-cn-xhs-seeding',
    previewImage: '/template-previews/copy-cn-xhs-seeding-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '小红书',
    category: '种草文案',
    title: '小红书种草笔记',
    subtitle: '标题钩子 + 真实体验 + 标签',
    description: '适合生活方式、穿搭、美妆、家居、礼物类商品，避免硬广味和虚假体验。',
    routeType: 'copy',
    tags: ['小红书', '种草', '标题', '标签'],
    previewTone: 'pink',
    copyPreset: {
      outputType: '小红书种草笔记',
      requiredInfo: ['商品名', '目标人群', '使用场景', '真实卖点', '禁用词'],
      opening: '请你作为小红书电商运营，帮我写一篇真实、有收藏欲、不像硬广的小红书种草笔记。输出 8 个标题备选、正文、分段卖点、图片封面文字、评论区引导和标签。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-xhs-comparison',
    previewImage: '/template-previews/copy-cn-xhs-comparison-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '小红书',
    category: '评测对比',
    title: '小红书评测对比',
    subtitle: '对比逻辑 + 选购建议',
    description: '适合“同类怎么选”“踩坑避雷”“真实体验”内容，强调事实边界。',
    routeType: 'copy',
    tags: ['小红书', '评测', '对比', '避雷'],
    previewTone: 'green',
    copyPreset: {
      outputType: '小红书评测对比',
      requiredInfo: ['商品 A/B 信息', '对比维度', '适合人群', '真实优缺点'],
      opening: '请你作为小红书评测型运营，帮我写一篇对比笔记。要求先给标题，再给对比维度表、适合谁、不适合谁、真实购买建议和封面文案。不能编造未提供参数。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-douyin-short-video',
    previewImage: '/template-previews/copy-cn-douyin-short-video-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '抖音电商',
    category: '短视频脚本',
    title: '抖音带货短视频脚本',
    subtitle: '3 秒钩子 + 镜头脚本 + 口播',
    description: '适合 15-45 秒短视频，包含镜头、口播、画面文字和转化引导。',
    routeType: 'copy',
    tags: ['抖音', '短视频', '脚本', '口播'],
    previewTone: 'dark',
    copyPreset: {
      outputType: '抖音短视频带货脚本',
      requiredInfo: ['商品名', '核心卖点', '目标用户', '使用场景', '视频时长'],
      opening: '请你作为抖音电商短视频编导，帮我写一条高停留但合规的带货脚本。输出：3 秒开头钩子、分镜脚本、口播、画面文字、拍摄道具、结尾引导。不要夸大功效，不要编造价格。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-douyin-live-talk',
    previewImage: '/template-previews/copy-cn-douyin-live-talk-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '抖音电商',
    category: '直播话术',
    title: '抖音直播间话术',
    subtitle: '开场 / 讲品 / 逼单 / 异议处理',
    description: '适合直播间主播和中控，用分段话术提升讲品效率。',
    routeType: 'copy',
    tags: ['抖音', '直播', '话术', '转化'],
    previewTone: 'red',
    copyPreset: {
      outputType: '抖音直播话术',
      requiredInfo: ['商品名', '卖点', '价格规则', '赠品/优惠', '常见问题'],
      opening: '请你作为抖音电商直播运营，帮我设计一套直播讲品话术。输出开场、痛点引入、卖点讲解、场景演示、互动提问、异议处理、下单提醒。所有价格和承诺只能使用我提供的信息。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-taobao-detail',
    previewImage: '/template-previews/copy-cn-taobao-detail-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '淘宝/天猫',
    category: '详情页文案',
    title: '淘宝详情页卖点文案',
    subtitle: '模块标题 + 卖点解释 + 图片文字',
    description: '适合详情页模块和主图卖点，一次生成页面结构与图片中文字。',
    routeType: 'copy',
    tags: ['淘宝', '详情页', '卖点', 'AI 图内中文'],
    previewTone: 'gold',
    copyPreset: {
      outputType: '淘宝详情页卖点文案',
      requiredInfo: ['商品名', '目标用户', '材质/规格', '卖点', '使用场景'],
      opening: '请你作为淘宝天猫详情页策划，帮我输出一套详情页文案结构。包含首屏标题、副标题、5 个详情页模块、每个模块的图片内文字、卖点解释和注意事项。不能编造未提供参数。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-pdd-main',
    previewImage: '/template-previews/copy-cn-pdd-main-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '拼多多',
    category: '主图短文案',
    title: '拼多多主图短文案',
    subtitle: '缩略图大字 + 直给利益点',
    description: '适合拼多多主图和卖点图，短句直接、信息密度高但不乱。',
    routeType: 'copy',
    tags: ['拼多多', '主图', '短文案', '转化'],
    previewTone: 'red',
    copyPreset: {
      outputType: '拼多多主图短文案',
      requiredInfo: ['商品名', '核心利益点', '适用人群', '真实规格', '禁用词'],
      opening: '请你作为拼多多电商主图策划，帮我生成一组适合主图和卖点图的大字短文案。输出主标题 10 个、角标 10 个、三卖点组合 8 组、图片排版建议。不要生成未提供价格、补贴、包邮或限时承诺。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-jd-spec',
    previewImage: '/template-previews/copy-cn-jd-spec-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '京东',
    category: '参数卖点',
    title: '京东参数型卖点',
    subtitle: '理性表达 + 参数可信',
    description: '适合京东数码、家电、工具、日百等偏理性决策商品。',
    routeType: 'copy',
    tags: ['京东', '参数', '专业', '卖点'],
    previewTone: 'blue',
    copyPreset: {
      outputType: '京东参数卖点文案',
      requiredInfo: ['商品名', '参数', '材质', '适用场景', '对比优势'],
      opening: '请你作为京东电商运营，帮我写一组理性、可信、参数清晰的商品卖点文案。输出商品标题建议、5 条卖点、参数模块文案、详情页图片文字和合规注意。参数只能来自我提供的信息。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-taobao-live-script',
    previewImage: '/template-previews/copy-cn-taobao-live-script-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '淘宝/天猫',
    category: '直播话术',
    title: '淘宝直播话术',
    subtitle: '开场 / 讲品 / 逼单 / 异议',
    description: '适合淘宝直播间讲品、过款、福利口播和常见异议处理，风格更贴近直播间实时转化。',
    routeType: 'copy',
    tags: ['淘宝', '直播', '话术', '逼单'],
    previewTone: 'red',
    copyPreset: {
      outputType: '淘宝直播话术',
      requiredInfo: ['商品名', '核心卖点', '价格规则', '优惠/赠品', '常见问题'],
      opening: '请你作为淘宝天猫直播运营，帮我设计一套适合直播间的讲品话术。输出开场、痛点引入、卖点讲解、上链接提醒、异议处理和催单话术。所有价格、赠品和承诺只能使用我提供的信息。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-jd-install-guide',
    previewImage: '/template-previews/copy-cn-jd-install-guide-real.jpg',
    kind: 'copy',
    market: 'domestic',
    platform: '京东',
    category: '安装说明',
    title: '京东安装说明文案',
    subtitle: '安装 / 装配 / 售后提示',
    description: '适合家电、家具、工具类商品，补齐安装步骤、注意事项和售后说明文案。',
    routeType: 'copy',
    tags: ['京东', '安装', '说明', '售后'],
    previewTone: 'blue',
    copyPreset: {
      outputType: '京东安装说明文案',
      requiredInfo: ['商品名', '安装步骤', '工具需求', '注意事项', '售后说明'],
      opening: '请你作为京东电商运营，帮我写一套安装说明文案。输出安装步骤、工具/配件说明、注意事项、适配提醒和售后提示。只能使用我提供的事实，不要补充未确认的参数。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-1688-inquiry-conversion',
    previewImage: '/template-previews/copy-cn-1688-inquiry-conversion.webp',
    kind: 'copy',
    market: 'domestic',
    platform: '1688',
    category: '询盘转化',
    title: '1688 询盘转化文案',
    subtitle: '采购痛点 / 供货卖点 / 询盘引导',
    description: '适合工厂店和源头供应商，把商品卖点改写成采购商更关心的供货能力、拿样路径和询盘话术。',
    routeType: 'copy',
    tags: ['1688', '询盘', 'B2B', '供货'],
    previewTone: 'blue',
    copyPreset: {
      outputType: '1688 询盘转化文案',
      requiredInfo: ['商品名', '适用采购商', '供货能力', '真实规格', '拿样/定制规则'],
      opening: '请你作为 1688 工厂店运营，帮我写一套面向采购商的询盘转化文案。输出商品标题方向、主图短文案、5 条供货卖点、详情页模块文案、询盘引导话术和合规注意。不要编造价格、起订量、库存、认证或交期。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-1688-supply-detail',
    previewImage: '/template-previews/copy-cn-1688-supply-detail.webp',
    kind: 'copy',
    market: 'domestic',
    platform: '1688',
    category: '供货详情',
    title: '1688 供货详情页文案',
    subtitle: '货源说明 / 参数模块 / 批发逻辑',
    description: '适合详情页说明工厂实力、货盘、规格、包装、发货和采购沟通流程，降低反复问询。',
    routeType: 'copy',
    tags: ['1688', '详情页', '批发', '参数'],
    previewTone: 'green',
    copyPreset: {
      outputType: '1688 供货详情页文案',
      requiredInfo: ['商品名', '货源类型', '规格参数', '包装信息', '采购常见问题'],
      opening: '请你作为 1688 B2B 详情页策划，帮我生成一套供货详情页文案。输出首屏标题、副标题、工厂/货源模块、规格参数模块、包装发货模块、采购 FAQ 和图片内短文案。所有参数、价格、起订量、库存和交期只能使用我提供的信息。商品信息如下：'
    }
  },
  {
    id: 'copy-cn-1688-sample-talk',
    previewImage: '/template-previews/copy-cn-1688-sample-talk.webp',
    kind: 'copy',
    market: 'domestic',
    platform: '1688',
    category: '拿样话术',
    title: '1688 拿样沟通话术',
    subtitle: '首次咨询 / 报价沟通 / 复购跟进',
    description: '适合客服和业务员，用标准话术承接拿样、规格确认、报价和批量采购跟进。',
    routeType: 'copy',
    tags: ['1688', '客服', '拿样', '报价'],
    previewTone: 'gold',
    copyPreset: {
      outputType: '1688 拿样沟通话术',
      requiredInfo: ['商品名', '规格范围', '拿样规则', '定制规则', '常见疑问'],
      opening: '请你作为 1688 店铺客服主管，帮我设计一套拿样与批发沟通话术。输出首次咨询回复、规格确认、报价信息收集、拿样说明、定制说明、异议处理和复购跟进。不得编造未提供的价格、起订量、交期或承诺。商品信息如下：'
    }
  },
  {
    id: 'copy-overseas-amazon-listing',
    previewImage: '/template-previews/copy-overseas-amazon-listing-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'Amazon',
    category: 'Listing 文案',
    title: 'Amazon Listing 文案',
    subtitle: '标题 / 五点 / 描述 / 关键词',
    description: '适合 Amazon 多站点 Listing，强调语言、本地化、参数真实性和禁词规避。',
    routeType: 'listing',
    tags: ['Amazon', 'Listing', 'A9', '合规'],
    previewTone: 'clean',
    listingPreset: {
      country: 'US',
      language: 'en-US',
      keywords: 'title keywords in priority order',
      sellingKeywords: 'bullet keywords in priority order',
      descriptionKeywords: 'description keywords in priority order',
      description: 'Brand: \nProduct name: \nMaterial: \nDimensions: \nCompatible models: \nPackage included: \nTarget audience: \nUse cases: \nForbidden claims: '
    },
    copyPreset: {
      outputType: 'Amazon Listing',
      requiredInfo: ['品牌', '产品名', '站点', '关键词', '材质尺寸', '包装清单'],
      opening: 'Please act as an Amazon marketplace operator and create a compliant Listing. Output JSON with title, 5 bullet points, product_description, search terms, and image prompt brief. Use only provided parameters and avoid unsupported claims. Product info:'
    }
  },
  {
    id: 'copy-overseas-amazon-aplus-copy',
    previewImage: '/template-previews/copy-overseas-amazon-aplus-copy-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'Amazon',
    category: 'A+ 文案',
    title: 'Amazon A+ 文案',
    subtitle: '品牌故事 / 模块卖点 / FAQ',
    description: '适合 A+ 图对应的模块文案、品牌故事和对比/FAQ 文案，不与 Listing 文案混用。',
    routeType: 'copy',
    tags: ['Amazon', 'A+', '文案', '品牌故事'],
    previewTone: 'blue',
    copyPreset: {
      opening: 'Please act as an Amazon A+ content writer. Create brand-story copy, comparison module copy, feature module copy, and FAQ copy for the provided product. Use only verified product facts and keep the tone conversion-focused but compliant. Product info:',
      outputType: 'Amazon A+ copy',
      requiredInfo: ['brand', 'product', 'key benefits', 'proof points', 'FAQ topics', 'forbidden claims']
    }
  },
  {
    id: 'copy-overseas-tiktok-shop',
    previewImage: '/template-previews/copy-overseas-tiktok-shop-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '商品文案',
    title: 'TikTok Shop 商品文案',
    subtitle: '短标题 + 卖点 + 视频钩子',
    description: '适合 TikTok Shop 商品卡、短视频文案和达人 brief。',
    routeType: 'copy',
    tags: ['TikTok Shop', '商品文案', '达人', '海外'],
    previewTone: 'dark',
    copyPreset: {
      outputType: 'TikTok Shop product copy',
      requiredInfo: ['product', 'target audience', 'benefits', 'usage scenario', 'country'],
      opening: 'Act as a TikTok Shop ecommerce copywriter. Create short product titles, benefit bullets, video hooks, creator brief, on-screen caption ideas, and compliance notes. Keep the tone natural and conversion-focused. Product info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-shop-pdp',
    previewImage: '/template-previews/copy-overseas-tiktok-shop-pdp.webp',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '商品详情文案',
    title: 'TikTok Shop 商品详情文案',
    subtitle: 'Title / Bullets / PDP Gallery Copy',
    description: '适合 TikTok Shop 商品标题、卖点、详情页轮播图短文案和合规注意。',
    routeType: 'copy',
    tags: ['TikTok Shop', 'PDP', '商品详情', '英文'],
    previewTone: 'blue',
    sourceNote: '参考 TikTok Shop 商品卡/PDP 内容结构，文案为 PicPilot 原创。',
    copyPreset: {
      outputType: 'TikTok Shop PDP copy',
      requiredInfo: ['product', 'target customer', 'benefits', 'materials/specs', 'package included', 'forbidden claims'],
      opening: 'Act as a TikTok Shop product content editor. Create a compliant product title, 5 short benefit bullets, PDP gallery image copy, creator-friendly caption ideas, and compliance notes. Use only provided facts and do not invent prices, discounts, ratings, certifications, or delivery promises. Product info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-ugc',
    previewImage: '/template-previews/copy-overseas-tiktok-ugc-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok',
    category: 'UGC 口播',
    title: 'TikTok UGC 口播脚本',
    subtitle: 'Hook / Problem / Demo / CTA',
    description: '适合达人投放素材，让口播更像真实体验而不是硬广。',
    routeType: 'copy',
    tags: ['TikTok', 'UGC', '口播', '脚本'],
    previewTone: 'purple',
    copyPreset: {
      outputType: 'TikTok UGC script',
      requiredInfo: ['product', 'creator persona', 'pain point', 'demo scene', 'CTA'],
      opening: 'Act as a TikTok UGC creative strategist. Write 5 short scripts with hook, pain point, product demo, proof moment, CTA, on-screen text, and shot list. Avoid fake claims and keep it creator-native. Product info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-live-rundown',
    previewImage: '/template-previews/copy-overseas-tiktok-live-rundown.webp',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '直播话术',
    title: 'TikTok Shop Live 话术',
    subtitle: 'Opening / Demo / Objection / CTA',
    description: '适合 TikTok Shop Live 主播讲品节奏，输出开场、演示、互动和合规下单提醒。',
    routeType: 'copy',
    tags: ['TikTok Shop', 'Live', '话术', '英文'],
    previewTone: 'dark',
    sourceNote: '参考 GitHub UGC ads 公开场景分类，文案由 PicPilot 重写。',
    copyPreset: {
      outputType: 'TikTok Shop Live rundown',
      requiredInfo: ['product', 'live theme', 'target audience', 'benefits', 'demo points', 'allowed offer'],
      opening: 'Act as a TikTok Shop Live host coach. Create a live-selling rundown with opening hook, product demo flow, audience interaction prompts, objection handling, safe CTA lines, and on-screen text ideas. Use only provided facts and offers. Live info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-affiliate-brief',
    previewImage: '/template-previews/copy-overseas-tiktok-affiliate-brief.webp',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok Shop',
    category: '达人 Brief',
    title: 'TikTok 达人 Brief',
    subtitle: 'Creator angle / Shot list / Do & Don’t',
    description: '适合给达人、联盟和素材团队发 brief，明确卖点、镜头、禁用词和交付要求。',
    routeType: 'copy',
    tags: ['TikTok Shop', 'Affiliate', '达人', 'Brief'],
    previewTone: 'purple',
    sourceNote: '参考 GitHub creator ads / UGC workflow 分类，模板内容为 PicPilot 原创。',
    copyPreset: {
      outputType: 'TikTok creator affiliate brief',
      requiredInfo: ['product', 'creator persona', 'audience', 'key benefits', 'must-show details', 'forbidden claims'],
      opening: 'Act as a TikTok Shop affiliate manager. Create a creator brief with campaign goal, target audience, creator angle, must-show product details, shot list, hook options, caption ideas, compliance notes, and do/don’t rules. Product info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-spark-ad-angles',
    previewImage: '/template-previews/copy-overseas-tiktok-spark-ad-angles.webp',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok Ads',
    category: '广告角度',
    title: 'TikTok Spark Ads 角度',
    subtitle: 'Hooks / Angles / Testing Matrix',
    description: '适合投放前拆 5-8 个广告角度，输出钩子、首帧、口播和测试变量。',
    routeType: 'copy',
    tags: ['TikTok Ads', 'Spark Ads', 'Hook', '测试'],
    previewTone: 'blue',
    sourceNote: '参考 GitHub short-form ads hook 分类，未复制第三方模板。',
    copyPreset: {
      outputType: 'TikTok Spark Ads angle matrix',
      requiredInfo: ['product', 'audience', 'pain points', 'benefits', 'proof points', 'forbidden claims'],
      opening: 'Act as a TikTok paid-social creative strategist. Build a Spark Ads testing matrix with 6 creative angles, first-frame ideas, hooks, short scripts, on-screen text, CTA, and compliance risks. Do not invent proof, prices, or results. Product info:'
    }
  },
  {
    id: 'copy-overseas-tiktok-comment-reply',
    previewImage: '/template-previews/copy-overseas-tiktok-comment-reply.webp',
    kind: 'copy',
    market: 'overseas',
    platform: 'TikTok',
    category: '评论区运营',
    title: 'TikTok 评论区回复',
    subtitle: 'FAQ / Objection / Follow-up',
    description: '适合短视频评论区、私信和售前 FAQ，回复自然、简短、合规。',
    routeType: 'copy',
    tags: ['TikTok', '评论区', 'FAQ', '客服'],
    previewTone: 'green',
    sourceNote: '参考 UGC/product-demo 工作流中的互动环节，PicPilot 重写为客服运营模板。',
    copyPreset: {
      outputType: 'TikTok comment and DM reply kit',
      requiredInfo: ['product', 'common questions', 'shipping/returns facts', 'benefits', 'forbidden claims'],
      opening: 'Act as a TikTok ecommerce community manager. Create short comment replies, DM replies, FAQ answers, objection handling lines, and follow-up prompts. Keep replies natural, concise, and based only on provided facts. Product info:'
    }
  },
  {
    id: 'copy-overseas-walmart-listing',
    previewImage: '/template-previews/copy-overseas-walmart-listing-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'Walmart',
    category: 'Listing 文案',
    title: 'Walmart Listing 文案',
    subtitle: '站点 SEO / 合规 / 参数真实',
    description: '适合 Walmart Marketplace 的商品标题、卖点和描述写法，强调站点搜索逻辑和真实参数。',
    routeType: 'listing',
    tags: ['Walmart', 'Listing', 'SEO', '合规'],
    previewTone: 'clean',
    listingPreset: {
      country: 'US',
      language: 'en-US',
      keywords: 'walmart title keywords, product type, use case',
      sellingKeywords: 'main benefit, audience, compatibility',
      descriptionKeywords: 'feature, specification, package included',
      description: 'Brand: \\nProduct name: \\nMaterial: \\nDimensions: \\nCompatible models: \\nPackage included: \\nTarget audience: \\nUse cases: \\nForbidden claims: '
    },
    copyPreset: {
      opening: 'Act as a Walmart Marketplace copywriter. Create compliant product title, bullets, and description based on the provided facts only. Keep the language concise, SEO-friendly, and free of unsupported claims. Product info:',
      outputType: 'Walmart Listing',
      requiredInfo: ['brand', 'product', 'material', 'dimensions', 'compatibility', 'package contents']
    }
  },
  {
    id: 'copy-overseas-shopify-landing',
    previewImage: '/template-previews/copy-overseas-shopify-landing-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'Shopify/DTC',
    category: '落地页文案',
    title: 'DTC 落地页文案',
    subtitle: 'Hero / Benefits / FAQ / CTA',
    description: '适合独立站首屏、广告落地页和邮件承接页。',
    routeType: 'copy',
    tags: ['DTC', 'Shopify', '落地页', '英文'],
    previewTone: 'green',
    copyPreset: {
      outputType: 'DTC landing page copy',
      requiredInfo: ['brand', 'product', 'audience', 'benefits', 'proof', 'offer'],
      opening: 'Act as a DTC ecommerce landing page copywriter. Create a conversion-focused page structure with hero headline, subheadline, benefit sections, proof blocks, FAQ, CTA copy, and image prompt suggestions. Use only provided product facts. Product info:'
    }
  },
  {
    id: 'copy-overseas-email-campaign',
    previewImage: '/template-previews/copy-overseas-email-campaign-real.jpg',
    kind: 'copy',
    market: 'overseas',
    platform: 'Email',
    category: '营销邮件',
    title: '海外营销邮件',
    subtitle: '新品 / 召回 / 促销 / 内容型',
    description: '适合独立站 EDM，输出主题、预览文本和正文结构。',
    routeType: 'copy',
    tags: ['Email', 'DTC', 'EDM', '英文'],
    previewTone: 'blue',
    copyPreset: {
      outputType: 'ecommerce email campaign',
      requiredInfo: ['campaign goal', 'product', 'audience', 'offer', 'deadline'],
      opening: 'Act as an ecommerce lifecycle marketer. Create an email campaign with subject lines, preview text, body copy, CTA, segmentation notes, and compliance reminders. Do not invent offers or deadlines. Campaign info:'
    }
  }
]

export function getTemplateById(id: string | null | undefined) {
  if (!id) return undefined
  return TEMPLATE_CATALOG.find(item => item.id === id)
}

export function templateHref(item: TemplateCatalogItem) {
  const query = `templateId=${encodeURIComponent(item.id)}`
  if (item.routeType === 'domestic') return `/commerce?tab=domestic&${query}`
  if (item.routeType === 'listing') return `/commerce?tab=listing&${query}`
  if (item.routeType === 'aplus') return `/commerce?tab=aplus&${query}`
  if (item.routeType === 'creative') return `/ai-create?tab=free&${query}`
  return `/ai-create?tab=copy&${query}`
}

export function templateIntentMeta(item: TemplateCatalogItem | undefined): TemplateIntentMeta {
  if (!item) return { intent: 'free_create', textMode: 'no_text', referencePolicy: 'preserve' }
  const poster = item.routeType === 'domestic'
    || item.category.includes('主图')
    || item.category.includes('详情')
    || item.category.includes('海报')
    || item.category.includes('卖点')
    || item.category.includes('模块')
  return {
    templateId: item.id,
    intent: item.intent || (poster ? 'poster_layout' : 'free_create'),
    textMode: item.textMode || (item.routeType === 'domestic' ? 'ai_short_text' : 'no_text'),
    referencePolicy: item.referencePolicy || 'preserve'
  }
}

export function copyTemplateSeed(item: TemplateCatalogItem) {
  if (!item.copyPreset) return ''
  const fields = item.copyPreset.requiredInfo.map(field => `- ${field}: `).join('\n')
  return `${item.copyPreset.opening}\n${fields}`
}
