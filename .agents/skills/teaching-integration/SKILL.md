---
name: teaching-integration
description: yak-academy 教题一体的内容模型约束。涉及 Room/Lab/Topic/LearningPath 数据模型或接口设计、学习路径、难度分级时必须加载，防止把教程与题目拆成两套体系。
---

# 教题一体模型（不可拆分）

## 硬性原则

- **Room = 教程 + 内嵌 Lab**，是学习的原子单元（参考 TryHackMe 的 Room 模式）。Room 有 lesson.md 正文，**至多一个**内嵌 lab.yaml。
- **禁止**建立独立的"教程库"和"题库"两套实体/两张表。Challenge 必然内嵌于 Room（外键 room_id），不存在无 Room 的 Challenge。
- 层级唯一：`Topic（主题，如 SQLi/XSS）→ Room（教程+Lab）`。Topic 下可以有纯教程 Room（无 Lab），但 Lab 必属于某 Room。

## 学习路径（LearningPath）

- **Path 只引用 Room ID**（PathItem = path_id + room_id + order），**禁止**在 Path 中复制/内联 Room 内容字段——内容归属 Room，路径只是有序视图（参考 PortSwigger：路径引用而非复制）。
- Path 的排序字段 `order` 必须在 path 内唯一，调整顺序只能改 order，不能动 Room。

## 难度分级（唯一枚举）

| 值 | 对标 |
|---|---|
| `apprentice` | 入门 |
| `practitioner` | 进阶 |
| `expert` | 专家 |

- 该枚举出现在 model、lab.yaml 校验、前端展示三处，**必须保持一致**，禁止新增第四级（如 "beginner"）。

## 正反例

✅ 正确：新建 Room 时同时解析 lesson.md 与可选 lab.yaml，入库为 Room + 关联 Challenge。
❌ 错误：设计独立的 `GET /challenges` 题库列表接口，允许脱离 Room 出题。
❌ 错误：LearningPath 表里存 room_title/lesson_content 冗余字段。