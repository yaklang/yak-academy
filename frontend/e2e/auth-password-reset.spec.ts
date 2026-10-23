import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 找回账号 / 重置密码旅程（竞赛域外的账号自助能力补全）：
//   自助找回：登录页「找回账号」→ 已注册手机号发码（回显模式取码）→
//     凭码重置密码 → 回显用户名与账号编码（忘记用户名的学员凭手机号找回）；
//     旧密码随即失效，新密码可登录。
//   管理端重置：用户管理行内「重置密码」直接设新密码（未绑定手机号的
//     竞赛批量账号走此入口），旧密码同样失效。
// 建号走 API 播种（发码回显模式取码），重置全程走真实 UI。

// 经回显模式发码注册一个绑定手机号的新账号（e2e 后端 ACADEMY_SMS_SHOW_CODE=1）。
async function registerViaApi(
  request: APIRequestContext,
  username: string,
  phone: string,
  password: string,
): Promise<void> {
  const sms = await request.post(`${E2E.apiURL}/api/v1/auth/sms/code`, { data: { phone } })
  expect(sms.ok()).toBeTruthy()
  const { code } = (await sms.json()) as { code: string }
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

// 11 位大陆手机号（3 位前缀 + 时间戳后 7 位 + 1 位用例序号；整跑 DB 全新，
// 仅需保证本次运行内唯一）。
function freshPhone(seed: number): string {
  return `139${String(Date.now()).slice(-7)}${seed}`
}

test.describe('找回账号与重置密码', () => {
  test('自助找回：手机验证码重置密码并回显账号，旧密码失效', async ({ page, request }) => {
    test.slow() // 发码限频 60s：注册播种 + 找回发码两次真实短信通道往返
    const phone = freshPhone(1)
    const username = `e2e-found-${Date.now().toString().slice(-6)}`
    const oldPass = 'old-pass-123'
    const newPass = 'found-pass-456'
    await registerViaApi(request, username, phone, oldPass)

    await page.goto('/login')
    // el-link 在可访问树是 generic（非 link role），用精确文本锚定。
    await page.getByText('找回账号', { exact: true }).click()
    await expect(page.getByRole('heading', { name: 'yak-academy 找回账号' })).toBeVisible()

    await page.getByPlaceholder('手机号（注册时绑定的手机号）').fill(phone)
    await page.getByRole('button', { name: '获取验证码' }).click()
    const hint = page.locator('.code-hint')
    await expect(hint).toBeVisible()
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]

    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('新密码（至少 6 位）').fill(newPass)
    await page.getByRole('button', { name: '重置密码并找回账号' }).click()

    // 找回结果：回显用户名与学号式账号编码（忘记用户名也能找回）。
    const panel = page.locator('.el-result', { hasText: '账号找回成功' })
    await expect(panel).toBeVisible()
    await expect(panel).toContainText(`用户名：${username}`)
    await expect(panel.getByText(/账号编码：YA\d{2}-\d{5}/)).toBeVisible()

    // 旧密码立即失效，新密码可登录（先 API 验证再走 UI 登录进入平台）。
    expect(await apiLogin(request, username, oldPass)).toBe(401)
    expect(await apiLogin(request, username, newPass)).toBe(200)

    await panel.getByRole('button', { name: '返回登录' }).click()
    await page.getByPlaceholder('用户名').fill(username)
    await page.getByPlaceholder('密码').fill(newPass)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
  })

  test('管理端重置：用户管理行内设新密码，旧密码失效新密码可登录', async ({ page, request }) => {
    const phone = freshPhone(2)
    const username = `e2e-adminreset-${Date.now().toString().slice(-6)}`
    const oldPass = 'before-reset-1'
    const newPass = 'after-reset-9'
    await registerViaApi(request, username, phone, oldPass)
    expect(await apiLogin(request, username, oldPass)).toBe(200)

    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 手机号').fill(username)
    await page.getByRole('button', { name: '搜索' }).click()
    const row = page.locator('tr', { hasText: username })
    await expect(row).toHaveCount(1)

    await row.getByRole('button', { name: '重置密码' }).click()
    const dialog = page.locator('.el-dialog').filter({ has: page.locator('.el-dialog__title', { hasText: '重置密码' }) })
    await expect(dialog).toBeVisible()
    // 预填平台批量建号同款初始密码 123456，此处改为自定义密码。
    await expect(dialog.getByPlaceholder('至少 6 位')).toHaveValue('123456')
    await dialog.getByPlaceholder('至少 6 位').fill(newPass)
    await dialog.getByRole('button', { name: '重置', exact: true }).click()
    await expect(page.locator('.el-message', { hasText: '已重置' })).toBeVisible()

    expect(await apiLogin(request, username, oldPass)).toBe(401)
    expect(await apiLogin(request, username, newPass)).toBe(200)
  })
})
