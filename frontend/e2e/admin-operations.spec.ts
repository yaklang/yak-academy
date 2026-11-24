import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 运维管理页页面级覆盖：备份记录（生成/刷新）与操作审计（动作映射/筛选）。
// 这两页此前只有 API 覆盖或完全没有 e2e，重构后补上页面旅程。
test.describe('运维管理页', () => {
  test('备份管理可以生成并列出备份', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/backups')
    await expect(page.getByRole('heading', { level: 1, name: '备份管理' })).toBeVisible()
    await expect(page.getByRole('table', { name: '备份记录' })).toBeVisible()

    await page.getByRole('button', { name: '立即备份', exact: true }).first().click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '备份完成' })).toBeVisible()
    await expect(page.locator('.backup-name').first()).toBeVisible()
    await expect(page.getByRole('table', { name: '备份记录' })).toContainText('本地数据目录')
  })

  test('操作审计显示中文动作并可按动作筛选', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/audit-logs')
    await expect(page.getByRole('heading', { level: 1, name: '操作审计' })).toBeVisible()
    const table = page.getByRole('table', { name: '操作审计记录' })
    await expect(table).toBeVisible()
    // 全局种子必定产生“创建账号”记录；页面不得回退显示英文动作码。
    const rows = table.locator('tbody tr')
    await expect(rows.first()).toBeVisible()
    await expect(table.locator('.audit-action').first()).toBeVisible()
    await expect(table.locator('.audit-action').first()).not.toContainText(/[a-z]+\.[a-z_]+/)

    await page.getByRole('combobox', { name: '动作' }).selectOption({ label: '创建账号' })
    await expect(page.getByRole('table', { name: '操作审计记录' })).toBeVisible()
    const actions = page.locator('.audit-action')
    await expect(actions.first()).toHaveText('创建账号')
    const count = await actions.count()
    for (let i = 0; i < count; i += 1) await expect(actions.nth(i)).toHaveText('创建账号')

    await page.getByRole('button', { name: '清除筛选', exact: true }).click()
    await expect(page.getByRole('combobox', { name: '动作' })).toHaveValue('')
  })
})