# Configuration Deprecation

## Current compatibility

部分环境变量仍接受 `ANDB_*` alias，构建 option 也保留该前缀。新配置使用 `PLASMOD_*`，但移除 alias 前要
给出至少一个明确迁移窗口。

## YAML status

只有启动代码真实读取的 YAML 才是 active config。未接入 `BuildServer` 的 app/storage/retrieval/graph YAML 应
标为 reference，而不是承诺自动生效。

## Deprecation process

1. 增加新 key 并保持旧 key fallback；
2. 日志警告旧 key（不打印 secret）；
3. effective config 只显示 canonical key；
4. 更新 Compose/SDK/docs；
5. 发布迁移说明；
6. 在后续 major release 删除 fallback。
