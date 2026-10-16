import { expect, test } from '@playwright/test'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 内置训练（vulinbox）一键导入旅程：管理端确认弹窗 → 导入 → 幂等重导入
// （新增 0 / 全量更新）→ 管理表格出现 vulinbox 题 → 学生训练场分组可见。
// 默认 e2e 后端为 noop 驱动，恰好覆盖 image_built=false 提示路径；
// 断言只锚定两种驱动下都成立的汇总文案与表格/列表行，不区分镜像是否构建。
test.describe('内置训练一键导入', () => {
  test('管理端一键导入 vulinbox 题集，重复导入即更新，学生端可见', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')

    // 首次导入：确认弹窗 → 汇总提示（全新库为新增 11；KEEP_STATE 复跑可能全量更新，故只锚定句式）
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新' }).click()
    await expect(page.locator('.el-message', { hasText: /内置训练 v1\.0\.0：新增 \d+ 题、更新 \d+ 题/ })).toBeVisible()

    // 管理表格出现内置题（覆盖 regex 判定题与 static 问答题各抽一）
    const table = page.locator('.el-table')
    await expect(table.locator('tr', { hasText: 'vulinbox-ssrf-basic' })).toBeVisible()
    await expect(table.locator('tr', { hasText: 'vulinbox-jwt-none' })).toBeVisible()

    // 重复导入 = 更新：本次运行首轮已全量落库，第二轮必然新增 0、更新 11
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新' }).click()
    await expect(page.locator('.el-message', { hasText: '新增 0 题、更新 11 题' })).toBeVisible()

    // 学生训练场：vulinbox 分类分组可见，题目可进详情
    await loginAsStudent(page)
    await page.getByRole('link', { name: '训练' }).click()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'jwt' })).toBeVisible()
    await page.getByRole('link', { name: 'SSRF 基础：让服务器替你访问内网接口' }).click()
    await expect(page).toHaveURL(/\/training\/vulinbox-ssrf-basic$/)
    await expect(
      page.getByRole('heading', { name: 'SSRF 基础：让服务器替你访问内网接口' }),
    ).toBeVisible()
  })
})
