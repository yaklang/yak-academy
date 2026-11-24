import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：管理端固定侧栏和表格撑宽手机页面（2026-11-01 修复；2026-11-02 起
// 管理端与学生端共用侧栏 + 顶栏外壳，回归断言随外壳迁移）。
// 修复前行为：320px 视口下侧栏始终占宽，主内容只剩约 100px，筛选区难以操作。
// 修复后行为：侧栏在手机上收进抽屉，表格仅在自己的区域内滚动，页面不溢出。
// 复现路径：管理员登录 → 打开管理端 → 抽屉导航 → 用户管理。
test('手机访问用户管理时导航与表格不会撑宽页面（回归：横向溢出）', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 })
  await loginAsAdmin(page)
  await page.goto('/admin')
  await expect(page.getByRole('heading', { name: '学习路线', exact: true })).toBeVisible()
  await expect.poll(async () => page.getByRole('main').first().evaluate(el =>
    Math.round(el.getBoundingClientRect().width),
  )).toBeGreaterThanOrEqual(280)
  const navigation = page.getByRole('button', { name: '打开导航', exact: true })
  await expect(navigation).toBeVisible()
  await navigation.click()
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户管理', exact: true })).toBeVisible()
  await expect.poll(async () => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1)
})

// 回归：管理端侧栏保留四个分区，页面入口直接可见，不再有第二级顶栏分组。
test('管理端侧栏外壳：分区与页面入口直达，可返回学习端', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/admin/panel/tutorials')

  const sections = page.getByRole('navigation', { name: '主导航' })
  for (const label of ['学习预览', '内容管理', '用户与竞赛', '系统管理']) {
    await expect(sections.getByRole('button', { name: label, exact: true })).toBeVisible()
  }

  const menu = page.getByRole('navigation', { name: '页面导航' })
  await expect(menu.getByText('教程管理', { exact: true })).toBeVisible()
  await menu.getByRole('link', { name: '题目管理', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/panel\/challenges$/)

  await sections.getByRole('button', { name: '系统管理', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/panel\/containers$/)
  await expect(menu.getByRole('link', { name: '备份管理', exact: true })).toBeVisible()
  await expect(menu.getByRole('link', { name: '操作审计', exact: true })).toBeVisible()

  await expect(page.getByRole('link', { name: '返回学习端', exact: true }).first()).toBeVisible()
})