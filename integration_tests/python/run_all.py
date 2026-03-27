"""Runner for Python SDK integration tests.

Usage:
    python integration_tests/python/run_all.py

S3 dataflow test (requires MinIO running):
    ANDB_RUN_S3_TESTS=true \\
    S3_ENDPOINT=127.0.0.1:9000 S3_ACCESS_KEY=minioadmin \\
    S3_SECRET_KEY=minioadmin S3_BUCKET=andb-integration \\
    python integration_tests/python/run_all.py
"""
import importlib
import os
import sys

# Ensure this directory is on sys.path so relative imports in submodules work.
_here = os.path.dirname(os.path.abspath(__file__))
if _here not in sys.path:
    sys.path.insert(0, _here)


def run_module(name: str) -> None:
    mod = importlib.import_module(name)
    mod.main()


def main() -> None:
    modules = [
        "test_sdk_ingest_query",
        "test_chain_main",
        "test_chain_query",
        "test_chain_collab",
    ]

    if os.getenv("ANDB_RUN_S3_TESTS", "").lower() in ("1", "true", "yes"):
        modules.append("test_s3_dataflow")

    failures: list[str] = []
    for m in modules:
        print(f"\n=== {m} ===")
        try:
            run_module(m)
            print(f"[PASS] {m}")
        except SystemExit as e:
            if e.code:
                failures.append(m)
        except Exception as exc:
            failures.append(f"{m}: {exc}")
            print(f"[FAIL] {m}: {exc}", file=sys.stderr)

    print("\n" + "=" * 50)
    if failures:
        print(f"FAILED: {failures}", file=sys.stderr)
        sys.exit(1)
    else:
        print(f"ALL PYTHON SDK TESTS PASSED ({len(modules)} modules)")


if __name__ == "__main__":
    main()
