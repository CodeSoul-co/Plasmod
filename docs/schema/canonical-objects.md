# Canonical Objects

## 1. Purpose

This document defines the canonical objects used by the ANDB v1 prototype. These objects are the first-class semantic units of the system and are the backbone for event evolution, retrieval, provenance, graph expansion, and structured response assembly.

The semantic definitions in this document should be read together with the implementation structs in [`src/internal/schemas/canonical.go`](../../src/internal/schemas/canonical.go). If there is a mismatch, current field names in code take precedence for immediate implementation, and the docs should then be updated.

## 2. Why Canonical Objects Matter

ANDB explicitly moves away from the idea that agent cognition can be represented by a single table like:

`memory(id, content, embedding, metadata)`

That approach does not naturally capture:

- event source
- state evolution
- relation structure
- runtime state
- artifact linkage
- provenance
- versioning

Canonical objects separate those concerns into stable semantic units.

## 3. v1 Canonical Object Set

The v1 prototype includes:

- `Agent`
- `Session`
- `Event`
- `Memory`
- `State`
- `Artifact`
- `Edge`
- `ObjectVersion`

Among these, the operational core of v1 is:

- `Event`
- `Memory`
- `State`
- `Artifact`
- `Edge`
- `ObjectVersion`

`Agent` and `Session` remain foundational because they define ownership, scope, and execution context.

## 4. Agent

### 4.1 Meaning

`Agent` represents an execution identity inside the MAS context. It is the namespace anchor for actions, memories, and state.

### 4.2 Current Fields

Current Go fields:

- `agent_id`
- `tenant_id`
- `workspace_id`
- `agent_type`
- `role_profile`
- `policy_ref`
- `capability_set`
- `default_memory_policy`
- `created_at`
- `status`

### 4.3 Role

`Agent` is used to:

- scope events and memories
- partition query context
- attach policy defaults
- define ownership boundaries

## 5. Session

### 5.1 Meaning

`Session` represents a task, thread, or execution context in which events occur and runtime state evolves.

### 5.2 Current Fields

- `session_id`
- `agent_id`
- `parent_session_id`
- `task_type`
- `goal`
- `context_ref`
- `start_ts`
- `end_ts`
- `status`
- `budget_token`
- `budget_time_ms`

### 5.3 Role

`Session` is used to:

- group event flows
- bind runtime state
- constrain retrieval context
- support local task-level reasoning

## 6. Event

### 6.1 Meaning

`Event` is the fundamental source of state evolution. Events capture messages, tool calls, tool results, plan updates, critiques, retrieval operations, and task transitions.

### 6.2 Typical Event Types

- `user_message`
- `assistant_message`
- `tool_call_issued`
- `tool_result_returned`
- `retrieval_executed`
- `plan_updated`
- `critique_generated`
- `task_finished`
- `handoff_occurred`

### 6.3 Current Fields

- `event_id`
- `tenant_id`
- `workspace_id`
- `agent_id`
- `session_id`
- `event_type`
- `event_time`
- `ingest_time`
- `visible_time`
- `logical_ts`
- `parent_event_id`
- `causal_refs`
- `payload`
- `source`
- `importance`
- `visibility`
- `version`

### 6.4 Field Notes

`visible_time` and `logical_ts` already exist in the current Go schema. In v1 they should be treated as reserved-but-useful fields: present in the contract, but not yet backed by a full publication or logical-time system.

`payload` is intentionally flexible because event content varies by event type. Its semantic interpretation belongs to the materialization layer.

### 6.5 Role

`Event` serves as:

- ingest-level source of truth
- provenance anchor
- replay-ready mutation record
- trigger source for canonical-object materialization

## 7. Memory

### 7.1 Meaning

`Memory` is a reusable cognitive unit derived from one or more events or summaries. It is not identical to raw event payload. It represents something the system should later retrieve and reason over.

### 7.2 Memory Types

Suggested v1 memory categories:

- `episodic`
- `semantic`
- `procedural`
- `social`
- `reflective`

### 7.3 Current Fields

- `memory_id`
- `memory_type`
- `agent_id`
- `session_id`
- `scope`
- `level`
- `content`
- `summary`
- `source_event_ids`
- `confidence`
- `importance`
- `freshness_score`
- `ttl`
- `valid_from`
- `valid_to`
- `provenance_ref`
- `version`
- `is_active`

### 7.4 Field Notes

`scope` is expected to be a simple string in v1, such as `private`, `session`, `workspace`, or `shared`.

`level` represents distillation depth, for example:

- `0`: raw or near-raw record
- `1`: summary
- `2`: higher-level abstraction

`source_event_ids` is critical and should not be dropped. It is the most direct provenance bridge between event origin and reusable memory.

### 7.5 Role

`Memory` is the primary retrieval-oriented cognitive object in v1. It should be:

- retrievable
- filterable
- provenance-linked
- relation-expandable

## 8. State

### 8.1 Meaning

`State` captures current or operational execution condition rather than reusable long-term knowledge.

### 8.2 Typical State Examples

- current plan
- tool stack
- execution status
- budget state
- temporary blackboard
- failure marker

### 8.3 Current Fields

- `state_id`
- `agent_id`
- `session_id`
- `state_type`
- `state_key`
- `state_value`
- `derived_from_event_id`
- `checkpoint_ts`
- `version`

### 8.4 Role

`State` is used to:

- track runtime execution context
- explain why an agent is currently blocked or active
- support runtime-aware retrieval
- attach evidence to live operating conditions

## 9. Artifact

### 9.1 Meaning

`Artifact` represents external or derived work products. These are outputs that should remain linked to cognition and provenance rather than floating outside the database model.

### 9.2 Typical Artifact Examples

- documents
- code
- SQL
- reports
- files
- API outputs
- generated blobs

### 9.3 Current Fields

- `artifact_id`
- `session_id`
- `owner_agent_id`
- `artifact_type`
- `uri`
- `content_ref`
- `mime_type`
- `metadata`
- `hash`
- `produced_by_event_id`
- `version`

### 9.4 Role

`Artifact` is used to:

- preserve tool outputs
- bridge external actions back into the evidence graph
- support explainability and reproducibility
- anchor references to large content outside inline event payloads

## 10. Edge

### 10.1 Meaning

`Edge` represents an explicit typed relation between canonical objects. ANDB does not want relation semantics to disappear inside implicit application joins.

### 10.2 Typical Edge Types

- `caused_by`
- `derived_from`
- `supports`
- `contradicts`
- `summarizes`
- `updates`
- `uses_tool`
- `belongs_to_task`
- `shared_with`

### 10.3 Current Fields

- `edge_id`
- `src_object_id`
- `src_type`
- `edge_type`
- `dst_object_id`
- `dst_type`
- `weight`
- `provenance_ref`
- `created_ts`

### 10.4 Role

`Edge` is essential for:

- graph expansion
- evidence assembly
- provenance chaining
- proof-trace explanation

## 11. ObjectVersion

### 11.1 Meaning

`ObjectVersion` records lineage for mutable canonical objects.

### 11.2 Current Fields

- `object_id`
- `object_type`
- `version`
- `mutation_event_id`
- `valid_from`
- `valid_to`
- `snapshot_tag`

### 11.3 Role

`ObjectVersion` allows ANDB to:

- track object evolution
- attach version hints in responses
- preserve mutation provenance
- prepare for future rollback and time-travel behavior

In v1, this is intentionally lighter than a full visibility engine.

## 12. Relationship Patterns

Common v1 relationships include:

- `Event -> Memory`
- `Event -> State`
- `Event -> Artifact`
- `Event -> ObjectVersion`
- `Memory -> Event`
- `Memory -> Artifact`
- `Memory -> Memory`
- `State -> Event`
- `Artifact -> Event`

These relationships may be represented through explicit edges or direct object references depending on the layer.

## 13. v1 Simplifications

The following are explicitly acceptable in v1:

1. policy/governance objects may remain reserved rather than fully operational
2. share contracts are not required
3. logical time semantics may remain shallow
4. conflict/merge objects are deferred
5. some fields may be present before their full runtime behavior exists

These simplifications are acceptable only if the contracts remain extensible.

## 14. Design Rules

### Rule 1

Every retrievable cognitive unit should map back to a canonical object.

### Rule 2

Every derived object should preserve provenance to source event(s).

### Rule 3

Every mutable object should carry version semantics.

### Rule 4

Every structure needed for evidence assembly should be representable through explicit edges or object references.

### Rule 5

In v1, schema stability is more important than field completeness.

## 15. Summary

Canonical objects are the semantic backbone of ANDB. They define what the system fundamentally stores, materializes, retrieves, relates, and returns.

They should be treated as the primary abstraction of the repository, not as incidental structs.
