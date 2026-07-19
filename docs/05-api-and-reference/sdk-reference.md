# SDK Reference

## Python

路径：`sdk/python`；包：`plasmod-sdk`；模块：`plasmod_sdk`；类：`PlasmodClient`。

主要方法：

```python
PlasmodClient(base_url=None, timeout=None)
ingest_event(event_id, agent_id, session_id, event_type, payload, **extra)
ingest_vectors(vectors, segment_id="", object_ids=None, index_type="", ...)
query(query_text, query_scope="global", top_k=10, **filters)
get_consistency_mode()
set_consistency_mode(mode)
get(path)
post(path, body)
```

默认地址优先读取 `PLASMOD_URI`、`PLASMOD_BASE_URL`，否则使用 `http://127.0.0.1:19530`。超时读取
`PLASMOD_HTTP_TIMEOUT`。SDK 使用 `requests.raise_for_status()`，错误响应会抛出异常。

`ingest_event` 当前构造兼容平铺 Event；需要 v0.4 全部嵌套字段时可使用通用 `post()`，或扩展 SDK。

## Node.js

路径：`sdk/nodejs`；包名当前为 `andb-sdk-node`，标记为 private；类为 `AndbClient`。目前公开能力主要是
consistency mode 操作，命名仍处于兼容迁移状态，稳定性为 Partial。

## SDK 兼容原则

SDK 不应自行改变 ID、scope 和 consistency 语义。新增方法必须从 Go schema JSON tag 生成请求，并增加
服务端契约测试；不能仅更新 SDK 示例。
