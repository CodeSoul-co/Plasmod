# 07. Scope, Governance, Collaboration, and Security

> Language: [中文](../07-governance-collaboration-and-security.md) | English

---

This chapter defines scope resolution, share contracts, policy enforcement, collaborative derivation, conflict handling, deletion, isolation, and security boundaries.

---

## 07.1. Memory Scope and Governance Mechanism

### 07.1.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Tenant/workspace/team/agent/session scope decides visibility, sharing, derivation, TTL and quarantine |
| Maturity | Schema, stores, and policy primitives are Implemented; centralized enforcement is Partial. |

### 07.1.2. Scope representation

| Level | Fields/records |
|---|---|
| tenant/workspace | Event identity, Agent, QueryRequest |
| team/agent/session | Event actor, Memory AgentID/SessionID/Scope |
| visibility | Event access, policy tags, PolicyRecord |
| sharing | ShareContract + shared Memory copy |
| lifecycle governance | TTL, quarantine, verified state, AuditRecord |

The `MemoryScope` enum includes private user or agent scope, session-local scope, and workspace, team, global, or restricted shared scope. The materializer often resolves `Memory.Scope` to a workspace, retrieval, or session namespace string instead of copying the enum value directly.

### 07.1.3. Engines and APIs

PolicyEngine, ReflectionPolicyWorker, Policy/Contract stores, Runtime filters/contamination detector, Gateway visibility middleware; canonical policy/share routes and internal share/query APIs.

### 07.1.4. Decisions

| Decision | Current implementation |
|---|---|
| TTL/quarantine/confidence/salience | reflection and PolicyEngine helpers |
| read ACL | `IsACLAllowed` helper and contamination contract check; not universal gate |
| write/derive ACL | fields stored, no central mandatory evaluator |
| allow/deny/mask/partial | Audit decision supports values; runtime composer not complete |
| share copy | CommunicationWorker |
| conflict merge | LWW worker, not contract-selected strategy |

### 07.1.5. State and audit

`PolicyRecord` and audit records are append-only; ShareContract supports put/list operations; `PolicyDecisionLog` is maintained separately. Memory stores its own scope, tags, TTL, and lifecycle state. Read responses do not carry a universal policy-snapshot ID.

### 07.1.6. Sync/async

Query filtering and annotation are synchronous. Reflection runs asynchronously after ingest. Share and conflict operations are synchronous internal calls, while reflection may archive memories. Direct canonical routes may bypass policy evaluation.

### 07.1.7. Correctness/security

Contamination metric detects returned cross-agent Memory without contract but does not remove it.Internal routes lack admin auth by default.Production visibility strips debug fields but is not object ACL.

### 07.1.8. Claim Boundaries

Supported claim: Plasmod provides scoped schemas, policy/share/audit records, TTL and quarantine helpers, and contamination observation.

Do not claim complete hierarchical ACLs, deny-by-default enforcement, field masking, contract-enforced derive/write operations, or zero cross-agent leakage.

### 07.1.9. Gaps

Need canonical ScopeResolver, PolicyDecision{allow/deny/partial/mask/quarantine}, mandatory Gateway/Runtime enforcement, policy composition/version snapshot, shared/derived command authorization, leakage prevention tests and decision logging on every access.

---

## 07.2. Memory Governance Engine

### 07.2.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Policy Engine + reflection/access/visibility helpers |
| Goals | Analyze scope/policy/contract and output decisions such as allow/deny/partial/mask/quarantine |
| Critical-path role | Some of the routes to query/maintenance/collaboration/admin |
| Maturity | Partial |

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

Inputs include requester, scope, Memory, PolicyRecord, ShareContract, and time. Outputs are booleans, effective values, filter descriptions, annotations, or Memory mutations. No unified `PolicyDecision` struct is used on every path.

Reflection can mark Memory inactive and quarantined or decayed, override confidence, reduce salience, and archive to Cold. Query Assembler annotates quarantine and retraction; direct share and canonical writes are not all centrally authorized.

### 07.2.6. Call relationships

Policy coordinator wraps the engine and store; Runtime invokes planning, filtering, and detection; subscribers invoke reflection; Evidence Assembler reads PolicyStore; Gateway handles canonical and admin controls. These are parallel governance hooks, not one mandatory policy middleware.

### 07.2.7. Correctness/security

- PolicyRecord append-only but "latest" selection sometimes uses PolicyEventID lexical comparison.
- `ReadACL` is one string, not general principal list/expression.
- WriteACL/DeriveACL/merge/quarantine contract fields lack universal evaluator.
- Internal routes are not covered by admin key automatically.
- response visibility and object authorization are different mechanisms.

### 07.2.8. Claim Boundaries

Supported claim: Plasmod provides policy/share/audit schemas, TTL/quarantine/score helpers, reflection enforcement, and contamination observation.

Do not claim complete hierarchical ACLs, policy composition and version snapshots, deny-by-default enforcement, a partial/mask response engine, or a zero-leakage guarantee.

### 07.2.9. Gaps

Remaining work includes defining `GovernanceRequest` and `PolicyDecision`, a principal/scope resolver, policy precedence and versioning, mandatory authorization middleware, read/write/derive/share evaluators, a mask/partial transformer, policy-safe graph traversal, decision logging, and exhaustive access-matrix tests.

---

## 07.3. Security Model

### 07.3.1. Current Implementation

- `PLASMOD_ADMIN_API_KEY` enables shared-key authentication for `/v1/admin/*`.
- Clients may use `X-Admin-Key` or `Authorization: Bearer`; an HMAC digest is compared in constant time.
- `APP_MODE=prod` removes debug, raw, log, and chain-trace fields from JSON responses.
- Gateway limits resource use through a write semaphore, payload validation, and selected batch limits.

### 07.3.2. Not Provided

- TLS termination, mTLS, OAuth/OIDC, user login, and fine-grained RBAC.
- Built-in authentication for `/v1/internal/*`; deployments must protect these routes at the network layer.
- Complete tenant/workspace isolation and row-level authorization.
- Secret management, key rotation, KMS, and S3 IAM configuration management.

### 07.3.3. Deployment Requirements

1. Set the admin key in every non-development environment.
2. Provide TLS, identity verification, network policy, and request limits through a reverse proxy or service mesh.
3. In split mode, restrict management `9091`, application API `19530`, and gRPC `19531` independently.
4. Do not expose the MinIO console, and replace the Compose default credentials.
5. Use least-privilege S3 credentials and a dedicated bucket or prefix.
6. Set `APP_MODE=prod` and run `make prod-safety-check`.

### 07.3.4. Data Access Semantics

Event access fields, PolicyRecord, and ShareContract describe application-level visibility; they do not authenticate the caller. Policy evaluation becomes a security boundary only when the service can bind each request to a verified identity.
