import { expect, test } from '@playwright/test'
import { E2E } from '../fixtures/consts'
import { loginAsAdmin, loginAsStudent } from '../helpers'

// 回归：容器启动失败详情混入 docker registry 代理的 emoji（🚫👀🔗）（2026-11-02 修复）。
// 修复前行为：题目引用本地不存在的镜像（vulnapp:1.0）时，docker 经镜像代理拉取
// 被拒，代理输出的 emoji 装饰符随错误原文透传，落进管理端容器日志的详情列
//（学生端 toast 是通用「服务器内部错误」，raw 详情只在管理端日志可见）。
// 修复后行为：后端剥离外部工具输出中的非 ASCII 符号区段（emoji）再落日志，
// 报错原文（镜像名 / URL / 提示语）保持原样。
// 复现路径：管理员种一道引用不存在镜像的容器题 → 学生进详情点「启动容器」→
// 失败后管理员打开容器面板看日志详情。
// 依赖本机 Docker daemon（docker project，E2E_DOCKER=1）；镜像名刻意不存在——
// 正是「拉取失败」路径本身的触发条件（无镜像代理的环境报 not found，同样断言通过）。
const EMOJI_SCAN = /[\u{1F000}-\u{1FAFF}\u{2600}-\u{27BF}\u{2B00}-\u{2BFF}\u{FE00}-\u{FE0F}\u{200D}\u{20E3}]/u

test.describe('回归：容器错误信息去 emoji', () => {
  test('启动不存在镜像的容器：日志详情无 emoji 且保留报错原文', async ({ page, request }) => {
    // 镜像拉取失败（镜像代理重试）+ 双端登录，整体放宽到 2 分钟。
    test.setTimeout(120_000)
    const login = await request.post(`${E2E.apiURL}/api/v1/auth/login`, {
      data: { username: E2E.admin.username, password: E2E.admin.password },
    })
    const { token } = (await login.json()) as { token: string }
    // 本地不存在的镜像名：docker 尝试经 registry 代理拉取 → 白名单拒绝/不存在，
    // 错误输出即含代理装饰符的路径。
    const seedTitle = '容器失败信息回归：不存在的镜像'
    await request.post(`${E2E.apiURL}/api/v1/admin/challenges`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        slug: 'docker-missing-image-emoji',
        title: seedTitle,
        category: 'docker-journey',
        difficulty: 'apprentice',
        description_md: '容器失败信息 E2E 用题。',
        score: 10,
        flag_mode: 'shared',
        challenge: { type: 'static', flag: 'flag{missing_image}' },
        env: { image: 'vulnapp:1.0', port: 80 },
      },
    })

    // 学生发起启动：docker run 失败（拉不到镜像），toast 报环境启动失败
    //——toast 出现即代表后端已回滚并落日志，可安全去管理端查详情。
    const studentPage = await page.context().newPage()
    await loginAsStudent(studentPage)
    await studentPage.goto('/training/docker-missing-image-emoji')
    await studentPage.getByRole('button', { name: '启动容器' }).click()
    await expect(
      studentPage.locator('[data-sonner-toast]', { hasText: '环境启动失败' }),
    ).toBeVisible({ timeout: 60_000 })
    await studentPage.close()

    // 管理端容器面板：日志详情列含报错原文（镜像名），且不含任何 emoji 区段字符
    //（用范围扫描而非逐个枚举，防止镜像代理更换装饰符绕过断言）。
    await loginAsAdmin(page)
    await page.goto('/admin/panel/containers')
    await expect(page.getByRole('tab', { name: '容器管理' })).toHaveAttribute('aria-selected', 'true')
    const detailCell = page
      .locator('tr.ui-data-row', { hasText: seedTitle })
      .filter({ hasText: 'create failed' })
      .first()
    await expect(detailCell).toBeVisible({ timeout: 30_000 })
    const detail = (await detailCell.textContent()) ?? ''
    expect(detail).toContain('vulnapp:1.0')
    expect(detail, `日志详情中发现未剥离的装饰字符: ${detail}`).not.toMatch(EMOJI_SCAN)

    // 收尾：删种题（无解题记录，可安全删除，避免污染 docker project 后续用例）。
    await request.delete(`${E2E.apiURL}/api/v1/admin/challenges/docker-missing-image-emoji`, {
      headers: { Authorization: `Bearer ${token}` },
    })
  })
})
