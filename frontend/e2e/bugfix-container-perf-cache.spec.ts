import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：进入容器管理会等待 docker stats 约 2 秒才出现性能数据（2026-11-06 修复）。
// 修复前行为：/admin/performance 每次同步执行 docker info + docker stats，页面
// 必须等这次采集结束才能展示主机/容器数据；离开后再进入仍会重新等待。
// 修复后行为：服务端优先返回缓存快照并后台刷新；前端先立即展示缓存，再在
// 1.5s 后自动追平，不需要用户手动点刷新。
// 复现路径：用户与容器页 → 容器管理 tab，观察主机性能面板。
// 本用例用路由模拟服务端缓存响应，锁定前端“先展示旧快照 + 自动追平”的行为；
// 真实 Docker 采集耗时回归见 e2e/docker/bugfix-container-perf-cache.spec.ts。
test.describe('回归：容器管理性能面板不再阻塞进入', () => {
  test('缓存快照立即展示，后台刷新完成后自动追平（回归：进入等待 docker stats）', async ({ page }) => {
    let calls = 0
    const cached = {
      ncpu: 8,
      mem_total_bytes: 8 * 1024 ** 3,
      containers_total: 1,
      containers_running: 1,
      containers_paused: 0,
      containers_stopped: 0,
      images: 1,
      host: null,
      data_volume: null,
      sampled_at: '2026-11-06T08:00:00Z',
      refreshing: true,
      containers: [
        {
          id: 'cached',
          name: 'cached-container',
          cpu_percent: 1,
          mem_used_bytes: 1024,
          mem_limit_bytes: 2048,
          mem_percent: 50,
          net_io_bytes: [0, 0],
          block_io_bytes: [0, 0],
          pids: 1,
        },
      ],
    }
    await page.route('**/api/v1/admin/performance', async (route) => {
      calls++
      if (calls === 1) {
        await route.fulfill({ json: cached })
        return
      }
      await route.fulfill({
        json: {
          ...cached,
          ncpu: 16,
          sampled_at: '2026-11-06T08:00:02Z',
          refreshing: false,
          containers: [{ ...cached.containers[0], id: 'fresh', name: 'fresh-container' }],
        },
      })
    })

    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户与容器' })).toBeVisible()
    await page.getByRole('tab', { name: '容器管理' }).click()

    // 第一帧展示缓存快照（旧 CPU 核数与容器），并明确标记后台刷新中。
    await expect(page.getByRole('tab', { name: '容器管理' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('CPU（8 核）')).toBeVisible()
    await expect(page.getByText('cached-container')).toBeVisible()
    await expect(page.getByText(/采样于/)).toBeVisible()
    await expect(page.getByText(/刷新中/)).toBeVisible()

    // 无需用户操作：前端短轮询拿到后台刷新后的快照并替换旧值。
    await expect(page.getByText('CPU（16 核）')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('fresh-container')).toBeVisible()
    await expect(page.getByText(/刷新中/)).toHaveCount(0)
    expect(calls).toBeGreaterThanOrEqual(2)
  })
})
