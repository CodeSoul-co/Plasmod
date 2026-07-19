# Test Guide

## Core tests

```bash
go test ./src/...
```

## Full repository target

```bash
make test
```

该目标运行 Go tests 和 Python tests。原生 path 需先构建 library，再执行带 `retrieval` tag 的相关测试。

## Test layers

- schema normalize/validation；
- storage contract 和 Badger reopen；
- WAL append/scan/corruption；
- deterministic materialization/replay；
- consistency mode/backpressure/timeout；
- Gateway route/error/auth/visibility；
- DataPlane lexical/native/cold；
- Evidence graph/proof；
- shutdown/resource release。

## Persistent compatibility

修改 key/schema/WAL codec 时，测试必须用旧 fixture 打开并读取，或提供明确 migration。只测试新建空库不够。
