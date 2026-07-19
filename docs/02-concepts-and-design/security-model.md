# 安全模型

## 当前实现

- `PLASMOD_ADMIN_API_KEY` 开启 `/v1/admin/*` shared-key 认证。
- 支持 `X-Admin-Key` 或 `Authorization: Bearer`，使用固定长度 HMAC digest 做 constant-time compare。
- `APP_MODE=prod` 清理 JSON 中的 debug/raw/log/chain trace 等字段。
- Gateway 通过 write semaphore、payload checks 和部分 batch limits 限制资源消耗。

## 未提供

- TLS termination、mTLS、OAuth/OIDC、用户登录、细粒度 RBAC。
- 对 `/v1/internal/*` 和普通 data routes 的统一认证。
- 完整 tenant/workspace 强制隔离和 row-level authorization。
- secret manager、key rotation、KMS 或 S3 IAM 配置管理。

## 部署要求

1. 在非本机环境设置 admin key。
2. 通过反向代理/service mesh 提供 TLS、身份认证、IP/network policy 和 request limits。
3. split mode 下分别限制 9091 management、19530 API 和 19531 gRPC。
4. 不暴露 MinIO console；替换 compose 默认凭据。
5. 使用最小权限 S3 credential 和独立 bucket/prefix。
6. `APP_MODE=prod`，并运行 `make prod-safety-check`。

## 数据访问语义

Event access fields、PolicyRecord 和 ShareContract 是应用级可见性描述，不等同于已认证主体。只有当服务入口能可信地绑定 caller identity，policy evaluation 才能形成安全边界。
