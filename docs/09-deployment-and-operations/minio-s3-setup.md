# MinIO And S3 Setup

## Compose MinIO

```bash
docker compose up -d minio
docker compose logs -f minio
```

API 默认 9000，console 9001。创建专用 bucket，并为 Plasmod 使用最小权限 access key。

## Plasmod configuration

设置 endpoint、bucket、region、access/secret、TLS 和 prefix 对应环境变量；随后检查：

```bash
curl -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:9091/v1/admin/storage
```

## Verification

1. 导出一个受控对象；
2. 用 S3 client 检查 prefix/key；
3. 以 `include_cold=true` 查询；
4. 验证 edge/version/provenance 仍一致；
5. 不在验证完成前清理 Warm。

## Production S3

启用 TLS、encryption、versioning、bucket policy、lifecycle 和访问日志。Credential 通过 secret store 注入。
