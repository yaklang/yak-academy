import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, loginAsAdmin } from './helpers'

// 管理端分页约束：用户管理与容器管理的表格默认 20 条/页，有数据即显示分页器，
// 支持 20/50/100 切换；容器页的活跃容器与性能容器快照在前端分页。
test.describe('管理端表格分页', () => {
  test('用户管理默认每页 20 条，可翻页并切换每页条数', async ({ page, request }) => {
    const token = await adminToken(request)
    const prefix = `e2e-page-${Date.now()}-`
    for (let i = 1; i <= 21; i++) {
      const created = await request.post(`${E2E.apiURL}/api/v1/admin/users`, {
        headers: { Authorization: `Bearer ${token}` },
        data: {
          username: `${prefix}${String(i).padStart(2, '0')}`,
          password: 'e2e-page-pass-1',
          global_role: 'student',
        },
      })
      expect(created.ok()).toBeTruthy()
    }

    await loginAsAdmin(page)
    await page.goto('/admin/panel/users')
    await expect(page.getByRole('heading', { name: '用户与容器' })).toBeVisible()

    const search = page.getByPlaceholder('搜索：用户名 / 账号编码 / 邮箱 / 手机号')
    await search.fill(prefix)
    await page.getByRole('button', { name: '搜索' }).click()

    const rows = page.locator('tbody tr')
    await expect(page.getByText('共 21 条')).toBeVisible()
    await expect(rows).toHaveCount(20)
    await expect(page.getByLabel('每页条数')).toHaveValue('20')

    await page.getByRole('button', { name: '下一页' }).click()
    await expect(rows).toHaveCount(1)

    await page.getByLabel('每页条数').selectOption('50')
    await expect(rows).toHaveCount(21)

    // 单页也应保留分页器（避免数据少时“分页消失”的行为跳变）。
    await search.fill(`${prefix}01`)
    await page.getByRole('button', { name: '搜索' }).click()
    await expect(page.getByText('共 1 条')).toBeVisible()
    await expect(page.getByLabel('每页条数')).toHaveValue('50')
    await expect(rows).toHaveCount(1)
  })

  test('容器管理的性能容器与活跃容器表默认每页 20 条', async ({ page }) => {
    const perfContainers = Array.from({ length: 21 }, (_, index) => ({
      id: `container-${index + 1}`,
      name: `container-${index + 1}`,
      cpu_percent: 1.5,
      mem_used_bytes: 1024,
      mem_limit_bytes: 2048,
      mem_percent: 50,
      net_io_bytes: [0, 0],
      block_io_bytes: [0, 0],
      pids: 10,
    }))
    await page.route('**/api/v1/admin/performance', async (route) => {
      await route.fulfill({
        json: {
          ncpu: 4,
          mem_total_bytes: 8 * 1024 * 1024 * 1024,
          containers_total: 21,
          containers_running: 21,
          containers_paused: 0,
          containers_stopped: 0,
          images: 3,
          host: null,
          data_volume: null,
          containers: perfContainers,
        },
      })
    })
    const activeContainers = Array.from({ length: 21 }, (_, index) => ({
      id: index + 1,
      user_id: index + 1,
      username: `page-user-${index + 1}`,
      challenge_id: 1,
      challenge_title: `分页题 ${index + 1}`,
      challenge_slug: `page-challenge-${index + 1}`,
      status: 'running',
      endpoint: '',
      expire_at: new Date(Date.now() + 3600_000).toISOString(),
      created_at: new Date().toISOString(),
    }))
    await page.route('**/api/v1/admin/lab-instances/active', async (route) => {
      await route.fulfill({ json: { items: activeContainers } })
    })

    await loginAsAdmin(page)
    await page.goto('/admin/panel/containers')
    await expect(page.getByRole('tab', { name: '容器管理' })).toHaveAttribute('aria-selected', 'true')

    const perfCard = page.locator('.ui-card').filter({ hasText: '主机性能' })
    await expect(perfCard.getByText('共 21 条')).toBeVisible()
    await expect(perfCard.locator('tbody tr')).toHaveCount(20)
    await expect(perfCard.getByLabel('每页条数')).toHaveValue('20')
    await perfCard.getByRole('button', { name: '下一页' }).click()
    await expect(perfCard.locator('tbody tr')).toHaveCount(1)

    const activeCard = page.locator('.ui-card').filter({ hasText: '当前打开的容器' })
    await expect(activeCard.getByText('共 21 条')).toBeVisible()
    await expect(activeCard.locator('tbody tr')).toHaveCount(20)
    await expect(activeCard.getByLabel('每页条数')).toHaveValue('20')
    await activeCard.getByRole('button', { name: '下一页' }).click()
    await expect(activeCard.locator('tbody tr')).toHaveCount(1)
  })
})
