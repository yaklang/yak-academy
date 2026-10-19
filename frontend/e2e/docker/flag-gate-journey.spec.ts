import { expect, test } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsStudent } from '../helpers'

// /flag 门禁端到端旅程（E2E_DOCKER=1 时由 docker project 执行）：
// 一键导入内置训练集（含共享镜像构建）→ 学生启动 vulinbox-sqli-union 实例 →
// 零攻击直连 GET /flag 得 403（未解锁）→ 无注入的正常查询不解锁 →
// 教程的 UNION 攻击打中 → GET /flag 解锁放行 → UI 提交真 flag 判定通过。
// 回归（2026-10-19 修复）：修复前 GET /flag 无任何校验，浏览器零攻击直读真 flag；
//   cat flag.txt 注入也在所有实例可用。
// 依赖本机 Docker daemon；首次运行含镜像构建（golang 构建阶段较慢），整体放宽超时。

test.describe('flag 门禁旅程（vulinbox-sqli-union）', () => {
  test('零攻击 403 → UNION 攻击解锁 → 提交判定通过', async ({ page, request }) => {
    test.setTimeout(300_000)

    // 管理端一键导入内置训练集（幂等；Docker 可用时同步构建/更新共享镜像）
    const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.admin.username, password: E2E.admin.password },
    })
    const { token } = (await login.json()) as { token: string }
    const auth = { Authorization: `Bearer ${token}` }
    const imported = await request.post(`${E2E.apiURL}/api/v1/admin/challenges/builtin`, {
      headers: auth,
      timeout: 240_000,
    })
    expect(imported.ok()).toBeTruthy()

    // 学生视角：启动实例（镜像已随导入构建，容器就绪应在秒级）
    await loginAsStudent(page)
    await page.goto('/training/vulinbox-sqli-union')
    await expect(
      page.getByRole('heading', { name: 'SQL 注入：UNION 拖库取出 root 密码' }),
    ).toBeVisible()
    await page.getByRole('button', { name: '启动容器' }).click()
    const endpoint = page.locator('.endpoint')
    await expect(endpoint).toBeVisible({ timeout: 60_000 })
    const ep = ((await endpoint.textContent()) ?? '').replace(/\/+$/, '')

    // 1. 零攻击直读被拒：403 + 未解锁文案（修复前这里 200 直接返回真 flag）
    const before = await request.get(`${ep}/flag`)
    expect(before.status()).toBe(403)
    expect(await before.text()).toContain('未解锁')

    // 2. 无注入的正常查询不解锁（?id=2 上游本就返回 root 行，那不是攻击）。
    //    实例端口就绪先于上游 vulinbox 完成监听：首个代理请求可能 502，轮询等它起来。
    let plain = await request.get(`${ep}/user/id?id=2`)
    for (let i = 0; i < 15 && !plain.ok(); i++) {
      await page.waitForTimeout(1_000)
      plain = await request.get(`${ep}/user/id?id=2`)
    }
    expect(plain.ok()).toBeTruthy()
    expect(await plain.text()).toContain('root')
    const still = await request.get(`${ep}/flag`)
    expect(still.status()).toBe(403)

    // 3. 教程第 4 步的 UNION 攻击（9 列对齐，读出 root 行的密码列）
    const attack = await request.get(
      `${ep}/user/id?id=-1%20UNION%20SELECT%20id,created_at,updated_at,deleted_at,username,password,age,role,remake%20FROM%20vulin_users%20WHERE%20username%3D%27root%27`,
    )
    expect(attack.ok()).toBeTruthy()
    expect(await attack.text()).toContain('p@ssword')

    // 4. 攻击成功后端点自动解锁：直接 GET /flag 拿到本实例真 flag
    const unlocked = await request.get(`${ep}/flag`)
    expect(unlocked.status()).toBe(200)
    const flag = (await unlocked.text()).trim()
    expect(flag).toMatch(/^flag\{lab-/)

    // 5. 学生真实提交路径：答案框提交真 flag 判定通过
    await page.getByPlaceholder('输入 flag').fill(flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.result')).toContainText('判定通过', { timeout: 20_000 })

    // 收尾：销毁实例（单容器约束，不留运行容器影响后续用例）
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })
})
