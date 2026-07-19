# Observability

## Health

`/healthz` 检查进程。Readiness 还需检查 `/v1/admin/storage`、effective config、provider health 和查询。

## Metrics

`/v1/admin/metrics` 需要 admin key。建议采集：

- ingest accepted/error/backpressure；
- WAL latest/committed/projected/visible LSN；
- queue depth、retry、projection failure；
- query status、tier、candidate/hit；
- Badger/S3 error 和容量；
- purge/reindex/replay task；
- provider health。

## Logs

以 JSON 或可解析格式收集 stdout/stderr，关联 event ID、object ID、LSN、session 和 request。禁止收集 secret、
完整私有 payload 和原始向量。

## Alerts

对持续 queue saturation、visible LSN 不推进、WAL/Badger error、磁盘不足、S3 auth failure、panic/restart 和
admin auth disabled 告警。
