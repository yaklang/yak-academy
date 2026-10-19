import type { APIRequestContext, Page } from '@playwright/test'
import { expect } from '@playwright/test'
import { E2E } from './fixtures/consts'

// 经真实登录页登录（登录旅程本身就是被测行为，不走 API 直设 token）。
export async function login(page: Page, who: { username: string; password: string }) {
  await page.goto('/login')
  await page.getByPlaceholder('用户名').fill(who.username)
  await page.getByPlaceholder('密码').fill(who.password)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL((u) => !u.pathname.startsWith('/login'))
}

export function loginAsAdmin(page: Page) {
  return login(page, E2E.admin)
}

// 可传覆盖密码：账号生命周期用例改密/重置后以新密码重登。
export function loginAsStudent(page: Page, password: string = E2E.student.password) {
  return login(page, { username: E2E.student.username, password })
}

// 登出按钮位于两端布局头部；登出后应回到登录页。
export async function logout(page: Page) {
  await page.getByRole('button', { name: '登出' }).click()
  await page.waitForURL((u) => u.pathname === '/login')
}

// ---------- 竞赛域种子（隔离/解锁/回归 spec 共用） ----------

// 学生登录换 API token（服务端行为断言用；不经 UI 是因为 UI 断言由页面路径覆盖）。
export async function studentToken(request: APIRequestContext): Promise<string> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.student })
  const { token } = (await login.json()) as { token: string }
  return token
}

// 建一个进行中的竞赛（now-1min 起、+30min 止），绑给定种子题并把学生导入为
// 个人参赛者（自动投递站内邀请邮件），返回 (竞赛ID, 邀请码)。
// 竞赛起止时间相对“现在”，只能用例内即时创建（global-setup 预播种会过期/提前结束）；
// 创建走管理 API，与 global-setup 播种同一机制，不直写 DB、不硬编码运行期 ID。
export async function seedRunningCompetition(
  request: APIRequestContext,
  name: string,
  slugs: string[],
): Promise<{ id: number; code: string }> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  const { token } = (await login.json()) as { token: string }
  const auth = { Authorization: `Bearer ${token}` }
  const now = Date.now()
  const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
    headers: auth,
    data: {
      name,
      description: 'e2e 竞赛域种子',
      start_time: new Date(now - 60_000).toISOString(),
      end_time: new Date(now + 30 * 60_000).toISOString(),
    },
  })
  expect(created.ok()).toBeTruthy()
  const comp = (await created.json()) as { id: number }
  const bound = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/challenges`, {
    headers: auth,
    data: { slugs },
  })
  expect(bound.ok()).toBeTruthy()
  const imported = await request.post(
    `${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/participants`,
    { headers: auth, data: { individuals: [E2E.student.username] } },
  )
  expect(imported.ok()).toBeTruthy()
  const result = (await imported.json()) as {
    participants: Array<{ username: string; invite_code: string }>
  }
  const me = result.participants.find((p) => p.username === E2E.student.username)
  expect(me).toBeTruthy()
  return { id: comp.id, code: me!.invite_code }
}

// 把竞赛 end_time 改到过去（running 期间允许缩短）：比赛立即结束、入场锁解除。
// 已结束的竞赛再缩短会被 422 拒绝，调用方按需忽略响应（幂等收尾）。
export async function endCompetition(request: APIRequestContext, id: number): Promise<void> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  const { token } = (await login.json()) as { token: string }
  await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { end_time: new Date(Date.now() - 60_000).toISOString() },
  })
}
