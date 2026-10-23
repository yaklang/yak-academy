---
name: api-contract
description: yak-academy API 契约开发规范。新增或修改任何 HTTP 接口时必须加载，防止契约与实现漂移、命名不一致、错误码混乱。
---

# API 契约规范（强制）

## 硬性原则

1. **`docs/openapi.yaml` 是接口的唯一契约源**。改接口必须先改契约，再改后端 handler，再改前端 `src/api/` 封装——三者顺序不可倒置。
2. **禁止**实现契约中不存在的接口，**禁止**契约中的接口长期无实现（第一版允许 stub 返回 501，但路由必须注册）。
3. 前端**只能**通过 `frontend/src/api/` 发请求，组件内**禁止**直接 fetch/axios。

## REST 命名约定

- 资源名用复数 kebab-case：多词资源用连字符，如 `/content/packages`。
- 层级不超过两级，更深用查询参数：`/topics/{topicSlug}/rooms`（✅），`/topics/{a}/rooms/{b}/labs/{c}`（❌）。
- 动作用子资源动词（POST）：`/submissions/{id}/rejudge` 仅限管理场景。

## 统一响应与错误码

成功：直接返回资源 JSON。错误：统一信封：

```json
{ "error": { "code": "FORBIDDEN", "message": "..." } }
```

| code | HTTP | 场景 |
|---|---|---|
| `UNAUTHORIZED` | 401 | 未登录 / token 失效 / 登录凭据错误 |
| `FORBIDDEN` | 403 | 角色不足（禁止用 404 掩盖） |
| `NOT_FOUND` | 404 | 资源不存在 |
| `VALIDATION_FAILED` | 422 | 参数/内容包校验失败 |
| `CONFLICT` | 409 | 状态冲突（如有通关记录不可删） |
| `INSTANCE_CONFLICT` | 409 | 单容器冲突（信封外附 `current`） |
| `NOT_IMPLEMENTED` | 501 | 骨架 stub / 缺运行时配置 |
| `INTERNAL` | 500 | 兜底 |

- 分页统一 `?page=1&page_size=20`，响应含 `total`。
- 时间统一 RFC3339 字符串（UTC）。

## 统一报错接入口（强制）

信封 `message` 一律是**面向用户的中文提示**，由唯一出口产生——`backend/internal/api/errors.go` 的 `writeError`/`writeConflict`。**每次新增错误输出必须：**

1. **走唯一出口**：handler 一律 `writeError(w, ...)` 或 `mapError(w, err)`，禁止自行 `Encode` 错误响应 / 绕过出口 `WriteHeader`。
2. **登记中文映射**：新错误文案必须在 `backend/internal/api/error_messages.go` 目录登记（固定文案进 `exactDetails`；带动态参数/位置前缀的进 `matchRules`，**更具体的模式必须排在更泛化的模式之前**；仅场景级差异才落 `codeDefaults`）。同时补 `error_messages_test.go` 表驱动用例。
3. **系统内置 error 日志自动记录**：出口每写一次错误响应必写一条 `slog` `api_error` 日志（英文原文 `message` + 中文映射 `message_zh` + `code`/`status` 同记；`ACADEMY_LOG_JSON=1` 时为 JSON 行）。这是唯一记录点，禁止 handler 重复打错误日志。
4. **前端禁止另起翻译层**：`src/api/` 抛出的 `ApiError.message` 已是中文，组件直接透出；不得在前端再写英文→中文映射函数。

契约侧同步：`docs/openapi.yaml` 的 `Error.message` 描述即此约束的对外声明，新增错误码必须同时补进 Error schema 的 enum 与上表。

## 鉴权

除 `POST /auth/login` 与 `POST /checkpoints/{token}`（环境检测点回调，token 能力鉴权，见 rbac-model skill）外全部需要 Bearer JWT。

## 正反例

✅ 正确：先在 openapi.yaml 增加 `GET /leaderboard`，再写 handler，再补 `src/api/leaderboard.ts`。
❌ 错误：handler 里加了个契约没有的 `/admin/exportAll` 接口。
❌ 错误：登录失败返回 `{"err": "bad"}`（不符合错误信封）。
❌ 错误：新增校验文案只在 handler 里写英文、不进 `error_messages.go` 目录（信封将透出英文兜底文案）。
❌ 错误：前端组件里写 `if (msg.includes('phone already registered')) ...` 英译中映射（翻译层只许存在于后端唯一出口）。
