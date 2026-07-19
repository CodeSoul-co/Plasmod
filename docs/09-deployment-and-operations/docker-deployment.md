# Docker Deployment

## Split compose

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f plasmod
```

检查：

```bash
curl -fsS http://127.0.0.1:9091/healthz
```

## Unified compose

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

## Required overrides

正式环境至少覆盖：admin key、MinIO credentials、data volumes、resource limits、restart policy、APP_MODE、
network exposure 和 TLS gateway。

## Upgrade

1. 备份 volume/S3；
2. 构建带固定 commit/tag 的镜像；
3. 在数据副本上验证旧目录；
4. 停止旧容器；
5. 启动新容器并检查 health/config/storage；
6. 验证 write/query/trace；
7. 保留可回滚旧镜像和备份。
