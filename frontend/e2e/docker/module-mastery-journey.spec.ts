import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsStudent } from '../helpers'

// 模块专精三连旅程（E2E_DOCKER=1 时由 docker project 执行）：
// yakit-module-mastery 专题的 3 道题各走一遍「负例不解锁 → 模块动作 → /flag 解锁 →
// UI 提交判定通过」：
//   1. fuzzer-headers：POST /vul/auth-bypass/unsafe/api/cmd 重放 ls，不带自定义头
//      X-Replay 不解锁，带 yakit-fuzzer 标记头才解锁——证明「浏览器发不出的请求」；
//   2. mitm-session：两步链（logic 模块动态登录拿 _cookie 会话 → 动态会话
//      访问 profile 渲染本人资料）；只登录不解锁、预置 fixedSessionID 诱饵
//      （商城会话固定漏洞产物，虽能渲染）不解锁，动态会话复用才解锁；
//   3. codec-pipeline：网关注入的 GET /academy/codec/verify?token=...；口令
//      codec~proof 的 base64 结果含 + 与 =，不做 URL 编码直接发会 400（+ 被解析成
//      空格），base64 → URL 编码链正确才 ok:true 并解锁。
// 依赖本机 Docker daemon；首次运行含共享镜像构建（含 flaggateway 源码），放宽超时。

// 实例端口就绪先于上游 vulinbox 完成监听：首个代理请求可能 502，轮询等它起来。
async function waitUp(request: APIRequestContext, url: string) {
  let resp = await request.get(url)
  for (let i = 0; i < 15 && resp.status() >= 500; i++) {
    await new Promise((r) => setTimeout(r, 1_000))
    resp = await request.get(url)
  }
  return resp
}

// 学生视角启动实例，返回实例地址（去尾部斜杠）
async function startInstance(page: import('@playwright/test').Page, slug: string, heading: string) {
  await loginAsStudent(page)
  await page.goto(`/training/${slug}`)
  await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  await page.getByRole('button', { name: '启动容器' }).click()
  const endpoint = page.locator('.endpoint')
  await expect(endpoint).toBeVisible({ timeout: 60_000 })
  return (((await endpoint.textContent()) ?? '').replace(/\/+$/, ''))
}

// 管理端一键导入内置训练集（幂等；Docker 可用时同步构建/更新共享镜像）
async function importBuiltin(request: APIRequestContext) {
  const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
    data: { username: E2E.admin.username, password: E2E.admin.password },
  })
  const { token } = (await login.json()) as { token: string }
  const imported = await request.post(`${E2E.apiURL}/api/v1/admin/challenges/builtin`, {
    headers: { Authorization: `Bearer ${token}` },
    timeout: 240_000,
  })
  expect(imported.ok()).toBeTruthy()
}

test.describe('模块专精三连（fuzzer-headers / mitm-session / codec-pipeline）', () => {
  // 单容器约束：用例失败会留下运行中的实例，阻塞后续用例的「启动容器」。
  // 兜底销毁（成功用例尾部已主动销毁，这里是幂等保险）。
  test.afterEach(async ({ page }) => {
    const destroy = page.getByRole('button', { name: '销毁环境' })
    if (await destroy.isVisible().catch(() => false)) {
      await destroy.click()
      await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
    }
  })

  test('fuzzer-headers：无标记头不解锁，X-Replay 重放解锁', async ({ page, request }) => {
    test.setTimeout(300_000)
    await importBuiltin(request)

    const ep = await startInstance(page, 'vulinbox-fuzzer-headers', 'Fuzzer 手搓请求：自定义头重放命令接口')

    // 1. 负例：同样的 POST 重放，但不带 X-Replay 头（浏览器也能做到的动作）——
    //    上游正常执行 ls，/flag 仍锁
    const plain = await waitUp(request, `${ep}/vul/auth-bypass/unsafe/api/cmd`)
    const plainResp = await request.post(`${ep}/vul/auth-bypass/unsafe/api/cmd`, {
      data: { command: 'ls' },
      timeout: 30_000,
    })
    expect(plain.status()).toBeLessThan(500)
    expect(plainResp.ok()).toBeTruthy()
    const locked1 = await request.get(`${ep}/flag`)
    expect(locked1.status()).toBe(403)

    // 2. 正例：带上 Fuzzer 标记头重放（X-Replay: yakit-fuzzer）
    const replayed = await request.post(`${ep}/vul/auth-bypass/unsafe/api/cmd`, {
      headers: { 'X-Replay': 'yakit-fuzzer' },
      data: { command: 'ls' },
      timeout: 30_000,
    })
    expect(replayed.ok()).toBeTruthy()

    // 3. /flag 解锁 → 实例真 flag
    const unlocked = await request.get(`${ep}/flag`)
    expect(unlocked.status()).toBe(200)
    const flag = (await unlocked.text()).trim()
    expect(flag).toMatch(/^flag\{lab-/)

    // 4. UI 提交判定通过 → 销毁
    await page.getByPlaceholder('输入 flag').fill(flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.result')).toContainText('判定通过', { timeout: 20_000 })
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })

  test('mitm-session：诱饵与半程不解锁，动态会话两步链解锁', async ({ page, request }) => {
    test.setTimeout(300_000)
    await importBuiltin(request)

    const ep = await startInstance(page, 'vulinbox-mitm-session', 'MITM 观察：截获并复用动态会话')

    // 1. 负例①：预置 fixedSessionID 诱饵访问 profile——商城会话固定漏洞的
    //    产物（预置会话，上游能渲染），但本题只认动态签发的会话，不解锁
    const decoy = await waitUp(request, `${ep}/logic/user/profile?id=3`)
    expect(decoy.status()).toBeLessThan(500)
    await request.get(`${ep}/logic/user/profile?id=3`, {
      headers: { Cookie: '_cookie=fixedSessionID' },
      timeout: 30_000,
    })
    const locked1 = await request.get(`${ep}/flag`)
    expect(locked1.status()).toBe(403)

    // 2. 负例②：logic 模块真实登录拿动态 _cookie 会话（半程）——只完成第一步不解锁
    const login = await request.post(`${ep}/logic/user/login`, {
      data: { username: 'user1', password: 'password123' },
      timeout: 30_000,
    })
    expect(login.ok()).toBeTruthy()
    const setCookie = login
      .headersArray()
      .filter((h) => h.name.toLowerCase() === 'set-cookie')
      .map((h) => h.value)
      .find((v) => v.includes('_cookie='))
    expect(setCookie).toBeTruthy()
    const session = /_cookie=([^;]+)/.exec(setCookie ?? '')?.[1]
    expect(session).toBeTruthy()
    const locked2 = await request.get(`${ep}/flag`)
    expect(locked2.status()).toBe(403)

    // 3. 正例：携带本次动态会话访问本人资料（id=3，登录响应给出），
    //    响应渲染出 user1——两步链完成
    const profile = await request.get(`${ep}/logic/user/profile?id=3`, {
      headers: { Cookie: `_cookie=${session}` },
      timeout: 30_000,
    })
    expect(profile.ok()).toBeTruthy()
    expect(await profile.text()).toContain('user1')

    // 4. /flag 解锁 → 实例真 flag
    const unlocked = await request.get(`${ep}/flag`)
    expect(unlocked.status()).toBe(200)
    const flag = (await unlocked.text()).trim()
    expect(flag).toMatch(/^flag\{lab-/)

    // 5. UI 提交判定通过 → 销毁
    await page.getByPlaceholder('输入 flag').fill(flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.result')).toContainText('判定通过', { timeout: 20_000 })
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })

  test('codec-pipeline：裸 base64 token 400，URL 编码链 ok:true 解锁', async ({ page, request }) => {
    test.setTimeout(300_000)
    await importBuiltin(request)

    const ep = await startInstance(page, 'vulinbox-codec-pipeline', 'Codec 编码链：把口令安全送到验证端点')

    // 1. 负例：codec~proof 的 base64 结果 Y29kZWN+cHJvb2Y= 原样当 token——
    //    + 被 URL 解析成空格、base64 还原不出原口令，端点拒绝（HTTP 400）
    const probe = await waitUp(request, `${ep}/academy/codec/verify?token=probe`)
    expect(probe.status()).toBe(400)
    const raw = await request.get(`${ep}/academy/codec/verify?token=Y29kZWN+cHJvb2Y=`, { timeout: 30_000 })
    expect(raw.status()).toBe(400)
    expect(await raw.text()).toContain('"ok":false')
    const locked1 = await request.get(`${ep}/flag`)
    expect(locked1.status()).toBe(403)

    // 2. 正例：base64 → URL 编码（+ 变 %2B、= 变 %3D）——编码链正确，ok:true
    const good = await request.get(`${ep}/academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D`, { timeout: 30_000 })
    expect(good.status()).toBe(200)
    expect(await good.text()).toContain('"ok":true')

    // 3. 验证通过即解锁 /flag → 实例真 flag
    const unlocked = await request.get(`${ep}/flag`)
    expect(unlocked.status()).toBe(200)
    const flag = (await unlocked.text()).trim()
    expect(flag).toMatch(/^flag\{lab-/)

    // 4. UI 提交判定通过 → 销毁
    await page.getByPlaceholder('输入 flag').fill(flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.result')).toContainText('判定通过', { timeout: 20_000 })
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })
})
