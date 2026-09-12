[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string]$Source,
    [Parameter(Mandatory)] [string]$Destination
)

$ErrorActionPreference = 'Stop'

$sourcePath = [System.IO.Path]::GetFullPath($Source)
$destinationPath = [System.IO.Path]::GetFullPath($Destination)
if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
    throw "Source cloudflared binary not found: $sourcePath"
}

$encoding = [System.Text.Encoding]::GetEncoding(28591)
$sourceBytes = [System.IO.File]::ReadAllBytes($sourcePath)
$sourceText = $encoding.GetString($sourceBytes)
$from = '/etc/resolv.conf'
$to = '/data/xui/resolv'
$occurrences = ([regex]::Matches($sourceText, [regex]::Escape($from))).Count

if ($occurrences -ne 1) {
    throw "Expected one occurrence of $from, found $occurrences. Refusing to patch an unknown binary."
}
if ($from.Length -ne $to.Length) {
    throw "Patch changes binary length: $from -> $to"
}

$patchedBytes = $encoding.GetBytes($sourceText.Replace($from, $to))
if ($patchedBytes.Length -ne $sourceBytes.Length) {
    throw 'Patched binary length changed unexpectedly.'
}

$destinationDirectory = Split-Path -Parent $destinationPath
New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null
[System.IO.File]::WriteAllBytes($destinationPath, $patchedBytes)

$patchedText = $encoding.GetString($patchedBytes)
if ($patchedText.Contains($from) -or -not $patchedText.Contains($to)) {
    throw 'Post-patch verification failed for the cloudflared resolver path.'
}

$hash = (Get-FileHash -LiteralPath $destinationPath -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host "Patched cloudflared resolver path to $to."
Write-Host "Patched cloudflared SHA-256: $hash"
