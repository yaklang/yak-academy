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
| `UNAUTHORIZED` | 401 | 未登录 / token 失效 |
| `FORBIDDEN` | 403 | 角色不足（禁止用 404 掩盖） |
| `NOT_FOUND` | 404 | 资源不存在 |
| `VALIDATION_FAILED` | 422 | 参数/内容包校验失败 |
| `INTERNAL` | 500 | 兜底 |

- 分页统一 `?page=1&page_size=20`，响应含 `total`。
- 时间统一 RFC3339 字符串（UTC）。

## 鉴权

除 `POST /auth/login` 与 `POST /checkpoints/{token}`（环境检测点回调，token 能力鉴权，见 rbac-model skill）外全部需要 Bearer JWT。

## 正反例

✅ 正确：先在 openapi.yaml 增加 `GET /leaderboard`，再写 handler，再补 `src/api/leaderboard.ts`。
❌ 错误：handler 里加了个契约没有的 `/admin/exportAll` 接口。
❌ 错误：登录失败返回 `{"err": "bad"}`（不符合错误信封）。
