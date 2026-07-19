# schemas

`internal/schemas` 是 API 和持久化共同依赖的类型层：

- `dynamic_event.go`：v0.4 嵌套 Event 和 legacy alias normalize；
- `canonical.go`：对象、关系、版本、policy、share contract；
- `constants.go`：object/event/edge/memory 类型；
- `query.go`：query/filter/evidence response；
- 其他文件：retrieval、governance、memory algorithm 和扩展类型。

Schema package 不应依赖 Gateway 或具体 Badger 实现。变更 JSON tag 前先搜索 SDK、WAL fixture、storage codec
和 handler tests。
