# eventbackbone

Active core 包括 WAL contract、FileWAL、InMemoryWAL、Bus、watermark、derivation/policy decision log。

FileWAL 位于 `<dataDir>/wal.log`，提供持久重放。InMemoryWAL 只随进程存在。Derivation log 默认位于
`<dataDir>/derivation.log`。

`eventbackbone/streamplane` 是上游兼容快照，包含 stream coordinator/node/flush pipeline 等大量代码；默认
单进程 BuildServer 不等于完整启用该子系统。
