import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：教程页“前往配套题目练习”曾横跨整页、悬在 1440px 视口最右侧，
// 与阅读内容脱节。新外壳把教程页限制在阅读宽度内，入口必须留在该壳层内。
// PR #56（2026-11-26）补充：布局（AppShell）与路由（RoomDetail）都是懒加载
// chunk，两者样式注入顺序不固定；外壳的 `.app-content > * { max-width: 100% }`
// 与阅读页的 max-width 特异度相同，注入顺序反转时后者被覆盖，阅读壳层被拉满
// 整页（实测 1104px），入口重新贴到视口右缘。修复改为显式 width，
// 本用例先强制“外壳规则在后”的最坏注入顺序，再断言宽度与对齐。
// 复现路径：学生登录 → 直接打开带关联题目的教程章节。
test.describe('回归：教程配套练习入口', () => {
  test('配套练习入口留在阅读壳层内，不贴视口右缘（回归：样式注入顺序反转时壳层被拉满）', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsStudent(page)
    await page.goto(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`)

    const shell = page.locator('.room-page')
    const action = page.getByRole('link', { name: /前往配套题目练习/ })
    await expect(action).toBeVisible()

    // 等布局与路由两个 chunk 的 scoped CSS 都注入后，把外壳规则挪到阅读页
    // 规则之后，固定复现最坏注入顺序（等价于布局 chunk 后加载）。
    await expect
      .poll(() =>
        page.evaluate(() => {
          const styles = [...document.querySelectorAll('style')]
          return {
            room: styles.some((s) => s.textContent?.includes('.room-page[')),
            shell: styles.some((s) => s.textContent?.includes('.app-content[')),
          }
        }),
      )
      .toEqual({ room: true, shell: true })
    await page.evaluate(() => {
      const styles = [...document.querySelectorAll('style')]
      const roomStyle = styles.find((s) => s.textContent?.includes('.room-page['))
      const shellStyle = styles.find((s) => s.textContent?.includes('.app-content['))
      if (roomStyle && shellStyle && shellStyle.parentNode) {
        shellStyle.parentNode.insertBefore(roomStyle, shellStyle)
      }
    })

    await expect.poll(async () => (await shell.boundingBox())?.width ?? Infinity).toBeLessThanOrEqual(1040)
    const shellBox = await shell.boundingBox()
    const actionBox = await action.boundingBox()
    if (!shellBox || !actionBox) throw new Error('lesson header is not visible')

    // 入口右边缘与壳层右边缘对齐，不越过视口留白。
    expect(Math.abs(actionBox.x + actionBox.width - (shellBox.x + shellBox.width))).toBeLessThanOrEqual(1)
  })
})
