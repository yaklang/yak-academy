import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin, studentToken } from './helpers'

// AWD 管理端控制台：配置个人赛、创建服务定义、开始比赛、部署靶机（Noop
// 运行时给出可见错误）。验证管理端按 format 分发到 AWD 控制台而非 CTF 详情。
test.describe('AWD 管理端', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedAwdCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name: `E2E AWD 管理端-${now}`,
        description: 'AWD 管理端控制台验证',
        kind: 'public',
        format: 'awd',
        start_time: new Date(now - 60_000).toISOString(),
        end_time: new Date(now + 30 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }
    const student = await studentToken(request)
    const auth = { Authorization: `Bearer ${student}` }
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/register`, { headers: auth })).ok(),
    ).toBeTruthy()
    expect(
      (await request.post(`${E2E.apiURL}/api/v1/competitions/${comp.id}/enter`, { headers: auth })).ok(),
    ).toBeTruthy()
    return comp.id
  }

  test('配置个人赛、创建服务并开始比赛', async ({ page, request }) => {
    const compId = await seedAwdCompetition(request)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)

    // 管理端格式分发：AWD 控制台标题与标签，不出现 CTF 计分模式。
    await expect(page.getByRole('heading', { level: 2 })).toContainText('E2E AWD 管理端')
    await expect(page.getByText('AWD', { exact: true })).toBeVisible()
    await expect(page.getByText('未开始').first()).toBeVisible()

    // 配置：战队 → 个人，回合 60 秒，保存。
    await page.getByRole('combobox', { name: '竞域单位' }).click()
    await page.getByRole('option', { name: '个人（user）' }).click()
    const roundInput = page.locator('.config-form').getByRole('spinbutton')
    await roundInput.fill('60')
    await page.getByRole('button', { name: '保存配置' }).click()
    await expect(page.getByText('AWD 配置已保存')).toBeVisible()

    // 服务定义：新增 web 服务。
    await page.getByRole('tab', { name: '服务定义' }).click()
    await page.getByRole('button', { name: '新增服务' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Slug').fill('web')
    await dialog.getByLabel('名称').fill('Web 服务')
    await dialog.getByLabel('镜像').fill('nginx:alpine')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(page.getByText('服务定义已创建')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'web', exact: true })).toBeVisible()

    // 开始比赛（需要至少一个服务 + 一个已入场参赛者）。
    await page.getByRole('button', { name: '开始比赛' }).click()
    await expect(page.getByText('操作成功')).toBeVisible()
    await expect(page.getByText('进行中').first()).toBeVisible()

    // 无 Docker 运行时：部署靶机给出可见错误，而不是静默失败。
    await page.getByRole('button', { name: '部署靶机' }).click()
    await expect(page.getByText('请求参数有误，请检查后重试')).toBeVisible()

    // 回合/排名/告警标签均可访问（空态明确）。
    await page.getByRole('tab', { name: '回合' }).click()
    await expect(page.getByText('尚未开启回合')).toBeVisible()
    await page.getByRole('tab', { name: '排名' }).click()
    await expect(page.getByRole('cell', { name: E2E.student.username })).toBeVisible()
  })
})
