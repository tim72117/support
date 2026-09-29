// Fake data for the front-end-first build. No network calls yet — every
// "save" just updates in-memory state (see MockBackendContext.tsx). Once
// the real /auth + /console API is ready, this file's shapes are what get
// swapped for fetch calls; keep them close to what the Go backend already
// models (see backend/internal/business) so that swap stays mechanical.

/**
 * One content section a business owner fills in. This is presentation-only
 * structure — the backend still stores `business_content` as a single text
 * field (see backend/internal/business/business.go). buildContentText()
 * below is what flattens these sections back into that one string before
 * "saving".
 */
export interface ContentSection {
  id: string
  /** Plain-language label shown to the owner, e.g. "營業時間". */
  title: string
  /** One line of guidance under the title, e.g. "例如：週一到週五 9:00–18:00". */
  placeholder: string
  /** What the owner typed. Empty string = not filled in yet. */
  body: string
}

export interface Business {
  id: string
  name: string
  slug: string
  /** Short one-line description shown in the business list. */
  tagline: string
  /** Accent color for this business's mascot/branding (hex). */
  themeColor: string
  mascot: MascotId
  sections: ContentSection[]
  /** Whether this business has ever been connected to onagent. Mock-only flag. */
  connected: boolean
}

export type MascotId = 'fox' | 'bear' | 'cat' | 'bird'

export const MASCOT_LABELS: Record<MascotId, string> = {
  fox: '小狐狸',
  bear: '小熊',
  cat: '小貓',
  bird: '小鳥',
}

export const THEME_COLORS = ['#FF8A5B', '#4ECDC4', '#8E7DFF', '#FFB84C', '#4C9EFF'] as const

function defaultSections(): ContentSection[] {
  return [
    {
      id: 'hours',
      title: '營業時間',
      placeholder: '例如：週一到週五 9:00–18:00，週末公休',
      body: '',
    },
    {
      id: 'contact',
      title: '聯絡方式與地址',
      placeholder: '例如：門市地址、電話、Line 官方帳號',
      body: '',
    },
    {
      id: 'products',
      title: '產品或服務介紹',
      placeholder: '例如：你們賣什麼、有哪些方案、價格怎麼算',
      body: '',
    },
    {
      id: 'policy',
      title: '退換貨與訂單規則',
      placeholder: '例如：幾天內可退換、運費怎麼算、訂單怎麼取消',
      body: '',
    },
    {
      id: 'faq',
      title: '常見問題',
      placeholder: '例如：客人最常問的問題，以及你希望 AI 怎麼回答',
      body: '',
    },
    {
      id: 'other',
      title: '其他你想讓 AI 知道的事',
      placeholder: '任何前面沒提到、但希望 AI 回答顧客時可以參考的內容',
      body: '',
    },
  ]
}

export function createBlankBusiness(overrides: Partial<Business> = {}): Business {
  return {
    id: `biz_${Math.random().toString(36).slice(2, 9)}`,
    name: '',
    slug: '',
    tagline: '',
    themeColor: THEME_COLORS[0],
    mascot: 'fox',
    sections: defaultSections(),
    connected: false,
    ...overrides,
  }
}

/** Flattens a business's sections into the single text blob the backend stores. */
export function buildContentText(sections: ContentSection[]): string {
  return sections
    .filter((s) => s.body.trim().length > 0)
    .map((s) => `【${s.title}】\n${s.body.trim()}`)
    .join('\n\n')
}

export function seedBusinesses(): Business[] {
  const bakery = createBlankBusiness({
    id: 'biz_bakery',
    name: '晨光烘焙坊',
    slug: 'chenguang-bakery',
    tagline: '天然酵母麵包與客製化生日蛋糕',
    themeColor: '#FF8A5B',
    mascot: 'bear',
    connected: true,
  })
  bakery.sections = bakery.sections.map((s) => {
    if (s.id === 'hours') return { ...s, body: '週二到週日 8:00–19:00，週一公休' }
    if (s.id === 'contact')
      return { ...s, body: '台北市大安區忠孝東路四段 200 號\n電話：02-2345-6789\nLine ID：@chenguang' }
    if (s.id === 'products')
      return {
        ...s,
        body: '手工歐式麵包、每日限量可頌、客製化生日蛋糕（需提前 3 天預訂）',
      }
    if (s.id === 'policy')
      return { ...s, body: '客製化蛋糕一經訂購不接受退換，一般麵包若當天發現品質問題可到店退換' }
    if (s.id === 'faq')
      return { ...s, body: '常有人問「可以宅配嗎」——目前僅供台北市內外送，滿 500 元免運' }
    return s
  })

  const studio = createBlankBusiness({
    id: 'biz_studio',
    name: '毛孩美容工作室',
    slug: 'furry-studio',
    tagline: '寵物洗澡、美容與寄宿服務',
    themeColor: '#4ECDC4',
    mascot: 'cat',
    connected: false,
  })

  return [bakery, studio]
}
