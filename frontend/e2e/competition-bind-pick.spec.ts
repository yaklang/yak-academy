import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { adminToken, endCompetition, loginAsAdmin } from './helpers'

// 竞赛「绑定 / 导入题目」对话框的「从题库勾选」模式（2026-10 新增）：
// 此前只能手填 slug 或导入 JSON，题库里有什么、slug 是否打对全靠脑记。
// 现在默认落在勾选模式：题库列表（搜索/题库过滤/分页）→ 勾选 → 一次绑定，
// 已绑定的题禁选并打「已绑定」标记。提交仍走既有 { slugs } 通道（后端不变）。
// 题库种子由 global-setup 播种（示例 CTF 题 + 教程关联题 + ACM 题目包），
// 本 spec 只建竞赛（未开始，无学生锁定风险），afterEach 结束兜底收尾。
const { challenge: C } = E2E

// global-setup 已种的三道题（不同题库 tab，覆盖过滤维度）：
// 凯撒密码（CTF，本场要绑）、SQL 注入关联题（CTF，搜索目标）、
// E2E 求和题（ACM，题库过滤目标）。
const ACM_TITLE = E2E.acm.title

const openIds: number[] = []
test.afterEach(async ({ request }) => {
  for (const id of openIds.splice(0)) await endCompetition(request, id)
})

// 管理员建一场未开始的竞赛（起止落未来窗口），返回竞赛 ID。
async function createUpcomingCompetition(request: APIRequestContext, name: string) {
  const token = await adminToken(request)
  const now = Date.now()
  const resp = await request.post(`${E2E.apiURL}/api/v1/admin/competitions`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name,
      description: 'e2e 勾选绑题',
      start_time: new Date(now + 10 * 60_000).toISOString(),
      end_time: new Date(now + 60 * 60_000).toISOString(),
    },
  })
  expect(resp.ok()).toBeTruthy()
  return (await resp.json()) as { id: number }
}

test.describe('竞赛绑定：从题库勾选', () => {
  test('勾选题库题目绑定进竞赛，已绑定题禁选并带标记', async ({ page, request }) => {
    test.slow() // vite 首屏冷编译慢（本机并行 e2e 时更甚），放宽预算防假红
    const comp = await createUpcomingCompetition(request, 'E2E 勾选绑题赛')
    openIds.push(comp.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${comp.id}`)
    await page.getByRole('tab', { name: '题目', exact: true }).click()

    // 打开对话框默认落在「从题库勾选」（el-radio-button 可访问树是 generic，
    // 无 radio role，按类名断言选中态），题库列表已加载（含种子题）
    await page.getByRole('button', { name: '绑定 / 导入题目' }).click()
    const dialog = page.locator('.el-dialog')
    await expect(dialog.locator('.el-dialog__title')).toHaveText('绑定 / 导入题目')
    await expect(dialog.locator('.el-radio-button', { hasText: '从题库勾选' })).toHaveClass(
      /is-active/,
    )
    const pickTable = dialog.locator('.el-table')
    await expect(pickTable).toContainText(C.title, { timeout: 15_000 })

    // 搜索定位（管理员视角的定位习惯）：按标题关键词过滤出目标行
    await dialog.getByPlaceholder('搜索：标题 / slug / 分类').fill('凯撒')
    await dialog.getByRole('button', { name: '搜索' }).click()
    await expect(pickTable).toContainText(C.title)
    await expect(pickTable).not.toContainText(ACM_TITLE)

    // 未勾选直接提交被拦：提示后按钮计数仍为 0
    await dialog.getByRole('button', { name: /绑定所选（0 题）/ }).click()
    await expect(page.locator('.el-message', { hasText: '请先勾选要绑定的题目' })).toBeVisible()

    // 勾选目标行提交：成功提示 + 对话框关闭
    await pickTable
      .locator('tr', { hasText: C.title })
      .locator('.el-checkbox')
      .click()
    await expect(dialog.getByText(`已勾选 1 题`)).toBeVisible()
    await dialog.getByRole('button', { name: '绑定所选（1 题）' }).click()
    await expect(page.locator('.el-message', { hasText: '绑定完成：新增 1 题' })).toBeVisible()
    await expect(dialog).toBeHidden()

    // 竞赛题目表出现刚绑的题。关闭的勾选对话框 display:none 但 DOM 还在，
    // 其表格行同样含 slug——全局 tr 定位会 strict violation，须收进题目 tab。
    const chalPanel = page.getByRole('tabpanel', { name: '题目' })
    await expect(chalPanel.locator('tr', { hasText: C.slug })).toContainText(C.title)

    // 重开对话框：该题行带「已绑定」标记且禁选（避免白跑一趟的幂等绑定）
    await page.getByRole('button', { name: '绑定 / 导入题目' }).click()
    await expect(pickTable).toContainText(C.title, { timeout: 15_000 })
    const boundRow = pickTable.locator('tr', { hasText: C.title })
    await expect(boundRow).toContainText('已绑定')
    await expect(boundRow.locator('.el-checkbox')).toHaveClass(/is-disabled/)
  })

  test('题库过滤与换模式互不影响：ACM tab 只列 ACM 题，slug 直填通道仍在', async ({
    page,
    request,
  }) => {
    test.slow() // 同上：vite 冷启动预算
    const comp = await createUpcomingCompetition(request, 'E2E 勾选绑题过滤赛')
    openIds.push(comp.id)

    await loginAsAdmin(page)
    await page.goto(`/admin/panel/competitions/${comp.id}`)
    await page.getByRole('tab', { name: '题目', exact: true }).click()
    await page.getByRole('button', { name: '绑定 / 导入题目' }).click()
    const dialog = page.locator('.el-dialog')
    const pickTable = dialog.locator('.el-table')
    await expect(pickTable).toContainText(C.title, { timeout: 15_000 })

    // 题库过滤（el-select）：只选 ACM 题库 → 列表只剩 ACM 题（CTF 种子题消失）
    await dialog.locator('.el-select').click()
    await page.locator('.el-select-dropdown__item', { hasText: 'ACM 题库' }).click()
    await expect(pickTable).toContainText(ACM_TITLE)
    await expect(pickTable).not.toContainText(C.title)

    // 切到「填写 slug」：勾选列表让位给 textarea，直填通道照常可用
    await dialog.locator('.el-radio-button', { hasText: '填写 slug' }).click()
    await expect(dialog.getByPlaceholder(/web-lfi/)).toBeVisible()
    await dialog.getByPlaceholder(/web-lfi/).fill(C.slug)
    await dialog.getByRole('button', { name: '绑定', exact: true }).click()
    await expect(page.locator('.el-message', { hasText: '绑定完成：新增 1 题' })).toBeVisible()
    await expect(
      page.getByRole('tabpanel', { name: '题目' }).locator('tr', { hasText: C.slug }),
    ).toContainText(C.title)
  })
})