# Server Startup

```text
cmd/server.main
  -> app.BuildServer
     -> storage.BuildRuntimeStorage
     -> materialization/evidence/semantic constructors
     -> dataplane/embedder/retrieval constructors
     -> coordinator.NewHub
     -> worker.NewRuntime
     -> consistency.NewController
     -> access.NewGateway
  -> app.RunServers
     -> unified or split HTTP
     -> optional gRPC/transport
  -> signal
     -> shutdown in dependency-safe order
```

端口解析在 `app/ports.go`，存储选择在 `storage/factory.go`。定位启动问题时按此顺序检查，而不是先进入
上游 controlplane。
