# Storage Prefix Map

```text
seg|       retrieval segments
idx|       index metadata
obj|*|     canonical objects
edg|       graph edges
ver|       object versions
pol|       policy records
ctr|       share contracts
kpeS|      edge source index
kpeD|      edge destination index
```

源文件：`src/internal/storage/badger_stores.go`。完整对象 prefix 表见
[`../storage-key-layout.md`](../storage-key-layout.md)。
