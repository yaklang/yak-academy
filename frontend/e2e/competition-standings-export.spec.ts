import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, studentToken } from './helpers'

// 管理端榜单 CSV 导出：公开赛报名产生零分参赛者后，个人榜导出必须包含
// 账号编码列、UTF-8 BOM 与参赛者行；按钮下载真实文件。

async function seedPublicCompetition(
  request: Parameters<typeof adminToken>[0],
  name: string,
): Promise<number> {
  const token = await adminToken(request)
  const now = Date.now()
  const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name,
      description: '榜单 CSV 导出',
      kind: 'public',
      start_time: new Date(now - 60_000).toISOString(),
      end_time: new Date(now + 30 * 60_000).toISOString(),
    },
  })
  expect(created.ok()).toBeTruthy()
  const comp = (await created.json()) as { id: number }
  const student = await studentToken(request)
  const registered = await request.post(
    `${E2E.apiURL}/api/v1/competitions/${comp.id}/register`,
    { headers: { Authorization: `Bearer ${student}` } },
  )
  expect(registered.ok()).toBeTruthy()
  return comp.id
}

test.describe('竞赛榜单导出', () => {
  const openIds: number[] = []
  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  test('个人榜导出 CSV 含账号编码与零分参赛者', async ({ page, request }) => {
    const compId = await seedPublicCompetition(request, `e2e 榜单导出-${Date.now()}`)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await page.getByRole('tab', { name: '排行榜' }).click()
    await page.getByRole('radio', { name: '个人榜' }).check()

    const downloadPromise = page.waitForEvent('download')
    await page.getByRole('button', { name: '导出 CSV' }).click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe('竞赛榜单-个人.csv')

    const path = await download.path()
    expect(path).toBeTruthy()
    const raw = await readFile(path!, 'utf8')
    expect(raw.startsWith('\uFEFF排名,用户名,账号编码,队伍,得分,解题数,最近通关')).toBeTruthy()
    expect(raw).toContain(E2E.student.username)
  })
})
