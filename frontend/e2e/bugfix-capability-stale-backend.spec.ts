import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：前后端版本不一致时，旧后端对未知接口返回 text/plain 的
// “404 page not found”，client.ts 直接 JSON.parse 抛原始 SyntaxError
// （页面显示 Unexpected non-whitespace character after JSON at position 4）。
// 修复后行为：非 JSON 响应统一转成可读的 ApiError，能力画像给出中文错误与重试。
// 复现路径：后端未更新（无 /me/capabilities 路由）时打开账号信息页。
test.describe('回归：能力画像接口返回非 JSON', () => {
  test('旧后端纯文本 404 显示中文错误而不是 JSON 解析异常（回归：英文 SyntaxError）', async ({ page }) => {
    await loginAsStudent(page)
    // 只固定这一个接口的响应，登录、资料卡与页面布局仍走真实链路。
    await page.route('**/api/v1/me/capabilities', (route) => route.fulfill({
      status: 404,
      contentType: 'text/plain; charset=utf-8',
      body: '404 page not found\n',
    }))
    await page.goto('/profile')

    const alert = page.locator('[data-testid=capability-card] [role=alert]')
    await expect(alert).toBeVisible()
    await expect(alert).toContainText('HTTP 404')
    await expect(alert).toContainText('版本')
    await expect(alert).not.toContainText('Unexpected')
    await expect(alert.getByRole('button', { name: '重试' })).toBeVisible()
  })
})
