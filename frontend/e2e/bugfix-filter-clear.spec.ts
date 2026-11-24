import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 回归：用户画像过滤清空后必须回到「不带该条件」，而不是误发 active=false。
// 修复前行为：清掉不活跃筛选后仍发送 active=false，活跃学生不会回到列表。
// 修复后行为：选择「全部状态」后查询不再带该过滤条件，学生重新出现。
// 复现路径：管理员进入用户管理 → 选择不活跃 → 改回全部状态 → 学生重新出现。
test('清除活跃状态筛选后恢复全部用户（回归：清空后仍按 false 筛选）', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/admin/panel/users')
  const student = page.getByRole('row', { name: new RegExp(E2E.student.username) })
  await expect(student).toBeVisible()
  const active = page.getByRole('combobox', { name: '活跃状态' })
  await active.selectOption({ label: '不活跃（6 个月未登录）' })
  await expect(student).toHaveCount(0)
  await active.selectOption({ label: '全部状态' })
  await expect(student).toBeVisible()
})