param([ValidateRange(1,65535)][int]$Port = 8026, [switch]$NoBuild, [switch]$Background)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $projectRoot
$binary = Join-Path $projectRoot 'bin\wubu.exe'
$processRecord = Join-Path $projectRoot '.runtime\app.json'
if (Test-Path -LiteralPath $processRecord) {
    $record = Get-Content -LiteralPath $processRecord -Raw | ConvertFrom-Json
    $running = Get-Process -Id $record.pid -ErrorAction SilentlyContinue
    if ($running -and ($running.Path -eq $binary -or $running.Path -eq ($binary + '~'))) {
        Write-Host "Wubu is running: http://127.0.0.1:$($record.port)"
        return
    }
}
if (-not $NoBuild -or -not (Test-Path -LiteralPath $binary)) {
    $localGo = Join-Path $projectRoot '.tools\go\bin\go.exe'
    $goCommand = if (Test-Path -LiteralPath $localGo) { $localGo } else { (Get-Command go -ErrorAction Stop).Source }
    if ($goCommand -eq $localGo) {
        $env:GOCACHE = Join-Path $projectRoot '.tools\build-cache'
        $env:GOMODCACHE = Join-Path $projectRoot '.tools\mod-cache'
    }
    New-Item -ItemType Directory -Force (Split-Path $binary -Parent) | Out-Null
    & $goCommand build -trimpath -ldflags '-s -w -X main.version=0.2.0' -o $binary ./cmd/stockroom
    if ($LASTEXITCODE -ne 0) { throw 'Wubu build failed' }
}
$env:LISTEN_ADDR = "127.0.0.1:$Port"
$env:DATABASE_PATH = Join-Path $projectRoot 'data\wubu.db'
Write-Host "物簿 v0.1.1: http://127.0.0.1:$Port (Ctrl+C 停止)"
if ($Background) {
    New-Item -ItemType Directory -Force (Split-Path $processRecord -Parent) | Out-Null
    $process = Start-Process -FilePath $binary -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $projectRoot '.runtime\app.out.log') -RedirectStandardError (Join-Path $projectRoot '.runtime\app.err.log')
    @{pid=$process.Id;port=$Port} | ConvertTo-Json | Set-Content -LiteralPath $processRecord
    Write-Host 'Background service started; use scripts/stop.ps1 to stop.'
    return
}
& $binary
exit $LASTEXITCODE
