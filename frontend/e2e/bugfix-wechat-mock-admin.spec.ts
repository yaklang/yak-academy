import { expect, test } from '@playwright/test'

// 回归：Mock 模式登录后只能得到学员账号，管理员无法用模拟登录进入管理端
// （PR #55，2026-11-25 修复）。
// 修复前行为：登录页只有「微信 Mock」学员入口；点击后首次授权自动建 student，
// 访问 /admin/panel/* 被路由守卫重定向回学习端，管理端不可达。
// 修复后行为：服务端显式配置 ACADEMY_WECHAT_MOCK_ADMIN_USERNAME 时，登录页
// 出现「以管理员身份模拟登录」入口；点击后登录该既有 admin 账号并进入管理端。
// 复现路径：本地/内网用 ACADEMY_WECHAT_MOCK=1 部署 → 打开 /login → 点微信登录
// → 进入 /learn 后发现没有管理入口。
test.describe('回归：Mock 登录不是管理员账号', () => {
  test('Mock 管理员入口登录既有 admin 账号并进入管理端', async ({ page }) => {
    await page.goto('/login')

    // Mock 模式同时提供学员与管理员两个模拟入口；管理员入口来自显式配置。
    const adminButton = page.getByRole('button', { name: '以管理员身份模拟登录' })
    await expect(adminButton).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText(/管理员入口使用已配置的管理员账号/)).toBeVisible()

    await adminButton.click()
    // 模拟授权回跳 → callback 签发 admin token → 目标 /admin → 管理端首页。
    await page.waitForURL((url) => url.pathname.startsWith('/admin'), { timeout: 15_000 })
    await expect(page.getByRole('button', { name: /admin（管理员）/ })).toBeVisible()

    // 业务终态：管理端内容管理页可打开，而不是被守卫弹回学习端。
    await page.goto('/admin/panel/tutorials')
    await expect(page.getByRole('heading', { level: 1, name: '教程管理' })).toBeVisible()
    await expect(page.getByRole('link', { name: '题目管理' })).toBeVisible()
  })
})
