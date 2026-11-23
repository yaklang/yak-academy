# yak-academy AWD SSH 攻击盒镜像。
#
# 构建（仓库根目录为上下文）：
#   docker build -f deploy/awd-attackbox.Dockerfile -t yak-awd-attackbox:latest .
# 或直接运行 deploy/build-awd-attackbox.sh（含镜像源/代理处理）。
#
# 运行时契约（DockerAwdRuntime.CreateAttackBox）：容器监听 22，读取
# USER_NAME / USER_PASSWORD 创建或更新登录用户；SSH 仅密码认证，禁用 root。
# 攻击盒先接入竞赛 edge 网络发布随机 SSH 端口，再接入 --internal 网络访问靶机。
FROM alpine:3.21

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG http_proxy
ARG https_proxy
ARG NO_PROXY
ENV http_proxy=$http_proxy https_proxy=$https_proxy no_proxy=$NO_PROXY

RUN apk add --no-cache \
      bash \
      curl \
      jq \
      netcat-openbsd \
      openssh-server \
      python3 \
      tcpdump \
 && mkdir -p /run/sshd \
 && rm -rf /var/cache/apk/*

ENV http_proxy= https_proxy= no_proxy=

COPY deploy/awd-attackbox/entrypoint.sh /usr/local/bin/awd-attackbox-entrypoint.sh
RUN chmod 0755 /usr/local/bin/awd-attackbox-entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/usr/local/bin/awd-attackbox-entrypoint.sh"]
