---
title: Codec 编码链：把口令安全送到验证端点
difficulty: apprentice
---

# Codec 编码链：把口令安全送到验证端点

## 原理背景

编码不是"加密"，它**无密钥、可逆、无处不在**：URL 里参数要转义、JSON 里特殊字符要处理、HTTP 头里非 ASCII 要编码。对安全测试者来说，编码是**穿越解析边界的交通工具**——payload 想穿过 URL 参数、穿过 JSON 字符串而不被中途截断，靠的是每一层都用对编码。

最容易翻车的一环是 **base64 与 URL 的组合**：base64 的输出里可能出现 `+`、`/`、`=`，而它们在 URL 查询参数里全是"特殊字符"——`+` 会被服务端解析成**空格**，`=` 是键值分隔符。把 base64 结果原样塞进 URL，到达服务端时已经不是你发出的那串了。正确做法是再做一次 URL 编码：`+` 变 `%2B`、`=` 变 `%3D`。少这一步，payload 死在半路。**编码链的每一环都不能错**，这正是 Codec 模块的价值：给你几十种变换的"翻译台"，编解码两个方向随手对照。

## Yakit 操作步骤

1. 打开工具集里的 **Codec 编码解码**；
2. 输入本关口令 `codec~proof`，选 base64 编码，得到 `Y29kZWN+cHJvb2Y=`——注意输出里带着 `+` 和 `=`，这两个字符直接放进 URL 都会出事；
3. 对 base64 结果**再做一次 URL 编码**：`+` 变 `%2B`、`=` 变 `%3D`，得到 `Y29kZWN%2BcHJvb2Y%3D`；
4. 反向验证：把第 3 步的结果 URL 解码、再 base64 解码，确认回到 `codec~proof`——**先编后解、闭环验证**，这是构造编码 payload 的标准自检动作；
5. 把编好的 token 放进 Web Fuzzer 的 URL 参数里发送。

## 开始实战

点击 **前往配套题目练习**，启动环境后：靶机为本次实例开了一个验证端点 `GET /academy/codec/verify?token=...`。用 Codec 把本关口令 `codec~proof` 依次做 **base64 编码 → URL 编码**，作为 `token` 参数请求该端点——编码链全部正确时端点返回 `ok:true` 并解锁 flag 读取；错任何一环（最常见：忘了 URL 编码，`+` 在半路变成空格）只会得到 400。解锁后 `GET /flag` 读出本实例真 flag（形如 `flag{lab-...}`），提交完成本题。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址。

**第 2 步：Codec 里构造编码链**

1. 输入口令 `codec~proof`，做 base64 编码，得到：`Y29kZWN+cHJvb2Y=`；
2. 对这个结果再做一次 URL 编码，得到：`Y29kZWN%2BcHJvb2Y%3D`。

对照实验（建议做一遍）：只做 base64、不做 URL 编码，把 `Y29kZWN+cHJvb2Y=` 原样当 token 发——服务端按 URL 规则把 `+` 解析成空格，base64 还原出口令变成 `codec proof`，与 `codec~proof` 不符，端点拒绝。差一步编码，结果完全不同。

**第 3 步：请求验证端点**

在 Web Fuzzer 发送（URL 里的特殊字符已编码，可整行照抄）：

```http
GET /academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D HTTP/1.1
Host: 127.0.0.1:36870
```

响应是 JSON：编码链正确时 `{"ok":true,...}`；错误时 HTTP 400，`{"ok":false,"message":...}` 会告诉你 token 不对。

**第 4 步：读取本实例 flag**

验证端点返回 `ok:true` 的同时已解锁 flag 读取（之前直接访问是 403）：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 5 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：编码即攻击面

除了 base64+URL 组合，同族技巧还有：双重 URL 编码（`%25` 解一次变 `%`，绕过只过滤一层的 WAF）、Unicode 归一化绕过（`％2e` 全角点）、JSON `\u002f` 转义、HTML 实体二次解码（存储型 XSS 常客）。遇到"输进去的 payload 莫名其妙变了"，第一反应永远是打开 Codec 对照——**输入时是 A，到达时是 B，中间差的每一步都是编码**。

模块专精专题完——Fuzzer 手搓、MITM 观察、Codec 翻译，三件兵器在手，回到攻击面专题继续深入。
