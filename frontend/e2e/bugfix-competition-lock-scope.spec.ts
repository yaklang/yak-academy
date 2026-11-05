import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import {
  adminToken,
  endCompetition,
  loginAsAdmin,
  loginAsStudent,
  seedPublicCompetition,
  seedRunningCompetition,
  seedRunningCompetitionFor,
  studentToken,
} from './helpers'

// 回归：公开赛参赛者被错误锁定在竞赛页面（PR #45，2026-11-05 修复）。
// 修复前行为：入场锁不区分竞赛档位，`locked = (已入场 ∧ running)` 对公开赛
//   同样成立——学生在广场报名并进入公开赛后，访问题库/教程等平台页面全部
//   403，前端路由守卫反复把学员拉回竞赛首页；管理员进入邀请赛同样被告知
//   locked=true（服务端中间件虽豁免管理员，但 /competitions/me 的 UI 状态错误）。
// 修复后行为：锁定只作用于邀请赛——公开赛报名入场后可自由进出、随时返回平台；
//   邀请赛入场后仍锁定到比赛结束；管理员进入任何竞赛都不锁定，可返回管理端。
// 复现路径：竞赛广场 → 报名公开赛 → 进入竞赛 → 点「返回平台」→ 题库；
//   以及邀请赛邮件入口 → 进入竞赛 → 访问题库；管理员进入邀请赛 → 返回管理端。

const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

test.describe('回归：入场锁只作用于邀请赛', () => {
  test('公开赛入场后可返回平台并正常访问题库（回归：公开赛被误锁）', async ({ page, request }) => {
    const publicId = await seedPublicCompetition(request, E2E.competition.publicNoLockName)
    openIds.push(publicId)

    await loginAsStudent(page)
    await page.getByRole('link', { name: '竞赛', exact: true }).click()
    await page.waitForURL((u) => u.pathname === '/competitions')

    const row = page.locator('tr', { hasText: E2E.competition.publicNoLockName })
    await row.getByRole('button', { name: '报名' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '报名成功' })).toBeVisible()
    await row.getByRole('link', { name: '进入竞赛' }).click()
    await page.waitForURL((u) => u.pathname === `/competitions/enter/${publicId}`)
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '入场成功' })).toBeVisible()
    await page.waitForURL((u) => u.pathname === `/competitions/${publicId}`)
    await expect(page.getByRole('heading', { name: E2E.competition.publicNoLockName })).toBeVisible()

    // 修复点：公开赛不锁定，竞赛顶栏提供「返回平台」；点击后回到平台且不再被弹回。
    await page.getByRole('button', { name: '返回平台' }).click()
    await page.waitForURL((u) => u.pathname === '/learn')
    await expect(page.getByRole('heading', { name: /学习\s*路线/ })).toBeVisible()

    // 题库可正常访问（修复前这里会被路由守卫拉回竞赛首页，接口 403）。
    await page.getByRole('link', { name: '题库', exact: true }).click()
    await page.waitForURL((u) => u.pathname === '/training')
    await expect(page.getByRole('heading', { name: /题\s*库/ })).toBeVisible()

    // 服务端同语义：已入场的公开赛不产生锁定，平台接口 200。
    const token = await studentToken(request)
    const headers = { Authorization: `Bearer ${token}` }
    const entry = await request.get(`${E2E.apiURL}/api/v1/competitions/me`, { headers })
    const me = (await entry.json()) as { entered: boolean; locked: boolean; competition: { id: number } | null }
    expect(me.entered).toBe(true)
    expect(me.locked).toBe(false)
    expect(me.competition?.id).toBe(publicId)
    const topics = await request.get(`${E2E.apiURL}/api/v1/topics`, { headers })
    expect(topics.status()).toBe(200)
  })

  test('邀请赛入场后仍锁定平台（回归对照：不能误放开邀请赛）', async ({ page, request }) => {
    const comp = await seedRunningCompetition(request, E2E.competition.inviteLockName, [E2E.challenge.slug])
    openIds.push(comp.id)

    await loginAsStudent(page)
    await page.goto(`/competitions/enter/${comp.id}?code=${encodeURIComponent(comp.code)}`)
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '入场成功' })).toBeVisible()
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)
    await expect(page.getByRole('heading', { name: E2E.competition.inviteLockName })).toBeVisible()

    // 邀请赛仍锁：锁定期间没有「返回平台」入口，访问平台会被引导回竞赛首页。
    await expect(page.getByRole('button', { name: '返回平台' })).toHaveCount(0)
    await page.goto('/training')
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)

    // 服务端兜底：锁定态与 403 仍成立。
    const token = await studentToken(request)
    const headers = { Authorization: `Bearer ${token}` }
    const entry = await request.get(`${E2E.apiURL}/api/v1/competitions/me`, { headers })
    expect(((await entry.json()) as { locked: boolean }).locked).toBe(true)
    const topics = await request.get(`${E2E.apiURL}/api/v1/topics`, { headers })
    expect(topics.status()).toBe(403)
  })

  test('管理员进入邀请赛不被锁定，可返回管理端（回归：管理员被误锁）', async ({ page, request }) => {
    const comp = await seedRunningCompetitionFor(
      request,
      E2E.competition.adminNoLockName,
      [E2E.challenge.slug],
      [E2E.admin.username],
    )
    openIds.push(comp.id)

    await loginAsAdmin(page)
    await page.goto(`/competitions/enter/${comp.id}?code=${encodeURIComponent(comp.codes[E2E.admin.username])}`)
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '入场成功' })).toBeVisible()
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)
    await expect(page.getByRole('heading', { name: E2E.competition.adminNoLockName })).toBeVisible()

    // 修复点：管理员进入邀请赛也提供返回入口，可回到管理端。
    await page.getByRole('button', { name: '返回管理端' }).click()
    await page.waitForURL((u) => u.pathname === '/admin/tutorials')
    await expect(page.getByRole('link', { name: '竞赛管理', exact: true })).toBeVisible()

    // 服务端：管理员在邀请赛入场态下 locked=false，管理接口与平台接口均可用。
    const token = await adminToken(request)
    const headers = { Authorization: `Bearer ${token}` }
    const entry = await request.get(`${E2E.apiURL}/api/v1/competitions/me`, { headers })
    expect(((await entry.json()) as { locked: boolean }).locked).toBe(false)
    const users = await request.get(`${E2E.apiURL}/api/v1/admin/users`, { headers })
    expect(users.status()).toBe(200)
    const topics = await request.get(`${E2E.apiURL}/api/v1/topics`, { headers })
    expect(topics.status()).toBe(200)
  })
})
