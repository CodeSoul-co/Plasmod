# Common Code Changes

## Add Event Type

更新 constants/validation、materializer dispatch、query filters、tests 和 schema docs。

## Add Canonical Object

更新 schema、ObjectStore/RuntimeStorage、memory+Badger+S3 backend、key prefix、Gateway、query/evidence、replay、
delete/purge 和 migration。

## Add Query Filter

更新 QueryRequest JSON tag、planner、所有 candidate path、evidence/cold path、SDK 和 contract tests。过滤必须在
返回前对所有层一致执行。

## Add Environment Variable

在拥有该配置的 package 解析并验证，加入 effective config（脱敏）、Compose/template、配置文档和 tests。

## Add Background Worker

通过 app composition 创建，使用 context/stop channel，定义 queue/backpressure/metrics，并接入 shutdown。
