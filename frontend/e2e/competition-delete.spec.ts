import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsAdmin, seedRunningCompetition } from './helpers'

// 竞赛删除旅程（管理端操作列「删除」）：
// running 期间删除被 422 拒绝并给出可操作提示（先把结束时间改到过去）；
// 比赛结束后删除成功——列表行消失、详情 404，与后端级联清理一致。
const { challenge: C } = E2E

// 兜底收尾：失败也把比赛结束，避免学生账号被锁进后续 spec。
let openId: number | null = null
test.afterEach(async ({ request }) => {
  if (openId) await endCompetition(request, openId)
  openId = null
})

test.describe('竞赛删除旅程', () => {
  test('进行中拒绝删除；结束后删除成功且列表移除', async ({ page, request }) => {
    const comp = await seedRunningCompetition(request, 'E2E 删除验证赛', [C.slug])
    openId = comp.id

    await loginAsAdmin(page)
    await page.goto('/admin/panel/competitions')
    const row = page.locator('.el-table tr', { hasText: 'E2E 删除验证赛' })
    await expect(row).toBeVisible()
    await expect(row).toContainText('进行中')

    // running：确认后删除被拒，提示「进行中的竞赛不可删除」
    await row.getByRole('button', { name: '删除' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(page.locator('.el-message', { hasText: '进行中的竞赛不可删除' })).toBeVisible()
    // 行仍在（未误删）
    await expect(row).toBeVisible()

    // 比赛结束（管理 API 把 end_time 改到过去——起止相对“现在”，UI 表单时间
    // 解析涉及时区换算易碎；结束动作本身不是被测功能，删除操作才是）
    await endCompetition(request, comp.id)

    // 回列表删除：确认后成功，行消失
    await page.goto('/admin/panel/competitions')
    const row2 = page.locator('.el-table tr', { hasText: 'E2E 删除验证赛' })
    await expect(row2).toBeVisible()
    await row2.getByRole('button', { name: '删除' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(page.locator('.el-message', { hasText: '已删除' })).toBeVisible()
    await expect(page.locator('.el-table tr', { hasText: 'E2E 删除验证赛' })).toHaveCount(0)

    // 详情直访 404（真删，非仅前端隐藏）
    const resp = await request.get(`${E2E.apiURL}/api/v1/admin/competitions/${comp.id}`, {
      headers: { Authorization: `Bearer ${(await (await request.post(`${E2E.apiURL}/api/v1/auth/login`, { data: E2E.admin })).json()).token}` },
    })
    expect(resp.status()).toBe(404)
    openId = null // 已删，afterEach 无需再收尾
  })
})