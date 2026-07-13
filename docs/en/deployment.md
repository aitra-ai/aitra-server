# Deployment Guide

This guide walks you through deploying the Aitra platform from scratch, in three stages:

1. **Single-machine quick start** — everything on one host, good for evaluation and development
2. **GPU model deployment** — connect a Kubernetes + Knative cluster for one-click model serving
3. **Production hardening** — checklist before going live

Aitra consists of two repositories:

| Repo | What it is |
|---|---|
| [aitra-server](https://github.com/aitra-ai/aitra-server) | Go backend (this repo) — one binary, multiple service processes |
| [aitra-portal](https://github.com/aitra-ai/aitra-portal) | Web portal (Vue 3 + TypeScript + Vite) |

## 1. Architecture at a glance

One binary (`aitra-server`) runs several cooperating processes, all sharing a single config file:

| Process | Command | Port | Role |
|---|---|---|---|
| API Server | `start server` | 8080 | REST APIs, Git HTTP, asset management |
| User Server | `user launch` | 8088 | Users, auth, orgs, LLM provider configs |
| AI Gateway | `aigateway launch` | 8094 | OpenAI-compatible entry (`/v1/chat/completions`) |
| Accounting | `accounting launch` | 8086 | Consumes metering events, writes billing records |
| Temporal Worker | `temporal-worker launch` | — | Executes deployment workflows |
| Runner | `deploy runner` | 8082 | Talks to Kubernetes, reconciles deployments (GPU stage only) |

Infrastructure dependencies (all provided by `docker-compose.dev.yml`):

| Service | Host port | Purpose |
|---|---|---|
| PostgreSQL 15 | 5434 | Primary database (`starhub_server`) |
| Redis 7 | 6379 | Cache, sessions |
| MinIO | 9000 / 9001 (console) | Object storage (LFS, artifacts) |
| Gitaly | 8075 | Git repository storage |
| NATS JetStream | 4222 | Event bus (metering events) |
| Temporal | 7233 | Workflow engine (deployments, sync jobs) |
| Casdoor | 8000 | SSO / identity provider |

## 2. Prerequisites

- Linux or macOS host with ≥ 8 GB RAM
- **Go 1.25+**
- **Node.js 20+** and npm (for the portal)
- **Docker** with the compose plugin
- (GPU stage) a Kubernetes cluster with **Knative Serving** and GPU nodes

## 3. Single-machine quick start

### 3.1 Clone the repositories

```bash
git clone https://github.com/aitra-ai/aitra-server.git
git clone https://github.com/aitra-ai/aitra-portal.git
```

### 3.2 Start the infrastructure

```bash
cd aitra-server
docker compose -f docker-compose.dev.yml up -d
```

Wait until all containers are healthy (`docker compose -f docker-compose.dev.yml ps`).

### 3.3 Create your config file

```bash
cp common/config/config.toml.example local.toml
```

Open `local.toml` and review at least these sections:

```toml
# Master API token — CHANGE THIS before any non-local deployment
api_token = "<generate a long random string>"

[database]
# Must match the port published by docker-compose.dev.yml (5434)
dsn = "postgresql://postgres:postgres@localhost:5434/starhub_server?sslmode=disable"

[redis]
endpoint = "localhost:6379"

[gitaly_server]
address = "tcp://localhost:8075"

[s3]
endpoint = "localhost:9000"
# access_key_id / access_key_secret must match your MinIO credentials

[jwt]
signing_key = "<generate another random string>"
```

> **Note:** the example config ships with placeholder credentials and a well-known
> `api_token`. They are fine for a laptop, but every value in the example file is
> public on GitHub — treat all of them as compromised in any shared environment.

### 3.4 Run database migrations

```bash
make build          # outputs ./bin/aitra-server
./bin/aitra-server migration init    --config local.toml
./bin/aitra-server migration migrate --config local.toml
```

(Equivalently: `make migrate_local`.)

### 3.5 Start the backend services

Each service is the same binary with a different subcommand. For a quick start,
run them in the background; for anything longer-lived, see the systemd units in §5.1.

```bash
./bin/aitra-server start server        --config local.toml &   # :8080
./bin/aitra-server user launch         --config local.toml &   # :8088
./bin/aitra-server aigateway launch    --config local.toml &   # :8094
./bin/aitra-server accounting launch   --config local.toml &   # :8086
./bin/aitra-server temporal-worker launch --config local.toml &
```

Notes:

- **Start all five.** The two easiest to forget are `accounting` (without it,
  token usage is never written to the billing tables) and `temporal-worker`
  (without it, model deployments are queued but never executed).
- After starting `user`, the auth components take a couple of minutes to warm
  up; transient 401s during that window are normal.

### 3.6 Start the portal

```bash
cd ../aitra-portal
npm install
npm run dev          # http://localhost:5173
```

For production, build static assets instead: `npm run build` (outputs `dist/`,
serve with nginx or any static server, proxying `/api/v1` to :8080/:8088).

### 3.7 Verify

```bash
# API server up?
curl http://localhost:8080/api/v1/models

# AI Gateway speaks OpenAI?
curl http://localhost:8094/v1/models -H "Authorization: Bearer <your api_token>"
```

Then open http://localhost:5173, register a user, and log in.

## 4. GPU model deployment (Kubernetes + Knative)

This stage gives you one-click model serving: the platform creates a Knative
service on your GPU cluster, and the AI Gateway routes Playground traffic to it.

### 4.1 Cluster prerequisites

- Kubernetes with **Knative Serving** (tested with 1.16.x) and the
  **Kourier** networking layer
- NVIDIA device plugin installed; `nvidia.com/gpu` allocatable on GPU nodes
- The cluster can pull (or already has) the inference images referenced by
  your runtime frameworks (vLLM / SGLang)

### 4.2 Deploy the runner

The runner is the only component that needs cluster credentials. Run it on a
node that has a kubeconfig (or in-cluster with a service account):

```bash
./bin/aitra-server deploy runner --config runner.toml
```

Key config points for the runner:

- `[database] dsn` must point at the **platform's** PostgreSQL — the runner
  writes cluster snapshots and deployment status back to the same database.
  A wrong host/port here makes the runner crash-loop with `no clusters found`.
- The kubeconfig it picks up determines which cluster it manages. Once it
  starts, it registers the cluster and begins pushing heartbeats; the node/GPU
  snapshot in the admin UI refreshes within a minute.

### 4.3 Point the platform at the cluster ingress

Playground traffic reaches deployed models through the Kourier gateway. The
cluster record's `app_endpoint` must match Kourier's actual NodePort:

```bash
kubectl get svc -n kourier-system kourier   # note the port mapped to 80
```

Set the cluster's app endpoint to `http://<any-node-ip>:<that-nodeport>`.
If this drifts (e.g. the service is recreated), Playground calls fail with
`upstream_unreachable` even though the model pod itself is healthy.

### 4.4 Resources and runtime frameworks

Two admin-managed tables drive the deploy form:

- **Space resources** — the GPU SKUs users can pick (e.g. `A100 80G ×8`).
  The GPU `type` string must **exactly match** what the node reports
  (e.g. `A100-SXM4-80GB`); a partial string fails the resource check with
  `TASK-ERR-4` even when GPUs are free.
- **Runtime frameworks** — the inference engines (vLLM / SGLang) with their
  images. Keep only versions that support your target models; newer model
  architectures need recent engines (e.g. Qwen3.5-MoE requires vLLM ≥ 0.17).

### 4.5 Deploy a model

From the model page, choose *Deploy* → pick a runtime framework, a resource,
and replica counts. The chain is:

```
API Server → Temporal workflow → temporal-worker → runner → Knative service → pod
```

When the pod is `Running`, the model shows up in the AI Gateway's `/v1/models`
and in the Playground. Model weights are downloaded from the platform's own
repository at container startup — first boot of a large model takes a while.

### 4.6 Troubleshooting the deploy chain

| Symptom | Likely cause |
|---|---|
| Deploy request accepted, nothing ever happens | `temporal-worker` not running |
| Status stuck / snapshot stale | runner down (check its DB DSN first) |
| `TASK-ERR-4` with free GPUs | GPU `type` string mismatch (§4.4), or a stale/zombie deploy record holding the resource |
| Playground: `upstream_unreachable`, pod healthy | `app_endpoint` NodePort drift (§4.3) |
| Knative pods crash-looping across nodes | CNI: cross-node pod traffic must actually route — with Calico on non-flat L2 networks, set encapsulation to `VXLAN` (Always), not `VXLANCrossSubnet` |
| Kourier gateway OOMkilled/SIGABRT on high-core nodes | Envoy spawns one worker per CPU and exhausts the container's 1024-FD limit; add `--concurrency 2` to its args |
| Image pull timeouts on air-gapped nodes | pre-load images and rely on `imagePullPolicy: IfNotPresent` |

## 5. Production checklist

### 5.1 Process supervision

Run each service as a systemd unit instead of `nohup`/`&`. Template:

```ini
[Unit]
Description=Aitra API Server
After=network.target postgresql.service

[Service]
User=aitra
WorkingDirectory=/opt/aitra/aitra-server
ExecStart=/opt/aitra/aitra-server/bin/aitra-server start server --config /opt/aitra/local.toml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Duplicate for `user launch`, `aigateway launch`, `accounting launch`,
`temporal-worker launch` (and `deploy runner` on the cluster side).

### 5.2 Security

- [ ] Regenerate `api_token` — the value in the example config is public
- [ ] Regenerate `[jwt] signing_key`, Gitaly tokens, MinIO credentials, Casdoor secrets
- [ ] Put the portal and APIs behind TLS (nginx/caddy reverse proxy)
- [ ] Restrict PostgreSQL/Redis/NATS/MinIO ports to the internal network
- [ ] Review AI Gateway rate limits (default 60 RPM per user) before load testing

### 5.3 Data

- [ ] Schedule PostgreSQL backups (all platform state lives there)
- [ ] Back up MinIO buckets (LFS objects) and Gitaly storage (git repos)
- [ ] Rehearse `migration rollback` before upgrading versions

### 5.4 Upgrades

```bash
git pull
make build                                            # new binary
./bin/aitra-server migration migrate --config local.toml
# restart services one by one; a running process keeps its old inode,
# so replacing the binary on disk is safe before the restart
```

## 6. FAQ

**Which services do I actually need?**
Minimum for "browse assets + call models through the gateway": `start server`,
`user`, `aigateway`. Add `accounting` for billing, `temporal-worker` + runner +
Kubernetes for one-click GPU deployment.

**Can I use an external model provider instead of my own GPUs?**
Yes — register any OpenAI-compatible endpoint as an external model under
admin → LLM configs; the gateway dispatches to it like any deployed model.

**Where is the energy metering (J/token)?**
Built into the API server (`energy/`), gated by `[prometheus] api_address`:
leave it empty to disable (default); point it at a Prometheus scraping
DCGM-exporter on your GPU nodes to enable. See the
[aitra-meter](https://github.com/aitra-ai/aitra-meter) docs for details.
