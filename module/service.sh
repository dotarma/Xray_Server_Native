#!/system/bin/sh

MODDIR=${0%/*}
export MODDIR
export SSL_CERT_FILE=/data/xui/cacert.pem

sleep 15
mkdir -p /data/xui
mount -o bind "$MODDIR" /data/xui 2>/dev/null

cd "$MODDIR" || exit 1
nohup "$MODDIR/scripts/supervisor.sh" >> /data/xui/boot.log 2>&1 &
