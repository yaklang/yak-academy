import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin } from './helpers'

// 管理端创建竞赛的赛制/计分/报名窗口旅程：竞赛形式（CTF/AWD/考试）、
// CTF 固定计分、可选报名窗口都必须能从 UI 创建并在详情页正确回显。

function localDateTime(ms: number): string {
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

test.describe('竞赛赛制与报名窗口', () => {
  const openIds: number[] = []
  test.afterEach(async ({ request }) => {
    for (const id of openIds.splice(0)) await endCompetition(request, id)
  })

  test('创建固定计分公开赛并配置报名窗口', async ({ page, request }) => {
    test.slow()
    const name = `e2e 赛制-${Date.now()}`
    await loginAsAdmin(page)
    await page.goto('/admin/panel/competitions')
    await page.getByRole('button', { name: '创建竞赛' }).click()

    const dialog = page.locator('[data-slot=dialog-content]', { hasText: '创建竞赛' })
    await expect(dialog).toBeVisible()
    await dialog.getByPlaceholder('如：2026 春季新生赛').fill(name)
    await dialog.getByRole('radio', { name: /公开赛/ }).check()
    await dialog.getByRole('radio', { name: /固定计分/ }).check()

    const now = Date.now()
    await dialog.getByLabel('开始时间').fill(localDateTime(now - 5 * 60_000))
    await dialog.getByLabel('结束时间').fill(localDateTime(now + 30 * 60_000))
    await dialog.getByLabel('报名开始（可选）').fill(localDateTime(now - 2 * 60_000))
    await dialog.getByLabel('报名截止（可选）').fill(localDateTime(now + 10 * 60_000))
    await dialog.getByRole('button', { name: '创建', exact: true }).click()

    await expect(page.locator('[data-sonner-toast]', { hasText: '竞赛已创建' })).toBeVisible()
    const row = page.locator('tr', { hasText: name })
    await expect(row).toBeVisible()
    await expect(row).toContainText('CTF')

    // 详情页回显：格式/计分 tag + 报名窗口摘要。
    const token = await adminToken(request)
    const list = await request.get(`${E2E.apiURL}/api/v1/admin/competitions?page=1&page_size=100`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(list.ok()).toBeTruthy()
    const body = (await list.json()) as { items: Array<{ id: number; name: string }> }
    const comp = body.items.find((item) => item.name === name)
    expect(comp).toBeTruthy()
    openIds.push(comp!.id)

    await page.goto(`/admin/panel/competitions/${comp!.id}`)
    await expect(page.getByText('固定计分', { exact: true })).toBeVisible()
    await expect(page.locator('.hint')).toContainText('报名')
  })
})
