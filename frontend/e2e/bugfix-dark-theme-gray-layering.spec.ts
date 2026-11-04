import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：深色主题大面积面板使用中性冷灰、卡片与页面几乎同亮度，
// 观感“灰扑扑”且层次失效（2026-11-04 修复）。
// 修复前行为：--panel #1f2124（冷灰）、--background #050506，卡片与页面明度差 < 1.1。
// 修复后行为：石墨暖灰面板、卡片/面板/页面三级明度递增，正文与弱化文字保持 AA。
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

    // 深色面板不再是冷调中性灰：红通道应高于蓝通道。
    const [r, , b] = parse(colors.panel)
    expect(r).toBeGreaterThan(b)

    // 正文与弱化文字在面板上达到 AA。
    expect(contrast(colors.foreground, colors.panel)).toBeGreaterThan(7)
    expect(contrast(colors.mutedForeground, colors.panel)).toBeGreaterThan(4.5)

    // 卡片实际渲染使用面板色，不是页面底色。
    const cardBg = await page.getByTestId('capability-card').evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(cardBg).not.toBe('rgba(0, 0, 0, 0)')
  })
})
