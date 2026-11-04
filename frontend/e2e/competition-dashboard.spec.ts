import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsStudent, seedRunningCompetition, studentToken } from './helpers'

// 竞赛看板回归：多题进度、得分曲线、分类/提交统计、题目通过率与个人榜显示名。
// 关键回归点：
//   ① CompetitionScore 唯一索引必须包含 challenge_id——同一用户在同一竞赛
//      通关第二题不能被旧索引 (competition_id, user_id) 拒绝；
//   ② 个人榜"选手"列显示用户名，而不是所属队伍（曾误用 team_name）。
const { challenge: C } = E2E
const openIds: number[] = []

test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

async function waitAnalyticsSettled(
  request: APIRequestContext,
  competitionID: number,
  auth: { Authorization: string },
) {
  for (let i = 0; i < 60; i++) {
    const res = await request.get(`${E2E.apiURL}/api/v1/competitions/${competitionID}/view`, {
      headers: auth,
    })
    if (res.ok()) {
      const view = await res.json()
      const a = view.analytics
      if (
        a?.score === 110 &&
        a.solved_count === 2 &&
        a.submission_stats.correct === 2 &&
        a.submission_stats.incorrect === 1
      ) {
        return
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  throw new Error('competition judging did not settle')
}

test('竞赛概览看板：多题进度、得分曲线与个人榜显示名', async ({ page, request }) => {
  test.slow()
  const comp = await seedRunningCompetition(request, 'E2E 看板验证赛', [C.slug, C.linkedSlug])
  openIds.push(comp.id)

  // 播种全走 API：入场 + 两题通关（60 + 50）+ 一次错误提交。
  const token = await studentToken(request)
  const auth = { Authorization: `Bearer ${token}` }
  const enter = await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/enter`, {
    headers: auth,
    data: { invite_code: comp.code },
  })
  expect(enter.ok()).toBeTruthy()
  for (const [slug, answer] of [
    [C.slug, C.flag],
    [C.linkedSlug, 'wrong'],
    [C.linkedSlug, C.linkedFlag],
  ] as const) {
    const sub = await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/submissions`, {
      headers: auth,
      data: { challenge_slug: slug, answer },
    })
    expect(sub.ok()).toBeTruthy()
  }
  await waitAnalyticsSettled(request, comp.id, auth)

  // UI：登录后默认落在概览，KPI 与图表都读同一份 analytics。
  await loginAsStudent(page)
  await page.goto(`/competitions/${comp.id}`)
  await expect(page.getByRole('heading', { name: 'E2E 看板验证赛' })).toBeVisible()
  await expect(page.getByText('我的得分', { exact: true })).toBeVisible()

  const scoreKpi = page.locator('.kpi', { hasText: '我的得分' })
  await expect(scoreKpi.locator('.kpi-value')).toHaveText('110')
  const solvedKpi = page.locator('.kpi', { hasText: '已通关' })
  await expect(solvedKpi.locator('.kpi-value')).toContainText('2 / 2')
  const rankKpi = page.locator('.kpi', { hasText: '当前排名' })
  await expect(rankKpi.locator('.kpi-value')).toHaveText('#1')

  // 得分曲线有真实数据（canvas 的 aria-label 是文字版摘要，不依赖颜色）。
  await expect(page.locator('.overview canvas')).toHaveAttribute(
    'aria-label',
    /累计 110 分，共 2 次通关/,
  )
  const categoryCard = page.locator('.chart-card', { hasText: '分类进度' })
  await expect(categoryCard).toContainText('密码学')
  await expect(categoryCard).toContainText('Web 安全')
  const submissionCard = page.locator('.chart-card', { hasText: '提交结果' })
  await expect(submissionCard.locator('.legend li', { hasText: '正确' })).toContainText('2')
  await expect(submissionCard.locator('.legend li', { hasText: '错误' })).toContainText('1')

  // 题目表：两题都显示通关与通过人数（此前第二题会因唯一索引撞键而无法计分）。
  await page.getByRole('tab', { name: '题目' }).click()
  const caesarRow = page.locator('tr', { hasText: C.title })
  await expect(caesarRow).toContainText('已通关')
  await expect(caesarRow).toContainText('1/1 人通过')
  const linkedRow = page.locator('tr', { hasText: C.linkedTitle })
  await expect(linkedRow).toContainText('已通关')
  await expect(linkedRow).toContainText('1/1 人通过')

  // 排行榜：个人参赛默认个人榜，"选手"列必须是用户名而不是队伍占位符。
  await page.getByRole('tab', { name: '排行榜' }).click()
  const boardRow = page.locator('tr', { hasText: E2E.student.username })
  await expect(boardRow).toContainText('110')
  await expect(boardRow.locator('td').nth(1)).toHaveText(E2E.student.username)
})
