# 29. Memory Governance Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Policy Engine + reflection/access/visibility helpers |
| 目标 | 解析 scope/policy/contract 并输出 allow/deny/partial/mask/quarantine 等决策 |
| 关键路径 | query/maintenance/collaboration/admin 的部分路径 |
| 成熟度 | 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Core helpers | `semantic/policy.go: PolicyEngine` |
| Worker | `cognitive/baseline/reflection.go` |
| Stores | PolicyStore, ShareContractStore, AuditStore |
| Logs | PolicyDecisionLog |
| Runtime | query filters, contamination detector, governance mode |
| Access | admin auth, production response visibility |
| API | policies/contracts CRUD, internal share/query, admin governance mode |

## 3. Engine fields

| Type | Fields |
|---|---|
| `PolicyEngine` | stateless/no fields |
| Reflection worker | `id`, ObjectStore, PolicyStore, PolicyDecisionLogger, optional AuditStore, TieredObjectStore |
| Runtime governance state | policy engine, policy log, `GovernanceDisabled` flag, stores |
| Policy data | Policy/PolicyRecord/ShareContract/AuditRecord fields in registry |

## 4. Methods and decisions

| Method/component | Output |
|---|---|
| `ApplyQueryFilters` | descriptive filter list from QueryRequest |
| `IsTTLExpired` | bool |
| `IsQuarantined` | bool |
| `EffectiveSalience/Confidence` | effective score/value |
| `IsACLAllowed` | simple ReadACL equality/wildcard result |
| `IsVerified` | verification state |
| Reflection `Reflect` | Memory mutation, policy/audit log, optional archive |
| contamination detector | metrics increment, not deny |

## 5. Input/output and state mutations

Input：requester/scope/Memory/PolicyRecord/ShareContract/time。Current outputs are booleans, effective values, filter descriptions, annotations or Memory mutation。There is no unified `PolicyDecision` struct used on every path。

Reflection can set inactive + quarantined/decayed, override confidence, decay salience and archive to cold。Query Assembler annotates quarantine/retracted；direct share/canonical writes are not all centrally authorized。

## 6. Call relationships

Policy coordinator wraps engine/store；Runtime calls planner/filter/detection；subscriber calls reflection；Evidence Assembler reads PolicyStore；Gateway handles canonical/admin controls。These are parallel governance hooks, not one mandatory policy middleware。

## 7. Correctness/security

- PolicyRecord append-only but “latest” selection sometimes uses PolicyEventID lexical comparison。
- `ReadACL` is one string, not general principal list/expression。
- WriteACL/DeriveACL/merge/quarantine contract fields lack universal evaluator。
- Internal routes are not covered by admin key automatically。
- response visibility and object authorization are different mechanisms。

## 8. 声明边界

可声明 policy/share/audit schema, TTL/quarantine/score helpers, reflection enforcement and contamination observation。

不可声明 full hierarchical ACL, policy composition/version snapshots, deny-by-default, partial/mask response engine or zero-leak guarantee。

## 9. 缺口

Define `GovernanceRequest/PolicyDecision`, principal/scope resolver, policy precedence/versioning, mandatory authorization middleware, read/write/derive/share evaluators, mask/partial transformer, policy-safe graph traversal, decision logging and exhaustive access matrix tests。
