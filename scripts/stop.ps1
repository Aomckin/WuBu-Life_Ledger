$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$processRecord = Join-Path $projectRoot '.runtime\app.json'
if (-not (Test-Path -LiteralPath $processRecord)) { Write-Host 'No managed Wubu background service.'; return }
$record = Get-Content -LiteralPath $processRecord -Raw | ConvertFrom-Json
$running = Get-Process -Id $record.pid -ErrorAction SilentlyContinue
$binary = Join-Path $projectRoot 'bin\wubu.exe'
if ($running) {
    if ($running.Path -ne $binary -and $running.Path -ne ($binary + '~')) { throw 'PID belongs to a different program; refused to stop.' }
    Stop-Process -Id $running.Id
}
Remove-Item -LiteralPath $processRecord
Write-Host 'Wubu stopped; database preserved.'
