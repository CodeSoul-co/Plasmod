# Plasmod Node / TypeScript SDK 0.2

Use this SDK with a server implementing the [memory lifecycle contract](../../docs/16-memory-lifecycle-integration.md). The historical package name stays `andb-sdk-node`; `AndbClient` is a compatibility alias for `PlasmodClient`.

```ts
import { PlasmodClient, PlasmodError } from "andb-sdk-node";

const client = new PlasmodClient("http://127.0.0.1:8080", {
  headers: { authorization: "Bearer your-proxy-token" },
});
const scope = { tenant_id: "user", workspace_id: "partner", agent_id: "agent" };
const receipt = await client.maintain({
  ...scope, operation_id: "decay-2026-10-30-page-1", operation: "decay", limit: 100,
});
const persisted = await client.maintenanceStatus(receipt.operation_id, scope);
```

Methods: ingestEvent, ingestDocument, query, recall, listMemories, listStates, evidence, maintain, maintenanceStatus, mutate, forget, correct, mutationStatus, getConsistencyMode, setConsistencyMode. Domain methods accept request options with `signal` and `headers`. Scope is passed through without rewriting identity or access.

`PlasmodError` includes status, code, detail and effectUnknown. Transport errors, malformed write responses, and server 5xx can have unknown effects. No write is automatically retried. Read the operation receipt first; only replay the same operation request when its server contract permits recovery. A 200 response can contain `status: partial`; inspect failed_count/items or error. Forgetting is logical, not physical backup erasure.

The API is v1 and these maintenance/mutation methods require this server revision or later; older servers can return 404/400. There is no fallback to combining delete-and-write, and no client-side admission/decay logic. The SDK does not authenticate end users; configure your authorized proxy and bind tenant/agent identity there.

Run `npm test` for the transport-contract tests.
