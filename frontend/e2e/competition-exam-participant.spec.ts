import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsStudent, studentToken } from './helpers'

// 考试参赛端旅程：赛前组卷 → 开赛 → 学生报名入场 → 开始考试 → 单选/多选
// 自动保存 → 交卷 → 结果与答题回顾。答卷为单次尝试，服务端计时。
test.describe('考试参赛端', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedRunningExamCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: `E2E 考试参赛端-${now}`,
        description: '考试作答与自动保存验证',
        kind: 'public',
        format: 'exam',
        start_time: new Date(now + 10 * 60_000).toISOString(),
        end_time: new Date(now + 2 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }

    // 试卷与题目只能在开赛前配置；先组卷，再把竞赛窗口拨到进行中。
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/exam`, {
          headers: auth,
          data: {
            title: 'E2E 初赛笔试卷',
            description: '共两题，覆盖单选与多选。',
            duration_seconds: 1800,
            pass_score: 20,
          },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/exam/questions`, {
          headers: auth,
          data: {
            seq: 1,
            type: 'single',
            title: 'YAK Academy 是什么平台？',
            content: '请选择正确描述。',
            options: [
              { key: 'A', text: '安全实训平台' },
              { key: 'B', text: '视频播放器' },
            ],
            answer: 'A',
            explanation: 'YAK Academy 是安全实训平台。',
            score: 10,
          },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}/exam/questions`, {
          headers: auth,
          data: {
            seq: 2,
            type: 'multiple',
            title: '以下哪些是安全赛制？',
            options: [
              { key: 'A', text: 'CTF' },
              { key: 'B', text: '辩论赛' },
              { key: 'C', text: 'AWD' },
            ],
            answer: ['A', 'C'],
            score: 10,
          },
        })
      ).ok(),
    ).toBeTruthy()

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
    return comp.id
  }

  test('开始考试、自动保存答案并交卷查看结果', async ({ page, request }) => {
    test.slow()
    const compId = await seedRunningExamCompetition(request)
    openIds.push(compId)

    await loginAsStudent(page)
    await page.goto(`/competitions/${compId}`)

    // 考前须知：格式分发到考试视图，展示试卷信息与开考入口。
    await expect(page.getByText('YAK ACADEMY · 考试')).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'E2E 初赛笔试卷' })).toBeVisible()
    await expect(page.getByText('共 2 题 · 总分 20')).toBeVisible()
    await page.getByRole('button', { name: '开始考试' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '考试已开始' })).toBeVisible()

    // 单选题：选择 A 后 500ms 防抖自动保存。
    await expect(page.getByText('答题卡')).toBeVisible()
    await expect(page.getByRole('heading', { level: 3, name: 'YAK Academy 是什么平台？' })).toBeVisible()
    await page.locator('.exam-options label').first().click()
    await expect(page.locator('.exam-q-foot .exam-save-state')).toHaveText('已自动保存')

    // 多选题：第 2 题勾选 A、C，答题卡标记已答。
    await page.locator('.exam-nav-item').nth(1).click()
    await expect(page.getByRole('heading', { level: 3, name: '以下哪些是安全赛制？' })).toBeVisible()
    const options = page.locator('.exam-options-col label')
    await options.nth(0).click()
    await options.nth(2).click()
    await expect(page.locator('.exam-q-foot .exam-save-state')).toHaveText('已自动保存')
    await expect(page.getByText('已答 2/2', { exact: true })).toBeVisible()

    // 交卷确认。
    await page.getByRole('button', { name: '交卷', exact: true }).click()
    const confirm = page.locator('[role=alertdialog]')
    await expect(confirm).toContainText('确认交卷')
    await confirm.getByRole('button', { name: '交卷', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已交卷' })).toBeVisible()

    // 结果：客观题满分通过，答题回顾保留作答与解析。
    await expect(page.getByText('考试结果')).toBeVisible()
    await expect(page.locator('.exam-result-score')).toHaveText('20')
    await expect(page.getByText('通过', { exact: true })).toBeVisible()
    await expect(page.getByText('我的答案：A. 安全实训平台')).toBeVisible()
    await expect(page.getByText('我的答案：A、C')).toBeVisible()
    await expect(page.getByText('YAK Academy 是安全实训平台。')).toBeVisible()

    // 单次尝试：刷新后仍是结果页，不回到作答态。
    await page.reload()
    await expect(page.getByText('考试结果')).toBeVisible()
    await expect(page.locator('.exam-result-score')).toHaveText('20')
  })
})