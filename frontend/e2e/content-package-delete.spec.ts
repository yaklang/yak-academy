import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Browser } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 内容包删除旅程：管理端 UI 上传独立包 → 发布 → 学生侧可见 →
// 管理端强确认删除 → 管理列表与学生侧两侧消失。
// 与 globalSetup 的 e2e-test 种子互不影响（独立包名与 slug）。
const e2eDir = fileURLToPath(new URL('.', import.meta.url))

test.describe('内容包删除', () => {
  test('上传→发布→学生可见→删除→两侧消失', async ({ browser }: { browser: Browser }) => {
    const del = E2E.deletePkg

    // 管理端上传 zip（el-upload 的隐藏 file input）→ 出现 draft 行。
    const adminCtx = await browser.newContext()
    const admin = await adminCtx.newPage()
    await loginAsAdmin(admin)
    await admin.goto('/admin/panel/tutorials')
    await admin.setInputFiles('input[type=file]', path.join(e2eDir, 'fixtures', del.zip))
    const row = admin.getByRole('row', { name: new RegExp(del.name) })
    await expect(row).toBeVisible()
    await expect(row.locator('.el-tag', { hasText: 'draft' })).toBeVisible()

    // 发布 → 状态转 published。
    await row.getByRole('button', { name: '发布', exact: true }).click()
    await expect(row.locator('.el-tag', { hasText: 'published' })).toBeVisible()

    // 学生侧：专题入口可见（另一上下文，保持两端同时在线）。
    const stuCtx = await browser.newContext()
    const stu = await stuCtx.newPage()
    await loginAsStudent(stu)
    await expect(stu.getByRole('link', { name: del.topicTitle })).toBeVisible()

    // 管理端删除：行按钮触发，强确认弹窗（MessageBox）里再点删除。
    await row.getByRole('button', { name: '删除', exact: true }).click()
    const confirmBox = admin.locator('.el-message-box')
    await expect(confirmBox).toContainText('不可恢复')
    await confirmBox.getByRole('button', { name: '删除' }).click()
    // 成功提示携带清扫计数（发布 toast 可能仍在，按文本过滤）；管理列表行消失。
    await expect(
      admin.locator('.el-message--success').filter({ hasText: '移除' }),
    ).toContainText(`移除 1 个专题 / 1 篇教程`)
    await expect(admin.getByRole('row', { name: new RegExp(del.name) })).toHaveCount(0)

    // 学生侧刷新后专题入口消失。
    await stu.reload()
    await expect(stu.getByRole('link', { name: del.topicTitle })).toHaveCount(0)

    await adminCtx.close()
    await stuCtx.close()
  })
})
