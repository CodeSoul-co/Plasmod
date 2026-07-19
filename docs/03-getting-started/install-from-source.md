# Install From Source

## 1. 获取依赖

在仓库根目录执行：

```bash
go mod download
```

Python SDK 是独立可编辑包：

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

## 2. 最小启动

```bash
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

该命令的实际语义：

- `disk` 选择 Badger 持久化和文件 WAL；
- `.andb_data` 保存 canonical objects、edges、versions、WAL 和一致性 checkpoint；
- `tfidf` 避免依赖外部 embedding 服务；
- 关闭 gRPC，只开放统一 HTTP `127.0.0.1:8080`。

不要依赖 `configs/storage.yaml` 决定启动后端；当前 `app.BuildServer` 的存储选择以环境变量为准。

## 3. 启用原生检索

```bash
make cpp
make build
./bin/plasmod
```

`make cpp` 当前会请求 FAISS 支持。构建失败时，先查看
[`../07-dependencies/native-retrieval-stack.md`](../07-dependencies/native-retrieval-stack.md)，不要把
CGO 失败误判为 Go 对象存储失败。

## 4. 使用开发脚本

```bash
cp .env.example .env
make dev
```

`make dev` 调用 `scripts/dev_up.sh`，会读取 `.env` 并根据本地原生库是否存在决定是否添加
`retrieval` tag。`.env.example` 只是模板，最终有效配置仍应通过启动日志和管理配置接口确认。
