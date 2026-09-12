#!/system/bin/sh

# Fallback for ROMs where the late-start module stage races boot completion.
# supervisor.sh owns a PID lock, so this never runs a duplicate supervisor.
MODDIR=${0%/*}
exec "$MODDIR/scripts/supervisor.sh"
