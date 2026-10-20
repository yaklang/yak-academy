import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 教程管理页「内置训练集」区块旅程：目录展示（帐面清单来自平台分发资产，
// 不依赖已导入）→ 重新导入（一教程对一题，幂等更新）→ 目录逐篇「已导入」、
// 区块状态「已导入」。内置教程无删除/编辑操作（区块只有导入按钮；服务端按
// is_builtin 标记拦截覆盖与删除，见后端单测）。
// 套件串行共享一个后端库：builtin-import.spec 可能先导入过内置集，本 spec
// 不假设初始导入状态，断言锚定「目录→导入/更新→已导入」的收敛结果。
// 默认 e2e 后端为 noop 驱动：导入提示「未配置 Docker」路径（image_built=false）。
test.describe('教程管理内置训练集', () => {
  test('内置教程目录可见；导入/更新后逐篇已导入；区块无删除/编辑操作', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/tutorials')

    // 内置训练集卡片：版本 tag + 目录摘要（5 专题 / 12 篇，与二进制资产一致）
    const card = page.locator('.builtin-card')
    await expect(card).toBeVisible()
    await expect(card).toContainText('内置训练集（vulinbox）')
    await expect(card).toContainText('5 个专题 / 12 篇教程')
    await expect(card).toContainText('不支持删除与编辑')

    // 区块内除导入/目录外无任何删除/编辑入口
    const importBtn = card.getByRole('button', { name: /导入内置训练|重新导入\/更新/ })
    await expect(importBtn).toBeVisible()
    await expect(card.getByRole('button', { name: /删除|编辑|移除|归档|发布|回滚/ })).toHaveCount(0)

    // 展开目录：Room 行可见（导入状态由服务端探测，收敛断言见导入后）
    await card.getByRole('button', { name: /展开目录/ }).click()
    const roomRow = card.locator('.builtin-rooms li', { hasText: 'SSRF 初探：让服务器替你访问内网' })
    await expect(roomRow).toBeVisible()

    // 导入/更新：确认弹窗 → 幂等更新（全新库新增 12；已导入则更新 12——锚定教程数）
    // exact：卡片按钮「重新导入/更新」含子串"导入/更新"，非 exact 会同时命中两个。
    await importBtn.click()
    await page.getByRole('button', { name: '导入/更新', exact: true }).click()
    await expect(
      page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+：新增 \d+ 题、更新 \d+ 题、教程 12 篇/ }),
    ).toBeVisible()

    // 导入后：区块状态「已导入」、按钮文案「重新导入/更新」、Room 行逐篇已导入
    await expect(card.locator('.el-tag', { hasText: '已导入' }).first()).toBeVisible()
    await expect(card.getByRole('button', { name: '重新导入/更新' })).toBeVisible()
    await expect(roomRow.locator('.el-tag')).toHaveText('已导入')
    await expect(roomRow).not.toHaveClass(/not-imported/)

    // 上传内容包表格不受影响：global-setup 的种子教程包仍在列表
    await expect(page.locator('.el-table').locator('tr', { hasText: 'e2e-test' })).toBeVisible()
  })
})