import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, switchAdminChallengeBank } from './helpers'

// 题目管理页题库 tab 旅程：内置训练只读 / CTF 可管理 / ACM 独立题库，
// 与 CTF tab 的搜索（slug/标题/分类前缀）+ 筛选（分类/难度/来源）。
// 种子为 global-setup 上传的 3 道 CTF 题（凯撒/关联教程/附件）+ 1 道 ACM
// 编程题（acm-e2e-sum 求和题包）；内置集导入发生在 builtin-import.spec
// （字母序靠后），本 spec 不依赖内置行存在，只锚定顺序无关的不变量：
// 上传题永不落入内置 tab、内置来源筛选在 CTF tab 恒空。
const { challenge: C } = E2E

test.describe('题目管理：题库 tab 与搜索筛选', () => {
  test('内置训练 tab 只读：无上传/编辑/删除/可见性入口，上传种子不落入', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')

    // 默认即内置训练 tab（带「只读」角标），一键导入入口在位
    await expect(page.locator('.tab-label', { hasText: '内置训练' })).toBeVisible()
    await expect(page.getByRole('button', { name: '导入内置训练（vulinbox）' })).toBeVisible()

    // 只读：管理动作全收口（上传工具栏、行内编辑/删除、可见性开关）
    await expect(page.getByRole('button', { name: '选择 challenge.json' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '上传题目包 zip' })).toHaveCount(0)
    const table = page.locator('.chal-table')
    await expect(table.getByRole('button', { name: '编辑' })).toHaveCount(0)
    await expect(table.getByRole('button', { name: '删除' })).toHaveCount(0)
    await expect(table.locator('.el-switch')).toHaveCount(0)

    // global-setup 的 CTF 种子题不属于内置集：内置 tab 永不出现
    await expect(table.locator('tr', { hasText: C.slug })).toHaveCount(0)
    await expect(table.locator('tr', { hasText: C.attachSlug })).toHaveCount(0)
  })

  test('CTF tab：种子题可管理，搜索/筛选/来源与清除筛选联动', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, 'CTF 题目')

    const table = page.locator('.chal-table')
    await expect(table.locator('tr', { hasText: C.slug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.linkedSlug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.attachSlug })).toBeVisible()

    // 搜索（400ms 防抖后服务端前缀匹配）：标题「凯撒」只剩凯撒一行
    await page.getByPlaceholder('搜索 slug / 标题 / 分类（前缀匹配）').fill('凯撒')
    await expect(table.locator('tr', { hasText: C.linkedSlug })).toHaveCount(0)
    await expect(table.locator('tr', { hasText: C.attachSlug })).toHaveCount(0)
    await expect(table.locator('tr', { hasText: C.slug })).toBeVisible()

    // vulinbox 镜像状态卡只属于内置训练 tab：管理 tab 下不显示
    await expect(page.locator('.image-card')).toHaveCount(0)

    // 筛选条内下拉按 DOM 序：分类 / 难度 / 来源（内置 tab 无来源项）
    const selects = page.locator('.filter-bar .el-select')
    // 分类筛选：cryptography 同样收敛到凯撒
    await selects.nth(0).click()
    await page.locator('.el-select-dropdown__item', { hasText: 'cryptography' }).click()
    await expect(table.locator('tr', { hasText: C.attachSlug })).toHaveCount(0)
    await expect(table.locator('tr', { hasText: C.slug })).toBeVisible()

    // 难度筛选（与分类叠加）：expert 与种子（apprentice）不相交 → 空表
    await selects.nth(1).click()
    await page.locator('.el-select-dropdown__item', { hasText: 'expert' }).click()
    await expect(table.locator('.el-table__empty-text')).toBeVisible()

    // 清除筛选：关键字+分类+难度一次回到全量
    await page.locator('.filter-clear').click()
    await expect(table.locator('tr', { hasText: C.linkedSlug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.attachSlug })).toBeVisible()

    // 来源筛选：CTF tab 只含管理员上传题，筛「内置题集」恒空（内置集 bank=tutorial）
    await selects.nth(2).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：内置题集' }).click()
    await expect(table.locator('.el-table__empty-text')).toBeVisible()
    await selects.nth(2).click()
    await page.locator('.el-select-dropdown__item', { hasText: '来源：管理员上传' }).click()
    await expect(table.locator('tr', { hasText: C.slug })).toBeVisible()
  })

  test('ACM tab：种子编程题在列，与 CTF 题库隔离，管理入口齐备', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, 'ACM 题目')

    // global-setup 上传的 ACM 种子包在列；CTF 种子（flag 判定）不落入本 tab，
    // 上传工具栏在位（题库可管理）
    //（el-upload 包装层也带 button 角色，断言锚定内部真实按钮避免 strict 冲突）
    const table = page.locator('.chal-table')
    await expect(table.locator('tr', { hasText: E2E.acm.slug })).toBeVisible()
    await expect(table.locator('tr', { hasText: C.slug })).toHaveCount(0)
    await expect(table.locator('tr', { hasText: C.attachSlug })).toHaveCount(0)
    await expect(page.locator('button.el-button', { hasText: '选择 challenge.json' })).toBeVisible()
  })

  test('ACM tab：yak 判题引擎状态卡（OSS 下载入口，不触发真实下载）', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, 'ACM 题目')

    // 状态卡与 vulinbox 镜像卡同一形态：标签 + 装配态 tag + 路径 + OSS 下载 + 刷新
    const card = page.locator('.image-card', { hasText: 'yak 判题引擎' })
    await expect(card).toBeVisible()
    await expect(card.locator('.image-tag').first()).toBeVisible()
    await expect(card.getByRole('button', { name: '从 OSS 下载引擎' })).toBeVisible()
    await expect(card.getByRole('button', { name: '刷新' })).toBeVisible()

    // 装配态随 e2e 启动方向（ACADEMY_YAK_BIN 是否注入引擎）
    const engineConfigured = Boolean(process.env.ACADEMY_YAK_BIN)
    await expect(card.locator('.el-tag').first()).toContainText(engineConfigured ? '已装配' : '未装配')

    // 下载为后台任务（约 220MB）：只走确认弹窗文案，不点「开始下载」
    await card.getByRole('button', { name: '从 OSS 下载引擎' }).click()
    const dialog = page.locator('.el-message-box')
    await expect(dialog).toContainText('yak 引擎二进制')
    await expect(dialog).toContainText('220MB')
    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(dialog).toHaveCount(0)

    // 引擎卡只属于 ACM tab：切回 CTF tab 即收起
    await switchAdminChallengeBank(page, 'CTF 题目')
    await expect(page.locator('.image-card', { hasText: 'yak 判题引擎' })).toHaveCount(0)
  })
})
