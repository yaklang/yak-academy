# yak-academy

安全实训平台（Security Training Academy）：教程（文字+视频）与题目（独立 challenge.json 资源、可选关联教程）、用户权限管理、事件驱动的数据流。前后端分离（Vue3 前端 / Go 后端），所有内容来源于管理员上传，无内置内容。

## AI 协作规则（任何 AI 助手在本仓库工作前必读）

1. **任何代码改动前，必须先阅读 skill `project-conventions`**——它定义了分层依赖方向和目录职责，违反分层的改动一律重做。
2. 按任务涉及领域加载对应 skill（skill 位于 `.agents/skills/`，均为 SKILL.md 格式）：

| 任务涉及 | 必须加载的 skill |
|---|---|
| 任何代码改动 | `project-conventions` |
| 内容上传 / 教程解析 / 教程包或题目 JSON 格式 | `content-package-spec` |
| Room / Challenge / 训练场 / 学习路径设计 | `teaching-integration` |
| 用户 / 角色 / 接口鉴权 | `rbac-model` |
| 积分 / 进度 / 排行榜 / 判题 | `event-sourcing-rules` |
| 新增或修改 HTTP 接口 | `api-contract` |

3. **完整设计在 `docs/`**（architecture.md / data-flow.md / openapi.yaml）。skill 是约束的执行摘要，两者冲突时以 docs 为准并提交 issue 修正 skill。
4. 第一版的边界（不实现真实业务逻辑，仅契约 + 骨架 + stub）见 `docs/architecture.md` §演进路线。
