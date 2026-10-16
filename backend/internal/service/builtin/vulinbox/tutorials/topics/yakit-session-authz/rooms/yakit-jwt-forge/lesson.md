---
title: JWT 伪造：alg=none 的免签冒充
difficulty: practitioner
---

# JWT 伪造：alg=none 的免签冒充

## 原理背景

JWT 把"身份声明"直接交给客户端保管：`header.payload.signature` 三段、以 `.` 分隔，前两段只是 **base64url 编码的 JSON**，不加密、人人可读。安全性全靠第三段签名——服务端用密钥验签，确认"这个 token 真是我发的、没被改过"。

`alg: none` 是最讽刺的实现缺陷：库允许 token 声明"我不需要签名"，服务端居然照办。于是**任何人都能签发任意身份的 token**。更妙的是本题靶场读身份读的是 header 里的字段——伪造面直接开在了最前面。

## Yakit 操作步骤

**Codec 是本课主角**：

1. 登录拿到合法 token（表单 `user1` / `password123` POST 登录）；
2. 打开 **Codec**，把 token 的第一段粘进去做 **base64 解码**——看到了 header 的 JSON 原文；对第二段如法炮制；
3. 反向操作：把你想要的 header JSON **base64 编码**，重新拼出伪造 token；
4. `alg=none` 时第三段签名为**空串**——token 以结尾的 `.` 收尾（`header.payload.`）。

**Web Fuzzer 验证**：带着 `Authorization: Bearer <伪造 token>` 请求资料接口，观察服务端认不认。

## 开始实战

点击 **前往配套题目练习**，启动环境后：

1. `POST /jwt/unsafe-login1`（表单字段 `username`/`password`）登录拿 token；
2. 用 Codec 拆开 token，确认 header/payload 结构；
3. 构造 `{"alg":"none","username":"root"}` 形态的新 header（身份声明写在 header 里），base64 编码后拼出 `新header.原payload.`（末尾保留点）；
4. 带 `Authorization: Bearer` 头请求 `/jwt/unsafe-login1/profile`，拿到 root 资料；
5. 题目要求按 `id:username` 格式提交返回中的这两个值。

## 延伸：用现成 POC 批量检测

插件市场里有大量 nuclei 兼容的 JWT 检测规则（弱密钥、alg 混淆、none 接受等）。从 **插件市场** 检索 JWT 相关插件，对靶机执行一次，观察它能否自动发现本课的缺陷——单点手工利用 vs 规模化检测，两者结合才是完整能力。修复视角：服务端必须硬编码允许的算法白名单，绝不信 token 自述的 `alg`。

下一专题：服务端攻击面——SSRF 与文件上传。
