---
name: e2e-bugfix
description: yak-academy bug 修复回归约束。每解决一个 bug 就必须按本 skill 的固定格式补一个 Playwright e2e 回归测试（frontend/e2e/）。在宣布任何 bug 修复完成之前加载本 skill，防止修复无回归防护地合入后再次复发。
---

# Skill: e2e-bugfix

## 核心规则

**每解决一个 bug，就必须在同一个 PR 中附一个通过真实 UI 操作复现该 bug 场景的 e2e 回归测试**——先红（在修复 commit 上 revert 或在旧代码上可失败）后绿（修复后通过）。没有回归测试的 bugfix 视为未完成。

「解决一个 bug」的判据：用户报告了缺陷，或开发中发现需要改代码修正的错误行为（含 UI 黑页/死链、接口 5xx、数据不一致、交互不符合预期）。纯新功能不强制回归测试（但欢迎顺手加旅程用例）。

## 测试放哪、怎么跑

- 位置：`frontend/e2e/`（Playwright）。运行方式与全部约定见 **project-conventions skill 的「Docker 门控集成测试与 E2E」节**（串行 workers:1、全新 DB、global-setup 经管理 API 播种、默认套件不得依赖 Docker）。
- 本地跑：`cd frontend && npx playwright test`（先确认本机无 18080/15173 端口占用；浏览器走系统 Chrome/Edge 自动探测）。
- 单跑一个文件：`npx playwright test e2e/bugfix-<name>.spec.ts`。

## 固定格式

### 1. 文件与用例命名

- 文件：`frontend/e2e/bugfix-<bug-short-name>.spec.ts`（kebab-case，名字说清被修的缺陷，如 `bugfix-admin-tutorial-link.spec.ts`）。
- 一个 bug 一个 spec 文件；spec 内可含多个 test（例如同一缺陷的多个端别/分支）。
- 用例名用中文短语说清「修复后的预期行为」+ 括注 bug 语境，如 `test('管理端配套教程链接指向 /admin/tutorials 并正常渲染（回归：黑页）', ...)`。

### 2. spec 文件骨架（注释块必写）

```ts
import { expect, test } from '@playwright/test'
import { E2E } from './fixtures/consts'
import { loginAsAdmin } from './helpers'

// 回归：<bug 一句话描述>（PR #<编号>，<YYYY-MM-DD> 修复）。
// 修复前行为：<错误行为>。
// 修复后行为：<正确行为>。
// 复现路径：<用户视角的触发路径，即用户是怎么撞上这个 bug 的>。
test.describe('回归：<bug 短名>', () => {
  test('<预期行为描述>（回归：黑页）', async ({ page }) => {
    await loginAsAdmin(page)
    // 1. 按用户真实路径导航到触发点
    await page.goto('/admin/training/<seeded-slug>')
    // 2. 执行触发动作（点链接/提交表单/…）
    // 3. 断言「修复后的正确终态」——不是断言"不崩溃"，而是断言业务正确性
    await expect(page).toHaveURL(...)
    await expect(page.getByRole('heading', { name: ... })).toBeVisible()
  })
})
```

### 3. 断言要求（写"绿"的标准）

- **断言业务终态，不断言实现细节**：页面渲染出预期内容（heading/正文非空）、URL 落在预期路由、判定结果文案正确。黑页类 bug 的终态断言 = 关键内容可见，而非 `body` 非空。
- **从用户路径进，不从 API 进**：经真实导航/点击触发（`page.goto` 到入口页再操作 UI），这正是 bug 溜过单测的原因。
- **数据从 global-setup 的种子来**（`fixtures/consts.ts` 的 `E2E` 常量 + global-setup 经管理 API 播种）。种子不足时在 consts 加常量并在 global-setup 补播种——禁止在 spec 里直连 DB 或硬编码运行期 ID。
- 依赖 Docker 的容器行为：放 `e2e/docker/`（`E2E_DOCKER=1` 独立 project），默认套件保持零 Docker；纯前端行为（路由/链接/弹窗渲染）留在默认套件。

### 4. 收尾清单（bugfix PR 的完成定义）

1. e2e 新用例随修复同 PR 提交；
2. 本地 `npx playwright test` 全绿（不只是新 spec 单跑）；
3. commit message（英文）列出新增的回归文件，如 `Regression coverage: e2e/bugfix-xxx.spec.ts`；
4. 若 bug 属「用户撞到的 UI 缺陷」类，确认 spec 断言的是用户看到的那条路径（而非只测了内部等价路径）。

## 反例（不要这样写）

❌ 只在旧代码上手工验证过、不留 e2e——下次重构路由表时黑页复发。
❌ spec 里 `page.goto('/admin/topics/...')` 直接访问坏 URL 断言 404——用户是点链接撞上的，要测链接生成与点击路径。
❌ spec 里造数走 `sqlite` 直写或硬编码 seed 后自增 ID——换库重跑即挂；用 E2E 常量 + global-setup。
❌ 断言 `await expect(page.locator('body')).not.toBeEmpty()` 就算修好黑页——空壳布局也会通过；断言目标内容可见。