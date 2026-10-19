import { expect, test, type Page } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：SSRF 课程把上游 /ssrf/flag 的随机 uuid 假 flag 误描述为本题答案（2026-10-19 修复）。
// 修复前行为：课程摘要写"/ssrf/flag 响应原文里就是本实例 flag"（ssrf-first）与
//   "拿到哪个交哪个"（ssrf-json），学员按教程拿到上游演示接口的随机 uuid 后直接提交，
//   必然判定失败；且该假 flag 形态（flag{uuid}）与实例真 flag（flag{lab-...}）肉眼难辨。
// 修复后行为：flaggateway 把 /ssrf/flag 响应里的随机 uuid 改写为固定假 flag{test}（一眼假），
//   两篇 SSRF 课程（yakit-ssrf-first / yakit-ssrf-json）明确标注演示值不是答案、
//   真答案在 GET /flag；判定只认实例真 flag，假 flag 无法通过。
// 复现路径：学员在教程页阅读 SSRF 课程 → 按详细步骤演示请求 /ssrf/flag →
//   把拿到的"flag"提交到题目答案框。
// 说明：镜像内网关的响应改写由 flaggateway 单测覆盖（默认套件无 Docker 不起容器）；
//   本 spec 锚定用户实际阅读的课程文案与题目判定对假 flag 的拒绝。

// 详情页提交 flag 并等到终态判定（异步判题轮询由前端 useSubmissionPoll 完成）。
async function submitFlag(page: Page, flag: string) {
  await page.getByPlaceholder('输入 flag').fill(flag)
  await page.getByRole('button', { name: '提交', exact: true }).click()
  await expect(page.locator('.result')).toBeVisible({ timeout: 20_000 })
}

test.describe('回归：SSRF 课程演示假 flag 一眼可辨', () => {
  test('两篇 SSRF 课程标注 flag{test} 为演示假值并指向 /flag 真答案（回归：误导文案）', async ({ page }) => {
    await loginAsAdmin(page)

    // 一键导入内置训练集（embed 内容随本次后端二进制更新，导入即最新课程文案）
    await page.goto('/admin/panel/challenges')
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新' }).click()
    await expect(
      page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+：新增 \d+ 题、更新 \d+ 题、教程 \d+ 篇/ }),
    ).toBeVisible()

    // 第一课（yakit-ssrf-first）：详细步骤展示固定假 flag 并声明不是答案
    await page.goto('/topics/yakit-server-side/rooms/yakit-ssrf-first')
    const lesson1 = page.locator('.lesson')
    await expect(lesson1).toContainText('flag{test}')
    await expect(lesson1).toContainText('固定假 flag')
    await expect(lesson1).toContainText('不是本题要提交的答案')
    await expect(lesson1).toContainText('本题 flag 要读靶机根路径的 /flag')

    // 第二课（yakit-ssrf-json）：摘要不再写"拿到哪个交哪个"，详细步骤同步标注
    await page.goto('/topics/yakit-server-side/rooms/yakit-ssrf-json')
    const lesson2 = page.locator('.lesson')
    await expect(lesson2).toContainText('flag{test}')
    await expect(lesson2).toContainText('不是本题要提交的答案')
    await expect(lesson2).not.toContainText('拿到哪个交哪个')
  })

  test('提交上游演示假 flag 判定不通过（回归：uuid 假 flag 误判风险）', async ({ page }) => {
    // 学员真实路径：题目详情页答案框提交（vulinbox-ssrf-basic 为 static 实例判定，
    // 假 flag{test} 与实例真 flag flag{lab-...} 必然不匹配）
    await loginAsAdmin(page)
    await page.goto('/training/vulinbox-ssrf-basic')
    await expect(page.getByRole('heading', { name: 'SSRF 基础：让服务器替你访问内网接口' })).toBeVisible()

    await submitFlag(page, 'flag{test}')
    await expect(page.locator('.result')).toContainText('判定不通过')
  })
})