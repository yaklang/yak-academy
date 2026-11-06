import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 管理端教程预览：列表页直达教程管理；专题页用面包屑 + 单一 Room 目录，
// 不再重复「学习路线」横向节点与「具体专题」列表（后者需要先滚动再二次点击）。
test.describe('管理端教程预览', () => {
  test('列表与专题页提供直接管理入口，专题目录一次点击进入教程', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/tutorials')

    await expect(page.getByRole('heading', { name: '教程预览' })).toBeVisible()
    await expect(page.locator('.topic-card .go').first()).toContainText('查看专题')
    const manageFromList = page.locator('main').getByRole('link', { name: /教程管理/ })
    await expect(manageFromList).toBeVisible()

    await page.goto(`/admin/tutorials/${E2E.tutorial.topicSlug}`)
    await expect(page.getByRole('heading', { name: E2E.tutorial.topicTitle })).toBeVisible()
    const crumbs = page.getByRole('navigation', { name: '面包屑' })
    await expect(crumbs.getByRole('link', { name: '教程预览' })).toBeVisible()
    await expect(page.getByRole('heading', { name: '教程目录' })).toBeVisible()
    await expect(page.getByRole('heading', { name: '学习路线' })).toHaveCount(0)
    await expect(page.locator('.route-map')).toHaveCount(0)

    const previewLinks = page.getByRole('link', { name: /预览教程/ })
    expect(await previewLinks.count()).toBeGreaterThan(0)
    await previewLinks.first().click()
    await expect(page).toHaveURL(
      new RegExp(`/admin/tutorials/${E2E.tutorial.topicSlug}/rooms/${E2E.tutorial.roomSlug}$`),
    )

    await page.goto(`/admin/tutorials/${E2E.tutorial.topicSlug}`)
    await page.locator('main').getByRole('link', { name: /教程管理/ }).click()
    await expect(page).toHaveURL(/\/admin\/panel\/tutorials$/)
  })
})
