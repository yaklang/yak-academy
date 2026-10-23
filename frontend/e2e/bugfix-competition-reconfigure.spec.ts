import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { endCompetition, loginAsAdmin, seedRunningCompetition } from './helpers'

// 回归：进行中的竞赛无法重新配置（改 start_time 报 422 cannot change start_time
// after competition started，且没有显式的手动结束入口）（2026-10-21 修复）。
// 修复前行为：竞赛一旦开赛 start_time 永久锁死（结束后也无法重排复活），
// "结束"一场进行中的竞赛只能把 end_time 暗改成过去，列表/详情页均无结束动作。
// 修复后行为：进行中的竞赛可一键手动结束（end_time 置为当前时刻）；
// 已结束的竞赛可重新配置起止时间，新窗口落回未来即再次开赛（回到未开始）。
// 复现路径：管理员在竞赛管理面对进行中/已结束的竞赛想调整起止时间，
// 详情页开始时间被禁用、保存被 422 拒绝，列表页也没有结束入口。
const { challenge: C, competition: COMP } = E2E

// 本 spec 建的竞赛统一收尾：结束兜底（非 running 422 由 helper 容忍），
// 防止中途失败残留 running 竞赛殃及后续 spec。
const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

// el-date-picker 文本输入格式：本地时区墙钟时间，与页面展示格式一致。
function localDateTime(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:00`
}

test.describe('回归：竞赛手动结束与结束后重排', () => {
  test('进行中的竞赛可从列表一键手动结束（确认后立即生效）', async ({ page, request }) => {
    test.slow() // 冷启动 vite 首屏模块编译慢（本机并行 e2e 时更甚），放宽预算防假红
    const comp = await seedRunningCompetition(request, COMP.manualEndName, [C.slug])
    openIds.push(comp.id)

    await loginAsAdmin(page)
    await page.goto('/admin/panel/competitions')
    const row = page.locator('.el-table tr', { hasText: COMP.manualEndName })
    await expect(row).toContainText('进行中', { timeout: 15_000 })
    // 修复前：列表行没有「结束」按钮（只有隐蔽的改 end_time），此处超时即红
    await row.getByRole('button', { name: '结束', exact: true }).click()
    const box = page.locator('.el-message-box')
    await expect(box).toContainText('手动结束竞赛')
    await box.getByRole('button', { name: '结束竞赛' }).click()
    // 立即生效：成功提示 + 行状态翻转为已结束 + 结束按钮随状态消失
    await expect(page.locator('.el-message', { hasText: '已结束' })).toBeVisible()
    await expect(row).toContainText('已结束')
    await expect(row.getByRole('button', { name: '结束', exact: true })).toHaveCount(0)
  })

  test('已结束的竞赛可重新配置起止时间再次开赛（回归：start_time 永久锁死）', async ({ page, request }) => {
    test.slow() // 同上：冷启动预算
    const comp = await seedRunningCompetition(request, COMP.reviveName, [C.slug])
    openIds.push(comp.id)
    // 结束动作本身由上一用例经 UI 覆盖；此处经管理 API 置为已结束，聚焦重排能力
    await endCompetition(request, comp.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${comp.id}`)
    // 详情标题含赛制 + 状态两枚 tag（竞赛分公开/邀请两档引入），按文案锚定状态。
    const statusTag = page.locator('h2 .el-tag', { hasText: '已结束' })
    await expect(statusTag).toHaveText('已结束', { timeout: 15_000 })

    // 修复前：已结束竞赛的开始时间输入框仍禁用（红）
    const startInput = page.locator('.el-form-item', { hasText: '开始时间' }).locator('input')
    const endInput = page.locator('.el-form-item', { hasText: '结束时间' }).locator('input')
    await expect(startInput).not.toBeDisabled()
    await expect(endInput).not.toBeDisabled()

    // 重排到未来窗口保存：修复前报 422 cannot change start_time；修复后回到未开始
    await startInput.fill(localDateTime(new Date(Date.now() + 2 * 3_600_000)))
    await startInput.press('Enter')
    await endInput.fill(localDateTime(new Date(Date.now() + 4 * 3_600_000)))
    await endInput.press('Enter')
    await page.getByRole('button', { name: '保存配置' }).click()
    await expect(page.locator('.el-message', { hasText: '配置已保存' })).toBeVisible()
    await expect(page.locator('h2 .el-tag', { hasText: '未开始' })).toHaveText('未开始')
  })
})
