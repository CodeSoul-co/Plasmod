# Tier Promotion And Archive

```text
Warm object/segment
  -> admin export/archive
  -> encode canonical object + index metadata
  -> ColdObjectStore (S3/MinIO)
  -> cold diagnostics/key indexes

Cold query include_cold=true
  -> cold candidate/object read
  -> merge with hot/warm
  -> optional cache promotion
```

归档完成前不应删除 Warm。Promotion/cache 不改变 canonical object ID 和 provenance。
