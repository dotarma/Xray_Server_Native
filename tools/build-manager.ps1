[CmdletBinding()]
param(
    [string]$GoCommand = 'go'
)

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$sourceRoot = Join-Path $projectRoot 'manager'
$output = Join-Path $projectRoot 'module\manager\android-mini-server-manager'

if (-not (Test-Path -LiteralPath (Join-Path $sourceRoot 'main.go') -PathType Leaf)) {
    throw "Missing manager source at $sourceRoot"
}

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $output) | Out-Null
$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED
Push-Location $sourceRoot
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'arm64'
    $env:CGO_ENABLED = '0'
    # Windows/WSL filesystems can report stale source timestamps to Go. Build
    # without a package cache so a flashable ZIP always contains current code.
    & $GoCommand clean -cache
    & $GoCommand build -a -trimpath -ldflags='-s -w' -o $output .
    if ($LASTEXITCODE -ne 0) {
        throw 'Go build failed.'
    }
}
finally {
    Pop-Location
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
    $env:CGO_ENABLED = $previousCgo
}

Write-Host "Created $output"
