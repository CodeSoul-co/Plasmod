# Contribution Guide

## Scope

一个提交解决一个可解释问题。核心库不得引入特定外部测量流程、一次性数据路径或结果生成逻辑。

## Workflow

1. 从最新 `dev` 开始；
2. 阅读 requirements、design 和 call path；
3. 编写/更新测试；
4. 最小化修改 active ownership area；
5. 更新 API/schema/ops 文档；
6. 运行格式、tests、build 和 safety check；
7. commit 并 push `dev`。

## Review evidence

PR/commit 说明应包含：行为变化、持久化/API 影响、故障处理、验证命令、迁移要求。不能仅写“优化”或“修复”。

## Upstream code

修改 upstream snapshot 时，说明来源、原因和无法在 wrapper 层解决的证据，并保留 license。
