import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 回归：数据页在加载/错误/移动端/切 tab 时的状态不一致（2026-11-06 修复）。
// 修复前行为：
//   1. 题库移动端用 display:none 隐藏分数，手机上无法看到题目分值；
//   2. 数据加载失败只有 toast，页面随后显示空态，用户以为“没有数据”；
//   3. 表格加载期仍渲染“暂无数据”，切 tab 时上一个 tab 的旧行继续显示；
//   4. 只有一页时仍渲染分页器；
//   5. show-overflow-tooltip 是未实现的死属性，长 slug 无法查看完整值。
// 修复后行为：错误态留在页面内并可重试；表格加载期显示占位行且不闪空态；
//   切 tab 先清空旧行；单页隐藏分页器；移动端保留分数；截断列提供 title 提示。
// 复现路径：手机打开题库；断网/接口失败后打开徽章页；管理端切题目 tab；
//   邮箱只有一页时查看分页器。
test.describe('回归：数据页加载、错误与移动端状态一致性', () => {
  test('手机题库保留分数展示且页面不横向溢出（回归：分数被隐藏）', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await loginAsStudent(page)
    await page.route(/\/api\/v1\/challenges\?/, async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 600))
      await route.continue()
    })
    await page.goto('/training?bank=ctf')

    await expect(page.locator('.training-skeleton')).toBeVisible()
    const score = page.locator('.score').first()
    await expect(score).toBeVisible()
    await expect(score).toHaveText(/分/)

    await expect.poll(async () => page.evaluate(() =>
      document.documentElement.scrollWidth - document.documentElement.clientWidth,
    )).toBeLessThanOrEqual(1)
  })

  test('徽章加载失败时页面内显示错误并能重试恢复（回归：toast-only）', async ({ page }) => {
    await loginAsStudent(page)
    let fail = true
    await page.route('**/api/v1/badges', async (route) => {
      if (fail) await route.abort('failed')
      else await route.continue()
    })

    await page.goto('/badges')
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    await expect(alert.getByRole('button', { name: '重试' })).toBeVisible()

    fail = false
    await alert.getByRole('button', { name: '重试' }).click()
    await expect(alert).toHaveCount(0)
    await expect(page.locator('.badge').first()).toBeVisible()
  })

  test('邮箱表格加载期显示占位行、不闪空态，且单页不渲染分页器（回归：空态闪烁/单页分页）', async ({ page }) => {
    await loginAsStudent(page)
    await page.route(/\/api\/v1\/me\/mails\?/, async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 900))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, unread: 0, page: 1, page_size: 20 }),
      })
    })

    await page.goto('/mailbox')
    const table = page.locator('.ui-data-table')
    await expect(table.locator('.table-skeleton-bar').first()).toBeVisible()
    await expect(table.getByText('暂无邮件')).toHaveCount(0)

    await expect(table.getByText('暂无邮件')).toBeVisible()
    await expect(page.getByRole('navigation', { name: '分页' })).toHaveCount(0)
  })

  test('管理端切题目 tab 先清空旧行，长 slug 提供完整值提示（回归：旧行残留/死 tooltip）', async ({ page }) => {
    let delayNext = false
    await page.route(/\/api\/v1\/admin\/challenges\?/, async (route) => {
      if (delayNext) {
        delayNext = false
        await new Promise((resolve) => setTimeout(resolve, 900))
      }
      await route.continue()
    })

    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await page.getByRole('tab', { name: 'CTF 题目' }).click()

    const table = page.locator('.ui-data-table')
    await expect(table.getByText(E2E.challenge.slug)).toBeVisible()
    const slugCell = table.locator(`td[title="${E2E.challenge.slug}"]`)
    await expect(slugCell).toHaveCount(1)

    delayNext = true
    await page.getByRole('tab', { name: 'ACM 题目' }).click()
    await expect(table.locator('.table-skeleton-bar').first()).toBeVisible()
    await expect(table.getByText(E2E.challenge.slug)).toHaveCount(0)
  })
})
