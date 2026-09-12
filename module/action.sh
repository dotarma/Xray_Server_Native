#!/system/bin/sh

MODDIR=${0%/*}
export MODDIR
"$MODDIR/scripts/control.sh" restart
sleep 2
"$MODDIR/scripts/control.sh" status
printf '%s\n' 'Manager: http://127.0.0.1:2036/'
printf '%s\n' 'The manager has no separate login. Open the local URL in a browser.'
