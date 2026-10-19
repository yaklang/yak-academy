import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsStudent, seedRunningCompetition, studentToken } from './helpers'

// 竞赛隔离旅程（补竞赛域 e2e 空白，来源：权限隔离审计）：
// 管理端建赛/导入走 API 播种，参赛路径全部走真实 UI——
//   站内邀请邮件 → 入场页（邀请码预填）→ 入场 →
//   ① 入场前：学生知道管理端 URL 也进不去（前端守卫送回 + 服务端 403）；
//   ② 入场后：平台其余路径被引导回竞赛首页，服务端对非竞赛域一律 403，
//      登出重进仍锁（锁定态在服务端），竞赛域内提交 flag 正常判定；
//   ③ 比赛结束（管理端把 end_time 改到过去）：竞赛首页自检感知、放出 WP
//      提交，学生恢复平台访问（前端守卫与服务端同时解除）。
const { challenge: C } = E2E

// 学生凭邮件直达链接（?code= 预填邀请码）入场，落在竞赛首页。
async function enterViaMailLink(page: import('@playwright/test').Page, id: number, code: string) {
  await page.goto(`/competitions/enter/${id}?code=${encodeURIComponent(code)}`)
  await page.getByRole('button', { name: '进入竞赛' }).click()
  await page.waitForURL((u) => u.pathname === `/competitions/${id}`)
  await expect(page.getByText('进行中', { exact: true })).toBeVisible()
}

// 本 spec 运行中创建的竞赛：无论用例成败，afterEach 都把 end_time 改到
// 过去兜底收尾（锁定由「已入场 ∧ running」派生，比赛结束即解除）——
// 否则中途失败/重试会把学生锁死，殃及后续 spec 与重跑。
const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

test.describe('竞赛隔离旅程', () => {
  test('入场前管理端不可达；入场后锁死竞赛域，登出重进仍锁', async ({ page, request }) => {
    test.slow() // 双次完整登录 + 锁定往返 + 异步判题，超出默认 30s
    const comp = await seedRunningCompetition(request, E2E.competition.lockName, [C.slug])
    openIds.push(comp.id)

    // —— ① 入场前：学生知道管理端 URL 也进不去 ——
    // 服务端：管理接口对学生 token 一律 403（权限判定在中间件，路由不泄露更多信息）
    const stuToken = await studentToken(request)
    const forbidden = await request.get(`${E2E.apiURL}/api/v1/admin/users`, {
      headers: { Authorization: `Bearer ${stuToken}` },
    })
    expect(forbidden.status()).toBe(403)
    // 前端：直接输入管理端 URL 被守卫送回学生首页
    await loginAsStudent(page)
    await page.goto('/admin/panel/competitions')
    await page.waitForURL((u) => u.pathname === '/')

    // —— 邀请邮件 → 入场页（邀请码预填）→ 入场 ——
    await page.getByRole('link', { name: /邮箱/ }).click()
    await page.waitForURL((u) => u.pathname === '/mailbox')
    // 邮件标题含竞赛名（此前 spec 的邀请邮件也在邮箱里，须按本赛名定位）
    const inviteRow = page.locator('tr', { hasText: E2E.competition.lockName })
    await expect(inviteRow).toContainText('竞赛邀请')
    await inviteRow.getByRole('button', { name: '查看' }).click()
    const mailDialog = page.locator('.el-dialog')
    await expect(mailDialog).toContainText('邀请码：')
    await mailDialog.getByRole('button', { name: '进入竞赛' }).click()
    await page.waitForURL((u) => u.pathname.startsWith('/competitions/enter/'))
    await page.getByRole('button', { name: '进入竞赛' }).click()
    await expect(page.locator('.el-message', { hasText: '入场成功' })).toBeVisible()
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)
    await expect(page.getByRole('heading', { name: E2E.competition.lockName })).toBeVisible()
    // 绑定的种子题出现在题目表
    await expect(page.locator('tr', { hasText: C.title })).toBeVisible()

    // —— ② 入场后：平台其余路径全部被引导回竞赛首页 ——
    for (const path of ['/training', `/training/${C.slug}`, '/admin/panel/competitions']) {
      await page.goto(path)
      await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)
    }
    // 服务端隔离（锁的真正所在）：非竞赛域 403，竞赛域 200
    const locked = await request.get(`${E2E.apiURL}/api/v1/topics`, {
      headers: { Authorization: `Bearer ${stuToken}` },
    })
    expect(locked.status()).toBe(403)
    const board = await request.get(`${E2E.apiURL}/api/v1/competitions/${comp.id}/leaderboard`, {
      headers: { Authorization: `Bearer ${stuToken}` },
    })
    expect(board.status()).toBe(200)

    // 登出重进仍锁：锁定态在服务端，换会话不解除
    // （回归：曾因登录重置竞赛锁缓存用 unlock 而失效，见 bugfix-relogin-lock.spec.ts）
    await page.getByRole('button', { name: '登出' }).click()
    await page.waitForURL((u) => u.pathname === '/login')
    await page.getByPlaceholder('用户名').fill(E2E.student.username)
    await page.getByPlaceholder('密码').fill(E2E.student.password)
    await page.getByRole('button', { name: '登录' }).click()
    await page.waitForURL((u) => u.pathname === `/competitions/${comp.id}`)

    // —— 竞赛域内可用：题目抽屉提交 flag，异步判对转「已通关」 ——
    // 题目标题是 el-link（a 无 href，无 link role），按类名定位点击
    await page.locator('tr', { hasText: C.title }).locator('.el-link').click()
    await expect(page.locator('.el-drawer')).toBeVisible()
    await page.getByPlaceholder('flag{...}').fill(C.flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.el-message', { hasText: '已提交，异步判定中' })).toBeVisible()
    await page.reload()
    await expect(page.locator('tr', { hasText: C.title })).toContainText('已通关', {
      timeout: 15_000,
    })
  })

  test('比赛结束自动解锁：竞赛首页感知结束，学生恢复平台访问', async ({ page, request }) => {
    const comp = await seedRunningCompetition(request, E2E.competition.endName, [C.slug])
    openIds.push(comp.id)

    await loginAsStudent(page)
    await enterViaMailLink(page, comp.id, comp.code)

    // 管理端把 end_time 改到过去（running 期间允许缩短）：比赛立即结束
    await endCompetition(request, comp.id)

    // 竞赛首页 5s 节拍自检：感知结束 → 解锁并放出 WP 提交（结束态业务标识）
    await expect(page.getByRole('tab', { name: 'WP 提交' })).toBeVisible({ timeout: 12_000 })
    await expect(page.getByText('已结束', { exact: true })).toBeVisible()

    // 平台访问恢复：不再被引导回竞赛页；服务端同步解除（topics 200）
    await page.goto('/training')
    await page.waitForURL((u) => u.pathname === '/training')
    const stuToken = await studentToken(request)
    const topics = await request.get(`${E2E.apiURL}/api/v1/topics`, {
      headers: { Authorization: `Bearer ${stuToken}` },
    })
    expect(topics.status()).toBe(200)
  })
})