import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 用户与容器合并页：两套管理视图同页 tab，默认用户管理；
// 容器 tab 通过 ?tab=containers 深链，旧 /admin/panel/containers 路径兼容重定向。
test.describe('用户与容器合并页', () => {
  test('默认用户 tab，切到容器 tab 可深链且旧路径自动重定向', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/tutorials')

    // 从管理端顶栏进入合并页，默认落在用户管理。
    await page.getByRole('navigation', { name: '管理导航' }).getByText('用户与容器', { exact: true }).click()
    await expect(page).toHaveURL(/\/admin\/panel\/users$/)
    await expect(page.getByRole('heading', { name: '用户与容器' })).toBeVisible()

    const usersTab = page.getByRole('tab', { name: '用户管理' })
    const containersTab = page.getByRole('tab', { name: '容器管理' })
    await expect(usersTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号')).toBeVisible()

    // 切到容器管理：URL 同页深链，用户表格卸载，性能面板出现。
    await containersTab.click()
    await expect(page).toHaveURL(/\/admin\/panel\/users\?tab=containers$/)
    await expect(containersTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('主机性能', { exact: true })).toBeVisible()
    await expect(page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号')).toHaveCount(0)

    // 旧书签 / 外部链接仍可用。
    await page.goto('/admin/panel/containers')
    await expect(page).toHaveURL(/\/admin\/panel\/users\?tab=containers$/)
    await expect(containersTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('主机性能', { exact: true })).toBeVisible()

    // 切回用户管理时恢复干净 URL。
    await usersTab.click()
    await expect(page).toHaveURL(/\/admin\/panel\/users$/)
    await expect(usersTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号')).toBeVisible()
  })
})
