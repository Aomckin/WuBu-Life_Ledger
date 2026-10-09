param([Parameter(Mandatory)][string]$ZhaoxiSource, [Parameter(Mandatory)][string]$GoCommand)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$testDirectory = Join-Path $temporaryRoot ('wubu-dogfooding-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testDirectory | Out-Null
$binary = Join-Path $testDirectory 'wubu-test.exe'
$variables = @('DATABASE_PATH','LISTEN_ADDR','PYTHONPATH','PYTHONIOENCODING','WUBU_TEST_BASE_URL')
$previous = @{}
foreach ($name in $variables) { $previous[$name] = [Environment]::GetEnvironmentVariable($name) }
$verificationProcess = $null
try {
    & $GoCommand build -trimpath -ldflags '-s -w -X main.version=0.2.0' -o $binary ./cmd/stockroom
    if ($LASTEXITCODE -ne 0) { throw 'Integration build failed' }
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = $listener.LocalEndpoint.Port
    $listener.Stop()
    $env:DATABASE_PATH = Join-Path $testDirectory 'validation.db'
    $env:LISTEN_ADDR = "127.0.0.1:$port"
    $env:WUBU_TEST_BASE_URL = "http://127.0.0.1:$port"
    $env:PYTHONPATH = "$(Join-Path $projectRoot 'tools\wubu_tool');$ZhaoxiSource"
    $env:PYTHONIOENCODING = 'utf-8'
    $verificationProcess = Start-Process -FilePath $binary -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $testDirectory 'stdout.log') -RedirectStandardError (Join-Path $testDirectory 'stderr.log')
    $ready = $false
    for ($attempt=0; $attempt -lt 30; $attempt++) {
        try { $null = Invoke-RestMethod "$($env:WUBU_TEST_BASE_URL)/healthz" -TimeoutSec 1; $ready=$true; break }
        catch { Start-Sleep -Milliseconds 100 }
    }
    if (-not $ready) { Get-Content (Join-Path $testDirectory 'stderr.log'); throw 'Integration service unavailable' }
    python -B tools/wubu_tool/tests/integration_purchase.py
    if ($LASTEXITCODE -ne 0) { throw 'SDK integration failed' }
    python -B tools/wubu_tool/tests/integration_v02.py
    if ($LASTEXITCODE -ne 0) { throw 'SDK v0.2 integration failed' }
} finally {
    if ($verificationProcess -and -not $verificationProcess.HasExited) { Stop-Process -InputObject $verificationProcess }
    foreach ($name in $variables) { [Environment]::SetEnvironmentVariable($name, $previous[$name]) }
    # Recursive cleanup stays inside the OS temp directory and this test's UUID.
    $resolvedTest = [IO.Path]::GetFullPath($testDirectory)
    if (-not $resolvedTest.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolvedTest -Leaf) -notlike 'wubu-dogfooding-*') { throw 'Refused unsafe test cleanup path' }
    Remove-Item -LiteralPath $resolvedTest -Recurse -Force
}
