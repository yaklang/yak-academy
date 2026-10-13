---
name: teaching-integration
description: yak-academy 教程与题目两类资源的内容模型约束。涉及 Challenge/Room/Topic/LearningPath 数据模型或接口设计、训练场、题目 JSON、学习路径、难度分级时必须加载，防止教程与题目耦合回内嵌模式或拆成互不相通的两套体系。
---

# 教程与题目：两类独立资源 + 可选关联

## 硬性原则

- **教程与题目是两类独立上传的资源**：教程（Room，文字 + 视频）经教程包 zip 上传走 draft/publish 管线；题目（Challenge，固定格式 challenge.json）由 admin 上传 **upsert 即生效**。禁止把题目定义塞回教程包（旧 lab.yaml 模式已废弃，仅为旧 Payload 回放兼容保留解析）。
- **题目不内嵌于 Room**：Challenge 以自有 `slug`（uniqueIndex）为主键入口，含 `category`（训练场分组键）、`difficulty`、`description_md`、`env`、`hints` 等自身属性。不存在"Challenge 必须属于某 Room"的约束，纯题目合法。
- **关联是可选的单向引用**：Challenge.`tutorial_slug` 可选指向配套教程 Room。有关联时双向给入口（题目详情→"配套教程"，Room 详情→"前往题目"）；Room 侧的 `related_challenge_slug` 是按 TutorialSlug 反查的展示字段，不是存储的外键。禁止把题目字段（flag/env/hints）复制进 Room。
- 层级唯一：教程侧 `Topic → Room`；题目侧扁平（category 分组，无层级）。训练场（/training）**以题目为中心**：按 category 分组列表（Burp all-labs 风格），点入题目详情页完成容器启动与 flag 提交，列表页保持纯浏览。

## 学习路径（LearningPath）

- **Path 只引用 Room**（PathItem = path_id + room_slug + order），**禁止**改为引用 Challenge，也禁止复制/内联 Room 内容字段——内容归属 Room，路径只是有序视图（参考 PortSwigger：路径引用而非复制）。资源分离后题目已独立，但学习路径语义仍是「教程进度序列」，不是训练场题单。
- Path 的排序字段 `order` 必须在 path 内唯一，调整顺序只能改 order，不能动 Room。
- 管理端 `POST /admin/learning-paths` 创建；学生端 `GET /learning-paths` 返回节点完成态（Progress 派生）。

## 难度分级（唯一枚举）

| 值 | 对标 |
|---|---|
| `apprentice` | 入门 |
| `practitioner` | 进阶 |
| `expert` | 专家 |

- 该枚举出现在 model（Room 与 Challenge 各自持有）、题目 JSON 校验、教程包校验、前端展示四处，**必须保持一致**，禁止新增第四级（如 "beginner"）。

## 正反例

✅ 正确：题目 JSON 带 `tutorial_slug` 指向配套教程 Room，两边详情页互给入口。
✅ 正确：训练场列表只读展示（标题/难度/分值/容器/通关），交互集中在题目详情页。
❌ 错误：在教程包里恢复 lab.yaml / 让 Challenge 携带 room_id 外键（回退到内嵌模式）。
❌ 错误：LearningPath 表里存 room_title/lesson_content 冗余字段。
