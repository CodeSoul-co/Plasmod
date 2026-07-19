# Knowhere Integration

`cpp/vendor` 包含 Knowhere-style/upstream native source，`cpp/retrieval` 组合 Plasmod retrieval library。

## Why this boundary

选择 source-level ANN engine 是为了复用成熟的 HNSW/IVF/DiskANN index lifecycle，同时让 Plasmod 保持一个
稳定 C ABI，不把上游 C++ 类型扩散到 Go schema。替代方案包括纯 Go ANN 或直接绑定每个 engine；前者会增加
核心算法维护，后者会让 backend 差异渗入 DataPlane。

## Boundary

- C++：index create/load/search、dense/sparse primitive、batch optimization；
- Go CGO bridge：handle ownership、slice conversion、error mapping；
- Go DataPlane：namespace、scope、policy、fusion、tiering、Evidence。

## Build flags

- HNSW 始终可构建；
- `ANDB_KNOWHERE_FAISS` 控制 FAISS；
- DiskANN 和 RAFT/GPU 由独立 option 控制；
- GPU 路径要求 Linux/CUDA，不是 macOS 默认能力。

## Search paths

- native/raw：Knowhere batch search 返回原始 ID 和距离；
- optimized batch：`BatchPluginL2NormSort` 在 query rows 较多时重排并用 OpenMP 分发，之后恢复行顺序；
- Go result layer：映射 object ID，并用 RRF 合并 lexical/dense/sparse/tier candidates。

Batch plugin 不改变每行的逻辑归属；`row_lineage` 的 source fan-out 在 Go schema/service 层完成。

## Error mapping and fallback

CGO bridge 把 create/build/search/handle 错误转为 Go error。没有 `retrieval` tag 时 stub 明确返回 unavailable，
上层可使用 lexical/canonical path；不能将 unavailable 伪装为空 ANN 结果。

兼容 flag 仍使用 `ANDB_` 前缀是当前构建历史，不应由文档伪装成已完成重命名。
