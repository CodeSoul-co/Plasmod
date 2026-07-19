# 扩展模型

Plasmod 的扩展应保持 Event -> canonical -> retrieval -> evidence -> replay 的完整闭环。

| 扩展点 | Contract/Registry | 注册位置 |
|---|---|---|
| Event/object schema | `schemas.Event`, canonical types | schema + materializer + storage |
| Query operator | `semantic.QueryPlanner`, QueryRequest fields | planner/runtime query path |
| Worker | `worker/nodes` interfaces | `BuildServer` node manager wiring |
| Storage backend | `storage.RuntimeStorage` 子接口 | `storage.factory` |
| Retrieval backend | `dataplane.DataPlane` / retrievalplane | bootstrap/tiered plane |
| Memory algorithm | cognitive algorithm/dispatcher | bootstrap + AlgorithmStateStore |
| Policy/evidence hook | EventHooks/PolicyEngine/Assembler | materialization/query stage |
| Transport | Gateway service methods | HTTP/gRPC/binary adapter |

新增实现必须说明持久化、并发、错误、配置、replay、delete/purge、API/SDK 和 compatibility。详细步骤见 [Extensibility](../10-extensibility/extension-overview.md)。
