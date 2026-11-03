import { expect, test } from '@playwright/test'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 回归：两端外壳的顶栏/次级导航与正文左边缘不对齐（2026-11-03 修复）。
// 修复前行为：学生端 brand 左边缘 32px、正文 112px；管理端次级导航 32px、正文 112px。
// 修复后行为：共享同一内容宽度，brand/次级导航与页面标题对齐。
// 复现路径：任意学习端或管理端页面，宽屏下观察左侧对齐线。
test.describe('回归：外壳内容左边缘对齐', () => {
  test('学生端顶栏与页面标题左对齐（回归：外壳宽度不一致）', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsStudent(page)
    await page.goto('/training')
    const brand = await page.locator('.academy-brand').boundingBox()
    const title = await page.locator('.page-title').boundingBox()
    expect(brand).not.toBeNull()
    expect(title).not.toBeNull()
    expect(Math.abs(brand!.x - title!.x)).toBeLessThanOrEqual(1)
  })

  test('管理端次级导航与页面标题左对齐（回归：次级导航内宽过大）', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    const subnav = await page.locator('.admin-subnav-inner a').first().boundingBox()
    const title = await page.locator('.page-title').boundingBox()
    expect(subnav).not.toBeNull()
    expect(title).not.toBeNull()
    expect(Math.abs(subnav!.x - title!.x)).toBeLessThanOrEqual(1)
  })
})
