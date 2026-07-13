# aitra-server

*[English](README.md) ∙ 简体中文*

**aitra-server** 是 **Aitra** 企业级 AI 统一服务平台的后端——组织内所有 LLM 流量的统一控制平面：通过 OpenAI 兼容 API 一次接入，即可获得统一认证、用量计费、能耗计量，以及 Kubernetes 上的一键模型部署。

## 为什么做 Aitra

企业引入 LLM 时普遍会遇到四个问题：

- **Shadow AI 失控** —— 各团队绕过 IT 直连 LLM API，支出、数据流向、安全风险无人追踪
- **厂商锁定** —— 应用代码与单一 Provider 深度耦合，切换模型等于重写接入层
- **成本与能耗黑盒** —— 无法把钱和电归因到模型、团队或请求
- **GPU 利用率低** —— 粗放调度下集群利用率长期停留在 30–40%

Aitra 用一个 Kubernetes 原生平台同时解决这四个问题。

## 功能

| 能力 | 状态 | 说明 |
|---|---|---|
| OpenAI 兼容 AI 网关（`/v1/chat/completions`） | ✅ 已运行 | 多 Provider 分发、fallback、限流、响应缓存 |
| 模型 / 数据集资产管理（Git-first，LFS） | ✅ 已运行 | 浏览、版本管理、在线预览 |
| 用量计费与 chargeback | ✅ 已运行 | token 用量 + GPU 小时，按命名空间计费 |
| 能耗计量（J/token） | ✅ 已运行 | 内嵌 [aitra-meter](https://github.com/aitra-ai/aitra-meter)，见 `energy/` |
| 一键模型部署 | ✅ 已运行 | 声明式 DeployTask → Knative 服务，按 GPU SKU 调度，空闲缩容到零 |
| 全链路审计 | ✅ 已运行 | append-only 审计日志、事件溯源账单 |
| 按任务智能路由 | 🚧 设计中 | 根据任务自动路由至合适的模型，合规规则始终先行 |

## 架构

四层结构，全部 Kubernetes 原生：

```
┌──────────────────────────────────────────────────────────────┐
│ 展示层         aitra-portal（Vue 3 + TypeScript）            │
├──────────────────────────────────────────────────────────────┤
│ 网关层         API Server :8080（REST / Git HTTP）           │
│                AI Gateway :8094（OpenAI 兼容入口）           │
├──────────────────────────────────────────────────────────────┤
│ 业务服务层     User · Accounting · Builder · Runner          │
│                Notification · Mirror · DataViewer · Cron     │
├──────────────────────────────────────────────────────────────┤
│ 基础设施层     Kubernetes + Knative · PostgreSQL · Redis     │
│                NATS JetStream · MinIO/S3 · Gitea · Casdoor   │
└──────────────────────────────────────────────────────────────┘
```

核心目录：

- `aigateway/` —— OpenAI 兼容推理网关（分发、fallback、限流、缓存、健康检查）
- `energy/` —— J/token 能耗计量，基于 [aitra-meter](https://github.com/aitra-ai/aitra-meter) 核心
- `accounting/` —— 用量计量、计费、配额
- `builder/`、`component/`、`api/` —— 资产管理、镜像构建、REST API
- `cmd/aitra-server/` —— 主程序（服务、迁移、worker）

## 相关仓库

| 仓库 | 说明 |
|---|---|
| [aitra-portal](https://github.com/aitra-ai/aitra-portal) | Web 前端（Vue 3 + TypeScript + Vite） |
| [aitra-meter](https://github.com/aitra-ai/aitra-meter) | 独立的 GPU/NPU 能耗计量引擎（J/token） |

## 快速开始

前置要求：Go 1.25+，以及 PostgreSQL / Redis / NATS / MinIO / Gitea 依赖栈——仓库内的 `docker-compose.yml` 可一键拉起开发环境依赖。

完整部署教程（全部服务、Web 前端、Kubernetes GPU 模型部署、生产加固清单）见[部署指南](docs/zh-CN/deployment.md)。

```bash
# 构建
make build          # 产出 ./bin/aitra-server

# 执行数据库迁移（配置文件 local.toml）
make migrate_local

# 启动 API Server
./bin/aitra-server start server --config local.toml
```

AI 网关监听 `:8094`，直接使用 OpenAI API 格式：

```bash
curl http://localhost:8094/v1/chat/completions \
  -H "Authorization: Bearer $AITRA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "your-model", "messages": [{"role": "user", "content": "你好"}]}'
```

## 技术栈

Go 1.25 + Gin · PostgreSQL + Bun ORM · Redis · NATS JetStream · Kubernetes + Knative · OpenTelemetry + Prometheus + Loki

## 许可与致谢

Apache License 2.0。

资产管理部分的架构参考了 [OpenCSG CSGHub](https://github.com/OpenCSGs/csghub-server) 项目（Apache 2.0）。Aitra 专注于推理运营层：网关、路由、计费、能耗计量与部署编排。
