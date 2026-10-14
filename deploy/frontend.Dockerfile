# yak-academy 前端镜像：node 构建 → nginx 托管。
# 后端是纯 API 服务（router.go 不托管静态文件），SPA 路由与 /api 反代都由 nginx 完成。
FROM node:22-alpine AS build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
