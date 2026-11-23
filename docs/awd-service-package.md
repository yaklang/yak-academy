# AWD 服务包规范（v1）

AWD 服务包是一个 zip，上传入口：

```
POST /api/v1/admin/competitions/{id}/awd/services/package
Content-Type: multipart/form-data
file=<zip>
```

仅 `format=awd` 且比赛状态为 `pending` 时可上传。服务包构建镜像后按 `slug`
upsert 服务定义；重复上传同一 `slug` 会更新定义并重建同名镜像。

## 目录结构

```
awd-service.json
Dockerfile
checker/...          # 可选：checker 脚本/二进制，随镜像构建
```

`awd-service.json` 与 `Dockerfile` 必须在同一目录；包内可有一层顶层目录
（如 `web/awd-service.json` + `web/Dockerfile`），构建上下文取该目录。

## awd-service.json

```json
{
  "slug": "web",
  "name": "Web 靶机",
  "ports": [
    { "name": "http", "port": 80, "protocol": "tcp" }
  ],
  "checker_config": {
    "type": "http",
    "path": "/health",
    "flag_path": "/flag"
  },
  "base_score": 100,
  "sla_weight": 100,
  "attack_points": 100,
  "defense_loss": 50,
  "checkdown_penalty": 50,
  "flag_lifetime_rounds": 1
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `slug` | 是 | `^[a-z0-9][a-z0-9-]{0,63}$`，竞赛内唯一 |
| `name` | 否 | 展示名，缺省 = slug |
| `ports` | 是 | 至少一个；`protocol` 缺省 tcp |
| `checker_config` | 否 | checker 配置对象；v1 支持 `http` / `tcp` / `command` |
| `base_score` | 否 | 默认 100 |
| `sla_weight` | 否 | 0..100，默认 100 |
| `attack_points` | 否 | 每回合攻击分池，默认 100 |
| `defense_loss` | 否 | 每个受害服务每回合扣分，默认 50 |
| `checkdown_penalty` | 否 | PUT/CHECK 失败直接扣分，默认 50 |
| `flag_lifetime_rounds` | 否 | 1..20，默认 1 |

## 镜像与安全

- 镜像 tag 固定为 `yak-awd-<competitionID>-<slug>`（重新上传覆盖）。
- 构建在部署主机执行，构建上下文是临时解包目录，构建完成后删除。
- zip 解包拒绝 `..`、盘符、UNC（`pathutil` 统一校验），限制 32MB 压缩包 /
  64MB 解压总量 / 2000 个文件。
- 服务容器运行约束（内存/CPU/pids/cap-drop/no-new-privileges、内部网络、
  不挂 Docker socket/宿主目录）由 AWD runtime 实施，不在服务包中配置。
