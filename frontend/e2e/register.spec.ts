import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'

// 自助注册旅程（手机验证码）：登录页切注册 → 发码（后端开发投递器写服务
// 日志，从 e2e/.state/backend.log 取码）→ 建号即登录 → 登出后可用
// 用户名 + 密码正常登录（手机号不参与登录）→ 管理端侧栏无「用户与角色」。

const backendLog = path.join(fileURLToPath(new URL('.', import.meta.url)), '.state', 'backend.log')

// 从后端日志轮询取指定手机号最近一条开发投递的验证码。
async function smsCodeOf(phone: string): Promise<string> {
  for (let i = 0; i < 25; i++) {
    const code = readFileSync(backendLog, 'utf-8')
      .split('\n')
      .filter((l) => l.includes(`phone=${phone}`))
      .at(-1)
      ?.match(/code=(\d{6})/)?.[1]
    if (code) return code
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  throw new Error(`后端日志未出现 ${phone} 的开发投递验证码`)
}

test.describe('自助注册旅程', () => {
  test('发码 → 凭码注册 → 建号即登录 → 用户名密码可再登录', async ({ page }) => {
    const phone = '13955660001'
    const username = 'e2e-sms-reg'
    const password = 'reg-pass-123'

    await page.goto('/login')
    await page.getByText('手机验证码注册').click()
    await expect(page.getByRole('heading', { name: 'yak-academy 注册' })).toBeVisible()

    // 发码后按钮进入倒计时（限频 UI），验证码出现在后端日志
    await page.getByPlaceholder('手机号（仅用于注册验证）').fill(phone)
    await page.getByRole('button', { name: '获取验证码' }).click()
    await expect(page.getByRole('button', { name: /^\d+s$/ })).toBeDisabled()
    const code = await smsCodeOf(phone)

    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('用户名（3-64 字符，登录用）').fill(username)
    await page.getByPlaceholder('密码（至少 6 位）').fill(password)
    await page.getByRole('button', { name: '注册并进入' }).click()

    // 建号即登录：跳进平台，头部显示普通人身份
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    await expect(page.getByRole('link', { name: `${username}（普通人）` })).toBeVisible()
    await expect(page.getByRole('link', { name: '进入管理端' })).toHaveCount(0)

    // 登出后用用户名 + 密码走普通登录（手机号不参与登录）
    await page.getByRole('button', { name: '登出' }).click()
    await page.waitForURL((u) => u.pathname === '/login')
    await page.getByPlaceholder('用户名').fill(username)
    await page.getByPlaceholder('密码').fill(password)
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    await expect(page.getByRole('link', { name: `${username}（普通人）` })).toBeVisible()
  })

  test('同码不可二次使用（验证码一次性核销）', async ({ page, request }) => {
    const phone = '13955660002'
    await page.goto('/login')
    await page.getByText('手机验证码注册').click()
    await page.getByPlaceholder('手机号（仅用于注册验证）').fill(phone)
    await page.getByRole('button', { name: '获取验证码' }).click()
    const code = await smsCodeOf(phone)

    // 第一次注册成功
    const ok = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
      data: { phone, code, username: 'e2e-sms-once', password: 'pass-123' },
    })
    expect(ok.status()).toBe(200)
    // 同码换用户名再来一次：验证码已核销，必须 422
    const replay = await request.post(`${E2E.apiURL}/api/v1/auth/register`, {
      data: { phone, code, username: 'e2e-sms-twice', password: 'pass-123' },
    })
    expect(replay.status()).toBe(422)
  })
})
