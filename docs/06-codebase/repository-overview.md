# Repository Overview

Plasmod 核心仓库包含 Go runtime、C++ retrieval、SDK、配置、容器和工程文档。

| Path | Responsibility |
|---|---|
| `src/cmd/server` | 可执行程序入口 |
| `src/internal` | 核心 Go runtime |
| `cpp` | C++17 原生 retrieval library |
| `sdk/python` | Python HTTP SDK |
| `sdk/nodejs` | Node 兼容 SDK，能力较少 |
| `configs` | memory tier/provider 配置及参考配置 |
| `scripts` | 构建、启动、安全检查等脚本 |
| `docker-compose*.yml` | split/unified 容器拓扑 |
| `docs` | 当前核心工程文档 |

构建真值优先级：Makefile/CMakeLists/go.mod 和代码配置解析，高于注释、示例 YAML 或旧 README。
