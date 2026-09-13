#!/system/bin/sh

if [ -z "${MODDIR:-}" ]; then
  SCRIPT_DIR=${0%/*}
  MODDIR=${SCRIPT_DIR%/scripts}
fi
. "$MODDIR/scripts/common.sh"

ensure_state_dirs || exit 1

if ! acquire_lock "$SUPERVISOR_LOCK_DIR"; then
  exit 0
fi
printf '%s\n' "$$" > "$SUPERVISOR_PID_FILE"
cleanup() {
  rm -f "$SUPERVISOR_PID_FILE"
  release_lock "$SUPERVISOR_LOCK_DIR"
}
trap cleanup EXIT
trap 'cleanup; exit 0' INT TERM

while [ "$(getprop sys.boot_completed 2>/dev/null)" != "1" ]; do
  sleep 2
done

if [ "$KSU" = "true" ]; then
  manager=kernelsu
else
  manager=magisk
fi
log_line "supervisor started manager=$manager"

while :; do
  [ -f "$MODDIR/disable" ] && {
    log_line "module disabled; stopping services"
    stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"
    stop_pid_file "$PANEL_TUNNEL_PID_FILE" "cloudflared"
    stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"
    stop_native_demux
    stop_panel_demux
    stop_pid_file "$PANEL_PID_FILE" "x-ui"
    stop_pid_file "$MANAGER_PID_FILE" "android-mini-server-manager"
    exit 0
  }

  load_config
  if ! ensure_state_dirs; then
    log_line "state initialization failed; retrying"
    sleep 5
    continue
  fi

  if is_valid_interval "$SUPERVISOR_INTERVAL"; then
    interval=$SUPERVISOR_INTERVAL
  else
    interval=20
  fi

  start_manager
  start_panel
  start_panel_demux
  start_panel_tunnel
  if is_true "$MODE_ENABLED"; then
    start_quick_server
    start_native_demux
    start_tunnel
  else
    stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"
    stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"
    stop_native_demux
  fi
  sleep "$interval"
done
