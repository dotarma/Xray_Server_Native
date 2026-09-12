[CmdletBinding()]
param(
    [ValidatePattern('^v\d+\.\d+\.\d+$')]
    [string]$XuiVersion = 'v3.7.0',
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$projectRoot = Split-Path -Parent $PSScriptRoot
$moduleRoot = Join-Path $projectRoot 'module'
$binDir = Join-Path $projectRoot 'module\bin'
$userAgent = @{ 'User-Agent' = 'android-mini-server-native-builder' }

function Get-ReleaseAsset {
    param(
        [Parameter(Mandatory)] [string]$Repository,
        [Parameter(Mandatory)] [string]$Tag,
        [Parameter(Mandatory)] [string]$Name
    )

    $releaseUrl = "https://api.github.com/repos/$Repository/releases/tags/$Tag"
    $release = Invoke-RestMethod -Uri $releaseUrl -Headers $userAgent
    $asset = @($release.assets | Where-Object { $_.name -eq $Name })[0]
    if (-not $asset) {
        throw "Release $Repository $Tag does not contain asset $Name."
    }
    return $asset
}

function Get-LatestReleaseAsset {
    param(
        [Parameter(Mandatory)] [string]$Repository,
        [Parameter(Mandatory)] [string]$Name
    )

    $releaseUrl = "https://api.github.com/repos/$Repository/releases/latest"
    $release = Invoke-RestMethod -Uri $releaseUrl -Headers $userAgent
    $asset = @($release.assets | Where-Object { $_.name -eq $Name })[0]
    if (-not $asset) {
        throw "Latest release for $Repository does not contain asset $Name."
    }
    return $asset
}

function Assert-Sha256 {
    param(
        [Parameter(Mandatory)] [string]$Path,
        [Parameter(Mandatory)] [string]$Expected,
        [Parameter(Mandatory)] [string]$Label
    )

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    if ($actual -ne $Expected.ToLowerInvariant()) {
        throw "${Label} checksum mismatch. Expected $Expected, got $actual."
    }
    Write-Host "Verified SHA-256 for ${Label}: $actual"
}

function Save-Asset {
    param(
        [Parameter(Mandatory)] $Asset,
        [Parameter(Mandatory)] [string]$Destination
    )

    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if ($curl) {
        & $curl.Source --fail --location --silent --show-error --retry 3 --retry-all-errors --connect-timeout 20 --speed-time 60 --speed-limit 1024 --output $Destination -- $Asset.browser_download_url
        if ($LASTEXITCODE -ne 0) {
            throw "Download failed for $($Asset.name) with curl exit code $LASTEXITCODE."
        }
        return
    }

    Invoke-WebRequest -Uri $Asset.browser_download_url -OutFile $Destination -Headers $userAgent
}

function Get-AssetDigest {
    param(
        [Parameter(Mandatory)] $Asset,
        [Parameter(Mandatory)] [string]$Label
    )

    if ([string]::IsNullOrWhiteSpace($Asset.digest) -or $Asset.digest -notmatch '^sha256:([a-fA-F0-9]{64})$') {
        throw "$Label did not provide a SHA-256 digest in the official GitHub release metadata."
    }
    return $Matches[1]
}

function Assert-TargetAvailable {
    param([Parameter(Mandatory)] [string]$Path)

    if ((Test-Path -LiteralPath $Path) -and -not $Force) {
        throw "Refusing to overwrite $Path. Pass -Force only after verifying the existing binary."
    }
}

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

$requiredTargets = @(
    (Join-Path $moduleRoot 'x-ui'),
    (Join-Path $binDir 'xray-linux-arm64'),
    (Join-Path $binDir 'geoip.dat'),
    (Join-Path $binDir 'geosite.dat'),
    (Join-Path $binDir 'cloudflared'),
    (Join-Path $moduleRoot 'cacert.pem')
)
$requiredTargets | ForEach-Object { Assert-TargetAvailable $_ }

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("android-mini-server-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null

try {
    $xuiArchiveName = 'x-ui-linux-arm64.tar.gz'
    $xuiArchive = Join-Path $workDir $xuiArchiveName

    $xuiAsset = Get-ReleaseAsset -Repository 'MHSanaei/3x-ui' -Tag $XuiVersion -Name $xuiArchiveName
    Save-Asset -Asset $xuiAsset -Destination $xuiArchive
    Assert-Sha256 -Path $xuiArchive -Expected (Get-AssetDigest -Asset $xuiAsset -Label $xuiArchiveName) -Label $xuiArchiveName

    $xuiExtractDir = Join-Path $workDir 'x-ui-extract'
    New-Item -ItemType Directory -Path $xuiExtractDir | Out-Null
    tar -xzf $xuiArchive -C $xuiExtractDir

    $upstreamXuiDir = Join-Path $xuiExtractDir 'x-ui'
    $upstreamFiles = @{
        'xray-linux-arm64' = (Join-Path $upstreamXuiDir 'bin\xray-linux-arm64')
        'geoip.dat' = (Join-Path $upstreamXuiDir 'bin\geoip.dat')
        'geosite.dat' = (Join-Path $upstreamXuiDir 'bin\geosite.dat')
    }
    foreach ($entry in $upstreamFiles.GetEnumerator()) {
        if (-not (Test-Path -LiteralPath $entry.Value -PathType Leaf)) {
            throw "Expected upstream file is missing: $($entry.Value)"
        }
        Copy-Item -LiteralPath $entry.Value -Destination (Join-Path $binDir $entry.Key) -Force
    }

    $upstreamXui = Join-Path $upstreamXuiDir 'x-ui'
    if (-not (Test-Path -LiteralPath $upstreamXui -PathType Leaf)) {
        throw "Expected upstream file is missing: $upstreamXui"
    }
    & (Join-Path $PSScriptRoot 'patch-xui-android-path.ps1') -Source $upstreamXui -Destination (Join-Path $moduleRoot 'x-ui')
    $obsoleteXui = Join-Path $binDir 'x-ui'
    if (Test-Path -LiteralPath $obsoleteXui -PathType Leaf) {
        Remove-Item -LiteralPath $obsoleteXui -Force
    }

    $cloudflaredAsset = Get-LatestReleaseAsset -Repository 'cloudflare/cloudflared' -Name 'cloudflared-linux-arm64'
    $upstreamCloudflared = Join-Path $workDir 'cloudflared-linux-arm64'
    Save-Asset -Asset $cloudflaredAsset -Destination $upstreamCloudflared

    Assert-Sha256 -Path $upstreamCloudflared -Expected (Get-AssetDigest -Asset $cloudflaredAsset -Label 'cloudflared-linux-arm64') -Label 'cloudflared-linux-arm64'
    & (Join-Path $PSScriptRoot 'patch-cloudflared-android-dns.ps1') -Source $upstreamCloudflared -Destination (Join-Path $binDir 'cloudflared')

    Invoke-WebRequest -Uri 'https://curl.se/ca/cacert.pem' -OutFile (Join-Path $moduleRoot 'cacert.pem')

    Write-Host ''
    Write-Host "Prepared the patched x-ui in $moduleRoot and verified upstream data in $binDir"
    Write-Host 'Run tools\package-module.ps1 next.'
}
finally {
    $resolvedTemp = [System.IO.Path]::GetFullPath($workDir)
    $resolvedRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    if ($resolvedTemp.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedTemp)) {
        Remove-Item -LiteralPath $resolvedTemp -Recurse -Force
    }
}
