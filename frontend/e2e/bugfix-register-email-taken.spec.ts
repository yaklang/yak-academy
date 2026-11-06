import { expect, test } from '@playwright/test'
import { logout } from './helpers'

// 回归：已注册邮箱再次注册时学员只看到英文报错且被限频掩盖（2026-10-23 修复，
// 2026-11-06 随注册主通道切换为邮箱）。
// 修复前行为：注册成功后 60 秒内用同一邮箱再点「获取验证码」，后端限频检查
//   先于邮箱查重执行，返回 200 + retry_after——前端提示「发送过于频繁」，
//   学员等到倒计时结束再试才得到 422 英文原文，无从知道下一步该做什么
//   （邮箱是注册凭据，登录走用户名，学员不知道该去登录）。
// 修复后行为：发码时已注册邮箱查重优先于限频，立即 422；注册页把该错误
//   映射为中文行动指引「该邮箱已注册过账号：请返回登录；忘记密码可在
//   登录页「找回账号」自助重置」。
// 复现路径：登录页 → 「邮箱验证码注册」→ 填邮箱 → 获取验证码（回显拿码）
//   → 填码/用户名/密码 → 注册并进入 → 登出 → 再次注册同一邮箱 →
//   获取验证码 → 期待出现中文指引。

test.describe('回归：已注册邮箱再次发码给出中文登录指引', () => {
  test('注册成功后同邮箱再获取验证码，提示已注册并引导返回登录（不再被限频掩盖）', async ({ page }) => {
    const email = `e2e-emailtaken-${Date.now()}@example.com`
    const username = `e2e-emailtaken-${Date.now().toString().slice(-6)}`
    const password = 'taken-pass-123'

    // 1. 先走完整自助注册（回显模式拿码），让该邮箱绑定一个真实账号
    await page.goto('/login')
    await page.getByText('邮箱验证码注册').click()
    await page.getByLabel('邮箱', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()
    const hint = page.locator('[data-testid=verification-code]')
    await expect(hint).toBeVisible()
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]
    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('用户名（3-64 字符，登录用）').fill(username)
    await page.getByPlaceholder('密码（至少 6 位）').fill(password)
    await page.getByRole('button', { name: '注册并进入' }).click()
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))

    // 2. 登出回登录页，用同一邮箱再次进入注册流程
    await logout(page)
    await page.getByText('邮箱验证码注册').click()
    await page.getByLabel('邮箱', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()

    // 3. 修复点：立即得到「已注册」的中文行动指引（修复前 60s 限频先拦，
    //    显示的是发送成功/频繁 toast；等倒计时结束后也只会透出英文 422 原文）
    await expect(page.getByText('该邮箱已注册过账号')).toBeVisible()
    await expect(page.getByText('请返回登录')).toBeVisible()

    // 4. 指引可执行：页面能切回登录 tab（学员的出路确实存在）
    //    （toast 文案也含「返回登录」；定位 switch 容器内的按钮本体，避免歧义）
    await page.locator('.auth-switch .auth-text-button', { hasText: '返回登录' }).click()
    await expect(page.getByRole('heading', { name: 'yak-academy 登录' })).toBeVisible()
  })
})
