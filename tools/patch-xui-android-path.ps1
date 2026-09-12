[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string]$Source,
    [Parameter(Mandatory)] [string]$Destination
)

$ErrorActionPreference = 'Stop'

$sourcePath = [System.IO.Path]::GetFullPath($Source)
$destinationPath = [System.IO.Path]::GetFullPath($Destination)
if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
    throw "Source x-ui binary not found: $sourcePath"
}

$encoding = [System.Text.Encoding]::GetEncoding(28591)
$sourceBytes = [System.IO.File]::ReadAllBytes($sourcePath)
$sourceText = $encoding.GetString($sourceBytes)
$patchRules = @(
    @{ From = '/etc/x-ui'; To = '/data/xui'; Expected = 2 },
    @{ From = '/var/log/x-ui'; To = '/data/xui/log'; Expected = 1 }
)

$patchedText = $sourceText
foreach ($rule in $patchRules) {
    $occurrences = ([regex]::Matches($patchedText, [regex]::Escape($rule.From))).Count
    if ($occurrences -ne $rule.Expected) {
        throw "Expected $($rule.Expected) occurrences of $($rule.From), found $occurrences. Refusing to patch an unknown binary."
    }
    if ($rule.From.Length -ne $rule.To.Length) {
        throw "Patch changes binary length: $($rule.From) -> $($rule.To)"
    }
    $patchedText = $patchedText.Replace($rule.From, $rule.To)
}
$patchedBytes = $encoding.GetBytes($patchedText)
if ($patchedBytes.Length -ne $sourceBytes.Length) {
    throw 'Patched binary length changed unexpectedly.'
}

$destinationDirectory = Split-Path -Parent $destinationPath
New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null
[System.IO.File]::WriteAllBytes($destinationPath, $patchedBytes)

foreach ($rule in $patchRules) {
    $remaining = ([regex]::Matches($patchedText, [regex]::Escape($rule.From))).Count
    if ($remaining -ne 0) {
        throw "Post-patch verification failed for $($rule.From)."
    }
}

$hash = (Get-FileHash -LiteralPath $destinationPath -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host 'Patched x-ui Linux paths for the /data/xui Android runtime mount.'
Write-Host "Patched x-ui SHA-256: $hash"
