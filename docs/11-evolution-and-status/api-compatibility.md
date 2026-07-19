# API Compatibility

## Public HTTP

Changes to `/v1/ingest/events`、`/v1/query`、canonical collections 和 Trace 需要兼容评估。新增 optional field
优于改变 existing field type/default。

## Internal API

`/v1/internal/*` 与 transport routes 只保证同版本组件。仍应避免无迁移地破坏已部署 adapters。

## SDK

Python SDK release 应标明服务版本；先让服务端接受新字段，再发布使用新字段的 SDK。Node SDK 的旧命名需要
独立迁移方案。

## Error behavior

HTTP status、plain-text/JSON error body、query status 都属于客户端可观察行为。改变 error mapping 需测试和
release note。
