# Storage Format Evolution

持久化兼容面包括：

- Badger key prefix 和 key composition；
- JSON/binary value codec；
- WAL record framing/schema；
- consistency checkpoint；
- derivation log；
- native segment/index format；
- S3 key prefix/object body。

## Migration patterns

- additive value fields：旧 reader/new reader compatibility；
- key change：dual-read/dual-write + backfill；
- WAL change：versioned decoder；
- native index incompatibility：canonical/embedding-driven rebuild；
- S3 layout change：copy + manifest + cutover。

任何迁移都要可中断恢复、可观测进度并保留回滚备份。
