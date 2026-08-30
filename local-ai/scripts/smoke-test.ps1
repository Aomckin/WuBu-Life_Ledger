[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:11434",
    [Parameter(Mandatory = $true)]
    [string]$Model
)

$base = $BaseUrl.TrimEnd("/")
$tags = Invoke-RestMethod -Method Get -Uri "$base/api/tags" -TimeoutSec 15
$available = @($tags.models | ForEach-Object { $_.name })
if ($available -notcontains $Model) {
    throw "Model '$Model' is not installed. Installed: $($available -join ', ')"
}

$body = @{
    model = $Model
    stream = $false
    format = "json"
    options = @{
        temperature = 0
        num_predict = 64
    }
    messages = @(
        @{
            role = "user"
            content = 'Return exactly this JSON object: {"answer":"ok"}'
        }
    )
} | ConvertTo-Json -Depth 6

$request = @{
    Method = "Post"
    Uri = "$base/api/chat"
    ContentType = "application/json"
    Body = $body
    TimeoutSec = 180
}
$response = Invoke-RestMethod @request

if ([string]::IsNullOrWhiteSpace($response.message.content)) {
    throw "Ollama returned no message content."
}

$result = $response.message.content | ConvertFrom-Json
if ($result.answer -ne "ok") {
    throw "Unexpected model response: $($response.message.content)"
}

[pscustomobject]@{
    endpoint = $base
    model = $Model
    response = $result.answer
    total_duration_ns = $response.total_duration
}
