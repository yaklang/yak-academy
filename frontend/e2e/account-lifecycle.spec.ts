import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 账号生命周期旅程：学生改密（旧 token 失效、新密码可登录）与管理员重置密码
// （目标用户全部会话吊销）。token 吊销为后端撤销表 + 前端清理本地态双保险。

test.describe('账号生命周期', () => {
  test('学生改密后旧 token 失效，新密码可登录', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill(E2E.student.password)
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    const oldToken = await page.evaluate(() => localStorage.getItem('academy_token'))

    // 改密弹窗：当前密码 → 新密码 → 确认（新密码按 placeholder 定位避免「确认新密码」歧义）
    await page.getByRole('button', { name: '改密码' }).click()
    const dialog = page.locator('.el-dialog', { hasText: '修改密码' })
    await dialog.locator('.el-form-item').filter({ hasText: '当前密码' }).locator('input').fill(E2E.student.password)
    await dialog.getByPlaceholder('至少 6 位').fill('stu-e2e-pass-2')
    await dialog.locator('.el-form-item').filter({ hasText: '确认新密码' }).locator('input').fill('stu-e2e-pass-2')
    await dialog.getByRole('button', { name: '确认修改' }).click()

    // 前端登出回登录页（改密吊销全部旧会话）
    await expect(page).toHaveURL(/\/login/, { timeout: 15_000 })

    // 旧 token 直连 API 必 401（吊销表生效）
    const resp = await page.request.get(`${E2E.apiURL}/api/v1/me`, {
      headers: { Authorization: `Bearer ${oldToken}` },
    })
    expect(resp.status()).toBe(401)

    // 旧密码登录失败：停留登录页并报错
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill(E2E.student.password)
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.locator('.el-message').first()).toBeVisible()
    await expect(page).toHaveURL(/\/login/)

    // 新密码可登录
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill('stu-e2e-pass-2')
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))

    // 恢复初始密码，保证套件内其他 spec 的账号常量可用
    await page.getByRole('button', { name: '改密码' }).click()
    const d2 = page.locator('.el-dialog', { hasText: '修改密码' })
    await d2.locator('.el-form-item').filter({ hasText: '当前密码' }).locator('input').fill('stu-e2e-pass-2')
    await d2.getByPlaceholder('至少 6 位').fill(E2E.student.password)
    await d2.locator('.el-form-item').filter({ hasText: '确认新密码' }).locator('input').fill(E2E.student.password)
    await d2.getByRole('button', { name: '确认修改' }).click()
    await expect(page).toHaveURL(/\/login/, { timeout: 15_000 })
    // 恢复后验证初始密码可登录（下个用例与后续 spec 依赖它）
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill(E2E.student.password)
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
  })

  test('管理员重置学生密码：学生会话失效', async ({ page, request }) => {
    // 学生先登录拿 token
    const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.student.username, password: E2E.student.password },
    })
    const { token: studentToken } = (await login.json()) as { token: string }

    // 管理员 UI 重置学生密码
    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    const row = page.locator('.el-table tr', { hasText: E2E.student.username })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: '重置密码' }).click()
    const dialog = page.locator('.el-dialog', { hasText: /重置密码/ })
    await dialog.locator('.el-form-item').filter({ hasText: '新密码' }).locator('input').fill('stu-e2e-pass-3')
    await dialog.locator('.el-form-item').filter({ hasText: '确认' }).locator('input').fill('stu-e2e-pass-3')
    await dialog.getByRole('button', { name: '确认重置' }).click()
    await expect(page.locator('.el-message', { hasText: /已重置/ })).toBeVisible()

    // 学生的旧 token 立即 401
    const resp = await request.get(`${E2E.apiURL}/api/v1/me`, {
      headers: { Authorization: `Bearer ${studentToken}` },
    })
    expect(resp.status()).toBe(401)

    // 恢复初始密码（admin API 直接重置，避免 UI 往返）
    const adminLogin = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.admin.username, password: E2E.admin.password },
    })
    const { token: adminToken } = (await adminLogin.json()) as { token: string }
    const users = await request.get(`${E2E.apiURL}/api/v1/admin/users`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    })
    const usersBody = (await users.json()) as { items: Array<{ id: number; username: string }> }
    const stu = usersBody.items.find((u) => u.username === E2E.student.username)
    expect(stu).toBeTruthy()
    const reset = await request.post(
      `${E2E.apiURL}/api/v1/admin/users/${stu!.id}/reset-password`,
      { headers: { Authorization: `Bearer ${adminToken}` }, data: { password: E2E.student.password } },
    )
    expect(reset.ok()).toBeTruthy()
  })
})