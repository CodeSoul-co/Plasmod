# Logging And Metrics

## Logs

启动日志应记录监听器、storage mode/data dir、embedder/retrieval feature、admin key missing warning 和 shutdown。
不要记录 admin key、S3 secret、完整敏感 payload 或 embedding vector。

## Metrics endpoint

`GET /v1/admin/metrics` 返回当前 runtime/admin 指标，需要 admin key。关注：

- write queue/backpressure；
- consistency progress/retry/failure；
- materialization/projection；
- query/tier usage；
- purge task；
- provider health。

## Correlation

Event ID、object ID、LSN、session ID 和 request ID 是跨组件关联字段。新增日志应保留这些结构化字段，而不是
仅输出自由文本。

## Production visibility

APP_MODE=prod 会从 API JSON 删除 debug/raw/log/chain trace 字段；这不影响服务端运维日志。
