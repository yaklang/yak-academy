import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：空题库 tab 仍渲染零值统计、搜索/来源筛选与 tab 横向滚动条，
// 短视口下这些无效内容把页面撑出整页滚动条（2026-11-03 修复）。
// 修复前行为：教程题目空 tab 显示「容器题 0 / 附件题 0」、空白难度条、筛选栏；
// 1440x613 下文档高度 656 > 视口 613，出现右侧滚动条。
// 修复后行为：空题库只保留标题、tab 与空态；统计/筛选不渲染，页面不产生失焦滚动。
// 复现路径：题库 → 教程题目（内置题集尚未导入时）。
test.describe('回归：空题库页面的无效工具栏与滚动条', () => {
  test('空教程题目 tab 不渲染零值统计和筛选，短视口无整页滚动（回归：空页滚动条）', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 613 })
    await loginAsStudent(page)
    // 内置题集会被其他 spec 导入，空题库状态不能依赖全局种子的时序；
    // 只固定列表响应为空，导航、渲染与布局仍走真实页面。
    await page.route(/\/api\/v1\/challenges\?/, (route) => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], total: 0 }),
    }))
    await page.goto('/training?bank=tutorial')

    const empty = page.getByTestId('training-empty')
    await expect(empty).toBeVisible()
    await expect(empty.getByText('暂无教程题目')).toBeVisible()
    await expect(page.locator('.stats-strip')).toHaveCount(0)
    await expect(page.locator('.filters')).toHaveCount(0)
    // tab 条不产生横向滚动（用户可见结果），不断言具体滚动条样式。
    expect(await page.locator('.tabs-scroll').evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight + 1)).toBe(true)
  })
})
