# Delete And Purge

```text
admin delete/purge request
  -> admin auth
  -> validate selector and scope
  -> hardDeleteManager task (where applicable)
  -> canonical/index/audit/outbox deletion stages
  -> optional cold deletion
  -> task status/metrics
```

多 store 清理不是单一分布式事务。处理器保存 task progress，以便区分排队、运行、取消、失败和完成。
