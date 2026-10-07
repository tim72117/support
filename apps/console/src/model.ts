// Shapes and helpers for what the owner edits in the console. Everything
// here is plain data/logic with no network and no fake records — the data
// itself always comes from the backend (see BackendContext.tsx).

export type MascotId = 'fox' | 'bear' | 'cat' | 'bird'

export const MASCOT_LABELS: Record<MascotId, string> = {
  fox: '小狐狸',
  bear: '小熊',
  cat: '小貓',
  bird: '小鳥',
}

export const THEME_COLORS = ['#FF8A5B', '#4ECDC4', '#8E7DFF', '#FFB84C', '#4C9EFF'] as const

/** One business (a consumer-facing support page) as the backend stores it. */
export interface Business {
  id: number
  name: string
  /** The public URL segment (/support/<slug>). Fixed once created. */
  slug: string
  /** Short one-line description shown in the business list. */
  tagline: string
  themeColor: string
  mascot: MascotId
  /** True once the business has an onagent app, i.e. its page can chat. */
  connected: boolean
}

/** The look settings an owner can change after creation (slug is not one of them). */
export type BusinessPatch = Partial<Pick<Business, 'name' | 'tagline' | 'mascot' | 'themeColor'>>

export interface NewBusinessInput {
  slug: string
  name: string
  tagline?: string
  mascot: MascotId
  themeColor: string
}

/**
 * One content section the owner fills in. The backend keeps the flat text the
 * AI reads (`content`) and, next to it, these sections verbatim so the editor
 * can show them again; buildContentText() produces the former from the latter.
 */
export interface ContentSection {
  id: string
  /** Plain-language label shown to the owner, e.g. "營業時間". */
  title: string
  /** One line of guidance under the title. */
  placeholder: string
  /** What the owner typed. Empty string = not filled in yet. */
  body: string
}

export function defaultSections(): ContentSection[] {
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

/** Flattens the sections into the single text the backend hands to the AI. */
export function buildContentText(sections: ContentSection[]): string {
  return sections
    .filter((s) => s.body.trim().length > 0)
    .map((s) => `【${s.title}】\n${s.body.trim()}`)
    .join('\n\n')
}

/**
 * Rebuilds the editor's sections from what the backend returned. Saved
 * sections are matched to the current defaults by id (so a renamed title or
 * a new default section still works); anything unrecognised is ignored. If
 * the backend has text but no saved sections (content written through the API
 * directly), that text is kept in the catch-all "other" section instead of
 * being silently dropped.
 */
export function sectionsFromBackend(saved: unknown, flatContent: string): ContentSection[] {
  const sections = defaultSections()
  if (Array.isArray(saved)) {
    for (const item of saved) {
      if (item && typeof item === 'object') {
        const { id, body } = item as { id?: unknown; body?: unknown }
        const target = sections.find((s) => s.id === id)
        if (target && typeof body === 'string') target.body = body
      }
    }
    return sections
  }
  if (flatContent.trim().length > 0) {
    const other = sections.find((s) => s.id === 'other')
    if (other) other.body = flatContent
  }
  return sections
}

/** The compact form that is stored next to the content. */
export function sectionsForBackend(sections: ContentSection[]): { id: string; title: string; body: string }[] {
  return sections.map(({ id, title, body }) => ({ id, title, body }))
}

// --- URL slug ---------------------------------------------------------------

/** Same rule the backend enforces: lowercase letters, digits, hyphens. */
export const SLUG_RE = /^[a-z0-9][a-z0-9-]*$/
export const SLUG_MAX_LENGTH = 40

/**
 * A URL suggestion derived from a name. ASCII only: a Chinese-only name gives
 * '' (the owner then types one), because the backend rejects non-ASCII slugs
 * — they break when pasted into chat apps and are easy to impersonate.
 */
export function suggestSlug(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, SLUG_MAX_LENGTH)
    .replace(/-+$/g, '')
}

export function isValidSlug(slug: string): boolean {
  return slug.length <= SLUG_MAX_LENGTH && SLUG_RE.test(slug)
}
