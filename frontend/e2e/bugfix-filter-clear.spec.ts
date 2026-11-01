import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 回归：shadcn 迁移时清除下拉框发出 undefined，用户画像查询误当成 false（2026-11-02）。
// 修复前行为：清掉不活跃筛选后仍发送 active=false，活跃学生不会回到列表。
// 修复后行为：清除规范为原业务约定的空字符串，查询不再带该过滤条件。
// 复现路径：管理员进入用户管理 → 选择不活跃 → 点击清除选择 → 学生重新出现。
test('清除活跃状态筛选后恢复全部用户（回归：清空后仍按 false 筛选）', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/admin/panel/users')
  const student = page.getByRole('row', { name: new RegExp(E2E.student.username) })
  await expect(student).toBeVisible()
  await page.getByRole('combobox', { name: '活跃状态' }).click()
  await page.getByRole('option', { name: '不活跃（6 个月未登录）', exact: true }).click()
  await expect(student).toHaveCount(0)
  await page.getByRole('button', { name: '清除选择', exact: true }).click()
  await expect(student).toBeVisible()
})
