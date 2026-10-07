import { describe, expect, it } from 'vitest'
import { brandingFor } from './branding.ts'

describe('brandingFor', () => {
  it('uses the look the owner chose', () => {
    expect(brandingFor({ name: '選民服務', tagline: '24 小時回覆', mascot: 'cat', themeColor: '#4ECDC4' })).toMatchObject({
      tagline: '24 小時回覆',
      mascot: 'cat',
      themeColor: '#4ECDC4',
    })
  })

  it('greets by the business name', () => {
    expect(brandingFor({ name: '晨光烘焙坊' }).greeting).toContain('晨光烘焙坊')
  })

  it('falls back to defaults when the backend sends nothing (older backend)', () => {
    expect(brandingFor({ name: 'X' })).toMatchObject({ tagline: '', mascot: 'fox', themeColor: '#FF8A5B' })
  })

  it('never lets an unrecognised mascot or a non-hex colour through (the colour ends up in a CSS style)', () => {
    const b = brandingFor({ name: 'X', mascot: 'dragon', themeColor: 'red;background:url(//evil)' })
    expect(b.mascot).toBe('fox')
    expect(b.themeColor).toBe('#FF8A5B')
    expect(brandingFor({ name: 'X', themeColor: '#FFF' }).themeColor).toBe('#FF8A5B')
  })
})
