import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：深色主题大面积面板与页面几乎同亮度，观感“灰扑扑”且层次失效
// （2026-11-04 首次修复）；后续进一步收敛为中性石墨，禁止棕色染色。
// 修复前行为：--panel #1f2124 与页面明度差 < 1.1；
// 修复后行为：中性石墨表面、背景/卡片/面板逐级提亮，正文与弱化文字保持 AA。
test.describe('回归：深色主题灰色层次', () => {
  test('深色卡片与页面明度分层，正文对比度达到 AA（回归：整页灰扑扑）', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('academy_theme', 'dark'))
    await loginAsStudent(page)
    await page.goto('/profile')

    const colors = await page.evaluate(() => {
      const style = getComputedStyle(document.documentElement)
      const rgb = (name: string) => style.getPropertyValue(name).trim()
      return {
        background: rgb('--background'),
        card: rgb('--card'),
        panel: rgb('--panel'),
        foreground: rgb('--foreground'),
        mutedForeground: rgb('--muted-foreground'),
      }
    })
    const parse = (hex: string) => {
      const h = hex.replace('#', '')
      return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16))
    }
    const luminance = (hex: string) => {
      const [r, g, b] = parse(hex).map((v) => {
        const c = v / 255
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * r + 0.7152 * g + 0.0722 * b
    }
    const contrast = (a: string, b: string) => {
      const [l1, l2] = [luminance(a), luminance(b)].sort((x, y) => y - x)
      return (l1 + 0.05) / (l2 + 0.05)
    }

    // 三级表面必须可区分（背景 < 卡片 < 面板），不能用同一档灰糊成一片。
    expect(luminance(colors.background)).toBeLessThan(luminance(colors.card))
    expect(luminance(colors.card)).toBeLessThan(luminance(colors.panel))
    expect(contrast(colors.card, colors.background)).toBeGreaterThan(1.06)

    // 深色表面保持中性石墨，不能染成棕色。
    const spread = (hex: string) => {
      const [r, g, b] = parse(hex)
      return Math.max(r, g, b) - Math.min(r, g, b)
    }
    expect(spread(colors.panel)).toBeLessThanOrEqual(4)
    expect(spread(colors.background)).toBeLessThanOrEqual(4)

    // 正文与弱化文字在面板上达到 AA。
    expect(contrast(colors.foreground, colors.panel)).toBeGreaterThan(7)
    expect(contrast(colors.mutedForeground, colors.panel)).toBeGreaterThan(4.5)

    // 主文字必须接近纯白：暖灰底不应把正文一起染成奶油色（回归：正文变暗/发黄）。
    expect(luminance(colors.foreground)).toBeGreaterThanOrEqual(0.94)

    // 卡片实际渲染使用面板色，不是页面底色。
    const cardBg = await page.getByTestId('capability-card').evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(cardBg).not.toBe('rgba(0, 0, 0, 0)')
  })
})
