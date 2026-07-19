# 00. Plasmod Core Library Engineering Documentation

> Language: [中文](../README.md) | English

This documentation describes the public behavior, implementation boundaries, code structure, build process, deployment model, and maintenance procedures of the Plasmod core library. Its sources of truth are the current Go and C++ implementations, schemas, configuration loaders, build definitions, and tests. Planned contracts and implementations that are not connected to the active composition root are labeled explicitly and are never presented as completed capabilities.

## 00.1. Reading Order

| Order | Document | Primary question |
|---|---|---|
| 01 | [Project Overview, Requirements, and System Boundaries](01-project-overview-and-requirements.md) | Why does Plasmod exist, and what does it own? |
| 02 | [System Architecture and Design Principles](02-system-architecture-and-design.md) | Which layers and modules form the active system? |
| 03 | [Canonical Object Model and Memory Lifecycle](03-canonical-data-model-and-memory-lifecycle.md) | How do events become canonical objects, and how does memory evolve? |
| 04 | [Runtime, Four Execution Chains, and Scheduling](04-runtime-chains-and-scheduling.md) | How do requests move through the runtime and its execution chains? |
| 05 | [Consistency, Recovery, and Correctness Model](05-consistency-recovery-and-correctness.md) | What are the visibility stages, failure windows, and recovery guarantees? |
| 06 | [Retrieval, Query, and Evidence Assembly](06-retrieval-query-and-evidence.md) | How are candidates retrieved, hydrated, and assembled into evidence? |
| 07 | [Scope, Governance, Collaboration, and Security](07-governance-collaboration-and-security.md) | How are scope, sharing, policy, and security represented and enforced? |
| 08 | [API, Schema, Configuration, and SDK Reference](08-api-schema-and-sdk-reference.md) | Which HTTP, gRPC, transport, schema, configuration, and SDK contracts exist? |
| 09 | [Installation, Startup, and User Guide](09-getting-started-and-user-guide.md) | How do users install, start, verify, and operate Plasmod? |
| 10 | [Codebase, Interface Implementations, and Call Paths](10-codebase-interfaces-and-call-paths.md) | Where are interfaces, implementations, fields, and function-level call paths? |
| 11 | [Dependencies, Build, Test, and Development Workflow](11-dependencies-build-and-development.md) | How is the native and Go stack built, tested, and changed? |
| 12 | [Deployment, Operations, Recovery, and Troubleshooting](12-deployment-operations-and-troubleshooting.md) | How is Plasmod deployed, monitored, recovered, and diagnosed? |
| 13 | [Extensibility, Compatibility, and System Evolution](13-extensibility-compatibility-and-evolution.md) | How can the system be extended without breaking contracts? |
| 14 | [Implementation Status, Gaps, and Claim Boundaries](14-implementation-status-gaps-and-claim-boundaries.md) | Which capabilities are implemented, partial, unwired, or planned? |
| 15 | [Architecture Decision Records](15-architecture-decision-records.md) | Which architectural decisions govern the implementation? |

## 00.2. Role-based Reading Paths

| Reader | Recommended path |
|---|---|
| First-time reader | 01 -> 02 -> 03 -> 04 |
| API or SDK user | 01 -> 08 -> 09 |
| Core developer | 02 -> 03 -> 04 -> 10 -> 11 -> 14 |
| Deployment or operations engineer | 09 -> 12 -> 05 |
| Architecture reviewer | 02 -> 04 -> 05 -> 06 -> 07 -> 14 -> 15 |

## 00.3. Status Labels

| Label | Definition |
|---|---|
| Implemented | Reachable from the active bootstrap or main call path and supported by code or tests. |
| Partial | An implementation exists, but coverage, persistence, isolation, API surface, or recovery semantics are incomplete. |
| Experimental | Core code exists, but its API, configuration, or compatibility contract is not stable. |
| Defined-not-wired | Types and implementations exist, but the active composition root does not construct or invoke them. |
| Contract-only | An interface or schema exists without an active production implementation in the core library. |
| Planned | A target design that is not implemented in the current codebase. |

## 00.4. Source-of-truth Priority

1. `src/cmd/server/main.go` and `src/internal/app/bootstrap.go` for the real bootstrap path.
2. `src/internal/access/gateway.go`, `gateway_rpc.go`, and the gRPC server for externally reachable interfaces.
3. `src/internal/worker/runtime.go`, the consistency controller, and chain implementations for actual execution order.
4. `src/internal/schemas/`, `src/internal/storage/contracts.go`, and event-backbone contracts for data and persistence semantics.
5. `Makefile`, Compose definitions, `.env.example`, configuration loaders, and their tests for build and deployment behavior.

When historical comments or design names conflict with executable paths, this documentation follows the code and tests. Differences and overclaim risks are recorded in Chapter 14.
