# ADR-0003: Go Runtime + C++ Retrieval

- Status: Accepted, Conditional
- Context: runtime、HTTP、WAL 和 storage 需要 Go 的并发/工程生态；ANN backend 主要来自 C++ 生态。
- Decision: Go 定义业务 contract，CGO 调用 `libplasmod_retrieval`，C++ 封装 vendored Knowhere-style engine。
- Consequences: 需要 CMake、CGO、runtime library path 和 ABI 管理；无 native library 时使用 stub/lexical 降级。
- Alternatives: 全 Go ANN 或把全部 runtime 下沉 C++，当前均未采用。
- Invariant: RRF、policy、evidence 和 canonical semantics 留在 Go，不声明为第三方 ANN 内核能力。
