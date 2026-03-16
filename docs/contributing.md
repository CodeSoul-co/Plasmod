# Contributing Guide

## 1. Purpose

This document defines collaboration rules for the ANDB v1 repository.

ANDB is not a loose collection of experiments. It is a framework-first prototype with shared contracts across architecture, schemas, runtime flow, and benchmarks. Contributors should optimize for repository coherence, not only local module progress.

## 2. Core Collaboration Principles

### Principle 1: Framework first

Shared architecture, schemas, and main-flow contracts come before large module specialization.

### Principle 2: End-to-end loop first

The most important target is the integrated v1 path:

`event -> object -> retrieval -> graph -> evidence -> response`

### Principle 3: Schema stability first

Shared schemas are integration contracts. Casual changes create breakage across ingest, retrieval, SDKs, and tests.

### Principle 4: Scope discipline first

Do not silently turn the v1 sprint into a v2 or production-scale platform effort.

## 3. Repository Responsibility Map

Use the existing structure instead of inventing new top-level directories.

- `docs/`: architecture, schema, API, benchmark, and milestone docs
- `src/`: primary runtime and server code
- `sdk/python/`: Python SDK and scripts integration
- `sdk/nodejs/`: Node.js SDK placeholder
- `cpp/`: C++ retrieval path and future bindings
- `scripts/`: operational scripts and demos
- `tests/`: automated tests
- `configs/`: runtime and experiment configuration

If a new top-level directory is needed, it should be discussed first.

## 4. Branching Rules

The repository instructions in this environment require branch names prefixed with `codex/` when new branches are created. Use descriptive names under that prefix.

Recommended examples:

- `codex/feature-ingest-api`
- `codex/feature-retrieval-hybrid`
- `codex/docs-main-flow`
- `codex/fix-query-contract`

Keep one branch focused on one logical piece of work.

## 5. Commit Message Guidelines

Recommended styles:

- `feat: add event envelope validation`
- `feat: implement query response scaffold`
- `fix: correct object version mutation link`
- `docs: expand canonical object contract`
- `test: add ingest gateway coverage`

Avoid vague messages such as:

- `update`
- `misc changes`
- `fix stuff`

## 6. Pull Request Expectations

Every non-trivial change should explain:

1. what changed
2. why it changed
3. whether shared contracts were affected
4. whether tests were added or updated
5. whether documentation was updated

Recommended PR checklist:

- code builds or runs locally
- tests pass or are updated
- docs are synchronized with the change
- no unrelated files are mixed in
- v1 scope is still respected

## 7. Documentation Rules

Documentation is mandatory for architecture-significant changes.

You must update docs when:

- canonical object fields change
- query request or response shape changes
- ingest or query API behavior changes
- module boundaries change
- benchmark assumptions change
- v1 scope assumptions change

Key docs to keep aligned:

- [`README.md`](../README.md)
- [`docs/architecture/overview.md`](architecture/overview.md)
- [`docs/architecture/main-flow.md`](architecture/main-flow.md)
- [`docs/schema/canonical-objects.md`](schema/canonical-objects.md)
- [`docs/schema/query-schema.md`](schema/query-schema.md)
- [`docs/v1-scope.md`](v1-scope.md)

## 8. Shared Contract Change Rules

The following files are effectively protected shared contracts:

- [`src/internal/schemas/canonical.go`](../src/internal/schemas/canonical.go)
- [`src/internal/schemas/query.go`](../src/internal/schemas/query.go)
- [`docs/schema/canonical-objects.md`](schema/canonical-objects.md)
- [`docs/schema/query-schema.md`](schema/query-schema.md)
- [`docs/architecture/main-flow.md`](architecture/main-flow.md)

If you change one of these, you should:

1. explain the motivation
2. explain compatibility impact
3. update code and docs together
4. call out integration implications in review

No silent contract drift.

## 9. API Change Rules

Changes to `/v1/ingest/events` or `/v1/query` should be treated as contract changes, not only implementation changes.

This includes:

- required request fields
- response field types
- proof trace structure
- error handling shape
- validation behavior

Once SDKs and scripts depend on a shape, breaking it casually is expensive.

## 10. Module Ownership

The repository already hints at architectural ownership through module boundaries:

- access/API
- coordinator/control plane
- event backbone
- worker runtime
- data plane
- semantic layer
- SDKs
- experiments/tests

Ownership means accountability, not exclusivity. Others can contribute, but cross-module work should preserve the main flow and shared contracts.

## 11. Testing Rules

### 11.1 Required Test Levels

The repository should progressively include:

- unit tests
- integration tests
- end-to-end tests

### 11.2 Minimum Merge Expectation

Before merging, confirm at least:

- the modified code runs locally
- the affected tests pass or were updated
- integration assumptions still hold

### 11.3 Current Test and Run Commands

- `make dev`
- `make build`
- `make test`
- `pytest -q`
- `go test ./src/...`

If your change affects the Python SDK path, also verify the relevant scripts or tests.

## 12. Scope Control Rules

Features that usually belong outside v1:

- full logical clock engine
- full governance runtime
- conflict/merge engine
- distributed worker orchestration
- heavy optimization unrelated to the validation loop

If a feature does not directly support the v1 validation path, question whether it belongs in the sprint.

## 13. Communication Rules

The following must be communicated clearly in review or team discussion:

- schema changes
- integration blockers
- dependencies on other modules
- scope risks
- benchmark invalidation

The following should not happen:

- hidden large refactors
- undocumented contract changes
- parallel incompatible schema forks

## 14. Review Criteria

Review should focus on:

- correctness
- architectural consistency
- contract stability
- clarity of implementation
- documentation completeness
- respect for v1 scope

## 15. Coding Expectations

The repository values:

- clarity over cleverness
- explicit contracts over implicit coupling
- stable interfaces over improvisation
- integration-friendly code over isolated optimizations

Prefer readable module boundaries and obvious data flow.

## 16. Definition of Done

A task is done only when:

1. code is updated
2. relevant tests exist or are updated
3. documentation is updated if needed
4. integration impact is considered
5. the result still fits the v1 scope

## 17. Summary

The best contributions are the ones that strengthen the shared system path rather than only one isolated module.

Please contribute in a way that helps ANDB converge into one coherent prototype.
