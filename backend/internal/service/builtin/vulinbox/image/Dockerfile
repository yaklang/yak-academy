# vulinbox 基础镜像：yak-academy 内置训练集共用。
# 二进制为 yaklang 官方发行版（未修改），AGPL-3.0，见同目录 NOTICE。
# sha256 锁定官方 CDN latest 产物：上游更新会导致校验失败、构建 422——
# 这是刻意的（防止靶机行为漂移导致题目失配），更新题集时重新锁定即可。
FROM alpine:3.21

ARG VULINBOX_BIN_URL=https://oss-qn.yaklang.com/vulinbox/latest/vulinbox_linux_amd64
ARG VULINBOX_SHA256=e3bb30a813546c0ca1e0b59cbde16e5490eda4e7dc7b655f9db2edf7a12e43ba

RUN apk add --no-cache ca-certificates wget \
    && wget -q -O /usr/local/bin/vulinbox "${VULINBOX_BIN_URL}" \
    && echo "${VULINBOX_SHA256}  /usr/local/bin/vulinbox" | sha256sum -c - \
    && chmod +x /usr/local/bin/vulinbox

COPY NOTICE /usr/share/licenses/vulinbox/NOTICE

# --nohttps：训练环境关闭 https 相关行为；不加 --safe（保留完整漏洞面）。
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/vulinbox", "--host", "0.0.0.0", "--port", "8787", "--nohttps"]
