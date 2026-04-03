#!/usr/bin/env python3
"""
Import vector/groundtruth datasets into ANDB via /v1/ingest/events.

Usage examples:
  python3 scripts/e2e/import_dataset.py --file /path/to/ABC.fvecs --dataset ABC
  python3 scripts/e2e/import_dataset.py --file /path/to/datasets_dir --dataset ABC
  python3 scripts/e2e/import_dataset.py --file /path/to/datasets_dir --dataset ABC --limit 200
  python3 scripts/e2e/import_dataset.py --delete --file /path/to/base.10M.fbin --dataset deep1B --workspace-id deep1B_w
  python3 scripts/e2e/import_dataset.py --delete --dataset deep1B --workspace-id deep1B_w
  python3 scripts/e2e/import_dataset.py --delete --delete-dry-run --file /path/to/dir --dataset deep1B --workspace-id deep1B_w
  python3 scripts/e2e/import_dataset.py --purge --dataset deep1B --workspace-id deep1B_w
  python3 scripts/e2e/import_dataset.py --purge --purge-dry-run --file /path/to/base.fbin --dataset deep1B --workspace-id deep1B_w

Required:
  --dataset  Dataset name tag written into payload
  --file     File or directory path (required for ingest; optional for --delete / --purge — omit to act by dataset_name + workspace only)

Optional fields are auto-generated if omitted.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import struct
import sys
import threading
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from pathlib import Path
from typing import Iterable, Iterator
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

try:
    import pyarrow as pa
    import pyarrow.ipc as pa_ipc
except Exception:
    pa = None
    pa_ipc = None


SUPPORTED_EXTS = {".fvecs", ".ivecs", ".ibin", ".fbin", ".arrow"}


def _now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _http_post_json(
    base_url: str, path: str, body: dict, timeout: float = 30.0
) -> tuple[int, dict]:
    url = base_url.rstrip("/") + path
    req = Request(url, data=json.dumps(body).encode("utf-8"), method="POST")
    req.add_header("Content-Type", "application/json")
    try:
        with urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw.decode("utf-8")) if raw else {}
    except HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {e.code} POST {url}: {raw}") from e
    except URLError as e:
        raise RuntimeError(f"POST {url} failed: {e}") from e


def _ingest_event_post(base_url: str, body: dict, timeout: float) -> None:
    status, ack = _http_post_json(base_url, "/v1/ingest/events", body, timeout=timeout)
    if status != 200:
        raise RuntimeError(f"unexpected status={status} ack={ack}")


def _iter_fvecs(path: Path, limit: int) -> Iterator[tuple[int, int, list[float]]]:
    with path.open("rb") as f:
        i = 0
        while True:
            h = f.read(4)
            if not h:
                return
            if len(h) != 4:
                raise RuntimeError(f"{path}: malformed fvecs header at row {i}")
            dim = struct.unpack("<i", h)[0]
            if dim <= 0 or dim > 100000:
                raise RuntimeError(f"{path}: unexpected dim={dim} at row {i}")
            b = f.read(4 * dim)
            if len(b) != 4 * dim:
                raise RuntimeError(f"{path}: truncated vector row {i}")
            vals = list(struct.unpack("<" + "f" * dim, b))
            yield i, dim, vals
            i += 1
            if limit > 0 and i >= limit:
                return


def _iter_ivecs(path: Path, limit: int) -> Iterator[tuple[int, int, list[int]]]:
    with path.open("rb") as f:
        i = 0
        while True:
            h = f.read(4)
            if not h:
                return
            if len(h) != 4:
                raise RuntimeError(f"{path}: malformed ivecs header at row {i}")
            dim = struct.unpack("<i", h)[0]
            if dim <= 0 or dim > 100000:
                raise RuntimeError(f"{path}: unexpected dim={dim} at row {i}")
            b = f.read(4 * dim)
            if len(b) != 4 * dim:
                raise RuntimeError(f"{path}: truncated vector row {i}")
            vals = list(struct.unpack("<" + "i" * dim, b))
            yield i, dim, vals
            i += 1
            if limit > 0 and i >= limit:
                return


def _iter_ibin(path: Path, limit: int, ibin_dtype: str) -> Iterator[tuple[int, int, list[float] | list[int], str]]:
    with path.open("rb") as f:
        header = f.read(8)
        if len(header) != 8:
            raise RuntimeError(f"{path}: malformed ibin header")
        n, dim = struct.unpack("<II", header)
        if dim == 0 or dim > 100000:
            raise RuntimeError(f"{path}: unexpected dim={dim}")

        # Dtype selection:
        # - explicit override via --ibin-dtype
        # - fallback heuristic in auto mode
        lower = path.name.lower()
        if ibin_dtype == "int32":
            as_int = True
        elif ibin_dtype == "float32":
            as_int = False
        else:
            # Heuristic: groundtruth-like files usually contain integer IDs.
            as_int = ("groundtruth" in lower) or lower.startswith("gt.") or ".gt" in lower
        dtype = "int32" if as_int else "float32"

        rows = min(n, limit) if limit > 0 else n
        for i in range(rows):
            b = f.read(4 * dim)
            if len(b) != 4 * dim:
                raise RuntimeError(f"{path}: truncated data at row {i}")
            if as_int:
                vals = list(struct.unpack("<" + "i" * dim, b))
            else:
                vals = list(struct.unpack("<" + "f" * dim, b))
            yield i, dim, vals, dtype


def _iter_fbin(path: Path, limit: int) -> Iterator[tuple[int, int, list[float], str]]:
    with path.open("rb") as f:
        header = f.read(8)
        if len(header) != 8:
            raise RuntimeError(f"{path}: malformed fbin header")
        n, dim = struct.unpack("<II", header)
        if dim == 0 or dim > 100000:
            raise RuntimeError(f"{path}: unexpected dim={dim}")

        rows = min(n, limit) if limit > 0 else n
        for i in range(rows):
            b = f.read(4 * dim)
            if len(b) != 4 * dim:
                raise RuntimeError(f"{path}: truncated data at row {i}")
            vals = list(struct.unpack("<" + "f" * dim, b))
            yield i, dim, vals, "float32"


def _preview(vals: Iterable, k: int) -> str:
    out = []
    for i, v in enumerate(vals):
        if i >= k:
            break
        out.append(str(v))
    return " ".join(out)


def _is_vector_like(v: object) -> bool:
    if not isinstance(v, (list, tuple)) or len(v) == 0:
        return False
    sample = v[0]
    return isinstance(sample, (int, float))


def _iter_arrow_rows(path: Path, limit: int) -> Iterator[tuple[int, int, list[float] | list[int], str, str]]:
    if pa is None or pa_ipc is None:
        raise RuntimeError("pyarrow is required for .arrow files. Please install: pip install pyarrow")

    reader = pa_ipc.open_file(path)
    cols = reader.schema.names

    # Heuristic column selection (works for varying schemas):
    # 1) prefer canonical names
    # 2) fallback to first vector-like field in sampled rows
    preferred_vec = ("embedding", "vector", "values", "feature", "features")
    preferred_txt = ("text", "content", "title", "name", "caption")
    preferred_id = ("id", "row_id", "uid", "pk")

    vec_col = next((c for c in preferred_vec if c in cols), None)
    txt_col = next((c for c in preferred_txt if c in cols), None)
    id_col = next((c for c in preferred_id if c in cols), None)

    sampled_rows: list[dict] = []
    sample_budget = 50
    for bi in range(reader.num_record_batches):
        rb = reader.get_batch(bi)
        rows = rb.to_pylist()
        sampled_rows.extend(rows[: max(0, sample_budget - len(sampled_rows))])
        if len(sampled_rows) >= sample_budget:
            break

    if vec_col is None:
        for r in sampled_rows:
            for c in cols:
                if _is_vector_like(r.get(c)):
                    vec_col = c
                    break
            if vec_col is not None:
                break

    if vec_col is None:
        raise RuntimeError(f"{path}: no vector-like column detected in arrow file")

    emitted = 0
    row_idx = 0
    for bi in range(reader.num_record_batches):
        rb = reader.get_batch(bi)
        rows = rb.to_pylist()
        for r in rows:
            if limit > 0 and emitted >= limit:
                return
            vals_obj = r.get(vec_col)
            if not _is_vector_like(vals_obj):
                row_idx += 1
                continue
            vals = list(vals_obj)
            dim = len(vals)
            # Infer numeric dtype for metadata/text only.
            dtype = "float32" if isinstance(vals[0], float) else "int32"
            # Include best-effort extra text token for easier retrieval.
            extra = ""
            if txt_col and r.get(txt_col) is not None:
                extra = str(r.get(txt_col))
            elif id_col and r.get(id_col) is not None:
                extra = f"id:{r.get(id_col)}"
            yield row_idx, dim, vals, dtype, extra
            emitted += 1
            row_idx += 1


def _run_dataset_delete(
    base_url: str,
    workspace_id: str,
    dataset: str,
    dry_run: bool,
    file_name: str | None = None,
) -> dict:
    """POST /v1/admin/dataset/delete. If file_name is set, matches dataset=<file_name> AND dataset_name:...; if omitted, only dataset_name + workspace."""
    body: dict = {
        "workspace_id": workspace_id,
        "dataset_name": dataset,
        "dry_run": dry_run,
    }
    if file_name:
        body["file_name"] = file_name
    status, ack = _http_post_json(base_url, "/v1/admin/dataset/delete", body)
    if status != 200:
        raise RuntimeError(f"unexpected status={status} ack={ack}")
    return ack


def _run_dataset_purge(
    base_url: str,
    workspace_id: str,
    dataset: str,
    dry_run: bool,
    only_if_inactive: bool,
    file_name: str | None = None,
) -> dict:
    """POST /v1/admin/dataset/purge. Hard-remove inactive memories matching selectors (see server docs)."""
    body: dict = {
        "workspace_id": workspace_id,
        "dataset_name": dataset,
        "dry_run": dry_run,
        "only_if_inactive": only_if_inactive,
    }
    if file_name:
        body["file_name"] = file_name
    status, ack = _http_post_json(base_url, "/v1/admin/dataset/purge", body)
    if status != 200:
        raise RuntimeError(f"unexpected status={status} ack={ack}")
    return ack


def _collect_files(file_arg: str) -> list[Path]:
    p = Path(file_arg).expanduser().resolve()
    if not p.exists():
        raise RuntimeError(f"--file path does not exist: {p}")
    if p.is_file():
        if p.suffix.lower() not in SUPPORTED_EXTS:
            raise RuntimeError(f"unsupported file type: {p.suffix} ({p})")
        return [p]
    files = sorted([x for x in p.rglob("*") if x.is_file() and x.suffix.lower() in SUPPORTED_EXTS])
    if not files:
        raise RuntimeError(f"no supported files under directory: {p}")
    return files


def main() -> None:
    ap = argparse.ArgumentParser(description="Import dataset files to ANDB ingest API")
    ap.add_argument(
        "--file",
        default=None,
        help="Dataset file path or directory path (required for ingest; for --delete, omit to delete by --dataset + --workspace-id only)",
    )
    ap.add_argument("--dataset", required=True, help="Dataset name label, e.g. ABC")
    ap.add_argument("--base-url", default=os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080"))
    ap.add_argument("--tenant-id", default="t_demo")
    ap.add_argument("--workspace-id", default="w_demo")
    ap.add_argument("--agent-id", default="a_loader")
    ap.add_argument("--session-prefix", default="s")
    ap.add_argument("--event-type", default="dataset_record")
    ap.add_argument("--source", default="dataset_loader")
    ap.add_argument("--version", type=int, default=1)
    ap.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Max rows per file; omit for no cap; <=0 means all rows",
    )
    ap.add_argument("--preview-k", type=int, default=6, help="How many leading values in payload.text")
    ap.add_argument("--start-seq", type=int, default=0, help="Global event sequence start")
    ap.add_argument(
        "--concurrency",
        type=int,
        default=1,
        metavar="N",
        help="Ingest only: parallel HTTP requests (default 1 = one row at a time). Try 8–32 if server can handle load.",
    )
    ap.add_argument(
        "--http-timeout",
        type=float,
        default=30.0,
        metavar="SEC",
        help="Per-request timeout for ingest POST (default 30)",
    )
    ap.add_argument(
        "--ibin-dtype",
        choices=("auto", "float32", "int32"),
        default="auto",
        help="How to decode .ibin payload values (default: auto heuristic by filename)",
    )
    ap.add_argument(
        "--delete",
        action="store_true",
        help="Call /v1/admin/dataset/delete instead of ingest; sends file_name + dataset_name (same as --dataset)",
    )
    ap.add_argument(
        "--delete-dry-run",
        action="store_true",
        help="With --delete, preview only (dry_run=true)",
    )
    ap.add_argument(
        "--purge",
        action="store_true",
        help="Call /v1/admin/dataset/purge (hard delete after soft-delete); requires --workspace-id; same --file rules as --delete",
    )
    ap.add_argument(
        "--purge-dry-run",
        action="store_true",
        help="With --purge, preview only (dry_run=true)",
    )
    ap.add_argument(
        "--purge-include-active",
        action="store_true",
        help="With --purge, set only_if_inactive=false (also purge active matching memories; dangerous)",
    )
    args = ap.parse_args()

    if args.concurrency < 1:
        ap.error("--concurrency must be >= 1")

    if args.delete and args.purge:
        ap.error("cannot use --delete and --purge together")

    if not args.delete and not args.purge and not args.file:
        ap.error("the following arguments are required: --file (for ingest)")

    if args.purge:
        mode = "dry-run" if args.purge_dry_run else "purge"
        only_if_inactive = not args.purge_include_active
        if not args.file:
            print(
                f"[purge:{mode}] by dataset_name only dataset={args.dataset!r} "
                f"workspace_id={args.workspace_id!r} only_if_inactive={only_if_inactive} base={args.base_url}"
            )
            ack = _run_dataset_purge(
                args.base_url,
                args.workspace_id,
                args.dataset,
                args.purge_dry_run,
                only_if_inactive,
                file_name=None,
            )
            print(
                f"  matched={int(ack.get('matched', 0))} skipped_active={int(ack.get('skipped_active', 0))} "
                f"purgeable={int(ack.get('purgeable', 0))} purged={int(ack.get('purged', 0))}"
            )
            print(f"[purge:{mode}] done")
            return

        files = _collect_files(args.file)
        print(
            f"[purge:{mode}] files={len(files)} dataset_name={args.dataset!r} "
            f"workspace_id={args.workspace_id!r} only_if_inactive={only_if_inactive} base={args.base_url}"
        )
        total_matched = 0
        total_skipped = 0
        total_purgeable = 0
        total_purged = 0
        for path in files:
            ack = _run_dataset_purge(
                args.base_url,
                args.workspace_id,
                args.dataset,
                args.purge_dry_run,
                only_if_inactive,
                file_name=path.name,
            )
            matched = int(ack.get("matched", 0))
            skipped = int(ack.get("skipped_active", 0))
            purgeable = int(ack.get("purgeable", 0))
            purged = int(ack.get("purged", 0))
            total_matched += matched
            total_skipped += skipped
            total_purgeable += purgeable
            total_purged += purged
            print(
                f"  [file] {path.name} matched={matched} skipped_active={skipped} "
                f"purgeable={purgeable} purged={purged}"
            )
        print(
            f"[purge:{mode}] done total_matched={total_matched} total_skipped_active={total_skipped} "
            f"total_purgeable={total_purgeable} total_purged={total_purged}"
        )
        return

    if args.delete:
        mode = "dry-run" if args.delete_dry_run else "delete"
        if not args.file:
            print(
                f"[delete:{mode}] by dataset_name only dataset={args.dataset!r} "
                f"workspace_id={args.workspace_id!r} base={args.base_url}"
            )
            ack = _run_dataset_delete(
                args.base_url,
                args.workspace_id,
                args.dataset,
                args.delete_dry_run,
                file_name=None,
            )
            print(
                f"  matched={int(ack.get('matched', 0))} deleted={int(ack.get('deleted', 0))}"
            )
            print(f"[delete:{mode}] done")
            return

        files = _collect_files(args.file)
        print(
            f"[delete:{mode}] files={len(files)} dataset_name={args.dataset!r} "
            f"workspace_id={args.workspace_id!r} base={args.base_url}"
        )
        total_matched = 0
        total_deleted = 0
        for path in files:
            ack = _run_dataset_delete(
                args.base_url,
                args.workspace_id,
                args.dataset,
                args.delete_dry_run,
                file_name=path.name,
            )
            matched = int(ack.get("matched", 0))
            deleted = int(ack.get("deleted", 0))
            total_matched += matched
            total_deleted += deleted
            print(f"  [file] {path.name} matched={matched} deleted={deleted}")
        print(f"[delete:{mode}] done total_matched={total_matched} total_deleted={total_deleted}")
        return

    files = _collect_files(args.file)

    # None / <=0 → no row cap (iterators treat limit>0 as the cap).
    row_limit = 0 if args.limit is None else args.limit

    seq = args.start_seq
    total = 0

    lim_disp = "none" if args.limit is None else args.limit
    print(
        f"[import] files={len(files)} dataset={args.dataset} base={args.base_url} "
        f"limit={lim_disp} concurrency={args.concurrency} http_timeout={args.http_timeout}s"
    )
    for path in files:
        ext = path.suffix.lower()
        session_id = f"{args.session_prefix}_{args.dataset}_{path.name}"
        count = 0

        if ext == ".fvecs":
            row_iter = ((i, dim, vals, "float32", "") for i, dim, vals in _iter_fvecs(path, row_limit))
        elif ext == ".ivecs":
            row_iter = ((i, dim, vals, "int32", "") for i, dim, vals in _iter_ivecs(path, row_limit))
        elif ext == ".ibin":
            row_iter = (
                (i, dim, vals, dtype, "")
                for i, dim, vals, dtype in _iter_ibin(path, row_limit, args.ibin_dtype)
            )
        elif ext == ".fbin":
            row_iter = ((i, dim, vals, dtype, "") for i, dim, vals, dtype in _iter_fbin(path, row_limit))
        elif ext == ".arrow":
            row_iter = _iter_arrow_rows(path, row_limit)
        else:
            # Should never happen due to _collect_files
            continue

        print(f"[file] {path} ({ext})")

        def _build_ingest_body(
            row_i: int,
            dim: int,
            vals: list,
            dtype: str,
            extra: str,
            seq_val: int,
        ) -> dict:
            ts = _now_iso()
            ev_id = f"evt_{args.dataset}_{path.stem}_{seq_val:08d}"
            txt = (
                f"dataset={path.name} dataset_name:{args.dataset} row:{row_i} "
                f"dim:{dim} dtype:{dtype} head:{_preview(vals, args.preview_k)}"
            )
            if extra:
                txt = txt + " extra:" + extra
            return {
                "event_id": ev_id,
                "tenant_id": args.tenant_id,
                "workspace_id": args.workspace_id,
                "agent_id": args.agent_id,
                "session_id": session_id,
                "event_type": args.event_type,
                "event_time": ts,
                "ingest_time": ts,
                "visible_time": ts,
                "payload": {
                    "text": txt,
                    "dataset": args.dataset,
                    "file_name": path.name,
                    "row_index": row_i,
                    "dim": dim,
                    "dtype": dtype,
                },
                "source": args.source,
                "version": args.version,
            }

        if args.concurrency <= 1:
            for i, dim, vals, dtype, extra in row_iter:
                body = _build_ingest_body(i, dim, vals, dtype, extra, seq)
                seq += 1
                _ingest_event_post(args.base_url, body, args.http_timeout)
                count += 1
                total += 1
                if count % 200 == 0:
                    print(f"  ingested {count} rows...")
        else:
            seq_lock = threading.Lock()

            def _next_seq() -> int:
                nonlocal seq
                with seq_lock:
                    s = seq
                    seq += 1
                    return s

            prog_lock = threading.Lock()

            def _on_done() -> None:
                nonlocal count, total
                with prog_lock:
                    count += 1
                    total += 1
                    if count % 200 == 0:
                        print(f"  ingested {count} rows...")

            pending = set()
            with ThreadPoolExecutor(max_workers=args.concurrency) as ex:
                for i, dim, vals, dtype, extra in row_iter:
                    body = _build_ingest_body(i, dim, vals, dtype, extra, _next_seq())
                    pending.add(
                        ex.submit(_ingest_event_post, args.base_url, body, args.http_timeout)
                    )
                    if len(pending) >= args.concurrency:
                        done, pending = wait(pending, return_when=FIRST_COMPLETED)
                        for fut in done:
                            fut.result()
                            _on_done()
                while pending:
                    done, pending = wait(pending, return_when=FIRST_COMPLETED)
                    for fut in done:
                        fut.result()
                        _on_done()

        print(f"  done rows={count} session_id={session_id}")

    print(f"[done] total_rows={total}")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"[error] {e}", file=sys.stderr)
        raise SystemExit(1)

