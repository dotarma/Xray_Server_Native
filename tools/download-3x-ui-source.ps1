[CmdletBinding()]
param(
    [string]$Version = 'v3.7.0',
    [string]$Destination
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$projectRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Destination)) {
    $Destination = Join-Path $projectRoot "third_party\3x-ui-$Version"
}

$destinationPath = [System.IO.Path]::GetFullPath($Destination)
if (Test-Path -LiteralPath $destinationPath) {
    throw "Destination already exists: $destinationPath"
}

$archive = Join-Path ([System.IO.Path]::GetTempPath()) ("3x-ui-$Version-" + [guid]::NewGuid().ToString('N') + '.zip')
$extractRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("3x-ui-source-" + [guid]::NewGuid().ToString('N'))
try {
    $uri = "https://github.com/MHSanaei/3x-ui/archive/refs/tags/$Version.zip"
    Invoke-WebRequest -Uri $uri -OutFile $archive
    Expand-Archive -LiteralPath $archive -DestinationPath $extractRoot
    $sourceDirectory = Get-ChildItem -LiteralPath $extractRoot -Directory | Select-Object -First 1
    if (-not $sourceDirectory) {
        throw "Archive did not contain a source directory: $uri"
    }
    New-Item -ItemType Directory -Path (Split-Path -Parent $destinationPath) -Force | Out-Null
    Move-Item -LiteralPath $sourceDirectory.FullName -Destination $destinationPath
    Write-Host "Downloaded 3x-ui $Version source to $destinationPath"
}
finally {
    if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
    if (Test-Path -LiteralPath $extractRoot) { Remove-Item -LiteralPath $extractRoot -Recurse -Force }
}
