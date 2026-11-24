import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 找回账号 / 重置密码旅程。自助注册已下线（微信是唯一注册入口），新账号没有
// 邮箱/手机号，因此自助找回只服务历史账号——E2E 无法经公开 API 造出带邮箱/
// 手机号的账号，历史账号的验证码核销语义由 service 单测（password_reset_test.go）
// 覆盖。这里锚定两条真实可走路径：
//   1. 未注册邮箱发码：立即中文拒绝（不泄漏验证码、不进入倒计时）；
//   2. 管理端重置：用户管理行内「重置密码」直接设新密码，旧密码失效、新密码
//      可登录（竞赛批量建号/微信账号忘记密码的唯一人工通道）。

async function createUserViaAdminApi(
  request: APIRequestContext,
  username: string,
  password: string,
): Promise<void> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  expect(login.ok()).toBeTruthy()
  const { token } = (await login.json()) as { token: string }
  const created = await request.post(`${E2E.apiURL}/api/v1/admin/users`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { username, password },
  })
  expect(created.ok()).toBeTruthy()
}

async function apiLogin(
  request: APIRequestContext,
  username: string,
  password: string,
): Promise<number> {
  const resp = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
    data: { username, password },
  })
  return resp.status()
}

test.describe('找回账号与重置密码', () => {
  test('未注册邮箱发码给出中文指引（不回显验证码）', async ({ page }) => {
    const email = `e2e-missing-${Date.now()}@example.com`

    await page.goto('/login')
    // el-link 在可访问树是 generic（非 link role），用精确文本锚定。
    await page.getByText('找回账号', { exact: true }).click()
    await expect(page.getByRole('heading', { name: '找回你的账号。' })).toBeVisible()

    await page.getByLabel('邮箱或手机号', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()

    await expect(page.getByText('该邮箱未注册过账号：请先注册')).toBeVisible()
    // 未投递就没有验证码可回显，也不会进入 60s 倒计时。
    await expect(page.locator('[data-testid=verification-code]')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '获取验证码' })).toBeEnabled()
  })

  test('管理端重置：用户管理行内设新密码，旧密码失效新密码可登录', async ({ page, request }) => {
    const username = `e2e-adminreset-${Date.now().toString().slice(-6)}`
    const oldPass = 'before-reset-1'
    const newPass = 'after-reset-9'
    await createUserViaAdminApi(request, username, oldPass)
    expect(await apiLogin(request, username, oldPass)).toBe(200)

    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号').fill(username)
    await page.getByRole('button', { name: '搜索' }).click()
    const row = page.locator('tr', { hasText: username })
    await expect(row).toHaveCount(1)

    await row.getByRole('button', { name: '重置密码' }).click()
    const dialog = page.locator('[data-slot=dialog-content]').filter({ has: page.locator('[data-slot=dialog-title]', { hasText: '重置密码' }) })
    await expect(dialog).toBeVisible()
    // 预填平台批量建号同款初始密码 123456，此处改为自定义密码。
    await expect(dialog.getByPlaceholder('至少 6 位')).toHaveValue('123456')
    await dialog.getByPlaceholder('至少 6 位').fill(newPass)
    await dialog.getByRole('button', { name: '重置', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已重置' })).toBeVisible()

    expect(await apiLogin(request, username, oldPass)).toBe(401)
    expect(await apiLogin(request, username, newPass)).toBe(200)
  })
})
