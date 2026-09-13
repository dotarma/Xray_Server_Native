#!/system/bin/sh

if [ -z "${MODDIR:-}" ]; then
  SCRIPT_DIR=${0%/*}
  MODDIR=${SCRIPT_DIR%/scripts}
fi
. "$MODDIR/scripts/common.sh"

usage() {
  cat <<'EOF'
Usage: control.sh <command> [arguments]

Commands:
  start                     Start configured services now.
  stop                      Stop both Cloudflare tunnels, native Xray, and x-ui.
  restart                   Restart configured services.
  start-panel|stop-panel|restart-panel
                            Control only the 3x-ui panel.
  start-tunnel|stop-tunnel|restart-tunnel
                            Control only the Mode 1/2 Cloudflare Tunnel.
  start-panel-tunnel|stop-panel-tunnel|restart-panel-tunnel
                            Control the independent Cloudflare Tunnel for 3x-ui.
  start-quick-server|stop-quick-server|restart-quick-server
                            Control the native Xray used by Modes 1 and 2.
  start-native-demux|stop-native-demux|restart-native-demux
                            Control the Mode 2 WS/xHTTP demux listener.
  start-panel-demux|stop-panel-demux|restart-panel-demux
                            Control the Mode 3 WS/xHTTP demux listener.
  start-mode|stop-mode|restart-mode
                            Control the configured mode without stopping 3x-ui.
  status                    Print binary, process, and configuration state.
  credentials               Print the locally generated panel URL and credentials.
  check                     Execute version checks for installed binaries.
  logs [panel|panel-tunnel|tunnel|quick|all]
                            Print the last 120 log lines.
  set-token <token>         Save a Cloudflare Tunnel token securely and restart.
  set-admin <user> <pass> [base-path]
                            Change the first panel user, password, and optional path.
EOF
}

print_process_status() {
  label=$1
  pid_file=$2
  expected=${3:-$label}
  if pid_is_running "$pid_file" "$expected"; then
    printf '%s: running (pid %s)\n' "$label" "$(cat "$pid_file")"
  else
    printf '%s: stopped\n' "$label"
  fi
}

set_config_value() {
  key=$1
  value=$2
  temp_file="$CONFIG_FILE.$$.tmp"
  umask 077
  if [ -f "$CONFIG_FILE" ]; then
    sed "/^${key}=/d" "$CONFIG_FILE" > "$temp_file" || return 1
  else
    : > "$temp_file" || return 1
  fi
  printf '%s=%s\n' "$key" "$value" >> "$temp_file" || return 1
  chmod 600 "$temp_file" 2>/dev/null
  mv -f "$temp_file" "$CONFIG_FILE"
}

command_start() {
  ensure_state_dirs || exit 1
  load_config
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
}

command_stop() {
  keep_manager=${1:-}
  stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"
  stop_native_demux
  stop_panel_demux
  stop_pid_file "$PANEL_TUNNEL_PID_FILE" "cloudflared"
  stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"
  stop_pid_file "$PANEL_PID_FILE" "x-ui"
  if [ "$keep_manager" != "keep-manager" ]; then
    stop_pid_file "$MANAGER_PID_FILE" "android-mini-server-manager"
  fi
}

command_start_mode() {
  ensure_state_dirs || exit 1
  load_config
  [ "$ACTIVE_MODE" != "none" ] || {
    printf '%s\n' 'No deployment is configured yet.'
    return 1
  }
  set_config_value MODE_ENABLED true || return 1
  set_config_value TUNNEL_ENABLED true || return 1
  case "$ACTIVE_MODE" in
    mode1|mode2|quick) set_config_value QUICK_SERVER_ENABLED true || return 1 ;;
    *)
      printf '%s\n' 'Only Mode 1 or Mode 2 can be controlled from this card.'
      return 1
      ;;
  esac
  load_config
  start_quick_server
  start_native_demux
  start_tunnel
}

command_stop_mode() {
  ensure_state_dirs || exit 1
  set_config_value MODE_ENABLED false || return 1
  stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"
  stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"
  stop_native_demux
}

command_status() {
  ensure_state_dirs || exit 1
  load_config
  printf 'module: %s\n' "$MODDIR"
  printf 'state: %s\n' "$STATE_DIR"
  printf 'abi: %s\n' "$(getprop ro.product.cpu.abi 2>/dev/null)"
  printf 'panel config: enabled=%s listen=%s port=%s\n' "$PANEL_ENABLED" "$PANEL_LISTEN" "$PANEL_PORT"
  printf 'panel tunnel config: enabled=%s mode=%s target=%s\n' "$PANEL_TUNNEL_ENABLED" "$PANEL_TUNNEL_MODE" "$PANEL_TUNNEL_TARGET"
  printf 'active mode: %s\n' "$ACTIVE_MODE"
  printf 'mode enabled: %s\n' "$MODE_ENABLED"
  print_process_status "x-ui" "$PANEL_PID_FILE"
  print_process_status "panel-cloudflared" "$PANEL_TUNNEL_PID_FILE" "cloudflared"
  printf 'native Xray config: enabled=%s listen=127.0.0.1 port=%s\n' "$QUICK_SERVER_ENABLED" "$QUICK_SERVER_PORT"
  print_process_status "xray-linux-arm64" "$QUICK_SERVER_PID_FILE"
  printf 'native transport demux: enabled=%s listen=127.0.0.1:%s ws=127.0.0.1:%s xhttp=127.0.0.1:%s\n' "$NATIVE_DEMUX_ENABLED" "$NATIVE_DEMUX_PORT" "$NATIVE_DEMUX_WS_PORT" "$NATIVE_DEMUX_XHTTP_PORT"
  print_process_status "native transport demux" "$NATIVE_DEMUX_PID_FILE" "demux-listen"
  printf 'panel transport demux: enabled=%s listen=127.0.0.1:%s ws=127.0.0.1:%s xhttp=127.0.0.1:%s\n' "$PANEL_DEMUX_ENABLED" "$PANEL_DEMUX_PORT" "$PANEL_DEMUX_WS_PORT" "$PANEL_DEMUX_XHTTP_PORT"
  print_process_status "panel transport demux" "$PANEL_DEMUX_PID_FILE" "demux-listen"
  print_process_status "cloudflared" "$TUNNEL_PID_FILE"
  print_process_status "android-mini-server-manager" "$MANAGER_PID_FILE"
  [ -x "$XUI_BIN" ] && printf 'x-ui binary: ready\n' || printf 'x-ui binary: missing\n'
  [ -x "$CLOUDFLARED_BIN" ] && printf 'cloudflared binary: ready\n' || printf 'cloudflared binary: missing\n'
  [ -x "$QUICK_XRAY_BIN" ] && printf 'quick Xray binary: ready\n' || printf 'quick Xray binary: missing\n'
  [ -x "$MANAGER_BIN" ] && printf 'manager binary: ready\n' || printf 'manager binary: missing\n'
  [ -s "$TOKEN_FILE" ] && printf 'tunnel token: configured\n' || printf 'tunnel token: absent\n'
  [ -f "$CREDENTIALS_FILE" ] && printf 'panel credentials: %s\n' "$CREDENTIALS_FILE" || printf 'panel credentials: not initialized\n'
}

command_check() {
  ensure_state_dirs || exit 1
  load_config
  if [ -x "$XUI_BIN" ]; then
    printf '%s\n' 'Checking x-ui:'
    xui_env
    "$XUI_BIN" -v
  else
    printf '%s\n' 'x-ui binary missing.'
  fi
  if [ -x "$CLOUDFLARED_BIN" ]; then
    printf '%s\n' 'Checking cloudflared:'
    "$CLOUDFLARED_BIN" --version
  else
    printf '%s\n' 'cloudflared binary missing.'
  fi
  if [ -x "$QUICK_XRAY_BIN" ]; then
    printf '%s\n' 'Checking standalone Quick Tunnel Xray:'
    "$QUICK_XRAY_BIN" version
  else
    printf '%s\n' 'quick Xray binary missing.'
  fi
}

command_credentials() {
  if [ ! -f "$CREDENTIALS_FILE" ]; then
    printf '%s\n' 'Panel credentials have not been initialized yet.'
    return 1
  fi
  cat "$CREDENTIALS_FILE"
}

command_logs() {
  target=${1:-all}
  case "$target" in
    panel) tail -n 120 "$PANEL_LOG" 2>/dev/null ;;
    panel-tunnel) tail -n 120 "$PANEL_TUNNEL_LOG" 2>/dev/null ;;
    tunnel) tail -n 120 "$TUNNEL_LOG" 2>/dev/null ;;
    quick) tail -n 120 "$QUICK_SERVER_LOG" 2>/dev/null ;;
  all)
      printf '%s\n' '--- supervisor ---'
      tail -n 120 "$SUPERVISOR_LOG" 2>/dev/null
      printf '%s\n' '--- x-ui ---'
      tail -n 120 "$PANEL_LOG" 2>/dev/null
      printf '%s\n' '--- cloudflared ---'
      tail -n 120 "$TUNNEL_LOG" 2>/dev/null
      printf '%s\n' '--- panel cloudflared ---'
      tail -n 120 "$PANEL_TUNNEL_LOG" 2>/dev/null
      printf '%s\n' '--- quick xray ---'
      tail -n 120 "$QUICK_SERVER_LOG" 2>/dev/null
      printf '%s\n' '--- manager ---'
      tail -n 120 "$MANAGER_LOG" 2>/dev/null
      ;;
    *) usage; return 2 ;;
  esac
}

command_set_token() {
  token=$1
  [ -n "$token" ] || return 2
  ensure_state_dirs || exit 1
  umask 077
  printf '%s\n' "$token" > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE" 2>/dev/null
  command_stop
  command_start
}

command_set_admin() {
  user=$1
  password=$2
  requested_path=${3:-}
  [ -n "$user" ] && [ -n "$password" ] || return 2
  ensure_state_dirs || exit 1
  load_config
  [ -x "$XUI_BIN" ] || return 1
  base_path=${requested_path:-$(resolve_panel_base_path)}
  case "$base_path" in
    /*) ;;
    *) base_path="/$base_path/" ;;
  esac
  xui_env
  "$XUI_BIN" setting -username "$user" -password "$password" -webBasePath "$base_path" >> "$PANEL_LOG" 2>&1 || return 1
  umask 077
  {
    printf 'username=%s\n' "$user"
    printf 'password=%s\n' "$password"
    printf 'base_path=%s\n' "$base_path"
    printf 'local_url=http://%s:%s%s\n' "$PANEL_LISTEN" "$PANEL_PORT" "$base_path"
  } > "$CREDENTIALS_FILE"
  chmod 600 "$CREDENTIALS_FILE" 2>/dev/null
  printf '%s\n' "$base_path" > "$BASE_PATH_FILE"
  command_stop
  command_start
}

case "${1:-}" in
  start) command_start ;;
  stop) command_stop "$2" ;;
  restart) command_stop "$2"; command_start ;;
  start-panel) ensure_state_dirs; load_config; start_panel ;;
  stop-panel) stop_pid_file "$PANEL_PID_FILE" "x-ui" ;;
  restart-panel) stop_pid_file "$PANEL_PID_FILE" "x-ui"; ensure_state_dirs; load_config; start_panel ;;
  start-quick-server) ensure_state_dirs; set_config_value MODE_ENABLED true; set_config_value QUICK_SERVER_ENABLED true; load_config; start_quick_server ;;
  stop-quick-server) ensure_state_dirs; set_config_value QUICK_SERVER_ENABLED false; stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64" ;;
  restart-quick-server) stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"; ensure_state_dirs; set_config_value MODE_ENABLED true; set_config_value QUICK_SERVER_ENABLED true; load_config; start_quick_server ;;
  start-native-demux) ensure_state_dirs; set_config_value NATIVE_DEMUX_ENABLED true; load_config; start_native_demux ;;
  stop-native-demux) ensure_state_dirs; set_config_value NATIVE_DEMUX_ENABLED false; stop_native_demux ;;
  restart-native-demux) stop_native_demux; ensure_state_dirs; set_config_value NATIVE_DEMUX_ENABLED true; load_config; start_native_demux ;;
  start-panel-demux) ensure_state_dirs; set_config_value PANEL_DEMUX_ENABLED true; load_config; start_panel_demux ;;
  stop-panel-demux) ensure_state_dirs; set_config_value PANEL_DEMUX_ENABLED false; stop_panel_demux ;;
  restart-panel-demux) stop_panel_demux; ensure_state_dirs; set_config_value PANEL_DEMUX_ENABLED true; load_config; start_panel_demux ;;
  start-mode) command_start_mode ;;
  stop-mode) command_stop_mode ;;
  restart-mode) command_stop_mode; command_start_mode ;;
  start-tunnel) ensure_state_dirs; set_config_value MODE_ENABLED true; set_config_value TUNNEL_ENABLED true; load_config; start_tunnel ;;
  stop-tunnel) ensure_state_dirs; set_config_value TUNNEL_ENABLED false; stop_pid_file "$TUNNEL_PID_FILE" "cloudflared" ;;
  restart-tunnel) stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"; ensure_state_dirs; set_config_value MODE_ENABLED true; set_config_value TUNNEL_ENABLED true; load_config; start_tunnel ;;
  start-panel-tunnel) ensure_state_dirs; set_config_value PANEL_TUNNEL_ENABLED true; load_config; start_panel_tunnel ;;
  stop-panel-tunnel) ensure_state_dirs; set_config_value PANEL_TUNNEL_ENABLED false; stop_pid_file "$PANEL_TUNNEL_PID_FILE" "cloudflared" ;;
  restart-panel-tunnel) stop_pid_file "$PANEL_TUNNEL_PID_FILE" "cloudflared"; ensure_state_dirs; set_config_value PANEL_TUNNEL_ENABLED true; load_config; start_panel_tunnel ;;
  status) command_status ;;
  credentials) command_credentials ;;
  check) command_check ;;
  logs) shift; command_logs "$@" ;;
  set-token) shift; command_set_token "$1" ;;
  set-admin) shift; command_set_admin "$1" "$2" "$3" ;;
  ''|-h|--help|help) usage ;;
  *) usage; exit 2 ;;
esac
