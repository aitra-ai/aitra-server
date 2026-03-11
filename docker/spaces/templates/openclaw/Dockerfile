# OpenClaw Space - 沙盒容器镜像
# 生产：将 OpenClaw web 前端 + gateway 封装为独立 Space
FROM node:20-alpine AS builder
WORKDIR /app
# 此处 COPY 真实的 csg-portal 构建产物
# 开发阶段用 nginx demo 代替
FROM nginx:alpine
RUN mkdir -p /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY index.html /usr/share/nginx/html/index.html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
