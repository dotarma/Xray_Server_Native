#!/system/bin/sh

MODDIR=${0%/*}
"$MODDIR/scripts/control.sh" stop >/dev/null 2>&1
umount /data/xui 2>/dev/null

# Runtime data lives inside the module directory. Export anything needed before
# uninstalling; the root manager removes that directory after this hook exits.
