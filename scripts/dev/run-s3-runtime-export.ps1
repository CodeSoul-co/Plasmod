param(
  [string]$BaseUrl = "http://127.0.0.1:8080",
  [string]$S3Endpoint = "127.0.0.1:9000",
  [string]$S3AccessKey = "minioadmin",
  [string]$S3SecretKey = "minioadmin",
  [string]$S3Bucket = "andb-integration",
  [string]$S3Secure = "false",
  [string]$S3Region = "us-east-1",
  [string]$S3Prefix = "andb/integration_tests"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "../..")

# artifacts output folder
$runTs = Get-Date -Format "yyyyMMdd_HHmmss"
$artifactsDir = Join-Path $repoRoot "scripts/dev/artifacts/s3-runtime-export/$runTs"
New-Item -ItemType Directory -Force -Path $artifactsDir | Out-Null

# Prepare S3 env vars BEFORE starting the Go server,
# because the handler reads S3_* from the server process environment at request time.
$env:S3_ENDPOINT = $S3Endpoint
$env:S3_ACCESS_KEY = $S3AccessKey
$env:S3_SECRET_KEY = $S3SecretKey
$env:S3_BUCKET = $S3Bucket
$env:S3_SECURE = $S3Secure
$env:S3_REGION = $S3Region
$env:S3_PREFIX = $S3Prefix

# 1) Start MinIO (Docker via ensure-docker.ps1).
$startMinio = Join-Path $repoRoot "scripts/dev/start-minio.ps1"
if (!(Test-Path $startMinio)) {
  throw "start-minio.ps1 not found: $startMinio"
}

Write-Host "[run-s3-runtime-export] Starting MinIO..."
& $startMinio

# 2) Restart server to ensure it inherits the S3_* env vars.
try {
  $existing = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($existing -and $existing.OwningProcess) {
    Write-Host "[run-s3-runtime-export] Stopping existing server on port 8080 (PID=$($existing.OwningProcess))..."
    Stop-Process -Id $existing.OwningProcess -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
  }
} catch {
  # Non-fatal: port probe might fail on some systems.
}

Write-Host "[run-s3-runtime-export] Starting go server..."
$outLog = Join-Path $artifactsDir "server.out.log"
$errLog = Join-Path $artifactsDir "server.err.log"
if (Test-Path $outLog) { Remove-Item -Force $outLog }
if (Test-Path $errLog) { Remove-Item -Force $errLog }

Start-Process -FilePath "go" -ArgumentList @("run", "./src/cmd/server") -WorkingDirectory $repoRoot -NoNewWindow `
  -RedirectStandardOutput $outLog -RedirectStandardError $errLog | Out-Null

$deadline = (Get-Date).AddSeconds(60)
$ready = $false
while ((Get-Date) -lt $deadline) {
  try {
    $resp = Invoke-WebRequest -Method Get -Uri ($BaseUrl.TrimEnd("/") + "/healthz") -TimeoutSec 2 -UseBasicParsing
    if ($resp.StatusCode -eq 200) {
      $ready = $true
      break
    }
  } catch {
    # keep waiting
  }
  Start-Sleep -Milliseconds 400
}

if (-not $ready) {
  throw "server not ready after waiting; check $outLog / $errLog"
}
Write-Host "[run-s3-runtime-export] Server is ready."

# 3) Call dev endpoint that writes to S3 at runtime.
Write-Host "[run-s3-runtime-export] Calling /v1/admin/s3/export ..."
$resp = Invoke-RestMethod -Method Post `
  -Uri ($BaseUrl.TrimEnd("/") + "/v1/admin/s3/export") `
  -ContentType "application/json" `
  -Body "{}" `
  -UseBasicParsing

Write-Host "[run-s3-runtime-export] Result:"
$respJson = ($resp | ConvertTo-Json -Depth 20)
$resp | ConvertTo-Json -Depth 20

# 4) Write record.md for traceability.
$secretRedacted = if ([string]::IsNullOrEmpty($S3SecretKey)) { "" } else { ($S3SecretKey.Substring(0, [Math]::Min(2, $S3SecretKey.Length)) + "***") }

# Build markdown code fences without embedding PowerShell backticks directly.
$bt = [char]96 # '`'
$triple = $bt + $bt + $bt
$tripleJson = $triple + "json"

$recordMd = @"
## s3 runtime export record

- run_at_utc: $((Get-Date).ToUniversalTime().ToString("o"))
- base_url: $BaseUrl

### S3 config (from script parameters)
- S3_ENDPOINT: $S3Endpoint
- S3_ACCESS_KEY: $S3AccessKey
- S3_SECRET_KEY: $secretRedacted
- S3_BUCKET: $S3Bucket
- S3_SECURE: $S3Secure
- S3_REGION: $S3Region
- S3_PREFIX: $S3Prefix

### API call
- method: POST
- url: $($BaseUrl.TrimEnd('/') + '/v1/admin/s3/export')

### Response
$tripleJson
$respJson
$triple

### Artifacts
- server.out.log: $outLog
- server.err.log: $errLog
"@

Set-Content -Path (Join-Path $artifactsDir "record.md") -Value $recordMd -Encoding UTF8
Write-Host "[run-s3-runtime-export] record.md saved to: $artifactsDir"

