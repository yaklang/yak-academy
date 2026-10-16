---
name: content-package-spec
description: yak-academy 教程包与题目 JSON 格式唯一规范。涉及内容上传、zip 解析、Markdown front matter、challenge.json 题目定义、内容版本化或发布状态机时必须加载。
---

# 教程包与题目 JSON 规范（管理员上传是内容的唯一来源）

## 硬性原则

- **禁止任何内置/硬编码教程内容**。所有教程与题目都来自管理员上传（教程 zip 包 / 题目 challenge.json）。代码里不得出现示例课程数据、demo 文章常量。
- 唯一例外一：数据库迁移可以创建初始管理员账号，但**不得**创建任何 Topic/Room/Challenge 初始数据。
- 唯一例外二：官方内置训练集（配套教程 + 题目，基于 yaklang vulinbox 靶场引擎，`service/builtin/vulinbox/`，随二进制 go:embed 分发）。**仅**经管理员在管理端显式「一键导入」（`POST /admin/challenges/builtin`）生效，重复导入 = 更新；不得绕过该入口在迁移/启动时自动写入。题目走同一 `UploadChallenges` 校验 + upsert 管线；配套教程（`tutorials/` 子目录，同一 manifest/topic.md/lesson.md front matter 解析规则）复用 `parseFiles` 解析与静态校验后**直接 upsert Topic/Room**——绕过包状态机，无 draft/publish，也不产生 ContentPackage 行。这是平台唯一允许的随二进制分发的内容资产，新增其他内置内容必须先修订 docs/architecture.md 决策 2。
- **教程包只含教程**（文字 + 视频）；题目不经教程包上传——旧 lab.yaml 模式已废弃（解析代码仅为旧版本 Payload 回放兼容保留，apply 时忽略），新题目走 challenge.yml 包 zip 或兼容 challenge.json。

## 教程包结构（zip）

```
package.zip
├── manifest.yaml        # 包元数据（必填）
├── topics/
│   ├── <topic-slug>/
│   │   ├── topic.md     # topic 首页教程（front matter 必填）
│   │   ├── rooms/
│   │   │   └── <room-slug>/
│   │   │       └── lesson.md   # Room 教程正文（front matter 必填，可含 video）
```

## manifest.yaml 必填字段

`name`（包名）、`version`（语义化版本）、`language`、`topics`（topic slug 列表）。

## lesson.md front matter

- 必填：`title`、`difficulty`（apprentice/practitioner/expert 三级，禁止其他值）。
- 可选：`video`（视频 URL：mp4/webm 直链前端用 video 标签播放，其余 URL 用 iframe embed）。

## 题目定义

题目是独立资源，admin 上传后 **upsert 即生效**（无 draft/publish）。两种上传形态：

### A. 标准题目包 zip（推荐；有容器时强制文件约束）

`POST /admin/challenges/package`（multipart 字段 `package`）：

```
<challenge>/
├── challenge.yml          # 必填（或 challenge.yaml）
├── Dockerfile             # 声明 image 时必填
├── docker-compose.yml     # 声明 image 时必填（或 compose.yml）
└── src/…                  # 可选（构建上下文）
```

`challenge.yml` 关键字段（实训常用运行时约定）：

| 字段 | 说明 |
|---|---|
| `slug` / `name` | slug 必填；title 取 `title` 或 `name` |
| `category` / `difficulty` / `value`（或 `score`） | difficulty 三级枚举 |
| `description` | Markdown 描述 |
| `type` + `flags` | flag 匹配类型 static/regex；取首个 flag |
| `flag_mode` | `shared`（默认，题目级共享）/ `instance`（每实例唯一，模板含 `{token}`） |
| `image` + `port` | 可选容器；`image: .` 落库为 `yak-academy/<slug>:latest` 且**上传时 docker build** |
| `hints` / `tutorial_slug` / `bank` | 可选；bank = 题库 tab（**上传仅 ctf/acm**，缺省 ctf），见 B 节 |

- `flag_mode: instance` **必须**同时声明容器（image/port）；模板示例：`flag{lab-{token}}`。
- `image: .`：平台解压构建上下文并 `docker build -t yak-academy/<slug>:latest`；Docker 不可用 → 422，禁止悬空 tag。
- 远程镜像（非 `.`）不构建，仍要求包内含 Dockerfile + compose（文档/本地复现用）。

### C. 内置训练集（vulinbox 教程 + 题目，仅经管理端一键导入）

`POST /admin/challenges/builtin`（无 body）导入随二进制嵌入的 vulinbox 训练集（题目 `service/builtin/vulinbox/challenges.json`，schema 同 challenge.json；配套教程 `tutorials/` 子目录，结构同教程包）：

- Docker 可用时先构建共享镜像 `yak-academy/vulinbox:latest`（构建失败 → 422，整体零写入）；Noop 驱动跳过构建（`image_built=false`，仅导入定义）。
- 配套教程先于题目导入：`parseFiles` + 静态校验后直接 upsert Topic/Room（幂等，无 draft/publish、无 ContentPackage 行）；**一教程对一题**（`challenges.json` 每题 `tutorial_slug` 指向配套 Room，`yakit-` 前缀 slug 命名空间）。
- 全题集 `flag_mode: shared` + 同一 `env`（vulinbox 单进程按路径路由多靶机，不消费 FLAG 环境变量，instance 模式无效）。
- 全题集导入时统一注入 `bank: tutorial`（上传路径已禁 tutorial，此为唯一写入方），并带 `is_builtin=true` 来源标记（普通上传更新这些题时标记保留）。
- vulinbox 二进制为 AGPL-3.0 上游独立程序（镜像内附 NOTICE），修改 Dockerfile 锁定的 sha256 前先读 docs/architecture.md 决策 2 的例外边界。

参考样例（不自动加载）：`examples/challenges/vulinbox-xss-reflected/`。

### B. challenge.json（兼容批量导入）

`POST /admin/challenges`（单对象或对象数组）：

```json
{
  "slug": "sqli-login-bypass",
  "title": "SQL 注入登录绕过",
  "category": "sql-injection",
  "difficulty": "apprentice",
  "description_md": "# 背景\n任务目标……",
  "score": 100,
  "flag_mode": "shared",
  "bank": "ctf",
  "challenge": { "type": "static", "flag": "flag{answer}" },
  "env": { "image": "vulnapp:1.0", "port": 80 },
  "hints": [ { "title": "提示一", "content": "看 UNION", "cost": 10 } ],
  "tutorial_slug": "sqli-basics"
}
```

- 必填：`slug`/`title`/`category`/`difficulty`/`score`/`challenge`；`description_md`/`env`/`hints`/`tutorial_slug`/`flag_mode`/`bank` 可选。
- `bank`：`ctf`（缺省）/ `acm`——题库页 tab 分组键。**`tutorial` 为教程题目 tab 专属，保留给内置题集**：上传显式传 `tutorial` 一律 422；缺省时新建默认 `ctf`、重传保留原值。
- `is_builtin` 不是上传字段：它是导入来源标记，仅 `POST /admin/challenges/builtin` 置 true（题库页「是否内置」过滤依据），普通上传/编辑 upsert 不触碰、也不清除。
- `challenge.type`：`static`（明文 flag）/ `regex`（可编译正则）。
- `flag_mode`：`shared`|`instance`；`instance` 时 flag 为含 `{token}` 的模板，且必须有 env。
- `env` 整段可选；**要么都填要么都不填**。image 非空即 `has_env: true`。
- `hints[].cost` 非负；解锁走 `hint.unlocked` 事件扣分（见 event-sourcing-rules）。
- 批量数组逐题校验、**单题失败整体不入库**；slug 重复 = 更新（upsert）。
- 题目难度枚举与教程一致（apprentice/practitioner/expert），禁止其他值。

## 教程包上传管线五步（顺序不可跳、不可合并）

```
Upload → Parse → Validate → Persist → Publish
```

1. **Upload**：接收 zip，校验大小与 MIME；
2. **Parse**：解压解析 manifest/topic/room（lesson.md），解析失败整体拒绝（禁止部分入库）；
3. **Validate**：字段校验（front matter 必填、难度枚举、slug 冲突检测）；
4. **Persist**：写入 ContentPackage/ContentVersion，状态 = draft；
5. **Publish**：管理员显式发布，draft → published；已发布包再上传新版本 = 新 ContentVersion，旧版本可回滚（archived）。

条目名安全（zip slip / 盘符 / UNC / `..` 段）统一由 `backend/internal/pathutil.CleanEntry` 校验（规则见 project-conventions skill「路径处理」），业务层不得手写归一化。

## 状态机（唯一合法迁移，仅教程包）

包级：`draft → published → archived`。**禁止** draft 直接跳 archived；archived 为终态（包冻结，拒绝再发布/回滚）。

版本级（ContentVersion.Status，与包级同枚举）：

- Publish：目标版本 → published，包内其余版本 → archived（同时至多一个 published 版本）；
- Rollback（admin，`POST /content/packages/{id}/rollback`）：published 包回滚到某个 archived 版本——目标版本 Payload 覆盖重写内容表（只写 Topic/Room，含 video；旧 Payload 中的 Labs 段忽略），目标 → published、当前 → archived，`latest_version` 更新，包级保持 published。禁止回滚当前版本/draft 版本；
- 同名包重传新版本：既有版本 Payload 中的 slug 视为自有，不判 slug 冲突（他包占用的 slug 仍拒绝）。

**删除**（admin，`DELETE /content/packages/{id}`，任何包状态可用，不可恢复）：

- 删包记录 + 全部版本记录；内容表按**历代版本 Payload 的 slug 并集**清扫 Topic/Room（applyPayload 只 upsert 不删行，新版本移除的教程靠并集兜底清扫）；
- 连带清理：指向被删 Room 的 `challenges.tutorial_slug` 置空（题目本体保留）；被删 Room 的 `progresses` 行删除（派生表唯一例外写路径，见 event-sourcing-rules）；
- Event 表 append-only 不动（事件历史保留）；派生器 `room.completed` 有 Room 存在性守卫，重放不复活已删进度；
- draft 包从未发布、内容表无行，删除即仅清包与版本记录（同一入口，无需特判）；
- 内置训练集（vulinbox）不走内容包记录，无删除入口——重新一键导入即恢复。
