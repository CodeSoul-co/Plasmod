<div align="center">
  <img src="assets/plasmod.png" alt="Plasmod Logo" width="480"/>
</div>

<div align="center">

[English](README.md) · [中文](README.zh-CN.md)

</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Python](https://img.shields.io/badge/Python-3.x-3776AB?logo=python&logoColor=white)](https://www.python.org/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

# Plasmod

**Plasmod** 是一个面向 AI 应用和多智能体系统的开源 **Agent 原生数据库**。它提供认知对象存储、事件驱动的记忆生命周期管理和结构化证据检索——专为智能体记忆工作负载设计。

## Plasmod 是什么？

Plasmod 是一个为 AI 智能体设计的数据库，将 **记忆（Memory）**、**状态（State）**、**事件（Event）**、**产物（Artifact）** 和 **关系（Edge）** 作为一等数据库对象——而不仅仅是向量或文本块。

**核心设计原则：**

- **事件驱动架构** — 所有状态变更通过 append-only WAL 流转，支持重放、审计和溯源追踪
- **规范对象模型** — Agent、Session、Memory、State、Artifact、Edge 是原生数据库类型，具有生命周期管理
- **结构化证据检索** — 查询结果返回带有溯源信息的证据包，而非仅有 top-k 相似度匹配
- **工作空间 / 会话隔离** — 内置多租户支持，实现智能体级和会话级数据边界

## 为什么选择 Plasmod？

当前大多数智能体记忆方案都构建在通用基础设施之上：

| 方案 | 局限性 |
|------|--------|
| 向量数据库 + 元数据表 | 无原生事件/状态模型，无溯源 |
| RAG 文本块存储 | 仅检索，无结构化证据 |
| 应用层事件日志 | 与检索执行脱节 |
| 图层 | 与向量搜索分离，无统一查询 |

**Plasmod 从底层为智能体记忆而设计：**

- 事件驱动的状态演进（非直接覆写）
- 记忆生命周期管理（active → compressed → archived → deleted）
- 保留溯源的证据返回
- 多智能体 / 多会话记忆工作流

## 核心特性

| 特性 | 描述 |
|------|------|
| **Agent 原生数据模型** | Event、Memory、State、Artifact、Edge 作为一等对象 |
| **HTTP API** | 提供 ingest、query、admin 和规范 CRUD 端点 |
| **结构化证据** | 查询响应包含溯源和证明轨迹 |
| **工作空间隔离** | 内置 `workspace_id` 和 `session_id` 作用域 |
| **分层数据平面** | 热层（内存）→ 温层（段索引）→ 冷层（S3） |
| **Python SDK** | `pyplasmod` 已发布到 PyPI |
| **LangChain 集成** | `PlasmodVectorStore` 适配器（实验性） |
| **Docker 部署** | 单命令本地部署 |

## 快速开始

### 1. 启动 Plasmod 服务端

**Split 部署（管理 `9091`、API `19530`、MinIO `9000`/`9001`）：**

```bash
git clone https://github.com/CodeSoul-co/Plasmod.git
cd Plasmod
docker compose up -d
```

**单端口 unified（`8080`，与本地 `go run` 一致）：**

```bash
docker compose -f docker-compose.unified.yml up -d
```

### 2. 验证服务端

**`docker compose up -d`（split）之后：**

```bash
curl http://127.0.0.1:9091/healthz
```

**`docker-compose.unified.yml` 或 `go run ./src/cmd/server` 之后：**

```bash
curl http://127.0.0.1:8080/healthz
```

成功响应表示服务端正在运行。

### 3. 安装 Python SDK

```bash
pip install pyplasmod
```

或安装指定版本：

```bash
pip install pyplasmod==0.1.0
```

### 4. 验证 SDK 安装

```bash
python -c "import pyplasmod; print(pyplasmod.__version__)"
```

预期输出：

```text
0.1.0
```

### 5. 使用 SDK 连接

请与部署方式一致（split compose 可用 pyplasmod 默认 `http://127.0.0.1:19530`）：

```python
from pyplasmod import EasyPlasmod

# docker compose（split）
with EasyPlasmod(base_url="http://127.0.0.1:19530") as p:
    print(p.health())

# unified compose 或本地 go run
# with EasyPlasmod(base_url="http://127.0.0.1:8080") as p:
#     print(p.health())
```

### 数据写入与查询

使用 Plasmod HTTP API 作为 ingest 和 query schema 的权威来源。

**写入事件：**

```bash
curl -X POST http://127.0.0.1:19530/v1/ingest/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "observation",
    "workspace_id": "demo",
    "payload": {
      "text": "Plasmod 是一个 Agent 原生数据库。"
    }
  }'
```

**查询：**

```bash
curl -X POST http://127.0.0.1:19530/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query_text": "agent database",
    "workspace_id": "demo",
    "top_k": 5
  }'
```

最新请求 schema 请参阅：

- [docs/api/ingest.md](docs/api/ingest.md)
- [docs/api/query.md](docs/api/query.md)

## Python SDK

`pyplasmod` 已发布到 PyPI。

```bash
pip install pyplasmod
```

基础连接示例：

```python
from pyplasmod import EasyPlasmod

with EasyPlasmod() as p:
    print(p.health())
```

SDK 文档请参阅 [pyplasmod 仓库](https://github.com/CodeSoul-co/pyplasmod)。

## LangChain 集成

> **注意：** LangChain 适配器为实验性功能，API 可能变更。

```bash
pip install "pyplasmod[langchain]"
```

```python
from pyplasmod.langchain import PlasmodVectorStore
```

最新使用示例请参阅 [pyplasmod 仓库](https://github.com/CodeSoul-co/pyplasmod)。

## HTTP API 概览

| 分组 | 端点 |
|------|------|
| **健康检查** | `GET /healthz` |
| **核心** | `POST /v1/ingest/events` · `POST /v1/query` |
| **管理** | `/v1/admin/*` |
| **规范 CRUD** | `/v1/agents` · `/v1/sessions` · `/v1/memory` · `/v1/states` · `/v1/artifacts` · `/v1/edges` |

完整 API 文档：[docs/api/overview.md](docs/api/overview.md)

## 文档

| 文档 | 描述 |
|------|------|
| [API 概览](docs/api/overview.md) | HTTP API 参考 |
| [Ingest API](docs/api/ingest.md) | 事件写入 |
| [Query API](docs/api/query.md) | 查询请求/响应 |
| [Admin API](docs/api/admin.md) | 管理操作 |
| [架构设计](docs/architecture/) | 系统设计 |
| [English](README.md) | 英文文档 |
| [Python SDK](https://github.com/CodeSoul-co/pyplasmod) | pyplasmod |

## 路线图

### 当前

- Plasmod 服务端及 HTTP API
- Docker Compose 本地部署
- Event ingest 和 query API 基础
- 规范对象模型基础
- Python SDK 发布为 `pyplasmod` v0.1.0
- LangChain 适配器基础

### 实验性 / 进行中

- SDK ingest/query 与最新服务端 API 对齐
- 热/温/冷检索改进
- 富溯源查询响应
- 多智能体 / 会话隔离强化
- 更多 embedding 提供者

### 计划中

- Helm Chart / Kubernetes 部署
- 离线部署指南
- 监控指南
- 策略感知检索
- 更丰富的图推理
- 多语言 SDK

## 贡献

请参阅 [docs/contributing.md](docs/contributing.md) 了解贡献指南。

## 许可证

Plasmod 基于 [MIT 许可证](LICENSE) 开源。

---

<div align="center">

**[文档](docs/)** · **[Python SDK](https://github.com/CodeSoul-co/pyplasmod)** · **[Issues](https://github.com/CodeSoul-co/Plasmod/issues)**

</div>