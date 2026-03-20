param(
  [int]$TimeoutMinutes = 30,
  [switch]$ForceWingetInstall = $false
)

function Test-DockerAvailable {
  try {
    $null = docker version --format '{{.Server.Version}}' 2>$null
    return $true
  } catch {
    return $false
  }
}

function Get-CommandOrNull {
  param([string]$Name)
  try {
    return (Get-Command $Name -ErrorAction Stop).Source
  } catch {
    return $null
  }
}

Write-Host "[ensure-docker] Checking Docker availability..."
if (Test-DockerAvailable) {
  Write-Host "[ensure-docker] Docker is already available."
  return
}

$winget = Get-CommandOrNull -Name "winget"
$choco = Get-CommandOrNull -Name "choco"

Write-Host "[ensure-docker] Docker not found. winget=$winget choco=$choco"

#
# If Docker Desktop is already installed but the engine isn't running yet,
# try to start the GUI app first (best-effort).
#
$dockerDesktopExeCandidates = @(
  "$env:ProgramFiles\\Docker\\Docker\\Docker Desktop.exe",
  "${env:ProgramFiles(x86)}\\Docker\\Docker\\Docker Desktop.exe"
)
$dockerDesktopExe = $dockerDesktopExeCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if ($dockerDesktopExe) {
  Write-Host "[ensure-docker] Found Docker Desktop executable: $dockerDesktopExe"
  try {
    Start-Process -FilePath $dockerDesktopExe | Out-Null
  } catch {
    Write-Host "[ensure-docker] Failed to start Docker Desktop automatically: $($_.Exception.Message)"
  }

  $deadline = (Get-Date).AddMinutes($TimeoutMinutes)
  while ((Get-Date) -lt $deadline) {
    if (Test-DockerAvailable) {
      Write-Host "[ensure-docker] Docker is now available."
      return
    }
    Start-Sleep -Seconds 5
  }
}

if ($winget -ne $null -or $ForceWingetInstall) {
  Write-Host "[ensure-docker] Attempting Docker Desktop install via winget..."
  # Notes:
  # - Installing Docker Desktop typically requires admin rights and may open a UI.
  # - winget silent behavior can vary by environment. We still proceed and then wait for 'docker version'.
  $args = @(
    "install",
    "--id", "Docker.DockerDesktop",
    "-e",
    "--silent",
    "--accept-package-agreements",
    "--accept-source-agreements"
  )

  try {
    Start-Process -FilePath "winget" -ArgumentList $args -Wait -NoNewWindow
  } catch {
    Write-Host "[ensure-docker] winget install command failed: $($_.Exception.Message)"
  }
} elseif ($choco -ne $null) {
  Write-Host "[ensure-docker] Attempting Docker Desktop install via Chocolatey..."
  try {
    Start-Process -FilePath "choco" -ArgumentList @("install", "docker-desktop", "-y") -Wait -NoNewWindow
  } catch {
    Write-Host "[ensure-docker] choco install command failed: $($_.Exception.Message)"
  }
} else {
  Write-Host "[ensure-docker] Neither winget nor choco is available."
  Write-Host "[ensure-docker] Please install Docker Desktop manually, then re-run this script."
  exit 1
}

Write-Host "[ensure-docker] Waiting for Docker engine to become available..."
$deadline = (Get-Date).AddMinutes($TimeoutMinutes)
while ((Get-Date) -lt $deadline) {
  if (Test-DockerAvailable) {
    Write-Host "[ensure-docker] Docker is now available."
    return
  }
  Start-Sleep -Seconds 5
}

Write-Host "[ensure-docker] Timed out waiting for Docker."
Write-Host "[ensure-docker] If Docker Desktop was installed, please start Docker Desktop manually and re-run."
exit 2

