import { expect, test, type Page } from '@playwright/test'
import { loginAsAdmin } from './helpers'

// 回归：GET /flag 读取端点无校验，浏览器零攻击直读实例真 flag（2026-10-19 修复）。
// 修复前行为：flaggateway 的 /flag handler 无条件返回 flag；cat flag.txt 注入在
//   全部 11 个实例可用（任意题零攻击可 cat 出真 flag）；SSRF 两课宣称的
//   "Host 含 127.0.0.1 校验"在 /flag 上并不存在；8 篇攻击课程把"直接 GET /flag"
//   当最终步骤教学，题面还宣传"靶机文件系统内也有 /flag.txt"。
// 修复后行为：/flag 带按题门禁（平台注入 CHALLENGE_SLUG 标识实例归属）：
//   8 道攻击题完成攻击前 403、攻击成功后自动解锁；SSRF 两题只放行靶机内部
//   自请求（连接来源回环判定，伪造 Host 无效）；cat 注入仅 authz 题注册；
//   课程与题面文案同步门禁事实。
// 复现路径：学员打开任一 vulinbox 题实例，浏览器直接 GET 实例地址 /flag 即得
//   真 flag；或对任意实例 POST /vul/auth-bypass/unsafe/api/cmd 用 cat flag.txt。
// 说明：网关行为由 flaggateway 单测与 e2e/docker/flag-gate-journey.spec.ts
//   （真实容器 403 → UNION 攻击 → 解锁 → 提交通过）覆盖；本 spec 锚定学员
//   实际阅读的课程与题面文案。

async function importBuiltin(page: Page) {
  await page.goto('/admin/panel/challenges')
  await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
  await page.getByRole('button', { name: '导入/更新' }).click()
  await expect(
    page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+：/ }),
  ).toBeVisible({ timeout: 60_000 })
}

test.describe('回归：/flag 读取门禁与课程文案同步', () => {
  test('课程讲清 /flag 门禁：攻击题解锁制、SSRF 题连接来源校验（回归：误导文案）', async ({ page }) => {
    await loginAsAdmin(page)
    await importBuiltin(page)

    // union-dump（用户撞上本题的课程）：读 flag 步骤讲门禁与解锁，不再宣传 /flag.txt
    await page.goto('/topics/yakit-sql-injection/rooms/yakit-union-dump')
    const union = page.locator('.lesson')
    await expect(union).toContainText('带门禁')
    await expect(union).toContainText('403')
    await expect(union).toContainText('自动解锁')
    await expect(union).not.toContainText('存在靶机文件系统的')

    // ssrf-first：宣称的是连接来源校验，不实的"校验与 /ssrf/flag 相同（Host）"声明删除
    await page.goto('/topics/yakit-server-side/rooms/yakit-ssrf-first')
    const ssrf = page.locator('.lesson')
    await expect(ssrf).toContainText('连接来源')
    await expect(ssrf).not.toContainText('校验与 /ssrf/flag 相同')

    // fuzzer-first（authz 题）：读取路径是命令模拟接口 cat flag.txt，不经 GET /flag
    await page.goto('/topics/yakit-fundamentals/rooms/yakit-fuzzer-first')
    await expect(page.locator('.lesson')).toContainText('cat flag.txt')
  })

  test('题面「关于 flag」段按题写门禁事实（回归：零攻击直读宣传）', async ({ page }) => {
    await loginAsAdmin(page)
    await importBuiltin(page)

    // 解锁类（sqli-union，用户撞上的题）：完成攻击前 403、成功后自动解锁
    await page.goto('/training/vulinbox-sqli-union')
    const sqli = page.locator('.description')
    await expect(sqli).toContainText('完成上述攻击动作前直接访问会得到 403')
    await expect(sqli).toContainText('攻击成功后端点自动解锁')
    await expect(sqli).not.toContainText('靶机文件系统内也有')

    // SSRF 类（ssrf-basic）：只接受靶机内部自请求（非 Host 头），SSRF 一跳是读取方式
    await page.goto('/training/vulinbox-ssrf-basic')
    const ssrfDesc = page.locator('.description')
    await expect(ssrfDesc).toContainText('只接受靶机内部自请求')
    await expect(ssrfDesc).toContainText('不是 Host 头')
    await expect(ssrfDesc).not.toContainText('靶机文件系统内也有')

    // authz 类（authz-cmd）：正路是命令模拟接口 cat flag.txt
    await page.goto('/training/vulinbox-authz-cmd')
    const authz = page.locator('.description')
    await expect(authz).toContainText('读取路径就是上面的命令模拟接口')
    await expect(authz).not.toContainText('靶机文件系统内也有 /flag.txt')
  })
})
