param([switch]$Public)
$ErrorActionPreference='Stop'
$cfg=(adb shell "su -mm -c 'cat /data/adb/modules/android-mini-server-native/bin/config.json'" | Out-String) | ConvertFrom-Json
$ib=$cfg.inbounds | Where-Object protocol -eq vless | Select-Object -First 1
$stream=@{network='ws';security='none';sockopt=@{domainStrategy='UseIPv4'};wsSettings=@{path=$ib.streamSettings.wsSettings.path;host=$ib.streamSettings.wsSettings.host}}
$address='127.0.0.1'
$port=$ib.port
if($Public){
    $address='api24-normal-alisg.tiktokv.com'
    $port=443
    $stream.security='tls'
    $stream.tlsSettings=@{serverName=$ib.streamSettings.wsSettings.host;fingerprint='chrome';alpn=@('http/1.1')}
}
$client=@{
    log=@{loglevel='debug'}
    dns=@{servers=@('1.1.1.1','8.8.8.8');queryStrategy='UseIPv4'}
    inbounds=@(@{listen='127.0.0.1';port=18090;protocol='http';settings=@{}})
    routing=@{rules=@(@{type='field';network='udp';port='53';outboundTag='dns-direct'})}
    outbounds=@(@{protocol='vless';settings=@{vnext=@(@{address=$address;port=$port;users=@(@{id=$ib.settings.clients[0].id;encryption='none'})})};streamSettings=$stream},@{tag='dns-direct';protocol='freedom';settings=@{}})
}
$temp=Join-Path $env:TEMP 'mini-server-mode3-diagnostic.json'
[IO.File]::WriteAllText($temp,($client|ConvertTo-Json -Depth 20),(New-Object Text.UTF8Encoding($false)))
adb push $temp /data/local/tmp/mini-server-mode3-diagnostic.json
Remove-Item -LiteralPath $temp
