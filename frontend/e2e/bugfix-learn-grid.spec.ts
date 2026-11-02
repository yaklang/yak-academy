import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsStudent } from './helpers'

// 回归：学习路线卡片网格样式丢失，卡片退化为整行横幅（2026-11-02 修复）。
// 修复前行为：Topics.vue 使用 card-grid/tall-card，但样式在重构中被删除，
//   桌面端每张专题卡占满内容区整行，装饰纹样占据大量高度、信息密度极低。
// 修复后行为：桌面端专题卡按 3 列网格排列（单卡宽度约为内容区 1/3），
//   卡片展示标题、摘要、Room 数与明确入口。
// 复现路径：学生登录 → 顶部「教程」/learn → 观察专题卡片排列。
test.describe('回归：学习路线卡片网格', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('专题卡按桌面网格排列且入口信息完整（回归：整行大卡片）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto('/learn')

    const card = page.locator('.topic-card').filter({ hasText: E2E.tutorial.topicTitle }).first()
    await expect(card).toBeVisible()
    await expect(page.getByRole('heading', { name: '学习路线' })).toBeVisible()
    await expect(page.getByText('按专题循序渐进', { exact: false })).toBeVisible()

    // 旧实现没有 grid 样式，单卡宽度接近整个内容区（约 1200px）；
    // 3 列网格下单卡约 390px，必须明显小于半屏。
    const box = await card.boundingBox()
    expect(box, '专题卡应有可测量尺寸').not.toBeNull()
    expect(box!.width).toBeGreaterThan(320)
    expect(box!.width).toBeLessThan(520)

    await expect(card.getByText(/个 Room/)).toBeVisible()
    await expect(card.getByText('进入专题')).toBeVisible()

    // 回归（2026-11-02）：topic.md front matter 的 summary 曾不被解析，
    // 专题卡只剩标题 + Room 数（信息密度空缺）。修复后卡片必须渲染摘要行。
    await expect(card.getByText('请求伪造、注入与前端信任边界', { exact: false })).toBeVisible()
  })
})
