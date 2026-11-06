import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 找回账号 / 重置密码旅程（竞赛域外的账号自助能力补全）：
//   自助找回：登录页「找回账号」→ 已注册邮箱发码（本地回显取码）→
//     凭码重置密码 → 回显用户名与账号编码（忘记用户名的学员凭邮箱找回）；
//     旧密码随即失效，新密码可登录。旧手机号账号仍可用手机号找回。
//   管理端重置：用户管理行内「重置密码」直接设新密码（未绑定邮箱/手机号的
//     竞赛批量账号走此入口），旧密码同样失效。
// 建号走 API 播种（E2E 显式开启本地回显取码），重置全程走真实 UI。

async function registerViaApi(
  request: APIRequestContext,
  username: string,
  email: string,
  password: string,
): Promise<void> {
  const codeResp = await request.post(`${E2E.apiURL}/api/v1/auth/email/code`, { data: { email } })
  expect(codeResp.ok()).toBeTruthy()
  const { code } = (await codeResp.json()) as { code: string }
  expect(code).toMatch(/^\d{6}$/)
  const reg = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
    data: { email, code, username, password },
  })
  expect(reg.ok()).toBeTruthy()
}

// 旧版手机号注册（兼容路径）。
async function registerPhoneViaApi(
  request: APIRequestContext,
  username: string,
  phone: string,
  password: string,
): Promise<void> {
  const codeResp = await request.post(`${E2E.apiURL}/api/v1/auth/sms/code`, { data: { phone } })
  expect(codeResp.ok()).toBeTruthy()
  const { code } = (await codeResp.json()) as { code: string }
  expect(code).toMatch(/^\d{6}$/)
  const reg = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
    data: { phone, code, username, password },
  })
  expect(reg.ok()).toBeTruthy()
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

// 11 位大陆手机号（3 位前缀 + 时间戳后 7 位 + 1 位用例序号；整跑 DB 全新）。
function freshPhone(seed: number): string {
  return `139${String(Date.now()).slice(-7)}${seed}`
}

function freshEmail(seed: number): string {
  return `e2e-reset-${Date.now()}-${seed}@example.com`
}

test.describe('找回账号与重置密码', () => {
  test('自助找回：邮箱验证码重置密码并回显账号，旧密码失效', async ({ page, request }) => {
    const email = freshEmail(1)
    const username = `e2e-found-${Date.now().toString().slice(-6)}`
    const oldPass = 'old-pass-123'
    const newPass = 'found-pass-456'
    await registerViaApi(request, username, email, oldPass)

    await page.goto('/login')
    // el-link 在可访问树是 generic（非 link role），用精确文本锚定。
    await page.getByText('找回账号', { exact: true }).click()
    await expect(page.getByRole('heading', { name: 'yak-academy 找回账号' })).toBeVisible()

    await page.getByLabel('邮箱或手机号', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()
    const hint = page.locator('[data-testid=verification-code]')
    await expect(hint).toBeVisible()
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]

    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('新密码（至少 6 位）').fill(newPass)
    await page.getByRole('button', { name: '重置密码并找回账号' }).click()

    // 找回结果：回显用户名与学号式账号编码（忘记用户名也能找回）。
    const panel = page.locator('.auth-recovered', { hasText: '账号找回成功' })
    await expect(panel).toBeVisible()
    await expect(panel.getByText(username, { exact: true })).toBeVisible()
    await expect(panel.locator('dd').nth(1)).toHaveText(/^YA\d{2}-\d{5}$/)

    // 旧密码立即失效，新密码可登录（先 API 验证再走 UI 登录进入平台）。
    expect(await apiLogin(request, username, oldPass)).toBe(401)
    expect(await apiLogin(request, username, newPass)).toBe(200)

    await panel.getByRole('button', { name: '返回登录' }).click()
    await page.getByPlaceholder('用户名').fill(username)
    await page.getByPlaceholder('密码').fill(newPass)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
  })

  test('旧版手机号账号仍可通过手机号找回密码', async ({ page, request }) => {
    const phone = freshPhone(2)
    const username = `e2e-phone-found-${Date.now().toString().slice(-6)}`
    const oldPass = 'old-pass-123'
    const newPass = 'phone-pass-456'
    await registerPhoneViaApi(request, username, phone, oldPass)

    await page.goto('/login')
    await page.getByText('找回账号', { exact: true }).click()
    await page.getByLabel('邮箱或手机号', { exact: true }).fill(phone)
    await page.getByRole('button', { name: '获取验证码' }).click()
    const hint = page.locator('[data-testid=verification-code]')
    await expect(hint).toBeVisible()
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]

    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('新密码（至少 6 位）').fill(newPass)
    await page.getByRole('button', { name: '重置密码并找回账号' }).click()
    await expect(page.locator('.auth-recovered', { hasText: '账号找回成功' })).toBeVisible()

    expect(await apiLogin(request, username, oldPass)).toBe(401)
    expect(await apiLogin(request, username, newPass)).toBe(200)
  })

  test('管理端重置：用户管理行内设新密码，旧密码失效新密码可登录', async ({ page, request }) => {
    const email = freshEmail(3)
    const username = `e2e-adminreset-${Date.now().toString().slice(-6)}`
    const oldPass = 'before-reset-1'
    const newPass = 'after-reset-9'
    await registerViaApi(request, username, email, oldPass)
    expect(await apiLogin(request, username, oldPass)).toBe(200)

    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号').fill(username)
    await page.getByRole('button', { name: '搜索' }).click()
    const row = page.locator('tr', { hasText: username })
    await expect(row).toHaveCount(1)
    await expect(row).toContainText(email)

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
