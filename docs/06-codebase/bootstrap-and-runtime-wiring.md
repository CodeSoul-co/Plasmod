# Bootstrap And Runtime Wiring

## Entry

`src/cmd/server/main.go`：

1. 调用 `app.BuildServer`；
2. 注册 defer shutdown；
3. 调用 `app.RunServers`；
4. 处理启动错误和信号退出。

## BuildServer 顺序

`src/internal/app/bootstrap.go` 依次组装：

1. clock、bus、WAL；
2. RuntimeStorage 和 cold store；
3. semantic、materialization、evidence；
4. embedder 和 DataPlane；
5. node manager 和 active coordinator hub；
6. worker Runtime；
7. consistency controller/checkpoint；
8. Gateway、transport 和可选 gRPC。

顺序体现依赖方向：Gateway 不直接创建 Badger/native index，worker 不直接读取环境变量决定 HTTP 端口。

## Shutdown

关闭要停止 HTTP/gRPC 接收、Gateway background manager、consistency worker、runtime、storage/WAL 等资源。
新增后台 goroutine 必须挂到 server shutdown，不得依赖进程强制退出。
