# 前端设计契约（中性专业版）

> 版本：v1.0
> 本文档是 yak-academy 前端的视觉与交互唯一权威源。`.agents/skills/frontend-style/SKILL.md`
> 是执行摘要；两者冲突时以本文档为准，并提交 issue 修正 skill。

## 1. 为什么重构

现有前端不是“少几个样式”的问题，而是缺少可执行的约束：

- 两套 `:root`、两套 `@layer base`、重复的 public/auth/shell 样式块相互覆盖，同一个语义在不同页面颜色不同。
- 暖米色/咖啡色表面占满页面，品牌橙被当成背景色使用，深色模式整屏黑底 + 橙色高光，读起来像做旧照片或营销页，不像实训工具。
- 页面各自手写 `.page-head`、KPI 条、筛选条、表格卡片与骨架屏，21 个页面 21 种密度；信息密度低，1440×900 的表格经常只露出 2–3 行。
- 装饰性常驻动画（矩阵雨、解码文字、点击火花、滚动 reveal）带来性能与可访问性问题（`opacity: 0` 的正文、硬编码 `aria-label="text"`）。
- 表格列宽硬编码（如用户管理 1230px）被 `overflow-hidden` 裁切，1024 与 390 宽度下操作列不可达。
- 没有门禁：没有设计检查、没有页面清单、没有文案检查，问题会在每次迭代后重新长出来。

重构目标：**可预测的秩序 + 足够的信息 + 清晰层级 + 完整状态**。营销页可以有表现力；学习端与管理端必须克制、信息优先。

## 2. 视觉基调

- **中性专业**：浅色为白/浅灰画布 + 白色内容面 + 浅灰侧栏；深色为中性石墨层级，不使用纯黑。
- **品牌橙只做强调**：主操作、当前导航项、关键数值、焦点环。禁止铺满 hero、卡片、整行或大面积背景。
- **结构与主题解耦**：深浅色共用同一组件树，只切换语义 token；不为深色单独写组件。
- **内容优先**：能用列表/表格就不用卡片；卡片只承载“摘要 + 入口”。
- **密度**：默认紧凑。1440×900 的数据列表至少可见 5 行；表格行高 44px（两行内容 48px）。

禁止：大面积暖米色/咖啡色表面、彩色渐变底、发光边框、玻璃拟态、装饰性常驻动画、假数据填充。

## 3. 设计令牌

令牌集中在 `frontend/src/styles/tokens.css`，分三层：

### 3.1 原始层（只被语义层引用）

- 中性灰阶：`--gray-0 … --gray-950`（白到近黑，色相中性）。
- 品牌橙阶：`--orange-50 … --orange-900`。
- 状态色阶：success / warning / danger / info 各 50（浅底）、600（文字）、700（深底文字）。

页面与组件**不得直接引用原始层**，只能引用语义层。

### 3.2 语义层（浅色 / 深色各一套）

| 令牌 | 浅色 | 深色 | 用途 |
|---|---|---|---|
| `--bg-canvas` | `#f6f6f4` | `#0b0b0c` | 应用画布 |
| `--bg-surface` | `#ffffff` | `#141416` | 卡片、面板、表格 |
| `--bg-surface-raised` | `#ffffff` | `#19191b` | 弹层、Dialog、下拉 |
| `--bg-subtle` | `#f1f1ef` | `#1e1e21` | 表头、只读块、代码块 |
| `--bg-hover` | `#ececea` | `#26262a` | 悬停 |
| `--bg-active` | `#e6e6e3` | `#2e2e33` | 选中 |
| `--text-primary` | `#18181b` | `#f4f4f5` | 正文与标题 |
| `--text-secondary` | `#52525b` | `#a1a1aa` | 次要文字 |
| `--text-tertiary` | `#8a8a93` | `#71717a` | 元数据、占位 |
| `--border` | `#e4e4e7` | `#27272a` | 分隔线、卡片描边 |
| `--border-strong` | `#d4d4d8` | `#3f3f46` | 输入框、需要强调的边界 |
| `--action-primary` | `#c2410c` | `#fb923c` | 主按钮背景 |
| `--action-primary-hover` | `#9a3412` | `#fdba74` | 主按钮悬停 |
| `--action-primary-fg` | `#ffffff` | `#1c1206` | 主按钮文字 |
| `--action-subtle` | `#fff1e8` | `#2a1a0e` | 次级强调底 |
| `--action-subtle-fg` | `#9a3412` | `#fdba74` | 次级强调文字 |
| `--focus-ring` | `#ea580c` | `#fb923c` | 焦点环 |
| `--status-success` | `#15803d` | `#4ade80` | 成功 |
| `--status-warning` | `#a16207` | `#fbbf24` | 警告 |
| `--status-danger` | `#b91c1c` | `#f87171` | 危险 |
| `--status-info` | `#1d4ed8` | `#60a5fa` | 信息 |

派生色（成功/警告/危险的浅底）用 `color-mix(in srgb, var(--status-*) 10%, var(--bg-surface))`，不新增十六进制。

### 3.3 兼容层

shadcn-vue 与既有 `Ui*` 组件依赖 `--background/--foreground/--card/--panel/--popover/--primary/--secondary/--muted/--accent/--border/--input/--ring/--destructive`。这些变量在 `tokens.css` 中映射到语义层，例如：

```css
--background: var(--bg-canvas);
--card: var(--bg-surface);
--panel: var(--bg-surface);
--popover: var(--bg-surface-raised);
--primary: var(--action-primary);
--primary-foreground: var(--action-primary-fg);
--muted: var(--bg-subtle);
--muted-foreground: var(--text-secondary);
--accent: var(--action-subtle);
--accent-foreground: var(--action-subtle-fg);
```

兼容层是迁移期的唯一允许的双写；页面不得再新增自定义颜色变量。

### 3.4 图表

`--chart-1 … --chart-5` 从语义层派生：橙（主序列）、slate、teal、amber、violet，深色下整体提亮。`CompetitionScoreChart` 等图表必须读取 CSS 变量，不得硬编码调色板。

## 4. 排版

### 4.1 字号角色（只允许这些）

| 角色 | 令牌 | 字号/字重 | 行高 | 用途 |
|---|---|---|---|---|
| 微型 | `--text-micro` | 11px / 500 | 1.45 | 徽标、计数气泡 |
| 元数据 | `--text-meta` | 12px / 400 | 1.5 | 表格元信息、时间、说明 |
| 表头 | `--text-meta` + 500 | 12px / 500 | 1.4 | 表头、字段标签 |
| 次要 | `--text-caption` | 13px / 400 | 1.6 | 次要正文、表单帮助 |
| 正文 | `--text-body` | 14px / 400 | 1.65 | 默认正文 |
| 区块标题 | `--text-section` | 16px / 600 | 1.4 | 卡片/区块标题 |
| 页面标题 | `--text-page` | 20px / 600 | 1.35 | 每页唯一 `h1` |
| 指标 | `--text-metric` | 24px / 600 | 1.2 | StatCard 数值 |
| 展示 | `--text-display` | 32px / 600 | 1.25 | 公开页 hero（≤32px） |

- 不出现 9/10/15/19/21/28/30/44/110/120px；不使用 `clamp()` 字号。
- 中文标题字距 `0`；只有大号拉丁标题允许 `-0.02em`。
- 数字使用 `font-variant-numeric: tabular-nums`。
- 正文最大行宽 72ch（`max-width: 72ch`）；中文长文行高 1.7。

### 4.2 字体

- 正文：系统 UI 栈（`-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif`）。
- 代码/命令/分值/编码：`ui-monospace, "SFMono-Regular", Consolas, monospace`。
- 不声明未加载的字体（删除 `components.json` 中的 `geist-sans`）。

## 5. 间距、圆角、阴影、宽度

- **间距**：只用 4px 档位 `4 / 8 / 12 / 16 / 20 / 24 / 32 / 40 / 48 / 64`。页面外边距 16（≤720 用 12），卡片内边距 12/16，区块间距 16/24。
- **圆角**：`--radius-control: 8px`、`--radius-card: 12px`、`--radius-overlay: 16px`、`--radius-pill: 999px`。Tailwind `--radius-sm/md/lg/xl` 显式映射 6/8/10/12，不再用 `calc(var(--radius) * …)`。同一页面不混用。
- **阴影**：只有 popover / dialog / drawer 使用 `--shadow-popover`；卡片靠 1px 边框与表面明度差。
- **宽度**：应用内容最大 1280px；公开页 1200–1280px；竞赛满屏页最大 1440px。删除 `.public-container:1440` 与 `--layout-max-width:1280` 的双口径。
- **外壳**：左侧 48px 图标栏 + 224px 菜单（可折叠），顶栏 48px；移动端用 Sheet。

## 6. 页面配方

### 6.1 列表 / 管理页

```text
PageHeader（唯一 h1 + 一句事实描述 + 主操作）
DataTablePanel（标题/计数 + 工具栏筛选 + 表格 + 分页）
```

- 不再套第二层大卡片；`DataTablePanel` 是列表页唯一容器。
- 表格横向滚动容器 `overflow-x: auto`；列宽用 `min-width` 契约，禁止把列宽算成固定总和。
- 单元格默认截断 + `title` 提示；操作列 ≤2 个直接按钮，其余进菜单。

### 6.2 详情 / 阅读页

`PageHeader` → 72ch 正文列 + 右侧固定操作栏（≥1024）；<1024 单列。首屏必须出现标题、关键元信息与主操作。

### 6.3 表单 / 设置页

`PageHeader` → 若干 `FormSection`（标题 + 说明 + 字段）；主按钮右下，错误贴字段（`aria-describedby` + `aria-invalid`）。

### 6.4 满屏页（竞赛入场 / 竞赛首页）

独立 `CompetitionShell`，保留自己的顶栏；复用 token、表格、状态与按钮。移动端单列可用，倒计时与操作常显。

### 6.5 公开页（Landing / Login）

静态品牌视觉 + 清晰 CTA，不进入应用外壳；无 canvas、无解码动画、无点击粒子。品牌橙小面积使用。

## 7. 共享原语

| 原语 | 职责 | 禁止 |
|---|---|---|
| `AppShell` / `SidebarNav` / `TopBar` | 外壳、导航、折叠、移动端 Sheet | 页面内再写 header |
| `PageHeader` | 唯一 `h1`、描述、面包屑、动作、页内 tabs | 页面手写 `.page-head` |
| `PageTabs` | 管理端分组页内切换 | 第二条 subnav |
| `DataTablePanel` | 标题/计数/工具栏/表格/分页一体 | 页面再包一层卡片 |
| `EmptyState` | 首次空 / 筛选空 / 错误三态 + 下一步 | “暂无数据”孤零零一句 |
| `StatCard` / `MetricStrip` | 统一 KPI 条 | 每页自写 `.kpi/.stat/.metric` |
| `StatusTag` | 状态/难度/角色统一中文映射 + tone | 裸枚举、只靠颜色 |
| `FilterBar` / `Toolbar` | 搜索、筛选、清除、主操作 | 手写筛选条 |
| `FormSection` / `FormField` | 标签、帮助、错误、aria 绑定 | 裸 `<label>` + 无错误关联 |
| `ConfirmDialog` | 破坏性操作二次确认 | 直接执行删除 |
| `UiTable` 升级 | 横向滚动、sticky 表头、`aria-sort`、行键盘激活、动态选择标签 | 硬编码“选择本页全部题目” |

## 8. 状态与文案

每个数据页面必须显式实现：加载（骨架与最终布局一致）、空（原因 + 下一步）、错误（`role="alert"` + 原因 + 重试）、无权限（说明所需角色）、提交中（禁用主按钮）、悬停/焦点/按下/禁用。

### 8.1 术语表（界面可见文本）

| 内部术语 | 界面用语 |
|---|---|
| Room | 章节（首次出现可注“教程章节”） |
| Topic | 专题 |
| WP | 解题报告 |
| 普通人 / student | 学员 |
| 账号 ID | 账号编码 |
| running / draft / published / archived | 运行中 / 草稿 / 已发布 / 已归档 |
| flag_mode / ACADEMY_* / noop / slug | 不得出现，改写为用户语言 |
| 容器状态、审计动作、角色、难度 | 统一走中文映射表（`lib/format`） |

- 界面不得出现原始 JSON、内部字段名、英文枚举。
- 空态必须给下一步；筛选空给“清除筛选”；错误给重试；无权限说明所需角色。
- 示例内容（`examples/`）同步改写，避免演示题暴露内部字段。

## 9. 动效

- 只保留功能性过渡：hover/focus 120–160ms、展开/收起 160–200ms、加载 spinner。
- 全部尊重 `prefers-reduced-motion: reduce`。
- 删除 `TerminalRain`、`CipherReveal`、`ClickSpark`、`Reveal`、`AcademyBackground` 及对应全局 CSS。
- 删除 `.signal-orbit`、`.signal-emblem`、`.auth-orbit-mark`、`.closing` 的径向渐变等装饰层。

## 10. 可访问性

- 每页恰好一个 `h1`；表格有 `caption`/`aria-label`；可排序列 `aria-sort`；行可键盘激活。
- 表单错误通过 `aria-describedby`/`aria-invalid` 绑定；Toast 不作为唯一错误出口。
- 正文对比 ≥4.5:1，大字/图形 ≥3:1；焦点环可见（2px `--focus-ring`，offset 2px）。
- 移动端触控目标 ≥44px（按钮/输入/分页）。
- 不只用颜色表达状态，同时给文字/图标。

## 11. 响应式

至少检查 1440 / 1024 / 390 三个宽度：

- 1440：内容区最大 1280，表格完整可见，≥5 行。
- 1024：侧栏可折叠，筛选分级收纳，表格横向滚动可用。
- 390：单列，无横向溢出，触控目标 ≥44px，操作列可达。

## 12. 治理与门禁

| 脚本 | 检查 |
|---|---|
| `npm run check:design` | token 文件外禁止 hex/rgb、非 4px 档位 px、非约定字号/圆角、`!important`、静态内联 style |
| `npm run check:census` | 每个登录后页面使用 `PageHeader`，列表页使用 `DataTablePanel`（白名单：满屏竞赛页/阅读页），每页恰好一个 `h1` |
| `npm run check:copy` | 可见模板文本禁止 `running/draft/published/archived`、`ACADEMY_*`、`flag_mode`、`noop`、`slug`、裸 JSON |
| `e2e/visual-qa.spec.ts` | 全部路由 × 1440/1024/390 × 浅/深色：无横向溢出、一个 `h1`、无 console error、字号/对比度符合角色表、截图存档 |

CI 前端 job 依次运行 `check:design` → `check:census` → `check:copy` → `build` → `test` → `e2e`。截图只做 artifact，不做跨平台像素 diff。

## 13. 审查清单

- [ ] 页面标题、描述、主操作首屏可见。
- [ ] 没有未定义类名；没有 token 文件外的硬编码颜色/圆角/间距。
- [ ] 列表页使用 `DataTablePanel`，1440×900 至少 5 行，1024/390 不裁切。
- [ ] 加载、空、错误、无权限、提交中状态齐全，空态有下一步。
- [ ] 悬停、焦点、按下、禁用四态齐全。
- [ ] 每页一个 `h1`；表格/表单 aria 完整；对比度达标。
- [ ] 界面无内部术语与裸枚举。
- [ ] 无新增常驻动画/全局点击特效。
- [ ] 深色为中性石墨，品牌橙没有铺满大面积。
- [ ] `check:design`、`check:census`、`check:copy`、`build`、`test`、相关 e2e 全绿。
