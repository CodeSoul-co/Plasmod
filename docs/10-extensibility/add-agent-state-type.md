# Add An Agent State Type

1. 定义 `state_type` 和 `state_key` 语义；
2. 规定 value codec/schema version；
3. 更新 state Event extraction；
4. 保持 `state_<agent>_<key>` ID 或提供迁移；
5. 定义版本递增和并发更新规则；
6. 增加 latest query operator/filter；
7. 验证 restart/replay 后版本；
8. 补充 purge、trace 和 SDK 示例。

不要为每种状态创建新的顶层数据库类型；只有存储、查询和生命周期显著不同才考虑新 canonical object。
