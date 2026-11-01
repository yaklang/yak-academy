import { expect, test, type Page } from '@playwright/test'
import { loginAsAdmin, loginAsStudent, switchAdminChallengeBank } from './helpers'
import { E2E } from './fixtures/consts'

test.use({ reducedMotion: 'reduce' })
const widths = [320, 390, 768, 1024, 1440]
async function theme(page: Page, value: 'light' | 'dark') {
  await page.getByRole('button', { name: '切换主题', exact: true }).click()
  await page.getByRole('menuitemradio', { name: value === 'light' ? '浅色模式' : '深色模式', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', value)
  await page.keyboard.press('Escape')
}
async function fits(page: Page, width: number) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth), { message: `${width}px 页面不应横向溢出` }).toBeLessThanOrEqual(width)
}

test('公开首页与登录/注册/找回页面在两种主题和五种宽度下可用', async ({ page }) => {
  test.setTimeout(90_000)
  await page.goto('/')
  for (const mode of ['light', 'dark'] as const) {
    await theme(page, mode)
    for (const width of widths) {
      await test.step(`${mode} ${width}px`, async () => {
        await page.setViewportSize({ width, height: 900 })
        await page.goto('/')
        await expect(page.locator('#landing-title')).toBeVisible()
        await fits(page, width)
        await expect(page.getByRole('link', { name: '开始学习', exact: true })).toBeVisible()
        const canvasSize = await page.locator('.academy-background canvas').evaluate((el: HTMLCanvasElement) => ({ width: el.width, height: el.height }))
        expect(canvasSize.width).toBeGreaterThan(0)
        expect(canvasSize.height).toBeGreaterThan(0)
        await page.getByRole('link', { name: '开始学习', exact: true }).click()
        await expect(page.getByRole('textbox', { name: '用户名', exact: true })).toBeVisible()
        await fits(page, width)
        await page.getByRole('button', { name: '手机验证码注册', exact: true }).click()
        await expect(page.getByLabel('手机号', { exact: true })).toBeVisible()
        await fits(page, width)
        await page.getByRole('button', { name: '返回登录', exact: true }).click()
        await page.getByRole('button', { name: '找回账号', exact: true }).click()
        await expect(page.getByRole('button', { name: '重置密码并找回账号', exact: true })).toBeVisible()
        await fits(page, width)
      })
    }
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', mode)
    await page.goto('/')
  }
})

test('学习端、管理表格和编辑弹窗在明暗模式窄屏下保持可操作', async ({ page }) => {
  test.setTimeout(90_000)
  await loginAsAdmin(page)
  for (const mode of ['light', 'dark'] as const) {
    await theme(page, mode)
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 })
      for (const path of ['/learn', '/training', '/admin/panel/users', '/admin/panel/challenges']) {
        await test.step(`${mode} ${width}px ${path}`, async () => {
          await page.goto(path)
          await expect(page.locator('main h2').first()).toBeVisible()
          await fits(page, width)
          if (path.startsWith('/admin') && width < 1024) await expect(page.getByRole('button', { name: '打开管理导航' })).toBeVisible()
        })
      }
    }
    await page.setViewportSize({ width: 320, height: 900 })
    await switchAdminChallengeBank(page, 'CTF 题目')
    await page.getByRole('row', { name: new RegExp(E2E.challenge.slug) }).getByRole('button', { name: '编辑', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await fits(page, 320)
    await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(dialog).not.toBeVisible()
  }
})

test('首页训练入口直接打开对应题库', async ({ page }) => {
  await loginAsStudent(page)
  for (const [label, bank, tab] of [['ACM 编程', 'acm', 'ACM 题目'], ['CTF 靶场', 'ctf', 'CTF 题目']]) {
    await page.goto('/')
    await page.getByRole('link', { name: new RegExp(`^${label}`) }).click()
    await expect(page).toHaveURL(new RegExp(`/training\\?bank=${bank}`))
    await expect(page.getByRole('tab', { name: tab, exact: true })).toHaveAttribute('aria-selected', 'true')
  }
})
