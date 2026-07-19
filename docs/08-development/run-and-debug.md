# Run And Debug

## Debug startup

```bash
APP_MODE=test \
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data-debug \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

`APP_MODE=test` 会启用 `/v1/debug/echo` 和 debug response fields，不得用于开放网络。

## Delve

```bash
dlv debug ./src/cmd/server
```

Native crash 需结合 lldb/gdb 和 core dump；Go stack 不能解释 C++ 内存错误。

## Debug order

1. health/端口；
2. effective config；
3. WAL append/LSN；
4. canonical object；
5. projection；
6. query/evidence；
7. response visibility。

按链路定位可以区分“没写入”“已写但未物化”“已物化但未索引”和“查询被过滤”。
