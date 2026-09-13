$ErrorActionPreference = 'Stop'
$modulePath = '/data/adb/modules/android-mini-server-native/panel-credentials.txt'

adb forward tcp:2036 tcp:2036 | Out-Null
adb forward tcp:2053 tcp:2053 | Out-Null
$credentialLines = @(adb shell su -mm -c "cat $modulePath")
if ($LASTEXITCODE -ne 0) { throw 'Could not read the root-only panel credentials from the device.' }
$credentials = @{}
foreach ($line in $credentialLines) {
    $parts = $line -split '=', 2
    if ($parts.Count -eq 2) { $credentials[$parts[0]] = $parts[1] }
}
$username, $password = $credentials.username, $credentials.password
if ([string]::IsNullOrWhiteSpace($username) -or [string]::IsNullOrWhiteSpace($password)) { throw 'Panel credentials are incomplete.' }
$basic = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("${username}:${password}"))
$status = Invoke-RestMethod http://127.0.0.1:2036/api/status -Headers @{ Authorization = "Basic $basic" }
$base = $status.panelUrl.TrimEnd('/')
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$page = Invoke-WebRequest -UseBasicParsing "$base/" -WebSession $session
$csrf = [regex]::Match($page.Content, 'name="csrf-token" content="([^"]+)"').Groups[1].Value
$login = Invoke-RestMethod "$base/login" -Method Post -WebSession $session -Headers @{'X-CSRF-Token' = $csrf} -ContentType application/json -Body (@{username = $username; password = $password; twoFactorCode = ''} | ConvertTo-Json)
if (-not $login.success) { throw 'Panel login failed' }
$csrf = (Invoke-RestMethod "$base/csrf-token" -WebSession $session).obj
# The authenticated session is ready for requests to http://127.0.0.1:2053.
