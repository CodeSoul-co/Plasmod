# Native Retrieval Stack

## Library

`cpp/CMakeLists.txt` 构建 `libplasmod_retrieval`，主要源文件包括 `retrieval.cpp`、`segment_index.cpp`、
`dense.cpp`、`sparse.cpp` 和 `batch_optimizer.cpp`。

## Index types

- HNSW；
- IVF_FLAT；
- IVF_PQ；
- IVF_SQ8；
- DISKANN。

运行可用性取决于编译 feature，不是只由 API `index_type` 字符串决定。

## Go bridge

`src/internal/dataplane/retrievalplane/bridge.go` 仅在 `retrieval` tag 下编译；stub 文件提供无 native library 的
兼容实现。所有 C handle 必须有明确 release，Go memory 不得被 C 长期持有。

## TensorRT

仓库包含可选 TensorRT bridge。它不属于默认 macOS/CPU 启动路径，应在独立支持矩阵和镜像中启用。
