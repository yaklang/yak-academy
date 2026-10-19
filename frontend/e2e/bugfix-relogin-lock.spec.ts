import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsStudent, seedRunningCompetition } from './helpers'

// 回归：锁定参赛者登出重登后被留在 403 空页，锁缓存未随登录态重查（PR #22，2026-10-19 修复）。
// 修复前行为：auth.login()/logout() 用 unlock() 重置竞赛锁缓存，unlock 会把 checked 标记为
//   true；重登后路由守卫 ensureChecked 读到缓存的「未锁定」直接放行 → 落在 '/'，数据请求
//   全部 403，页面成为空壳，且无任何入口回到竞赛页（只有整页刷新才恢复）。
// 修复后行为：登录态切换改为 reset()（清缓存且 checked=false），守卫强制重查服务端，
//   锁定参赛者重登后仍被引导回竞赛首页。
// 复现路径：参赛学生入场（锁定）→ 点「登出」→ 登录页重新登录 → 观察落在哪一页。
// （旧代码上该用例可失败：重登后停在 / 而非竞赛首页——先红后绿。）
const { challenge: C } = E2E

// 兜底收尾：用例失败也把比赛结束掉，避免学生带着服务端锁定进入后续 spec。
let openId: number | null = null
test.afterEach(async ({ request }) => {
  if (openId) await endCompetition(request, openId)
  openId = null
})

test.describe('回归：重登后仍锁在竞赛页', () => {
  test('锁定参赛者登出重登被引导回竞赛首页，而非留在 403 空页（回归：重登后锁缓存失效）', async ({
    page,
    request,
  }) => {
    const comp = await seedRunningCompetition(request, E2E.competition.reloginName, [C.slug])
    openId = comp.id

    // 入场（邮件直达链接预填邀请码），落在竞赛首页
    await loginAsStudent(page)
    await page.goto(`/competitions/enter/${comp.id}?code=${encodeURIComponent(comp.code)}`)
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)

    // 登出 → 登录页重新登录：守卫重查服务端，仍被引导回竞赛首页
    await page.getByRole('button', { name: '登出' }).click()
    await page.waitForURL((u) => u.pathname === '/login')
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill(E2E.student.password)
    await page.getByRole('button', { name: '登录' }).click()

    // 业务终态：不是"没崩"，而是回到进行中的竞赛页且题目可见
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`, { timeout: 10_000 })
    await expect(page.getByRole('heading', { name: E2E.competition.reloginName })).toBeVisible()
    await expect(page.getByText('进行中', { exact: true })).toBeVisible()
    await expect(page.locator('tr', { hasText: C.title })).toBeVisible()
  })
})