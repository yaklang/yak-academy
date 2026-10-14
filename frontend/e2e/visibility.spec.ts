import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 可见性旅程：管理端开关/定时发布 → 学生端列表与直连 404 双侧验证。
// 语义：隐藏或未到发布时间 = 学生端整体不存在（列表不出现、直链 404）。
const { challenge: C } = E2E

test.describe('可见性旅程', () => {
  test('隐藏题目：学生列表消失、直链 404，恢复后可见', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
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
    const row2 = page.locator('.el-table tr', { hasText: C.slug })
    await row2.locator('.el-switch').click()
    await expect(page.locator('.el-message', { hasText: '已对学生可见' })).toBeVisible()

    await loginAsStudent(page)
    await page.goto('/training')
    await expect(page.getByRole('link', { name: C.title })).toBeVisible()
  })

  test('定时发布：未来时间学生不可见', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    const row = page.locator('.el-table tr', { hasText: C.attachSlug })

    // 打开定时发布弹窗，选“定时发布”模式并填未来时间
    await row.getByRole('button', { name: '定时' }).click()
    const dialog = page.locator('.el-dialog', { hasText: '定时发布' })
    await dialog.getByText('定时发布', { exact: true }).click()
    const future = new Date(Date.now() + 3_600_000) // 1 小时后
    await dialog.locator('.el-date-editor input').fill(
      `${future.getFullYear()}-${String(future.getMonth() + 1).padStart(2, '0')}-${String(future.getDate()).padStart(2, '0')} ${String(future.getHours()).padStart(2, '0')}:00:00`,
    )
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(page.locator('.el-message', { hasText: '已设定发布时间' })).toBeVisible()

    // 行内出现“定时”标注
    await expect(row.locator('.release-note')).toContainText(/定时/)

    // 学生端：题目整体 404（列表与直链）
    await loginAsStudent(page)
    await page.goto('/training')
    await expect(page.getByRole('link', { name: C.attachTitle })).toHaveCount(0)
    await page.goto(`/training/${C.attachSlug}`)
    await expect(page.getByRole('heading', { name: C.attachTitle })).toHaveCount(0)

    // 清理：恢复立即语义，避免影响其他用例
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    const row2 = page.locator('.el-table tr', { hasText: C.attachSlug })
    await row2.getByRole('button', { name: '定时' }).click()
    const dialog2 = page.locator('.el-dialog', { hasText: '定时发布' })
    await dialog2.getByText('立即语义', { exact: false }).click()
    await dialog2.getByRole('button', { name: '保存' }).click()
    await expect(page.locator('.el-message', { hasText: '已清除定时' })).toBeVisible()
  })
})