import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent, studentToken } from './helpers'

// ACM 编程题旅程：源码提交判定（对齐 ICPC 模型），不走 flag；
// 详情页内置动态执行框（CodeMirror 编辑器 + 自测运行，运营态不产生提交）。
// 引擎由 ACADEMY_YAK_BIN 门控（playwright.config 注入后端）：
// - 装配（本地设 yak 可执行路径）：正确源码 AC 计分、错误输出 WA；
//   自测运行样例模式比对期望、自定义输入模式回显 stdout；
// - 未装配（CI）：提交判 internal_error、自测运行 422 友好提示。
const engineConfigured = Boolean(process.env.ACADEMY_YAK_BIN)

// CodeMirror 6 的可编辑区是 contenteditable，fill() 直接可用。
async function fillSource(page: import('@playwright/test').Page, source: string) {
  await page.locator('.cm-content').fill(source)
}

test.describe('ACM 编程题（源码判定）', () => {
  test('详情页展示样例数据卡与代码编辑器，无 flag 输入', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    // 题面与编程题标识
    await expect(page.getByRole('heading', { name: E2E.acm.title })).toBeVisible()
    await expect(page.getByText('编程题 · 5s / 128MB')).toBeVisible()

    // 样例数据卡：sample=true 的组（demo/edge）可见，隐藏组不下发
    // （h4 定位避免与提交区提示文案的「样例数据」字样撞 strict mode）
    await expect(page.getByRole('heading', { name: '样例数据' })).toBeVisible()
    for (const name of E2E.acm.samples) {
      await expect(page.locator('.sample-group', { hasText: name })).toBeVisible()
    }
    await expect(page.locator('.sample-group')).toHaveCount(E2E.acm.samples.length)
    const demo = page.locator('.sample-group', { hasText: 'demo' })
    await expect(demo).toContainText('2 3') // demo 组输入
    await expect(demo).toContainText('30') // demo 组期望输出

    // 代码编辑器（CodeMirror）存在，flag 输入不存在（ACM 不通过 flag 判定）
    const editor = page.locator('.solve .cm-editor')
    await expect(editor).toBeVisible()
    await expect(page.getByPlaceholder('输入 flag')).toHaveCount(0)

    // 暗色编辑器 + 白色代码字 + yak 语法着色（yakit 词法移植的回归锚点：
    // 底色 #212428、默认字白、内建函数橙 #e0873c、字符串绿 #98c379）
    await expect(editor).toHaveCSS('background-color', 'rgb(33, 36, 40)')
    await expect(page.locator('.solve .cm-content')).toHaveCSS('color', 'rgb(255, 255, 255)')
    await fillSource(page, 'println("hi")')
    const tokenColors = await page.locator('.solve .cm-line span').evaluateAll(
      (spans) => spans.map((s) => getComputedStyle(s).color),
    )
    expect(tokenColors).toContain('rgb(224, 135, 60)') // println 内建函数
    expect(tokenColors).toContain('rgb(152, 195, 121)') // "hi" 字符串

    // 库策略提示：题面 chips 显示白名单（fixture 配 [str, io, os]），
    // 黑名单高危库说明可见（exec/file 在默认黑名单内）
    await expect(page.locator('.lib-policy .lib-title')).toHaveText('本题可用库')
    await expect(page.locator('.lib-policy .lib-chip')).toHaveCount(E2E.acm.allowedLibs.length)
    for (const lib of E2E.acm.allowedLibs) {
      await expect(page.locator('.lib-policy .lib-chip', { hasText: lib })).toBeVisible()
    }
    await expect(page.locator('.lib-policy .lib-blocked')).toContainText('已禁用高危库')
    await expect(page.locator('.lib-policy .lib-blocked')).toContainText('exec')
    await expect(page.locator('.lib-policy .lib-blocked')).toContainText('file')

    // yak 代码补全：词首补全（内建函数）与 lib. 成员补全（标准库真实函数）；
    // 补全菜单遵循题面白名单——str. 提供成员，白名单外（codec.）/黑名单（exec.）
    // 不提供任何成员（无菜单）
    const content = page.locator('.solve .cm-content')
    const menu = page.locator('.cm-tooltip-autocomplete')
    await content.click()
    await content.press('ControlOrMeta+a')
    await content.pressSequentially('print')
    await expect(menu).toBeVisible()
    await expect(menu.getByText('println', { exact: true })).toBeVisible()
    await content.press('Escape')
    await content.press('ControlOrMeta+a')
    await content.pressSequentially('str.')
    await expect(menu.getByText('Contains', { exact: true })).toBeVisible()
    await content.press('Escape')
    await content.press('ControlOrMeta+a')
    await content.pressSequentially('codec.')
    await expect(menu).not.toBeVisible()
    await content.press('Escape')
    await content.press('ControlOrMeta+a')
    await content.pressSequentially('exec.')
    await expect(menu).not.toBeVisible()
  })

  test('提交 yak 源码：逐组对拍全部通过 → AC 计分', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.correctSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('判定通过，得分', { timeout: 30_000 })
    await expect(page.locator('.result')).toHaveClass(/el-alert--success/)
  })

  test('提交错误输出的源码 → WA 判定不通过', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.wrongSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('答案错误（WA）', { timeout: 30_000 })
    await expect(page.locator('.result')).toHaveClass(/el-alert--error/)
  })

  // 部分得分回归（e2e-bugfix）：种子题 scores.json 按 2/1/1/1 配分，
  // partialSource 仅在 partialfail 组（5+5）输出 11≠10 → WA。4/5 组过 →
  // 题面全额按权重折算 4/5 计入（error 警示未通关但计部分分），逐组行可见
  // 结论与组分。题面全额随动态衰减变化（本 spec 内 AC 用例已判对 → n=1），
  // 期望分按线性衰减公式 max(initial - 10*n, max(20% initial, 10)) 折算。
  test('部分得分制：4/5 组通过 → 按动态题分折算部分得分且题目未通关，逐组结论可见', async ({ page, request }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    // 已判对人数经学生侧题解接口取 solved 态并按衰减公式还原 n
    // （AC 用例在前：全量套件下本用例运行时 n≥1；单独跑本 spec 则 n=0）。
    const token = await studentToken(request)
    const detail = await request.get(`${E2E.apiURL}/api/v1/challenges/${E2E.acm.slug}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    const chal = (await detail.json()) as { solved: boolean; score: number }
    const n = chal.solved ? 1 : 0
    const initial = 50
    const full = Math.max(initial - 10 * n, Math.max(Math.floor(initial / 5), 10))
    const expected = Math.floor((full * 4 + 2) / 5) // (full*4+total/2)/total, total=5

    await fillSource(page, E2E.acm.partialSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText(
      `部分通过，得分 ${expected}`, { timeout: 30_000 },
    )
    await expect(page.locator('.result')).toHaveClass(/el-alert--error/)
    // 逐组结论：通过组绿色「通过」并显示组分；失败组红色 WA 且无分
    for (const name of Object.keys(E2E.acm.partialGroups)) {
      await expect(page.locator('.group-row', { hasText: name })).toBeVisible()
    }
    const failRow = page.locator('.group-row', { hasText: 'partialfail' })
    await expect(failRow).toHaveClass(/group-fail/)
    await expect(failRow).toContainText('WA')
    await expect(failRow.locator('.group-score')).toHaveCount(0)
    const demoRow = page.locator('.group-row', { hasText: 'demo' })
    await expect(demoRow).toHaveClass(/group-pass/)
    await expect(demoRow.locator('.group-score')).toContainText('2 分')

    // 题目未通关：部分通过不翻转 solved——页面保持未通关态
    // （全量套件下 AC 用例先通关属正常前置，solved 一旦翻转不回退，
    // 因此断言收敛为「通关标记与结算文案互斥且部分通过为 error 警示」）。
    await expect(page.locator('.result')).toHaveClass(/el-alert--error/)
    await expect(page.locator('.result')).not.toContainText('判定通过')
  })

  test('引擎未装配 → internal_error（平台侧失败提示，区别于代码错误）', async ({ page }) => {
    test.skip(engineConfigured, '判题引擎已装配，internal_error 路径不适用')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.correctSource)
    await page.getByRole('button', { name: '提交代码' }).click()

    await expect(page.locator('.result')).toContainText('判定服务暂不可用', { timeout: 30_000 })
  })

  // 库策略拦截（e2e-bugfix 回归锚点）：静态扫描发生在受理阶段（引擎执行之前），
  // 不依赖判题引擎装配，也不产生提交记录。种子题白名单 [str, io, os]。
  test('提交 exec 黑名单库源码 → 422 拒绝并点名违规库', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, 'exec.Command("id")')
    await page.getByRole('button', { name: '提交代码' }).click()

    const toast = page.locator('.el-message--error')
    await expect(toast).toContainText('提交失败')
    await expect(toast).toContainText('blacklisted yak lib(s): exec')
  })

  test('提交白名单外库源码（codec）→ 422 拒绝', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, 'codec.EncodeToHex("a")')
    await page.getByRole('button', { name: '提交代码' }).click()

    const toast = page.locator('.el-message--error')
    await expect(toast).toContainText('提交失败')
    await expect(toast).toContainText('outside the challenge whitelist: codec')
  })

  test('自测运行：黑名单库源码 → 受理阶段 422 拦截', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, 'file.IsDir("/etc")')
    await page.getByRole('button', { name: '运行自测' }).click()

    const toast = page.locator('.el-message--error')
    await expect(toast).toContainText('自测运行失败')
    await expect(toast).toContainText('file')
  })

  test('自测运行面板：样例/自定义输入切换，运行入口齐备', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await expect(page.getByRole('button', { name: '运行自测' })).toBeVisible()
    // 样例组与自定义输入都以 radio 按钮呈现，默认选中第一个样例
    for (const name of E2E.acm.samples) {
      await expect(page.locator('.run-bar').getByText(`样例 ${name}`)).toBeVisible()
    }
    await expect(page.locator('.run-bar .el-radio-button').first()).toHaveClass(/is-active/)
    await expect(page.getByPlaceholder('自定义输入（作为程序的标准输入，回车换行）')).toHaveCount(0)

    // 切到自定义输入出现 stdin 输入框，切回样例收起
    await page.locator('.run-bar').getByText('自定义输入').click()
    await expect(page.getByPlaceholder('自定义输入（作为程序的标准输入，回车换行）')).toBeVisible()
    await page.locator('.run-bar').getByText(`样例 ${E2E.acm.samples[0]}`).click()
    await expect(page.getByPlaceholder('自定义输入（作为程序的标准输入，回车换行）')).toHaveCount(0)
  })

  test('自测运行（样例模式）：输出与期望比对一致，不产生提交', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.correctSource)
    await page.getByRole('button', { name: '运行自测' }).click()

    // demo 组：输入 2 3 / 10 20 → 期望 5 / 30
    await expect(page.locator('.run-output')).toContainText('运行完成', { timeout: 30_000 })
    await expect(page.locator('.run-output')).toContainText('与样例期望一致')
    await expect(page.locator('.run-stdout')).toContainText('30')
    await expect(page.locator('.run-output')).toContainText(/耗时 \d+ ms/)
  })

  test('自测运行（自定义输入）：回显 stdout，不做期望比对', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.correctSource)
    await page.locator('.run-bar').getByText('自定义输入').click()
    await page.getByPlaceholder('自定义输入（作为程序的标准输入，回车换行）').fill('10 20\n2 5')
    await page.getByRole('button', { name: '运行自测' }).click()

    await expect(page.locator('.run-output')).toContainText('运行完成', { timeout: 30_000 })
    await expect(page.locator('.run-stdout')).toContainText('30')
    await expect(page.locator('.run-stdout')).toContainText('7')
    // 自定义模式 matches 恒 null：不出现样例一致性标签
    await expect(page.locator('.run-output')).not.toContainText('与样例期望')
  })

  test('自测运行：错误输出的源码 → 样例比对不一致提示', async ({ page }) => {
    test.skip(!engineConfigured, '判题引擎未装配（ACADEMY_YAK_BIN 未设置）')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.wrongSource)
    await page.getByRole('button', { name: '运行自测' }).click()

    await expect(page.locator('.run-output')).toContainText('运行完成', { timeout: 30_000 })
    await expect(page.locator('.run-output')).toContainText('与样例期望不一致')
    await expect(page.locator('.run-stdout')).toContainText('42')
  })

  test('引擎未装配 → 自测运行 422 友好提示', async ({ page }) => {
    test.skip(engineConfigured, '判题引擎已装配，422 路径不适用')
    await loginAsStudent(page)
    await page.goto(`/training/${E2E.acm.slug}`)

    await fillSource(page, E2E.acm.correctSource)
    await page.getByRole('button', { name: '运行自测' }).click()

    await expect(page.locator('.el-message')).toContainText('自测运行不可用')
  })
})
