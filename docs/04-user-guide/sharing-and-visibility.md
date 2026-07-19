# Sharing And Visibility

## Scope

Event 和 Query 都可以携带 tenant、workspace、agent、session 范围。`access.visibility` 表达对象可见级别，
ShareContract 和 PolicyRecord 表达更明确的共享/治理决策。

## 推荐规则

1. tenant 是最高隔离边界；
2. workspace 表示协作范围；
3. agent/session 进一步缩小上下文；
4. private memory 不应只靠命名约定隔离；
5. shared memory 应创建 ShareContract 或可审计 Edge；
6. 查询必须带 scope，不依赖检索后过滤敏感结果。

## ShareContract

`schemas.ShareContract` 保存提供者、接收者、对象范围、授权条件和生命周期。入口为
`/v1/share-contracts`。直接写 contract 只建立 canonical contract；应用仍需在查询入口执行相应 policy。

## 当前安全边界

Plasmod 有 scope filter、policy records 和 admin key，但不是完整 IAM 产品。公开 HTTP 数据路由没有统一
用户认证中间件。生产部署必须由可信网关完成 TLS、身份认证、租户绑定和速率限制。
