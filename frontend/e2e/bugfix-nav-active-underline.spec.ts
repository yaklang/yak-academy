import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：顶栏主导航「教程」在非首页路由下仍保持绿色下划线高亮（PR #25，2026-10-19 修复）。
// 修复前行为：「教程」链接指向根路径 "/"，高亮样式挂在 router-link-active（前缀匹配）上，
//   "/" 是一切路由的前缀，导致进了题库/排行榜后「教程」依旧亮着绿色下划线。
// 修复后行为：导航高亮改挂精确匹配类 main-nav-exact（router-link-exact-active），
//   只有当前路由与链接路径完全一致时才点亮。
// 复现路径：学生登录 → 点击顶栏「题库」→ 观察顶栏「教程」是否仍有绿色下划线。
test.describe('回归：主导航高亮精确匹配', () => {
  test('进题库后只有「题库」高亮，「教程」下划线消失；回首页「教程」恢复高亮（回归：根路径前缀误亮）', async ({ page }) => {
    await loginAsStudent(page)

    // 从顶栏导航真实点击进入题库（用户视角路径，不直接 goto）
    await page.getByRole('link', { name: '题库' }).click()
    await expect(page.getByRole('heading', { name: '题库' })).toBeVisible()

    const tutorials = page.locator('.main-nav a', { hasText: '教程' })
    const training = page.locator('.main-nav a', { hasText: '题库' })

    // 修复后的正确终态：题库高亮，教程不高亮（修复前教程因 "/" 前缀匹配而错误高亮）
    await expect(training).toHaveClass(/main-nav-exact/)
    await expect(tutorials).not.toHaveClass(/main-nav-exact/)
    // 高亮样式即绿色下划线的来源：非激活项下划线必须保持透明
    await expect(tutorials).toHaveCSS('border-bottom-color', 'rgba(0, 0, 0, 0)')

    // 返回首页：教程（根路径）恢复精确高亮——防止修复把根路径自身的高亮也抹掉
    await page.getByRole('link', { name: '教程' }).click()
    await expect(page.getByRole('heading', { name: '学习路线' })).toBeVisible()
    await expect(tutorials).toHaveClass(/main-nav-exact/)
    await expect(training).not.toHaveClass(/main-nav-exact/)
  })
})