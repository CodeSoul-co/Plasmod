# Coding Conventions

## Go

- `gofmt`/`goimports`；
- interface 放在真正需要替换的边界；
- error 包含操作和稳定 ID，不吞掉底层 cause；
- context 从 handler 传到 storage/provider；
- goroutine 必须有 owner、cancel 和 shutdown；
- JSON tag 和持久化 key 视为兼容契约。

## C++

- C++17；
- RAII 管理 native resource；
- C ABI 边界不抛异常；
- 检查维度、长度、null 和 handle 状态；
- 不把 Go pointer 保存到调用之后。

## Architecture

- Agent-native semantics 保留在 Go core；
- retrieval backend 不执行租户授权；
- Event/Canonical/Projection 三层不混写；
- 实现扩展通过已有 contracts 和 composition root 接入；
- upstream/vendored 目录避免无关改写。
