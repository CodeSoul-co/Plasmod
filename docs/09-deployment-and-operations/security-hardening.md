# Security Hardening

## Mandatory controls

1. `APP_MODE=prod`；
2. 设置强 `PLASMOD_ADMIN_API_KEY`；
3. TLS reverse proxy/service mesh；
4. 数据 API 身份认证和 tenant/workspace 强绑定；
5. admin/internal/transport 仅私网；
6. request body/rate/concurrency 限制；
7. 非 root、只授必要文件和网络权限；
8. S3/MinIO 最小权限和 secret rotation；
9. 日志脱敏；
10. 定期恢复演练和依赖漏洞扫描。

## Network segmentation

- management port 只对 operator/control plane；
- data port 只对应用 gateway；
- gRPC/transport 只对受信节点；
- MinIO console 不对应用网络；
- Badger data dir 不通过共享文件服务公开。

## Known gap

内建 admin key 不是完整 IAM，internal route 也不受其保护。必须依赖部署层补齐。
