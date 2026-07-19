# Authentication

## 内建能力

Admin middleware 只匹配 `/v1/admin/`：

- primary env：`PLASMOD_ADMIN_API_KEY`；
- compatibility env：`ANDB_ADMIN_API_KEY`；
- headers：`X-Admin-Key` 或 Bearer token；
- 比较使用常量时间/HMAC 方式降低时序泄露。

## 未内建的能力

- 数据 API 的统一用户登录；
- `/v1/internal/*` 的认证；
- 细粒度 RBAC；
- TLS 终止；
- token 签发/轮换；
- request quota。

## 生产部署

在 Plasmod 前放置可信 gateway/service mesh：

1. TLS；
2. 验证用户或 workload identity；
3. 把身份绑定到 tenant/workspace，拒绝客户端任意越权覆盖；
4. 隔离 admin、internal、transport 端口；
5. 设置 body size、rate limit 和审计日志；
6. 将 admin key 放入 secret manager，并定期轮换。

Canonical User、Policy 和 ShareContract 是数据/治理模型，不等于 HTTP authentication 已完成。
