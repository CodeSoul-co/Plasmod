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
| team/agent/session | Event actor plus canonical `CanonicalAccess` |
| visibility | `CanonicalAccess.visibility/visible_to_*`, policy tags, PolicyRecord |
| sharing | ShareContract plus WAL-derived shared Memory/Version/Edge |
| lifecycle governance | TTL, quarantine, verified state, AuditRecord |

The `MemoryScope` enum includes private user or agent scope, session-local scope, and workspace, team, global, or restricted shared scope. `Memory.Scope` remains a compatibility field; `CanonicalAccess` on Memory, State, Artifact, Edge, and ObjectVersion is the structured representation used by Runtime authorization.

### 07.1.3. Engines and APIs

PolicyEngine, ReflectionPolicyWorker, Policy/Contract stores, Runtime canonical access/evidence filters, Gateway visibility middleware, canonical policy/share routes, and internal share/query APIs.

### 07.1.4. Decisions

| Decision | Current implementation |
|---|---|
| TTL/quarantine/confidence/salience | reflection and PolicyEngine helpers |
| read ACL | `EvaluateAccess` is mandatory for `/v1/query` candidates and evidence endpoints; `AccessDecision` explains allows |
| write/derive ACL | `IsShareContractAllowed` enforces source derive and target read for contract-backed sharing; other writes remain partial |
| allow/deny/mask/partial | Audit decision supports values; runtime composer not complete |
| share derivation | `Runtime.DispatchShareWithContract` to derived Event/WAL/canonical projection |
| conflict merge | LWW worker, not contract-selected strategy |

### 07.1.5. State and audit

`PolicyRecord` and audit records are append-only; ShareContract supports put/get/by-scope/list operations; `PolicyDecisionLog` is maintained separately. Canonical objects persist `CanonicalAccess` and `MutationLSN`. QueryResponse returns `access_decisions` for allowed objects and `read_watermark_lsn`, but no policy-snapshot/version ID represents every composed rule.

### 07.1.6. Sync/async

Query candidate and evidence filtering are synchronous. Reflection runs asynchronously after ingest. Share authorization and strict Event ingest are synchronous, while weaker consistency modes may project asynchronously. Reflection may archive memories. Direct canonical routes may bypass policy evaluation.

### 07.1.7. Correctness/security

Normal `/v1/query` removes unauthorized candidates before hydration and removes unauthorized nodes, edges, proof steps, and provenance after graph expansion. The contamination metric uses the same evaluator as an additional detector. An explicitly supplied tenant mismatch is denied first. A legacy request that omits tenant must still match owner, session, workspace, or an explicit grant; omission never makes the object public. Internal and raw canonical routes lack uniform authentication by default. Production response visibility strips debug fields but is not object authorization. `GovernanceDisabled` retains the WAL-watermark gate while bypassing object-scope policy.

### 07.1.8. Claim Boundaries

Supported claim: structured canonical scope, prevention-based query/evidence reads, typed and legacy ShareContract read/derive evaluation, WAL-derived sharing, policy/share/audit records, TTL/quarantine helpers, and contamination observation.

Do not claim authenticated principal binding, uniform hierarchical ACLs for every raw/admin/lifecycle write, field masking, policy-composition snapshots, or a security-audited zero-leakage guarantee.

### 07.1.9. Gaps

Remaining work includes an authentication-bound principal resolver, mask/partial/quarantine decisions, raw CRUD/lifecycle write enforcement, policy-composition snapshots, durable access-decision logs, cross-process policy-cache invalidation, and exhaustive adversarial access tests.

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
| `EvaluateAccess` | `(AccessDecision, allowed)` from canonical scope, principal, contracts, and watermark |
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

Inputs include requester agent/roles, tenant/workspace/team/session, canonical access, mutation/read watermark, PolicyRecord, ShareContract, and time. The read path returns `AccessDecision{object_id, principal_id, visibility, reason, share_contract_id, mutation_lsn}`. Lifecycle helpers still return booleans, values, or mutations, so no global decision union covers allow/deny/partial/mask/quarantine.

Reflection can mark Memory inactive and quarantined or decayed, override confidence, reduce salience, and archive to Cold. Query Assembler annotates quarantine and retraction; direct share and canonical writes are not all centrally authorized.

### 07.2.6. Call relationships

Policy coordinator wraps the engine and store. Runtime calls `EvaluateAccess` before query hydration, rechecks graph endpoints after Evidence assembly, and calls `IsShareContractAllowed` for shared derivation. Subscribers invoke reflection; Evidence Assembler reads PolicyStore; Gateway handles canonical/admin controls. Query reads and sharing have explicit gates, while direct CRUD/lifecycle paths still use parallel hooks.

### 07.2.7. Correctness/security

- PolicyRecord append-only but "latest" selection sometimes uses PolicyEventID lexical comparison.
- ShareContract accepts typed agent/role lists plus legacy ACL tokens; it is not a general condition-expression language.
- Read/write/derive share one evaluator, while merge/quarantine and every direct mutation are not yet uniform.
- Evidence filtering validates an Edge and both endpoints. Denied decisions are omitted from the response to avoid existence disclosure.
- Internal routes are not covered by admin key automatically.
- response visibility and object authorization are different mechanisms.

### 07.2.8. Claim Boundaries

Supported claim: canonical read decisions, watermark fencing, policy-safe evidence traversal, contract-backed share authorization, policy/share/audit schemas, and reflection helpers.

Do not claim authenticated IAM, deny-by-default behavior on every entry point, policy-composition/version snapshots, a partial/mask response engine, or a security-certified zero-leakage guarantee.

### 07.2.9. Gaps

Bind principals to verified transport identities; extend `AccessDecision` with deny/mask/partial/quarantine semantics without existence disclosure; apply mandatory write authorization to raw CRUD, lifecycle, and conflict operations; add policy precedence/versioning, durable decision logging, cache invalidation, and exhaustive access-matrix/security tests.

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
