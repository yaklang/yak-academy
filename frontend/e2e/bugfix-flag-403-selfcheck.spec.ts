import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：mall-chain 教程缺 /flag 403 排查信息（2026-10-20 补充，随 refactor/module-case-integration）。
// 修复前行为：/flag 的 403 文案不指明缺哪个信号，教程也没有自查表——用户实操
//   四步漏了 replay（codec 已 200），flag 仍 403，无从定位缺哪步。
// 修复后行为：教程第 6 步附「卡在 403？逐信号自查」：四信号判定特征表、codec
//   响应两态文案的含义、常见卡点（多件商品静默不改 / 头加错请求 / 实例重启
//   信号清零）与「最后一步落账瞬间解锁」。
// 复现路径：学员做题卡在 /flag 403 → 展开教程详细步骤找排查办法。
test.describe('回归：/flag 403 排查信息', () => {
  test('教程第 6 步含逐信号自查表与 codec 两态文案说明（回归：403 无从排查）', async ({ page }) => {
    await loginAsAdmin(page)

    // 导入内置训练集（幂等），教程内容随导入落库
    await page.goto('/admin/panel/challenges')
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新', exact: true }).click()
    await expect(
      page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+/ }),
    ).toBeVisible({ timeout: 60_000 })

    // 用户路径：打开配套教程页，展开详细步骤找答案
    await page.goto('/topics/yakit-module-mastery/rooms/yakit-mall-chain')
    await page.getByText('详细步骤（点击展开').click()
    const lesson = page.locator('.lesson')
    await expect(lesson).toContainText('逐信号自查')
    await expect(lesson).toContainText('业务链信号齐备后')
    await expect(lesson).toContainText('四步信号齐备，')
    await expect(lesson).toContainText('静默不改')
    await expect(lesson).toContainText('全部清零')
  })
})
