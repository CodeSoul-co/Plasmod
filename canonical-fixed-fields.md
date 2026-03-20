# Canonical 可固定字段建议（v1）

本文针对 `src/internal/schemas/canonical.go` 中“选择型、可枚举”的字段，给出建议固定值（Allowed Values），用于后续做强定义或校验。

## 1) Agent

- `agent_type`: `assistant` | `system` | `tool` | `user_proxy`
- `status`: `active` | `inactive` | `paused`

## 2) Session

- `task_type`: `chat` | `plan` | `workflow` | `tool_run`
- `status`: `open` | `running` | `completed` | `failed` | `cancelled`

## 3) Event

- `event_type`:
  - `user_message`
  - `assistant_message`
  - `tool_call_issued`
  - `tool_result_returned`
  - `retrieval_executed`
  - `memory_write_requested`
  - `memory_consolidated`
  - `plan_updated`
  - `critique_generated`
  - `task_finished`
  - `handoff_occurred`
- `source`: `user` | `assistant` | `system` | `tool`
- `visibility`: `private` | `workspace` | `shared` | `audit`

## 4) Memory

- `memory_type`: `episodic` | `semantic` | `procedural` | `social` | `reflective`
- `owner_type`: `agent` | `session` | `shared`
- `scope`: `private` | `session` | `workspace` | `shared`

## 5) State

- `state_type`: `runtime` | `plan` | `tool`

## 6) Artifact

- `artifact_type`: `document` | `image` | `code` | `tool_io`

## 7) Edge

- `src_type` / `dst_type`（建议统一对象类型集）:
  - `agent`
  - `session`
  - `event`
  - `memory`
  - `state`
  - `artifact`
  - `edge`
  - `object_version`
  - `user`
  - `embedding`
  - `policy`
  - `policy_record`
  - `share_contract`
- `edge_type`: `causes` | `depends_on` | `derived_from` | `mentions` | `references`

## 8) ObjectVersion

- `object_type`（与 Edge 的对象类型集保持一致）:
  - `agent`
  - `session`
  - `event`
  - `memory`
  - `state`
  - `artifact`
  - `edge`
  - `object_version`
  - `user`
  - `embedding`
  - `policy`
  - `policy_record`
  - `share_contract`

## 9) User

- `visibility`: `private` | `workspace` | `shared` | `audit`

## 10) Policy

- `publisher_type`: `system` | `agent` | `user`
- `policy_type`: `access` | `retention` | `governance` | `share` | `visibility` | `quarantine` | `consistency`

## 11) PolicyRecord

- `object_type`（同对象类型集）
- `verified_state`: `unverified` | `verified` | `disputed`
- `visibility_policy`: `normal` | `restricted` | `audit_only`

## 12) ShareContract

- `scope`: `private` | `session` | `workspace` | `shared`
- `consistency_level`: `eventual` | `session` | `strong`
- `merge_policy`: `lww` | `causal_merge` | `weighted_merge` | `crdt_partial`

---

## 说明（建议）

- 上述为 v1 推荐固定值集合，目的是减少自由输入造成的语义漂移。
- 若你希望更保守，可先仅固定高频字段：`scope`、`memory_type`、`visibility`、`object_type`。
- 正式落地时可分两步：
  1. 文档层固定 Allowed Values；
  2. 代码层引入强类型或统一 `Validate()` 校验并返回 `400`。
