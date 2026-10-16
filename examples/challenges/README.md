# 题目包样例（不会被平台自动加载）

本目录仅供管理员手工上传联调与文档对照，**不是**内置课程内容。

## 第三方二进制分发声明（AGPL-3.0）

平台内置训练的 vulinbox 基础镜像（`backend/internal/service/builtin/vulinbox/image/`）在构建时从 yaklang 官方 CDN 下载 **未修改** 的 vulinbox 二进制（AGPL-3.0，版权归属 yaklang 项目）。yak-academy 以独立程序方式分发该二进制并附 NOTICE（镜像内 `/usr/share/licenses/vulinbox/NOTICE`），不引用、不链接其源码模块；上游源码见 <https://github.com/yaklang/yaklang>（`common/vulinbox`）。本目录的 `vulinbox-xss-reflected` 样例为平台自研的模拟服务，与该二进制无关。

## 格式

两种形态（见 `.agents/skills/content-package-spec`）：

```
<challenge-slug>/
├── challenge.json        # 形态 B：JSON 定义（选择文件上传，页面校验+编辑）
├── challenge.yml         # 形态 A：实训常用 zip 包约定（必填）
├── Dockerfile            # 有容器时必填
├── docker-compose.yml    # 有容器时必填（本地开发；平台按单容器 image/port 拉起）
└── src/                  # 可选：题目源码
```

上传接口：

- challenge.json：管理页「选择 challenge.json」→ 校验 + 编辑确认（`POST /admin/challenges`）
- zip 题目包：`POST /api/v1/admin/challenges/package`（multipart 字段名 `package`，内容为 zip）。

打包示例：

```bash
cd examples/challenges
zip -r vulinbox-xss-reflected.zip vulinbox-xss-reflected
zip -r sqli-login-bypass.zip sqli-login-bypass
```

## 样例：`vulinbox-xss-reflected`

对标 [Yaklang Vulinbox](https://yaklang.com/Yaklab/vulinbox/) 的反射 XSS（默认端口 `8787`），用最小 Python HTTP 服务模拟 `/xss/echo?name=`，便于验证：

1. zip 上传解析 `challenge.yml`（含 `flag_mode: instance` + `{token}` 模板）
2. `image: .` 上传时构建镜像，容器字段落库
3. 启动实例注入 `FLAG`；判题比对运行中实例 flag
4. hints 扣分解锁链路

## 样例：`sqli-login-bypass`（challenge.json 形态 + 检测点自动判定）

管理员在「题目管理」页选择 `challenge.json` 文件即可导入（页面校验格式并弹出编辑确认）。用最小 Python HTTP 服务模拟 `/login` 表单直拼 SQL，便于验证：

1. challenge.json 选择 → 前端校验 + 编辑弹窗 → 一键上传（`POST /admin/challenges`）
2. `flag_mode: instance` + `flag{sqli-{token}}` 模板
3. **服务端自动判定**：实例启动注入 `CHECKPOINT_TOKEN`/`CHECKPOINT_URL`；靶场内置 checker 在确认「SQL 注入绕过登录成功」后 POST 回调，平台自动判过计分（无需手动交 flag）
4. 容器镜像两条路（任选）：
   - 预构建：`docker build -t yak-academy/sqli-login-bypass:latest sqli-login-bypass`（challenge.json 引用该 tag，适合 JSON 通道）
   - zip 包通道：`zip -r sqli-login-bypass.zip sqli-login-bypass` 后经 `POST /admin/challenges/package` 上传（包内 `challenge.yml` 的 `image: .` 上传时自动构建同名 tag）

## 样例：`crypto-caesar-shift`（纯题目：无容器，static flag）

最简形态——`flag_mode: shared`、无 `env`、static flag，直接管理页选择 `challenge.json` 上传即可。便于验证：

1. 纯题目在训练场只渲染「flag 提交框」（无容器控制条），提交即判分
2. hints 扣分解锁（cost 5/10）对纯题目同样生效
3. 解题后重复提交不重复计分（分值只结算一次）
