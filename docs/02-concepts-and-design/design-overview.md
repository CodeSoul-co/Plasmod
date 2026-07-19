# 设计总览

## 核心抽象

Plasmod 将 runtime 事实分为三层：

- **Causal input**：Event + WAL/LSN。
- **Canonical state**：Memory、AgentState、Artifact、Edge、ObjectVersion、Policy、ShareContract。
- **Derived access path**：hot/warm/cold cache、lexical/vector/sparse index 与 evidence cache。

Event 是“为什么发生”的因果输入，canonical store 是“当前对象是什么”的权威事实，retrieval projection 是“如何快速找到”的派生视图。三者不能混为一个向量表。

## 四个工程 plane

| Plane | 责任 | 主要代码 |
|---|---|---|
| Access | HTTP/gRPC/binary、验证、backpressure、admin boundary | `internal/access`, `internal/api/grpc`, `internal/transport` |
| Event/Consistency | WAL、LSN、admission、queue、watermark、checkpoint、replay | `internal/eventbackbone`, `worker/consistency` |
| Canonical/Coordination | object materialization、storage、version、policy、worker dispatch | `materialization`, `storage`, `coordinator`, `worker` |
| Retrieval/Evidence | planner、tiered search、native bridge、graph/version/proof assembly | `semantic`, `dataplane`, `evidence`, `cpp` |

## 同步与异步边界

- Gateway 的 write semaphore 是同步 admission。
- WAL Append 在所有 consistency mode 下先发生。
- strict projection 在写请求内等待；bounded/eventual 使用 controller queue 和 worker。
- retrieval index flush 可在 background loop 中发生；canonical visibility 与最终 index shape 不应被混为一项状态。
- EventSubscriber 只消费 controller 已推进的 visible LSN，并触发二级 worker chain。

## 设计限制

- direct canonical CRUD 并非全部 Event-first。
- access/policy 不是完整 IAM。
- runtime 为单进程组装；上游 control/stream code 不代表完整集群已启用。
- native retrieval 与 embedding provider 是条件依赖。
