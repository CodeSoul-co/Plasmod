# dataplane And retrieval

DataPlane 连接 embedder、lexical/vector store、tiered retrieval 和 query candidates。

`retrievalplane/bridge.go` 在 `retrieval` build tag 下调用 C++ library；stub build 让纯 Go 构建仍可运行 canonical/
lexical 路径。原生层负责 index/search，Go 层保留 scope、policy、fusion 和 evidence 语义。

预计算 query/event vector 可绕过 embedder。所有 vector path 都必须校验 dimension 和 embedding family。
