import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, login, loginAsStudent, studentToken } from './helpers'

// 排行榜改版（KPI + 前三 + 我的排名 + 积分分布 + 分页名次表）的用户可见终态。
// 数据来自真实派生链路：学生提交种子题 → 判题 worker 写 score_records →
// /leaderboard 只读投影。用例不依赖其它 spec 的执行顺序（可单独跑），
// 也不断言任何颜色/间距等视觉样式。
test.describe('排行榜', () => {
  test('展示全站概览、前三名、我的排名与积分分布', async ({ page, request }) => {
    const token = await studentToken(request)
    const auth = { Authorization: `Bearer ${token}` }

    // 先确保学生有积分记录：提交种子题并等判题终态；若其它 spec 已判定，
    // 重复提交只返回“不重复计分”，不会改变分数。
    const submitted = await request.post(`${E2E.apiURL}/api/v1/submissions`, {
      headers: auth,
      data: { challenge_slug: E2E.challenge.slug, answer: E2E.challenge.flag },
    })
    expect(submitted.status()).toBe(202)
    const submission = (await submitted.json()) as { id: number }
    await expect.poll(async () => {
      const resp = await request.get(`${E2E.apiURL}/api/v1/submissions/${submission.id}`, {
        headers: auth,
      })
      const body = (await resp.json()) as { status: string }
      return body.status
    }, { timeout: 15_000 }).not.toBe('pending')

    // 判题完成到派生表写入可能有毫秒级延迟；等 /leaderboard 出现本人记录。
    await expect.poll(async () => {
      const resp = await request.get(`${E2E.apiURL}/api/v1/leaderboard?page=1&page_size=20`, {
        headers: auth,
      })
      if (!resp.ok()) return false
      const body = (await resp.json()) as { me: { username: string } | null }
      return body.me?.username === E2E.student.username
    }, { timeout: 15_000 }).toBe(true)

    const boardResp = await request.get(`${E2E.apiURL}/api/v1/leaderboard?page=1&page_size=20`, {
      headers: auth,
    })
    expect(boardResp.ok()).toBeTruthy()
    const board = (await boardResp.json()) as {
      stats: { participants: number; total_score: number; total_solves: number; average_score: number }
      me: { rank: number; username: string; score: number; solved_count: number }
      top: Array<{ username: string; score: number }>
      distribution: Array<{ label: string; count: number }>
    }
    expect(board.me.username).toBe(E2E.student.username)
    expect(board.top.length).toBeGreaterThan(0)

    await loginAsStudent(page)
    await page.goto('/leaderboard')

    await expect(page.getByRole('heading', { name: '排行榜' })).toBeVisible()
    await expect(page.getByTestId('lb-participants')).toHaveText(String(board.stats.participants))
    await expect(page.getByTestId('lb-total-score')).toHaveText(String(board.stats.total_score))
    await expect(page.getByTestId('lb-total-solves')).toHaveText(String(board.stats.total_solves))
    await expect(page.getByTestId('lb-average-score')).toHaveText(String(board.stats.average_score))

    // 前三名领奖台展示 API 返回的全场第一（不按当前页推断）。
    const podium = page.locator('section[aria-label="全场前三名"]')
    await expect(podium).toContainText(board.top[0].username)
    await expect(podium).toContainText(String(board.top[0].score))

    // 我的排名卡片不受分页限制，且名次与接口一致。
    const myCard = page.getByTestId('my-rank')
    await expect(myCard).toContainText('已上榜')
    await expect(myCard).toContainText(`#${board.me.rank}`)
    await expect(myCard).toContainText(E2E.student.username)

    // 名次表包含本人、分数、通关数，并带“我”的标识。
    const row = page.locator('tr', { hasText: E2E.student.username })
    await expect(row).toBeVisible()
    await expect(row).toContainText(String(board.me.score))
    await expect(row).toContainText(`${board.me.solved_count} 题`)
    await expect(row.getByText('我', { exact: true })).toBeVisible()

    // 积分分布至少展示一个真实区间。
    await expect(page.getByText('积分分布', { exact: true })).toBeVisible()
    await expect(page.locator('.dist-list li')).toHaveCount(board.distribution.length)
    await expect(page.locator('.dist-list')).toContainText(board.distribution[0].label)

    // 刷新重新拉取同一视图，数据不丢失。
    const refreshed = page.waitForResponse(
      (resp) => resp.url().includes('/api/v1/leaderboard') && resp.request().method() === 'GET',
    )
    await page.getByRole('button', { name: '刷新排行榜' }).click()
    expect((await refreshed).ok()).toBeTruthy()
    await expect(row).toBeVisible()
  })

  test('未上榜学员看到明确的下一步引导', async ({ page, request }) => {
    const username = `lb-empty-${Date.now()}`
    const password = 'lb-empty-pass-1'
    const token = await adminToken(request)
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/users`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { username, password },
    })
    expect(created.ok()).toBeTruthy()

    await login(page, { username, password })
    await page.goto('/leaderboard')

    const myCard = page.getByTestId('my-rank')
    await expect(myCard).toContainText('未上榜')
    await expect(myCard).toContainText('完成任意题目获得积分后')
    await expect(myCard.getByRole('link', { name: '去题库' })).toHaveAttribute('href', '/training')
  })
})
