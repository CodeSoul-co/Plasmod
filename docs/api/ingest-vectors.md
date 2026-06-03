# Warm vector ingest API

## Endpoint

- Method: `POST`
- Path: `/v1/ingest/vectors`
- Content-Type: `application/json`

Builds (or rebuilds) a **warm segment** ANN index from caller-supplied vectors. This bypasses event materialization and embedder; vectors are written directly to the retrieval plane.

## Request

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vectors` | `[][]float32` | yes | One row per vector; all rows must share the same dimension |
| `segment_id` | string | no | Defaults to `warm.default` |
| `object_ids` | `[]string` | no | Same length as `vectors`; auto-generated if omitted |
| `index_type` | string | no | `HNSW` (default), `IVF_FLAT`, `IVF_PQ`, `IVF_SQ8`, `DISKANN` |
| `ivf_nlist` | int | no | IVF coarse cells (0 = default 128) |
| `ivf_nprobe` | int | no | IVF cells visited per query (0 = default 32) |
| `ivf_m` | int | no | IVF_PQ sub-vectors (0 = default 16) |
| `ivf_nbits` | int | no | IVF_PQ bits per sub-vector (0 = default 8) |
| `ivf_sq_type` | string | no | IVF_SQ8: `INT8` or `FP32` |

IVF index types require a build with FAISS/Knowhere enabled (`make cpp` / `-tags retrieval`).

## Example: HNSW (default)

```json
{
  "segment_id": "demo.warm.batch",
  "object_ids": ["doc_a", "doc_b"],
  "vectors": [
    [1.0, 0.0, 0.0, 0.0],
    [0.0, 1.0, 0.0, 0.0]
  ]
}
```

## Example: IVF_FLAT

```json
{
  "segment_id": "demo.ivf",
  "index_type": "IVF_FLAT",
  "ivf_nlist": 128,
  "ivf_nprobe": 32,
  "vectors": [[1,0,0,0], [0,1,0,0], [0,0,1,0]]
}
```

## Success response

```json
{
  "status": "ok",
  "segment_id": "demo.ivf",
  "ingested": 3,
  "vector_dim": 4,
  "index_type": "IVF_FLAT",
  "direct_warm": true
}
```

## Binary alternative

High-throughput clients may use `POST /v1/internal/rpc/ingest_batch` (wire version 3) with the same `index_type` and IVF fields in the binary framing (see `src/internal/transport/framing.go`).
