import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：管理端固定侧栏和表格撑宽手机页面（2026-11-01 修复；2026-11-02 随
// 管理端外壳与学生端统一为顶栏布局，回归断言同步迁移）。
// 修复前行为：320px 视口下侧栏始终占宽，主内容只剩约 100px，筛选区难以操作。
// 修复后行为：导航在手机上收进抽屉，管理端与学生端同一顶栏外壳，
//   表格仅在自己的区域内滚动，页面不溢出。
// 复现路径：管理员登录 → 打开管理端 → 抽屉导航 → 用户与容器。
test('手机访问用户与容器页时导航与表格不会撑宽页面（回归：横向溢出）', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 })
  await loginAsAdmin(page)
  await page.goto('/admin')
  await expect(page.getByRole('heading', { name: '教程预览', exact: true })).toBeVisible()
  await expect.poll(async () => page.getByRole('main').first().evaluate(el =>
    Math.round(el.getBoundingClientRect().width),
  )).toBeGreaterThanOrEqual(280)
  const navigation = page.getByRole('button', { name: '打开管理导航', exact: true })
  await expect(navigation).toBeVisible()
  await navigation.click()
  await page.getByRole('link', { name: '用户与容器', exact: true }).click()
  await expect(page.getByRole('heading', { name: '用户与容器', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '容器管理', exact: true })).toBeVisible()
  await expect.poll(async () => page.evaluate(() =>
    document.documentElement.scrollWidth - document.documentElement.clientWidth,
  )).toBeLessThanOrEqual(1)
})

// 回归（2026-11-02）：管理端外壳改为与学生端同构顶栏后，必须仍然只有
// 管理员能进入管理端，且顶栏入口收敛（不再出现 236px 侧栏与两级分组）。
test('管理端顶栏外壳：导航收敛为少量入口且分组次级 tab 可用', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/admin/panel/tutorials')

  // 顶栏主导航：2 个学习预览 + 4 个管理入口，无「容器/备份/审计」散项
  const nav = page.locator('.main-nav')
  await expect(nav.getByText('教程', { exact: true })).toBeVisible()
  await expect(nav.getByText('题库', { exact: true })).toBeVisible()
  await expect(nav.getByText('内容管理', { exact: true })).toBeVisible()
  await expect(nav.getByText('系统管理', { exact: true })).toBeVisible()
  await expect(nav.getByText('容器管理')).toHaveCount(0)
  await expect(nav.getByText('备份管理')).toHaveCount(0)
  await expect(nav.getByText('操作审计')).toHaveCount(0)

  // 内容管理分组：次级 tab 展开，可切到题目管理
  const subnav = page.getByRole('navigation', { name: '管理分组' })
  await expect(subnav.getByText('教程管理', { exact: true })).toBeVisible()
  await subnav.getByText('题目管理', { exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/panel\/challenges$/)

  // 系统管理分组只保留备份/审计/设置；容器管理已并入用户与容器页
  await page.getByRole('navigation', { name: '管理导航' }).getByText('系统管理', { exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/panel\/backups$/)
  await expect(subnav.getByText('备份管理', { exact: true })).toBeVisible()
  await expect(subnav.getByText('操作审计', { exact: true })).toBeVisible()
  await expect(subnav.getByText('容器管理', { exact: true })).toHaveCount(0)

  // 学生端外壳元素：返回学习端入口在顶栏
  await expect(page.getByRole('link', { name: '返回学习端' }).first()).toBeVisible()
})