# Member A 验收方案 A：Docker 只跑 MinIO + 本机编译运行 ANDB（disk + S3 冷层）
# 用法（在任意目录）:
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts/e2e/run_acceptance_scenario_a.ps1
# 要求：本机 Go 1.24+、Docker 可用、8080/8090/9000 未被占用（本脚本用 8090）

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $RepoRoot

Write-Host "[acceptance-a] repo: $RepoRoot"

Write-Host "[acceptance-a] starting MinIO + minio-init..."
docker compose up -d minio minio-init

$exeName = if ($env:OS -match "Windows") { "acceptance-andb-server.exe" } else { "acceptance-andb-server" }
$exePath = Join-Path $RepoRoot $exeName

Write-Host "[acceptance-a] go build -> $exeName"
go build -o $exePath ./src/cmd/server

Write-Host "[acceptance-a] deep1B.ibin vector fixture (no HTTP server)..."
go test ./integration_tests/dataset/... -count=1 -v

$dataDir = Join-Path $RepoRoot ".acceptance-e2e-data"
if (-not (Test-Path $dataDir)) {
  New-Item -ItemType Directory -Path $dataDir | Out-Null
}

# 子进程继承当前会话环境变量
$env:ANDB_HTTP_ADDR = "127.0.0.1:8090"
$env:ANDB_STORAGE = "disk"
$env:ANDB_DATA_DIR = $dataDir
$env:S3_ENDPOINT = "127.0.0.1:9000"
$env:S3_ACCESS_KEY = "minioadmin"
$env:S3_SECRET_KEY = "minioadmin"
$env:S3_BUCKET = "andb-integration"
$env:S3_SECURE = "false"
$env:S3_REGION = "us-east-1"
$env:S3_PREFIX = "andb/acceptance"

Get-Process -Name "acceptance-andb-server" -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host "[acceptance-a] starting ANDB on http://127.0.0.1:8090 ..."
Start-Process -FilePath $exePath -WorkingDirectory $RepoRoot -WindowStyle Hidden

$deadline = (Get-Date).AddMinutes(2)
$ok = $false
while ((Get-Date) -lt $deadline) {
  try {
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:8090/healthz" -UseBasicParsing -TimeoutSec 2
    if ($r.StatusCode -eq 200) { $ok = $true; break }
  } catch {}
  Start-Sleep -Seconds 1
}
if (-not $ok) {
  Write-Error "[acceptance-a] healthz timeout on :8090"
  exit 1
}
Write-Host "[acceptance-a] healthz OK"

$env:ANDB_BASE_URL = "http://127.0.0.1:8090"
Write-Host "[acceptance-a] go test ./integration_tests/..."
go test ./integration_tests/... -count=1 -timeout 120s -v

$env:ANDB_RUN_S3_TESTS = "true"
$env:S3_ENDPOINT = "127.0.0.1:9000"
$env:S3_ACCESS_KEY = "minioadmin"
$env:S3_SECRET_KEY = "minioadmin"
$env:S3_BUCKET = "andb-integration"
$env:S3_SECURE = "false"
Write-Host "[acceptance-a] TestS3Dataflow..."
go test ./integration_tests/... -run TestS3Dataflow -v -count=1

$outDir = Join-Path $RepoRoot "out/member_a_fullstack_verify"
Write-Host "[acceptance-a] member_a_capture -> $outDir"
python scripts/e2e/member_a_capture.py --out-dir $outDir

Write-Host "[acceptance-a] done. Stop server with:"
Write-Host "  Get-Process -Name acceptance-andb-server -ErrorAction SilentlyContinue | Stop-Process -Force"
Write-Host "Stop MinIO: docker compose down"
