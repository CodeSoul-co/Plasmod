<div align="center">
  <img src="assets/plasmod.png" alt="Plasmod Logo" width="480"/>
</div>

<div class="column" align="middle">
  <a href="https://github.com/CodeSoul-co/Plasmod/blob/main/LICENSE"><img height="20" src="https://img.shields.io/github/license/CodeSoul-co/Plasmod" alt="license"/></a>
  <a href="https://hub.docker.com/r/oneflybird/plasmod"><img src="https://img.shields.io/docker/pulls/oneflybird/plasmod" alt="docker-pull-count"/></a>
  <a href="https://pypi.org/project/pyplasmod/"><img src="https://img.shields.io/pypi/v/pyplasmod" alt="pypi-version"/></a>
  <a href="https://github.com/CodeSoul-co/Plasmod"><img src="https://img.shields.io/github/stars/CodeSoul-co/Plasmod" alt="github-stars"/></a>
</div>

<div align="center">

[English](README.md) · [中文](README.zh-CN.md)

</div>

# Plasmod

## Plasmod 是什么？

[Plasmod](https://github.com/CodeSoul-co/Plasmod) 是一款开源的 **Agent 原生数据库**，专为 AI 应用和多智能体系统设计。它为 AI Agent 提供记忆与检索基础设施，帮助 Agent 维护上下文、追踪状态演化，并在长期运行的工作流中检索结构化证据。

与传统向量数据库主要关注嵌入存储和相似度搜索不同，Plasmod 将 **记忆（Memory）**、**状态（State）**、**事件（Event）**、**产物（Artifact）** 和 **关系（Relation）** 作为一等数据库对象。这意味着你的 AI Agent 不仅可以存储向量，还可以存储完整的认知上下文——包括发生了什么（事件）、Agent 知道什么（记忆）、Agent 决定了什么（状态）、Agent 产出了什么（产物），以及这些对象之间的关系（边）。

Plasmod 专为 Agent 需要超越 top-k 相似度匹配的场景而构建。它支持 **事件驱动的状态演化**、**溯源感知检索** 和 **结构化证据组装**——查询结果不仅包含匹配内容，还包含证明链、来源归因和图上下文。这使得 Plasmod 非常适合 RAG 系统、自主 Agent、长期记忆应用和多 Agent 协作工作流。

Plasmod 使用 Go 语言编写，提供 Python SDK（`pyplasmod`），可通过 Docker 部署用于本地开发或扩展至生产环境。它与 LangChain 等 AI 框架集成，让你能够轻松为现有 AI 应用添加 Agent 原生记忆能力。

## 快速开始

### 1. 安装 Python SDK

```bash
pip install -U pyplasmod
```

这将安装 `pyplasmod`，Plasmod 的 Python SDK。导入 `EasyPlasmod` 创建客户端：

```python
from pyplasmod import EasyPlasmod
```

### 2. 使用 Docker 启动 Plasmod

拉取并运行 Plasmod 服务端：

```bash
docker pull oneflybird/plasmod
docker run -d --name plasmod -p 19530:19530 oneflybird/plasmod
```

服务将在 `http://127.0.0.1:19530` 可用。

### 3. 连接 Plasmod

```python
from pyplasmod import EasyPlasmod

# 连接本地 Plasmod 服务
with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # 检查服务健康状态
    print(client.health())  # {"status": "ok"}
```

### 4. 写入数据

Plasmod 使用事件驱动的写入模型。你可以写入事件，这些事件将被物化为记忆对象：

```python
from pyplasmod import EasyPlasmod

with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # 写入一个观察事件
    client.ingest_event({
        "event_type": "observation",
        "workspace_id": "my-agent",
        "payload": {
            "text": "用户偏好深色模式，使用 Python 进行开发。",
            "source": "user_preferences"
        }
    })
```

或直接使用 HTTP API：

```bash
curl -X POST http://127.0.0.1:19530/v1/ingest/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "observation",
    "workspace_id": "my-agent",
    "payload": {
      "text": "用户偏好深色模式，使用 Python 进行开发。",
      "source": "user_preferences"
    }
  }'
```

### 5. 查询数据

检索相关记忆及结构化证据：

```python
from pyplasmod import EasyPlasmod

with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # 搜索相关记忆
    results = client.search(
        query_text="用户有什么偏好？",
        workspace_id="my-agent",
        top_k=5
    )
    print(results)
```

或使用 HTTP API：

```bash
curl -X POST http://127.0.0.1:19530/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query_text": "用户有什么偏好？",
    "workspace_id": "my-agent",
    "top_k": 5
  }'
```

查询响应不仅包含匹配内容，还包含溯源信息、证明链和相关图边。

## 为什么选择 Plasmod？

Plasmod 旨在解决 AI Agent 的记忆问题。以下是开发者选择 Plasmod 而非传统方案的原因：

<details>
  <summary><b>Agent 原生数据模型</b></summary>

大多数向量数据库将所有数据视为带元数据的嵌入。Plasmod 提供专为 Agent 记忆设计的数据模型：
- **事件（Event）** — 发生事情的不可变记录（观察、行动、决策）
- **记忆（Memory）** — 从事件物化的知识
- **状态（State）** — 当前 Agent 状态及版本历史
- **产物（Artifact）** — 产出的输出（代码、文档、计划）
- **边（Edge）** — 对象之间的类型化关系
</details>

<details>
  <summary><b>事件驱动的状态演化</b></summary>

Plasmod 中所有状态变更都通过追加写入的 WAL（预写日志）流转，而非直接覆盖。这实现了：
- 完整的审计追踪和重放能力
- 时间点查询（"Agent 在时间 T 知道什么？"）
- 多 Agent 场景的冲突检测与解决
</details>

<details>
  <summary><b>结构化证据检索</b></summary>

传统向量搜索返回 top-k 相似片段。Plasmod 返回 **证据包**，包含：
- 带相关性分数的匹配记忆对象
- 溯源信息（这个知识从哪里来？）
- 证明链（如何得出这个结论？）
- 1 跳图扩展（与这些结果相关的是什么？）
</details>

<details>
  <summary><b>工作空间与会话隔离</b></summary>

内置多租户支持，通过 `workspace_id` 和 `session_id` 进行范围划分。每个 Agent 或用户会话可以拥有隔离的记忆，无需复杂的应用层分区。
</details>

<details>
  <summary><b>分层存储架构</b></summary>

Plasmod 实现热/温/冷数据平面：
- **热层** — 内存 LRU 缓存频繁访问的数据
- **温层** — 基于分段的索引存储近期数据
- **冷层** — S3 兼容存储用于归档
</details>

## 核心概念

| 概念 | 描述 |
|------|------|
| **事件（Event）** | 发生事情的不可变记录（观察、行动、决策）。事件是真相的来源。 |
| **记忆（Memory）** | 从事件物化的知识。记忆有生命周期状态（活跃 → 压缩 → 归档 → 删除）。 |
| **状态（State）** | Agent 或实体的当前状态，带完整版本历史。 |
| **产物（Artifact）** | 产出的输出，如代码、文档或计划。 |
| **边（Edge）** | 任意两个对象之间的类型化关系（如 "derived_from"、"references"、"contradicts"）。 |
| **工作空间（Workspace）** | 租户、项目或 Agent 的隔离边界。 |
| **会话（Session）** | 工作空间内对话或任务的隔离边界。 |
| **溯源（Provenance）** | 追踪数据来源和转换历史的元数据。 |

## 核心特性

| 特性 | 描述 |
|------|------|
| **Agent 原生对象模型** | Event、Memory、State、Artifact、Edge 作为一等数据库类型 |
| **事件驱动架构** | 追加写入的 WAL，支持重放和审计 |
| **结构化证据检索** | 查询响应包含溯源、证明链和图上下文 |
| **工作空间/会话隔离** | 内置多租户，无需应用层分区 |
| **分层数据平面** | 热（内存）→ 温（分段索引）→ 冷（S3）存储 |
| **HTTP API** | RESTful API 支持写入、查询、管理和 CRUD 操作 |
| **Python SDK** | `pyplasmod` 提供同步客户端、批量助手和嵌入工具 |
| **LangChain 集成** | `PlasmodVectorStore` 适配器用于 LangChain 应用 |
| **Docker 部署** | 单命令本地部署 `docker run` |
| **多嵌入提供商** | TF-IDF（默认）、OpenAI、Cohere、ONNX 等 |

## 使用场景

Plasmod 专为需要超越简单向量搜索的 AI 应用设计：

| 使用场景 | Plasmod 如何帮助 |
|----------|------------------|
| **带溯源的 RAG** | 不仅返回相关片段，还返回来源归因和置信度分数 |
| **自主 Agent** | 通过事件驱动的状态演化维护跨会话的长期记忆 |
| **多 Agent 系统** | 工作空间隔离和冲突检测支持协作 Agent |
| **对话式 AI** | 会话范围的记忆，支持自动摘要和压缩 |
| **知识管理** | 追踪知识如何随时间演化，提供完整审计追踪 |
| **决策支持** | 结构化证据组装，带证明链支持可解释 AI |

## 生态与集成

Plasmod 与流行的 AI 开发工具集成：

| 集成 | 描述 |
|------|------|
| **LangChain** | `PlasmodVectorStore` 适配器无缝集成 LangChain 应用 |
| **嵌入提供商** | 支持 OpenAI、Cohere、HuggingFace、ONNX 和本地 TF-IDF |
| **Docker** | 官方镜像 `oneflybird/plasmod` 便于部署 |
| **Python SDK** | `pyplasmod` 在 PyPI 可用，提供完整 API 覆盖 |

### LangChain 集成

```bash
pip install "pyplasmod[langchain]"
```

```python
from pyplasmod.langchain import PlasmodVectorStore
from langchain_openai import OpenAIEmbeddings

# 创建 Plasmod 支持的向量存储
vectorstore = PlasmodVectorStore(
    base_url="http://127.0.0.1:19530",
    workspace_id="my-agent",
    embedding=OpenAIEmbeddings()
)

# 像往常一样使用 LangChain
vectorstore.add_texts(["Hello world", "Plasmod is great"])
results = vectorstore.similarity_search("greeting")
```

## 部署选项

| 选项 | 描述 |
|------|------|
| **Docker（推荐）** | `docker run -d -p 19530:19530 oneflybird/plasmod` |
| **Docker Compose** | 完整栈，包含 MinIO 冷存储 |
| **从源码构建** | 直接构建和运行 Go 服务 |

### Docker Compose（带 MinIO 冷存储）

```bash
git clone https://github.com/CodeSoul-co/Plasmod.git
cd Plasmod
docker compose up -d
```

这将启动 Plasmod 和 MinIO（S3 兼容冷存储）。

## HTTP API 概览

| 分组 | 端点 |
|------|------|
| **健康检查** | `GET /healthz` |
| **写入** | `POST /v1/ingest/events` · `POST /v1/ingest/document` |
| **查询** | `POST /v1/query` · `POST /v1/query/batch` |
| **规范 CRUD** | `/v1/memory` · `/v1/agents` · `/v1/sessions` · `/v1/states` · `/v1/artifacts` · `/v1/edges` |
| **管理** | `/v1/admin/topology` · `/v1/admin/dataset/delete` · `/v1/admin/dataset/purge` |
| **追踪** | `GET /v1/traces/{object_id}` |

## 文档

| 资源 | 描述 |
|------|------|
| [Python SDK (pyplasmod)](https://github.com/CodeSoul-co/pyplasmod) | SDK 文档和示例 |
| [GitHub Issues](https://github.com/CodeSoul-co/Plasmod/issues) | Bug 报告和功能请求 |
| [GitHub Discussions](https://github.com/CodeSoul-co/Plasmod/discussions) | 问题和社区讨论 |

## 贡献

Plasmod 开源项目欢迎贡献。请参阅 [CONTRIBUTING.md](CONTRIBUTING.md) 了解提交补丁和开发工作流的指南。

### 从源码构建 Plasmod

环境要求：

- **Go:** >= 1.21
- **Python:** >= 3.9（用于 SDK 开发）
- **Docker:** 用于运行依赖

克隆并构建：

```bash
# 克隆仓库
git clone https://github.com/CodeSoul-co/Plasmod.git
cd Plasmod

# 安装依赖
make deps

# 构建服务
make build

# 运行服务
./bin/plasmod
```

开发模式（热重载）：

```bash
make dev
```

服务默认监听 `127.0.0.1:19530`。可通过 `PLASMOD_HTTP_ADDR` 环境变量覆盖。

## 许可证

Plasmod 基于 [MIT 许可证](LICENSE) 发布。

---

<div align="center">

**[GitHub](https://github.com/CodeSoul-co/Plasmod)** · **[Python SDK](https://github.com/CodeSoul-co/pyplasmod)** · **[Docker Hub](https://hub.docker.com/r/oneflybird/plasmod)** · **[Issues](https://github.com/CodeSoul-co/Plasmod/issues)**

</div>