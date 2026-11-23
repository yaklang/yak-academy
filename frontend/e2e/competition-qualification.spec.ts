import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, seedRunningCompetitionFor, studentToken } from './helpers'

// 晋级旅程：来源 CTF 榜单预览 → 目标邀请赛应用，参赛者被导入并生成邀请码。
// 用 min_score=0 纳入零分参赛者，避免依赖异步判题时序。

test.describe('晋级名单预览与应用', () => {
  const openIds: number[] = []
  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  test('从来源 CTF 榜单预览并导入目标邀请赛', async ({ page, request }) => {
    test.slow()
    const suffix = Date.now()
    const sourceName = `e2e 晋级来源-${suffix}`
    const targetName = `e2e 晋级目标-${suffix}`
    const source = await seedRunningCompetitionFor(
      request,
      sourceName,
      [E2E.challenge.slug],
      [E2E.student.username],
    )
    openIds.push(source.id)

    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name: targetName,
        description: '晋级目标邀请赛',
        start_time: new Date(now + 60 * 60_000).toISOString(),
        end_time: new Date(now + 3 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const target = (await created.json()) as { id: number }
    openIds.push(target.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${target.id}`)
    await page.getByRole('tab', { name: '晋级' }).click()

    const pane = page.getByRole('tabpanel', { name: '晋级' })
    await pane.getByRole('combobox', { name: '来源竞赛' }).click()
    await page.getByRole('option').filter({ hasText: sourceName }).click()
    const threshold = pane.getByRole('spinbutton', { name: '最低分' })
    await threshold.fill('0')
    await threshold.press('Tab')
    await pane.getByRole('button', { name: '预览晋级名单' }).click()

    await expect(pane.locator('tr', { hasText: E2E.student.username })).toBeVisible()
    await pane.getByRole('button', { name: '应用晋级（导入参赛者）' }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入 1 名参赛者' })).toBeVisible()
    await expect(pane.locator('tr', { hasText: sourceName })).toContainText('1')
  })

  test('从考试来源按成绩单排名晋级', async ({ page, request }) => {
    test.slow()
    const suffix = Date.now()
    const token = await adminToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const now = Date.now()

    // 考试来源：先组卷，导入学生，再开赛作答交卷。
    const sourceName = `e2e 考试晋级来源-${suffix}`
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: sourceName,
        description: '考试来源晋级验证',
        kind: 'invitational',
        format: 'exam',
        start_time: new Date(now + 10 * 60_000).toISOString(),
        end_time: new Date(now + 2 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const source = (await created.json()) as { id: number }
    openIds.push(source.id)
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${source.id}/exam`, {
          headers: auth,
          data: { title: '考试晋级试卷', duration_seconds: 1800, pass_score: 10 },
        })
      ).ok(),
    ).toBeTruthy()
    const question = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${source.id}/exam/questions`, {
      headers: auth,
      data: {
        seq: 1,
        type: 'single',
        title: '晋级题',
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
    const imported = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${source.id}/participants`, {
      headers: auth,
      data: { individuals: [E2E.student.username] },
    })
    expect(imported.ok()).toBeTruthy()
    const code = ((await imported.json()) as { participants: Array<{ invite_code: string }> }).participants[0]
      .invite_code
    expect(
      (
        await request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${source.id}`, {
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
      (
        await request.post(`${E2E.apiURL}/api/v1/competitions/${source.id}/enter`, {
          headers: studentAuth,
          data: { invite_code: code },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${source.id}/exam/attempts`, { headers: studentAuth })).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.put(
          `${E2E.apiURL}/api/v1/competitions/${source.id}/exam/attempts/current/answers/${questionID}`,
          { headers: studentAuth, data: { answer: 'A' } },
        )
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/competitions/${source.id}/exam/attempts/current/submit`, {
          headers: studentAuth,
        })
      ).ok(),
    ).toBeTruthy()

    // 目标邀请赛（复赛）。
    const targetName = `e2e 复赛目标-${suffix}`
    const targetResp = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: targetName,
        description: '考试晋级目标',
        kind: 'invitational',
        format: 'ctf',
        start_time: new Date(now + 60 * 60_000).toISOString(),
        end_time: new Date(now + 3 * 60 * 60_000).toISOString(),
      },
    })
    expect(targetResp.ok()).toBeTruthy()
    const target = (await targetResp.json()) as { id: number }
    openIds.push(target.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${target.id}`)
    await page.getByRole('tab', { name: '晋级' }).click()
    const pane = page.getByRole('tabpanel', { name: '晋级' })
    await pane.getByRole('combobox', { name: '来源竞赛' }).click()
    await page.getByRole('option').filter({ hasText: sourceName }).click()
    await pane.getByRole('combobox', { name: '筛选方式' }).click()
    await page.getByRole('option', { name: '前 N 名' }).click()
    const threshold = pane.getByRole('spinbutton', { name: 'N 名' })
    await threshold.fill('1')
    await threshold.press('Tab')
    await pane.getByRole('button', { name: '预览晋级名单' }).click()
    await expect(pane.locator('tr', { hasText: E2E.student.username })).toContainText('10')
    await pane.getByRole('button', { name: '应用晋级（导入参赛者）' }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入 1 名参赛者' })).toBeVisible()
  })
})
