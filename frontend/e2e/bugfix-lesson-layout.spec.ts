import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：教程正文列表序号丢失、流程卡与阅读列不对齐（PR #44，2026-11-03 修复）。
// 修复前行为：Tailwind preflight 将 ol/ul 的 list-style 重置为 none，有序步骤只剩缩进；
//             流程卡/视频最大宽度 860px，而正文阅读列是 780px，右边缘参差。
// 修复后行为：正文恢复 decimal/disc 列表标记；流程卡与阅读列同宽（780px）。
// 复现路径：学生登录 → 教程首页 → Web 安全 → SQL 注入基础 Room，查看流程与正文列表。
test.describe('回归：教程排版', () => {
  test('正文列表显示序号且流程卡与阅读列对齐（回归：列表无序号/宽度参差）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`)

    const article = page.locator('.lesson')
    const flow = page.locator('.lesson-flow')
    await expect(article).toBeVisible()
    await expect(flow).toBeVisible()

    const articleBox = await article.boundingBox()
    const flowBox = await flow.boundingBox()
    if (!articleBox || !flowBox) throw new Error('lesson or flow is not visible')
    expect(flowBox.width).toBeCloseTo(articleBox.width, 0)

    const orderedList = article.locator('ol').first()
    await expect(orderedList).toBeVisible()
    await expect(orderedList).toContainText('探测列数')
    await expect(orderedList.locator('li')).toHaveCount(2)
    expect(await orderedList.evaluate((el) => getComputedStyle(el).listStyleType)).toBe('decimal')

    const unorderedList = article.locator('ul').first()
    await expect(unorderedList).toBeVisible()
    await expect(unorderedList).toContainText('先手工验证单条 payload')
    expect(await unorderedList.evaluate((el) => getComputedStyle(el).listStyleType)).toBe('disc')
  })
})
