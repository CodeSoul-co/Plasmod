# Artifact Creation

```text
artifact/tool result Event
  -> object descriptor + payload
  -> ArtifactIDOrDefault
  -> Artifact record
  -> produced_by/derived_from edges
  -> ObjectVersion
  -> optional retrieval projection for indexable text
```

大内容可以外置到 S3，Artifact 保留 URI/hash/mime/provenance。显式 object ID 在该链路被优先采用。
