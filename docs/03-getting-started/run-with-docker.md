# Run With Docker

## 拆分端口模式

```bash
docker compose up -d --build
docker compose ps
```

默认服务入口：

- 数据 API：`http://127.0.0.1:19530`；
- 管理 API：`http://127.0.0.1:9091`；
- gRPC：`127.0.0.1:19531`；
- MinIO API：`http://127.0.0.1:9000`；
- MinIO Console：`http://127.0.0.1:9001`。

健康检查：

```bash
curl -fsS http://127.0.0.1:9091/healthz
```

## 统一 HTTP 模式

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

统一模式在一个 HTTP 监听器上注册管理和数据路由，gRPC 仍使用独立端口。

## 数据持久化

Compose 将 Plasmod 数据目录挂载到 `/data`，MinIO 使用独立 volume。删除容器不会自动删除 volume；
只有显式执行 `docker compose down -v` 才会清除持久化数据。

## 管理接口认证

正式环境必须设置 `PLASMOD_ADMIN_API_KEY`。请求可使用：

```bash
curl -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:9091/v1/admin/config/effective
```

未设置管理密钥时，当前实现只记录警告，不会自动拒绝管理请求，因此不能把默认 Compose 当作安全配置。
