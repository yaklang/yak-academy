import { mkdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { endCompetition, loginAsAdmin, loginAsStudent, seedRunningCompetition } from './helpers'

// 视觉验收：全部页面在 1440/1024/390、浅色/深色下的结构与可读性守护。
// 截图只作为 artifact 上传，不做像素基线（跨平台字体渲染不可比），
// 断言的是与实现无关的契约：无横向溢出、单一 h1、角色字号、对比度、触控目标。
test.use({ reducedMotion: 'reduce' })

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'laptop', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 844 },
] as const

const THEMES = ['light', 'dark'] as const

// 学习端：教程/题目/账号/排行榜/站内邮箱/竞赛广场。
const STUDENT_ROUTES = [
  '/learn',
  '/topics/web-security',
  '/topics/web-security/rooms/sqli-basics',
  '/training',
  '/training/crypto-caesar-shift',
  '/badges',
  '/leaderboard',
  '/profile',
  '/mailbox',
  '/competitions',
]

// 管理端：内容、题目、容器、备份、审计、设置、账号、竞赛。
const ADMIN_ROUTES = [
  '/admin/panel/tutorials',
  '/admin/panel/challenges',
  '/admin/panel/containers',
  '/admin/panel/backups',
  '/admin/panel/audit-logs',
  '/admin/panel/settings',
  '/admin/panel/users',
  '/admin/panel/competitions',
]

const FONT_SIZES = new Set([11, 12, 13, 14, 16, 20, 24, 32])

function slugOf(route: string) {
  return route === '/' ? 'landing' : route.replace(/^\//, '').replace(/[/?=&]+/g, '-')
}

function shotPath(testInfo: TestInfo, viewport: string, theme: string, route: string) {
  const dir = testInfo.outputPath('visual')
  mkdirSync(dir, { recursive: true })
  return path.join(dir, `${viewport}-${theme}-${slugOf(route)}.png`)
}

// 每个页面走同一套契约检查；截图落 Playwright artifact 供人工复核。
async function auditPage(
  page: Page,
  route: string,
  testInfo: TestInfo,
  viewport: (typeof VIEWPORTS)[number],
  theme: (typeof THEMES)[number],
) {
  const consoleErrors: string[] = []
  const onConsole = (message: { type: () => string; text: () => string }) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  }
  page.on('console', onConsole)
  try {
    await page.goto(route)
    await expect(page.locator('h1')).toHaveCount(1)
    await page.waitForTimeout(400)

    await expect
      .poll(
        () =>
          page.evaluate(
            () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
          ),
        { message: `${route} 在 ${viewport.width}px 不应横向溢出`, timeout: 5_000 },
      )
      .toBeLessThanOrEqual(1)

    const sizes = await page.evaluate(() => {
      const found = new Set<number>()
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      while (walker.nextNode()) {
        const node = walker.currentNode
        if (!node.textContent?.trim()) continue
        const el = node.parentElement
        if (!el || el.closest('svg, script, style, noscript')) continue
        const rects = el.getClientRects()
        if (!rects.length) continue
        const style = getComputedStyle(el)
        if (style.visibility === 'hidden' || style.display === 'none') continue
        found.add(Math.round(parseFloat(style.fontSize)))
      }
      return [...found].sort((a, b) => a - b)
    })
    const offScale = sizes.filter((size) => !FONT_SIZES.has(size))
    expect(offScale, `${route} 出现角色表外的字号（px）`).toEqual([])

    // 正文字号与对比度：正文 ≥4.5:1，大字号（≥24px）≥3:1。
    // 半透明叠加/渐变/禁用态无法可靠还原最终底色，按“不适用”跳过。
    const contrastFailures = await page.evaluate(() => {
      const parse = (value: string) => {
        const match = value.match(/rgba?\(([^)]+)\)/)
        if (!match) return null
        const parts = match[1].split(/[,\s/]+/).filter(Boolean).map(Number)
        return { r: parts[0], g: parts[1], b: parts[2], a: parts.length > 3 ? parts[3] : 1 }
      }
      const channel = (value: number) => {
        const scaled = value / 255
        return scaled <= 0.04045 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4
      }
      const luminance = (color: { r: number; g: number; b: number }) =>
        0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b)
      const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) => {
        const la = luminance(a)
        const lb = luminance(b)
        return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
      }
      const backgroundOf = (el: Element) => {
        let node: Element | null = el
        while (node) {
          const style = getComputedStyle(node)
          if (style.backgroundImage !== 'none') return null
          const bg = parse(style.backgroundColor)
          if (bg && bg.a >= 0.95) return bg
          if (bg && bg.a > 0.05) return null
          node = node.parentElement
        }
        return { r: 255, g: 255, b: 255, a: 1 }
      }
      const failures = new Map<string, string>()
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      while (walker.nextNode()) {
        const node = walker.currentNode
        const text = node.textContent?.trim()
        if (!text) continue
        const el = node.parentElement
        if (!el || el.closest('svg, script, style, noscript')) continue
        if (el.closest('[disabled], [aria-disabled="true"]')) continue
        if (!el.getClientRects().length) continue
        const style = getComputedStyle(el)
        if (style.visibility === 'hidden' || style.display === 'none') continue
        const fg = parse(style.color)
        if (!fg || fg.a < 0.95) continue
        const bg = backgroundOf(el)
        if (!bg) continue
        const size = parseFloat(style.fontSize)
        const weight = Number(style.fontWeight) || 400
        const required = size >= 24 || (size >= 18.66 && weight >= 700) ? 3 : 4.5
        const value = ratio(fg, bg)
        if (value + 0.05 < required) {
          const key = `${style.color}|${style.backgroundColor}|${size}`
          if (!failures.has(key)) {
            failures.set(
              key,
              `「${text.slice(0, 18)}」 ${value.toFixed(2)}:1 < ${required}:1（${size}px）`,
            )
          }
        }
      }
      return [...failures.values()].slice(0, 8)
    })
    expect(contrastFailures, `${route} 存在对比度不足的文本`).toEqual([])

    if (viewport.width <= 390) {
      const smallTargets = await page.evaluate(() => {
        // 开关的可见轨道只有 32×18，但组件用 ::after 把可点击区域扩到 ≥44px，
        // 包围盒无法反映扩展命中区，因此按控件类型排除。
        const selector =
          'button:not([role="switch"]), [role="button"], input:not([type="hidden"]), select, textarea, [role="tab"], .app-nav-item, .ui-pagination button'
        return [...document.querySelectorAll<HTMLElement>(selector)]
          .filter((el) => {
            if (el.closest('[disabled], [aria-disabled="true"]')) return false
            const rect = el.getBoundingClientRect()
            const style = getComputedStyle(el)
            if (style.visibility === 'hidden' || style.display === 'none') return false
            return rect.width > 0 && rect.height > 0 && rect.width < 44 && rect.height < 44
          })
          .map((el) => {
            const rect = el.getBoundingClientRect()
            return `${el.tagName.toLowerCase()}「${(el.textContent || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim().slice(0, 16)}」${Math.round(rect.width)}×${Math.round(rect.height)}`
          })
          .slice(0, 8)
      })
      expect(smallTargets, `${route} 390px 下触控目标应 ≥44×44`).toEqual([])
    }

    await page.screenshot({
      path: shotPath(testInfo, viewport.name, theme, route),
      fullPage: true,
    })

    // 浏览器把失败的 fetch（如 noop 驱动下 501 的容器指标）记为 console error，
    // 这类响应由页面错误态承接并另有功能用例覆盖；这里只拦未捕获的脚本错误。
    expect(
      consoleErrors.filter((message) => !message.startsWith('Failed to load resource')),
      `${route} 控制台不应出现错误`,
    ).toEqual([])
  } finally {
    page.off('console', onConsole)
  }
}

for (const viewport of VIEWPORTS) {
  for (const theme of THEMES) {
    const label = `${viewport.name} ${viewport.width}px / ${theme}`

    test(`公开页视觉走查 · ${label}`, async ({ page }, testInfo) => {
      await page.addInitScript((value) => localStorage.setItem('academy_theme', value), theme)
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      for (const route of ['/', '/login']) {
        await auditPage(page, route, testInfo, viewport, theme)
      }
    })

    test(`学习端视觉走查 · ${label}`, async ({ page }, testInfo) => {
      await page.addInitScript((value) => localStorage.setItem('academy_theme', value), theme)
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      await loginAsStudent(page)
      for (const route of STUDENT_ROUTES) {
        await auditPage(page, route, testInfo, viewport, theme)
      }
    })

    test(`管理端视觉走查 · ${label}`, async ({ page }, testInfo) => {
      await page.addInitScript((value) => localStorage.setItem('academy_theme', value), theme)
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      await loginAsAdmin(page)
      for (const route of ADMIN_ROUTES) {
        await auditPage(page, route, testInfo, viewport, theme)
      }
    })

    test(`竞赛满屏页视觉走查 · ${label}`, async ({ page, request }, testInfo) => {
      await page.addInitScript((value) => localStorage.setItem('academy_theme', value), theme)
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      const { id, code } = await seedRunningCompetition(
        request,
        `E2E 视觉走查赛 ${viewport.name} ${theme}`,
        ['crypto-caesar-shift'],
      )
      try {
        await loginAsStudent(page)
        await page.goto(`/competitions/enter/${id}`)
        await expect(page.locator('h1')).toHaveCount(1)
        await page.getByPlaceholder('粘贴邀请邮件中的邀请码').fill(code)
        await page.getByRole('button', { name: '进入竞赛', exact: true }).click()
        await page.waitForURL((url) => url.pathname === `/competitions/${id}`)
        await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
        await page.waitForTimeout(400)

        await expect
          .poll(
            () =>
              page.evaluate(
                () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
              ),
            { message: `竞赛满屏页在 ${viewport.width}px 不应横向溢出`, timeout: 5_000 },
          )
          .toBeLessThanOrEqual(1)
        await page.screenshot({
          path: shotPath(testInfo, viewport.name, theme, `/competitions/${id}`),
          fullPage: true,
        })
      } finally {
        // 入场会锁定学生账号的非竞赛接口；结束比赛解除锁，避免污染后续用例。
        await endCompetition(request, id)
      }
    })
  }
}