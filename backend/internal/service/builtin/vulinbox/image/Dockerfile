# vulinbox 基础镜像：yak-academy 内置训练集共用。
# 二进制为 yaklang 官方发行版（未修改），AGPL-3.0，见同目录 NOTICE。
# sha256 锁定官方 CDN latest 产物：上游更新会导致校验失败、构建 422——
# 这是刻意的（防止靶机行为漂移导致题目失配），更新题集时重新锁定即可。
#
# 分层：flaggateway（容器入口，:8787）→ vulinbox（上游，:8788）。
# 网关只做五件事：把平台按实例注入的 FLAG 写成 /flag.txt、给 GET /flag 加
# 按题门禁（CHALLENGE_SLUG 识别实例：SSRF 题只放行回环自请求、攻击题
# 观察到攻击成功信号后解锁、authz 题指路 cmd 接口）、给命令模拟接口补
# cat（仅 authz 题注册）、把 /ssrf/flag 演示接口的随机 uuid 改写为固定假
# flag{test}；其余路径原样反代上游，不改靶机行为。
FROM golang:1.26-alpine AS gateway

WORKDIR /src
# flaggateway 源码嵌在主 backend module 的 go:embed 树里（不能放嵌套 go.mod，
# 否则主模块 go test ./... 不再覆盖它），构建时在此合成最小 module。
COPY flaggateway/ ./
RUN printf 'module flaggateway\n\ngo 1.26\n' > go.mod \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/flaggateway ./

FROM alpine:3.21

ARG VULINBOX_BIN_URL=https://oss-qn.yaklang.com/vulinbox/latest/vulinbox_linux_amd64
ARG VULINBOX_SHA256=52f5a0db7f5f158424e15fd0a289b65fe86fc6deca6fdc0de60413c59131bcca

RUN apk add --no-cache ca-certificates wget \
    && wget -q --tries=3 --timeout=60 -O /usr/local/bin/vulinbox "${VULINBOX_BIN_URL}" \
    && echo "${VULINBOX_SHA256}  /usr/local/bin/vulinbox" | sha256sum -c - \
    && chmod +x /usr/local/bin/vulinbox

COPY --from=gateway /out/flaggateway /usr/local/bin/flaggateway
COPY NOTICE /usr/share/licenses/vulinbox/NOTICE

# --nohttps：训练环境关闭 https 相关行为；不加 --safe（保留完整漏洞面）。
EXPOSE 8787
# 上游监听容器回环 :8788，网关对外 :8787（平台端口映射指向网关）。
ENTRYPOINT ["/bin/sh", "-c", "/usr/local/bin/vulinbox --host 127.0.0.1 --port 8788 --nohttps & exec /usr/local/bin/flaggateway"]