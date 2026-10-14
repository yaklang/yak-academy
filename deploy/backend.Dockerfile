# yak-academy 后端镜像：多阶段构建。
# 运行时镜像必须带 docker CLI——env 驱动通过 exec.Command("docker", ...) 管理题目容器，
# 配合 docker-compose 挂载宿主 /var/run/docker.sock（题目容器创建在宿主 daemon 上，
# 与本容器是兄弟关系，非嵌套）。
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# glebarez/sqlite 纯 Go 驱动：CGO 关闭可静态编译。
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/academy-server ./cmd/academy-server

FROM alpine:3.21
RUN apk add --no-cache docker-cli ca-certificates tzdata
COPY --from=build /out/academy-server /usr/local/bin/academy-server
# 数据卷挂载点（ACADEMY_DB=/data/academy.db）。
VOLUME /data
ENV ACADEMY_DB=/data/academy.db
EXPOSE 8080
ENTRYPOINT ["academy-server"]
