import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent, switchAdminChallengeBank } from './helpers'

// 可见性旅程：管理端开关 → 学生端列表与直连 404 双侧验证。
// 语义：隐藏 = 学生端整体不存在（列表不出现、直链 404）。
// （定时发布功能已随操作列精简移除；后端 release_at 能力仍在契约中保留。）
const { challenge: C } = E2E

test.describe('可见性旅程', () => {
  test('隐藏题目：学生列表消失、直链 404，恢复后可见', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, 'CTF 题目')
    const row = page.locator('.el-table tr', { hasText: C.slug })
    const toggle = row.locator('.el-switch')
    await expect(toggle).toHaveClass(/is-checked/, { timeout: 10_000 })

    // 关闭可见性开关
    await toggle.click()
    await expect(page.locator('.el-message', { hasText: '已隐藏' })).toBeVisible()
    // 开关状态翻转
    await expect(row.locator('.el-switch')).not.toHaveClass(/is-checked/)

    // 学生端：列表无此题
    await loginAsStudent(page)
    await page.goto('/training')
    await expect(page.getByRole('link', { name: C.title })).toHaveCount(0)

    // 学生端：直连详情 404（错误提示 + 页面无题目标题，骨架外无内容）
    await page.goto(`/training/${C.slug}`)
    await expect(page.getByRole('heading', { name: C.title })).toHaveCount(0)
    await expect(page.locator('.el-message')).toContainText(/not found|不存在/i)

    // 恢复可见：学生立即可见
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, 'CTF 题目')
    const row2 = page.locator('.el-table tr', { hasText: C.slug })
    await row2.locator('.el-switch').click()
    await expect(page.locator('.el-message', { hasText: '已对学生可见' })).toBeVisible()

    await loginAsStudent(page)
    await page.goto('/training')
    // 上传的种子题归 CTF 题目 tab（教程题目专属内置题集）
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.getByRole('link', { name: C.title })).toBeVisible()
  })
})