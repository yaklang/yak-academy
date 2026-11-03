import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：题目详情难度标签直接显示 English slug（apprentice），
// 与题库列表/管理端使用的中文难度不一致（2026-11-03 修复）。
// 修复前行为：标题右侧标签为 apprentice。
// 修复后行为：显示「入门」，并补充分类标签「密码学」。
// 复现路径：题库 → 凯撒密码：找回偏移量。
test.describe('回归：题目详情难度与分类标签', () => {
  test('难度显示中文「入门」而不是 apprentice（回归：英文 slug 泄露）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto('/training/crypto-caesar-shift')
    const tags = page.locator('.tags')
    await expect(tags.getByText('入门', { exact: true })).toBeVisible()
    await expect(tags.getByText('apprentice', { exact: true })).toHaveCount(0)
    await expect(tags.getByText('密码学', { exact: true })).toBeVisible()
  })
})
