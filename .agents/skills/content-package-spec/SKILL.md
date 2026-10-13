---
name: content-package-spec
description: yak-academy 内容包（教程上传）格式唯一规范。涉及内容上传、zip 解析、Markdown front matter、Lab 定义 YAML、内容版本化或发布状态机时必须加载。
---

# 内容包规范（管理员上传是教程的唯一来源）

## 硬性原则

- **禁止任何内置/硬编码教程内容**。所有教程、题目定义都来自管理员上传的内容包（zip）。代码里不得出现示例课程数据、demo 文章常量。
- 唯一例外：数据库迁移可以创建初始管理员账号，但**不得**创建任何 Topic/Room/Challenge 初始数据。

## 内容包结构（zip）

```
package.zip
├── manifest.yaml        # 包元数据（必填）
├── topics/
│   ├── <topic-slug>/
│   │   ├── topic.md     # topic 首页教程（front matter 必填）
│   │   ├── rooms/
│   │   │   └── <room-slug>/
│   │   │       ├── lesson.md   # Room 教程正文（front matter 必填）
│   │   │       └── lab.yaml    # 内嵌 Lab 定义（Room 可选有一个）
```

## manifest.yaml 必填字段

`name`（包名）、`version`（语义化版本）、`language`、`topics`（topic slug 列表）。

## lesson.md front matter 必填字段

`title`、`difficulty`（apprentice/practitioner/expert 三级，禁止其他值）。

## lab.yaml 必填字段

`title`、`challenge.type`（static/regex）、`challenge.flag`、`score`（初始分值）。hint 为可选列表（title/content/cost）。

## lab.yaml 可选 env 段（容器环境）

```yaml
env:
  image: <docker 镜像>   # 非空即表示该 Lab 有容器环境
  port: <容器内端口 1-65535>
```

- 整段可选；**要么都填要么都不填**（只填其一 = 校验失败整体拒绝）。
- 有 env 的 Lab 在 Room 详情接口返回 `has_env: true`，前端才展示"启动环境"；无 env 的 Lab 调实例接口返回 422。

## 上传管线五步（顺序不可跳、不可合并）

```
Upload → Parse → Validate → Persist → Publish
```

1. **Upload**：接收 zip，校验大小与 MIME；
2. **Parse**：解压解析 manifest/topic/room/lab，解析失败整体拒绝（禁止部分入库）；
3. **Validate**：字段校验（front matter 必填、难度枚举、flag 格式、slug 冲突检测）；
4. **Persist**：写入 ContentPackage/ContentVersion，状态 = draft；
5. **Publish**：管理员显式发布，draft → published；已发布包再上传新版本 = 新 ContentVersion，旧版本可回滚（archived）。

## 状态机（唯一合法迁移）

包级：`draft → published → archived`。**禁止** draft 直接跳 archived；archived 为终态（包冻结，拒绝再发布/回滚）。

版本级（ContentVersion.Status，与包级同枚举）：

- Publish：目标版本 → published，包内其余版本 → archived（同时至多一个 published 版本）；
- Rollback（admin，`POST /content/packages/{id}/rollback`）：published 包回滚到某个 archived 版本——目标版本 Payload 覆盖重写内容表，目标 → published、当前 → archived，`latest_version` 更新，包级保持 published。禁止回滚当前版本/draft 版本；
- 同名包重传新版本：既有版本 Payload 中的 slug 视为自有，不判 slug 冲突（他包占用的 slug 仍拒绝）。
