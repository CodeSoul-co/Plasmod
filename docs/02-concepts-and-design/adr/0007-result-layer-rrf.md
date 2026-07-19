# ADR-0007: Fuse Retrieval Candidates At The Result Layer

## Status

Accepted.

## Context

Lexical、dense、sparse、Hot、Warm 和 Cold 候选的原始 score 尺度不同，直接数值相加会依赖 backend 的距离
定义和归一化细节。

## Decision

在 Go DataPlane 的结果层使用 rank-based fusion（RRF）合并候选，再执行 canonical load、scope/policy filter 和
Evidence assembly。Native backend 只返回自身候选和距离。

## Consequences

- 不要求不同 backend score 可直接比较；
- 可以替换 native backend 而不移动业务语义；
- rank tie、candidate depth 和 RRF constant 会影响结果，必须稳定配置；
- RRF 不替代 latest-version、policy 或 relation constraints。
