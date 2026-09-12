$ErrorActionPreference='Stop'
adb forward tcp:2036 tcp:2036 | Out-Null
adb forward tcp:2053 tcp:2053 | Out-Null
$status=Invoke-RestMethod http://127.0.0.1:2036/api/status
$base=$status.panelUrl.TrimEnd('/')
$session=New-Object Microsoft.PowerShell.Commands.WebRequestSession
$page=Invoke-WebRequest -UseBasicParsing "$base/" -WebSession $session
$csrf=[regex]::Match($page.Content,'name="csrf-token" content="([^"]+)"').Groups[1].Value
$login=Invoke-RestMethod "$base/login" -Method Post -WebSession $session -Headers @{'X-CSRF-Token'=$csrf} -ContentType application/json -Body (@{username=$status.panelUsername;password=$status.panelPassword;twoFactorCode=''}|ConvertTo-Json)
if(-not $login.success){throw 'Panel login failed'}
$csrf=(Invoke-RestMethod "$base/csrf-token" -WebSession $session).obj
