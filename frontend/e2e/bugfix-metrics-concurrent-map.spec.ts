import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：后端指标采集器无锁并发写 map，多请求并发页面加载触发
// `fatal error: concurrent map writes`，进程直接崩溃、后续请求全部
// ERR_CONNECTION_REFUSED（2026-11-02 修复：collector 加 sync.Mutex）。
// 修复前行为：并发页面加载若干次后后端崩溃，登录/列表请求开始失败。
// 修复后行为：持续并发加载下后端存活，业务终态（教程列表可达）正常。
// 复现路径：学生登录 → 首页/教程/题库等页面同时发起 topics、score、me
// 等多个 API 请求，每个请求完成都会并发写 metrics map。
test.describe('回归：指标并发写崩溃', () => {
  test('并发页面加载下后端保持存活且页面可用（回归：concurrent map writes）', async ({ page }) => {
    test.setTimeout(60_000)
    await loginAsStudent(page)
    // 覆盖真实用户会并发触发的页面：每页都并发打多个 API，
    // 循环多轮放大并发写指标的窗口，旧实现下足以撞出 fatal。
    for (let round = 0; round < 3; round++) {
      for (const path of ['/', '/learn', '/training', '/learn', '/']) {
        await page.goto(path)
      }
    }
    // 断言业务终态：后端未崩，教程页正常渲染（不断言"不崩溃"本身，
    // 断言用户看到的页面内容仍然可用）。
    await page.goto('/learn')
    await expect(page.locator('main').first()).toBeVisible()
    // 后端健康检查仍应 200：进程级存活的直接证据。
    const health = await page.request.get(`${E2E.apiURL}/health`)
    expect(health.status()).toBe(200)
  })
})