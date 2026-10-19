import { expect, test } from '@playwright/test'

// 回归：内网部署上学员无法完成手机号注册（2026-10-19 修复）。
// 回归描述：验证码仅由开发投递器写服务端日志，注册页发码后学员拿不到验证码
//   原文，注册流程卡死在第一步——UI 提示"验证码见服务端日志"，而学员没有
//   途径访问容器日志（表单与后端注册链路本身均正常）。
// 修复前行为：/auth/sms/code 响应只有 {ok, retry_after?}，登录页对验证码
//   唯一的展示是一句"见服务端日志"的 toast，页面上不出现验证码，无法自助注册。
// 修复后行为：服务端开启回显模式（ACADEMY_SMS_SHOW_CODE=1，playwright 后端
//   已开启；内网/演示部署同样开启）时，发码响应携带验证码原文，注册页在验证码
//   输入框下方内联显示，学员自助复制完成注册；默认部署不回显、行为不变。
// 复现路径：登录页 → 「手机验证码注册」→ 填手机号 → 「获取验证码」→
//   页面上找不到验证码 → 注册无法进行。
// 说明：回显开关的后端取值正确性由 service 单测覆盖
//   （TestSendCodeReturnsDeliveredCode：返回码=投递码、限频不回显）；
//   本 spec 锚定学员真实路径——UI 显示验证码并全程自助注册成功。

test.describe('回归：回显模式下学员可自助完成手机号注册', () => {
  test('发码后页面内联显示验证码，无需服务端日志即可注册成功', async ({ page }) => {
    const phone = '13955660086'
    const username = `e2e-showcode-${Date.now().toString().slice(-6)}`
    const password = 'show-pass-123'

    await page.goto('/login')
    await page.getByText('手机验证码注册').click()
    await expect(page.getByRole('heading', { name: 'yak-academy 注册' })).toBeVisible()

    await page.getByPlaceholder('手机号（仅用于注册验证）').fill(phone)
    await page.getByRole('button', { name: '获取验证码' }).click()

    // 修复点：验证码原文内联出现在验证码输入框下方（修复前这里只有一句
    // "见服务端日志"的 toast，页面永远不显示验证码）
    const hint = page.locator('.code-hint')
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
