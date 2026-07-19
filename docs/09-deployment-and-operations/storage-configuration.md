# Storage Configuration

## Disk mode

```bash
PLASMOD_STORAGE=disk
PLASMOD_DATA_DIR=/var/lib/plasmod
```

目录包含 Badger、`wal.log`、`consistency_checkpoint.json` 和 `derivation.log`。需要低延迟、可靠 fsync 和足够
容量，不应放在临时文件系统。

## Memory mode

```bash
PLASMOD_STORAGE=memory
```

所有 records/WAL 随进程退出消失，不用于需要恢复的环境。

## Capacity

容量规划分别考虑 canonical payload、Badger value log、WAL retention、native indexes、hot cache 和 S3 cold data。
监控可用空间，避免 Badger/WAL 同时因磁盘满失败。

## Single writer

不要让多个进程直接共享一个 Badger data directory。多实例需要明确的分布式存储/协调实现，而不是网络文件系统锁。
