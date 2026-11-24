import { expect, test } from '@playwright/test'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 回归：应用外壳（侧栏 + 顶栏）与页面内容必须共用同一条左侧基线。
// 2026-11 重构前的旧顶栏布局曾出现 brand/次级导航 32px、正文 112px 的错位；
// 新外壳固定为 48px 图标栏 + 224px 菜单，内容区独立滚动，二者不得重叠。
test.describe('回归：外壳与内容对齐', () => {
  test('学生端页面标题与内容面板左对齐，侧栏不覆盖内容', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsStudent(page)
    await page.goto('/training')

    const sidebar = await page.locator('.app-sidebar').boundingBox()
    const main = await page.locator('.app-content').boundingBox()
    const title = await page.locator('.page-header').boundingBox()
    const panel = await page.locator('.data-table-panel').first().boundingBox()
    expect(sidebar).not.toBeNull()
    expect(main).not.toBeNull()
    expect(title).not.toBeNull()
    expect(panel).not.toBeNull()
    expect(sidebar!.x + sidebar!.width).toBeLessThanOrEqual(main!.x + 1)
    expect(Math.abs(title!.x - panel!.x)).toBeLessThanOrEqual(1)
  })

  test('管理端页面标题与内容面板左对齐，不再有第二层次级导航', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')

    const title = await page.locator('.page-header').boundingBox()
    const panel = await page.locator('.data-table-panel').first().boundingBox()
    expect(title).not.toBeNull()
    expect(panel).not.toBeNull()
    expect(Math.abs(title!.x - panel!.x)).toBeLessThanOrEqual(1)
    await expect(page.locator('.admin-subnav')).toHaveCount(0)
  })
})
