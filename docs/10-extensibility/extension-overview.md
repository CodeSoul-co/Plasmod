# Extension Overview

## Stable extension boundaries

- Event payload/extensions；
- schema constants 和 canonical types；
- materializer/worker contracts；
- RuntimeStorage interfaces；
- DataPlane/retrievalplane interfaces；
- semantic query operators；
- policy/evidence hooks；
- HTTP/SDK adapters。

## Required questions

1. 新事实是否写入 WAL？
2. canonical source 是什么？
3. ID 是否 deterministic/replay-safe？
4. 哪些 scope/policy 适用？
5. projection 失败如何恢复？
6. storage migration 如何处理？
7. delete/purge/backup 是否覆盖？
8. public API 是否需要兼容承诺？

只新增 handler 或 struct 通常不足以形成完整功能。
