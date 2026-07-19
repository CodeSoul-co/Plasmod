# Stop, Reset And Cleanup

## 源码启动

在前台终端按 `Ctrl-C`。`app.RunServers` 会触发 HTTP/gRPC shutdown，并等待运行时关闭。

确认进程已停止：

```bash
pgrep -af 'plasmod|src/cmd/server'
```

需要清空本地数据时，先停止服务，再删除你显式设置的目录：

```bash
rm -rf .andb_data
```

该操作会删除 WAL、canonical records、versions 和 checkpoint，不可恢复。

## Docker

停止但保留数据：

```bash
docker compose down
```

停止并删除 Compose volumes：

```bash
docker compose down -v
```

不要在服务仍写入时手工删除 Badger 文件。对于生产数据，优先执行备份、快照或受控 purge；参见
[`../09-deployment-and-operations/backup-and-restore.md`](../09-deployment-and-operations/backup-and-restore.md)。
