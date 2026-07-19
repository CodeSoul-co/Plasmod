# Add An Event Type

1. 在 `schemas/constants.go` 添加 canonical string；
2. 更新 Dynamic Event validation/normalize；
3. 定义 payload/object/causality 约束；
4. 在 materializer/worker dispatch 接入；
5. 规定生成对象、Edge、Version 和 deterministic IDs；
6. 更新 query filters 和 trace；
7. 增加 ingest、replay、invalid payload 和 consistency tests；
8. 更新 schema/API/user guide。

Event type 名称一旦进入 WAL 就是兼容面。重命名应通过 alias + migration，而不是直接删除旧常量。
