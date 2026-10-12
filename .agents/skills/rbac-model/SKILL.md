---
name: rbac-model
description: yak-academy 双层 RBAC 权限模型约束。涉及用户、角色、登录、JWT、接口鉴权、管理后台权限设计时必须加载，防止散落式权限判断和越权接口。
---

# 双层 RBAC 模型（强制）

## 两层角色

1. **全局角色**（GlobalRole，作用于平台）：`student / teacher / admin`。teacher 是"内容作者/出题人"，admin 拥有用户管理与内容发布权。**禁止**新增全局角色而不更新本 skill。
2. **课程级角色**（CourseRole，作用于单个课程/Topic 域）：同一人在课程 A 可以是 teacher、在课程 B 是 student。

权限判定顺序：先全局，再课程级；admin 短路通过全部检查。

## 接口鉴权规则（强制）

- **每个接口必须声明所需角色**（路由注册时的声明式元数据，如 `r.RequireRole("teacher")`），由统一中间件执行。
- **禁止**在 handler 函数体内写 `if user.Role == ...` 这类散落权限判断。
- 资源属主判断（"只能看自己班级"这类规则）放 service 层，但角色/权限检查放中间件。

## 接口 → 最低角色速查（新接口必须对号入座）

| 操作 | 最低角色 |
|---|---|
| 浏览已发布教程、提交答案、查看自己进度 | student |
| 上传内容包、创建/编辑 Room 与 Lab | teacher |
| 发布内容、管理用户/角色 | admin |

- 鉴权失败统一返回 403 + 错误码（见 api-contract skill），**禁止**返回 404 掩盖权限问题。

## 正反例

✅ 正确：`POST /content/packages` 注册时声明 `RequireRole("teacher")`。
❌ 错误：handler 开头手写 `if currentUser.GlobalRole != "admin" { return 403 }`。
❌ 错误：用隐藏资源（404）代替 403 处理无权访问。