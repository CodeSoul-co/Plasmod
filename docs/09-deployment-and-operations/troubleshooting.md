# Operations Troubleshooting

## Service unavailable after restart

检查 Docker/进程、端口、Badger lock、data dir 权限、native dynamic libraries 和依赖服务。

## Writes accepted but not visible

查看 consistency metrics、queue、projection failure、embedder/native backend、checkpoint；按 Event ID 查询
canonical object 和 trace，区分物化与索引失败。

## Latest state is old

确认 scope/state key、Event logical order、state version、materializer status 和 query filter。向量 top-1 不是 latest。

## Cold query fails

检查 `include_cold`、S3 endpoint/TLS/credential/bucket/prefix、对象 key 和 edge index。

## Disk usage grows

分别检查 Badger value log、WAL retention、native segments、purge queue 和 cold archive。不要直接删除任一子目录。

## Admin returns 401

确认 `PLASMOD_ADMIN_API_KEY` 与 header，注意 split 模式 admin route 在 9091。不要为排错永久关闭认证。
