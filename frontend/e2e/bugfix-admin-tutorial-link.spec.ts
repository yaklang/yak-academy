import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 回归：管理端题目详情「配套教程」链接指向不存在的路由导致黑页（PR #22，2026-10-19 修复）。
// 修复前行为：链接拼成 /admin/topics/<topic>/rooms/<room>——该路由只在学生端存在，
//   vue-router 匹配不到 → 整页空白（暗色主题下即"黑页"）。
// 修复后行为：链接拼成 /admin/tutorials/<topic>/rooms/<room>，点击后渲染教程正文。
// 复现路径：管理员在管理端题库预览（/admin/training/<关联教程的题目>）点「配套教程」。
test.describe('回归：管理端配套教程链接黑页', () => {
  test('链接指向 /admin/tutorials 且点击后渲染教程正文（回归：黑页）', async ({ page }) => {
    await loginAsAdmin(page)

    // 触发点：管理端题库详情页的配套教程链接（linkedSlug 种子题带 tutorial_slug）
    await page.goto(`/admin/training/${E2E.challenge.linkedSlug}`)
    const link = page.getByRole('link', { name: /配套教程/ })
    await expect(link).toHaveAttribute(
      'href',
      `/admin/tutorials/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`,
    )

    // 用户路径：点击链接进入教程页——修复前此处落进无匹配路由的黑页
    await link.click()
    await expect(page).toHaveURL(
      `/admin/tutorials/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`,
    )
    // 终态断言：正文真实渲染（标题可见 + 教程容器非空），而非"页面没崩"
    await expect(
      page.getByRole('heading', { name: E2E.tutorial.roomTitle }),
    ).toBeVisible()
    await expect(page.locator('.lesson')).not.toBeEmpty()
  })
})