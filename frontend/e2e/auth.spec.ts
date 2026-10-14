import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent, logout } from './helpers'

// 登录与角色可见性旅程：错误凭据、student 无管理入口、admin 有、
// 登出吊销 token（后端撤销表 + 前端清理本地态）。
test.describe('认证旅程', () => {
  test('错误密码停留在登录页并提示', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill('wrong-password')
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.locator('.el-message').first()).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })

  test('student 登录：核心导航可达、无管理入口', async ({ page }) => {
    await loginAsStudent(page)
    await expect(page.getByRole('link', { name: '教程' })).toBeVisible()
    await expect(page.getByRole('link', { name: '训练' })).toBeVisible()
    await expect(page.getByRole('link', { name: '进入管理端' })).toHaveCount(0)
    // 普通人直入管理路由被守卫弹回普通端
    await page.goto('/admin/panel/users')
    await expect(page).not.toHaveURL(/\/admin/)
  })

  test('admin 登录：有管理入口且可进入管理端', async ({ page }) => {
    await loginAsAdmin(page)
    await expect(page.getByRole('link', { name: '进入管理端' })).toBeVisible()
    await page.goto('/admin/panel/users')
    await expect(page).toHaveURL(/\/admin\/panel\/users/)
  })

  test('登出后旧 token 失效（API 401）', async ({ page, request }) => {
    await loginAsStudent(page)
    const token = await page.evaluate(() => localStorage.getItem('academy_token'))
    expect(token).toBeTruthy()
    await logout(page)
    // 吊销写入后端撤销表：拿旧 token 直连 API 必 401。
    // 用 poll 重发请求，吸收高负载下首次探测与吊销落库的竞态（曾偶发）。
    await expect
      .poll(async () => {
        const resp = await request.get(`${E2E.apiURL}/api/v1/me`, {
          headers: { Authorization: `Bearer ${token}` },
        })
        return resp.status()
      }, { timeout: 5_000 })
      .toBe(401)
  })
})
