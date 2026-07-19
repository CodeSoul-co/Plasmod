# Native Deployment

## Build artifact

```bash
make cpp
make build
```

将 `bin/plasmod`、所需动态库、配置和 license notices 放入同一发布版本。使用 `otool -L`/`ldd` 检查链接。

## Service account

使用非 root 用户，授予：

- data dir 读写；
- WAL/checkpoint 原子 rename 权限；
- 必需的 S3 network/credential；
- 监听非特权端口；
- 日志输出权限。

## Process manager

使用 launchd/systemd/Kubernetes 管理 restart、signal、environment 和 stdout/stderr。Shutdown timeout 应大于
Plasmod consistency shutdown timeout，以便队列和 checkpoint 受控退出。

## Readiness

`/healthz` 当前主要表示进程存活。上线流量前还应检查 storage、effective config、provider health 和一次受控查询。
