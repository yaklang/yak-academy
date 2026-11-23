import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin } from './helpers'

// 考试管理端：格式分发到考试管理页，组卷（单选/多选）、试卷总分随题目重算，
// 编辑与删除题目同步更新总分。竞赛开始后试卷锁定（后端校验）。
test.describe('考试管理端', () => {
  const openIds: number[] = []

  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  async function seedExamCompetition(request: Parameters<typeof adminToken>[0]): Promise<number> {
    const token = await adminToken(request)
    const now = Date.now()
    const created = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        name: `E2E 考试管理-${now}`,
        description: '考试试卷与试题管理验证',
        kind: 'public',
        format: 'exam',
        start_time: new Date(now + 10 * 60_000).toISOString(),
        end_time: new Date(now + 2 * 60 * 60_000).toISOString(),
      },
    })
    expect(created.ok()).toBeTruthy()
    const comp = (await created.json()) as { id: number }
    return comp.id
  }

  test('配置试卷、组卷并重算总分', async ({ page, request }) => {
    const compId = await seedExamCompetition(request)
    openIds.push(compId)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${compId}`)
    await expect(page.getByRole('heading', { level: 2 })).toContainText('E2E 考试管理')
    await expect(page.getByText('考试', { exact: true })).toBeVisible()
    await expect(page.getByText('可编辑')).toBeVisible()

    // 试卷配置。
    await page.getByLabel('试卷标题').fill('E2E 初赛笔试卷')
    const paperInputs = page.locator('.paper-form').getByRole('spinbutton')
    await paperInputs.nth(0).fill('1800')
    await paperInputs.nth(1).fill('60')
    await page.getByRole('button', { name: '保存试卷' }).click()
    await expect(page.getByText('试卷配置已保存')).toBeVisible()

    // 单选题：10 分。
    await page.getByRole('button', { name: '新增试题' }).click()
    let dialog = page.getByRole('dialog')
    await dialog.getByLabel('题目标题').fill('YAK 是什么')
    await dialog.getByLabel('题干').fill('请选择对 YAK Academy 的正确描述。')
    await dialog.getByLabel('选项 JSON').fill(
      JSON.stringify([
        { key: 'A', text: '安全实训平台' },
        { key: 'B', text: '视频播放器' },
      ]),
    )
    await dialog.getByLabel('答案').fill('A')
    await dialog.getByLabel('分值').fill('10')
    await dialog.getByRole('button', { name: '保存试题' }).click()
    await expect(page.getByText('试题已创建')).toBeVisible()
    await expect(page.getByText('共 1 题 · 总分 10')).toBeVisible()

    // 多选题：20 分，答案 A,C。
    await page.getByRole('button', { name: '新增试题' }).click()
    dialog = page.getByRole('dialog')
    await dialog.getByRole('combobox', { name: '题型' }).click()
    await page.getByRole('option', { name: '多选' }).click()
    await dialog.getByLabel('题目标题').fill('以下哪些是安全赛制？')
    await dialog.getByLabel('选项 JSON').fill(
      JSON.stringify([
        { key: 'A', text: 'CTF' },
        { key: 'B', text: '辩论赛' },
        { key: 'C', text: 'AWD' },
      ]),
    )
    await dialog.getByLabel('答案').fill('A,C')
    await dialog.getByLabel('分值').fill('20')
    await dialog.getByLabel('题序').fill('2')
    await dialog.getByRole('button', { name: '保存试题' }).click()
    await expect(page.getByText('试题已创建')).toBeVisible()
    await expect(page.getByText('共 2 题 · 总分 30')).toBeVisible()

    // 编辑多选题分值 20 → 25，总分 35。
    const multipleRow = page.getByRole('row').filter({ hasText: '以下哪些是安全赛制？' })
    await multipleRow.getByRole('button', { name: '编辑' }).click()
    dialog = page.getByRole('dialog')
    await dialog.getByLabel('分值').fill('25')
    await dialog.getByRole('button', { name: '保存试题' }).click()
    await expect(page.getByText('试题已更新')).toBeVisible()
    await expect(page.getByText('共 2 题 · 总分 35')).toBeVisible()

    // 删除单选题，总分 25。
    const singleRow = page.getByRole('row').filter({ hasText: 'YAK 是什么' })
    await singleRow.getByRole('button', { name: '删除' }).click()
    await page.getByRole('button', { name: '确定' }).click()
    await expect(page.getByText('试题已删除')).toBeVisible()
    await expect(page.getByText('共 1 题 · 总分 25')).toBeVisible()
    await expect(page.getByText('YAK 是什么')).toHaveCount(0)
  })
})
