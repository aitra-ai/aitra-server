# 部署指南

本指南分三个阶段带你从零部署 Aitra 平台:

1. **单机快速部署** —— 全部组件跑在一台机器上,适合评估与开发
2. **GPU 模型部署** —— 接入 Kubernetes + Knative 集群,实现一键模型服务
3. **生产加固** —— 上线前检查清单

Aitra 由两个仓库组成:

| 仓库 | 说明 |
|---|---|
| [aitra-server](https://github.com/aitra-ai/aitra-server) | Go 后端(本仓库)—— 一个二进制,多个服务进程 |
| [aitra-portal](https://github.com/aitra-ai/aitra-portal) | Web 前端(Vue 3 + TypeScript + Vite) |

## 1. 架构速览

一个二进制(`aitra-server`)以不同子命令跑出多个协作进程,共用同一份配置文件:

| 进程 | 命令 | 端口 | 职责 |
|---|---|---|---|
| API Server | `start server` | 8080 | REST API、Git HTTP、资产管理 |
| User Server | `user launch` | 8088 | 用户、认证、组织、LLM 提供商配置 |
| AI Gateway | `aigateway launch` | 8094 | OpenAI 兼容入口(`/v1/chat/completions`) |
| Accounting | `accounting launch` | 8086 | 消费计量事件,写账单记录 |
| Temporal Worker | `temporal-worker launch` | — | 执行部署工作流 |
| Runner | `deploy runner` | 8082 | 对接 Kubernetes,调和部署状态(仅 GPU 阶段需要) |

基础设施依赖(`docker-compose.dev.yml` 一键提供):

| 服务 | 宿主端口 | 用途 |
|---|---|---|
| PostgreSQL 15 | 5434 | 主数据库(`starhub_server`) |
| Redis 7 | 6379 | 缓存、会话 |
| MinIO | 9000 / 9001(控制台) | 对象存储(LFS、制品) |
| Gitaly | 8075 | Git 仓库存储 |
| NATS JetStream | 4222 | 事件总线(计量事件) |
| Temporal | 7233 | 工作流引擎(部署、同步任务) |
| Casdoor | 8000 | SSO / 身份认证 |

## 2. 环境要求

- Linux 或 macOS,内存 ≥ 8 GB
- **Go 1.25+**
- **Node.js 20+** 与 npm(前端需要)
- **Docker**(含 compose 插件)
- (GPU 阶段)装有 **Knative Serving** 的 Kubernetes 集群 + GPU 节点

## 3. 单机快速部署

### 3.1 克隆仓库

```bash
git clone https://github.com/aitra-ai/aitra-server.git
git clone https://github.com/aitra-ai/aitra-portal.git
```

### 3.2 启动基础设施

```bash
cd aitra-server
docker compose -f docker-compose.dev.yml up -d
```

等待所有容器健康(`docker compose -f docker-compose.dev.yml ps`)。

### 3.3 创建配置文件

```bash
cp common/config/config.toml.example local.toml
```

打开 `local.toml`,至少检查以下几段:

```toml
# 平台主 API token —— 任何非本机环境部署前务必更换
api_token = "<生成一个足够长的随机串>"

[database]
# 端口须与 docker-compose.dev.yml 发布的一致(5434)
dsn = "postgresql://postgres:postgres@localhost:5434/starhub_server?sslmode=disable"

[redis]
endpoint = "localhost:6379"

[gitaly_server]
address = "tcp://localhost:8075"

[s3]
endpoint = "localhost:9000"
# access_key_id / access_key_secret 须与你的 MinIO 凭据一致

[jwt]
signing_key = "<再生成一个随机串>"
```

> **注意:** 示例配置里带的是占位凭据和一个众所周知的 `api_token`,本机玩玩没问题,
> 但示例文件里的每个值都公开在 GitHub 上——在任何共享环境中都应视为已泄露、必须全部更换。

### 3.4 执行数据库迁移

```bash
make build          # 产出 ./bin/aitra-server
./bin/aitra-server migration init    --config local.toml
./bin/aitra-server migration migrate --config local.toml
```

(等价于 `make migrate_local`。)

### 3.5 启动后端服务

每个服务都是同一个二进制换子命令。快速体验可直接后台跑;长期运行请用 §5.1 的 systemd 单元。

```bash
./bin/aitra-server start server        --config local.toml &   # :8080
./bin/aitra-server user launch         --config local.toml &   # :8088
./bin/aitra-server aigateway launch    --config local.toml &   # :8094
./bin/aitra-server accounting launch   --config local.toml &   # :8086
./bin/aitra-server temporal-worker launch --config local.toml &
```

注意事项:

- **五个都要起。** 最容易漏的两个:`accounting`(不起则 token 用量永远不落账单表)、
  `temporal-worker`(不起则模型部署任务只入队、永远不执行)。
- `user` 启动后认证组件需要几分钟预热,窗口期内偶发 401 属正常现象。

### 3.6 启动前端

```bash
cd ../aitra-portal
npm install
npm run dev          # http://localhost:5173
```

生产环境改为构建静态资源:`npm run build`(产出 `dist/`,用 nginx 等静态服务托管,
并将 `/api/v1` 反向代理到 :8080/:8088)。

### 3.7 验证

```bash
# API server 起来了吗?
curl http://localhost:8080/api/v1/models

# AI Gateway 能按 OpenAI 协议应答吗?
curl http://localhost:8094/v1/models -H "Authorization: Bearer <你的 api_token>"
```

然后打开 http://localhost:5173,注册用户并登录。

## 4. GPU 模型部署(Kubernetes + Knative)

这一阶段实现一键模型服务:平台在 GPU 集群上创建 Knative 服务,AI Gateway 把
Playground 流量路由过去。

### 4.1 集群前提

- Kubernetes + **Knative Serving**(1.16.x 验证过)+ **Kourier** 网络层
- 已装 NVIDIA device plugin,GPU 节点的 `nvidia.com/gpu` 可分配
- 集群能拉取(或已预置)runtime framework 引用的推理镜像(vLLM / SGLang)

### 4.2 部署 runner

runner 是唯一需要集群凭据的组件,跑在有 kubeconfig 的节点上(或以 in-cluster
ServiceAccount 方式运行):

```bash
./bin/aitra-server deploy runner --config runner.toml
```

runner 配置要点:

- `[database] dsn` 必须指向**平台的** PostgreSQL —— runner 要把集群快照和部署状态
  写回同一个库。host/port 写错会导致 runner 以 `no clusters found` 崩溃循环。
- 它读到的 kubeconfig 决定它管理哪个集群。启动后会注册集群并推送心跳,
  管理后台的节点/GPU 快照一分钟内刷新。

### 4.3 配置集群入口地址

Playground 流量经 Kourier 网关到达已部署模型。集群记录的 `app_endpoint`
必须与 Kourier 实际的 NodePort 一致:

```bash
kubectl get svc -n kourier-system kourier   # 记下映射到 80 的端口
```

将集群 app endpoint 设为 `http://<任一节点IP>:<该NodePort>`。
如果它漂了(比如 service 被重建),即使模型 pod 本身健康,Playground 调用也会报
`upstream_unreachable`。

### 4.4 资源规格与推理框架

部署表单由两张管理员维护的表驱动:

- **资源规格(space resources)** —— 用户可选的 GPU SKU(如 `A100 80G ×8`)。
  其中 GPU `type` 字符串必须与节点上报值**完全一致**(如 `A100-SXM4-80GB`);
  写成子串会在 GPU 空闲时也报 `TASK-ERR-4` 资源不足。
- **推理框架(runtime frameworks)** —— 推理引擎(vLLM / SGLang)及其镜像。
  只保留支持目标模型的版本;新架构模型需要新引擎(如 Qwen3.5-MoE 需 vLLM ≥ 0.17)。

### 4.5 部署一个模型

在模型页点 *部署* → 选择推理框架、资源规格和副本数。整条链路是:

```
API Server → Temporal 工作流 → temporal-worker → runner → Knative 服务 → pod
```

pod `Running` 后,模型会出现在 AI Gateway 的 `/v1/models` 和 Playground 中。
模型权重由容器启动时从平台自己的仓库下载——大模型首次启动耗时较长。

### 4.6 部署链排障

| 症状 | 可能原因 |
|---|---|
| 部署请求受理了,但一直没动静 | `temporal-worker` 没在跑 |
| 状态卡住 / 资源快照过期 | runner 挂了(先查它的数据库 DSN) |
| GPU 明明空闲却报 `TASK-ERR-4` | GPU `type` 字符串不匹配(§4.4),或有僵尸部署记录占着资源 |
| Playground 报 `upstream_unreachable` 但 pod 健康 | `app_endpoint` NodePort 漂移(§4.3) |
| Knative 组件跨节点崩溃循环 | CNI 问题:跨节点 Pod 流量必须真正可路由——非扁平二层网络下 Calico 封装模式要设 `VXLAN`(Always),不能用 `VXLANCrossSubnet` |
| 高核数节点上 Kourier 网关反复崩溃 | Envoy 默认按 CPU 数起 worker,耗尽容器 1024 FD 上限;给其 args 加 `--concurrency 2` |
| 离线节点拉镜像超时 | 预置镜像,依赖 `imagePullPolicy: IfNotPresent` |

## 5. 生产加固清单

### 5.1 进程托管

每个服务用 systemd 单元托管,不要 `nohup`/`&`。模板:

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

为 `user launch`、`aigateway launch`、`accounting launch`、`temporal-worker launch`
(以及集群侧的 `deploy runner`)各复制一份。

### 5.2 安全

- [ ] 重新生成 `api_token` —— 示例配置里的值是公开的
- [ ] 重新生成 `[jwt] signing_key`、Gitaly token、MinIO 凭据、Casdoor 密钥
- [ ] 前端与 API 套 TLS(nginx/caddy 反向代理)
- [ ] PostgreSQL/Redis/NATS/MinIO 端口只对内网开放
- [ ] 压测前确认 AI Gateway 限流配置(默认每用户 60 RPM)

### 5.3 数据

- [ ] 定期备份 PostgreSQL(平台全部状态都在库里)
- [ ] 备份 MinIO 桶(LFS 对象)与 Gitaly 存储(git 仓库)
- [ ] 升级前演练 `migration rollback`

### 5.4 升级

```bash
git pull
make build                                            # 新二进制
./bin/aitra-server migration migrate --config local.toml
# 逐个重启服务;运行中的进程持有旧 inode,
# 先替换磁盘上的二进制再重启是安全的
```

## 6. 常见问题

**到底需要起哪些服务?**
只要"浏览资产 + 经网关调模型"的最小集:`start server`、`user`、`aigateway`。
要计费加 `accounting`;要一键 GPU 部署再加 `temporal-worker` + runner + Kubernetes。

**没有自己的 GPU,能接外部模型吗?**
可以——在管理后台的 LLM 配置里注册任意 OpenAI 兼容端点为外部模型,
网关会像调度自部署模型一样分发流量。

**能耗计量(J/token)在哪里?**
内置在 API server(`energy/`),由 `[prometheus] api_address` 门控:
留空即关闭(默认);将其指向抓取 GPU 节点 DCGM-exporter 的 Prometheus 即开启。
详见 [aitra-meter](https://github.com/aitra-ai/aitra-meter) 文档。
