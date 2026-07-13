# aitra-server

*English ∙ [简体中文](README_cn.md)*

**aitra-server** is the backend of the **Aitra** unified AI service platform — one control plane for all LLM traffic in your organization: integrate once through an OpenAI-compatible API, and get unified authentication, usage accounting, energy metering, and one-click model deployment on Kubernetes.

## Why Aitra

Enterprises adopting LLMs hit the same four walls:

- **Shadow AI** — teams bypass IT and call LLM APIs directly; spend, data flows, and risk go untracked
- **Vendor lock-in** — application code couples to a single provider; switching models means rewriting integrations
- **Cost & energy black box** — nobody can attribute money or power to a model, team, or request
- **Low GPU utilization** — coarse scheduling leaves clusters at 30–40% utilization

Aitra addresses all four with one Kubernetes-native platform.

## Features

| Capability | Status | Notes |
|---|---|---|
| OpenAI-compatible AI Gateway (`/v1/chat/completions`) | ✅ Running | Multi-provider dispatch, fallback, rate limiting, response caching |
| Model / dataset asset management (Git-first, LFS) | ✅ Running | Browse, version, and preview models & datasets |
| Usage accounting & chargeback | ✅ Running | Token usage + GPU-hours, per-namespace billing |
| Energy metering (J/token) | ✅ Running | Embeds [aitra-meter](https://github.com/aitra-ai/aitra-meter); see `energy/` |
| One-click model deployment | ✅ Running | Declarative DeployTask → Knative service, GPU-SKU-aware scheduling, scale-to-zero |
| Full-chain auditability | ✅ Running | Append-only audit logs, event-sourced billing |
| Task-aware smart routing | 🚧 In design | Route each request to the right model for its task; compliance rules first |

## Architecture

Four layers, all Kubernetes-native:

```
┌──────────────────────────────────────────────────────────────┐
│ Presentation   aitra-portal (Vue 3 + TypeScript)             │
├──────────────────────────────────────────────────────────────┤
│ Gateway        API Server :8080 (REST / Git HTTP)            │
│                AI Gateway :8094 (OpenAI-compatible entry)    │
├──────────────────────────────────────────────────────────────┤
│ Services       User · Accounting · Builder · Runner          │
│                Notification · Mirror · DataViewer · Cron     │
├──────────────────────────────────────────────────────────────┤
│ Infrastructure Kubernetes + Knative · PostgreSQL · Redis     │
│                NATS JetStream · MinIO/S3 · Gitea · Casdoor   │
└──────────────────────────────────────────────────────────────┘
```

Key directories:

- `aigateway/` — OpenAI-compatible inference gateway (dispatch, fallback, rate limiting, caching, health checks)
- `energy/` — J/token energy metering, built on the [aitra-meter](https://github.com/aitra-ai/aitra-meter) core
- `accounting/` — usage metering, billing, quotas
- `builder/`, `component/`, `api/` — asset management, image building, REST APIs
- `cmd/aitra-server/` — the main binary (server, migrations, workers)

## Related repositories

| Repo | What it is |
|---|---|
| [aitra-portal](https://github.com/aitra-ai/aitra-portal) | Web portal (Vue 3 + TypeScript + Vite) |
| [aitra-meter](https://github.com/aitra-ai/aitra-meter) | Standalone GPU/NPU energy-metering engine (J/token) |

## Quick start

Prerequisites: Go 1.25+, and a PostgreSQL / Redis / NATS / MinIO / Gitea stack — `docker-compose.yml` in this repo brings up the development dependencies.

For a full walkthrough — all services, the web portal, GPU model deployment on Kubernetes, and a production checklist — see the [Deployment Guide](docs/en/deployment.md).

```bash
# build
make build          # outputs ./bin/aitra-server

# run database migrations (config file: local.toml)
make migrate_local

# start the API server
./bin/aitra-server start server --config local.toml
```

The AI Gateway listens on `:8094` and speaks the OpenAI API:

```bash
curl http://localhost:8094/v1/chat/completions \
  -H "Authorization: Bearer $AITRA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "your-model", "messages": [{"role": "user", "content": "hello"}]}'
```

## Tech stack

Go 1.25 + Gin · PostgreSQL + Bun ORM · Redis · NATS JetStream · Kubernetes + Knative · OpenTelemetry + Prometheus + Loki

## License & acknowledgements

Apache License 2.0.

The asset-management architecture references the [OpenCSG CSGHub](https://github.com/OpenCSGs/csghub-server) project (Apache 2.0). Aitra focuses on the inference-operations layer: gateway, routing, accounting, energy metering, and deployment orchestration.
