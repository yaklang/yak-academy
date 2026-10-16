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
Host: 127.0.0.1:8080
Content-Type: application/json

{"key": "value"}
```

要点（最容易踩的坑就在第一条）：

- **`Host` 头只写「主机:端口」**（如 `127.0.0.1:36870`），**不要**带 `http://` 前缀、不要带路径——浏览器地址栏里的 `http://` 和路径分别拆到「请求行的路径」和目标地址里，Host 只剩主机和端口。Host 写成完整 URL 会导致请求根本发不出去（请求失败或服务端（代理）异常）；
- 请求行里的路径以 `/` 开始，不带主机名；
- JSON body 必须带 `Content-Type: application/json` 头；
- 请求行、头、空行、body 的顺序不能乱——头与 body 之间必须空一行。

## 开始实战

点击本页底部 **前往配套题目练习**，启动题目环境后，用 Web Fuzzer 向靶机的 `POST /vul/auth-bypass/unsafe/api/cmd` 发送 `{"command": "ls"}`——不带任何 Cookie 或 token，观察未授权接口直接返回的命令执行结果，并按题目要求找出 `secret.txt` 的文件大小。

两个提前说明，帮你少走弯路：

- 这个接口是靶场**模拟实现**的命令执行，**只支持 `ls` 一个命令**——发 `ls -l`、`cat flag.txt` 之类都会返回 `bash: <命令>: command not found`。而 `ls` 返回的本身就已是带权限、大小、属主的完整长列表，答案就在里面；
- 列表里的 `flag.txt` 是个"摆设"——没有任何命令能读到它，本题答案与它无关。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

在配套题目详情页点击「启动实验环境」，等待状态变为 running，复制页面给出的访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：在 Web Fuzzer 中粘贴下面的完整请求**

```http
POST /vul/auth-bypass/unsafe/api/cmd HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"command": "ls"}
```

注意 `Host` 只写 `127.0.0.1:36870`（不带 `http://`、不带路径），路径只在请求行里。

**第 3 步：点击发送，读取响应**

状态码 `200`，body 是 JSON：

```json
{"command":"ls","result":"total 36\ndrwxr-xr-x  4 root root 4096 Dec 15 10:30 admin\ndrwxr-xr-x  3 user user 4096 Dec 15 10:30 documents\ndrwxr-xr-x  2 user user 4096 Dec 15 10:25 downloads\n-rw-r--r--  1 root root 1024 Dec 15 10:20 config.txt\n-rw-r--r--  1 root root  512 Dec 15 10:15 secret.txt\n-rw-r--r--  1 user user  256 Dec 15 10:10 readme.md\n-rw-r--r--  1 root root   64 Dec 15 10:05 flag.txt"}
```

在 `result` 里找到 `secret.txt` 那一行：`-rw-r--r-- 1 root root 512 ... secret.txt`——文件大小是 **512 字节**。

如果你发的命令带参数（如 `ls -l`、`cat flag.txt`），只会得到 `bash: ls -l: command not found` 之类的回显——该接口仅模拟实现了 `ls`，且 `ls` 输出本身已是长格式列表，无需第二个命令。

**第 4 步（可选对照）：发给安全版本体会差别**

把请求行换成 `POST /vul/auth-bypass/safe/api/cmd`（其余不变），响应变成：

```
未授权：缺少session
```

同样的命令接口，安全版本要求登录且具备 admin 角色——这就是"裸奔接口"与"有校验接口"的差别。

**第 5 步：提交答案**

回到题目详情页，在答案框提交 `512`（`secret.txt` 的字节数）。

</details>

## 延伸：打点前的侦察

实战中"未授权接口"要先被发现。Yakit 工具集里的 **端口扫描** 可以先摸清目标开放的服务，**目录扫描** 可以枚举隐藏路径。对已抓到流量的站点，MITM 的爬虫会把接口自动整理成站点树——这些能力在本专题后续教程会陆续上手。

下一课：用 MITM 劫持真实浏览器流量，在半路改掉一个请求。
