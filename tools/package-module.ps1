[CmdletBinding()]
param(
    [switch]$AllowNoBinaries,
    [string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$moduleRoot = Join-Path $projectRoot 'module'
$moduleProps = Join-Path $moduleRoot 'module.prop'

if (-not (Test-Path -LiteralPath $moduleProps -PathType Leaf)) {
    throw "Missing $moduleProps"
}

$properties = @{}
Get-Content -LiteralPath $moduleProps | ForEach-Object {
    if ($_ -match '^([^=]+)=(.*)$') {
        $properties[$Matches[1]] = $Matches[2]
    }
}

$requiredBinaries = @(
    (Join-Path $moduleRoot 'x-ui'),
    (Join-Path $moduleRoot 'bin\xray-linux-arm64'),
    (Join-Path $moduleRoot 'bin\geoip.dat'),
    (Join-Path $moduleRoot 'bin\geosite.dat'),
    (Join-Path $moduleRoot 'bin\cloudflared'),
    (Join-Path $moduleRoot 'manager\android-mini-server-manager')
)
$missingBinaries = @($requiredBinaries | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missingBinaries.Count -gt 0 -and -not $AllowNoBinaries) {
    throw "Missing required binaries: $($missingBinaries -join ', '). Run prepare-upstream-binaries.ps1 first, or use -AllowNoBinaries only for structure testing."
}

$version = $properties['version']
if ([string]::IsNullOrWhiteSpace($version)) {
    throw 'module.prop does not define version.'
}
$outputRoot = if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    Join-Path $projectRoot "artifacts\packages\xray-server-native\$version"
}
else {
    [System.IO.Path]::GetFullPath($OutputDirectory)
}

$stage = Join-Path $outputRoot ("stage-" + [guid]::NewGuid().ToString('N'))
$zipPath = Join-Path $outputRoot ("xray-server-native-$version.zip")
if (Test-Path -LiteralPath $zipPath) {
    throw "Package already exists: $zipPath. Pass -OutputDirectory with a new path to keep both packages."
}

New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
New-Item -ItemType Directory -Path $stage -Force | Out-Null
try {
    Get-ChildItem -LiteralPath $moduleRoot -Force | Copy-Item -Destination $stage -Recurse -Force
    @('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_SOURCES.md') | ForEach-Object {
        $complianceFile = Join-Path $projectRoot $_
        if (-not (Test-Path -LiteralPath $complianceFile -PathType Leaf)) {
            throw "Missing compliance file: $complianceFile"
        }
        Copy-Item -LiteralPath $complianceFile -Destination $stage -Force
    }
    $licensesDirectory = Join-Path $projectRoot 'LICENSES'
    if (-not (Test-Path -LiteralPath $licensesDirectory -PathType Container)) {
        throw "Missing compliance directory: $licensesDirectory"
    }
    Copy-Item -LiteralPath $licensesDirectory -Destination $stage -Recurse -Force
    $redundantXui = Join-Path $stage 'bin\x-ui'
    if (Test-Path -LiteralPath $redundantXui -PathType Leaf) {
        Remove-Item -LiteralPath $redundantXui -Force
    }

    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [System.IO.Compression.ZipFile]::Open($zipPath, [System.IO.Compression.ZipArchiveMode]::Create)
    try {
        Get-ChildItem -LiteralPath $stage -Recurse -File -Force | ForEach-Object {
            $entryName = $_.FullName.Substring($stage.Length).TrimStart('\', '/').Replace('\', '/')
            [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                $archive,
                $_.FullName,
                $entryName,
                [System.IO.Compression.CompressionLevel]::Optimal
            ) | Out-Null
        }
    }
    finally {
        $archive.Dispose()
    }
}
finally {
    $stageFull = [System.IO.Path]::GetFullPath($stage)
    $outputPrefix = [System.IO.Path]::GetFullPath($outputRoot).TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
    if (-not $stageFull.StartsWith($outputPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove staging directory outside output root: $stageFull"
    }
    if (Test-Path -LiteralPath $stageFull -PathType Container) {
        Remove-Item -LiteralPath $stageFull -Recurse -Force
    }
}

Write-Host "Created $zipPath"
if ($missingBinaries.Count -gt 0) {
    Write-Warning 'This ZIP is a structure-only test package and cannot start services.'
}
else {
    Write-Host 'Package contains the required upstream binaries. Validate on the target with control.sh check.'
}
