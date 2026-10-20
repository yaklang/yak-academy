import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：内置训练集目录瘦身（3 道独立模块题合并为 1 道综合题）后，旧库重导入
// 残留已下线题目（2026-10-19 修复，随 refactor/module-case-integration）。
// 修复前行为：导入仅 upsert 不清理——历史版本导入过的内置题（fuzzer-headers /
//   mitm-session / codec-pipeline）在题库与教程页永久残留，与学生新看到的
//   目录（12 题）不一致。
// 修复后行为：导入以目录为唯一事实源——目录外残留随重导入级联清理，汇总提示
//   带「清理目录外残留 N 题」；目录一致时 removed=0 不出现清理文案。
// 复现路径：管理员在管理端题库点「导入内置训练（vulinbox）」重导入——全新库
//   无残留（removed=0），残留场景的数据准备在单测
//   TestImportBuiltinPrunesRemovedEntries 覆盖（e2e 无法跑两个二进制版本）；
//   这里锚定用户可见面：导入后的表格行集合与幂等性（重复导入 removed=0）。
test.describe('回归：内置导入清理目录外残留', () => {
  test('重导入后内置题行与目录一致且幂等，汇总提示含清理计数位（回归：残留）', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')

    // 触发动作：一键导入 → 确认弹窗 → 汇总提示（含教程计数）
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新', exact: true }).click()
    await expect(
      page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+：新增 \d+ 题、更新 \d+ 题、教程 12 篇/ }),
    ).toBeVisible()
    // 全新库无目录外残留：不得出现清理文案（removed=0 时不加后缀）
    await expect(page.locator('.el-message', { hasText: '清理目录外残留' })).toHaveCount(0)

    // 终态断言：目录内题目在表格可见，已下线的 3 道旧模块题永不可见
    const table = page.locator('.el-table')
    await expect(table.locator('tr', { hasText: 'vulinbox-mall-chain' })).toBeVisible()
    for (const staleSlug of ['vulinbox-fuzzer-headers', 'vulinbox-mitm-session', 'vulinbox-codec-pipeline']) {
      await expect(table.locator('tr', { hasText: staleSlug })).toHaveCount(0)
    }

    // 重复导入幂等：无新增、无清理，目录与库保持一致
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新', exact: true }).click()
    await expect(
      page.locator('.el-message', { hasText: '新增 0 题、更新 12 题、教程 12 篇' }),
    ).toBeVisible()
    await expect(page.locator('.el-message', { hasText: '清理目录外残留' })).toHaveCount(0)
  })
})