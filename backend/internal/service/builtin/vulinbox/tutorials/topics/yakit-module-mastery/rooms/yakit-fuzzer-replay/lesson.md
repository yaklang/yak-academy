---
title: Web Fuzzer 重放：自定义请求头的攻与防
difficulty: practitioner
---

# Web Fuzzer 重放：自定义请求头的攻与防

## 原理背景

浏览器是个"听话"的客户端：表单提交只会带固定的头部集合（Cookie、Content-Type 等），**你想加一个自定义请求头，浏览器做不到**。而真实攻击与测试里，自定义头部几乎是家常便饭——

- 绕过只认识浏览器流量的弱 WAF：伪造 `X-Forwarded-For` 冒充内网来源；
- 测试内部接口：补一个 `X-Internal-Debug: true` 触发调试分支；
- 重放带签名的 API 请求：`X-Signature`、`X-Timestamp` 原样回放。

这正是 Web Fuzzer 存在的第一理由：**它让你以任意方法、任意头、任意体发送任意请求**，然后反复修改重放。

## Yakit 操作步骤

1. 打开 **Web Fuzzer**，粘贴一段最简单的请求模板（`Host` 只写 `主机:端口`，不带 `http://`）；
2. 在请求头区新增一行 `X-Replay: yakit`——观察它如何随请求一起发出；
3. 发送后回到请求编辑区，改头部值再发——**重放**就是"改一点、再发一次"，Fuzzer 的默认工作循环；
4. 打开 **History**（MITM 页的历史区也可以）：你刚发的每一条请求都被落库，可检索、可右键"发送到 Web Fuzzer"二次修改。

## 开始实战

点击 **前往配套题目练习**，启动环境后：对靶机的命令模拟接口重放 `{"command": "ls"}`，并给请求加上自定义头 `X-Replay: yakit-fuzzer`——服务端观察到"带了重放标记的合法命令请求"即判定你完成了 Fuzzer 动作，`/flag` 端点解锁。读出本实例 flag 并提交，完成本题。

> 为什么浏览器做不了：用浏览器访问该接口只能靠地址栏（GET），而该接口只认 POST + JSON 体；就算用开发者工具改体，也加不了自定义头。这道题从头到尾都是 Fuzzer 的主场。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：在 Web Fuzzer 手搓基础请求**

粘贴模板后发送，先确认接口本身可用：

```http
POST /vul/auth-bypass/unsafe/api/cmd HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"command": "ls"}
```

响应是 `ls` 的执行结果（带权限、大小、属主的完整长列表）。这一步没有任何攻击——只是确认重放对象活着。

**第 3 步：加重放标记头，重放**

在请求头区（`Content-Type` 那一行的下面）新增一行：

```http
X-Replay: yakit-fuzzer
```

保持 body 不变，再次发送。服务端这次观察到的是：**同一条命令请求，但携带了自定义重放标记**——浏览器发不出这样的请求，这是 Fuzzer 动作的证明。

**第 4 步：读取本实例 flag**

靶机为本次启动的环境单独生成了唯一 flag（形如 `flag{lab-xxxx}`）。读取端点 `GET /flag` 带门禁：重放标记动作成功前直接访问会得到 403（零操作拿不到 flag）；动作命中后端点自动解锁：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 5 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：头部安全测试清单

把自定义头当攻击面来测，是安全测试的固定科目：`X-Forwarded-For`（伪造来源 IP，测试 IP 白名单）、`X-Original-URL` / `X-Rewrite-URL`（绕过路由鉴权）、`X-HTTP-Method-Override`（方法降权绕过）、`X-Internal-*`（内部标记泄露）。Fuzzer 的 fuzztag（如 `{{x-forwarded-for(127.0.0.1)}}` 类字典）可以把一批候选头批量打进同一请求，一次执行看遍响应差异。

下一课：MITM——不手搓，先观察再复用。