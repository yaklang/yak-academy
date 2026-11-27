import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin, loginAsStudent, switchAdminChallengeBank } from './helpers'

// 基础题（bank=basic）旅程：管理端 tab/编辑回填/JSON 导入 → 学员端
// 单选/多选（部分分）/判断/填空作答与通关。种子在 global-setup 上传。
const { basic: B } = E2E

test.describe('基础题（bank=basic）', () => {
  test('管理端：基础题 tab、编辑回填、JSON 导入与判定三选一守卫', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/panel/challenges')
    await switchAdminChallengeBank(page, '基础题')

    const table = page.locator('.chal-table')
    for (const slug of [B.single.slug, B.multiple.slug, B.truefalse.slug, B.fill.slug]) {
      await expect(table.locator('tr', { hasText: slug })).toBeVisible()
    }
    // 题库隔离：CTF 种子不落入基础题 tab；vulinbox 镜像卡只属于内置训练 tab
    await expect(table.locator('tr', { hasText: E2E.challenge.slug })).toHaveCount(0)
    await expect(page.locator('.image-card')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '选择 challenge.json' })).toBeVisible()

    // 行内编辑回填：单选种子勾选 B，选项文本可见
    await table.locator('tr', { hasText: B.single.slug }).getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('[data-slot=dialog-content]', { hasText: `编辑题目：${B.single.slug}` })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('radio', { name: '将 B 设为正确答案' })).toBeChecked()
    await expect(dialog.getByPlaceholder('选项文本').nth(1)).toHaveValue(B.single.rightText)
    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(dialog).toHaveCount(0)

    // 选择 challenge.json 导入一道新基础题（question 段自动切换判定方式并归 basic）
    const imported = {
      slug: B.importedSlug,
      title: 'E2E 导入基础题',
      category: 'web-basics',
      difficulty: 'apprentice',
      description_md: '导入基础题守卫与落库旅程。',
      score: 10,
      bank: 'basic',
      question: { type: 'truefalse', answer: true, explanation: '导入解析。' },
    }
    await page.locator('input[accept*="json"]').setInputFiles({
      name: 'basic-import.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(imported)),
    })
    const importDialog = page.locator('[data-slot=dialog-content]', {
      hasText: '编辑题目（basic-import.json）',
    })
    await expect(importDialog).toBeVisible()
    await expect(importDialog.locator('.form-errors')).toHaveCount(0)
    await importDialog.getByRole('button', { name: '确认上传' }).click()
    await expect(page.locator('[data-sonner-toast]', { hasText: '已导入题目' })).toBeVisible()
    await expect(table.locator('tr', { hasText: B.importedSlug })).toBeVisible()

    // 三选一守卫：question 与 challenge 同时出现 → 前端直接拦截，不打开编辑弹窗
    const conflict = {
      ...imported,
      slug: 'basic-e2e-conflict',
      challenge: { type: 'static', flag: 'flag{x}' },
    }
    await page.locator('input[accept*="json"]').setInputFiles({
      name: 'basic-conflict.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(conflict)),
    })
    await expect(page.locator('[data-sonner-toast]', { hasText: '三选一' })).toBeVisible()
    await expect(page.locator('[data-slot=dialog-content]')).toHaveCount(0)
  })

  test('学员端：四种题型作答、多选部分分与通关', async ({ page }) => {
    await loginAsStudent(page)

    // 题库 tab：基础题独立成 tab，列表出现种子题
    await page.goto('/training')
    await page.getByRole('tab', { name: '基础题' }).click()
    await expect(page.locator('.challenge-title', { hasText: B.single.title })).toBeVisible()

    // 单选：错 → 判定不通过；对 → 通关并下发解析
    await page.goto(`/training/${B.single.slug}`)
    await expect(page.getByText('基础题 · 单选')).toBeVisible()
    await page.locator('.question-options label', { hasText: B.single.wrongText }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定不通过', { timeout: 20_000 })
    await page.locator('.question-options label', { hasText: B.single.rightText }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定通过', { timeout: 20_000 })
    await expect(page.getByText('已通关', { exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: '解析' })).toBeVisible()

    // 多选：只选 A → 部分正确 10 分且未通关；补选 HEAD → 通关
    await page.goto(`/training/${B.multiple.slug}`)
    await expect(page.getByText('基础题 · 多选')).toBeVisible()
    await page.locator('.question-options label', { hasText: 'GET' }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText(
      `部分正确，得分 ${B.multiple.partialScore}`,
      { timeout: 20_000 },
    )
    await expect(page.locator('.result-notice')).toHaveAttribute('data-tone', 'warning')
    await expect(page.getByText('已通关', { exact: true })).toHaveCount(0)
    await page.locator('.question-options label', { hasText: 'HEAD' }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定通过', { timeout: 20_000 })
    await expect(page.getByText('已通关', { exact: true })).toBeVisible()

    // 已通关后重复提交全对答案：判对但不重复计分
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('不重复计分', { timeout: 20_000 })

    // 判断：错误 → 判定不通过；正确 → 通关
    await page.goto(`/training/${B.truefalse.slug}`)
    await expect(page.getByText('基础题 · 判断')).toBeVisible()
    await page.locator('.question-options label', { hasText: '错误' }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定不通过', { timeout: 20_000 })
    await page.locator('.question-options label', { hasText: '正确' }).click()
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定通过', { timeout: 20_000 })
    await expect(page.getByText('已通关', { exact: true })).toBeVisible()

    // 填空：大小写与首尾空白按题目匹配策略归一 → 通关
    await page.goto(`/training/${B.fill.slug}`)
    await expect(page.getByText('基础题 · 填空')).toBeVisible()
    await page.getByPlaceholder('输入答案').fill(B.fill.submitted)
    await page.getByRole('button', { name: '提交答案' }).click()
    await expect(page.locator('.result-notice')).toContainText('判定通过', { timeout: 20_000 })
    await expect(page.getByText('已通关', { exact: true })).toBeVisible()
  })
})
