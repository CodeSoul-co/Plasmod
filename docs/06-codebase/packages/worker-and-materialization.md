# worker And materialization

`worker.Runtime` 接收 Event 并协调 WAL、materialization、projection。`worker/consistency.Controller` 提供模式、
queue、slot、retry、tracker 和 checkpoint。

`materialization.Service` 负责通用 Event 到 Memory/Artifact/Edge/Version；`worker/nodes` 还包含 state、object、
tool trace、index、proof、algorithm dispatch 等 worker contract/实现。

增加 materializer 时要明确 deterministic ID、重放重入、canonical transaction、projection failure 和 tracker
推进条件。
