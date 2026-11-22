$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
Write-Host 'Plasmod demo: http://127.0.0.1:5180'
node server.mjs
