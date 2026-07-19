# Memory Algorithm Dispatch

```text
/v1/internal/memory/<operation>
  -> Gateway request decode
  -> active provider/profile lookup
  -> AlgorithmDispatchWorker/agent SDK service
  -> canonical Memory/algorithm state update
  -> policy/lifecycle effects
  -> response
```

Provider 实现可替换，但不能绕过 canonical storage、scope 和 Event provenance。运行时切换 profile 后，已有
algorithm state 不会自动转换，需由升级逻辑处理。
