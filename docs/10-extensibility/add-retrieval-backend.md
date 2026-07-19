# Add A Retrieval Backend

实现 `retrievalplane`/DataPlane 所需 search/storage contracts，并保留 Go 层业务语义。

必须定义：

- index types 和 build/load lifecycle；
- metric、dimension、embedding family；
- batch 和 concurrency；
- segment persistence；
- timeout/cancel/error mapping；
- delete/reindex/compaction；
- handle/resource ownership。

Backend 返回 object ID + score/candidate metadata；tenant policy、canonical load、fusion 和 Evidence 仍由 Go 完成。

对不支持的 index type 返回明确错误，不静默回退成不同算法。
