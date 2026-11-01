param([string]$PlaywrightModule = '', [string]$ScreenshotDirectory = '', [ValidatePattern('^[a-z0-9-]+$')][string]$ScreenshotPrefix = 'v02')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $projectRoot
$localGo = Join-Path $projectRoot '.tools\go\bin\go.exe'
$goCommand = if (Test-Path -LiteralPath $localGo) { $localGo } else { (Get-Command go -ErrorAction Stop).Source }
if ($goCommand -eq $localGo) {
    $env:GOCACHE = Join-Path $projectRoot '.tools\build-cache'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools\mod-cache'
}
$temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$testDirectory = Join-Path $temporaryRoot ('wubu-v02-ui-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testDirectory | Out-Null
$binary = Join-Path $testDirectory 'wubu-ui.exe'
$variables = @('DATABASE_PATH','LISTEN_ADDR','WUBU_TEST_BASE_URL','WUBU_UI_OUTPUT','WUBU_PLAYWRIGHT_MODULE')
$previous = @{}
foreach ($name in $variables) { $previous[$name] = [Environment]::GetEnvironmentVariable($name) }
$verificationProcess = $null
try {
    & $goCommand build -trimpath -o $binary ./cmd/stockroom
    if ($LASTEXITCODE -ne 0) { throw 'UI test build failed' }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = $listener.LocalEndpoint.Port
    $listener.Stop()
    $env:DATABASE_PATH = Join-Path $testDirectory 'validation.db'
    $env:LISTEN_ADDR = "127.0.0.1:$port"
    $env:WUBU_TEST_BASE_URL = "http://127.0.0.1:$port"
    $env:WUBU_UI_OUTPUT = $testDirectory
    if ($PlaywrightModule) { $env:WUBU_PLAYWRIGHT_MODULE = $PlaywrightModule }
    $verificationProcess = Start-Process -FilePath $binary -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $testDirectory 'stdout.log') -RedirectStandardError (Join-Path $testDirectory 'stderr.log')
    $ready = $false
    for ($attempt=0; $attempt -lt 30; $attempt++) {
        try { $null = Invoke-RestMethod "$($env:WUBU_TEST_BASE_URL)/healthz" -TimeoutSec 1; $ready=$true; break }
        catch { Start-Sleep -Milliseconds 100 }
    }
    if (-not $ready) { Get-Content (Join-Path $testDirectory 'stderr.log'); throw 'UI service unavailable' }
    node scripts/test-v02-ui.cjs
    if ($LASTEXITCODE -ne 0) { throw 'UI acceptance failed' }
    if ($ScreenshotDirectory) {
        New-Item -ItemType Directory -Path $ScreenshotDirectory -Force | Out-Null
        foreach ($name in @('overview','durables','mobile','accounts')) {
            Copy-Item -LiteralPath (Join-Path $testDirectory "$name.png") -Destination (Join-Path $ScreenshotDirectory "$ScreenshotPrefix-$name.png")
        }
    }
} finally {
    if ($verificationProcess -and -not $verificationProcess.HasExited) {
        Stop-Process -InputObject $verificationProcess
        $verificationProcess.WaitForExit()
    }
    foreach ($name in $variables) { [Environment]::SetEnvironmentVariable($name, $previous[$name]) }
    $resolvedTest = [IO.Path]::GetFullPath($testDirectory)
    if (-not $resolvedTest.StartsWith($temporaryRoot,[StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolvedTest -Leaf) -notlike 'wubu-v02-ui-*') { throw 'Refused unsafe UI test cleanup path' }
    Remove-Item -LiteralPath $resolvedTest -Recurse -Force
}
