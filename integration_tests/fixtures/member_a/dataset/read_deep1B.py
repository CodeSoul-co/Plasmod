#!/usr/bin/env python3
"""
Read deep1B.ibin: 8-byte little-endian header (uint32 n, uint32 dim) + n*dim float32 row-major.
"""

from __future__ import annotations

import argparse
import struct
from pathlib import Path
from typing import Iterator

_HEADER = struct.Struct("<II")


def read_ibin(path: Path | str) -> tuple[int, int, list[float]]:
    """
    Load an .ibin file. Returns (n, dim, flat_floats) where flat_floats has length n*dim,
    row-major: vector i is flat_floats[i * dim : (i + 1) * dim].
    """
    p = Path(path)
    raw = p.read_bytes()
    if len(raw) < _HEADER.size:
        raise ValueError(f"{p}: file too small for header")
    n, dim = _HEADER.unpack_from(raw, 0)
    if n == 0 or dim == 0:
        raise ValueError(f"{p}: invalid header n={n} dim={dim}")
    payload = len(raw) - _HEADER.size
    need = n * dim * 4
    if payload != need:
        raise ValueError(f"{p}: expected {need} payload bytes, got {payload}")
    flat = list(struct.unpack_from(f"<{n * dim}f", raw, _HEADER.size))
    return n, dim, flat


def iter_vectors(n: int, dim: int, flat: list[float]) -> Iterator[list[float]]:
    for i in range(n):
        off = i * dim
        yield flat[off : off + dim]


def main() -> None:
    here = Path(__file__).resolve().parent
    ap = argparse.ArgumentParser(description="Read deep1B.ibin (n, dim header + float32 rows)")
    ap.add_argument(
        "path",
        nargs="?",
        type=Path,
        default=here / "deep1B.ibin",
        help="Path to .ibin (default: deep1B.ibin next to this script)",
    )
    ap.add_argument("--summary", action="store_true", help="Print n, dim, file size only")
    args = ap.parse_args()
    path = args.path.resolve()
    n, dim, flat = read_ibin(path)
    if args.summary:
        print(f"path={path}")
        print(f"n={n} dim={dim} floats={len(flat)}")
        return
    print(f"path={path} n={n} dim={dim}")
    first = flat[:dim]
    print(f"first_vector[:5]={first[:5]}")


if __name__ == "__main__":
    main()
