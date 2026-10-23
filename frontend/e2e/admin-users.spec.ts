import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 管理端用户管理旅程：表格 + 过滤/搜索（q/role/active/no_solves）。
// 种子学生账号由 global-setup 创建（本套件串行、DB 每整跑清空），
// 学生账号必然「活跃」（最近登录或注册在 6 个月内——从未登录按注册起算）。
test.describe('管理端用户管理', () => {
  test('表格展示账号编码与画像标记；搜索/角色/不活跃过滤生效', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()

    // 学生行：账号编码（YA{YY}-{NNNNN}）+ 活跃标记。
    const stuRow = page.locator('tr', { hasText: E2E.student.username })
    await expect(stuRow).toBeVisible()
    await expect(stuRow.locator('code')).toHaveText(/^YA\d{2}-\d{5}$/)
    await expect(stuRow.locator('.el-tag', { hasText: '活跃' })).toBeVisible()
    await expect(stuRow.locator('.el-tag', { hasText: '普通人' })).toBeVisible()

    // 搜索：按账号编码（新维度的核心断言——编码是导入表格的条目来源）。
    const code = await stuRow.locator('code').textContent()
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 手机号').fill(code!)
    await page.getByRole('button', { name: '搜索' }).click()
    await expect(stuRow).toBeVisible()
    // 用户名子串同样命中。
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 手机号').fill('stu-e2e')
    await page.getByRole('button', { name: '搜索' }).click()
    await expect(stuRow).toBeVisible()
    // 无命中：空表。
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 手机号').fill('no-such-user-xyz')
    await page.getByRole('button', { name: '搜索' }).click()
    await expect(page.getByText('暂无用户')).toBeVisible()

    // 角色过滤：管理员只剩 admin 行（下拉按 DOM 序：角色/活跃/解题）。
    await page.getByPlaceholder('搜索：用户名 / 账号编码 / 手机号').fill('')
    await page.getByRole('button', { name: '搜索' }).click()
    const selects = page.locator('.filters .el-select')
    await selects.nth(0).click()
    await page.locator('.el-select-dropdown__item', { hasText: '管理员' }).click()
    await expect(page.locator('tr', { hasText: E2E.admin.username })).toBeVisible()
    await expect(page.locator('tr', { hasText: E2E.student.username })).toHaveCount(0)

    // 重载复位过滤（过滤状态为组件内状态）：不活跃过滤（active=false）
    // 不含刚建的学生（从未登录按注册起算，注册在本套件内 → 必然活跃）。
    await page.reload()
    const stuRow2 = page.locator('tr', { hasText: E2E.student.username })
    await expect(stuRow2).toBeVisible()
    await selects.nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '不活跃（6 个月未登录）' }).click()
    await expect(page.locator('tr', { hasText: E2E.student.username })).toHaveCount(0)

    // 组合画像过滤按钮：不活跃 ∩ 从未完成题目（表格正常出数即可——
    // 判定口径的覆盖在后端单测 user_list_test）。
    await page.getByRole('button', { name: '不活跃且从未解题' }).click()
    await expect(page.getByRole('button', { name: '搜索' })).toBeVisible()
  })
})
