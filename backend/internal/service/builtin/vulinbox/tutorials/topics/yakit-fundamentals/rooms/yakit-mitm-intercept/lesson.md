---
title: MITM 劫持：一分钱买走购物车
difficulty: practitioner
---

# MITM 劫持：一分钱买走购物车

## 原理背景

**价格篡改**是业务逻辑漏洞的教科书案例：前端把商品和总价算好展示给用户，用户点"提交订单"时把总价一并回传，而后端**完全信任客户端提交的金额**、不做重算——那么把 `999` 改成 `0.01` 再提交呢？

要攻击"客户端提交"环节，最好的位置是流量经过的中间人代理。Yakit 的 **MITM 交互式劫持** 就是这样一个代理：浏览器流量先经过它，你可以逐个查看、拦截、修改请求后再放行，是 Burp Suite 式工作流的国产平替。

## Yakit 操作步骤

1. 左侧导航进入 **MITM 交互式劫持**，点击启动监听（记下监听端口，默认 `8083`）；
2. 浏览器把 HTTP 代理指向 `127.0.0.1:8083`（Yakit 内置浏览器则免去手工配置）；
3. 正常浏览目标站点：MITM 的 **历史** 标签里会滚动出现每一个请求；
4. 想改哪个包，就在 **拦截** 开启的状态下让它停下——请求会被挂起等待你编辑，改完点 **放行**。

关键认知：**凡是浏览器能发出的请求，你都能在 MITM 里改了再发**——价格、数量、用户 ID、权限字段，无一例外。

## 开始实战

点击 **前往配套题目练习**，启动环境后：用预置账号 `user1` / `password123` 正常登录靶场「购物商城」，提交一笔正常订单，在 MITM 历史里观察 `POST /mall/order/submit` 的请求体结构（登录态在 `sessionID` Cookie 里）；再次下单并**拦截**提交请求，把订单项里的总价字段 `total_price` 改成 `"0.01"` 后放行——看到「订单提交成功」即代表篡改成立。然后读取靶机 flag（`GET /flag`）并提交，完成本题。

也可以不走拦截：在历史里右键该请求 **发送到 Web Fuzzer**，直接编辑 body 后重放，效果相同。

<details>
<summary>📋 详细步骤（点击展开，照做即可拿到答案）</summary>

以下以实例地址 `http://127.0.0.1:36870` 为例——**端口每次启动环境都随机分配，请替换成你题目页显示的端口**。走 MITM 拦截或 Web Fuzzer 重放都可以，下面用 Web Fuzzer 给出可完全复制的请求（MITM 路径只是把第 3 步换成"拦截后手改"）。

**第 1 步：启动实验环境**

题目详情页点「启动实验环境」，等状态 running，复制访问地址（形如 `http://127.0.0.1:36870`）。

**第 2 步：登录商城拿会话 Cookie**

在 Web Fuzzer 发送（或配好 MITM 代理后在登录表单里用 `user1` / `password123` 登录，从历史里找到同样的请求）：

```http
POST /mall/user/login HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json

{"username": "user1", "password": "password123"}
```

响应（`200`）：

```json
{"id":3,"success":true,"message":"Login successful"}
```

同时响应头里有 `Set-Cookie: sessionID=...`——这就是后续下单要带的登录态。（若用浏览器+MITM 走，Cookie 浏览器会自动带。）

**第 3 步：提交一笔"一分钱"订单**

把上一步响应里的 `sessionID` Cookie 值填进下面的请求再发送：

```http
POST /mall/order/submit HTTP/1.1
Host: 127.0.0.1:36870
Content-Type: application/json
Cookie: sessionID=fixedSessionID

{"user_id": 3, "order_items": [{"product_name": "iPhone", "quantity": "1", "total_price": "0.01"}]}
```

三个细节：字段是 **`user_id` + `order_items` 数组**；每项含 `product_name`、`quantity`、`total_price`；`quantity` 和 `total_price` 都是**字符串**。服务端对 `total_price` 只做转数字入库、不与商品定价核对——改价空间全在这里。

**第 4 步：读取响应**

状态码 `200`，body 是纯文本：

```
订单提交成功
```

（不带 Cookie 直接发第 3 步的请求，只会得到一段 `alert("请先登录")` 的跳转脚本——登录态校验是过的，价格校验是没有的。）

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

## 延伸：多包联动

登录 → 下单是有先后依赖的两步（第二步要带第一步的 Cookie）。Web Fuzzer 的**多包联动序列**可以把多个请求编成一组按序执行、自动传递变量，适合把"登录拿会话 → 篡改下单"固化成一键复测脚本。此外，MITM 的**热加载**还支持用 Yak 脚本对流量做自动改写，感兴趣的可以读 Yak 语言文档入门。

下一课：不拦截，只观察——用 Fuzzer 与 Codec 看清 XSS 与转义。
