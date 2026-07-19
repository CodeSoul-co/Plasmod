# Implementation Maturity

## Implemented

Registered in active composition root, has concrete backend/handler and tests for primary behavior.

## Experimental

Code is usable for controlled integration, but payload/name/lifecycle may change. Most `/v1/internal/*` runtime bridges
belong here。

## Partial

Only some backends/platforms/operations are complete. Example: gRPC parity, Node SDK, compile-dependent native indexes。

## Not Confirmed

No current code evidence for a reliable contract. Documentation must not infer the capability from directory names or
upstream snapshots。

## Deprecated

Still accepted for compatibility but new callers should not use. Examples include selected `ANDB_*` environment aliases and
legacy flat Event fields。

Status change requires code link, tests, migration impact and documentation review。
