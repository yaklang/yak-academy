import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, loginAsStudent, studentToken } from './helpers'

// 竞赛身份名册：参赛者补填实名信息；管理员导入 CSV 名册后，参赛人员表展示
// 姓名/学校，并自动把命中账号导入为本场参赛者。
test.describe('竞赛身份名册', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedInvitational(request: Parameters<typeof adminToken>[0], name: string): Promise<number> {
    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name,
        description: '身份名册验证',
        kind: 'invitational',
        format: 'ctf',
        start_time: new Date(now - 60_000).toISOString(),
        end_time: new Date(now + 30 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    return ((await created.json()) as { id: number }).id
  }

  test('参赛者补填并再次查看实名信息', async ({ page, request }) => {
    const compId = await seedInvitational(request, `E2E 身份补填-${Date.now()}`)
    openIds.push(compId)

    const token = await adminToken(request)
    const imported = await request.post(`${E2E.apiURL}/api/v1/admin/competitions/${compId}/participants`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { individuals: [E2E.student.username] },
    })
    expect(imported.ok()).toBeTruthy()
    const code = ((await imported.json()) as { participants: Array<{ invite_code: string }> }).participants[0]
      .invite_code
    const student = await studentToken(request)
    expect(
      (
        await request.post(`${E2E.apiURL}/api/v1/competitions/${compId}/enter`, {
          headers: { Authorization: `Bearer ${student}` },
          data: { invite_code: code },
        })
      ).ok(),
    ).toBeTruthy()

    await loginAsStudent(page)
    await page.goto(`/competitions/${compId}`)
    await page.getByRole('button', { name: '补填参赛信息' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('姓名').fill('张三')
    await dialog.getByLabel('学校 / 单位').fill('示例大学')
    await dialog.getByLabel('学号').fill('2026001')
    await dialog.getByLabel('邮箱').fill('zhangsan@example.com')
    await dialog.getByLabel('手机').fill('13800000000')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '参赛信息已保存' })).toBeVisible()

    // 再次打开：回显已保存信息，入口按钮文案切换为「参赛信息」。
    await page.getByRole('button', { name: '参赛信息', exact: true }).click()
    await expect(page.getByRole('dialog').getByLabel('姓名')).toHaveValue('张三')
    await expect(page.getByRole('dialog').getByLabel('学校 / 单位')).toHaveValue('示例大学')
    await expect(page.getByRole('dialog').getByLabel('学号')).toHaveValue('2026001')
  })

  test('管理员导入 CSV 名册并在参赛人员表查看', async ({ page, request }) => {
    const compId = await seedInvitational(request, `E2E 名册导入-${Date.now()}`)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await page.getByRole('tab', { name: /参赛人员/ }).click()
    await page.getByRole('button', { name: '导入身份名册' }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: '导入身份名册' })).toBeVisible()
    const csv =
      '\uFEFF账号编码,姓名,学校,学号,邮箱,手机\n' +
      `${E2E.student.username},李四,示例大学,2026002,lisi@example.com,13900000000\n`
    await dialog.getByLabel('选择上传文件').setInputFiles({
      name: 'roster.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(csv, 'utf8'),
    })
    await expect(dialog.getByRole('cell', { name: '李四' })).toBeVisible()
    await dialog.getByRole('button', { name: /导入名册/ }).click()

    const result = page.getByRole('dialog')
    await expect(result.getByText('已导入 1 人名册，并确保其为参赛者。')).toBeVisible()
    await result.getByRole('button', { name: '关闭', exact: true }).click()

    // 名册回填到参赛人员表。
    await expect(page.getByRole('cell', { name: E2E.student.username })).toBeVisible()
    await expect(page.getByRole('cell', { name: '李四' })).toBeVisible()
    await expect(page.getByRole('cell', { name: '示例大学' })).toBeVisible()
    await expect(page.getByRole('cell', { name: '2026002' })).toBeVisible()
  })
})