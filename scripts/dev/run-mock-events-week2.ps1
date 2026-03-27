# Posts each event from mock-events-week2.json to a running ANDB server.
# Usage: .\run-mock-events-week2.ps1 [-BaseUrl http://127.0.0.1:8080]

param(
    [string] $BaseUrl = "http://127.0.0.1:8080"
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$jsonPath = Join-Path $here "mock-events-week2.json"
$raw = Get-Content -LiteralPath $jsonPath -Raw -Encoding UTF8
$events = $raw | ConvertFrom-Json

foreach ($ev in $events) {
    $body = $ev | ConvertTo-Json -Depth 20 -Compress
    Write-Host "POST $($ev.event_id) ..."
    $r = Invoke-RestMethod -Uri "$BaseUrl/v1/ingest/events" -Method Post -Body $body -ContentType "application/json; charset=utf-8"
    $r | ConvertTo-Json -Depth 10
}
