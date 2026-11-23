import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, studentToken } from './helpers'

// 证书签发：赛后按个人榜前 N 名签发，接收人姓名取竞赛实名名册快照；
// 管理端可预览、签发并查看验证码。
test.describe('竞赛证书', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedEndedScoredCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const auth = { Authorization: `Bearer ${token}` }
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: auth,
      data: {
        name: `E2E 证书签发-${now}`,
        description: '证书预览与签发验证',
        kind: 'invitational',
        format: 'ctf',
        start_time: new Date(now - 2 * 60 * 60_000).toISOString(),
        end_time: new Date(now + 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const compId = ((await created.json()) as { id: number }).id

    const student = await studentToken(request)
    const studentAuth = { Authorization: `Bearer ${student}` }
    const me = await request.get(`${E2E.apiURL}/api/v1/me`, { headers: studentAuth })
    const userID = ((await me.json()) as { id: number }).id
    const imported = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${compId}/participants`, {
      headers: auth,
      data: { individuals: [E2E.student.username] },
    })
    expect(imported.ok()).toBeTruthy()
    expect(
      (
        await request.put(`${E2E.apiURL}/api/v1/competitions/${compId}/profile`, {
          headers: studentAuth,
          data: {
            real_name: '张三',
            school: '示例大学',
            student_no: '2026001',
            email: 'zhangsan@example.com',
            phone: '13800000000',
          },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${compId}/adjustments`, {
          headers: auth,
          data: { user_id: userID, points: 100, reason: '证书测试基础分' },
        })
      ).ok(),
    ).toBeTruthy()
    await endCompetition(request, compId)
    return compId
  }

  test('预览接收人并签发证书', async ({ page, request }) => {
    test.slow()
    const compId = await seedEndedScoredCompetition(request)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await page.getByRole('tab', { name: '证书', exact: true }).click()
    await expect(page.getByText('证书在竞赛结束后签发')).toHaveCount(0)
    await expect(page.getByText('可签发')).toBeVisible()

    await page.getByLabel('证书名称').fill('E2E 一等奖')
    await page.getByLabel('证书级别').fill('一等奖')
    await page.getByRole('button', { name: '预览接收人' }).click()
    await expect(page.getByText('符合「E2E 一等奖」1 人/队。')).toBeVisible()
    await expect(page.getByRole('cell', { name: '张三' })).toBeVisible()
    await expect(page.getByRole('cell', { name: E2E.student.username })).toBeVisible()

    await page.getByRole('button', { name: '签发证书（1）' }).click()
    const confirm = page.locator('[role=alertdialog]')
    await expect(confirm).toContainText('签发后不可撤销')
    await confirm.getByRole('button', { name: '确定' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已签发 1 张证书' })).toBeVisible()

    await expect(page.getByText('签发批次（1）')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'E2E 一等奖' })).toBeVisible()
    await expect(page.getByText('证书明细（1）')).toBeVisible()
    await expect(page.getByRole('cell', { name: '张三' })).toBeVisible()
    const codeCell = page.getByRole('cell', { name: /^[A-F0-9]{16}$/ })
    await expect(codeCell).toBeVisible()
    const code = (await codeCell.innerText()).trim()

    // 证书 CSV 含验证码/验证路径；获奖名单 CSV 不含验证码。
    const certDownloadPromise = page.waitForEvent('download')
    await page.getByRole('button', { name: '导出证书 CSV' }).click()
    const certDownload = await certDownloadPromise
    expect(certDownload.suggestedFilename()).toContain('certificates.csv')
    const certCSV = await readFile((await certDownload.path()) as string, 'utf8')
    expect(certCSV.startsWith('﻿')).toBeTruthy()
    expect(certCSV).toContain('张三')
    expect(certCSV).toContain(code)
    expect(certCSV).toContain(`/certificates/${code}`)

    const awardDownloadPromise = page.waitForEvent('download')
    await page.getByRole('button', { name: '导出获奖名单 CSV' }).click()
    const awardDownload = await awardDownloadPromise
    expect(awardDownload.suggestedFilename()).toContain('awards.csv')
    const awardCSV = await readFile((await awardDownload.path()) as string, 'utf8')
    expect(awardCSV).toContain('张三')
    expect(awardCSV).toContain('一等奖')
    expect(awardCSV).not.toContain(code)

    // 公开验证接口无需登录；页面凭验证码展示姓名/奖项/名次快照并支持打印。
    const verified = await request.get(`${E2E.apiURL}/api/v1/certificates/${code}`)
    expect(verified.ok()).toBeTruthy()
    const payload = (await verified.json()) as {
      valid: boolean
      recipient_name: string
      competition_name: string
      title: string
      rank: number
      score: number
    }
    expect(payload.valid).toBeTruthy()
    expect(payload.recipient_name).toBe('张三')
    expect(payload.title).toBe('E2E 一等奖')
    expect(payload.rank).toBe(1)
    expect(payload.score).toBeGreaterThan(0)

    await page.goto(`/certificates/${code}`)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('张三')
    await expect(page.getByText('已验证 · 证书有效')).toBeVisible()
    await expect(page.getByText('E2E 一等奖')).toBeVisible()
    await expect(page.getByRole('button', { name: '打印 / 保存 PDF' })).toBeVisible()
  })
})