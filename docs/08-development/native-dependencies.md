# Native Dependencies

## Configure manually

```bash
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release
cmake --build cpp/build -j
```

若需要与 Makefile 一致的 FAISS 选项，查看 `make cpp` 展开的具体参数。

## CGO build

```bash
CGO_ENABLED=1 go build -tags retrieval -o bin/plasmod ./src/cmd/server
```

需要正确的 include、library search path 和 runtime rpath。不要把本机绝对路径硬编码进源码。

## Ownership

Go 传给 C 的 slice 只在调用期间有效；C++ handle 通过显式 destroy 释放；错误字符串复制回 Go。增加 API 时
需要同时测试空输入、维度错误、重复 close 和并发 search。
