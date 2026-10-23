import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsStudent, seedRunningCompetition } from './helpers'

// 公开赛旅程（竞赛分公开/邀请两档）：公开赛不发邀请函——
// 展示在竞赛广场供查看与提前报名（邀请赛不出现在广场），已报名者免邀请码入场。
// 管理端建赛走 API 播种，学生侧报名/入场全部走真实 UI。

// 建一个进行中的公开赛（无题目绑定：本旅程只覆盖广场报名与免码入场）。
async function seedPublicCompetition(request: APIRequestContext, name: string): Promise<number> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  const { token } = (await login.json()) as { token: string }
  const now = Date.now()
  const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name,
      description: '公开赛广场旅程',
      start_time: new Date(now - 60_000).toISOString(),
      end_time: new Date(now + 30 * 60_000).toISOString(),
      kind: 'public',
    },
  })
  expect(created.ok()).toBeTruthy()
  const comp = (await created.json()) as { id: number; kind: string }
  expect(comp.kind).toBe('public')
  return comp.id
}

// 本 spec 运行中创建的竞赛：无论用例成败，afterEach 手动结束兜底
// （学生在本旅程中入场，不结束会把账号锁进后续 spec）。
const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

test.describe('公开赛广场与免码入场', () => {
  test('广场列出公开赛并可报名；邀请赛不外显；已报名者免码入场', async ({ page, request }) => {
    const publicId = await seedPublicCompetition(request, E2E.competition.publicName)
    openIds.push(publicId)
    // 邀请赛对照组：同样进行中，但不应出现在广场（绑定种子题满足建赛必填）。
    const hidden = await seedRunningCompetition(request, E2E.competition.hiddenName, [E2E.challenge.slug])
    openIds.push(hidden.id)

    await loginAsStudent(page)
    await page.getByRole('link', { name: '竞赛', exact: true }).click()
    await page.waitForURL((u) => u.pathname === '/competitions')

    // 公开赛在广场；同时间的邀请赛不出现在广场（定向邀请，不对外展示）。
    const publicRow = page.locator('tr', { hasText: E2E.competition.publicName })
    await expect(publicRow).toBeVisible()
    await expect(publicRow).toContainText('进行中')
    await expect(page.locator('tr', { hasText: E2E.competition.hiddenName })).toHaveCount(0)

    // 自助报名：原位置换为「已报名 + 进入竞赛」（比赛进行中，报名即可入场）。
    await publicRow.getByRole('button', { name: '报名' }).click()
    await expect(page.locator('.el-message', { hasText: '报名成功' })).toBeVisible()
    await expect(publicRow.locator('.el-tag', { hasText: '已报名' })).toBeVisible()

    // 免码入场：入场页邀请码留空直接进入，落在竞赛首页。
    await publicRow.getByRole('link', { name: '进入竞赛' }).click()
    await page.waitForURL((u) => u.pathname === `/competitions/enter/${publicId}`)
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await expect(page.locator('.el-message', { hasText: '入场成功' })).toBeVisible()
    await page.waitForURL((u) => u.pathname === `/competitions/${publicId}`)
    await expect(page.getByRole('heading', { name: E2E.competition.publicName })).toBeVisible()
  })
})
