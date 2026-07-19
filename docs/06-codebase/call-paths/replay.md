# Replay

```text
POST /v1/admin/replay
  -> authenticate admin request
  -> choose WAL range/checkpoint
  -> WAL.Scan
  -> Runtime reprocess Event
  -> canonical materialization
  -> retrieval projection
  -> tracker/checkpoint advance
  -> replay summary
```

FileWAL scan error必须传出。Replay 的重入正确性依赖 deterministic IDs 和 materializer 不变量。
