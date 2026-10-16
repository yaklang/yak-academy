import { expect, test } from '@playwright/test'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 内置训练（vulinbox）一键导入旅程：管理端确认弹窗 → 导入 → 幂等重导入
// （新增 0 / 全量更新）→ 管理表格出现 vulinbox 题 → 学生题库「教程题目」tab
// 分组可见 + CTF/ACM tab 空态 + 「是否内置」过滤。
// 默认 e2e 后端为 noop 驱动，恰好覆盖 image_built=false 提示路径；
// 断言只锚定两种驱动下都成立的汇总文案与表格/列表行，不区分镜像是否构建。
test.describe('内置训练一键导入', () => {
  test('管理端一键导入 vulinbox 题集，重复导入即更新，学生端题库可见', async ({ page }) => {
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

    // 学生题库：默认「教程题目」tab，vulinbox 分类分组可见
    await loginAsStudent(page)
    await page.getByRole('link', { name: '题库' }).click()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'jwt' })).toBeVisible()

    // CTF / ACM 题目 tab 暂无内容：空态文案 + 无题目行
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.locator('.el-empty')).toContainText('暂无CTF 题目')
    await expect(page.locator('.row')).toHaveCount(0)
    await page.getByRole('tab', { name: 'ACM 题目' }).click()
    await expect(page.locator('.el-empty')).toContainText('暂无ACM 题目')

    // 「是否内置」过滤（服务端）：内置只剩 vulinbox 题，管理员上传只剩种子题
    await page.getByRole('tab', { name: '教程题目' }).click()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toBeVisible()
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：内置题集' }).click()
    await expect(page.locator('.row', { hasText: 'SSRF 基础：让服务器替你访问内网接口' })).toBeVisible()
    await expect(page.locator('.row', { hasText: '凯撒密码：找回偏移量' })).toHaveCount(0)
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：管理员上传' }).click()
    await expect(page.locator('.row', { hasText: '凯撒密码：找回偏移量' })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toHaveCount(0)

    // 题目可进详情
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：全部' }).click()
    await page.getByRole('link', { name: 'SSRF 基础：让服务器替你访问内网接口' }).click()
    await expect(page).toHaveURL(/\/training\/vulinbox-ssrf-basic$/)
    await expect(
      page.getByRole('heading', { name: 'SSRF 基础：让服务器替你访问内网接口' }),
    ).toBeVisible()
  })
})
