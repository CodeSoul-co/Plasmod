# 12. 部署、运维、恢复与故障排查

---

覆盖原生与 Docker 部署、持久化、备份恢复、可观测性、安全加固和排障。

---

## 12.1. Backup And Restore

### 12.1.1. Backup scope

完整恢复至少考虑：

- Badger canonical data；
- `wal.log`；
- consistency checkpoint；
- derivation/policy logs；
- native index/segment metadata；
- S3/MinIO cold objects；
- effective configuration 和 binary version。

### 12.1.2. Backup procedure

1. 记录 latest LSN 和版本；
2. 暂停或隔离写流量；
3. 使用一致的 Badger backup/snapshot，而非复制正在变化的文件集合；
4. 复制 WAL/checkpoint；
5. 记录 S3 version/prefix；
6. 校验 checksum；
7. 恢复写流量。

### 12.1.3. Restore procedure

1. 在隔离目录恢复；
2. 使用兼容 binary；
3. 检查 Badger 可打开和 LatestLSN；
4. 根据 checkpoint replay；
5. rebuild 缺失 retrieval projection；
6. 验证 object/edge/version/trace/query；
7. 再切换流量。

---

## 12.2. Deployment Modes

### 12.2.1. Local unified

一个 HTTP listener (`127.0.0.1:8080`) 注册 management 和 data routes。适合本地开发，不提供网络隔离。

### 12.2.2. Split HTTP

Management `9091`、data API `19530`、gRPC `19531`。适合通过不同 network policy 暴露控制面和数据面。

### 12.2.3. Docker Compose

Plasmod + MinIO 的单机容器拓扑，提供可重复依赖环境。不是高可用集群。

### 12.2.4. Native process

直接运行 binary，外部管理 Badger data dir、S3、TLS gateway、service manager 和日志。

### 12.2.5. Current boundary

仓库包含上游分布式 control/stream 代码，但默认 `BuildServer` 仍是一个主动 runtime 进程。部署多副本共享同一
Badger 目录不是受支持的 HA 方案。

---

## 12.3. Docker Deployment

### 12.3.1. Split compose

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f plasmod
```

检查：

```bash
curl -fsS http://127.0.0.1:9091/healthz
```

### 12.3.2. Unified compose

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

### 12.3.3. Required overrides

正式环境至少覆盖：admin key、MinIO credentials、data volumes、resource limits、restart policy、APP_MODE、
network exposure 和 TLS gateway。

### 12.3.4. Upgrade

1. 备份 volume/S3；
2. 构建带固定 commit/tag 的镜像；
3. 在数据副本上验证旧目录；
4. 停止旧容器；
5. 启动新容器并检查 health/config/storage；
6. 验证 write/query/trace；
7. 保留可回滚旧镜像和备份。

---

## 12.4. MinIO And S3 Setup

### 12.4.1. Compose MinIO

```bash
docker compose up -d minio
docker compose logs -f minio
```

API 默认 9000，console 9001。创建专用 bucket，并为 Plasmod 使用最小权限 access key。

### 12.4.2. Plasmod configuration

设置 endpoint、bucket、region、access/secret、TLS 和 prefix 对应环境变量；随后检查：

```bash
curl -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:9091/v1/admin/storage
```

### 12.4.3. Verification

1. 导出一个受控对象；
2. 用 S3 client 检查 prefix/key；
3. 以 `include_cold=true` 查询；
4. 验证 edge/version/provenance 仍一致；
5. 不在验证完成前清理 Warm。

### 12.4.4. Production S3

启用 TLS、encryption、versioning、bucket policy、lifecycle 和访问日志。Credential 通过 secret store 注入。

---

## 12.5. Native Deployment

### 12.5.1. Build artifact

```bash
make cpp
make build
```

将 `bin/plasmod`、所需动态库、配置和 license notices 放入同一发布版本。使用 `otool -L`/`ldd` 检查链接。

### 12.5.2. Service account

使用非 root 用户，授予：

- data dir 读写；
- WAL/checkpoint 原子 rename 权限；
- 必需的 S3 network/credential；
- 监听非特权端口；
- 日志输出权限。

### 12.5.3. Process manager

使用 launchd/systemd/Kubernetes 管理 restart、signal、environment 和 stdout/stderr。Shutdown timeout 应大于
Plasmod consistency shutdown timeout，以便队列和 checkpoint 受控退出。

### 12.5.4. Readiness

`/healthz` 当前主要表示进程存活。上线流量前还应检查 storage、effective config、provider health 和一次受控查询。

---

## 12.6. Observability

### 12.6.1. Health

`/healthz` 检查进程。Readiness 还需检查 `/v1/admin/storage`、effective config、provider health 和查询。

### 12.6.2. Metrics

`/v1/admin/metrics` 需要 admin key。建议采集：

- ingest accepted/error/backpressure；
- WAL latest/committed/projected/visible LSN；
- queue depth、retry、projection failure；
- query status、tier、candidate/hit；
- Badger/S3 error 和容量；
- purge/reindex/replay task；
- provider health。

### 12.6.3. Logs

以 JSON 或可解析格式收集 stdout/stderr，关联 event ID、object ID、LSN、session 和 request。禁止收集 secret、
完整私有 payload 和原始向量。

### 12.6.4. Alerts

对持续 queue saturation、visible LSN 不推进、WAL/Badger error、磁盘不足、S3 auth failure、panic/restart 和
admin auth disabled 告警。

---

## 12.7. Operations Runbook

### 12.7.1. Start

1. 启动 S3/MinIO（若启用）；
2. 检查 data dir 和 secret；
3. 启动 Plasmod；
4. 检查 health/effective config/storage/provider；
5. 执行受控 write/query/trace；
6. 开放流量。

### 12.7.2. Stop

1. 从 load balancer 摘除；
2. 停止新写；
3. 等待 queue/visible LSN；
4. 发送 SIGTERM；
5. 等待 checkpoint/shutdown；
6. 确认端口和进程退出。

### 12.7.3. Daily checks

- health、restart count；
- disk/WAL/Badger size；
- visible lag/queue/retry；
- S3/provider health；
- failed admin task；
- backup freshness。

### 12.7.4. Incident evidence

保存版本、effective config（脱敏）、LSN/checkpoint、相关 event/object ID、错误首发时间、日志和资源状态，再进行
repair。不要先删除 data dir 或重建所有索引。

---

## 12.8. Replay And Recovery

### 12.8.1. Failure classes

- 进程中断：从持久 store/checkpoint/WAL 继续；
- canonical projection 不完整：replay Event range；
- retrieval index 丢失：从 canonical/embedding 重建；
- cold store 不可用：Hot/Warm 可继续，显式 cold query 降级/失败；
- WAL 损坏：停止自动恢复，使用备份并确定最后可靠 LSN。

### 12.8.2. Recovery order

1. 阻止新写入；
2. 备份故障现场；
3. 检查 disk、Badger、WAL 和 checkpoint；
4. 启动依赖；
5. 以兼容 binary 打开 store；
6. replay 必需范围；
7. rebuild retrieval；
8. 验证 strict write、latest state、trace 和 cold query；
9. 恢复流量。

直接 CRUD 历史没有 Event 时只能依靠 canonical backup，不能通过 WAL replay 恢复。

---

## 12.9. Security Hardening

### 12.9.1. Mandatory controls

1. `APP_MODE=prod`；
2. 设置强 `PLASMOD_ADMIN_API_KEY`；
3. TLS reverse proxy/service mesh；
4. 数据 API 身份认证和 tenant/workspace 强绑定；
5. admin/internal/transport 仅私网；
6. request body/rate/concurrency 限制；
7. 非 root、只授必要文件和网络权限；
8. S3/MinIO 最小权限和 secret rotation；
9. 日志脱敏；
10. 定期恢复演练和依赖漏洞扫描。

### 12.9.2. Network segmentation

- management port 只对 operator/control plane；
- data port 只对应用 gateway；
- gRPC/transport 只对受信节点；
- MinIO console 不对应用网络；
- Badger data dir 不通过共享文件服务公开。

### 12.9.3. Known gap

内建 admin key 不是完整 IAM，internal route 也不受其保护。必须依赖部署层补齐。

---

## 12.10. Storage Configuration

### 12.10.1. Disk mode

```bash
PLASMOD_STORAGE=disk
PLASMOD_DATA_DIR=/var/lib/plasmod
```

目录包含 Badger、`wal.log`、`consistency_checkpoint.json` 和 `derivation.log`。需要低延迟、可靠 fsync 和足够
容量，不应放在临时文件系统。

### 12.10.2. Memory mode

```bash
PLASMOD_STORAGE=memory
```

所有 records/WAL 随进程退出消失，不用于需要恢复的环境。

### 12.10.3. Capacity

容量规划分别考虑 canonical payload、Badger value log、WAL retention、native indexes、hot cache 和 S3 cold data。
监控可用空间，避免 Badger/WAL 同时因磁盘满失败。

### 12.10.4. Single writer

不要让多个进程直接共享一个 Badger data directory。多实例需要明确的分布式存储/协调实现，而不是网络文件系统锁。

---

## 12.11. Operations Troubleshooting

### 12.11.1. Service unavailable after restart

检查 Docker/进程、端口、Badger lock、data dir 权限、native dynamic libraries 和依赖服务。

### 12.11.2. Writes accepted but not visible

查看 consistency metrics、queue、projection failure、embedder/native backend、checkpoint；按 Event ID 查询
canonical object 和 trace，区分物化与索引失败。

### 12.11.3. Latest state is old

确认 scope/state key、Event logical order、state version、materializer status 和 query filter。向量 top-1 不是 latest。

### 12.11.4. Cold query fails

检查 `include_cold`、S3 endpoint/TLS/credential/bucket/prefix、对象 key 和 edge index。

### 12.11.5. Disk usage grows

分别检查 Badger value log、WAL retention、native segments、purge queue 和 cold archive。不要直接删除任一子目录。

### 12.11.6. Admin returns 401

确认 `PLASMOD_ADMIN_API_KEY` 与 header，注意 split 模式 admin route 在 9091。不要为排错永久关闭认证。
