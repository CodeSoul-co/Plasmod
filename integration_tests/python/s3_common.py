"""S3/MinIO helpers for Python SDK integration tests.

Requires:
    pip install minio==7.2.16

Environment variables:
    S3_ENDPOINT   e.g. 127.0.0.1:9000
    S3_ACCESS_KEY e.g. minioadmin
    S3_SECRET_KEY e.g. minioadmin
    S3_BUCKET     e.g. andb-integration
    S3_SECURE     false (default)
    S3_REGION     us-east-1 (default)
    S3_PREFIX     andb/integration_tests (default)
"""
import os


def load_s3_config() -> dict:
    endpoint = os.getenv("S3_ENDPOINT", "")
    access_key = os.getenv("S3_ACCESS_KEY", "")
    secret_key = os.getenv("S3_SECRET_KEY", "")
    bucket = os.getenv("S3_BUCKET", "")
    if not all([endpoint, access_key, secret_key, bucket]):
        raise RuntimeError(
            "S3 config incomplete: set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET"
        )
    return {
        "endpoint": endpoint,
        "access_key": access_key,
        "secret_key": secret_key,
        "bucket": bucket,
        "secure": os.getenv("S3_SECURE", "false").lower() in ("true", "1", "yes"),
        "region": os.getenv("S3_REGION", "us-east-1"),
        "prefix": os.getenv("S3_PREFIX", "andb/integration_tests").rstrip("/"),
    }


def make_minio_client(cfg: dict):
    from minio import Minio
    return Minio(
        cfg["endpoint"],
        access_key=cfg["access_key"],
        secret_key=cfg["secret_key"],
        secure=cfg["secure"],
        region=cfg["region"],
    )


def ensure_bucket(client, bucket: str) -> None:
    if not client.bucket_exists(bucket):
        client.make_bucket(bucket)
        print(f"  [S3] created bucket: {bucket}")
    else:
        print(f"  [S3] bucket already exists: {bucket}")
