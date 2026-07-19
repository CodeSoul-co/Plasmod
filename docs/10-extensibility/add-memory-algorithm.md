# Add A Memory Algorithm

1. 定义 provider/algorithm ID 和配置 schema；
2. 实现 agent SDK/dispatch contract；
3. 使用 canonical Memory 和独立 algorithm state；
4. 明确 ingest/recall/compress/summarize/decay/conflict 行为；
5. 尊重 scope、policy、TTL 和 lifecycle；
6. 注册 profile 和 health；
7. 定义切换 provider 时 state migration；
8. 增加 deterministic unit/contract tests。

算法不得直接修改 Badger key 或绕过 Event provenance。算法 score 也不能覆盖数据库的访问控制结论。
