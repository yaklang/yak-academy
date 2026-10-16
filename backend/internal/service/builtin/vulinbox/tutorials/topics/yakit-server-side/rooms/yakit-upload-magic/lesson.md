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
Host: 靶机地址
Content-Type: multipart/form-data; boundary=----Boundary

------Boundary
Content-Disposition: form-data; name="filename"; filename="shell.php"
Content-Type: image/gif

<文件内容>
------Boundary--
```

2. 文件内容 = `GIF89a` 魔数 + 换行 + 任意 PHP 代码（如 `<?php echo 1; ?>`）。可以直接在 Fuzzer 里手打，也可以先用 **Codec** 把"魔数+代码"的组合处理成需要的形态再粘贴；
3. 发送后读响应：上传成功时结果页会给出检测信息。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. 先上传一个纯 PHP 文件，观察被魔数校验拒绝的响应长什么样；
2. 再发多态文件（GIF 魔数开头），上传成功；
3. 题目要求提交结果信息里检测出的 MIME 类型值（`FileTypeHeaderType:` 后面的 `image/xxx` 形态）。

上传页在 `/upload/main?case=fileheader`，字段名 `filename`——这些细节从上传页的表单或 MITM 抓一次正常上传就能确认。

## 延伸：上传防御的正确姿势

魔数不是错，错在**只查魔数**。完整防御：白名单扩展名 + 魔数一致性 + 重命名存储（抹掉原始文件名）+ 上传目录剥离执行权限 + 隔离存储域。测试侧，Yakit 插件市场有现成的上传绕过插件（多态文件生成、解析漏洞探测），原理都建立在本课的手工构造之上。

至此四个专题通关——Web Fuzzer、MITM、Codec、fuzztag、反连、插件、扫描，Yakit 的核心武器库你都已上手。回到题库继续刷题巩固，或到 Yaklab 文档挑战更多靶机。
