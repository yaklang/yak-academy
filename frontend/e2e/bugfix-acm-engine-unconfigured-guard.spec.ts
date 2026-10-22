import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：判题引擎未装配（ACADEMY_YAK_BIN 未配置）时提交/自测按钮仍可点击，
// 学生点击后才收到「判定服务暂不可用」/422 滞后报错（2026-10-22 修复）。
// 修复前行为：详情页按钮按源码是否为空启用，点击提交 → internal_error
// 「判定服务暂不可用」；点击自测 → 422 toast，无任何事前提示。
// 修复后行为：详情 judge.engine_available=false 随题目下发，页面顶部警示
// 「判题引擎未配置」，提交代码与运行自测按钮禁用（编辑器仍可编码）。
// 复现路径：学生在未装配引擎的部署上打开 ACM 题 → 写代码 → 想提交/自测。
const engineConfigured = Boolean(process.env.ACADEMY_YAK_BIN)

test.describe('回归：引擎未装配禁用提交/自测入口', () => {
  test('引擎未装配 → 警示可见且两按钮禁用，不再是点击后才报错', async ({ page }) => {
    test.skip(engineConfigured, '判题引擎已装配，禁用态路径不适用')
    // bugfix-* 字母序最先跑，吃满 vite 对详情页大 chunk 的冷编译
    test.setTimeout(90_000)
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    // 事前提示：页面警示条点名引擎未配置（heading 放宽超时：首访冷编译慢）
    await expect(page.getByRole('heading', { name: E2E.acm.title })).toBeVisible({ timeout: 20_000 })
    const alert = page.locator('.engine-missing')
    await expect(alert).toBeVisible()
    await expect(alert).toContainText('判题引擎未配置')
    await expect(alert).toContainText('请联系管理员')

    // 编辑器仍可编码（守卫只挡提交/自测，不挡写代码）
    await page.locator('.cm-content').fill(E2E.acm.correctSource)

    // 写入源码后两入口仍禁用（修复前：source 非空即启用，可点击）
    await expect(page.getByRole('button', { name: '提交代码' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '运行自测' })).toBeDisabled()
  })
})
