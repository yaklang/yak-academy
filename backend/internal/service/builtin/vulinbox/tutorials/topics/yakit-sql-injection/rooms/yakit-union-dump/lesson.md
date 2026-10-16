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

1. UNION 两侧**列数**必须一致——先用 `ORDER BY n` 或 `UNION SELECT NULL,NULL,...` 探测；
2. 让原查询返回空（如 `id=-1`），页面才会显示 UNION 的结果。

## Yakit 操作步骤

全程 Web Fuzzer（请求是简单 GET，MITM 抓首页请求后"发送到 Web Fuzzer"亦可）：

1. 构造 `GET /user/id?id=1` 观察正常响应的 JSON 结构（几列、什么类型）；
2. 逐个发探测请求：`?id=1 ORDER BY 2`、`?id=1 ORDER BY 3`……报错与正常的交界处就是列数；
3. 换成 `?id=-1 UNION SELECT NULL,NULL,...` 确认位置对齐；
4. 把 NULL 换成真正的目标查询。

要批量试 payload 时，在 Fuzzer 里把注入点写成 fuzztag（如 `id=-1 UNION SELECT {{payload}}` 配合字典），一次执行生成整组请求——手工探测和理解原理，fuzztag 提效，先慢后快。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. 探测 `/user/id` 的列数（提示：表是 `vulin_users`，含 username/password/age/role 等列）；
2. 构造 UNION 查询读出 `username='root'` 那一行的密码列；
3. 提交密码原文即通关。注意用负数 id 让原查询空结果，UNION 的行才会出现在响应里。

## 延伸：验证与自动化

- **数据库客户端**：Yakit 工具集自带数据库连接能力，等你在后续题目拿到更多数据时可以直接连库核对；
- **漏洞检测**：对靶机跑一次主动扫描，观察 SQL 注入规则能否自动命中 `/user/id`——把它报的位置与你手工找到的注入点对照，感受自动化覆盖面；
- URL 编码遇到麻烦时随时开 **Codec**。

下一课：真实注入点往往裹着引号和括号——学会拆"包装纸"。
