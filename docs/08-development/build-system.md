# Build System

## Go

`go.mod` 声明 Go 1.25。常用目标：

```bash
make build
make test
go test ./src/...
```

`make build` 检查 `cpp/build/libplasmod_retrieval.dylib` 或对应 `.so`；存在时启用 `retrieval` tag，否则构建
stub path。

## C++

```bash
make cpp
```

目标调用 CMake，当前打开 FAISS option。更细 feature 以 `cpp/CMakeLists.txt` 为准。

## Docker

Dockerfile 分阶段构建 C++ library、Go binary 和 runtime image。Compose 提供 unified/split 运行拓扑。

## Artifacts

- Go binary：`bin/plasmod`；
- native library：`cpp/build/libplasmod_retrieval*`；
- runtime data：由 `PLASMOD_DATA_DIR` 决定。

构建产物与持久化数据都不应作为源码提交。
