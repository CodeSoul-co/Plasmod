# Quickstart

## 1. 启动

```bash
PLASMOD_STORAGE=disk PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

## 2. 检查健康状态

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

## 3. 写入和查询

按 [`first-event-and-query.md`](first-event-and-query.md) 写入 `evt_quickstart_001`，随后查询
`mem_evt_quickstart_001`。严格一致性写入返回成功时，该对象已通过当前一致性 gate。

## 4. 查看追踪

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

完整验证项见 [`verify-installation.md`](verify-installation.md)。
