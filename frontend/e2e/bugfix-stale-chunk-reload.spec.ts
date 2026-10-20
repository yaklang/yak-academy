import { expect, test } from '@playwright/test'
import { loginAsStudent } from './helpers'

// 回归：重新部署后旧页面点击导航无反应（2026-10-19 修复，随
// refactor/module-case-integration——内容重构频繁重建预览时用户撞上）。
// 修复前行为：旧页面持旧版 index.html，点击进入未访问过的路由时 vue-router
//   按需加载的 chunk 文件名已随新构建变化 → 404 → 路由组件加载失败，
//   导航中止、URL 不动、页面无任何反应（即"教程点不进去"）。
// 修复后行为：chunk 加载失败被 router.onError / vite:preloadError 捕获，
//   自动整页刷新一次拿新 bundle；sessionStorage 哨兵防无限刷新循环。
// 复现路径：任何一次重新部署后，在部署前就已打开的页面上点导航链接。
test.describe('回归：重新部署后旧页面 chunk 失效自动刷新', () => {
  test('路由 chunk 加载失败时自动 reload，刷新后导航恢复（回归：点击无反应）', async ({ page }) => {
    await loginAsStudent(page)
    await page.goto('/')
    await expect(page.getByRole('heading', { name: '学习路线' })).toBeVisible()

    // 部署切换的等价模拟：首次请求题库路由的懒加载组件模块时返回 404
    //（= 旧 bundle 引用的文件在新部署中已不存在）。仅失败一次：自动
    // reload 后的第二次请求放行，等价于新 bundle 里的模块就位。
    // 默认 e2e 后端为 dev server（/src/views/Training.vue 即懒加载模块），
    // 生产构建下同名请求是 assets/*Training*.js——两种形态都拦。
    let chunkFailed = false
    await page.route(
      /(src\/views\/Training\.vue)|(assets\/.*Training.*\.js)/,
      async (route) => {
        if (!chunkFailed) {
          chunkFailed = true
          await route.fulfill({ status: 404, body: 'gone with old deploy' })
          return
        }
        await route.continue()
      },
    )

    // 触发：点击「题库」——chunk 404 → 导航中止（URL 停在原地）→ 修复后
    // 自动整页 reload。先挂 load 事件屏障再点击：page.evaluate 不跨刷新，
    // 必须用事件等待 reload 完成（修复缺失时此处超时失败，断言同样成立）。
    const reloaded = page.waitForEvent('load')
    await page.getByRole('link', { name: '题库' }).click()
    await reloaded
    // 导航中止时 URL 未变：reload 落回当前页（按 pathname 断言，兼容 dev/docker 两种 baseURL）
    expect(new URL(page.url()).pathname).toBe('/')
    expect(await page.evaluate(() => sessionStorage.getItem('chunk-reloaded'))).toBe('1')

    // reload 落回当前页（导航中止时 URL 未变）：新 bundle 下应用恢复响应，
    // 同一次点击目标可以正常进入了——不再出现"点了没反应"
    await page.getByRole('link', { name: '题库' }).click()
    await expect(page.getByRole('heading', { name: '题库' })).toBeVisible()

    // 不刷新循环：恢复后再导航教程/题库往返，均正常渲染且不再触发 reload
    await page.getByRole('link', { name: '教程', exact: true }).click()
    await expect(page.getByRole('heading', { name: '学习路线' })).toBeVisible()
    await page.getByRole('link', { name: '题库' }).click()
    await expect(page.getByRole('heading', { name: '题库' })).toBeVisible()
    expect(await page.evaluate(() => sessionStorage.getItem('chunk-reloaded'))).toBe('1')
  })
})