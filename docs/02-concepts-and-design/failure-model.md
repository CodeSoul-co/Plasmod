# 失败模型

## Before WAL

JSON 解码、schema/consistency 验证或 Gateway semaphore 失败。Event 未获得 LSN，客户端可以修正请求或使用同一 event ID 重试。

## WAL append

file IO、encoding 或 sync 失败。不得返回 accepted。运维应检查 data dir 权限、磁盘空间和 WAL 状态。

## After WAL, before visibility

Event 已有 LSN，但 projection queue、worker、canonical write 或 retrieval ingest 失败。strict 返回 `AcceptedNotVisibleError`/`ProjectionFailureError`；客户端必须保留 event ID 并查询/replay，不能盲目创建新 ID。

## Canonical projection failure

共享 Badger backend 使用 transaction；任一 object/edge/version 编码或写入失败应回滚该 transaction。memory/hybrid backend 不提供跨独立 backend 的同等原子性，因此 factory 禁止 objects/edges/versions 混用 backend。

## Retrieval projection failure

canonical data 可能已经可恢复而 index 未更新。系统应报告错误或保持 watermark，不应把 index 当作唯一事实。修复 embedder/native bridge 后使用 reindex。

## Evidence failure

edge/version/cache fragment 缺失时 QueryResponse 可能退化但仍返回对象。需要强 proof 的调用方必须检查 proof/provenance，而不是只检查 HTTP 200。

## Async worker failure

subscriber handler panic 被捕获并进入 dead-letter channel/overflow buffer；普通 error 的处理取决于 worker。二级算法失败不应倒写 WAL accepted 事实。

## Shutdown during operation

controller 停止接受新任务、取消 admission、等待 active/worker 和 checkpoint flush。超时会返回 shutdown error；不得在资源仍被使用时先关闭 Badger。

## Replay failure

WAL scan/decode、旧 schema、embedding family 或 projection error 可中断 replay。先使用 preview、备份 data dir，再执行 apply；不要通过删除 checkpoint 掩盖坏记录。
