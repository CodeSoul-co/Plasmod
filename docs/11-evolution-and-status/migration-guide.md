# Migration Guide

## Before upgrade

1. 记录当前 commit/tag、Go/native dependency 和 effective config；
2. 备份 Badger、WAL、checkpoint 和 Cold store；
3. 阅读 schema/API/storage/config changes；
4. 在副本上运行新 binary；
5. 验证旧数据 query/trace/replay。

## Upgrade

1. 停止新写；
2. 等待 visible checkpoint；
3. 正常 shutdown；
4. 执行离线 migration（若需要）；
5. 启动新版本；
6. 检查 health/storage/config/provider；
7. 写一个 strict Event；
8. 验证 Memory、State、Artifact、Edge、Trace、Cold；
9. 恢复流量。

## Rollback

只有旧 binary 能读取新版本写入格式时才能直接 rollback。否则恢复升级前备份，并隔离升级期间新增写入。
