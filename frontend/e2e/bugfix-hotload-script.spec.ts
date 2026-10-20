import { expect, test } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：yakit-mall-chain 教程的热加载脚本使用了 yak 引擎不认识的钩子与函数
// （2026-10-19 修复，随 refactor/module-case-integration）。
// 修复前行为：脚本钩子名是 hijackSave（引擎静默忽略，热加载永远不生效），函数
//   isPost()/fisContains() 引擎里不存在、replace() 直接 panic——用户照教程抄进
//   Yakit 改不了包，报告「按照教程走热加载也没有生效」。
// 修复后行为：教程页两处代码块均为引擎真实钩子 beforeRequest = func(req)，经
//   poc.GetHTTPPacketBody / str.ReplaceAll / poc.ReplaceHTTPPacketBody 改写金额，
//   引擎不支持的写法在页面上绝迹。
// 复现路径：管理员导入内置训练集 → 打开配套教程页阅读热加载脚本并照抄进 Yakit。
test.describe('回归：热加载脚本使用引擎真实钩子', () => {
  test('教程页热加载脚本是 beforeRequest 且无引擎不存在的函数（回归：热加载不生效）', async ({ page }) => {
    await loginAsAdmin(page)

    // 导入内置训练集（幂等：重复导入为更新），教程页内容随导入落库
    await page.goto('/admin/panel/challenges')
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新', exact: true }).click()
    await expect(
      page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+/ }),
    ).toBeVisible()

    // 用户路径：打开配套教程页读脚本（与既有 spec 同用普通端教程路由）
    await page.goto('/topics/yakit-module-mastery/rooms/yakit-mall-chain')
    const heading = page.locator('h2', { hasText: '业务链攻击：1 分钱买下商城商品' })
    await expect(heading).toBeVisible()

    // 两处代码块（模块介绍 + 详细步骤）都是可直接照抄的 beforeRequest 脚本
    const code = page.locator('pre code')
    await expect(code.filter({ hasText: 'beforeRequest = func(req)' })).toHaveCount(2)
    await expect(code.filter({ hasText: 'str.ReplaceAll' })).toHaveCount(2)
    await expect(code.filter({ hasText: '"TotalPrice":"0.01"' })).toHaveCount(2)

    // 引擎不认识的钩子/不存在的函数/会 panic 的内置不得再出现在页面任何位置
    for (const bad of ['hijackSave', 'isPost(', 'fisContains(', 'replace(body']) {
      await expect(page.getByText(bad)).toHaveCount(0)
    }
  })
})
