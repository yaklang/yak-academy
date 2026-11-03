import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent, studentToken } from './helpers'

// 账号信息页：能力画像按题目分类聚合；雷达图与文字图例同源，
// 数据来自 /me/capabilities（只统计当前可见题目）。
test.describe('账号信息能力画像', () => {
  test('按分类展示通关覆盖，雷达图与图例一致且移动端无横向溢出', async ({ page, request }) => {
    const token = await studentToken(request)
    const response = await request.get(`${E2E.apiURL}/api/v1/me/capabilities`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(response.ok()).toBeTruthy()
    const capability = (await response.json()) as {
      dimensions: { category: string; solved: number; total: number }[]
      solved: number
      total: number
    }
    expect(capability.total).toBeGreaterThan(0)
    expect(capability.dimensions.length).toBeGreaterThan(0)

    await loginAsStudent(page)
    await page.goto('/profile')

    await expect(page.getByRole('heading', { name: '账号信息' })).toBeVisible()
    const card = page.getByTestId('capability-card')
    await expect(card.getByRole('heading', { name: '能力画像' })).toBeVisible()
    await expect(card.getByRole('img', { name: /能力画像雷达图/ })).toBeVisible()

    // 图例逐项出现，题数口径与接口一致（覆盖率而非只展示通关数）。
    const legend = card.locator('.legend-row')
    await expect(legend).toHaveCount(capability.dimensions.length)
    const first = capability.dimensions[0]
    await expect(legend.first()).toContainText(`${first.solved}/${first.total}`)

    // 移动端：能力画像与账号资料纵向堆叠，页面不产生横向滚动。
    await page.setViewportSize({ width: 390, height: 844 })
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)
    expect(overflow).toBe(false)
    await expect(card).toBeVisible()
  })
})
