---
title: Web Fuzzer 第一个请求：手搓一个未授权 API 调用
difficulty: apprentice
---

# Web Fuzzer 第一个请求：手搓一个未授权 API 调用

## 原理背景

真实业务里有一类非常普遍的缺陷：**后端接口裸奔**。前端页面把按钮藏得好好的，只有登录后才渲染入口，但后端接口本身既不校验登录态、也不校验角色——谁直接调接口谁就能用。这类"未授权访问"漏洞不需要任何花哨技巧，只需要你能**手工构造并发送一个 HTTP 请求**。

这正是 Yakit **Web Fuzzer** 的定位：一个强大的 HTTP 请求工作台，构造、修改、重放请求，还可以用 fuzztag 做批量模糊测试（后者在后续教程会用到）。

## Yakit 操作步骤

1. 打开 Yakit，在左侧导航进入 **Web Fuzzer**，新建一个空白请求；
2. 在请求编辑区按 HTTP 报文原样输入（第一行是请求行，随后是头，空一行后是 body）；
3. 点击 **发送**，右侧即出现响应原文（状态码、响应头、body）。

一个最小请求模板：

```http
POST /path/to/api HTTP/1.1
Host: 目标地址
Content-Type: application/json

{"key": "value"}
```

要点：

- `Host` 头与目标一致，路径以 `/` 开始；
- JSON body 必须带 `Content-Type: application/json`；
- 请求行、头、空行、body 的顺序不能乱。

## 开始实战

点击本页底部 **前往配套题目练习**，启动题目环境后：

1. 用 Web Fuzzer 向靶机的 `POST /vul/auth-bypass/unsafe/api/cmd` 发送 `{"command": "ls"}`——不带任何 Cookie 或 token；
2. 观察响应：未授权接口直接返回了命令执行结果；
3. 题目要求提交结果里 `secret.txt` 的文件大小，可以再发一次 `{"command": "ls -l"}` 读取详细信息；
4. 对照体验：同样的请求发给安全版本 `/vul/auth-bypass/safe/api/cmd`，观察被拒绝的样子。

## 延伸：打点前的侦察

实战中"未授权接口"要先被发现。Yakit 工具集里的 **端口扫描** 可以先摸清目标开放的服务，**目录扫描** 可以枚举隐藏路径。对已抓到流量的站点，MITM 的爬虫会把接口自动整理成站点树——这些能力在本专题后续教程会陆续上手。

下一课：用 MITM 劫持真实浏览器流量，在半路改掉一个请求。
