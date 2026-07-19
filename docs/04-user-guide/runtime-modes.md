# Runtime Modes

Plasmod 有多组彼此独立的模式，不能只用一个“生产模式”概括。

## APP_MODE

- `test`：响应可包含 `_debug` 和内部字段；
- `prod`：visibility middleware 删除 debug、raw、log、chain traces 等字段。

它影响响应暴露面，不自动开启 TLS 或用户认证。

## Storage Mode

- `disk`：默认，Badger + FileWAL，可恢复；
- `memory`：进程内 store + InMemoryWAL，仅适合测试和临时运行。

## Consistency Mode

- `strict_visible`/`strict`；
- `bounded_staleness`/`bounded`；
- `eventual_visibility`/`eventual`。

服务默认值可由管理 API 修改，单个 Event 也可覆盖。

## Governance 与 Memory Provider Mode

管理 API 可以调整 governance mode、runtime mode、memory provider mode，并查询 provider health。这些切换
改变协调和策略行为，不会迁移已有数据格式。

## Unified 与 Split Server

统一模式共享一个 HTTP 监听器；拆分模式把管理面和数据面分开。二者使用相同核心 runtime，不代表两个
独立数据库实例。
