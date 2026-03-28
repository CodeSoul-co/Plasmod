package storage

import "testing"

func TestLoadFromEnv_S3KeysTakePrecedence(t *testing.T) {
	t.Setenv("S3_ENDPOINT", "127.0.0.1:9000")
	t.Setenv("S3_ACCESS_KEY", "s3_key")
	t.Setenv("S3_SECRET_KEY", "s3_secret")
	t.Setenv("S3_BUCKET", "s3_bucket")
	t.Setenv("S3_PREFIX", "s3/prefix")

	t.Setenv("MINIO_ADDRESS", "minio:9000")
	t.Setenv("MINIO_ACCESS_KEY_ID", "minio_key")
	t.Setenv("MINIO_SECRET_ACCESS_KEY", "minio_secret")
	t.Setenv("MINIO_BUCKET_NAME", "minio_bucket")
	t.Setenv("MINIO_ROOT_PATH", "minio/prefix")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.Endpoint != "127.0.0.1:9000" || cfg.AccessKey != "s3_key" || cfg.SecretKey != "s3_secret" || cfg.Bucket != "s3_bucket" || cfg.Prefix != "s3/prefix" {
		t.Fatalf("expected S3_* keys to win, got cfg=%+v", cfg)
	}
}

func TestLoadFromEnv_FallbackToMinioAliases(t *testing.T) {
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_ACCESS_KEY", "")
	t.Setenv("S3_SECRET_KEY", "")
	t.Setenv("S3_BUCKET", "")
	t.Setenv("S3_PREFIX", "")
	t.Setenv("S3_SECURE", "")

	t.Setenv("MINIO_ADDRESS", "127.0.0.1:9000")
	t.Setenv("MINIO_ACCESS_KEY_ID", "minio_key")
	t.Setenv("MINIO_SECRET_ACCESS_KEY", "minio_secret")
	t.Setenv("MINIO_BUCKET_NAME", "minio_bucket")
	t.Setenv("MINIO_ROOT_PATH", "andb/minio")
	t.Setenv("MINIO_REGION", "us-east-1")
	t.Setenv("MINIO_USE_SSL", "1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed with MINIO_* aliases: %v", err)
	}
	if cfg.Endpoint != "127.0.0.1:9000" || cfg.AccessKey != "minio_key" || cfg.SecretKey != "minio_secret" || cfg.Bucket != "minio_bucket" {
		t.Fatalf("unexpected cfg from MINIO_* aliases: %+v", cfg)
	}
	if cfg.Prefix != "andb/minio" {
		t.Fatalf("expected prefix from MINIO_ROOT_PATH, got %q", cfg.Prefix)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("expected region from MINIO_REGION, got %q", cfg.Region)
	}
	if !cfg.Secure {
		t.Fatalf("expected secure=true from MINIO_USE_SSL")
	}
}
