---
name: teaching-integration
description: yak-academy 教程与题目两类资源的内容模型约束。涉及 Challenge/Room/Topic 数据模型或接口设计、训练场、题目 JSON、难度分级时必须加载，防止教程与题目耦合回内嵌模式或拆成互不相通的两套体系。
---

# 教程与题目：两类独立资源 + 可选关联

## 硬性原则

- **教程与题目是两类独立上传的资源**：教程（Room，文字 + 视频）经教程包 zip 上传走 draft/publish 管线；题目（Challenge，固定格式 challenge.json）由 admin 上传 **upsert 即生效**。禁止把题目定义塞回教程包（旧 lab.yaml 模式已废弃，仅为旧 Payload 回放兼容保留解析）。
- **题目不内嵌于 Room**：Challenge 以自有 `slug`（uniqueIndex）为主键入口，含 `category`（训练场分组键）、`difficulty`、`description_md`、`env`、`hints` 等自身属性。不存在"Challenge 必须属于某 Room"的约束，纯题目合法。
- **关联是可选的单向引用**：Challenge.`tutorial_slug` 可选指向配套教程 Room。有关联时双向给入口（题目详情→"配套教程"，Room 详情→"前往题目"）；Room 侧的 `related_challenge_slug` 是按 TutorialSlug 反查的展示字段，不是存储的外键。禁止把题目字段（flag/env/hints）复制进 Room。
- 层级唯一：教程侧 `Topic → Room`；题目侧扁平（category 分组，无层级）。训练场（/training）**以题目为中心**：按 bank（ctf/acm/basic）分 tab、按 category 分组列表（Burp all-labs 风格），点入题目详情页完成容器启动/源码提交/客观题作答与通关，列表页保持纯浏览。
- **训练 Challenge 的题型**：flag 题（`static`/`regex`）、ACM 编程题（`code`）、基础题（`single`/`multiple`/`truefalse`/`fill`，`bank=basic`）。基础题定义落 1:1 `challenge_questions` 表，答案不下发；仅训练场可用，**禁止绑定 CTF 竞赛**（绑定与提交入口双拒）。多选部分分只作作答反馈，不计 XP/排行榜。

## 难度分级（唯一枚举）

| 值 | 对标 |
|---|---|
| `apprentice` | 入门 |
| `practitioner` | 进阶 |
| `expert` | 专家 |

- 该枚举出现在 model（Room 与 Challenge 各自持有）、题目 JSON 校验、教程包校验、前端展示四处，**必须保持一致**，禁止新增第四级（如 "beginner"）。

## 竞赛形式与内容模型的边界

- Competition 的 `format=ctf|awd|exam` 创建后不可更改；共享核心只含 Competition/Participant/Team/Profile/Qualification/Certificate。
- **CTF** 才绑定 Challenge（CompetitionChallenge/CompetitionScore/Writeup）；**AWD 与考试不得内嵌进 Room/教程包**，也不得复用 Challenge 的 flag 判定做攻防/考试判定：
  - AWD 的靶机服务用独立 `AwdService`/服务包（镜像+端口+checker），攻击 flag 按回合生成；
  - 考试的客观题用 `ExamQuestion`，编程题只能通过 Challenge 的 `exam` judge scope 复用判题引擎（不是把整场考试塞进一道题）；
  - 训练的**基础题（bank=basic）不得进入 CTF 竞赛**（`bindBySlugs` 与 `SubmitFlag` 双拒）；考试客观题继续走 `ExamQuestion`，禁止把 `ChallengeQuestion` 与考试域混用。
- AWD/考试/CTF 的模型、service、路由、前端视图互相独立；跨赛制只允许通过 `QualificationRankingSource` / `CertificateRankingSource` 接口协作。
- 训练场（/training）仍只服务 Challenge；竞赛入口 `/competitions/enter/{id}` 按 format 分发，禁止把 AWD 靶机或考试作答混进题目列表。

## 正反例

✅ 正确：题目 JSON 带 `tutorial_slug` 指向配套教程 Room，两边详情页互给入口。
✅ 正确：内置训练集一个教程对应一道内置题目（11 篇 Yakit 教程 ↔ 11 道 vulinbox 题），`tutorial_slug` 由内置导入的 challenges.json 注入；教程先于题目落库保证双向入口即刻生效（见 content-package-spec 例外二）。
✅ 正确：训练场列表只读展示（标题/难度/分值/容器/通关），交互集中在题目详情页。
✅ 正确：AWD 服务包独立上传，比赛页面从 `/competitions/{id}/awd` 取数。
❌ 错误：在教程包里恢复 lab.yaml / 让 Challenge 携带 room_id 外键（回退到内嵌模式）。
❌ 错误：把 AWD 靶机定义或考试试卷塞进 challenge.json / 教程包。
