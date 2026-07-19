# First Event And Query

以下示例使用 Dynamic Event v0.4，并以 Event 作为 WAL 和物化链路的入口。

## 1. 写入 Event

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/ingest/events \
  -H 'Content-Type: application/json' \
  -d '{
    "schema_version": "plasmod.dynamic_event.v0.4",
    "identity": {
      "event_id": "evt_quickstart_001",
      "tenant_id": "tenant-quickstart",
      "workspace_id": "workspace-quickstart"
    },
    "actor": {
      "agent_id": "agent-quickstart",
      "session_id": "session-quickstart"
    },
    "time": {
      "event_time": 1767225600000,
      "logical_ts": 1
    },
    "event": {
      "event_type": "user_message",
      "importance": 0.8
    },
    "object": {
      "object_type": "memory"
    },
    "access": {
      "consistency": "strict",
      "visibility": "workspace"
    },
    "materialization": {
      "enabled": true,
      "targets": ["memory", "object_version"]
    },
    "retrieval": {
      "index_text": "The user prefers dark mode",
      "has_embedding": false
    },
    "payload": {
      "text": "The user prefers dark mode."
    }
  }'
```

当前默认 memory materializer 使用 `mem_` 加 `event_id` 生成 Memory ID，因此本例的对象 ID 是
`mem_evt_quickstart_001`。不要假设 `object.object_id` 会覆盖这一规则；显式对象 ID 在 Artifact 路径上
才由 `ArtifactIDOrDefault` 处理。

`has_embedding=false` 且提供 `index_text` 会跳过向量索引，但对象仍进入 canonical storage，且可以走
词法查询。这适合不依赖外部 embedding 的安装验证。

## 2. 精确查询对象

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{
    "query_text": "dark mode preference",
    "tenant_id": "tenant-quickstart",
    "workspace_id": "workspace-quickstart",
    "session_id": "session-quickstart",
    "agent_id": "agent-quickstart",
    "target_object_ids": ["mem_evt_quickstart_001"],
    "object_types": ["memory"],
    "top_k": 10,
    "response_mode": "structured_evidence"
  }'
```

响应的主要部分包括：

- `objects`：命中的 canonical objects；
- `edges`、`versions`、`provenance`：可恢复的关联证据；
- `proof_trace`：证据组装步骤；
- `retrieval_summary`：实际使用的检索层和过滤信息；
- `query_status`：查询是否完成或降级。

## 3. 查询追踪

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

Trace API 从 object、edge、version、policy 等 canonical records 组装结果，不等同于原始应用日志。
