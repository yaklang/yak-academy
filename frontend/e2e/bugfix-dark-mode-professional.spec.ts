import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：深色主题被大面积暖棕色染色、业务页 hero 使用极光光斑与无限旋转装饰，
// 整页像做旧照片而不是专业深色界面（2026-11-04 修复）。
// 修复前行为：--panel #23211d（RGB 极差 6）、相邻表面亮度比 < 1.15，
//   /learn hero 渲染 .aurora-blur，轨道环带 60s 无限旋转。
// 修复后行为：中性石墨表面（RGB 极差 ≤ 4）、相邻表面亮度比 ≥ 1.15，
//   业务页不渲染装饰光斑，装饰不使用无限动画。
test.describe('回归：深色模式专业度', () => {
  test('深色表面为中性石墨且层级可辨，业务页不使用极光与无限旋转', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('academy_theme', 'dark'))
    await loginAsStudent(page)
    await page.goto('/learn')

    const colors = await page.evaluate(() => {
      const style = getComputedStyle(document.documentElement)
      const token = (name: string) => style.getPropertyValue(name).trim()
      return {
        background: token('--background'),
        card: token('--card'),
        panel: token('--panel'),
        popover: token('--popover'),
        border: token('--border'),
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
    const spread = (hex: string) => {
      const [r, g, b] = parse(hex)
      return Math.max(r, g, b) - Math.min(r, g, b)
    }

    // 大面积表面保持中性石墨：允许极轻暖调，但不能染成棕色/橙色。
    for (const hex of [colors.background, colors.card, colors.panel, colors.popover]) {
      expect(spread(hex), `${hex} 应保持中性`).toBeLessThanOrEqual(4)
    }

    // 层级必须靠明度拉开，相邻表面亮度比 ≥ 1.15。
    expect(contrast(colors.background, colors.card)).toBeGreaterThanOrEqual(1.15)
    expect(contrast(colors.card, colors.panel)).toBeGreaterThanOrEqual(1.15)
    expect(contrast(colors.panel, colors.popover)).toBeGreaterThanOrEqual(1.15)

    // 边框要能看清，不能退化成装饰。
    expect(contrast(colors.border, colors.panel)).toBeGreaterThanOrEqual(1.3)

    // 业务页欢迎区不允许装饰性模糊光斑与无限循环动画。
    await expect(page.locator('.learn-hero .aurora-blur')).toHaveCount(0)
    const decorations = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('.learn-hero *')]
      return {
        animations: nodes
          .map((el) => getComputedStyle(el).animationName)
          .filter((name) => name !== 'none'),
        blurred: nodes
          .map((el) => getComputedStyle(el).filter)
          .filter((filter) => filter.includes('blur(')),
      }
    })
    expect(decorations.animations).toEqual([])
    expect(decorations.blurred).toEqual([])

    // 同一页面的卡片必须使用同一层表面，不能一块浅一块深。
    const surfaces = await page.evaluate(() => {
      const bg = (selector: string) => {
        const el = document.querySelector(selector)
        return el ? getComputedStyle(el).backgroundColor : null
      }
      return { topic: bg('.topic-card'), quick: bg('.quick-card') }
    })
    expect(surfaces.topic).toBe(surfaces.quick)
  })
})
