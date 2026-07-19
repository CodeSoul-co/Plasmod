# Verify Installation

按以下顺序检查，能区分进程、持久化、物化和检索问题。

## 1. 服务存活

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

Docker 拆分模式把地址改为 `http://127.0.0.1:9091/healthz`。

## 2. Event 写入成功

执行 [`first-event-and-query.md`](first-event-and-query.md) 的写入命令。HTTP 非 2xx 时先查看响应体；
严格一致性超时、投影失败和普通校验错误使用不同状态码。

## 3. 对象可查询

查询响应应至少包含 ID 为 `mem_evt_quickstart_001` 的对象。只得到空向量结果时，确认请求包含
`target_object_ids`，或确认词法索引文本已写入。

## 4. WAL 和数据目录存在

```bash
test -f .andb_data/wal.log
test -d .andb_data
```

磁盘模式下还会保存一致性 checkpoint 和 Badger 数据。内存模式不会留下可恢复数据。

## 5. 有效配置

设置管理密钥后：

```bash
curl -fsS -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:8080/v1/admin/config/effective
```

该接口比静态 YAML 更接近当前进程实际配置。

## 6. 重启后仍可读取

保持 `PLASMOD_DATA_DIR` 不变，正常停止服务并重新执行同一启动命令，然后再次运行 Query 和 Trace。对象、Edge
和 Version 应仍存在，`wal.log` 的 LatestLSN 不应回到 0。若改成 `PLASMOD_STORAGE=memory`，重启后数据消失
是预期行为。
