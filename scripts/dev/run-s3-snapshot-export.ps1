param(
  [string]$BaseUrl = "http://127.0.0.1:8080",
  [string]$S3Endpoint = "127.0.0.1:9000",
  [string]$S3AccessKey = "minioadmin",
  [string]$S3SecretKey = "minioadmin",
  [string]$S3Bucket = "plasmod-integration",
  [string]$S3Secure = "false",
  [string]$S3Region = "us-east-1",
  [string]$S3Prefix = "plasmod/integration_tests"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "../..")

$runTs = Get-Date -Format "yyyyMMdd_HHmmss"
$artifactsDir = Join-Path $repoRoot "scripts/dev/artifacts/s3-snapshot-export/$runTs"
New-Item -ItemType Directory -Force -Path $artifactsDir | Out-Null

if ($S3Secure -ne "true" -and $S3Secure -ne "false" -and $S3Secure -ne "1" -and $S3Secure -ne "0") {
  throw "S3Secure must be true/false (or 1/0)"
}

$env:S3_ENDPOINT = $S3Endpoint
$env:S3_ACCESS_KEY = $S3AccessKey
$env:S3_SECRET_KEY = $S3SecretKey
$env:S3_BUCKET = $S3Bucket
$env:S3_SECURE = $S3Secure
$env:S3_REGION = $S3Region
$env:S3_PREFIX = $S3Prefix

# 1) Start MinIO
$startMinio = Join-Path $repoRoot "scripts/dev/start-minio.ps1"
Write-Host "[run-s3-snapshot-export] Starting MinIO..."
& $startMinio

# 2) Restart server (non-extended build is sufficient for dev snapshot-export).
try {
  $c = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($c -ne $null -and $c.OwningProcess -ne $null) {
    Write-Host "[run-s3-snapshot-export] Stopping existing server on port 8080 (PID=$($c.OwningProcess))..."
    Stop-Process -Id $c.OwningProcess -Force -ErrorAction SilentlyContinue | Out-Null
  }
} catch {
  # ignore
}

$outLog = Join-Path $artifactsDir "server.out.log"
$errLog = Join-Path $artifactsDir "server.err.log"

if (Test-Path $outLog) { Remove-Item -Force $outLog }
if (Test-Path $errLog) { Remove-Item -Force $errLog }

Write-Host "[run-s3-snapshot-export] Starting go server..."
Start-Process -FilePath "go" -ArgumentList @("run", "./src/cmd/server") `
  -WorkingDirectory $repoRoot -NoNewWindow `
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
Write-Host "[run-s3-snapshot-export] Server is ready."

# 3) Call snapshot-export endpoint (writes snapshot manifests to S3 and reads back).
Write-Host "[run-s3-snapshot-export] Calling /v1/admin/s3/snapshot-export ..."
$resp = Invoke-RestMethod -Method Post `
  -Uri ($BaseUrl.TrimEnd("/") + "/v1/admin/s3/snapshot-export") `
  -ContentType "application/json" `
  -Body "{}" `
  -UseBasicParsing

Write-Host "[run-s3-snapshot-export] Result:"
$respJson = ($resp | ConvertTo-Json -Depth 20)
$resp | ConvertTo-Json -Depth 20

$bt = [char]96
$triple = $bt + $bt + $bt
$tripleJson = $triple + "json"

$secretRedacted = $S3SecretKey.Substring(0, [Math]::Min(2, $S3SecretKey.Length)) + "***"
$recordMd = @"
## s3 snapshot-export record

- run_at_utc: $((Get-Date).ToUniversalTime().ToString("o"))
- base_url: $BaseUrl
- endpoint: $($BaseUrl.TrimEnd('/') + '/v1/admin/s3/snapshot-export')

### S3 config
- S3_ENDPOINT: $S3Endpoint
- S3_ACCESS_KEY: $S3AccessKey
- S3_SECRET_KEY: $secretRedacted
- S3_BUCKET: $S3Bucket
- S3_SECURE: $S3Secure
- S3_REGION: $S3Region
- S3_PREFIX: $S3Prefix

### Response
$tripleJson
$respJson
$triple

### Artifacts
- server.out.log: $outLog
- server.err.log: $errLog
"@

Set-Content -Path (Join-Path $artifactsDir "record.md") -Value $recordMd -Encoding UTF8
Write-Host "[run-s3-snapshot-export] record.md saved to: $artifactsDir"

