# Dependency Overview

Plasmod 依赖分为四层：

1. Go runtime：HTTP、Badger、对象模型、WAL、一致性、Evidence；
2. Native retrieval：C++17、Knowhere-style adapter、HNSW/FAISS/DiskANN；
3. External services：S3/MinIO、可选 embedding provider；
4. Build/runtime support：CMake、CGO、OpenMP、ONNX Runtime、Docker。

核心原则：第三方 retrieval 只负责物理候选，Agent scope、canonical objects、policy、provenance 和 consistency
仍由 Plasmod Go runtime 决定。
