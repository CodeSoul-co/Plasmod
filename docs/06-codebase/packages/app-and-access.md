# app And access

`internal/app` 是 composition root。它读取环境配置、创建依赖、选择 unified/split listeners，并负责 shutdown。

`internal/access` 是 HTTP boundary：

- `gateway.go` 注册 route 和 handler；
- `admin_auth.go` 只保护 admin prefix；
- `visibility.go` 按 APP_MODE 过滤响应；
- write semaphore 控制并发写入；
- hard delete manager 运行后台 purge task。

Handler 应做协议转换和错误映射，不应重新实现 storage、materialization 或 query 业务。
