# WAL Stream

## 接口

Transport server 注册 `/v1/wal/stream`，用于从指定位置传输 WAL records。核心接口定义在
`src/internal/eventbackbone`：

```go
type WAL interface {
    Append(Event) (LSN, error)
    Scan(from LSN, fn func(Record) bool)
    LatestLSN() LSN
}
```

支持错误传播的实现还实现 `ErrorAwareWAL`。

## 语义

- LSN 表示日志顺序，不等于 wall-clock 时间；
- stream record 是 Event 事实，不是已经物化的 object snapshot；
- 消费者必须处理断线、重复和从 checkpoint 恢复；
- scan 完成不代表 retrieval projection 已完成；
- FileWAL 损坏必须显式报错，不能把尾部缺失当作正常 EOF。

## 安全边界

WAL 可能包含原始 payload 和跨 scope 事件。该 route 应只对受信节点开放，并通过网络身份、TLS 和最小
权限控制。它不是公共变更数据捕获 API。
