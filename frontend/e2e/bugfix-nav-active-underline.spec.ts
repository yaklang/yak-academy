import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：主导航高亮必须精确匹配当前页面，不能因为共享前缀或父分区而误亮。
// 新外壳的页面导航位于侧栏（页面导航 aria-label），只允许当前项带 is-active。
test.describe('回归：主导航高亮精确匹配', () => {
  test('题库只亮题库、教程不亮；返回教程后反向成立', async ({ page }) => {
    await loginAsStudent(page)

    const nav = page.getByRole('navigation', { name: '页面导航' })
    const tutorials = nav.getByRole('link', { name: '教程', exact: true })
    const training = nav.getByRole('link', { name: '题库', exact: true })

    await training.click()
    await expect(page.getByRole('heading', { level: 1, name: '题库' })).toBeVisible()
    await expect(training).toHaveClass(/is-active/)
    await expect(tutorials).not.toHaveClass(/is-active/)

    await tutorials.click()
    await expect(page.getByRole('heading', { level: 1, name: '学习路线' })).toBeVisible()
    await expect(tutorials).toHaveClass(/is-active/)
    await expect(training).not.toHaveClass(/is-active/)
  })
})
