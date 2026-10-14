import { expect, test } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsStudent } from '../helpers'

// 容器生命周期旅程（E2E_DOCKER=1 时由 docker project 执行）：
// 管理端种一道 nginx:alpine 容器题 → 学生启动容器 → endpoint 可访问 →
// 单容器约束（409 冲突弹窗）→ 续期 → 销毁。
// 依赖本机 Docker daemon；镜像 nginx:alpine 需可拉取（README 镜像加速章节）。

test.describe('容器生命周期', () => {
  test('启动→访问→冲突→续期→销毁', async ({ page, request }) => {
    // 种容器题：管理 API 上传（走 seed 之外独立 slug，避免污染主套件状态）
    const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.admin.username, password: E2E.admin.password },
    })
    const { token } = (await login.json()) as { token: string }
    const auth = { Authorization: `Bearer ${token}` }
    const slug = 'docker-nginx-journey'
    await request.post(`${E2E.apiURL}/api/v1/admin/challenges`, {
      headers: auth,
      data: {
        slug,
        title: '容器旅程：nginx',
        category: 'docker-journey',
        difficulty: 'apprentice',
        description_md: '容器生命周期 E2E 用题。',
        score: 10,
        flag_mode: 'shared',
        challenge: { type: 'static', flag: 'flag{docker_journey}' },
        env: { image: 'nginx:alpine', port: 80 },
      },
    })

    await loginAsStudent(page)
    await page.goto(`/training/${slug}`)

    // 启动容器：endpoint 出现（启动含拉镜像/起容器，放宽等待）
    await page.getByRole('button', { name: '启动容器' }).click()
    const endpoint = page.locator('.endpoint')
    await expect(endpoint).toBeVisible({ timeout: 60_000 })
    const epText = (await endpoint.textContent()) ?? ''
    expect(epText).toMatch(/^https?:\/\//)

    // endpoint 可真实访问（nginx 欢迎页）
    const resp = await request.get(epText)
    expect(resp.status()).toBe(200)
    expect(await resp.text()).toContain('Welcome to nginx')

    // 续期 1 小时（剩 2 次的第一次）
    await page.getByRole('button', { name: /续期/ }).click()
    await expect(page.locator('.el-message', { hasText: '已续期 1 小时' })).toBeVisible()

    // 单容器约束：另一道容器题启动被 409 拦（用 API 直接种第二道 + UI 点）
    const slug2 = 'docker-nginx-journey-2'
    await request.post(`${E2E.apiURL}/api/v1/admin/challenges`, {
      headers: auth,
      data: {
        slug: slug2,
        title: '容器旅程：第二题',
        category: 'docker-journey',
        difficulty: 'apprentice',
        description_md: '单容器约束验证。',
        score: 10,
        flag_mode: 'shared',
        challenge: { type: 'static', flag: 'flag{second}' },
        env: { image: 'nginx:alpine', port: 80 },
      },
    })
    await page.goto(`/training/${slug2}`)
    await page.getByRole('button', { name: '启动容器' }).click()
    const conflict = page.locator('.el-message-box', { hasText: '已有运行中的容器' })
    await expect(conflict).toBeVisible({ timeout: 30_000 })
    await conflict.getByRole('button', { name: '知道了' }).click()

    // 销毁环境：回到第一题，销毁后可重新启动
    await page.goto(`/training/${slug}`)
    await page.getByRole('button', { name: '销毁环境' }).click()
    await expect(page.getByRole('button', { name: '启动容器' })).toBeVisible({ timeout: 30_000 })
  })
})