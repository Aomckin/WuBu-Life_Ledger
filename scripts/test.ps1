param([string]$ZhaoxiSource = '')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $projectRoot
$localGo = Join-Path $projectRoot '.tools\go\bin\go.exe'
$goCommand = if (Test-Path -LiteralPath $localGo) { $localGo } else { (Get-Command go -ErrorAction Stop).Source }
if ($goCommand -eq $localGo) {
    $env:GOCACHE = Join-Path $projectRoot '.tools\build-cache'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools\mod-cache'
}
& $goCommand test ./...
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
& $goCommand vet ./...
if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
if (Get-Command python -ErrorAction SilentlyContinue) {
    $env:PYTHONPATH = Join-Path $projectRoot 'tools\wubu_tool'
    if ($ZhaoxiSource) { $env:PYTHONPATH += ";$ZhaoxiSource" }
    $pattern = if ($ZhaoxiSource) { 'test_*.py' } else { 'test_client.py' }
    # -B prevents any bytecode writes to read-only Zhaoxi sources.
    python -B -m unittest discover -s tools\wubu_tool\tests -p $pattern -v
    if ($LASTEXITCODE -ne 0) { throw 'Tool package tests failed' }
    if ($ZhaoxiSource) { & (Join-Path $PSScriptRoot 'test-sdk-integration.ps1') -ZhaoxiSource $ZhaoxiSource -GoCommand $goCommand }
}
if (Get-Command node -ErrorAction SilentlyContinue) {
    Get-ChildItem -LiteralPath internal\web\static\assets -Filter '*.js' | ForEach-Object {
        node --check $_.FullName
        if ($LASTEXITCODE -ne 0) { throw 'JavaScript syntax check failed' }
    }
    node scripts\test-frontend.cjs
    if ($LASTEXITCODE -ne 0) { throw 'Frontend checks failed' }
}
