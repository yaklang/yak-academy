import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent } from './helpers'

// 内置训练（vulinbox）一键导入旅程：管理端确认弹窗 → 导入 → 幂等重导入
// （新增 0 / 全量更新）→ 管理表格出现 vulinbox 题 → 学生题库「教程题目」tab
// 专属内置题集（CTF tab 只见管理员上传题、ACM tab 只见 ACM 种子编程题）
// + 「是否内置」过滤。
// 默认 e2e 后端为 noop 驱动，恰好覆盖 image_built=false 提示路径；
// 断言只锚定两种驱动下都成立的汇总文案与表格/列表行，不区分镜像是否构建。
test.describe('内置训练一键导入', () => {
  test('管理端一键导入 vulinbox 题集，重复导入即更新，学生端题库可见', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')

    // vulinbox 镜像状态卡：默认套件为 noop 驱动（未启用提示 + 构建按钮禁用）；
    // E2E_DOCKER=1 时后端为 docker 驱动，卡片展示 Docker/镜像就绪状态，
    // noop 专属断言只在默认套件生效。
    const imageCard = page.locator('.image-card')
    await expect(imageCard).toBeVisible()
    if (process.env.E2E_DOCKER !== '1') {
      await expect(imageCard).toContainText('驱动：未启用（noop，环境启动 501）')
      await expect(imageCard).toContainText('镜像未构建（需 Docker）')
      await expect(imageCard).toContainText('yak-academy/vulinbox:latest')
      await expect(imageCard.getByRole('button', { name: '从 OSS 构建镜像' })).toBeDisabled()
    }

    // 首次导入：确认弹窗 → 汇总提示（全新库为新增 12 + 教程 12；KEEP_STATE 复跑可能全量更新，故只锚定句式）
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新' }).click()
    await expect(page.locator('.el-message', { hasText: /内置训练 v\d+\.\d+\.\d+：新增 \d+ 题、更新 \d+ 题、教程 \d+ 篇/ })).toBeVisible()

    // 管理表格出现内置题（覆盖 regex 判定题与 static 问答题各抽一）
    const table = page.locator('.el-table')
    await expect(table.locator('tr', { hasText: 'vulinbox-ssrf-basic' })).toBeVisible()
    await expect(table.locator('tr', { hasText: 'vulinbox-jwt-none' })).toBeVisible()

    // 重复导入 = 更新：本次运行首轮已全量落库，第二轮必然新增 0、更新 12、教程 12 篇（upsert 不重复）
    await page.getByRole('button', { name: '导入内置训练（vulinbox）' }).click()
    await page.getByRole('button', { name: '导入/更新' }).click()
    await expect(page.locator('.el-message', { hasText: '新增 0 题、更新 12 题、教程 12 篇' })).toBeVisible()

    // 学生题库：「教程题目」tab 专属内置题集，vulinbox 分类分组可见
    await loginAsStudent(page)
    await page.getByRole('link', { name: '题库' }).click()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'jwt' })).toBeVisible()

    // CTF tab 只见管理员上传的种子题（凯撒密码），内置题集不混入；
    // ACM tab 只见 global-setup 上传的 ACM 种子编程题（源码判定题）
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.locator('.row', { hasText: '凯撒密码：找回偏移量' })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toHaveCount(0)
    await page.getByRole('tab', { name: 'ACM 题目' }).click()
    await expect(page.locator('.row', { hasText: E2E.acm.title })).toBeVisible()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toHaveCount(0)

    // 「是否内置」过滤（服务端）：教程题目 tab 全是内置（筛上传为空），
    // CTF tab 全是上传（筛内置为空）——两个 tab 的题集归属互不稀释。
    await page.getByRole('tab', { name: '教程题目' }).click()
    await expect(page.locator('h3.cat', { hasText: 'ssrf' })).toBeVisible()
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：管理员上传' }).click()
    await expect(page.locator('.row')).toHaveCount(0)
    // 过滤条件跨 tab 保留：CTF tab 仍按「管理员上传」筛出种子题
    await page.getByRole('tab', { name: 'CTF 题目' }).click()
    await expect(page.locator('.row', { hasText: '凯撒密码：找回偏移量' })).toBeVisible()
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：内置题集' }).click()
    await expect(page.locator('.row')).toHaveCount(0)

    // 题目可进详情（教程题目 tab 恢复「全部」后进入内置题）
    await page.getByRole('tab', { name: '教程题目' }).click()
    await page.locator('.filters .el-select').nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：全部' }).click()
    await page.getByRole('link', { name: 'SSRF 基础：让服务器替你访问内网接口' }).click()
    await expect(page).toHaveURL(/\/training\/vulinbox-ssrf-basic$/)
    await expect(
      page.getByRole('heading', { name: 'SSRF 基础：让服务器替你访问内网接口' }),
    ).toBeVisible()

    // 配套教程（一教程对一题）：题目详情反向入口 → Room 详情正文与「前往题目」闭环
    await page.getByRole('link', { name: /配套教程/ }).click()
    await expect(page).toHaveURL(/\/topics\/yakit-server-side\/rooms\/yakit-ssrf-first$/)
    await expect(page.locator('.lesson')).toContainText('Web Fuzzer')
    await page.getByRole('link', { name: /前往配套题目练习/ }).click()
    await expect(page).toHaveURL(/\/training\/vulinbox-ssrf-basic$/)

    // 教程首页出现内置专题（与 global-setup 上传的种子教程包并存）
    await page.getByRole('link', { name: '教程', exact: true }).click()
    await expect(page.locator('.topic-link', { hasText: 'Yakit 上手与流量操控' })).toBeVisible()
    await expect(page.locator('.topic-link', { hasText: 'SQL 注入实战' })).toBeVisible()
    await page.locator('.topic-link', { hasText: 'Yakit 上手与流量操控' }).click()
    await expect(page.locator('.room-link', { hasText: 'Web Fuzzer 第一个请求' })).toBeVisible()
  })
})
