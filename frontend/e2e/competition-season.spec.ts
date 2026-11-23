import { expect, test, type APIRequestContext, type APIResponse } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, studentToken } from './helpers'

// 赛季旅程：初赛考试 → 复赛 CTF → 决赛 AWD 的完整晋级链，最后从 AWD
// 决赛榜签发证书。覆盖三种赛制共享的参赛者/晋级/证书主线，不重复各赛制
// 自己的单域旅程（competition-exam-* / competition-journey / competition-awd-*）。
test.describe('三赛制赛季旅程', () => {
  const openIds: number[] = []
  const awdIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of awdIds.splice(0)) {
      const token = await adminToken(request)
      await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${id}/awd/end`, {
        headers: { Authorization: `Bearer ${token}` },
      })
    }
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function must(resp: Promise<APIResponse>, what: string): Promise<APIResponse> {
    const r = await resp
    if (!r.ok()) throw new Error(`${what}: HTTP ${r.status()} ${await r.text()}`)
    return r
  }

  test('考试晋级复赛、复赛晋级决赛并签发证书', async ({ page, request }) => {
    test.setTimeout(180_000)
    const suffix = Date.now()
    const admin = await adminToken(request)
    const at = { Authorization: `Bearer ${admin}` }
    const now = Date.now()

    // —— 初赛：组卷、报名、作答交卷（10 分）——
    const examName = `e2e 赛季初赛-${suffix}`
    const examResp = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
        headers: at,
        data: {
          name: examName,
          description: '赛季初赛（考试）',
          kind: 'invitational',
          format: 'exam',
          start_time: new Date(now + 10 * 60_000).toISOString(),
          end_time: new Date(now + 2 * 60 * 60_000).toISOString(),
        },
      }),
      '创建初赛',
    )
    const exam = (await examResp.json()) as { id: number }
    openIds.push(exam.id)
    await must(
      request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${exam.id}/exam`, {
        headers: at,
        data: { title: '赛季初赛卷', duration_seconds: 1800, pass_score: 5 },
      }),
      '配置初赛试卷',
    )
    const question = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${exam.id}/exam/questions`, {
        headers: at,
        data: {
          seq: 1,
          type: 'single',
          title: '赛季初赛题',
          options: [
            { key: 'A', text: '正确' },
            { key: 'B', text: '错误' },
          ],
          answer: 'A',
          score: 10,
        },
      }),
      '创建初赛试题',
    )
    const questionID = ((await question.json()) as { id: number }).id
    const examImport = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${exam.id}/participants`, {
        headers: at,
        data: { individuals: [E2E.student.username] },
      }),
      '导入初赛参赛者',
    )
    const examCode = ((await examImport.json()) as { participants: Array<{ invite_code: string }> })
      .participants[0].invite_code
    await must(
      request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${exam.id}`, {
        headers: at,
        data: {
          start_time: new Date(now - 60_000).toISOString(),
          end_time: new Date(now + 30 * 60_000).toISOString(),
        },
      }),
      '开始初赛',
    )

    const student = await studentToken(request)
    const stu = { Authorization: `Bearer ${student}` }
    await must(
      request.post(`${E2E.apiURL}/api/v1/competitions/${exam.id}/enter`, {
        headers: stu,
        data: { invite_code: examCode },
      }),
      '初赛入场',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/competitions/${exam.id}/exam/attempts`, { headers: stu }),
      '开始答卷',
    )
    await must(
      request.put(
        `${E2E.apiURL}/api/v1/competitions/${exam.id}/exam/attempts/current/answers/${questionID}`,
        { headers: stu, data: { answer: 'A' } },
      ),
      '保存答案',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/competitions/${exam.id}/exam/attempts/current/submit`, {
        headers: stu,
      }),
      '交卷',
    )
    const results = await must(
      request.get(`${E2E.apiURL}/api/v1/admin/competitions/${exam.id}/exam/results`, { headers: at }),
      '初赛成绩单',
    )
    const resultRows = ((await results.json()) as {
      results: Array<{ username: string; total_score: number }>
    }).results
    expect(resultRows.find((row) => row.username === E2E.student.username)?.total_score).toBe(10)
    await endCompetition(request, exam.id)

    // —— 复赛 CTF 目标（邀请赛，晋级后进入）——
    const ctfName = `e2e 赛季复赛-${suffix}`
    const ctfResp = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
        headers: at,
        data: {
          name: ctfName,
          description: '赛季复赛（CTF）',
          kind: 'invitational',
          format: 'ctf',
          start_time: new Date(now + 60 * 60_000).toISOString(),
          end_time: new Date(now + 3 * 60 * 60_000).toISOString(),
        },
      }),
      '创建复赛',
    )
    const ctf = (await ctfResp.json()) as { id: number }
    openIds.push(ctf.id)
    await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${ctf.id}/challenges`, {
        headers: at,
        data: { slugs: [E2E.challenge.slug] },
      }),
      '绑定复赛题目',
    )

    // —— 决赛 AWD 目标（邀请赛，晋级后进入）——
    const awdName = `e2e 赛季决赛-${suffix}`
    const awdResp = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
        headers: at,
        data: {
          name: awdName,
          description: '赛季决赛（AWD）',
          kind: 'invitational',
          format: 'awd',
          start_time: new Date(now + 2 * 60 * 60_000).toISOString(),
          end_time: new Date(now + 5 * 60 * 60_000).toISOString(),
        },
      }),
      '创建决赛',
    )
    const awd = (await awdResp.json()) as { id: number }
    openIds.push(awd.id)
    awdIds.push(awd.id)
    await must(
      request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${awd.id}/awd`, {
        headers: at,
        data: { competitor_type: 'user' },
      }),
      '决赛设为个人赛',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${awd.id}/awd/services`, {
        headers: at,
        data: {
          slug: 'web',
          name: 'Web',
          image: 'nginx:alpine',
          ports: [{ name: 'http', port: 80, protocol: 'tcp' }],
        },
      }),
      '创建决赛服务',
    )

    // —— 晋级 ①：考试榜 → 复赛邀请赛（管理端 UI）——
    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${ctf.id}`)
    await page.getByRole('tab', { name: '晋级' }).click()
    let pane = page.getByRole('tabpanel', { name: '晋级' })
    await pane.getByRole('combobox', { name: '来源竞赛' }).click()
    await page.getByRole('option').filter({ hasText: examName }).click()
    const examThreshold = pane.getByRole('spinbutton', { name: '最低分' })
    await examThreshold.fill('0')
    await examThreshold.press('Tab')
    await pane.getByRole('button', { name: '预览晋级名单' }).click()
    await expect(pane.locator('tr', { hasText: E2E.student.username })).toContainText('10')
    await pane.getByRole('button', { name: '应用晋级（导入参赛者）' }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入 1 名参赛者' })).toBeVisible()

    // 晋级导入不发邀请码回执；重新导入既有参赛者拿到同一邀请码。
    const ctfImport = await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${ctf.id}/participants`, {
        headers: at,
        data: { individuals: [E2E.student.username] },
      }),
      '读取复赛邀请码',
    )
    const ctfCode = ((await ctfImport.json()) as { participants: Array<{ invite_code: string }> })
      .participants[0].invite_code

    // —— 复赛：入场、解题（异步判定）、榜单得分 ——
    await must(
      request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${ctf.id}`, {
        headers: at,
        data: {
          start_time: new Date(now - 60_000).toISOString(),
          end_time: new Date(now + 30 * 60_000).toISOString(),
        },
      }),
      '开始复赛',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/competitions/${ctf.id}/enter`, {
        headers: stu,
        data: { invite_code: ctfCode },
      }),
      '复赛入场',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/competitions/${ctf.id}/submissions`, {
        headers: stu,
        data: { challenge_slug: E2E.challenge.slug, answer: E2E.challenge.flag },
      }),
      '提交复赛 flag',
    )
    await expect
      .poll(
        async () => {
          const board = await request.get(
            `${E2E.apiURL}/api/v1/competitions/${ctf.id}/leaderboard?mode=individual`,
            { headers: stu },
          )
          if (!board.ok()) return -1
          const items = ((await board.json()) as {
            items: Array<{ user_id: number; score: number }>
          }).items
          const me = await request.get(`${E2E.apiURL}/api/v1/me`, { headers: stu })
          const userID = ((await me.json()) as { id: number }).id
          return items.find((item) => item.user_id === userID)?.score ?? 0
        },
        { timeout: 20_000, message: '复赛异步判题后个人榜应显示 50 分' },
      )
      .toBe(50)
    await endCompetition(request, ctf.id)

    // —— 晋级 ②：复赛榜 → 决赛邀请赛（管理端 UI）——
    await page.goto(`/admin/panel/competitions/${awd.id}`)
    await page.getByRole('tab', { name: '晋级' }).click()
    pane = page.getByRole('tabpanel', { name: '晋级' })
    await pane.getByRole('combobox', { name: '来源竞赛' }).click()
    await page.getByRole('option').filter({ hasText: ctfName }).click()
    const ctfThreshold = pane.getByRole('spinbutton', { name: '最低分' })
    await ctfThreshold.fill('0')
    await ctfThreshold.press('Tab')
    await pane.getByRole('button', { name: '预览晋级名单' }).click()
    await expect(pane.locator('tr', { hasText: E2E.student.username })).toContainText('50')
    await pane.getByRole('button', { name: '应用晋级（导入参赛者）' }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入 1 名参赛者' })).toBeVisible()

    // —— 决赛：实名补填 → 开赛/结束 → 从最终榜签发证书 ——
    await must(
      request.put(`${E2E.apiURL}/api/v1/competitions/${awd.id}/profile`, {
        headers: stu,
        data: {
          real_name: '李四',
          school: '示例大学',
          student_no: '2026002',
          email: 'lisi@example.com',
          phone: '13900000000',
        },
      }),
      '决赛实名信息',
    )
    await must(
      request.patch(`${E2E.apiURL}/api/v1/admin/competitions/${awd.id}`, {
        headers: at,
        data: {
          start_time: new Date(now - 60_000).toISOString(),
          end_time: new Date(now + 30 * 60_000).toISOString(),
        },
      }),
      '开始决赛竞赛',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${awd.id}/awd/start`, { headers: at }),
      '开始 AWD 比赛',
    )
    await must(
      request.post(`${E2E.apiURL}/api/v1/admin/competitions/${awd.id}/awd/end`, { headers: at }),
      '结束 AWD 比赛',
    )
    await endCompetition(request, awd.id)

    await page.goto(`/admin/panel/competitions/${awd.id}`)
    await page.getByRole('tab', { name: '证书', exact: true }).click()
    await expect(page.getByText('可签发')).toBeVisible()
    await page.getByLabel('证书名称').fill('赛季决赛获奖证书')
    await page.getByLabel('证书级别').fill('一等奖')
    // 本场没有回合/战报得分（0 分）；「前 N 名」只收正分，改用最低分 0。
    await page.getByRole('combobox', { name: '筛选方式' }).click()
    await page.getByRole('option', { name: '最低总分' }).click()
    await page.getByRole('spinbutton', { name: '最低分' }).fill('0')
    await page.getByRole('button', { name: '预览接收人' }).click()
    await expect(page.getByText('符合「赛季决赛获奖证书」1 人/队。')).toBeVisible()
    await expect(page.getByRole('cell', { name: '李四' })).toBeVisible()
    await page.getByRole('button', { name: '签发证书（1）' }).click()
    const confirm = page.locator('[role=alertdialog]')
    await expect(confirm).toContainText('签发后不可撤销')
    await confirm.getByRole('button', { name: '确定' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已签发 1 张证书' })).toBeVisible()
    const codeCell = page.getByRole('cell', { name: /^[A-F0-9]{16}$/ })
    await expect(codeCell).toBeVisible()
    const code = (await codeCell.innerText()).trim()

    const verified = await request.get(`${E2E.apiURL}/api/v1/certificates/${code}`)
    expect(verified.ok()).toBeTruthy()
    const certificate = (await verified.json()) as {
      valid: boolean
      recipient_name: string
      competition_name: string
    }
    expect(certificate.valid).toBeTruthy()
    expect(certificate.recipient_name).toBe('李四')
    expect(certificate.competition_name).toBe(awdName)
  })
})
