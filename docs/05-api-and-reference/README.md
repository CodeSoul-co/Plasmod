# API And Reference

本目录记录当前代码中的 HTTP、transport、SDK、Schema 和配置契约。路由来源为
`src/internal/access/gateway.go`，JSON 字段来源为 `src/internal/schemas`，不是由示例反推。

## 入口

- [`api-overview.md`](api-overview.md)
- [`route-index.md`](route-index.md)
- [`public-http-api.md`](public-http-api.md)
- [`internal-api.md`](internal-api.md)
- [`admin-api.md`](admin-api.md)
- [`schema-reference/README.md`](schema-reference/README.md)
- [`configuration-reference.md`](configuration-reference.md)

`/v1/internal/*` 和 `/v1/admin/*` 不属于同一种安全边界：admin middleware 只保护 admin 前缀，internal
路由需要由部署网络或外部网关隔离。
