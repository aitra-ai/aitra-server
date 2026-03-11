# OpenClaw Space Template

这个模板把 OpenClaw 作为一个 Space 应用运行在沙盒容器中。

## 使用场景
- 用户无需自己部署，直接在 Spaces 平台体验 OpenClaw
- 每个用户/会话有独立隔离的容器实例
- 支持热池（Hot Pool）预热，用户点击即用

## 镜像构建

```bash
docker build -t opencsg/openclaw-demo:latest \
  -f docker/spaces/templates/openclaw/Dockerfile \
  docker/spaces/templates/openclaw/
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| OPENCLAW_API_KEY | API 密钥（接入 csghub-server） | - |
| OPENCLAW_BASE_URL | csghub-server 地址 | http://host.docker.internal:8088 |
| OPENCLAW_TITLE | 页面标题 | OpenClaw Demo |

## 本地开发

开发阶段使用 `openclaw-local` 模板（基于 nginx:alpine），
启动时会在 `http://localhost:32xxx` 显示一个 Demo 页面，
用于测试沙盒启动/停止/TTL 流程，无需构建真实镜像。
