import { expect, test } from '@playwright/test'
import { loginAsAdmin } from '../helpers'

// 回归：进入容器管理会卡顿约 2 秒（2026-11-06 修复）。
// 修复前行为：容器页每次进入都同步执行 docker info + docker stats；docker stats
// 的采样窗口约 1-2s，离开再进入仍要重新等待，主机性能面板迟迟不出数据。
// 修复后行为：服务端启动预热并在缓存过期时先返回旧快照、后台刷新；前端渲染
// 缓存数据后短轮询追平。第二次进入应直接命中缓存，不再等待 docker 采集。
// 复现路径：用户与容器页 → 容器管理 tab → 等性能面板出现 → 用户管理 tab → 再回容器管理 tab。
// 依赖本机 Docker daemon（docker project，E2E_DOCKER=1）；本用例不创建容器，
// 只验证 Docker 采集链路的缓存行为，结束后无需清理运行时资源。
test.describe('回归：容器管理性能快照缓存', () => {
  test('再次进入容器管理直接展示缓存数据（回归：每次进入等待 docker stats）', async ({ page }) => {
    test.setTimeout(90_000)
    await loginAsAdmin(page)

    // 首次进入：允许冷启动等待 docker 采集/预热完成。
    await page.goto('/admin/panel/containers')
    await expect(page.getByRole('tab', { name: '容器管理' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText(/CPU（\d+ 核）/)).toBeVisible({ timeout: 30_000 })

    // 离开后再进入：服务端已有快照，页面不应再被 docker stats 阻塞。
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户与容器' })).toBeVisible()
    // 等用户列表加载完再切 tab：加载骨架会让页面高度/滚动条变化，Playwright
    // 的稳定性等待属于页面布局而不是容器页数据延迟。
    await expect(page.locator('tbody tr').first()).toBeVisible()
    const started = Date.now()
    await page.getByRole('tab', { name: '容器管理' }).click()
    await expect(page.getByText(/CPU（\d+ 核）/)).toBeVisible({ timeout: 5_000 })
    const elapsed = Date.now() - started

    // 缓存命中路径只做 HTTP + 渲染；旧实现会同步等待约 1-2s 的 docker stats。
    expect(elapsed, `容器管理二次进入耗时 ${elapsed}ms，疑似未命中性能快照缓存`).toBeLessThan(800)
  })
})
