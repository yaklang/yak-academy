import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'

// 微信 OAuth 注册/登录旅程（E2E 以 ACADEMY_WECHAT_MOCK=1 走本地模拟，不访问
// 微信）：登录页唯一的自助入口是微信按钮 → 模拟授权页回跳
// /login?code=mock&state=... → 登录页把 code/state 交回后端换应用 token
// （首次自动建号）→ 进入平台。手机号/邮箱自助注册接口已永久下线。
//
// 模拟身份由 HttpOnly Cookie 稳定 30 天；每个用例使用独立的浏览器上下文，
// 因此每个用例都是新账号。用户名取自微信昵称「微信用户」，重名时追加稳定
// 后缀，断言按前缀匹配。

test.describe('微信注册/登录', () => {
  test('登录页微信按钮完成授权回跳，自动建号并进入平台', async ({ page }) => {
    await page.goto('/login')
    // 首屏先显示「正在检查微信登录…」，status 返回后才变成可用按钮（冷启动
    // 首个模块编译较慢，放宽等待时间）。
    const wechatButton = page.locator('.login-wechat__button')
    await expect(wechatButton).toHaveText('微信扫码注册 / 登录', { timeout: 15_000 })
    await expect(wechatButton).toBeEnabled()
    await expect(page.getByText('首次使用微信会自动创建账号，无需邮箱/手机号或密码。')).toBeVisible()

    await wechatButton.click()
    // 模拟授权页把 code/state 回跳到登录页；登录页完成 callback 后进入 /learn。
    await page.waitForURL((u) => u.pathname === '/learn')
    await expect(page.getByRole('button', { name: /微信用户（学员）/ })).toBeVisible()

    // 微信账号没有平台密码：资料页显示「微信登录」标签，不提供修改密码入口。
    await page.goto('/profile')
    await expect(page.getByText('微信登录', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '修改密码' })).toHaveCount(0)
    await expect(page.getByText('如需使用密码登录，请联系管理员重置密码。')).toBeVisible()
  })

  test('status 暴露 mock 能力；回调必须持有 state Cookie；state 一次性', async ({ request }) => {
    const statusResp = await request.get(`${E2E.apiURL}/api/v1/auth/wechat/status`)
    expect(statusResp.ok()).toBeTruthy()
    expect(await statusResp.json()).toMatchObject({
      enabled: true,
      mock: true,
      mode: 'official_account',
    })

    // 缺 state Cookie：即使 code/state 齐全也必须 401（防登录 CSRF）。
    const forged = await request.post(`${E2E.apiURL}/api/v1/auth/wechat/callback`, {
      data: { code: 'mock', state: 'forged-state' },
    })
    expect(forged.status()).toBe(401)

    const start = await request.get(`${E2E.apiURL}/api/v1/auth/wechat/start`, {
      params: { redirect: '/profile' },
    })
    expect(start.ok()).toBeTruthy()
    const { authorize_url } = (await start.json()) as { authorize_url: string }
    const authorize = new URL(authorize_url)
    expect(authorize.pathname).toBe('/login')
    expect(authorize.searchParams.get('code')).toBe('mock')
    const state = authorize.searchParams.get('state')
    expect(state).toBeTruthy()

    // 同一请求上下文持有 start 写入的 HttpOnly state Cookie。
    const callback = await request.post(`${E2E.apiURL}/api/v1/auth/wechat/callback`, {
      data: { code: 'mock', state },
    })
    expect(callback.status()).toBe(200)
    const { token, redirect } = (await callback.json()) as { token: string; redirect: string }
    expect(redirect).toBe('/profile')
    expect(token).toBeTruthy()

    // 无密码账号：/me 标记 password_set=false；用户名密码登录必须被拒绝。
    const me = await request.get(`${E2E.apiURL}/api/v1/me`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(me.ok()).toBeTruthy()
    const profile = (await me.json()) as { username: string; password_set: boolean }
    expect(profile.password_set).toBe(false)
    expect(profile.username).toContain('微信用户')

    const passwordLogin = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: profile.username, password: 'any-password' },
    })
    expect(passwordLogin.status()).toBe(401)

    // state 一次性：callback 已清除 Cookie，重放同一 state 必须 401。
    const replay = await request.post(`${E2E.apiURL}/api/v1/auth/wechat/callback`, {
      data: { code: 'mock', state },
    })
    expect(replay.status()).toBe(401)
  })

  test('手机号/邮箱自助注册接口已下线（404）', async ({ request }) => {
    for (const path of [
      '/api/v1/auth/register',
      '/api/v1/auth/email/code',
      '/api/v1/auth/sms/code',
    ]) {
      const resp = await request.post(`${E2E.apiURL}${path}`, {
        data: { email: 'nobody@example.com', phone: '13900000000', username: 'x', password: 'pass-123' },
      })
      expect(resp.status(), `${path} 必须返回 404`).toBe(404)
    }
  })
})
