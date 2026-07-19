# Add An Agent Framework Adapter

Adapter 的职责是把 framework lifecycle 转换为 Plasmod Event/Query，而不是把 framework 状态塞进自由文本。

## Mapping

- conversation/task -> Session；
- model/tool callback -> Event；
- durable memory -> Memory target；
- environment mutation -> AgentState；
- plan/report/file -> Artifact；
- dependency/evidence -> Edge；
- framework identity -> tenant/workspace/agent scope。

## Requirements

稳定 ID、时间/逻辑顺序、重试幂等、precomputed embedding、consistency 选择、错误回传、shutdown flush 和版本兼容。

Adapter 放在独立 SDK/package，不把特定 framework 依赖引入 core composition root。
