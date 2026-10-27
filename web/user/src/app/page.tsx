'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { ArrowRight, ArrowUpRight, BadgeCheck, ChevronLeft, ChevronRight, CircleCheckBig, GalleryHorizontalEnd, Gauge, ImagePlus, Images, Languages, Layers3, Maximize2, Sparkles, Store, UsersRound, Video, Wand2, X } from 'lucide-react'
import { BrandLogo } from '@/components/BrandLogo'
import { useRuntime } from '@/components/AppRuntime'
import { LanguageMenu } from '@/components/LanguageMenu'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useLanguage } from '@/components/i18n'
import { getToken } from '@/lib/api'

const copy = {
  'zh-CN': {
    navProduct: '创作工具',
    navDemo: '案例 Demo',
    navWorkflow: '生成流程',
    navUseCases: '适用场景',
    login: '登录',
    register: '注册',
    workspace: '进入工作台',
    eyebrow: '商品图片 · 多语言素材 · 商品视频',
    title: 'PicPilot 电商视觉工作台',
    subtitle: '上传商品资料和参考图，在一个工作台里制作主图、平台套图、详情素材、多语言译图与商品视频，并持续沉淀可复用的视觉资产。',
    primaryCta: '开始制作',
    secondaryCta: '浏览模板',
    heroStatOne: '商品图与套图',
    heroStatTwo: '图片翻译',
    heroStatThree: '商品视频',
    proofAria: 'PicPilot 平台数据',
    proofKicker: 'PicPilot 生产数据',
    proofLead: '真实创作任务持续累计',
    proofUsers: '已服务用户',
    proofImages: '累计生成商品图',
    proofSuccess: '图片任务生成成功率',
    proofSpeed: '极致响应',
    proofPlatforms: '覆盖电商平台',
    heroVisualAlt: 'PicPilot 商品视觉工作台展示商品主图、细节图、场景图和生成流程',
    channelTitle: '从一张商品图，进入完整的内容工作流',
    channelDesc: '直接选择要完成的任务。每个模块都会保留参考图、生成任务与历史结果，方便后续继续迭代。',
    channelCross: '商品图与平台套图',
    channelCrossDesc: '制作商品主图、卖点图、场景图、详情图和 Amazon、TikTok Shop 等平台套图。',
    channelCrossTags: '主图 / Listing / A+ / 平台套图',
    channelDomestic: '图片翻译与本地化',
    channelDomesticDesc: '批量上传商品详情图，识别原图内容并生成对应目标语言版本。',
    channelDomesticTags: '多图输入 / 多语言 / 对应输出',
    channelCreative: '商品图片转视频',
    channelCreativeDesc: '使用商品参考图生成适合电商展示、广告投放和短视频渠道的商品视频。',
    channelCreativeTags: '9:16 / 16:9 / 标准 / 品质',
    demoEyebrow: '真实商品资产包',
    demoTitle: '1 张商品参考图，生成 6 张可直接投放的雨伞素材',
    demoDesc: '锁定商品外观与核心卖点，一次完成封面、主图、场景、卖点、人群和细节素材，并保持整套视觉一致。',
    demoCta: '用我的商品开始制作',
    demoProductName: '朱砂红轻量折叠晴雨伞',
    demoProductMeta: '小红书 / 淘宝 / 商品详情页',
    demoCaseBadge: '6 张对应输出',
    demoMainAlt: '朱砂红折叠晴雨伞商品套图封面',
    demoLifestyleAlt: '朱砂红折叠晴雨伞白底商品主图',
    demoDetailAlt: '朱砂红折叠晴雨伞雨天通勤场景图',
    demoSellingAlt: '朱砂红折叠晴雨伞便携卖点图',
    demoCrowdAlt: '朱砂红折叠晴雨伞目标人群图',
    demoStructureAlt: '朱砂红折叠晴雨伞结构细节图',
    demoPreview: '查看完整图',
    demoClose: '关闭预览',
    demoPrevious: '上一张',
    demoNext: '下一张',
    demoInputTitle: '01 / 商品资料',
    demoInput1: '参考：原始商品图 1 张',
    demoInput2: '卖点：折叠便携、晴雨两用、细密伞面',
    demoInput3: '人群：城市通勤、学生与旅行用户',
    demoPromptTitle: '02 / 视觉策略',
    demoPrompt: '锁定朱砂红伞面、黑色伞骨与米白手柄；围绕便携收纳、雨天通勤和结构细节组织画面，并按渠道控制文字密度与构图。',
    demoOutputTitle: '03 / 交付资产',
    demoOutput1: '主视觉：套图封面与干净商品主图',
    demoOutput2: '使用场景：雨天街头、地铁口与随身收纳',
    demoOutput3: '转化素材：卖点拆解、目标人群与结构细节',
    demoQuality: '一致性控制',
    demoQualityDesc: '六张图保持同一把伞的颜色、结构和材质',
    demoSimple: '批量交付',
    demoSimpleDesc: '一次生成封面、主图、场景、卖点、人群和细节',
    demoReusable: '多渠道复用',
    demoReusableDesc: '同一套素材可继续用于小红书、淘宝与详情页',
    workflow: '标准化生产链',
    sectionFeature: '从商品资料到可复用资产，四步完成',
    sectionFeatureDesc: '每一步都有明确输入与产出，让上新、投放和二次创作不再从零开始。',
    feature1: '建立商品档案',
    feature1Desc: '保存商品事实、核心卖点、目标人群、参考图和不可变信息。',
    feature2: '形成画面计划',
    feature2Desc: '按渠道拆分主图、场景、细节、翻译和视频等具体任务。',
    feature3: '批量生成与校验',
    feature3Desc: '统一主体与材质表达，跟踪进度，并明确处理失败和退款。',
    feature4: '沉淀可复用资产',
    feature4Desc: '保留历史结果与优秀素材，下一轮上新和投放直接继续。',
    step1: '商品事实与参考图',
    step2: '渠道目标与画面 Brief',
    step3: '生成任务与结果检查',
    step4: '历史记录与再次创作',
    useCases: '不同团队，共用一套内容生产系统',
    useCasesLead: '覆盖上新、Listing、A+、详情页、广告和内容渠道，让策略、生成与资产始终在同一工作流里。',
    case1: '跨境卖家与运营',
    case1Desc: '围绕 Listing、A+、广告图和店铺素材快速做测试与迭代。',
    case2: '国内电商团队',
    case2Desc: '为主图、详情页、活动页和内容平台持续产出不同风格的视觉方向。',
    case3: '设计师与内容服务商',
    case3Desc: '用 AI 快速扩展方案，再用专业设计完成精修、排版和交付。',
    finalEyebrow: '开始建立商品内容资产',
    finalTitle: '下一款商品，从第一张图开始规模化生产',
    finalDesc: '创建商品项目，上传参考图，用一套工作流完成策划、生成、检查与持续复用。',
    finalCta: '创建账号'
  },
  'en-US': {
    navProduct: 'Create',
    navDemo: 'Demo',
    navWorkflow: 'Workflow',
    navUseCases: 'Use cases',
    login: 'Sign in',
    register: 'Create account',
    workspace: 'Open workspace',
    eyebrow: 'Product images · Localization · Product video',
    title: 'PicPilot Commerce Visual Workspace',
    subtitle: 'Upload product facts and references, then create hero images, marketplace sets, detail assets, localized visuals, and product videos in one reusable workspace.',
    primaryCta: 'Start creating',
    secondaryCta: 'Browse templates',
    heroStatOne: 'Product image sets',
    heroStatTwo: 'Image localization',
    heroStatThree: 'Product video',
    proofAria: 'PicPilot platform metrics',
    proofKicker: 'PicPilot production data',
    proofLead: 'Growing with real creative tasks',
    proofUsers: 'Users served',
    proofImages: 'Product images generated',
    proofSuccess: 'Image task success rate',
    proofSpeed: 'Ultra-fast response',
    proofPlatforms: 'Commerce platforms covered',
    heroVisualAlt: 'PicPilot commerce workspace showing product hero images, details, lifestyle scenes, and a generation workflow',
    channelTitle: 'Turn one product image into a complete content workflow',
    channelDesc: 'Choose the task you need. Every module keeps references, jobs, and results together so the next iteration starts with context.',
    channelCross: 'Product images and channel sets',
    channelCrossDesc: 'Create hero images, benefits, scenes, detail visuals, and complete sets for Amazon, TikTok Shop, and other channels.',
    channelCrossTags: 'Hero / Listing / A+ / Channel sets',
    channelDomestic: 'Image translation and localization',
    channelDomesticDesc: 'Upload product detail images in batches and produce corresponding versions in the target language.',
    channelDomesticTags: 'Multi-image / Multilingual / Paired output',
    channelCreative: 'Product image to video',
    channelCreativeDesc: 'Use product references to create ecommerce demos, ad creatives, and short-form product videos.',
    channelCreativeTags: '9:16 / 16:9 / Standard / Quality',
    demoEyebrow: 'Real product asset pack',
    demoTitle: 'Turn one product reference into six campaign-ready umbrella assets',
    demoDesc: 'Lock the product appearance and key benefits, then deliver a cover, hero, scene, benefit, audience, and detail set with consistent art direction.',
    demoCta: 'Start with my product',
    demoProductName: 'Vermilion lightweight folding umbrella',
    demoProductMeta: 'Social / Marketplace / Product detail',
    demoCaseBadge: '6 paired outputs',
    demoMainAlt: 'Cover image for a vermilion folding umbrella product set',
    demoLifestyleAlt: 'Clean product image of a vermilion folding umbrella',
    demoDetailAlt: 'Rainy commute scene featuring a vermilion folding umbrella',
    demoSellingAlt: 'Portable benefit visual for a vermilion folding umbrella',
    demoCrowdAlt: 'Audience visual for a vermilion folding umbrella',
    demoStructureAlt: 'Structural detail visual for a vermilion folding umbrella',
    demoPreview: 'View full image',
    demoClose: 'Close preview',
    demoPrevious: 'Previous image',
    demoNext: 'Next image',
    demoInputTitle: '01 / Product data',
    demoInput1: 'Reference: one original product image',
    demoInput2: 'Benefits: compact, rain-or-shine use, tightly woven canopy',
    demoInput3: 'Audience: urban commuters, students, and travelers',
    demoPromptTitle: '02 / Visual strategy',
    demoPrompt: 'Lock the vermilion canopy, black ribs, and warm-white handle. Build around compact storage, rainy commutes, and construction details, adapting composition and text density by channel.',
    demoOutputTitle: '03 / Delivered assets',
    demoOutput1: 'Hero visuals: set cover and clean product image',
    demoOutput2: 'Usage scenes: rainy street, subway entrance, and bag storage',
    demoOutput3: 'Conversion assets: benefits, audience, and construction details',
    demoQuality: 'Consistency control',
    demoQualityDesc: 'The same color, structure, and materials across all six images',
    demoSimple: 'Batch delivery',
    demoSimpleDesc: 'Cover, hero, scene, benefits, audience, and details in one run',
    demoReusable: 'Multi-channel reuse',
    demoReusableDesc: 'Reuse the same set across social, marketplace, and detail pages',
    workflow: 'Standard production pipeline',
    sectionFeature: 'From product data to reusable assets in four stages',
    sectionFeatureDesc: 'Every stage has a clear input and output, so launches, campaigns, and revisions never restart from zero.',
    feature1: 'Build a product profile',
    feature1Desc: 'Keep facts, benefits, audience, references, and non-negotiable product details together.',
    feature2: 'Create the visual plan',
    feature2Desc: 'Split channel goals into hero, scene, detail, localization, and video tasks.',
    feature3: 'Generate and review',
    feature3Desc: 'Keep subjects and materials consistent while tracking progress, failures, and refunds.',
    feature4: 'Reuse proven assets',
    feature4Desc: 'Save strong outputs and history so the next launch or campaign starts ahead.',
    step1: 'Facts and references',
    step2: 'Channel goals and visual briefs',
    step3: 'Generation tasks and review',
    step4: 'History and further creation',
    useCases: 'One content system for different ecommerce teams',
    useCasesLead: 'Cover launches, Listing, A+, detail pages, ads, and content channels while keeping strategy, generation, and assets in one workflow.',
    case1: 'Cross-border sellers',
    case1Desc: 'Test and iterate Listing, A+, ad, and storefront materials around each product.',
    case2: 'Domestic ecommerce teams',
    case2Desc: 'Produce visual directions for hero images, detail pages, campaigns, and content channels.',
    case3: 'Designers and agencies',
    case3Desc: 'Use AI to expand directions quickly, then refine layout, text, and delivery professionally.',
    finalEyebrow: 'Start building product content assets',
    finalTitle: 'Scale the next product from its very first image',
    finalDesc: 'Create a product project and use one workflow for planning, generation, review, and continued reuse.',
    finalCta: 'Create account'
  },
  'ja-JP': {
    navProduct: '制作ツール',
    navDemo: 'デモ',
    navWorkflow: '流れ',
    navUseCases: '用途',
    login: 'ログイン',
    register: '登録',
    workspace: 'ワークスペースへ',
    eyebrow: '商品画像 · 多言語素材 · 商品動画',
    title: 'PicPilot ECビジュアルワークスペース',
    subtitle: '商品情報と参考画像をアップロードし、メイン画像、プラットフォーム向けセット、詳細素材、多言語画像、商品動画を一つのワークスペースで制作できます。',
    primaryCta: '制作を始める',
    secondaryCta: 'テンプレートを見る',
    heroStatOne: '商品画像セット',
    heroStatTwo: '画像ローカライズ',
    heroStatThree: '商品動画',
    proofAria: 'PicPilot プラットフォーム実績',
    proofKicker: 'PicPilot 制作データ',
    proofLead: '実際の制作タスクから継続集計',
    proofUsers: '利用ユーザー',
    proofImages: '生成済み商品画像',
    proofSuccess: '画像タスク成功率',
    proofSpeed: '高速レスポンス',
    proofPlatforms: '対応ECプラットフォーム',
    heroVisualAlt: 'PicPilotの商品メイン画像、詳細画像、シーン画像、生成フローを表示するECワークスペース',
    channelTitle: '一枚の商品画像から、完全なコンテンツ制作へ',
    channelDesc: '必要なタスクを直接選択。参考画像、生成タスク、履歴結果をモジュールごとに保存し、次の改善に活用できます。',
    channelCross: '商品画像とプラットフォームセット',
    channelCrossDesc: '商品メイン画像、訴求、シーン、詳細画像、AmazonやTikTok Shop向けの画像セットを制作。',
    channelCrossTags: 'メイン / Listing / A+ / セット',
    channelDomestic: '画像翻訳とローカライズ',
    channelDomesticDesc: '商品詳細画像をまとめてアップロードし、対象言語に対応した画像を生成します。',
    channelDomesticTags: '複数画像 / 多言語 / 対応出力',
    channelCreative: '商品画像から動画へ',
    channelCreativeDesc: '商品参考画像からEC展示、広告、ショート動画向けの商品動画を制作します。',
    channelCreativeTags: '9:16 / 16:9 / 標準 / 品質',
    demoEyebrow: '実際の商品アセットパック',
    demoTitle: '1枚の商品参考画像から、配信できる傘素材6枚を制作',
    demoDesc: '商品外観と主な訴求点を固定し、表紙、メイン、シーン、訴求、対象ユーザー、詳細素材を統一した方向で一括制作します。',
    demoCta: '自分の商品で制作する',
    demoProductName: '朱色の軽量折りたたみ晴雨兼用傘',
    demoProductMeta: 'SNS / ECモール / 商品詳細',
    demoCaseBadge: '6枚を対応出力',
    demoMainAlt: '朱色の折りたたみ傘の商品セット表紙',
    demoLifestyleAlt: '朱色の折りたたみ傘の商品メイン画像',
    demoDetailAlt: '朱色の折りたたみ傘を使う雨の日の通勤シーン',
    demoSellingAlt: '朱色の折りたたみ傘の携帯性訴求画像',
    demoCrowdAlt: '朱色の折りたたみ傘の対象ユーザー画像',
    demoStructureAlt: '朱色の折りたたみ傘の構造詳細画像',
    demoPreview: '全体画像を見る',
    demoClose: 'プレビューを閉じる',
    demoPrevious: '前の画像',
    demoNext: '次の画像',
    demoInputTitle: '01 / 商品データ',
    demoInput1: '参考：元の商品画像1枚',
    demoInput2: '訴求：折りたたみ、晴雨兼用、細密な傘生地',
    demoInput3: '対象：都市通勤、学生、旅行ユーザー',
    demoPromptTitle: '02 / ビジュアル戦略',
    demoPrompt: '朱色の傘生地、黒い骨、温かみのある白い持ち手を固定し、携帯収納、雨天通勤、構造詳細を中心に、チャネル別に構図と文字量を調整します。',
    demoOutputTitle: '03 / 納品アセット',
    demoOutput1: 'メイン：セット表紙とクリーンな商品画像',
    demoOutput2: '利用シーン：雨の街、地下鉄入口、バッグ収納',
    demoOutput3: '転換素材：訴求点、対象ユーザー、構造詳細',
    demoQuality: '一貫性を管理',
    demoQualityDesc: '6枚すべてで同じ色、構造、素材を維持',
    demoSimple: 'まとめて納品',
    demoSimpleDesc: '表紙、メイン、シーン、訴求、対象、詳細を一度に制作',
    demoReusable: '複数チャネルで再利用',
    demoReusableDesc: 'SNS、ECモール、詳細ページに継続利用',
    workflow: '標準化された制作フロー',
    sectionFeature: '商品データから再利用アセットまで4段階',
    sectionFeatureDesc: '各段階の入力と出力を明確にし、発売、広告、再制作をゼロから始めない仕組みにします。',
    feature1: '商品プロフィールを作る',
    feature1Desc: '商品事実、訴求点、対象ユーザー、参考画像、変えてはいけない情報を保存します。',
    feature2: 'ビジュアル計画を作る',
    feature2Desc: 'チャネル目標をメイン、シーン、詳細、翻訳、動画の各タスクに分けます。',
    feature3: 'まとめて生成・確認',
    feature3Desc: '主体と素材を統一しながら、進捗、失敗、ポイント返還を追跡します。',
    feature4: '資産として再利用',
    feature4Desc: '履歴と良い結果を保存し、次の発売や広告制作へそのまま続けます。',
    step1: '商品事実と参考画像',
    step2: 'チャネル目標と画面 Brief',
    step3: '生成タスクと結果確認',
    step4: '履歴と再制作',
    useCases: '異なるECチームで共有できる一つの制作システム',
    useCasesLead: '発売、Listing、A+、詳細ページ、広告、コンテンツチャネルまで、戦略・生成・資産を同じ流れで管理します。',
    case1: '越境セラーと運用者',
    case1Desc: 'Listing、A+、広告、店舗素材を商品ごとにテスト・改善できます。',
    case2: '国内ECチーム',
    case2Desc: 'メイン画像、詳細ページ、販促、内容平台向けの方向案を継続的に作れます。',
    case3: 'デザイナーと制作会社',
    case3Desc: 'AI で案を素早く広げ、最後は専門的なレイアウトや文字入れで仕上げます。',
    finalEyebrow: '商品コンテンツ資産を作り始める',
    finalTitle: '次の商品を、最初の一枚から規模化',
    finalDesc: '商品プロジェクトを作成し、企画、生成、確認、継続利用まで一つの流れで進めます。',
    finalCta: 'アカウント登録'
  }
}

const featureIcons = [GalleryHorizontalEnd, Layers3, Wand2, BadgeCheck]
const channelIcons = [Images, Languages, Video]
const channelHrefs = ['/commerce?tab=suite', '/commerce?tab=translate', '/video-create']
const DEMO_IMAGE_COUNT = 6

export default function LandingPage() {
  const { registrationEnabled } = useRuntime()
  const language = useLanguage()
  const t = copy[language as keyof typeof copy] || copy['zh-CN']
  const [signedIn, setSignedIn] = useState(false)
  const [previewIndex, setPreviewIndex] = useState<number | null>(null)
  const primaryHref = signedIn ? '/workspace' : registrationEnabled ? '/register' : '/login'
  const demoImages = [
    ['/landing-demo-umbrella-cover.webp', t.demoMainAlt],
    ['/landing-demo-umbrella-main.webp', t.demoLifestyleAlt],
    ['/landing-demo-umbrella-scenario.webp', t.demoDetailAlt],
    ['/landing-demo-umbrella-selling.webp', t.demoSellingAlt],
    ['/landing-demo-umbrella-crowd.webp', t.demoCrowdAlt],
    ['/landing-demo-umbrella-detail.webp', t.demoStructureAlt]
  ]

  useEffect(() => {
    setSignedIn(Boolean(getToken()))
  }, [])

  useEffect(() => {
    if (previewIndex === null) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPreviewIndex(null)
      if (event.key === 'ArrowLeft') {
        setPreviewIndex(current => current === null ? null : (current + DEMO_IMAGE_COUNT - 1) % DEMO_IMAGE_COUNT)
      }
      if (event.key === 'ArrowRight') {
        setPreviewIndex(current => current === null ? null : (current + 1) % DEMO_IMAGE_COUNT)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [previewIndex])

  return (
    <main className="landing-page">
      <header className="landing-nav">
        <Link href="/" className="landing-brand" aria-label="PicPilot">
          <BrandLogo className="picpilot-brand-landing" />
        </Link>
        <nav className="landing-links" aria-label="Landing navigation">
          <a href="#product">{t.navProduct}</a>
          <a href="#demo">{t.navDemo}</a>
          <a href="#workflow">{t.navWorkflow}</a>
          <a href="#use-cases">{t.navUseCases}</a>
        </nav>
        <div className="landing-actions">
          <LanguageMenu />
          <ThemeToggle />
          {signedIn ? <Link className="btn btn-primary" href="/workspace">{t.workspace}</Link> : (
            <>
              <Link className="btn" href="/login">{t.login}</Link>
              {registrationEnabled ? <Link className="btn btn-primary" href="/register">{t.register}</Link> : null}
            </>
          )}
        </div>
      </header>

      <section className="landing-hero">
        <img className="landing-hero-bg" src="/landing-picpilot-studio-v2.webp" alt={t.heroVisualAlt} />
        <div className="landing-hero-shade" />
        <div className="landing-hero-content">
          <div className="landing-eyebrow"><Sparkles size={16} />{t.eyebrow}</div>
          <h1>
            <span className="landing-hero-brand-name">PicPilot</span>
            <span className="landing-hero-category">{t.title.replace(/^PicPilot\s*/, '')}</span>
          </h1>
          <p>{t.subtitle}</p>
          <div className="landing-hero-actions">
            <Link className="btn btn-primary landing-cta" href={primaryHref}>{t.primaryCta}<ArrowRight size={17} /></Link>
            <Link className="btn landing-cta-secondary" href="/templates">{t.secondaryCta}</Link>
          </div>
          <div className="landing-stats" aria-label="PicPilot highlights">
            {[t.heroStatOne, t.heroStatTwo, t.heroStatThree].map(item => <span key={item}>{item}</span>)}
          </div>
        </div>
      </section>


      <section className="landing-section landing-channels" id="product">
        <div className="landing-section-head">
          <h2>{t.channelTitle}</h2>
          <p>{t.channelDesc}</p>
        </div>
        <div className="landing-channel-grid">
          {[
            [t.channelCross, t.channelCrossDesc, t.channelCrossTags],
            [t.channelDomestic, t.channelDomesticDesc, t.channelDomesticTags],
            [t.channelCreative, t.channelCreativeDesc, t.channelCreativeTags]
          ].map(([title, desc, tags], index) => {
            const Icon = channelIcons[index]
            return (
              <Link className="landing-channel-card" href={channelHrefs[index]} key={title}>
                <div className="landing-channel-top">
                  <span className="landing-channel-icon"><Icon size={20} /></span>
                  <small>{tags}</small>
                </div>
                <h3>{title}</h3>
                <p>{desc}</p>
                <span className="landing-channel-enter"><ArrowUpRight size={18} /></span>
              </Link>
            )
          })}
        </div>
      </section>

      <section className="landing-demo-band" id="demo">
        <div className="landing-section landing-demo">
          <div className="landing-section-head">
            <div>
              <span className="landing-demo-eyebrow"><Sparkles size={15} />{t.demoEyebrow}</span>
              <h2>{t.demoTitle}</h2>
            </div>
            <div className="landing-demo-intro">
              <p>{t.demoDesc}</p>
              <Link className="landing-demo-cta" href={primaryHref}>{t.demoCta}<ArrowRight size={16} /></Link>
            </div>
          </div>
          <div className="landing-demo-board">
            <div className="landing-demo-product">
              <div className="demo-product-gallery">
                {demoImages.map(([src, alt], index) => (
                  <button
                    type="button"
                    className={`demo-product-frame demo-product-frame-${index + 1}`}
                    key={src}
                    onClick={() => setPreviewIndex(index)}
                    aria-label={`${t.demoPreview}: ${alt}`}
                  >
                    <img src={src} alt={alt} loading="lazy" decoding="async" />
                    <span className="demo-product-frame-action"><Maximize2 size={15} />{t.demoPreview}</span>
                  </button>
                ))}
                <span className="demo-product-badge">{t.demoCaseBadge}</span>
              </div>
              <div className="demo-product-caption">
                <strong>{t.demoProductName}</strong>
                <span>{t.demoProductMeta}</span>
              </div>
            </div>
            <div className="landing-demo-flow">
              <article className="landing-demo-panel">
                <h3>{t.demoInputTitle}</h3>
                <ul>
                  {[t.demoInput1, t.demoInput2, t.demoInput3].map(item => <li key={item}>{item}</li>)}
                </ul>
              </article>
              <article className="landing-demo-panel demo-prompt-panel">
                <h3>{t.demoPromptTitle}</h3>
                <p>{t.demoPrompt}</p>
              </article>
              <article className="landing-demo-panel">
                <h3>{t.demoOutputTitle}</h3>
                <div className="landing-demo-output-grid">
                  {[t.demoOutput1, t.demoOutput2, t.demoOutput3].map((item, index) => (
                    <div className="landing-demo-output" key={item}>
                      <span>{index + 1}</span>
                      <p>{item}</p>
                    </div>
                  ))}
                </div>
              </article>
            </div>
          </div>
          <div className="landing-demo-highlights">
            {[
              [t.demoQuality, t.demoQualityDesc],
              [t.demoSimple, t.demoSimpleDesc],
              [t.demoReusable, t.demoReusableDesc]
            ].map(([title, desc], index) => (
              <div className="landing-demo-highlight" key={title}>
                <small>0{index + 1}</small>
                <strong>{title}</strong>
                <span>{desc}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="landing-production-band" id="workflow">
        <div className="landing-section landing-production">
          <div className="landing-section-head">
            <div>
              <span className="landing-production-eyebrow">{t.workflow}</span>
              <h2>{t.sectionFeature}</h2>
            </div>
            <p>{t.sectionFeatureDesc}</p>
          </div>
          <div className="landing-production-grid">
            {[
              [t.feature1, t.feature1Desc, t.step1],
              [t.feature2, t.feature2Desc, t.step2],
              [t.feature3, t.feature3Desc, t.step3],
              [t.feature4, t.feature4Desc, t.step4]
            ].map(([title, desc, action], index) => {
              const Icon = featureIcons[index]
              return (
                <article className="landing-production-step" key={title}>
                  <div className="landing-production-step-top">
                    <span className="landing-production-index">0{index + 1}</span>
                    <span className="landing-feature-icon"><Icon size={19} /></span>
                  </div>
                  <h3>{title}</h3>
                  <p>{desc}</p>
                  <strong>{action}</strong>
                </article>
              )
            })}
          </div>
        </div>
      </section>

      <section className="landing-audience-band" id="use-cases">
        <div className="landing-section landing-audience">
          <div className="landing-section-head">
            <h2>{t.useCases}</h2>
            <p>{t.useCasesLead}</p>
          </div>
          <div className="landing-use-grid">
            {[
              [t.case1, t.case1Desc],
              [t.case2, t.case2Desc],
              [t.case3, t.case3Desc]
            ].map(([title, desc], index) => (
              <article className="landing-use-card" key={title}>
                <div className="landing-use-card-top">
                  <span>0{index + 1}</span>
                  <ImagePlus size={18} />
                </div>
                <h3>{title}</h3>
                <p>{desc}</p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section className="landing-final">
        <div>
          <span className="landing-final-eyebrow">{t.finalEyebrow}</span>
          <h2>{t.finalTitle}</h2>
          <p>{t.finalDesc}</p>
        </div>
        <Link className="btn btn-primary landing-cta" href={primaryHref}>{signedIn ? t.workspace : t.finalCta}<ArrowRight size={17} /></Link>
      </section>

      {previewIndex !== null ? (
        <div className="landing-image-lightbox" role="dialog" aria-modal="true" aria-label={t.demoPreview} onClick={() => setPreviewIndex(null)}>
          <div className="landing-image-lightbox-stage" onClick={event => event.stopPropagation()}>
            <button className="landing-lightbox-close" type="button" onClick={() => setPreviewIndex(null)} aria-label={t.demoClose} title={t.demoClose}>
              <X size={20} />
            </button>
            <button
              className="landing-lightbox-nav landing-lightbox-previous"
              type="button"
              onClick={() => setPreviewIndex((previewIndex + demoImages.length - 1) % demoImages.length)}
              aria-label={t.demoPrevious}
              title={t.demoPrevious}
            >
              <ChevronLeft size={24} />
            </button>
            <img src={demoImages[previewIndex][0]} alt={demoImages[previewIndex][1]} />
            <button
              className="landing-lightbox-nav landing-lightbox-next"
              type="button"
              onClick={() => setPreviewIndex((previewIndex + 1) % demoImages.length)}
              aria-label={t.demoNext}
              title={t.demoNext}
            >
              <ChevronRight size={24} />
            </button>
            <div className="landing-lightbox-caption">
              <strong>{String(previewIndex + 1).padStart(2, '0')} / {String(demoImages.length).padStart(2, '0')}</strong>
              <span>{demoImages[previewIndex][1]}</span>
            </div>
          </div>
        </div>
      ) : null}
    </main>
  )
}
