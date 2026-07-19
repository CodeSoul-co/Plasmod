# Prerequisites

## 仅使用 Go 数据路径

- Go `1.25.x`，版本以 `go.mod` 为准；
- Git；
- macOS 或 Linux；
- 可写的数据目录。

这种方式使用 Go stub retrieval，不要求先编译 C++，适合验证对象、WAL、查询和证据链。

## 启用原生检索

还需要：

- CMake `3.20+`；
- 支持 C++17 的编译器；
- OpenMP；
- FAISS 及其传递依赖，或关闭相应构建选项；
- CGO 可用。

`cpp/CMakeLists.txt` 定义原生库，`src/internal/dataplane/retrievalplane/bridge.go` 通过
`retrieval` build tag 启用 CGO 桥接。若 `cpp/build/libplasmod_retrieval.dylib` 或对应 `.so` 存在，`make build`
会自动添加该 tag。

## Docker 路径

- Docker Desktop 或兼容 Docker Engine；
- Docker Compose v2；
- 至少为镜像构建和 Badger/MinIO 数据卷预留足够磁盘空间。

## 启动前检查

```bash
go version
docker version
docker compose version
cmake --version
```

不使用 Docker 或原生检索时，可忽略对应检查。
