import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, seedRunningCompetitionFor } from './helpers'

// 晋级旅程：来源 CTF 榜单预览 → 目标邀请赛应用，参赛者被导入并生成邀请码。
// 用 min_score=0 纳入零分参赛者，避免依赖异步判题时序。

test.describe('晋级名单预览与应用', () => {
  const openIds: number[] = []
  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  test('从来源 CTF 榜单预览并导入目标邀请赛', async ({ page, request }) => {
    test.slow()
    const suffix = Date.now()
    const sourceName = `e2e 晋级来源-${suffix}`
    const targetName = `e2e 晋级目标-${suffix}`
    const source = await seedRunningCompetitionFor(
      request,
      sourceName,
      [E2E.challenge.slug],
      [E2E.student.username],
    )
    openIds.push(source.id)

    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name: targetName,
        description: '晋级目标邀请赛',
        start_time: new Date(now + 60 * 60_000).toISOString(),
        end_time: new Date(now + 3 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const target = (await created.json()) as { id: number }
    openIds.push(target.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${target.id}`)
    await page.getByRole('tab', { name: '晋级' }).click()

    const pane = page.getByRole('tabpanel', { name: '晋级' })
    await pane.getByRole('combobox', { name: '来源竞赛' }).click()
    await page.getByRole('option').filter({ hasText: sourceName }).click()
    const threshold = pane.getByRole('spinbutton', { name: '最低分' })
    await threshold.fill('0')
    await threshold.press('Tab')
    await pane.getByRole('button', { name: '预览晋级名单' }).click()

    await expect(pane.locator('tr', { hasText: E2E.student.username })).toBeVisible()
    await pane.getByRole('button', { name: '应用晋级（导入参赛者）' }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入 1 名参赛者' })).toBeVisible()
    await expect(pane.locator('tr', { hasText: sourceName })).toContainText('1')
  })
})
