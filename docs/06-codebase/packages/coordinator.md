# coordinator

默认 active coordinator 由顶层 `hub.go` 及 object/memory/index/policy 等轻量 coordinator 组成，它们包裹
storage 和模块注册。

`coordinator/controlplane` 是上游兼容控制面，包含 meta/data/query/access proxy 组件。其存在支持未来/兼容
集成，但默认 BuildServer 不创建完整分布式集群。

新增 core coordination 优先扩展 active Hub contract；只有明确接入上游 lifecycle 时才修改 controlplane。
