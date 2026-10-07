import type { PublicBusiness } from './api.ts'
import type { MascotId } from './Mascot.tsx'

// How the page looks. The owner chooses name, tagline, mascot and colour in
// the console; they arrive with the public business record. Anything missing
// or unrecognised falls back to a default so a bad value can never break the
// page (the colour in particular ends up in a CSS style).
export type LayoutId = 'center' | 'split'

export interface Branding {
  tagline: string
  themeColor: string
  mascot: MascotId
  layout: LayoutId
  greeting: string
  suggestedQuestions: string[]
}

const MASCOTS: MascotId[] = ['fox', 'bear', 'cat', 'bird', 'rabbit', 'dog', 'owl', 'penguin', 'panda', 'pig']
const LAYOUTS: LayoutId[] = ['center', 'split']
const DEFAULT_COLOR = '#FF8A5B'
const COLOR_RE = /^#[0-9a-fA-F]{6}$/

export function brandingFor(
  b: Pick<PublicBusiness, 'name' | 'tagline' | 'mascot' | 'themeColor' | 'layout'>,
): Branding {
  return {
    tagline: b.tagline ?? '',
    themeColor: b.themeColor && COLOR_RE.test(b.themeColor) ? b.themeColor : DEFAULT_COLOR,
    mascot: b.mascot && (MASCOTS as string[]).includes(b.mascot) ? (b.mascot as MascotId) : 'fox',
    layout: b.layout && (LAYOUTS as string[]).includes(b.layout) ? (b.layout as LayoutId) : 'center',
    greeting: `嗨，我是 ${b.name} 的 AI 小幫手！有什麼想問的都可以跟我說。`,
    suggestedQuestions: [],
  }
}
