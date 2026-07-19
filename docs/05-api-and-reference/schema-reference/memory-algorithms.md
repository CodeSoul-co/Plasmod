# Memory Algorithm Schema

Memory algorithm 通过稳定 canonical Memory 字段和独立 algorithm state 工作，而不是定义新的数据库主对象。

相关字段包括：

- `memory_type`、`content`、`summary`；
- `confidence`、`importance`、`freshness_score`；
- `ttl`、`valid_from`、`valid_to`；
- `lifecycle_state`、`is_active`；
- `policy_tags`、`algorithm_state_ref`；
- source event IDs 和 provenance ref。

Provider profile 配置位于 `configs/algorithm_baseline.yaml`、`configs/algorithm_memorybank.yaml` 和
`configs/algorithm_zep.yaml`。算法可以影响召回、衰减、压缩和冲突处理，但不能绕过 tenant/
workspace scope、canonical persistence 或治理过滤。
