import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 教程内容旅程：globalSetup 已上传并发布 e2e-test 包；这里验证学生侧
// Topic 列表 → Topic 详情（Room 列表）→ Room 详情（文字教程正文）。
// 管理端的上传→发布按钮链路在 UI 上传 zip 会产生新 draft 包（与 seed 冲突），
// 由 challenge-journey 的管理端表格断言间接覆盖管理视图，这里专注学生阅读链路。
test.describe('教程旅程', () => {
  test('学生从 Topic 列表一路读到 Room 正文', async ({ page }) => {
    await loginAsStudent(page)

    // Topic 列表出现已发布主题卡片
    await page.getByRole('link', { name: E2E.tutorial.topicTitle }).click()
    await expect(page).toHaveURL(new RegExp(`/topics/${E2E.tutorial.topicSlug}`))

    // Topic 详情给 Room 入口
    await page.getByRole('link', { name: E2E.tutorial.roomTitle }).click()
    await expect(page).toHaveURL(
      new RegExp(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`),
    )
    // Room 正文渲染出 markdown 内容（非空标题 + 正文容器）
    await expect(page.getByRole('heading', { name: E2E.tutorial.roomTitle })).toBeVisible()
    await expect(page.locator('.lesson')).not.toBeEmpty()
  })

  test('Room 关联题目入口可达题目详情（教题双向入口）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`)
    // globalSetup 种了 tutorial_slug 指向本 Room 的关联题
    const related = page.getByRole('link', { name: /前往配套题目练习/ })
    await expect(related).toBeVisible()
    await related.click()
    await expect(page).toHaveURL(new RegExp(`/training/${E2E.challenge.linkedSlug}`))
    await expect(page.getByRole('heading', { name: E2E.challenge.linkedTitle })).toBeVisible()
    // 反向入口：题目详情给出「配套教程」链接回到 Room
    const back = page.getByRole('link', { name: /配套教程/ })
    await back.click()
    await expect(page).toHaveURL(
      new RegExp(`/topics/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}`),
    )
  })
})