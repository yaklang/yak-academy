---
title: 业务链攻击：1 分钱买下商城商品
difficulty: practitioner
---

# 业务链攻击：1 分钱买下商城商品

## 原理背景

真实渗透不是"单点漏洞一把梭"，而是**把工具串成流水线**：代理看清业务 → 脚本自动改包 → 手搓完成最后一击 → 编码穿过校验边界。价格篡改是这套流水线最经典的舞台——服务端**信任了客户端提交的金额**，攻击者只要在下单请求到达服务端之前改掉金额，就能用任意价格买走商品。

本题要把商城里 **89 元** 的商品用 **1 分钱** 买下来，并读出本实例 flag。靶机会在网关层观察整条业务链：**MITM 观察、热加载改价、Fuzzer 标记头重放、Codec 编码链**四个信号全部命中，`/flag` 才解锁。四步可按任意顺序完成，但缺一步都不行。

## Yakit 四模块怎么用

**① MITM 代理观察**：系统（或浏览器插件）代理指向 Yakit MITM 监听地址，装一次证书（MITM 页有引导）。浏览器里的每一次登录、下单都会出现在 MITM 历史里——**先让真实业务流量跑起来，你在旁边看**，这是所有测试的标准开局。

**② 热加载自动改包（本题必经之路）**：MITM 页进入「热加载」/插件面板，新建 Yak 脚本。脚本对**每一个经过代理的请求**做检查改写，保存即生效——**不用断开代理、不用重启**，下一个经过的请求就带着你的改写逻辑。本题就用它把浏览器结算订单的金额自动换成 0.01。一段照抄即可用的完整脚本：

```javascript
// MITM 热加载脚本：自动把「提交订单」的金额改写为 0.01。
// beforeRequest 是 Yakit MITM 热加载的请求劫持钩子：req 是经过代理的
// 原始请求报文，返回值就是真正发给服务端的请求——保存即生效
beforeRequest = func(req) {
    // 只处理「提交订单」这一条请求，其余流量原样放行
    if str.Contains(string(poc.GetHTTPPacketFirstLine(req)), `POST /mall/order/submit`) {
        body = string(poc.GetHTTPPacketBody(req))
        // 替换串必须与真实 body 原文一字不差（紧凑 JSON、金额无小数位）
        body = str.ReplaceAll(body, `"TotalPrice":"89"`, `"TotalPrice":"0.01"`)
        // 写回报文，Content-Length 由 poc 帮你自动重算
        req = poc.ReplaceHTTPPacketBody(req, body)
    }
    return req
}
```

注意替换规则要对准 body 的**确切原文**：先用 MITM 历史抓一次真实结算，看清金额字段名和当前值（本题是 `TotalPrice`，单件 89 元商品结算时值就是 `89`），再对照写替换规则——**观察在先，改写在后**。你的购物车小计和示例不同（比如买了两件），就以抓到的实际值为准。

**③ Web Fuzzer 手搓重放**：右键 MITM 历史里的订单请求「发送到 Web Fuzzer」，在请求头区手工新增一行 `X-Mall-Internal: yakit-order`（订单系统只认内部通道，浏览器表单发不出这种自定义头），发送。整条请求（方法、路径、头、体）都在你手里——这正是 Fuzzer 与浏览器的本质差别。

**④ Codec 编码链**：订单系统还有一道内部校验：口令 `codec~proof` 要先 base64 编码、**再** URL 编码，作为 `token` 参数请求校验端点。base64 结果 `Y29kZWN+cHJvb2Y=` 里的 `+` 在 URL 查询参数中会被解析成**空格**、`=` 是键值分隔符——不做 URL 编码，口令在半路就被改写，端点只会 400。**编码链的每一环都不能错**。

## 开始实战

点击 **前往配套题目练习**，启动环境后按链路走：浏览器挂代理登录商城并结算一次订单（观察）；写热加载脚本自动改价后浏览器再结算一次（tamper）；Fuzzer 重放订单并加 `X-Mall-Internal: yakit-order` 头（replay）；最后用 Codec 把口令 `codec~proof` 经 base64 → URL 编码送到校验端点 `GET /academy/codec/verify?token=...`（codec）。四步信号齐备后 `GET /flag` 读出本实例真 flag 并提交。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址。

**第 2 步：MITM 观察——真实登录 + 原价结算**

浏览器代理指向 Yakit MITM，访问环境首页进「购物商城」（`/mall/shop/index`）：

1. 商城首页的 MacBook 是摆设（「加入购物车」没实现，点了只提示登录）——**真正的卖场在登录之后**。进「Login」页用预置账号 `user1` / `password123` 登录，登录成功自动跳到个人页（`/mall/user/profile?id=3`），这里的商品 1～12（每件 89 元）才能买；
2. 个人页点任一商品的「加入购物车」，再进购物车页（My Cart）点「提交订单」；
3. 在 MITM 历史里找到 `POST /mall/order/submit`——**看它的 body**：浏览器结算订单长这样（金额在 `OrderItems` 每一项的 `TotalPrice` 里，这就是热加载脚本要改的字段。注意两点：字段值没有小数位，`89` 元就是 `"89"` 而不是 `"89.00"`；JSON 是紧凑格式，冒号后没有空格）：

```json
{"UserID":3,"OrderItems":[{"ProductName":"商品1","Quantity":"1","TotalPrice":"89"}]}
```

注意：直接在 Web Fuzzer 里手打 body 也能「订单提交成功」，但**网关只认浏览器结算订单被改价**（改价信号锚定了浏览器请求形态）——这就是为什么本题必须用热加载脚本，而不是手改重发。结算成功后购物车会被清空，重结算前要把商品再加回购物车（个人页 → 加入购物车）。

**第 3 步：热加载——挂上自动改价脚本**

MITM 页 →「热加载」，新建 Yak 脚本，贴入脚本（替换串对准第 2 步抓到的 body 原文；一次买多件就以抓到的实际金额为准，比如两件是 `"TotalPrice":"178"`）：

```javascript
// MITM 热加载脚本：自动把「提交订单」的金额改写为 0.01。
// beforeRequest 是 Yakit MITM 热加载的请求劫持钩子：req 是经过代理的
// 原始请求报文，返回值就是真正发给服务端的请求——保存即生效
beforeRequest = func(req) {
    // 只处理「提交订单」这一条请求，其余流量原样放行
    if str.Contains(string(poc.GetHTTPPacketFirstLine(req)), `POST /mall/order/submit`) {
        body = string(poc.GetHTTPPacketBody(req))
        // 替换串必须与真实 body 原文一字不差（紧凑 JSON、金额无小数位）
        body = str.ReplaceAll(body, `"TotalPrice":"89"`, `"TotalPrice":"0.01"`)
        // 写回报文，Content-Length 由 poc 帮你自动重算
        req = poc.ReplaceHTTPPacketBody(req, body)
    }
    return req
}
```

保存后**浏览器里**重新加购物车 → 提交订单——MITM 历史里这条结算请求的 body 中 `TotalPrice` 已是 `0.01`，响应仍是「订单提交成功」：价格篡改由脚本自动完成，你一个字都没手改。

**第 4 步：Fuzzer 重放——加上内部通道标记头**

右键 MITM 历史里这条 0.01 订单「发送到 Web Fuzzer」，在请求头区加一行 `X-Mall-Internal: yakit-order`，发送。整条请求（方法、路径、头、体）都在你手里——浏览器表单永远发不出这种自定义头，这是 Fuzzer 的活。

**第 5 步：Codec 编码链——通过内部校验端点**

打开工具集里的 **Codec 编码解码**：

1. 输入口令 `codec~proof`，做 base64 编码，得到 `Y29kZWN+cHJvb2Y=`——注意输出里带着 `+` 和 `=`，这两个字符直接放进 URL 都会出事；
2. 对 base64 结果**再做一次 URL 编码**：`+` 变 `%2B`、`=` 变 `%3D`，得到 `Y29kZWN%2BcHJvb2Y%3D`；
3. 请求校验端点：

```http
GET /academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D HTTP/1.1
Host: 127.0.0.1:36870
```

响应 JSON `{"ok":true,...}` 即编码链正确（裸发 base64 会得到 400——对照做一遍加深印象）。

**第 6 步：读取本实例 flag**

四步信号齐备后 `GET /flag` 自动解锁（之前直接访问是 403）：

```http
GET /flag HTTP/1.1
Host: 127.0.0.1:36870
```

响应就是 flag 原文（一行，`flag{lab-...}`）。

**第 7 步：提交答案**

回到题目详情页，在答案框输入刚读到的 flag 原文（`flag{lab-...}` 整串）提交。

</details>

## 延伸：流水线化是效率的分水岭

四步链里热加载是承上启下的枢纽：**观察**（MITM 历史）产出样本，**改写**（热加载脚本）把样本变成攻击载荷，**执行**（Fuzzer/批量重放）拿到结果。 Yakit 里还能把改写脚本、fuzztag 字典、批量重放组合起来，让整条流水线全自动跑——人工只负责判断哪条结果值得深挖。练熟这套组合，你就从"会用工具"进阶到了"指挥工具"。

模块专精专题完。回到攻击面专题，用流水线打法重新审视那些单点漏洞。