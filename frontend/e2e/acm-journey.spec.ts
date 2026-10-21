import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// ACM 编程题旅程：源码提交判定（对齐 ICPC 模型），不走 flag。
// 引擎由 ACADEMY_YAK_BIN 门控（playwright.config 注入后端）：
// - 装配（本地设 yak 可执行路径）：正确源码 AC 计分、错误输出 WA；
// - 未装配（CI）：正确源码判 internal_error（平台侧失败，可重试提示）。
const engineConfigured = Boolean(process.env.ACADEMY_YAK_BIN)

test.describe('ACM 编程题（源码判定）', () => {
  test('详情页展示样例数据卡与源码提交框，无 flag 输入', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    // 题面与编程题标识
    await expect(page.getByRole('heading', { name: E2E.acm.title })).toBeVisible()
    await expect(page.getByText('编程题 · 5s / 128MB')).toBeVisible()

    // 样例数据卡：sample=true 的组（demo/edge）可见，隐藏组不下发
    // （h4 定位避免与提交区提示文案的「样例数据」字样撞 strict mode）
    await expect(page.getByRole('heading', { name: '样例数据' })).toBeVisible()
    for (const name of E2E.acm.samples) {
      await expect(page.locator('.sample-group', { hasText: name })).toBeVisible()
    }
    await expect(page.locator('.sample-group')).toHaveCount(E2E.acm.samples.length)
    const demo = page.locator('.sample-group', { hasText: 'demo' })
    await expect(demo).toContainText('2 3') // demo 组输入
    await expect(demo).toContainText('30') // demo 组期望输出

    // 源码提交框存在，flag 输入不存在（ACM 不通过 flag 判定）
    await expect(page.getByPlaceholder('粘贴 yak 源码（从标准输入读取数据，向标准输出打印结果）')).toBeVisible()
    await expect(page.getByPlaceholder('输入 flag')).toHaveCount(0)
  })

  test('提交 yak 源码：逐组对拍全部通过 → AC 计分', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await page.getByPlaceholder('粘贴 yak 源码（从标准输入读取数据，向标准输出打印结果）')
      .fill(E2E.acm.correctSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('判定通过，得分', { timeout: 30_000 })
    await expect(page.locator('.result')).toHaveClass(/el-alert--success/)
  })

  test('提交错误输出的源码 → WA 判定不通过', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await page.getByPlaceholder('粘贴 yak 源码（从标准输入读取数据，向标准输出打印结果）')
      .fill(E2E.acm.wrongSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('答案错误（WA）', { timeout: 30_000 })
    await expect(page.locator('.result')).toHaveClass(/el-alert--error/)
  })

  test('引擎未装配 → internal_error（平台侧失败提示，区别于代码错误）', async ({ page }) => {
    test.skip(engineConfigured, '判题引擎已装配，internal_error 路径不适用')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await page.getByPlaceholder('粘贴 yak 源码（从标准输入读取数据，向标准输出打印结果）')
      .fill(E2E.acm.correctSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('判定服务暂不可用', { timeout: 30_000 })
  })
})
