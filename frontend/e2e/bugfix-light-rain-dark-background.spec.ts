import { expect, test } from '@playwright/test'

// 回归：浅色主题下雨幕画布被整块刷成不透明黑底（2026-11-02 修复）。
// 修复前行为：TerminalRain 的 ink 是 Boolean prop，未显式传入时 Vue 将其
// 转为 false，palette 恒走 dark 分支，用 rgba(12,12,13) 做整屏 wash，
// 反复叠加后 canvas 变成不透明黑板，浅色主题 hero 正文对比度被破坏。
// 修复后行为：canvas 始终透明（destination-out 擦除旧字形），雨丝是深橙
// 墨色，页面浅色背景透出；不透明像素仅来自字形本身。
// 复现路径：切到浅色主题打开首页 → hero 卡片整块呈深色背景。
test.describe('回归：浅色主题雨幕黑底', () => {
  // reduced-motion 下组件只画一帧静态雨：像素状态确定，便于断言画布底色。
  test.use({ reducedMotion: 'reduce' })

  async function opaqueRatio(canvas: import('@playwright/test').Locator) {
    return canvas.evaluate((el: HTMLCanvasElement) => {
      const ctx = el.getContext('2d')!
      const { data } = ctx.getImageData(0, 0, el.width, el.height)
      let opaque = 0
      const total = data.length / 4
      for (let i = 3; i < data.length; i += 4) if (data[i] > 200) opaque++
      return opaque / total
    })
  }

  test('浅色主题首页 hero 雨幕保持透明、不再是黑底（回归：黑底）', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('academy_theme', 'light'))
    await page.goto('/')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    const canvas = page.locator('.hero-rain canvas')
    await expect(canvas).toBeVisible()
    // 旧实现整屏 wash 不透明度收敛到 1，opaqueRatio ≈ 1；
    // 新实现只有雨丝字形着色，绝大部分像素透明。
    expect(await opaqueRatio(canvas)).toBeLessThan(0.35)
  })

  test('浅色主题登录页故事区雨幕保持透明、不再是黑底（回归：黑底）', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('academy_theme', 'light'))
    await page.goto('/login')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible()
    const canvas = page.locator('.auth-rain canvas')
    await expect(canvas).toBeVisible()
    expect(await opaqueRatio(canvas)).toBeLessThan(0.35)
  })
})
