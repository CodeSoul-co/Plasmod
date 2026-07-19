# Call Graph

## Write

```text
Gateway.handleIngest
  -> Runtime.SubmitIngestContext
  -> consistency.Controller
  -> WAL.Append
  -> materialization.Service
  -> RuntimeStorage canonical projection
  -> DataPlane.Ingest
  -> tracker/checkpoint
```

## Read

```text
Gateway.handleQuery
  -> Gateway.ServiceQueryContext
  -> semantic planner
  -> DataPlane Query hot/warm/(cold)
  -> canonical supplement/filter
  -> evidence assembler
  -> visibility middleware
```

## Recovery

```text
Gateway.handleAdminReplay -> WAL.Scan -> Runtime processing -> projection -> checkpoint
```
