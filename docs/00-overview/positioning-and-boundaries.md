# 定位与边界

## 与 Vector + Metadata 的区别

Vector + Metadata 通常以“向量行”为主要事实，更新、版本、状态和关系由应用自行编码。Plasmod 将 Event、Memory、AgentState、Artifact、Edge 与 ObjectVersion 放入 canonical object model；向量、稀疏和 lexical index 是可以从 canonical 数据重建的 retrieval projection。

这种区别不意味着 Plasmod 的 ANN 内核天然优于专用向量数据库。Plasmod 的核心价值是 agent 对象语义、可见性、恢复、版本和 evidence 组合；ANN 部分由 Plasmod bridge 与第三方引擎共同提供。

## 与普通向量数据库的关系

Plasmod 可以接收预计算向量并构建 warm segment，也可以使用 TF-IDF 或外部/本地 embedder 产生向量。它同时维护 canonical object、WAL 和关系/版本存储。专用向量数据库通常提供更成熟的分布式 ANN、集群管理和生态；Plasmod 当前不承诺这些能力的完全等价。

## 与 Agent Framework 的关系

Plasmod 不是 agent planner、LLM 调用器或工具执行框架。Framework 负责决定何时产生 event、如何执行任务和如何消费查询结果；Plasmod 负责接收结构化 runtime 数据、持久化、物化、检索与恢复。`src/internal/agent/` 提供接入抽象，但不构成完整 framework runtime。

## 与 MemoryBank/Zep 类算法的关系

MemoryBank、Zep 和 baseline profile 位于 `src/internal/worker/cognitive/`，通过 algorithm dispatcher 影响 memory lifecycle、recall 或 graph processing。它们是可插拔策略，不是 canonical storage 的替代品。算法状态通过 `MemoryAlgorithmStateStore` 保存，canonical Memory 仍由 Plasmod storage 所有。

## Plasmod 负责

- Event 接受、WAL 顺序和 replay 输入。
- Event 到 canonical object 的 projection。
- canonical object、edge、version、policy、contract 和算法状态存储。
- retrieval projection、query planning 和 structured evidence assembly。
- consistency admission、可见性等待、checkpoint 和恢复。
- hot/warm/cold 路由以及可选 S3/MinIO 接入。
- HTTP、内部 binary/SSE、gRPC 和 SDK 接口边界。

## 交给外部组件

- LLM 推理、tool execution 和 agent orchestration。
- OpenAI、Cohere、Vertex AI、Hugging Face 等 embedding provider 的服务可用性。
- S3 服务本身的 durability、replication、IAM 和生命周期策略。
- Knowhere/FAISS/DiskANN、ONNX Runtime、llama.cpp、TensorRT 的内部算法与 ABI。
- TLS、WAF、完整身份系统和网络隔离。

## 明确的非目标或未保证能力

- 不提供跨所有对象和外部系统的全局 ACID 事务。
- 不保证所有 materializer 的 exactly-once 副作用。
- 不提供完整分布式集群控制面的生产承诺。
- 不提供默认开启的用户级认证；只有 admin shared-key middleware。
- 不保证所有配置 YAML 都进入当前启动路径。
- 不保证每种平台、GPU、ANN backend 和 index 文件之间的二进制兼容。

更精确的约束见 [Constraints and Non-goals](../01-requirements/constraints-and-non-goals.md)。
