# Build And Link Model

## Pure Go

```bash
go build ./src/cmd/server
```

未启用 `retrieval` tag 时使用 stub bridge。

## Native

```bash
make cpp
make build
```

`make cpp` 使用 CMake 构建动态库；`make build` 检测库后添加 Go build tag/CGO link flags。Dockerfile 还安装
FAISS、ONNX Runtime 等镜像依赖。

## Runtime linking

动态 linker 必须找到 `libplasmod_retrieval` 及其 FAISS/OpenMP/ONNX 依赖。macOS 使用 `otool -L`，Linux
使用 `ldd` 检查：

```bash
otool -L ./bin/plasmod
```

能编译不代表部署镜像中动态库路径正确；必须在最终运行环境执行启动 smoke test。
