import type { MascotId } from './Mascot.tsx'

// Stand-in for a real lookup of "business by slug" (would hit the
// ai-support backend to resolve slug -> onagent_app_id/api_key before
// handing off to @onagent/bridge). This demo page only ever needs to
// resolve branding + a canned opening message; the actual conversation
// once connected is entirely onagent's concern, not this app's.

export interface DemoBusiness {
  slug: string
  name: string
  tagline: string
  themeColor: string
  mascot: MascotId
  greeting: string
  suggestedQuestions: string[]
}

const DEMO_BUSINESSES: DemoBusiness[] = [
  {
    slug: 'chenguang-bakery',
    name: '晨光烘焙坊',
    tagline: '天然酵母麵包與客製化生日蛋糕',
    themeColor: '#FF8A5B',
    mascot: 'bear',
    greeting: '嗨，我是晨光烘焙坊的小幫手！有什麼想問的都可以跟我說喔 🍞',
    suggestedQuestions: ['你們今天有營業嗎？', '生日蛋糕怎麼訂？', '可以宅配到府嗎？'],
  },
  {
    slug: 'furry-studio',
    name: '毛孩美容工作室',
    tagline: '寵物洗澡、美容與寄宿服務',
    themeColor: '#4ECDC4',
    mascot: 'cat',
    greeting: '喵～歡迎光臨毛孩美容工作室，我可以幫你回答預約跟服務項目的問題！',
    suggestedQuestions: ['洗澡要多少錢？', '可以寄宿嗎？', '要先預約嗎？'],
  },
]

export function findBusinessBySlug(slug: string): DemoBusiness | undefined {
  return DEMO_BUSINESSES.find((b) => b.slug === slug)
}

export const DEFAULT_DEMO_SLUG = DEMO_BUSINESSES[0].slug
