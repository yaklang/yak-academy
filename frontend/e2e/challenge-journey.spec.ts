import { expect, test, type Page } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 题目全链路旅程（本套件最重要的一条）：管理端表格 → 学生题库分组 →
// 详情提示解锁（扣分）→ 错 flag / 对 flag 判定 → 排行榜 → 徽章 → 附件下载。
// 结算语义：分值只结算一次，重复判对 score=0 不再计分。
const { challenge: C } = E2E

// 详情页提交 flag 并等到终态判定（异步判题轮询由前端 useSubmissionPoll 完成）。
async function submitFlag(page: Page, flag: string) {
  await page.getByPlaceholder('输入 flag').fill(flag)
  await page.getByRole('button', { name: '提交', exact: true }).click()
  await expect(page.locator('.result')).toBeVisible({ timeout: 20_000 })
}

test.describe('题目全链路', () => {
  test('题库按分类分组，详情可通题', async ({ page }) => {
    await loginAsStudent(page)

    // 题库列表：上传的种子题归「CTF 题目」tab（教程题目 tab 专属内置题集），
    // 按 category 分组出现
    await page.getByRole('link', { name: '题库' }).click()
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.locator('h3.cat', { hasText: C.category })).toBeVisible()
    await page.getByRole('link', { name: C.title }).click()
    await expect(page).toHaveURL(new RegExp(`/training/${C.slug}`))

    // 详情渲染描述与提示折叠
    await expect(page.getByRole('heading', { name: C.title })).toBeVisible()

    // 未解锁提示：展开面板，正文折叠，只有解锁按钮
    const hintRow = page.locator('.el-collapse-item', { hasText: C.hintTitle })
    await hintRow.locator('.el-collapse-item__header').click()
    await expect(hintRow.getByRole('button', { name: `花费 ${C.hintCost} 分解锁` })).toBeVisible()
    await expect(hintRow.locator('.hint-content')).toHaveCount(0)

    // 错 flag：判定不通过
    await submitFlag(page, 'flag{totally_wrong}')
    await expect(page.locator('.result')).toContainText('判定不通过')

    // 对 flag：通关 + 结算一次（50 分）
    await submitFlag(page, C.flag)
    await expect(page.locator('.result')).toContainText(/得分 \d+/)
    await expect(page.getByText('已通关', { exact: true })).toBeVisible()

    // 解锁提示：通关后才有积分可扣（0 分学生解锁付费提示被 422 拒绝，
    // 余额约束是产品语义）。解锁会重渲染面板导致塌缩，重新展开读正文。
    await hintRow.getByRole('button', { name: `花费 ${C.hintCost} 分解锁` }).click()
    await expect(page.locator('.el-message', { hasText: `扣除 ${C.hintCost} 分` })).toBeVisible()
    await hintRow.locator('.el-collapse-item__header').click()
    await expect(hintRow.locator('.hint-content')).toBeVisible()

    // 排行榜出现学生分数（结算 50 - 提示 5 = 45）
    await page.getByRole('link', { name: '排行榜' }).click()
    const row = page.locator('tr', { hasText: E2E.student.username })
    await expect(row).toBeVisible()
    await expect(row).toContainText('45')

    // 徽章：first_solve（初试锋芒）点亮
    await page.getByRole('link', { name: '徽章' }).click()
    const badge = page.locator('.badge', { hasText: '初试锋芒' })
    await expect(badge).toHaveClass(/earned/)

    // 重复提交正确 flag：判对但不重复计分
    await page.goto(`/training/${C.slug}`)
    await submitFlag(page, C.flag)
    await expect(page.locator('.result')).toContainText('不重复计分')
  })

  test('附件题：学生端凭 JWT 下载附件', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${C.attachSlug}`)

    // 附件区出现种子文件；下载要求登录态，验证响应体内容回读
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      page.locator('.attach-row', { hasText: C.attachFile }).getByRole('button', { name: '下载' }).click(),
    ])
    expect(download.suggestedFilename()).toBe(C.attachFile)
    const stream = await download.createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of stream) chunks.push(chunk as Buffer)
    expect(Buffer.concat(chunks).toString('utf-8').trim()).toBe(C.attachContent.trim())
  })

  test('管理端题目表格展示种子题与可见性控件', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')

    // 种子题在管理表格中可见（含关联题与附件题）
    const table = page.locator('.el-table')
    await expect(table.locator('tr', { hasText: C.slug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.linkedSlug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.attachSlug })).toBeVisible()

    // 关联教程列展示 Room 标题
    await expect(table.locator('tr', { hasText: C.linkedSlug })).toContainText(E2E.tutorial.roomTitle)
  })

  test('管理端行内编辑：回填库内完整定义，改标题保存生效', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    const row = page.locator('.el-table tr', { hasText: C.attachSlug })
    const ops = row.locator('.el-table__cell').last()

    // 操作列仅 编辑/删除
    await expect(ops.getByRole('button', { name: '编辑' })).toBeVisible()
    await expect(ops.getByRole('button', { name: '删除' })).toBeVisible()
    await expect(ops.getByRole('button')).toHaveCount(2)

    // 行内编辑：弹窗回填库内完整定义（GET /admin/challenges/{slug}）
    await ops.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog', { hasText: `编辑题目：${C.attachSlug}` })
    await expect(dialog).toBeVisible()
    const titleInput = dialog.locator('.el-form-item', { hasText: '标题' }).locator('input')
    await expect(titleInput).toHaveValue(C.attachTitle)
    await expect(dialog.getByPlaceholder('flag{answer}')).toHaveValue(C.attachFlag)

    // 附件管理并入编辑弹窗：种子附件在列表中
    await expect(dialog.locator('.el-table')).toContainText(C.attachFile)

    // 改标题保存：管理表格与学生端同时生效
    const editedTitle = `${C.attachTitle}（已编辑）`
    await titleInput.fill(editedTitle)
    await dialog.getByRole('button', { name: '确认上传' }).click()
    await expect(page.locator('.el-message', { hasText: '已导入题目' })).toBeVisible()
    await expect(page.locator('.el-table tr', { hasText: C.attachSlug })).toContainText(editedTitle)

    await loginAsStudent(page)
    await page.goto('/training')
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.getByRole('link', { name: editedTitle })).toBeVisible()

    // 还原标题（后续用例与种子常量依赖初始标题）
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    const row2 = page.locator('.el-table tr', { hasText: C.attachSlug })
    await row2.getByRole('button', { name: '编辑' }).click()
    const dialog2 = page.locator('.el-dialog', { hasText: `编辑题目：${C.attachSlug}` })
    await dialog2.locator('.el-form-item', { hasText: '标题' }).locator('input').fill(C.attachTitle)
    await dialog2.getByRole('button', { name: '确认上传' }).click()
    await expect(page.locator('.el-table tr', { hasText: C.attachSlug })).toContainText(C.attachTitle)
  })
})