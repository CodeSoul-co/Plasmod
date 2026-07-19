# Development Troubleshooting

## Port already in use

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
lsof -nP -iTCP:19530 -sTCP:LISTEN
```

确认旧本地进程和 Docker container，而不是直接更改所有默认端口。

## Badger lock

检查是否两个测试/服务共享 data dir。停止 owner 后重试，不删除 lock 绕过。

## Build unexpectedly uses stub

确认 `cpp/build/libplasmod_retrieval.dylib`/`.so` 文件名和 Makefile 检测条件，查看 `go build` 是否包含
`-tags retrieval`。

## Query returns object but no native hit

查看 `query_status` 是否为 supplemented，检查 vector projection、embedding family 和 warm segment。

## Strict ingest times out

检查 controller queue、worker、projection error、embedder/native backend 和 checkpoint；先确认对象是否已物化再重试。
