# 16. Chain and companion memory integration

This contract accompanies the 2026-10-30 adoption-audit fixes. The [Chinese contract](../16-memory-lifecycle-integration.md) includes complete request examples.

## Production routing

Event and document ingestion use `MainChain.Submit` and `MainChain.Project` around the existing WAL/visibility controller. Projection stages are canonical commit, MemoryPipelineChain governance, then retrieval projection. Memory operations and recall use `MemoryPipelineChain.RunAlgorithm`; query reasoning uses `QueryChain.Run`; sharing and explicit conflict decisions use CollaborationChain. The subscriber uses the same chains, and the production Orchestrator is bound to Runtime. Failed/invalid queued tasks increment Failed, not Completed. Direct administrative canonical CRUD remains a lower-level API.

## Maintenance

POST `/v1/internal/memory/maintenance` accepts operation_id, operation, tenant_id, workspace_id, optional agent_id/session_id/memory_ids/signals/now_ts/limit/after_id. Existing ingest/update/decay/compress/summarize endpoints use this contract. Scope is required. Page size defaults to 100, maximum 1000. Each page uses its own operation ID; continue with next_cursor while has_more is true.

Receipts report selected/updated/skipped/failed counts, per-object reason and lifecycle transition, source IDs and produced IDs. GET `/v1/internal/memory/operations/{id}?tenant_id=...&workspace_id=...` reads a persisted receipt. Duplicate requests do not reinforce twice. Conflicting reuse is rejected. Interrupted maintenance reports partial rather than silently replaying an uncertain reinforcement; inspect and continue with a new operation.

Ordinary utterances remain candidates. A signal of at least 1 means explicit user confirmation, not a default for messages. Extracted facts/preferences can be promoted after matching independent evidence in three sessions and three source events, confidence >= 0.7, and no noise/serious conflict. Decay alone never turns a candidate into recallable stale memory. Ingest reevaluates matching evidence; the MemoryBank subscriber runs paged decay every ten events per agent/session. An external scheduler may trigger idle-period decay; selection and formulas stay inside Plasmod.

Derived memories preserve scope, access, source IDs, Event/Version/Edge provenance and embedding projection. Different access descriptors are not merged. Source lifecycle changes are committed only after derived index writes succeed. Canonical storage and ANN are not claimed to be one ACID transaction.

## Mutation and semantic processing

POST `/v1/internal/memory/mutate` accepts operation_id, operation (forget/correct), tenant_id, workspace_id, reference_ids, and replacement Event for correction. GET `/v1/internal/memory/mutations/{id}` with scope reads status. Partial mutations can resume using the same request. Memory-ID forgetting first revokes the original source event IDs, preventing raw copies from recreating extracted facts. Forgotten markers block raw/derived recall, traces, retained versions and WAL resurrection. This is logical forgetting, not physical erasure of backups or WAL.

Optional `PLASMOD_MEMORY_LLM_BASE_URL`, `PLASMOD_MEMORY_LLM_MODEL`, and `PLASMOD_MEMORY_LLM_API_KEY` configure Plasmod's provider. User-message extraction validates exact Unicode quote spans and confidence. Semantic summaries require per-conclusion source citations. Provider errors are explicit; without a provider summaries are labeled extractive. Real-model semantic quality requires a separate evaluation.

Explicit attribute keys support fact conflict detection; preference changes are distinguished from contradictions. POST `/v1/internal/memory/conflict/decision` takes operation_id, scope, winner_id, loser_id and kind (factual_conflict/preference_change). Decisions preserve previous evidence and valid time. Inferred profile States retain sources and versions and never overwrite explicit Agent.RoleProfile configuration.

## Extraction isolation, recovery and multilingual evidence (2026-11-02)

Queries with `tenant_id` wait for that tenant's accepted WAL prefix and use the same scoped read watermark for evidence visibility. Unrelated tenant failures do not block these reads or bounded writes. Unscoped queries remain global, and legacy events without a tenant remain dependencies of all tenants. Isolation is at tenant level, preserving dependencies across shared workspaces/agents within a tenant. The global recovery checkpoint still advances only over a contiguous successful prefix. Repair unresolved failures rather than indefinitely retaining their recovery tracking state.

GET `/v1/internal/memory/extractions/{event_id}?tenant_id=...&workspace_id=...` returns a durable receipt with source memory ID, original LSN, base projection status, extraction status/stage, pipeline attempts, fact count, timestamp and classified error code/fact index. It does not store raw provider responses. Extraction `completed` covers admission, not the subsequent base retrieval index projection. Invalid evidence stays rejected; existing projection retry limits are unchanged.

After repairing the provider/dependency, use the existing controlled POST `/v1/admin/replay` with `from_lsn` from the receipt, `limit: 1`, `apply: true`, `confirm: "apply_replay"`. Preview first by omitting apply/confirm. Replay pauses online admission and retains the original event; verify the receipt, strict retrieval and provenance before resuming the application task. Do not create a new event ID to disguise recovery.

Facts persist `fact_identity` and explicit `fact_valid_from`/`fact_valid_to`, separate from materialization time. Different text can share an identity only after the configured provider compares complete assertions and exact source quotes, preserving values, negation, constraints, uncertainty and time. Matching candidates are restricted to the same tenant/workspace/agent, type and explicit validity. Attribute keys and embedding similarity alone never merge evidence. Equivalent decisions must reference a supplied identity and both exact quotes with confidence >= 0.95; distinct/uncertain decisions remain separate. Invalid decisions fail explicitly.

Comparison may add model requests in batches of up to 32 candidates. Valid extraction and resolved identities are cached durably; successful replay reuses them. Each fact keeps its own source event/session/span/version; admission counts independent evidence and conflict detection uses the same identity. Candidate governance is reloaded before reevaluation after restart. Existing facts have a deterministic compatibility identity; replay of historical source events can reconcile previously split candidates without a database reset or automatic full-store rewrite. Real-provider semantic quality still requires separate evaluation.

## Documents, SDK, and boundaries

Document segments/manifests are durable, scoped and idempotent. Changed content for an existing segment returns 409. Missing segments and per-chunk failures are reported; Unicode offsets and source IDs survive ingestion. Multi-user callers must provide tenant/workspace even though legacy unscoped document requests remain accepted.

Node SDK 0.2 includes TypeScript declarations, ingestion/query/recall/evidence, object/state reads, maintenance, mutation and receipt methods. Writes are never silently retried; transport errors identify unknown outcomes.

Automated tests cover real HTTP routing, admission, provenance, retrieval, isolation, forgetting/correction, disk reopening, document upload, provider validation and SDK behavior. Production Shenfer version adoption, real provider quality and S3/Cold restore remain separate acceptance steps. No claims are made for distributed maintenance locks, universal semantic conflict resolution, end-to-end exactly-once model calls or built-in user login authentication.
