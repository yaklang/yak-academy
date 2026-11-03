import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：教程页“前往配套题目练习”远离正文、悬在页面右上角（PR #44，2026-11-03 修复）。
// 修复前行为：page-head 横跨 1040px 全宽，入口被推到页面右边缘，与 780px 阅读列脱节。
// 修复后行为：page-head 限制在阅读列宽度内，入口右边缘与流程卡/正文列对齐。
// 复现路径：学生登录 → 教程首页 → Web 安全 → SQL 注入基础 Room，观察标题行右侧入口。
test.describe('回归：教程配套练习入口', () => {
  test('配套练习入口与正文阅读列对齐（回归：入口悬在页面最右侧）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`)

    const head = page.locator('.page-head')
    const flow = page.locator('.lesson-flow')
    const action = page.getByRole('link', { name: /前往配套题目练习/ })
    await expect(flow).toBeVisible()
    await expect(action).toBeVisible()

    const headBox = await head.boundingBox()
    const flowBox = await flow.boundingBox()
    const actionBox = await action.boundingBox()
    if (!headBox || !flowBox || !actionBox) throw new Error('lesson header is not visible')

    expect(headBox.width).toBeCloseTo(flowBox.width, 0)
    expect(actionBox.x).toBeGreaterThanOrEqual(flowBox.x)
    expect(Math.abs(actionBox.x + actionBox.width - (flowBox.x + flowBox.width))).toBeLessThanOrEqual(1)
  })
})
