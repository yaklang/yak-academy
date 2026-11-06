import { expect, test } from '@playwright/test'

// 回归：本地演示/内网联调时学员拿不到验证码、注册流程卡死（2026-10-19 修复，
// 2026-11-06 随注册主通道切换为邮箱）。
// 回归描述：生产默认关闭验证码回显；若本地演示没有显式开启
//   ACADEMY_SHOW_VERIFICATION_CODE=1，发码响应只有 {ok, retry_after?}，页面上
//   永远不出现验证码，学员无法自助注册。
// 修复后行为：本地/E2E 显式开启回显，发码响应携带验证码原文，注册页在验证码
//   输入框下方内联显示；生产严格模式禁止开启该开关。
// 复现路径：登录页 → 「邮箱验证码注册」→ 填邮箱 → 「获取验证码」→
//   页面上找不到验证码 → 注册无法进行。
// 说明：回显开关的配置取值正确性由 config 单测覆盖；本 spec 锚定学员真实路径
//   ——UI 显示验证码并全程自助注册成功。

test.describe('回归：本地演示回显下学员可自助完成邮箱注册', () => {
  test('发码后页面内联显示验证码，无需访问服务端日志即可注册成功', async ({ page }) => {
    const email = `e2e-showcode-${Date.now()}@example.com`
    const username = `e2e-showcode-${Date.now().toString().slice(-6)}`
    const password = 'show-pass-123'

    await page.goto('/login')
    await page.getByText('邮箱验证码注册').click()
    await expect(page.getByRole('heading', { name: 'yak-academy 注册' })).toBeVisible()

    await page.getByLabel('邮箱', { exact: true }).fill(email)
    await page.getByRole('button', { name: '获取验证码' }).click()

    // 修复点：验证码原文内联出现在验证码输入框下方（回显关闭时这里只有
    // "请查收邮箱"提示，页面不显示验证码）。
    const hint = page.locator('[data-testid=verification-code]')
    await expect(hint).toBeVisible()
    await expect(hint).toContainText(/验证码：\d{6}（5 分钟内有效/)

    // 学员自助路径：把页面显示的码抄进输入框，完成注册
    const code = (await hint.textContent())!.match(/(\d{6})/)![1]
    await page.getByPlaceholder('验证码').fill(code)
    await page.getByPlaceholder('用户名（3-64 字符，登录用）').fill(username)
    await page.getByPlaceholder('密码（至少 6 位）').fill(password)
    await page.getByRole('button', { name: '注册并进入' }).click()

    // 建号即登录，进入平台（普通人身份，无管理端入口）
    await page.waitForURL((u) => !u.pathname.startsWith('/login'))
    await expect(page.getByRole('link', { name: `${username}（普通人）` })).toBeVisible()
    await expect(page.getByRole('link', { name: '进入管理端' })).toHaveCount(0)
  })
})
