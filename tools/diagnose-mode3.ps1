[CmdletBinding()]
param(
    [switch]$Public,
    [ValidateSet('ws', 'xhttp')]
    [string]$Transport = 'ws'
)

$ErrorActionPreference = 'Stop'
$modulePath = '/data/adb/modules/android-mini-server-native'
$deploymentText = adb shell "su -mm -c 'cat $modulePath/deployment.json'" | Out-String
if ([string]::IsNullOrWhiteSpace($deploymentText)) {
    throw 'No saved Mode 3 deployment was found on the device.'
}
$deployment = $deploymentText | ConvertFrom-Json
if ($deployment.mode -ne 'mode3') {
    throw "Saved deployment is '$($deployment.mode)', not Mode 3."
}

$transport = if ($deployment.transport -eq 'dual') { $Transport } else { $deployment.transport }
$path = $deployment.path
$stream = @{
    network = $transport
    security = 'none'
    sockopt = @{ domainStrategy = 'UseIPv4' }
}
if ($transport -eq 'xhttp') {
    $stream.xhttpSettings = @{ path = $path; mode = $deployment.xhttpMode }
} else {
    $stream.wsSettings = @{ path = $path; host = $deployment.host }
}

$address = '127.0.0.1'
$port = if ($deployment.originPort) { [int]$deployment.originPort } else { 8080 }
if ($Public) {
    $address = $deployment.host
    $port = 443
    $stream.security = 'tls'
    $alpn = if ($transport -eq 'xhttp') { @('h3', 'h2') } else { @('http/1.1') }
    $stream.tlsSettings = @{ serverName = $deployment.host; fingerprint = 'chrome'; alpn = $alpn }
}

$client = @{
    log = @{ loglevel = 'debug' }
    dns = @{ servers = @('1.1.1.1', '8.8.8.8'); queryStrategy = 'UseIPv4' }
    inbounds = @(@{ listen = '127.0.0.1'; port = 18090; protocol = 'http'; settings = @{} })
    outbounds = @(
        @{
            protocol = 'vless'
            settings = @{ vnext = @(@{ address = $address; port = $port; users = @(@{ id = $deployment.uuid; encryption = 'none' }) }) }
            streamSettings = $stream
        },
        @{ tag = 'dns-direct'; protocol = 'freedom'; settings = @{} }
    )
}

$temp = Join-Path ([IO.Path]::GetTempPath()) 'xray-server-native-mode3-diagnostic.json'
try {
    [IO.File]::WriteAllText($temp, ($client | ConvertTo-Json -Depth 20), (New-Object Text.UTF8Encoding($false)))
    adb push $temp /data/local/tmp/xray-server-native-mode3-diagnostic.json
}
finally {
    if (Test-Path -LiteralPath $temp) { Remove-Item -LiteralPath $temp -Force }
}
