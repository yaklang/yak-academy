import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, studentToken } from './helpers'

// 考试管理端成绩单：学生经 API 完成作答并交卷后，管理员在考试管理页看到
// 排名/状态/分数汇总，并导出带 BOM 的成绩 CSV。
test.describe('考试成绩单', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedSubmittedExam(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: `E2E 考试成绩单-${now}`,
        description: '考试结果与导出验证',
        kind: 'public',
        format: 'exam',
        start_time: new Date(now + 10 * 60_000).toISOString(),
        end_time: new Date(now + 2 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/exam`, {
          headers: auth,
          data: { title: 'E2E 成绩单试卷', duration_seconds: 1800, pass_score: 10 },
        })
      ).ok(),
    ).toBeTruthy()
    const question = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/exam/questions`, {
      headers: auth,
      data: {
        seq: 1,
        type: 'single',
        title: '成绩单题目',
        options: [
          { key: 'A', text: '正确' },
          { key: 'B', text: '错误' },
        ],
        answer: 'A',
        score: 10,
      },
    })
    expect(question.ok()).toBeTruthy()
    const questionID = ((await question.json()) as { id: number }).id
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}`, {
          headers: auth,
          data: {
            start_time: new Date(now - 60_000).toISOString(),
            end_time: new Date(now + 30 * 60_000).toISOString(),
          },
        })
      ).ok(),
    ).toBeTruthy()

    const student = await studentToken(request)
    const studentAuth = { Authorization: `Bearer ${student}` }
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/register`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/enter`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/exam/attempts`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.put(
          `${E2E.apiURL}/api/v1/competitions/${comp.id}/exam/attempts/current/answers/${questionID}`,
          { headers: studentAuth, data: { answer: 'A' } },
        )
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/exam/attempts/current/submit`, {
          headers: studentAuth,
        })
      ).ok(),
    ).toBeTruthy()
    return comp.id
  }

  test('查看成绩排名并导出 CSV', async ({ page, request }) => {
    test.slow()
    const compId = await seedSubmittedExam(request)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await expect(page.getByRole('heading', { level: 2 })).toContainText('E2E 考试成绩单')

    const card = page.locator('.ui-card', { hasText: '成绩单' }).last()
    await expect(card.getByText('成绩单', { exact: true })).toBeVisible()
    await expect(card.getByText('参赛 1 · 已开始 1 · 已交卷 1 · 超时 0 · 通过 1', { exact: false })).toBeVisible()
    await expect(card.getByRole('cell', { name: E2E.student.username })).toBeVisible()
    await expect(card.getByRole('cell', { name: '1', exact: true })).toBeVisible()
    await expect(card.getByRole('cell', { name: '已交卷' })).toBeVisible()
    await expect(card.getByRole('cell', { name: '10', exact: true }).first()).toBeVisible()
    await expect(card.getByRole('cell', { name: '通过', exact: true })).toBeVisible()

    const downloadPromise = page.waitForEvent('download')
    await card.getByRole('button', { name: '导出成绩 CSV' }).click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toContain('exam-results.csv')
    const path = await download.path()
    expect(path).toBeTruthy()
    const raw = await readFile(path as string, 'utf8')
    expect(raw.startsWith('\uFEFF')).toBeTruthy()
    expect(raw).toContain(E2E.student.username)
    expect(raw).toContain('已交卷')
  })
})