import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsStudent, studentToken } from './helpers'

// AWD 参赛端旅程：公开赛报名入场后进入 AWD 首页，验证格式分发（不落在 CTF
// 视图）、比赛状态/空态、排行榜与目标目录；flag 提交在未开赛时禁用。
test.describe('AWD 参赛端', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedAwdCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name: `E2E AWD 参赛端-${now}`,
        description: 'AWD 参赛端页面验证',
        kind: 'public',
        format: 'awd',
        start_time: new Date(now - 60_000).toISOString(),
        end_time: new Date(now + 30 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }
    // 公开赛自助报名是个人参赛：AWD 竞域单位切到 user（默认 team 需先编队）。
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/awd`, {
          headers: { Authorization: `Bearer ${token}` },
          data: { competitor_type: 'user' },
        })
      ).ok(),
    ).toBeTruthy()
    const student = await studentToken(request)
    const auth = { Authorization: `Bearer ${student}` }
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/register`, { headers: auth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/enter`, { headers: auth })).ok(),
    ).toBeTruthy()
    return comp.id
  }

  test('AWD 首页按格式分发并展示空态与排行榜', async ({ page, request }) => {
    const compId = await seedAwdCompetition(request)
    openIds.push(compId)

    await loginAsStudent(page)
    await page.goto(`/competitions/${compId}`)

    // 格式分发：AWD 头部与 CTF 视图（题目/WP）互斥。
    await expect(page.getByRole('heading', { level: 1 })).toContainText('E2E AWD 参赛端')
    await expect(page.getByText('YAK ACADEMY · AWD')).toBeVisible()
    await expect(page.getByText('AWD 攻防赛')).toBeVisible()
    await expect(page.getByText('尚未开启回合')).toBeVisible()
    await expect(page.getByRole('tab', { name: '题目' })).toHaveCount(0)

    // 未开赛：flag 提交禁用；攻击盒与目标目录为空态。
    await expect(page.getByPlaceholder('flag{...}')).toBeDisabled()
    await expect(page.getByText('攻击盒尚未部署；裁判部署后刷新即可获得 SSH 凭据。')).toBeVisible()
    await page.getByRole('tab', { name: '目标目录' }).click()
    await expect(page.getByText('暂无其他竞域单位的服务')).toBeVisible()

    // 排行榜包含已入场参赛者（0 分）与本人标记。
    await page.getByRole('tab', { name: '排行榜' }).click()
    await expect(page.getByRole('cell', { name: E2E.student.username })).toBeVisible()
    await expect(page.getByText('我', { exact: true })).toBeVisible()
    await expect(page.getByRole('cell', { name: '0' }).first()).toBeVisible()
  })
})
