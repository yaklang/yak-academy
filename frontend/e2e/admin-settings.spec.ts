import { expect, test } from '@playwright/test'
import { adminToken, loginAsAdmin, studentToken } from './helpers'
import { E2E } from './fixtures/consts'

// 管理端系统设置回归：SMTP 配置从页面保存 → 立即生效 → 刷新后回显，
// 密码只写不读；用例结束停用投递，避免影响其他 e2e 的邮箱验证码链路。
test.describe('系统设置', () => {
  test.afterEach(async ({ request }) => {
    const token = await adminToken(request)
    await request.put(`${E2E.apiURL}/api/v1/admin/settings/smtp`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { enabled: false, addr: '', username: '', password: '', from: '', tls: 'starttls' },
    })
  })

  test('保存 SMTP 配置后立即生效，刷新不丢失且密码不回显', async ({ page, request }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/backups')
    await page.getByRole('link', { name: '系统设置' }).click()
    await page.waitForURL('**/admin/panel/settings')
    await expect(page.getByRole('heading', { name: '系统设置' })).toBeVisible()
    await expect(page.getByText('未配置', { exact: true })).toBeVisible()

    await page.getByLabel('SMTP 服务器').fill('smtp.e2e.example.com:587')
    await page.getByLabel('SMTP 用户名').fill('mailer@example.com')
    await page.getByLabel('SMTP 密码 / API Key').fill('e2e-smtp-secret')
    await page.getByLabel('发件人').fill('Yak Academy <no-reply@example.com>')
    await page.getByLabel('TLS 模式').click()
    await page.getByRole('option', { name: '隐式 TLS（465）' }).click()
    await page.getByRole('switch', { name: '启用邮箱验证码投递' }).click()
    await page.getByRole('button', { name: '保存配置' }).click()

    await expect(page.getByText('SMTP 配置已保存并立即生效')).toBeVisible()
    await expect(page.getByText('管理端配置', { exact: true })).toBeVisible()

    const token = await adminToken(request)
    const saved = await request.get(`${E2E.apiURL}/api/v1/admin/settings/smtp`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(saved.ok()).toBeTruthy()
    const body = (await saved.json()) as Record<string, unknown>
    expect(body.enabled).toBe(true)
    expect(body.addr).toBe('smtp.e2e.example.com:587')
    expect(body.tls).toBe('implicit')
    expect(body.has_password).toBe(true)
    expect(body.source).toBe('database')
    expect(body).not.toHaveProperty('password')

    await page.reload()
    await expect(page.getByLabel('SMTP 服务器')).toHaveValue('smtp.e2e.example.com:587')
    await expect(page.getByLabel('SMTP 密码 / API Key')).toHaveValue('')
    await expect(page.getByLabel('SMTP 密码 / API Key')).toHaveAttribute('placeholder', '已保存，留空保持不变')
    await expect(page.getByRole('button', { name: '发送测试邮件' })).toBeEnabled()

    await page.getByRole('switch', { name: '启用邮箱验证码投递' }).click()
    await page.getByRole('button', { name: '保存配置' }).click()
    await expect(page.getByText('已停用 SMTP 投递，配置已保存')).toBeVisible()

    const disabled = await request.get(`${E2E.apiURL}/api/v1/admin/settings/smtp`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(((await disabled.json()) as Record<string, unknown>).enabled).toBe(false)
  })

  test('启用但未填地址时前端拦截并保留页面状态', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/settings')
    await page.getByRole('switch', { name: '启用邮箱验证码投递' }).click()
    await page.getByLabel('发件人').fill('no-reply@example.com')
    await page.getByRole('button', { name: '保存配置' }).click()
    await expect(page.getByText('请填写 SMTP 服务器地址')).toBeVisible()
    await expect(page.getByLabel('SMTP 服务器')).toBeVisible()
  })

  test('普通学员不能读取或修改 SMTP 设置', async ({ request }) => {
    const token = await studentToken(request)
    const headers = { Authorization: `Bearer ${token}` }
    const read = await request.get(`${E2E.apiURL}/api/v1/admin/settings/smtp`, { headers })
    expect(read.status()).toBe(403)
    const write = await request.put(`${E2E.apiURL}/api/v1/admin/settings/smtp`, {
      headers,
      data: { enabled: true, addr: 'smtp.example.com:587', username: '', password: '', from: 'no-reply@example.com', tls: 'starttls' },
    })
    expect(write.status()).toBe(403)
  })
})
