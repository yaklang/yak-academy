import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// UI 改版（CTF 平台布局）窄屏守护：移动端视口下页面不得出现横向滚动。
// 回归背景：顶栏 XP chips + 操作区单行不折行、页脚 width:100% 叠加 content-box
// padding 撑宽、排行榜领奖台三列 min-width 合计超宽，三者都曾把 scrollWidth
// 撑出 390px 视口；此处对登录页与登录后三个主页面做零溢出断言。
test.describe('窄屏响应式布局', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('移动端视口下主要页面无横向溢出', async ({ page }) => {
    // 登录页：分屏 hero 在 ≤880px 折为单列，仍不得横向滚动
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: 'yak-academy 登录' })).toBeVisible()
    await expectNoOverflow(page, '/login')

    // 登录后：顶栏（≤960px 收起 chips/用户名，≤640px 主导航折行）与内容区
    await loginAsAdmin(page)
    for (const path of ['/', '/training', '/leaderboard']) {
      await page.goto(path)
      await expectNoOverflow(page, path)
    }
  })
})

async function expectNoOverflow(page: import('@playwright/test').Page, path: string) {
  // 等一帧路由渲染完成再量（卡片网格/领奖台为同步渲染，DOM 就绪即可）
  await page.waitForLoadState('domcontentloaded')
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )
  expect(overflow, `${path} 在 390px 视口不应横向溢出`).toBeLessThanOrEqual(0)
}
