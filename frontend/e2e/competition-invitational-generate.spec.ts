import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsAdmin } from './helpers'

// 邀请赛批量建号旅程（竞赛分公开/邀请两档）：邀请赛不开放报名，参赛者由
// 管理端定向配置——
//   一键生成账号（可配置自动组队、默认密码 123456、可导出 CSV 分发）；
//   固定格式表格导入（条目 = 账号编码或用户名，引用既有账号；缺失条目回报
//   missing，不自动建号）。
// 建赛走 API 播种，生成/导入/导出全部走真实 UI。

// 建一个进行中的邀请赛（kind 缺省 = invitational）。
async function seedInvitational(request: APIRequestContext, name: string): Promise<number> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  const { token } = (await login.json()) as { token: string }
  const now = Date.now()
  const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name,
      description: '邀请赛生成旅程',
      start_time: new Date(now - 60_000).toISOString(),
      end_time: new Date(now + 30 * 60_000).toISOString(),
    },
  })
  expect(created.ok()).toBeTruthy()
  const comp = (await created.json()) as { id: number; kind: string }
  expect(comp.kind).toBe('invitational')
  return comp.id
}

const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

// 队伍导入断言需要「首次导入」的账号（重复导入沿用首次队伍归属，以先到为准），
// 经管理 API 预建两个专用户号（与 global-setup 建学生号同一机制）。
async function seedTeamUser(request: APIRequestContext, username: string): Promise<void> {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })
  const { token } = (await login.json()) as { token: string }
  const resp = await request.post(`${E2E.apiURL}/api/v1/admin/users`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { username, password: 'team-seed-pass-1', global_role: 'student' },
  })
  expect(resp.ok()).toBeTruthy()
}

test.describe('邀请赛一键生成与表格导入', () => {
  test('生成账号默认密码 123456 并自动组队；CSV 导入回报缺失条目', async ({ page, request }) => {
    test.slow() // 管理端整页 UI 往返（生成 + 两种 CSV 导入 + 登录验证）
    const compId = await seedInvitational(request, E2E.competition.generateName)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await page.getByRole('tab', { name: /参赛人员/ }).click()

    // —— 一键生成：4 人、每 2 人一队、初始密码留空（默认 123456）——
    await page.getByRole('button', { name: '一键生成账号' }).click()
    const genDialog = page.locator('.el-dialog', { hasText: '一键生成参赛账号' })
    await expect(genDialog).toBeVisible()
    await genDialog.getByPlaceholder('如 class26（1-16 位小写字母/数字/连字符）').fill('e2egen')
    // 数量/每队人数为 el-input-number：填值后 Tab 失焦提交（Element Plus 失焦才落值）。
    await genDialog.locator('.el-input-number').nth(0).locator('input').fill('4')
    await genDialog.locator('.el-input-number').nth(0).locator('input').press('Tab')
    await genDialog.locator('.el-input-number').nth(1).locator('input').fill('2')
    await genDialog.locator('.el-input-number').nth(1).locator('input').press('Tab')
    await genDialog.getByRole('button', { name: '生成并导入' }).click()

    await expect(genDialog.getByText('新建 4 个账号')).toBeVisible()
    await expect(genDialog.getByText('编入 2 支队伍')).toBeVisible()
    // 默认密码 123456 逐账号显示（用户名零填充：e2egen-01…e2egen-04）。
    for (const name of ['e2egen-01', 'e2egen-02', 'e2egen-03', 'e2egen-04']) {
      await expect(genDialog.locator('tr', { hasText: name })).toBeVisible()
    }
    await expect(genDialog.getByText('123456').first()).toBeVisible()
    // 自动组队按生成顺序：01/02 → 第1队，03/04 → 第2队。
    await expect(genDialog.locator('tr', { hasText: 'e2egen-01' })).toContainText('第1队')
    await expect(genDialog.locator('tr', { hasText: 'e2egen-03' })).toContainText('第2队')

    // 导出 CSV（分发账号凭据的主要途径）。
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      genDialog.getByRole('button', { name: '导出账号 CSV' }).click(),
    ])
    expect(download.suggestedFilename()).toContain('e2egen')
    await genDialog.getByRole('button', { name: '关闭', exact: true }).click()

    // 生成账号真实可用：默认密码 123456 能登录（服务端行为断言）。
    const genLogin = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: 'e2egen-01', password: '123456' },
    })
    expect(genLogin.ok()).toBeTruthy()

    // —— 固定格式表格导入（个人名单）：缺失条目回报 missing，不阻断 ——
    await page.getByRole('button', { name: '表格导入队伍 / 人员' }).click()
    const importDialog = page.locator('.el-dialog', { hasText: '表格导入队伍 / 参赛人员' })
    await expect(importDialog).toBeVisible()
    // 个人名单默认选中；CSV 首行表头自动跳过，条目 = 账号编码或用户名。
    await importDialog.locator('input[type=file]').setInputFiles({
      name: 'individuals.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(`账号\nYA26-99999\n${E2E.student.username}\n`, 'utf-8'),
    })
    await expect(importDialog.locator('code', { hasText: 'YA26-99999' })).toBeVisible()
    await importDialog.getByRole('button', { name: /导入/ }).click()

    // 以标题锚定结果对话框（导入对话框的提示文案里也含「导入结果」字样）。
    const resultDialog = page
      .locator('.el-dialog')
      .filter({ has: page.locator('.el-dialog__title', { hasText: '导入结果' }) })
    await expect(resultDialog).toBeVisible()
    // 缺失条目（编码与用户名均未命中）在警告中列出，导入不中断。
    await expect(resultDialog).toContainText('YA26-99999')
    await expect(resultDialog.locator('tr', { hasText: E2E.student.username })).toBeVisible()
    await resultDialog.getByRole('button', { name: '关闭', exact: true }).click()

    // —— 固定格式表格导入（队伍名单）：一行一队，成员 = 既有账号 ——
    // 成员用专用新号：重复导入沿用首次队伍归属（生成时已进第1/2队），新号
    // 才能让「蓝队」归属在本轮导入生效。
    await seedTeamUser(request, 'teamseed-1')
    await seedTeamUser(request, 'teamseed-2')
    await page.getByRole('button', { name: '表格导入队伍 / 人员' }).click()
    await expect(importDialog).toBeVisible()
    await importDialog.getByText('队伍名单', { exact: true }).click()
    await importDialog.locator('input[type=file]').setInputFiles({
      name: 'teams.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from('队伍,成员1,成员2\n蓝队,teamseed-1,teamseed-2\n', 'utf-8'),
    })
    await expect(importDialog.locator('tr', { hasText: '蓝队' })).toBeVisible()
    await importDialog.getByRole('button', { name: /导入/ }).click()
    await expect(resultDialog).toBeVisible()
    await expect(resultDialog.locator('tr', { hasText: '蓝队' })).toHaveCount(2) // 两名成员行
    await resultDialog.getByRole('button', { name: '关闭', exact: true }).click()

    // 参赛人员表：生成 + 导入的参赛者都在，邀请码可见（邀请赛凭码入场）。
    const partTab = page.locator('.el-table', { hasText: E2E.student.username }).first()
    await expect(partTab).toBeVisible()
    await expect(page.locator('tr', { hasText: 'e2egen-01' }).first()).toBeVisible()
  })
})
