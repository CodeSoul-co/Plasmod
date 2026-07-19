# Component Ownership

## Plasmod-owned active core

- top-level `internal/app`, `access`, `schemas`, `storage`, `materialization`, `evidence`, `semantic`；
- top-level lightweight coordinator files；
- `worker/consistency` 和 Agent-native workers；
- dataplane glue、SDK、配置和运维脚本；
- `cpp/retrieval` 的 Plasmod retrieval composition。

## Upstream/vendored/compatibility areas

- `src/internal/platformpkg`：上游平台代码快照，保留独立 license；
- `src/internal/coordinator/controlplane`：大体量上游兼容控制面；
- `src/internal/eventbackbone/streamplane`：上游 stream/flush 组件；
- `cpp/vendor`：原生检索第三方源码。

这些目录的存在不等于 `app.BuildServer` 默认创建完整 Milvus 式集群。判断运行使用情况必须沿构造函数和
interface 注入查看。

## 修改原则

1. 新 Agent-native 逻辑放在 Plasmod-owned package；
2. 不为方便直接重写上游快照；
3. 上游修改保留来源、license 和差异说明；
4. active wrapper 与 upstream API 分离；
5. 更新依赖时先验证启动链路实际使用的子集。
