# Admin API

## 认证

设置：

```bash
export PLASMOD_ADMIN_API_KEY='replace-with-a-secret'
```

请求使用 `X-Admin-Key` 或 `Authorization: Bearer <key>`。兼容环境变量
`ANDB_ADMIN_API_KEY` 仍可读取，但新部署应使用 Plasmod 名称。

如果密钥未设置，当前代码记录警告并放行 admin route。生产安全检查必须把“密钥为空”视为部署失败。

## 只读接口

- topology、storage、config/effective；
- metrics；
- consistency/governance/runtime/provider mode 的 GET；
- provider health；
- purge task status。

## 变更接口

- S3 export/snapshot/cold purge；
- warm prebuild、embedding reindex；
- dataset/source delete 和 purge；
- data wipe、rollback、replay；
- consistency/governance/runtime/provider mode 的 POST。

## 高风险操作

`data/wipe`、purge、cold-purge、rollback 和 replay 会改变大范围数据或派生状态。调用前必须：

1. 验证 effective config 和目标实例；
2. 备份；
3. 限制并发写入；
4. 保存请求参数和返回 task ID；
5. 通过 query、trace、storage key 和 metrics 验证结果。

Admin API 没有内建多人审批、细粒度 RBAC 或审计归档服务，这些应由控制面网关补齐。
