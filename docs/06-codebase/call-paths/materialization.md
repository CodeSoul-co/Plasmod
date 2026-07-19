# Materialization

```text
Event + LSN
  -> materialization.Service.MaterializeEvent
  -> deterministic IDs
  -> Memory/Artifact/Edge/ObjectVersion records
  -> state/object specialized workers when targeted
  -> RuntimeStorage canonical projection
  -> retrieval projection
```

同一 Badger backend 中 object/edge/version 可使用一个事务。S3 和原生 index 不属于该事务，失败由 consistency
controller 记录并重试/报告。
