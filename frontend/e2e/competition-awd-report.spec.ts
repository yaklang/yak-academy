import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, loginAsStudent, logout, studentToken } from './helpers'

// AWD 战报旅程：赛后参赛者保存 Markdown，管理员按 rubric 评审，参赛者看到
// 最终分与评审历史。战报只在游戏 ended 后可写。
test.describe('AWD 战报', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedEndedAwdCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: `E2E AWD 战报-${now}`,
        description: 'AWD 战报评审验证',
        kind: 'public',
        format: 'awd',
        start_time: new Date(now - 60_000).toISOString(),
        end_time: new Date(now + 30 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/awd`, {
          headers: auth,
          data: { competitor_type: 'user' },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/awd/services`, {
          headers: auth,
          data: {
            slug: 'web',
            name: 'Web',
            image: 'nginx:alpine',
            ports: [{ name: 'http', port: 80, protocol: 'tcp' }],
          },
        })
      ).ok(),
    ).toBeTruthy()
    const student = await studentToken(request)
    const studentAuth = { Authorization: `Bearer ${student}` }
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/register`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/enter`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/awd/start`, { headers: auth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/awd/end`, { headers: auth })).ok(),
    ).toBeTruthy()
    return comp.id
  }

  test('赛后提交战报并由管理员评审', async ({ page, request }) => {
    const compId = await seedEndedAwdCompetition(request)
    openIds.push(compId)

    await loginAsStudent(page)
    await page.goto(`/competitions/${compId}`)
    await expect(page.getByText('已结束').first()).toBeVisible()

    await page.getByRole('tab', { name: '战报' }).click()
    const content = page.locator('textarea').first()
    await content.fill('# 战报 E2E\n\n漏洞复盘与修复过程')
    await page.getByRole('button', { name: '保存战报' }).click()
    await expect(page.getByText('战报已保存')).toBeVisible()
    await expect(page.locator('.awd-report-status')).toContainText('待评审')

    await logout(page)
    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await page.getByRole('tab', { name: '战报评审' }).click()
    await expect(page.getByRole('cell', { name: E2E.student.username })).toBeVisible()

    await page.getByRole('button', { name: '评审' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('spinbutton').fill('90')
    await dialog.getByLabel('评审意见').fill('漏洞覆盖完整，修复说明清晰')
    await dialog.getByRole('button', { name: '提交评审' }).click()
    await expect(page.getByText('战报评审已提交')).toBeVisible()
    await expect(page.getByRole('cell', { name: '90' })).toBeVisible()

    await logout(page)
    await loginAsStudent(page)
    await page.goto(`/competitions/${compId}`)
    await page.getByRole('tab', { name: '战报' }).click()
    await expect(page.locator('.awd-report-status')).toContainText('已评审')
    await expect(page.locator('.awd-report-status')).toContainText('90')
    await expect(page.getByText('漏洞覆盖完整，修复说明清晰')).toBeVisible()
    await expect(content).toHaveValue(/# 战报 E2E/)
  })
})
