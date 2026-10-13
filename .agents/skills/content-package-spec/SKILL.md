---
name: content-package-spec
description: yak-academy 教程包与题目 JSON 格式唯一规范。涉及内容上传、zip 解析、Markdown front matter、challenge.json 题目定义、内容版本化或发布状态机时必须加载。
---

# 教程包与题目 JSON 规范（管理员上传是内容的唯一来源）

## 硬性原则

- **禁止任何内置/硬编码教程内容**。所有教程与题目都来自管理员上传（教程 zip 包 / 题目 challenge.json）。代码里不得出现示例课程数据、demo 文章常量。
- 唯一例外：数据库迁移可以创建初始管理员账号，但**不得**创建任何 Topic/Room/Challenge 初始数据。
- **教程包只含教程**（文字 + 视频）；题目不经教程包上传——旧 lab.yaml 模式已废弃（解析代码仅为旧版本 Payload 回放兼容保留，apply 时忽略），新题目一律走 challenge.json。

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

## 题目定义（challenge.json，固定格式）

单对象或对象数组上传（`POST /admin/challenges`，admin，upsert 即生效，无 draft/publish 流程）：

```json
{
  "slug": "sqli-login-bypass",
  "title": "SQL 注入登录绕过",
  "category": "sql-injection",
  "difficulty": "apprentice",
  "description_md": "# 背景\n任务目标……",
  "score": 100,
  "challenge": { "type": "static", "flag": "flag{answer}" },
  "env": { "image": "vulnapp:1.0", "port": 80 },
  "hints": [ { "title": "提示一", "content": "看 UNION", "cost": 10 } ],
  "tutorial_slug": "sqli-basics"
}
```

- 必填：`slug`/`title`/`category`/`difficulty`/`score`/`challenge`；`description_md`/`env`/`hints`/`tutorial_slug` 可选。
- `challenge.type`：`static`（明文 flag）/ `regex`（可编译正则）。
- `env` 整段可选；**要么都填要么都不填**（只填其一 = 校验失败整体拒绝）。image 非空即 `has_env: true`，学生在题目详情页启动容器；无 env 的题目调实例接口返回 422。
- `hints[].cost` 非负；`tutorial_slug` 可选指向教程 Room（关联入口见 teaching-integration skill）。
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

## 状态机（唯一合法迁移，仅教程包）

包级：`draft → published → archived`。**禁止** draft 直接跳 archived；archived 为终态（包冻结，拒绝再发布/回滚）。

版本级（ContentVersion.Status，与包级同枚举）：

- Publish：目标版本 → published，包内其余版本 → archived（同时至多一个 published 版本）；
- Rollback（admin，`POST /content/packages/{id}/rollback`）：published 包回滚到某个 archived 版本——目标版本 Payload 覆盖重写内容表（只写 Topic/Room，含 video；旧 Payload 中的 Labs 段忽略），目标 → published、当前 → archived，`latest_version` 更新，包级保持 published。禁止回滚当前版本/draft 版本；
- 同名包重传新版本：既有版本 Payload 中的 slug 视为自有，不判 slug 冲突（他包占用的 slug 仍拒绝）。