import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'

// 自助注册旅程（邮箱验证码主通道）：登录页切注册 → 发码（E2E 显式开启本地
// 回显，页面直接显示验证码）→ 建号即登录 → 登出后可用用户名 + 密码正常登录
// （邮箱不参与登录）→ 管理端侧栏无「用户与角色」。
// 旧版手机号注册 API 仍在最后一个用例覆盖（兼容保留）。

// 11 位大陆手机号（3 位前缀 + 时间戳后 7 位 + 1 位用例序号；整跑 DB 全新）。
function freshPhone(seed: number): string {
  return `139${String(Date.now()).slice(-7)}${seed}`
}

function freshEmail(tag: string): string {
  return `e2e-${tag}-${Date.now()}@example.com`
}

test.describe('自助注册旅程', () => {
  test('邮箱发码 → 凭码注册 → 建号即登录 → 用户名密码可再登录', async ({ page }) => {
    const email = freshEmail('reg')
    const username = `e2e-mail-reg-${Date.now().toString().slice(-6)}`
    const password = 'reg-pass-123'

    await page.goto('/login')
    await page.getByText('邮箱验证码注册').click()
    await expect(page.getByRole('heading', { name: 'yak-academy 注册' })).toBeVisible()

    // 发码后按钮进入倒计时（限频 UI），验证码由本地回显内联显示。
    await page.getByLabel('邮箱', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()
    await expect(page.getByRole('button', { name: /^\d+s$/ })).toBeDisabled()
    const hint = page.locator('[data-testid=verification-code]')
    await expect(hint).toContainText(/验证码：\d{6}/)
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]

    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('用户名（3-64 字符，登录用）').fill(username)
    await page.getByPlaceholder('密码（至少 6 位）').fill(password)
    await page.getByRole('button', { name: '注册并进入' }).click()

    // 建号即登录：跳进平台，头部显示普通人身份
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    await expect(page.getByRole('link', { name: `${username}（普通人）` })).toBeVisible()
    await expect(page.getByRole('link', { name: '进入管理端' })).toHaveCount(0)

    // 登出后用用户名 + 密码走普通登录（邮箱不参与登录）
    await page.getByRole('button', { name: '登出' }).click()
    await page.waitForURL((u) => u.pathname === '/login')
    await page.getByPlaceholder('用户名').fill(username)
    await page.getByPlaceholder('密码').fill(password)
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    await expect(page.getByRole('link', { name: `${username}（普通人）` })).toBeVisible()
  })

  test('邮箱验证码一次性核销（同码不可二次使用）', async ({ request }) => {
    const email = freshEmail('once')
    const sms = await request.post(`${E2E.apiURL}/api/v1/auth/email/code`, { data: { email } })
    expect(sms.ok()).toBeTruthy()
    const { code } = (await sms.json()) as { code: string }
    expect(code).toMatch(/^\d{6}$/)

    const ok = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
      data: { email, code, username: `e2e-mail-once-${Date.now().toString().slice(-6)}`, password: 'pass-123' },
    })
    expect(ok.status()).toBe(200)
    // 同码换用户名再来一次：验证码已核销，必须 422
    const replay = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
      data: { email, code, username: `e2e-mail-twice-${Date.now().toString().slice(-6)}`, password: 'pass-123' },
    })
    expect(replay.status()).toBe(422)
  })

  test('旧版手机号注册 API 兼容可用', async ({ request }) => {
    const phone = freshPhone(3)
    const sms = await request.post(`${E2E.apiURL}/api/v1/auth/sms/code`, { data: { phone } })
    expect(sms.ok()).toBeTruthy()
    const { code } = (await sms.json()) as { code: string }
    expect(code).toMatch(/^\d{6}$/)

    const reg = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
      data: { phone, code, username: `e2e-phone-reg-${Date.now().toString().slice(-6)}`, password: 'pass-123' },
    })
    expect(reg.status()).toBe(200)
  })
})
