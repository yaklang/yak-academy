# 题目包样例（不会被平台自动加载）

本目录仅供管理员手工上传联调与文档对照，**不是**内置课程内容。

## 格式（实训常用 challenge.yml 约定）

```
<challenge-slug>/
├── challenge.yml          # 必填：元数据 / flag / hints / image+port
├── Dockerfile             # 有容器时必填
├── docker-compose.yml     # 有容器时必填（本地开发；平台按单容器 image/port 拉起）
└── src/                   # 可选：题目源码
```

上传接口：`POST /api/v1/admin/challenges/package`（multipart 字段名 `package`，内容为 zip）。

打包示例：

```bash
cd examples/challenges
zip -r vulinbox-xss-reflected.zip vulinbox-xss-reflected
```

## 样例：`vulinbox-xss-reflected`

对标 [Yaklang Vulinbox](https://yaklang.com/Yaklab/vulinbox/) 的反射 XSS（默认端口 `8787`），用最小 Python HTTP 服务模拟 `/xss/echo?name=`，便于验证：

1. zip 上传解析 `challenge.yml`（含 `flag_mode: instance` + `{token}` 模板）
2. `image: .` 上传时构建镜像，容器字段落库
3. 启动实例注入 `FLAG`；判题比对运行中实例 flag
4. hints 扣分解锁链路
