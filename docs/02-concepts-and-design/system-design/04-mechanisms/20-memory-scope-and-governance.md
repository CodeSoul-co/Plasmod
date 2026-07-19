# 20. Memory Scope and Governance Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Tenant/workspace/team/agent/session scope 决定 visibility、sharing、derivation、TTL 和 quarantine |
| 成熟度 | schema/store/policy primitives 完整，central enforcement 部分 |

## 2. Scope representation

| Level | Fields/records |
|---|---|
| tenant/workspace | Event identity, Agent, QueryRequest |
| team/agent/session | Event actor, Memory AgentID/SessionID/Scope |
| visibility | Event access, policy tags, PolicyRecord |
| sharing | ShareContract + shared Memory copy |
| lifecycle governance | TTL, quarantine, verified state, AuditRecord |

MemoryScope enum includes private user/agent, session local, workspace/team/global/restricted shared；actual materializer often resolves `Memory.Scope` to workspace/retrieval/session namespace string rather than always using enum value。

## 3. Engines and APIs

PolicyEngine, ReflectionPolicyWorker, Policy/Contract stores, Runtime filters/contamination detector, Gateway visibility middleware；canonical policy/share routes and internal share/query APIs。

## 4. Decisions

| Decision | Current implementation |
|---|---|
| TTL/quarantine/confidence/salience | reflection and PolicyEngine helpers |
| read ACL | `IsACLAllowed` helper and contamination contract check；not universal gate |
| write/derive ACL | fields stored, no central mandatory evaluator |
| allow/deny/mask/partial | Audit decision supports values; runtime composer not complete |
| share copy | CommunicationWorker |
| conflict merge | LWW worker, not contract-selected strategy |

## 5. State and audit

PolicyRecord append-only；ShareContract put/list；Audit append-only；PolicyDecisionLog separate。Memory itself stores scope/tags/TTL/lifecycle。No single policy snapshot ID is attached to every read response。

## 6. Sync/async

Query filtering/annotation synchronous；reflection asynchronous after ingest；share/conflict synchronous internal call；archive can occur in reflection。Direct canonical routes may bypass policy evaluation。

## 7. Correctness/security

Contamination metric detects returned cross-agent Memory without contract but does not remove it。Internal routes lack admin auth by default。Production visibility strips debug fields but is not object ACL。

## 8. 声明边界

可声明 scoped schema, policy/share/audit records, TTL/quarantine helpers and contamination observation。

不可声明 complete hierarchical ACL, deny-by-default, field masking, contract-enforced derive/write or zero cross-agent leakage。

## 9. 缺口

Need canonical ScopeResolver, PolicyDecision{allow/deny/partial/mask/quarantine}, mandatory Gateway/Runtime enforcement, policy composition/version snapshot, shared/derived command authorization, leakage prevention tests and decision logging on every access。
