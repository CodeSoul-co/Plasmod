# Local Development

## Setup

```bash
go mod download
cp .env.example .env
```

不需要原生 ANN 时：

```bash
PLASMOD_STORAGE=disk PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

使用仓库脚本：

```bash
make dev
```

`scripts/dev_up.sh` 读取 `.env`，并根据原生库存在情况决定 build tag。

## Isolated data

为每个开发实例使用独立 `PLASMOD_DATA_DIR`，避免 Badger lock 和测试污染。不要把 `.andb_data` 提交到 Git。

## Before editing

1. `git fetch origin --prune`；
2. 确认 `dev` 和 `origin/dev` 状态；
3. 检查工作区已有修改；
4. 阅读 active call path 和 tests；
5. 保留无关用户修改。
