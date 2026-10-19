import { expect, test } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsStudent } from '../helpers'

// 回归：服务器部署后平台展示的实例 endpoint 仍是 http://127.0.0.1:<端口>，
// 学员浏览器打开指向的是自己的机器，靶机不可达（PR #23，2026-10-19 修复）。
// 修复前行为：DockerDriver 端口只发布宿主 127.0.0.1 且 endpoint 硬编码回环
// 地址——本地开发正常，部署到服务器后远程学员无法访问自己启动的容器。
// 修复后行为：配置 ACADEMY_INSTANCE_PUBLIC_HOST（e2e 后端为 localhost）时
// 端口发布全网卡，endpoint 使用配置的主机名，按展示地址可真实访问靶机。
// 复现路径：学生端打开容器题详情 → 启动容器 → 页面展示 endpoint（部署在
// 远程服务器时该地址为 127.0.0.1，学员浏览器不可达）→ 按 endpoint 访问。
test.describe('回归：实例 endpoint 使用配置的对外主机名', () => {
  test('endpoint 为配置主机名且可真实访问（回归：硬编码 127.0.0.1 不可达）', async ({
    page,
    request,
  }) => {
    // 种容器题：管理 API 上传（独立 slug + category，不污染主套件与既有 docker 旅程）
    const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.admin.username, password: E2E.admin.password },
    })
    const { token } = (await login.json()) as { token: string }
    const auth = { Authorization: `Bearer ${token}` }
    const slug = 'docker-endpoint-public-host'
    await request.post(`${E2E.apiURL}/api/v1/admin/challenges`, {
      headers: auth,
      data: {
        slug,
        title: 'endpoint 对外主机名回归',
        category: 'docker-endpoint',
        difficulty: 'apprentice',
        description_md: '实例 endpoint 对外主机名回归用题。',
        score: 10,
        flag_mode: 'shared',
        challenge: { type: 'static', flag: 'flag{endpoint_host}' },
        env: { image: 'nginx:alpine', port: 80 },
      },
    })

    await loginAsStudent(page)
    await page.goto(`/training/${slug}`)
    await page.getByRole('button', { name: '启动容器' }).click()

    // endpoint 必须用配置的对外主机名（localhost），而非硬编码的 127.0.0.1
    const endpoint = page.locator('.endpoint')
    await expect(endpoint).toBeVisible({ timeout: 60_000 })
    await expect(endpoint).toHaveText(/^http:\/\/localhost:\d+$/)

    // 穿透真实可用：按展示地址访问靶机返回 nginx 欢迎页
    const epText = (await endpoint.textContent()) ?? ''
    const resp = await request.get(epText)
    expect(resp.status()).toBe(200)
    expect(await resp.text()).toContain('Welcome to nginx')

    // 收尾：销毁实例，不留运行容器影响后续用例
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })
})
