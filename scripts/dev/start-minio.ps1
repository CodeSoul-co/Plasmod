param(
  [string]$MinioRootUser = "minioadmin",
  [string]$MinioRootPassword = "minioadmin",
  [string]$ContainerName = "minio",
  [int]$ApiPort = 9000,
  [int]$ConsolePort = 9001,
  [string]$MinioDataDir = "$PSScriptRoot/../../.minio-data"
)

function DotSourceIfExists {
  param([string]$Path)
  if (Test-Path $Path) {
    . $Path
    return $true
  }
  return $false
}

if (!(Test-Path $MinioDataDir)) {
  New-Item -ItemType Directory -Path $MinioDataDir -Force | Out-Null
}

$repoRoot = Resolve-Path "$PSScriptRoot/../.."
$ensureDockerPath = Join-Path $repoRoot "scripts/dev/ensure-docker.ps1"
if (!(Test-Path $ensureDockerPath)) {
  throw "ensure-docker.ps1 not found at: $ensureDockerPath"
}

# Call ensure-docker.ps1 as a script (do not dot-source).
& $ensureDockerPath

try {
  $null = docker version --format '{{.Server.Version}}' 2>$null
} catch {
  throw "Docker engine not available after ensure-docker. Please start Docker Desktop manually and re-run."
}

Write-Host "[start-minio] Ensuring no stale container named '$ContainerName'..."
$existing = docker ps -a --format '{{.Names}}' | Where-Object { $_ -eq $ContainerName }
if ($existing) {
  docker rm -f $ContainerName | Out-Null
}

Write-Host "[start-minio] Starting MinIO..."
$hostPath = Resolve-Path $MinioDataDir
docker run -d --name $ContainerName -p ($ApiPort.ToString() + ":9000") -p ($ConsolePort.ToString() + ":9001") -e "MINIO_ROOT_USER=$MinioRootUser" -e "MINIO_ROOT_PASSWORD=$MinioRootPassword" -v "${hostPath}:/data" quay.io/minio/minio server /data --address ":9000" | Out-Null

Write-Host "[start-minio] MinIO started."
Write-Host "[start-minio] API:      http://127.0.0.1:$ApiPort"
Write-Host "[start-minio] Console:  http://127.0.0.1:$ConsolePort"

