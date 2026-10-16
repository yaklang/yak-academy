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

如果用户名输入 `admin' -- `，SQL 就变成了：

```sql
select * from users where username = 'admin' -- ' and password = '...'
```

`--` 把密码判断整个注释掉——查询只看用户名，恒能命中，服务端于是认定"登录成功"。这就是**万能密码**。根治方案是参数化查询（占位符），让用户输入永远是"值"而不可能变成"代码"。

## Yakit 操作步骤

登录是 POST 请求，浏览器里点一次登录按钮，抓下来再改是最顺的路径：

1. 启动 **MITM 交互式劫持**，浏览器配好代理；
2. 在登录表单里随便填个用户名密码，点登录——MITM 历史里出现 `POST` 登录请求；
3. 右键该请求 **发送到 Web Fuzzer**；
4. 在 Fuzzer 里只改 body 里的 `username` 字段为注入 payload，重发；
5. 观察响应：登录成功与否、返回的 JSON 结构。

也可以跳过 MITM，直接在 Web Fuzzer 手写整个 POST 请求（记得 `Content-Type: application/json`）。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. 进入靶场首页的「购物商城」，定位登录接口 `POST /mall/user/login`（JSON body 含 `username`、`password`）；
2. 用 Web Fuzzer 重放该请求，把 `username` 换成"闭合引号 + 恒真 + 注释"的万能密码形态（数据库是 SQLite，`--` 注释可用）；
3. 登录成功后，响应 JSON 中 `message` 字段的值就是本题答案；
4. **对照实验**：把同样的 payload 发给 Web 后台的 `/logic/user/login`——那里用了参数化查询，万能密码原样失效。同一个 payload，两种命运，这就是拼接与参数化的差别。

## 延伸：弱口令爆破

如果目标没有注入点，登录框还可以拼字典：Web Fuzzer 的 fuzztag（如 `{{file(用户名字典)}}` 配合 `{{file(密码字典)}}`）可以批量生成组合请求；Yakit 工具集里的 **弱口令爆破** 模块对常见服务（SSH/数据库/HTTP 表单）有现成模板。注入与爆破，是登录框攻防的两条基本线。

下一课：登录进去只是开始——用 UNION 把整个数据库拖出来。
