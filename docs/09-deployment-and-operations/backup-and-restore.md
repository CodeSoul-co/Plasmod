# Backup And Restore

## Backup scope

完整恢复至少考虑：

- Badger canonical data；
- `wal.log`；
- consistency checkpoint；
- derivation/policy logs；
- native index/segment metadata；
- S3/MinIO cold objects；
- effective configuration 和 binary version。

## Backup procedure

1. 记录 latest LSN 和版本；
2. 暂停或隔离写流量；
3. 使用一致的 Badger backup/snapshot，而非复制正在变化的文件集合；
4. 复制 WAL/checkpoint；
5. 记录 S3 version/prefix；
6. 校验 checksum；
7. 恢复写流量。

## Restore procedure

1. 在隔离目录恢复；
2. 使用兼容 binary；
3. 检查 Badger 可打开和 LatestLSN；
4. 根据 checkpoint replay；
5. rebuild 缺失 retrieval projection；
6. 验证 object/edge/version/trace/query；
7. 再切换流量。
