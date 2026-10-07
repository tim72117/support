import { describe, expect, it } from 'vitest'
import {
  buildContentText,
  defaultSections,
  isValidSlug,
  sectionsForBackend,
  sectionsFromBackend,
  suggestSlug,
} from './model.ts'

describe('suggestSlug', () => {
  it('derives a lowercase ASCII URL', () => {
    expect(suggestSlug('Demo Shop')).toBe('demo-shop')
    expect(suggestSlug('  Chen Guang Bakery!  ')).toBe('chen-guang-bakery')
    expect(suggestSlug('shop_2024 / main')).toBe('shop-2024-main')
  })

  it('gives nothing for a name with no ASCII letters or digits (the owner types one)', () => {
    expect(suggestSlug('選民服務')).toBe('')
    expect(suggestSlug('   ')).toBe('')
    expect(suggestSlug('！！！')).toBe('')
  })

  it('keeps the ASCII part of a mixed name', () => {
    expect(suggestSlug('晨光 Bakery 2號')).toBe('bakery-2')
  })

  it('never exceeds the length limit and never ends with a hyphen', () => {
    const slug = suggestSlug('a'.repeat(39) + ' b')
    expect(slug.length).toBeLessThanOrEqual(40)
    expect(slug.endsWith('-')).toBe(false)
    expect(isValidSlug(suggestSlug('x'.repeat(100)))).toBe(true)
  })
})

describe('isValidSlug (mirrors the backend rule)', () => {
  it('accepts lowercase letters, digits and inner hyphens', () => {
    for (const ok of ['a', '0', 'voter-service', 'shop2', 'a-b-c', 'a'.repeat(40)]) expect(isValidSlug(ok)).toBe(true)
  })
  it('rejects everything else', () => {
    for (const bad of ['', '-a', 'A', 'has space', 'a/b', 'a_b', '選民', 'a.b', 'a'.repeat(41)]) {
      expect(isValidSlug(bad)).toBe(false)
    }
  })
})

describe('content sections', () => {
  it('buildContentText joins only the filled sections, trimmed, with their titles', () => {
    const s = defaultSections()
    s[0].body = '  週一到週五  '
    s[4].body = '可以宅配嗎？可以。'
    expect(buildContentText(s)).toBe('【營業時間】\n週一到週五\n\n【常見問題】\n可以宅配嗎？可以。')
    expect(buildContentText(defaultSections())).toBe('')
  })

  it('sectionsFromBackend restores saved bodies by id and ignores junk', () => {
    const restored = sectionsFromBackend(
      [
        { id: 'hours', title: '舊標題', body: '9–5' },
        { id: 'does-not-exist', body: 'dropped' },
        { id: 'faq', body: 123 },
        null,
        'x',
      ],
      'ignored when sections exist',
    )
    expect(restored.find((s) => s.id === 'hours')?.body).toBe('9–5')
    expect(restored.find((s) => s.id === 'hours')?.title).toBe('營業時間') // current title wins
    expect(restored.find((s) => s.id === 'faq')?.body).toBe('')
    expect(restored.map((s) => s.id)).toEqual(defaultSections().map((s) => s.id))
  })

  it('keeps flat text that has no saved sections in the catch-all section', () => {
    const restored = sectionsFromBackend(null, '直接寫入的內容')
    expect(restored.find((s) => s.id === 'other')?.body).toBe('直接寫入的內容')
    expect(restored.filter((s) => s.body).length).toBe(1)
    expect(sectionsFromBackend(null, '   ').every((s) => s.body === '')).toBe(true)
    expect(sectionsFromBackend('not an array', '').every((s) => s.body === '')).toBe(true)
  })

  it('round-trips through the backend form', () => {
    const s = defaultSections()
    s[1].body = '台北市大安區'
    const back = sectionsFromBackend(sectionsForBackend(s), buildContentText(s))
    expect(back).toEqual(s)
    expect(sectionsForBackend(s)[0]).toEqual({ id: 'hours', title: '營業時間', body: '' }) // placeholders are not stored
  })
})
