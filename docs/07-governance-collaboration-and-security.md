# 07. 作用域、治理、协作与安全

> Language: 中文 | [English](en/07-governance-collaboration-and-security.md)

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
| team/agent/session | Event actor + canonical `CanonicalAccess` |
| visibility | `CanonicalAccess.visibility/visible_to_*`, policy tags, PolicyRecord |
| sharing | ShareContract + WAL-derived shared Memory/Version/Edge |
| lifecycle governance | TTL, quarantine, verified state, AuditRecord |

MemoryScope enum includes private user/agent, session local, workspace/team/global/restricted shared。`Memory.Scope` 保留兼容值；Memory、State、Artifact、Edge、ObjectVersion 上的 `CanonicalAccess` 是 Runtime access evaluator 使用的结构化表示。

### 07.1.3. Engines and APIs

PolicyEngine、ReflectionPolicyWorker、Policy/Contract stores、Runtime canonical access/evidence filters、Gateway visibility middleware；canonical policy/share routes and internal share/query APIs。

### 07.1.4. Decisions

| Decision | Current implementation |
|---|---|
| TTL/quarantine/confidence/salience | reflection and PolicyEngine helpers |
| read ACL | `EvaluateAccess` 在 `/v1/query` candidate 与 evidence endpoint 上强制执行；`AccessDecision` 解释 allow |
| write/derive ACL | `IsShareContractAllowed` 对 contract-backed share 强制 source derive + target read；其他 write path 部分 |
| allow/deny/mask/partial | Audit decision supports values; runtime composer not complete |
| share derivation | `Runtime.DispatchShareWithContract` -> derived Event/WAL/canonical projection |
| conflict merge | LWW worker, not contract-selected strategy |

### 07.1.5. State and audit

PolicyRecord append-only；ShareContract put/get/by-scope/list；Audit append-only；PolicyDecisionLog separate。Canonical objects persist `CanonicalAccess` 和 `MutationLSN`。QueryResponse 返回允许对象的 `access_decisions` 与 `read_watermark_lsn`，但仍没有覆盖每条规则组合的 policy snapshot/version ID。

### 07.1.6. Sync/async

Query candidate/evidence filtering synchronous；reflection asynchronous after ingest；share authorization 与 strict Event ingest 同步，其他 consistency mode 可按 controller 异步投影；archive can occur in reflection。Direct canonical routes may bypass policy evaluation。

### 07.1.7. Correctness/security

普通 `/v1/query` 在 hydration 前移除未授权 candidate，并在 graph expansion 后移除未授权 node/edge/proof/provenance；contamination metric 使用相同 evaluator 作为附加检测。请求显式提供 tenant 时，tenant mismatch 优先拒绝；旧客户端省略 tenant 时仍必须命中 owner、session、workspace 或显式 grant，不能把缺失 tenant 解释为 public。Internal/raw canonical routes lack uniform auth by default。Production visibility strips debug fields but is not object ACL。`GovernanceDisabled` 仍保留 WAL watermark gate，但绕过对象 scope policy。

### 07.1.8. 声明边界

可声明 structured canonical scope、query/evidence read prevention、typed/legacy ShareContract read/derive evaluation、WAL-derived sharing、policy/share/audit records、TTL/quarantine helpers and contamination observation。

不可声明 authenticated principal binding、所有 raw/admin/lifecycle write 的统一 hierarchical ACL、field masking、policy composition snapshot 或经安全审计的 zero cross-agent leakage。

### 07.1.9. 缺口

Need authentication-bound principal resolver、mask/partial/quarantine response decision、raw CRUD/lifecycle write enforcement、policy composition/version snapshot、durable access decision log、cross-process policy cache invalidation and exhaustive adversarial access tests。

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
| `EvaluateAccess` | `(AccessDecision, allowed)` from canonical scope, principal, contracts and watermark |
| `IsShareContractAllowed` | typed read/write/derive decision for agent/roles plus legacy ACL tokens |
| `IsTTLExpired` | bool |
| `IsQuarantined` | bool |
| `EffectiveSalience/Confidence` | effective score/value |
| `IsACLAllowed` | simple ReadACL equality/wildcard result |
| `IsVerified` | verification state |
| Reflection `Reflect` | Memory mutation, policy/audit log, optional archive |
| Runtime access filter | removes denied candidate IDs and graph references; emits allow decisions |
| contamination detector | uses the same evaluator and increments metrics for residual violations |

### 07.2.5. Input/output and state mutations

Input：requester agent/roles、tenant/workspace/team/session、canonical access、mutation/read watermark、PolicyRecord、ShareContract、time。Read-path output uses `AccessDecision{object_id, principal_id, visibility, reason, share_contract_id, mutation_lsn}`；其他 lifecycle helper 仍返回 bool/value/mutation，因此尚无覆盖 allow/deny/partial/mask/quarantine 的全局 decision union。

Reflection can set inactive + quarantined/decayed, override confidence, decay salience and archive to cold。Query Assembler annotates quarantine/retracted；direct share/canonical writes are not all centrally authorized。

### 07.2.6. Call relationships

Policy coordinator wraps engine/store；Runtime 在 query hydration 前调用 `EvaluateAccess`，Evidence 组装后再次调用 endpoint filter，share mutation 调用 `IsShareContractAllowed`；subscriber calls reflection；Evidence Assembler reads PolicyStore；Gateway handles canonical/admin controls。Read/query 与 share 已有明确 gate，但 direct CRUD/lifecycle 仍是并行 hooks。

### 07.2.7. Correctness/security

- PolicyRecord append-only but “latest” selection sometimes uses PolicyEventID lexical comparison。
- ShareContract 同时支持 typed agent/role lists 与 legacy ACL token；不支持通用条件表达式。
- Read/write/derive 使用统一 helper，但 merge/quarantine 与所有 direct mutation 尚未统一。
- Evidence filter 同时验证 Edge 本身及两个 endpoint；拒绝 decision 不返回客户端，避免对象存在性泄漏。
- Internal routes are not covered by admin key automatically。
- response visibility and object authorization are different mechanisms。

### 07.2.8. 声明边界

可声明 canonical read access decision、watermark gate、policy-safe evidence traversal、contract-backed share authorization、policy/share/audit schema 和 reflection helpers。

不可声明 authenticated IAM、所有入口 deny-by-default、policy composition/version snapshots、partial/mask response engine 或 security-certified zero-leak guarantee。

### 07.2.9. 缺口

Bind principal to verified transport identity；extend `AccessDecision` to deny/mask/partial/quarantine without existence disclosure；apply mandatory write authorization to raw CRUD/lifecycle/conflict；add policy precedence/versioning, durable decision logging, cache invalidation and exhaustive access-matrix/security tests。

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
