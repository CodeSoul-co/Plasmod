# Add A Canonical Object

## Required code changes

- `schemas` struct 和 object type constant；
- ObjectStore/RuntimeStorage contract；
- memory store、Badger codec/prefix、S3 cold representation；
- factory wiring；
- coordinator/handler；
- materializer 和 ObjectVersion；
- query listing/filter/evidence；
- backup/replay/delete/purge；
- SDK 和 docs。

## Required tests

- CRUD/reopen；
- transaction with Edge/Version；
- deterministic replay；
- scope/policy；
- old data compatibility；
- cold archive/read；
- purge completeness。

新增 prefix 前检查已有 key space，写入后不得在 patch release 中随意修改。
