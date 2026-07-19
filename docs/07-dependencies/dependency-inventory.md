# Dependency Inventory

| Dependency | Version/source evidence | Role | Optional | Owner |
|---|---|---|---:|---|
| Go | `go.mod`: 1.25 | Core server toolchain | No | Go project |
| Badger | `github.com/dgraph-io/badger/v4 v4.8.0` | Disk canonical storage | Storage-mode | Upstream |
| gRPC | `google.golang.org/grpc v1.72.1` | gRPC server/transport | Yes | Upstream |
| Protobuf | `google.golang.org/protobuf v1.36.6` | gRPC wire types | gRPC path | Upstream |
| Knowhere-style source | `cpp/vendor`; upstream commit not recorded in one manifest | ANN abstraction/engines | Native path | Upstream snapshot |
| HNSW engine | `cpp/vendor/engines/hnsw_engine` | Default native ANN | Native path | Upstream |
| FAISS engine | CMake option `ANDB_KNOWHERE_FAISS` | IVF family | Yes | Upstream |
| DiskANN engine | CMake option `ANDB_KNOWHERE_DISKANN` | Disk ANN | Yes | Upstream |
| OpenMP | system/Homebrew toolchain | Native batch parallelism | Yes | Toolchain |
| ONNX Runtime | Docker/native provider dependency | Local neural embedder | Yes | Upstream |
| S3/MinIO | External service | Cold storage | Yes | External |
| Python `requests` | `sdk/python/setup.py` | Python SDK HTTP | SDK only | Upstream |

Native vendor 缺少单一 upstream commit manifest 是升级风险；发布前应补 source revision 和 notices。精确版本以
`go.mod`、`cpp/CMakeLists.txt`、Dockerfile 和 SDK package metadata 为准。
