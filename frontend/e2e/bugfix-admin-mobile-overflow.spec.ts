import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：管理端固定侧栏和表格撑宽手机页面（2026-11-01，随本次界面重构修复）。
// 修复前行为：320px 视口下侧栏始终占宽，主内容只剩约 100px，筛选区难以操作。
// 修复后行为：导航在手机上收进抽屉，表格仅在自己的区域内滚动，页面不溢出。
// 复现路径：管理员登录 → 打开管理端 → 用户管理。
test('手机访问用户管理时导航与表格不会撑宽页面（回归：横向溢出）', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 })
  await loginAsAdmin(page)
  await page.goto('/admin')
  await expect(page.getByRole('heading', { name: '学习路线', exact: true })).toBeVisible()
  await expect.poll(async () => page.getByRole('main').first().evaluate(el =>
    Math.round(el.getBoundingClientRect().width),
  )).toBeGreaterThanOrEqual(280)
  const navigation = page.getByRole('button', { name: '打开管理导航', exact: true })
  await expect(navigation).toBeVisible()
  await navigation.click()
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  await expect.poll(async () => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1)
})
