---
name: project-conventions
description: yak-academy 项目分层与目录约定。任何代码改动前必须阅读本 skill，防止破坏分层架构。适用于：新增/修改 Go 后端代码、调整目录结构、新增模块、依赖引入、编写或修改测试。
---

# 项目分层约定（强制）

## 分层依赖方向（唯一允许的依赖箭头）

```
api → service → repo → model
```

- `api`：HTTP handler + 中间件。只做：参数解析 → 调 service → 返回响应。**禁止**包含业务逻辑、**禁止**直接 import repo 或 model 之外触碰数据库。
- `service`：业务逻辑。**禁止**直接使用 gorm/sql 对象，一切数据访问走 repo 接口。
- `repo`：数据访问层。每个 repo 面向 service 暴露接口（Go interface），SQLite 是其唯一实现细节，**禁止**向上泄漏 gorm 类型。
- `model`：gorm 实体定义。只有字段和标签，**禁止**包含业务方法。

## 禁止事项

- 禁止 service 之间横向 import（白名单除外：`judge` 依赖 `event`、`content` 依赖 `challenge`、`content` 依赖 `ImageBuilder`（题目包 `image: .` 构建））。
- 禁止 handler 里散落权限判断（见 rbac-model skill：统一中间件）。
- 禁止绕过 repo 直接开 DB 连接。
- 禁止引入 Casbin / Redis / Kafka 等中间件（第一版自实现 + 进程内广播，见 docs/architecture.md）。

## Git 提交信息（强制）

- commit message（subject 与 body）一律**英文**。Conventional Commits 前缀保留，描述用英文，例：`feat(challenge): trim admin ops column to edit/delete`。
- 历史中文 commit 保持原样，**禁止**为统一语言重写已有历史。

## 目录职责表

| 目录 | 职责 |
|---|---|
| `backend/internal/api` | handler、中间件、路由注册 |
| `backend/internal/service` | 业务逻辑（auth/content/challenge/judge/env/userprogress）；env 含 InstanceDriver + ImageBuilder |
| `backend/internal/repo` | 数据访问接口及 SQLite 实现 |
| `backend/internal/model` | gorm 实体 |
| `backend/internal/eventbus` | 进程内事件总线 |
| `backend/internal/config` | 配置加载 |
| `backend/internal/pathutil` | 磁盘路径统一入口（`Join` / `SafeJoin` / `CleanEntry`） |
| `frontend/src/api` | 按契约手写的请求封装（唯一允许发 HTTP 的地方） |

## 路径处理（强制，跨 Windows / macOS / Linux）

磁盘路径拼接一律走 `backend/internal/pathutil`，业务代码**禁止新写裸 `filepath.Join`**：

- `pathutil.Join(elems...)`：拼接服务端自有段（slug、ID、配置目录）。
- `pathutil.SafeJoin(base, name)` / `pathutil.CleanEntry(name)`：任何来自 zip 条目、上传文件名等**不可信路径段**必须先过这两个函数——zip slip 防护（拒绝 `..` 段、盘符、UNC）集中在这一处，业务层不得手写归一化。错误哨兵 `pathutil.ErrUnsafePath`。
- 例外：`embed.FS` 等包内虚拟路径是 `/` 分隔的另一套语义，仍用标准库 `path.Join`（见 `service/builtin.go`），不经 pathutil。
- `filepath.Base/Ext/Dir/ToSlash` 等非拼接函数可直接使用。

### 跨 shell 传路径（脚本与命令行，Windows 开发环境必读）

git-bash 的 `/tmp`、`/c/...` 是 MSYS 虚拟路径，Windows 原生程序（python / node / go 构建产物等）**看不到**。给非 bash 程序传路径时用相对路径或 Windows 原生写法（`C:\...`、`$(cygpath -w ...)`）；脚本里禁止硬编码 `/tmp`，临时目录用 `"$TEMP"` 或 `mktemp` 后立即转换。

## 正反例

✅ 正确：`api/room.go` 的 handler 调用 `service.Room.Get(ctx, id)`，service 调用 `repo.Room.Find(ctx, id)`。
❌ 错误：handler 里 `db.Where("id = ?", id).First(&room)`。
❌ 错误：`service/content` import `service/judge`（不在白名单）。

## 测试编写要点（CI 强制全绿）

测试与被测代码同包（`xxx_test.go` 放在实现旁），走 `go vet ./... && go test -count=1 ./...` 与 GitHub Actions（`.github/workflows/ci.yml`）。

### 数据库与 fixture

- **纯 Go 内存 SQLite**：`gorm.Open(sqlite.Open("file:<testname>?mode=memory&cache=shared"), &gorm.Config{})`（glebarez 驱动，无 CGO）。每个测试文件一个独立的库名，**禁止**两个测试文件共用同一库名（cache=shared 会互相污染）。
- **必做清理**：`t.Cleanup` 里 `sqlDB.Close()` 关闭共享内存库，否则下个测试撞表。
- **fixture 工厂**：每包一个 `newXxxTestService(t) (被测 Service, *gorm.DB)`，内部 `db.Migrate(gdb)` + 构造真实 repo。**禁止** mock repo 接口——repo 本身就是抽象，测试用真实 SQLite 实现。
- **造数走 model/repo 或 Raw SQL**：fixture 直接 `gdb.Create(&model.X{...})`；要构造"历史遗留结构"（model 已删的物理列），用 `ALTER TABLE ... ADD COLUMN` + Raw SQL（参考 `db/open_test.go` 的 legacyRow）。
- **测试数据在测试代码中构造**（内存 zip / JSON 字符串），**禁止**向生产路径或仓库添加示例内容文件（无内置内容原则）。

### 测试替身与纯函数

- 外部依赖（Docker 等运行时）用**手写 fake**（如 `fakeDriver`）：结构体记录调用参数与次数，断言交互而非只断言返回值；错误注入用 `createErr/destroyErr` 字段，测试中置 nil 可模拟恢复重试。
- 纯函数（解析、派生逻辑）抽出来直接表驱动测试（如 `parseHostPort`、`containerName`），不依赖任何环境。
- 时钟注入：service 保留 `now func() time.Time` 字段，测试固定时间推进 TTL（参考 env_test 的 reaper 用例）。

### 断言与用例设计

- **错误断言用 `errors.Is(err, ErrValidation)` 等哨兵错误**，**禁止**断言错误字符串；DB 缺失行断言 `gorm.ErrRecordNotFound`（api 层负责映射 404，测试不测 HTTP 语义）。
- **一个用例一个行为**，名字说清场景：`TestEnsureInstanceDriverFailureRollsBack`、`TestUploadChallengesUpsertAndBatch`。正向、拒绝、幂等、失败回滚四类都要覆盖。
- **状态断言查库**，不信内存对象：关键路径后 `gdb.First(&after)` 断言落库结果（回滚、级联删除、事件写入都这样验）。
- **事件溯源断言**（event-sourcing-rules skill）：只查 Event 表的 type/payload 计数，**禁止**在测试里直改派生表来"准备"进度或积分。
- 边界条件必须显式：`go test -count=1` 防缓存是本地命令约定，**禁止**依赖测试执行顺序或共享状态。

### Docker 门控集成测试与 E2E

- **真实 Docker 集成测试**：文件置顶 `requireDocker(t)`（探测 `docker info`，不可用即 `t.Skip`），用 `ACADEMY_TEST_DOCKER=1` 显式开启（CI 的 backend-docker job 与本地手动跑），**禁止**让无 Docker 的环境因测试报错。参考 `service/env_docker_test.go`。
- **E2E 位置**：`frontend/e2e/`（Playwright）。配置 `frontend/playwright.config.ts`：双 webServer（后端 :18080 预编译单二进制 + vite :15173 代理）、每次整跑全新 DB、`global-setup.ts` 走管理 API 播种（**禁止**内置内容或前端直改 DB）。
- **浏览器获取**：默认探测系统 Chrome/Edge 作 channel（零下载）；`E2E_CHANNEL=chrome|msedge|bundled` 覆盖。**禁止**依赖 npmmirror/huaweicloud 的 playwright 镜像（已停更/假 200，实测不可用）。
- **串行执行**：e2e 套件共享一个后端 DB 与账号，`workers: 1`；改全局状态（密码/可见性）的用例必须**用例内恢复**（后续 spec 依赖初始常量）。
- **容器旅程**：`frontend/e2e/docker/`，`E2E_DOCKER=1` 才成为独立 project；默认套件不得依赖 Docker。

### GORM 陷阱（测试中反复踩过）

- **禁止复用同一 struct 变量做多次 First/查询**：struct 里残留的旧主键 ID 会成为隐式条件，查不到或查错行。每查一次用新变量（`var room model.Room`）。
- 共享内存库的表跨测试累积：断言**增量**（before/after 计数对比），不假设表为空。
- **`cache=shared` 内存库偶尔会泄漏到下个测试**（`t.Cleanup` 关闭连接的时序竞态，"下个测试重建"并非绝对保证）：断言**绝对计数/绝对状态**的用例必须用专属资源（独立 slug + 全新用户/ID），保证计数只由本用例产生（教训：`TestJudgeDynamicDecay` 曾随机 `score=50, want 70`）。
