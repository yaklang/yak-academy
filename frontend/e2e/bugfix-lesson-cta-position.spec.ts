import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：教程页“前往配套题目练习”曾横跨整页、悬在 1440px 视口最右侧，
// 与阅读内容脱节。新外壳把教程页限制在阅读宽度内，入口必须留在该壳层内。
test.describe('回归：教程配套练习入口', () => {
  test('配套练习入口留在阅读壳层内，不贴视口右缘', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsStudent(page)
    await page.goto(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`)

    const shell = page.locator('.room-page')
    const action = page.getByRole('link', { name: /前往配套题目练习/ })
    await expect(action).toBeVisible()

    // 路由 chunk 的 scoped CSS 可能晚于 DOM 注入；等阅读壳层宽度收敛后再量。
    await expect.poll(async () => (await shell.boundingBox())?.width ?? Infinity).toBeLessThanOrEqual(1040)
    const shellBox = await shell.boundingBox()
    const actionBox = await action.boundingBox()
    if (!shellBox || !actionBox) throw new Error('lesson header is not visible')

    // 入口右边缘与壳层右边缘对齐，不越过视口留白。
    expect(Math.abs(actionBox.x + actionBox.width - (shellBox.x + shellBox.width))).toBeLessThanOrEqual(1)
  })
})
