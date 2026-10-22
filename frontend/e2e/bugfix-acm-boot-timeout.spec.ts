import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：ACM 判题把 yak 引擎冷启动/退出滞留计入题目时限，正确提交被误判 TLE
//（2026-10-22 修复）。
// 修复前行为：时限只约束引擎进程全程（含解释器装载编译与退出滞留，native
// 热跑约 2.3s），1s 时限的正确程序必 TLE。
// 修复后行为：判题计时从用户代码开始执行算起（引擎注入的起止标记之间），
// 引擎启动/收尾只受兜底上限约束，正确程序 AC。
// 复现路径：学生打开 1s 时限 ACM 题 → 提交正确代码（println(42)，执行仅数毫秒）
// → 判定结果应为通过。
const engineConfigured = Boolean(process.env.ACADEMY_YAK_BIN)

test.describe('回归：ACM 时限不含引擎启动', () => {
  test('1 秒时限题提交正确程序判 AC（回归：误 TLE）', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acmBoot.slug}`)

    await expect(page.getByRole('heading', { name: E2E.acmBoot.title })).toBeVisible()
    await page.locator('.cm-content').fill(E2E.acmBoot.correctSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    // 执行段仅数毫秒：AC 证明计时起点是用户代码而非引擎进程启动
    await expect(page.locator('.result')).toContainText('判定通过，得分', { timeout: 30_000 })
    await expect(page.locator('.result')).toHaveClass(/el-alert--success/)
  })
})
