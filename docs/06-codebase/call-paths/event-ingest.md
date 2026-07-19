# Event Ingest

```text
POST /v1/ingest/events
  -> access.Gateway.handleIngest
  -> decode/normalize schemas.Event
  -> write concurrency semaphore
  -> worker.Runtime ingest
  -> consistency.Controller submit(mode)
  -> WAL.Append -> LSN
  -> materialize canonical projection
  -> retrieval projection
  -> tracker/checkpoint advance
  -> HTTP status mapping and response
```

Backpressure/paused/accepted-not-visible/projection failure 映射为 503；deadline/cancel 映射为 504/408。
