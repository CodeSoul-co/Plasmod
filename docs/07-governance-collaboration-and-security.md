# 07. 作用域、治理、协作与安全

---

核对 Scope、ShareContract、Policy、协作派生、冲突、删除、隔离和安全边界。

---

## 07.1. Memory Scope and Governance Mechanism

### 07.1.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Tenant/workspace/team/agent/session scope 决定 visibility、sharing、derivation、TTL 和 quarantine |
| 成熟度 | schema/store/policy primitives 完整，central enforcement 部分 |

### 07.1.2. Scope representation

| Level | Fields/records |
|---|---|
| tenant/workspace | Event identity, Agent, QueryRequest |
| team/agent/session | Event actor, Memory AgentID/SessionID/Scope |
| visibility | Event access, policy tags, PolicyRecord |
| sharing | ShareContract + shared Memory copy |
| lifecycle governance | TTL, quarantine, verified state, AuditRecord |

MemoryScope enum includes private user/agent, session local, workspace/team/global/restricted shared；actual materializer often resolves `Memory.Scope` to workspace/retrieval/session namespace string rather than always using enum value。

### 07.1.3. Engines and APIs

PolicyEngine, ReflectionPolicyWorker, Policy/Contract stores, Runtime filters/contamination detector, Gateway visibility middleware；canonical policy/share routes and internal share/query APIs。

### 07.1.4. Decisions

| Decision | Current implementation |
|---|---|
| TTL/quarantine/confidence/salience | reflection and PolicyEngine helpers |
| read ACL | `IsACLAllowed` helper and contamination contract check；not universal gate |
| write/derive ACL | fields stored, no central mandatory evaluator |
| allow/deny/mask/partial | Audit decision supports values; runtime composer not complete |
| share copy | CommunicationWorker |
| conflict merge | LWW worker, not contract-selected strategy |

### 07.1.5. State and audit

PolicyRecord append-only；ShareContract put/list；Audit append-only；PolicyDecisionLog separate。Memory itself stores scope/tags/TTL/lifecycle。No single policy snapshot ID is attached to every read response。

### 07.1.6. Sync/async

Query filtering/annotation synchronous；reflection asynchronous after ingest；share/conflict synchronous internal call；archive can occur in reflection。Direct canonical routes may bypass policy evaluation。

### 07.1.7. Correctness/security

Contamination metric detects returned cross-agent Memory without contract but does not remove it。Internal routes lack admin auth by default。Production visibility strips debug fields but is not object ACL。

### 07.1.8. 声明边界

可声明 scoped schema, policy/share/audit records, TTL/quarantine helpers and contamination observation。

不可声明 complete hierarchical ACL, deny-by-default, field masking, contract-enforced derive/write or zero cross-agent leakage。

### 07.1.9. 缺口

Need canonical ScopeResolver, PolicyDecision{allow/deny/partial/mask/quarantine}, mandatory Gateway/Runtime enforcement, policy composition/version snapshot, shared/derived command authorization, leakage prevention tests and decision logging on every access。

---

## 07.2. Memory Governance Engine

### 07.2.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Policy Engine + reflection/access/visibility helpers |
| 目标 | 解析 scope/policy/contract 并输出 allow/deny/partial/mask/quarantine 等决策 |
| 关键路径 | query/maintenance/collaboration/admin 的部分路径 |
| 成熟度 | 部分 |

### 07.2.2. Code entry

| Item | Code |
|---|---|
| Core helpers | `semantic/policy.go: PolicyEngine` |
| Worker | `cognitive/baseline/reflection.go` |
| Stores | PolicyStore, ShareContractStore, AuditStore |
| Logs | PolicyDecisionLog |
| Runtime | query filters, contamination detector, governance mode |
| Access | admin auth, production response visibility |
| API | policies/contracts CRUD, internal share/query, admin governance mode |

### 07.2.3. Engine fields

| Type | Fields |
|---|---|
| `PolicyEngine` | stateless/no fields |
| Reflection worker | `id`, ObjectStore, PolicyStore, PolicyDecisionLogger, optional AuditStore, TieredObjectStore |
| Runtime governance state | policy engine, policy log, `GovernanceDisabled` flag, stores |
| Policy data | Policy/PolicyRecord/ShareContract/AuditRecord fields in registry |

### 07.2.4. Methods and decisions

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

### 07.2.5. Input/output and state mutations

Input：requester/scope/Memory/PolicyRecord/ShareContract/time。Current outputs are booleans, effective values, filter descriptions, annotations or Memory mutation。There is no unified `PolicyDecision` struct used on every path。

Reflection can set inactive + quarantined/decayed, override confidence, decay salience and archive to cold。Query Assembler annotates quarantine/retracted；direct share/canonical writes are not all centrally authorized。

### 07.2.6. Call relationships

Policy coordinator wraps engine/store；Runtime calls planner/filter/detection；subscriber calls reflection；Evidence Assembler reads PolicyStore；Gateway handles canonical/admin controls。These are parallel governance hooks, not one mandatory policy middleware。

### 07.2.7. Correctness/security

- PolicyRecord append-only but “latest” selection sometimes uses PolicyEventID lexical comparison。
- `ReadACL` is one string, not general principal list/expression。
- WriteACL/DeriveACL/merge/quarantine contract fields lack universal evaluator。
- Internal routes are not covered by admin key automatically。
- response visibility and object authorization are different mechanisms。

### 07.2.8. 声明边界

可声明 policy/share/audit schema, TTL/quarantine/score helpers, reflection enforcement and contamination observation。

不可声明 full hierarchical ACL, policy composition/version snapshots, deny-by-default, partial/mask response engine or zero-leak guarantee。

### 07.2.9. 缺口

Define `GovernanceRequest/PolicyDecision`, principal/scope resolver, policy precedence/versioning, mandatory authorization middleware, read/write/derive/share evaluators, mask/partial transformer, policy-safe graph traversal, decision logging and exhaustive access matrix tests。

---

## 07.3. 安全模型

### 07.3.1. 当前实现

- `PLASMOD_ADMIN_API_KEY` 开启 `/v1/admin/*` shared-key 认证。
- 支持 `X-Admin-Key` 或 `Authorization: Bearer`，使用固定长度 HMAC digest 做 constant-time compare。
- `APP_MODE=prod` 清理 JSON 中的 debug/raw/log/chain trace 等字段。
- Gateway 通过 write semaphore、payload checks 和部分 batch limits 限制资源消耗。

### 07.3.2. 未提供

- TLS termination、mTLS、OAuth/OIDC、用户登录、细粒度 RBAC。
- 对 `/v1/internal/*` 和普通 data routes 的统一认证。
- 完整 tenant/workspace 强制隔离和 row-level authorization。
- secret manager、key rotation、KMS 或 S3 IAM 配置管理。

### 07.3.3. 部署要求

1. 在非本机环境设置 admin key。
2. 通过反向代理/service mesh 提供 TLS、身份认证、IP/network policy 和 request limits。
3. split mode 下分别限制 9091 management、19530 API 和 19531 gRPC。
4. 不暴露 MinIO console；替换 compose 默认凭据。
5. 使用最小权限 S3 credential 和独立 bucket/prefix。
6. `APP_MODE=prod`，并运行 `make prod-safety-check`。

### 07.3.4. 数据访问语义

Event access fields、PolicyRecord 和 ShareContract 是应用级可见性描述，不等同于已认证主体。只有当服务入口能可信地绑定 caller identity，policy evaluation 才能形成安全边界。
