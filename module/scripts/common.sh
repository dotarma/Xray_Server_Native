#!/system/bin/sh

MODULE_ID=android-mini-server-native
if [ -z "${MODDIR:-}" ]; then
  SCRIPT_DIR=${0%/*}
  case "$SCRIPT_DIR" in
    */scripts) MODDIR=${SCRIPT_DIR%/scripts} ;;
    *) MODDIR=$SCRIPT_DIR ;;
  esac
fi
# Keep mutable module state under MODDIR. Unlike /data/xui, this path is shared
# by the Magisk service and its child processes across their mount namespaces.
# /data/xui remains only the runtime mount expected by 3x-ui itself.
STATE_DIR=$MODDIR
CONFIG_FILE=$STATE_DIR/service.env
TOKEN_FILE=$STATE_DIR/token.txt
PANEL_TUNNEL_TOKEN_FILE=$STATE_DIR/panel-tunnel-token.txt
CREDENTIALS_FILE=$STATE_DIR/panel-credentials.txt
BASE_PATH_FILE=$STATE_DIR/panel-base-path.txt
PANEL_SETTINGS_FILE=$STATE_DIR/panel-settings.sig
PID_DIR=$STATE_DIR/run
LOG_DIR=$STATE_DIR/log
PANEL_PID_FILE=$PID_DIR/x-ui.pid
TUNNEL_PID_FILE=$PID_DIR/cloudflared.pid
PANEL_TUNNEL_PID_FILE=$PID_DIR/panel-cloudflared.pid
QUICK_SERVER_PID_FILE=$PID_DIR/quick-xray.pid
NATIVE_DEMUX_PID_FILE=$PID_DIR/native-demux.pid
PANEL_DEMUX_PID_FILE=$PID_DIR/panel-demux.pid
PANEL_LOG=$LOG_DIR/x-ui.log
TUNNEL_LOG=$LOG_DIR/cloudflared.log
PANEL_TUNNEL_LOG=$LOG_DIR/panel-cloudflared.log
QUICK_SERVER_LOG=$LOG_DIR/quick-xray.log
NATIVE_DEMUX_LOG=$LOG_DIR/native-demux.log
PANEL_DEMUX_LOG=$LOG_DIR/panel-demux.log
SUPERVISOR_LOG=$LOG_DIR/supervisor.log
SUPERVISOR_PID_FILE=$PID_DIR/supervisor.pid
MANAGER_LOG=$LOG_DIR/manager.log
MANAGER_PID_FILE=$PID_DIR/manager.pid
XUI_BIN=$MODDIR/x-ui
CLOUDFLARED_BIN=$MODDIR/bin/cloudflared
QUICK_XRAY_BIN=$MODDIR/bin/xray-linux-arm64
QUICK_XRAY_CONFIG=$STATE_DIR/quick-xray.json
MANAGER_BIN=$MODDIR/manager/android-mini-server-manager
CA_CERT_FILE=$MODDIR/cacert.pem
RESOLV_FILE=$STATE_DIR/resolv

PANEL_ENABLED=true
PANEL_PORT=2053
PANEL_LISTEN=127.0.0.1
PANEL_BASE_PATH=/
PANEL_USERNAME=admin
TUNNEL_ENABLED=false
TUNNEL_MODE=named
QUICK_TUNNEL_TARGET=http://127.0.0.1:8888
QUICK_SERVER_ENABLED=false
QUICK_SERVER_PORT=8888
NATIVE_DEMUX_ENABLED=false
NATIVE_DEMUX_PORT=8888
NATIVE_DEMUX_WS_PORT=28888
NATIVE_DEMUX_XHTTP_PORT=38888
PANEL_DEMUX_ENABLED=false
PANEL_DEMUX_PORT=8080
PANEL_DEMUX_WS_PORT=28080
PANEL_DEMUX_XHTTP_PORT=38080
ACTIVE_MODE=none
MODE_ENABLED=false
PANEL_TUNNEL_ENABLED=false
PANEL_TUNNEL_MODE=named
PANEL_TUNNEL_TARGET=http://127.0.0.1:2053
MANAGER_ENABLED=true
MANAGER_PORT=2036
SUPERVISOR_INTERVAL=20

log_line() {
  mkdir -p "$LOG_DIR" 2>/dev/null
  message="$(date '+%Y-%m-%d %H:%M:%S') $*"
  printf '%s\n' "$message" >> "$SUPERVISOR_LOG"
  /system/bin/log -t AndroidMiniServer "$message" 2>/dev/null || true
}

ensure_state_dirs() {
  mkdir -p /data/xui || return 1
  if ! grep -q ' /data/xui ' /proc/mounts 2>/dev/null; then
    mount -o bind "$MODDIR" /data/xui || return 1
  fi
  mkdir -p "$STATE_DIR" "$STATE_DIR/db" "$PID_DIR" "$LOG_DIR" || return 1
  chmod 700 "$STATE_DIR/db" "$PID_DIR" "$LOG_DIR" 2>/dev/null

  if [ ! -f "$CONFIG_FILE" ] && [ -r "$MODDIR/config/service.env.example" ]; then
    umask 077
    cp -f "$MODDIR/config/service.env.example" "$CONFIG_FILE" || return 1
    chmod 600 "$CONFIG_FILE" 2>/dev/null
  fi
  if [ ! -f "$TOKEN_FILE" ]; then
    umask 077
    : > "$TOKEN_FILE" || return 1
    chmod 600 "$TOKEN_FILE" 2>/dev/null
  fi
  if [ ! -f "$PANEL_TUNNEL_TOKEN_FILE" ]; then
    umask 077
    : > "$PANEL_TUNNEL_TOKEN_FILE" || return 1
    chmod 600 "$PANEL_TUNNEL_TOKEN_FILE" 2>/dev/null
  fi
  if [ ! -s "$RESOLV_FILE" ] && [ -r "$MODDIR/config/resolv.conf" ]; then
    cp -f "$MODDIR/config/resolv.conf" "$RESOLV_FILE" || return 1
    chmod 644 "$RESOLV_FILE" 2>/dev/null
  fi
}

load_config() {
  PANEL_ENABLED=true
  PANEL_PORT=2053
  PANEL_LISTEN=127.0.0.1
  PANEL_BASE_PATH=/
  PANEL_USERNAME=admin
  TUNNEL_ENABLED=false
  TUNNEL_MODE=named
  QUICK_TUNNEL_TARGET=http://127.0.0.1:8888
  QUICK_SERVER_ENABLED=false
  QUICK_SERVER_PORT=8888
  NATIVE_DEMUX_ENABLED=false
  NATIVE_DEMUX_PORT=8888
  NATIVE_DEMUX_WS_PORT=28888
  NATIVE_DEMUX_XHTTP_PORT=38888
  PANEL_DEMUX_ENABLED=false
  PANEL_DEMUX_PORT=8080
  PANEL_DEMUX_WS_PORT=28080
  PANEL_DEMUX_XHTTP_PORT=38080
  ACTIVE_MODE=none
  MODE_ENABLED=false
  PANEL_TUNNEL_ENABLED=false
  PANEL_TUNNEL_MODE=named
  PANEL_TUNNEL_TARGET=http://127.0.0.1:2053
  MANAGER_ENABLED=true
  MANAGER_PORT=2036
  SUPERVISOR_INTERVAL=20

  if [ -f "$CONFIG_FILE" ]; then
    # This file is root-owned under /data/adb and intentionally supports shell values.
    . "$CONFIG_FILE"
  fi

  # Preserve a configured deployment when upgrading from releases before
  # MODE_ENABLED existed. New installations stay disabled until a mode is set.
  if [ -f "$CONFIG_FILE" ] && ! grep -q '^MODE_ENABLED=' "$CONFIG_FILE" 2>/dev/null; then
    if is_true "$QUICK_SERVER_ENABLED" || is_true "$TUNNEL_ENABLED"; then
      MODE_ENABLED=true
    fi
  fi
}

is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

is_valid_port() {
  case "${1:-}" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 1 ] 2>/dev/null && [ "$1" -le 65535 ] 2>/dev/null
}

is_valid_interval() {
  case "${1:-}" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$1" -ge 10 ] 2>/dev/null && [ "$1" -le 3600 ] 2>/dev/null
}

pid_is_running() {
  pid_file=$1
  expected=${2:-}
  [ -f "$pid_file" ] || return 1
  pid=$(cat "$pid_file" 2>/dev/null)
  case "$pid" in
    ''|*[!0-9]*) return 1 ;;
  esac
  kill -0 "$pid" 2>/dev/null || return 1
  [ -z "$expected" ] && return 0
  [ -r "/proc/$pid/cmdline" ] || return 1
  tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null | grep -F -q "$expected"
}

remove_stale_pid() {
  pid_file=$1
  expected=${2:-}
  if ! pid_is_running "$pid_file" "$expected"; then
    rm -f "$pid_file"
  fi
}

stop_pid_file() {
  pid_file=$1
  name=$2
  if ! pid_is_running "$pid_file" "$name"; then
    rm -f "$pid_file"
    return 0
  fi

  pid=$(cat "$pid_file")
  log_line "stopping $name pid=$pid"
  kill -TERM "$pid" 2>/dev/null
  attempt=0
  while pid_is_running "$pid_file" "$name" && [ "$attempt" -lt 15 ]; do
    sleep 1
    attempt=$((attempt + 1))
  done
  if pid_is_running "$pid_file" "$name"; then
    log_line "force stopping $name pid=$pid"
    kill -KILL "$pid" 2>/dev/null
  fi
  rm -f "$pid_file"
}

generate_secret() {
  tr -dc 'A-Za-z0-9' < /dev/urandom 2>/dev/null | head -c "${1:-24}"
}

resolve_panel_base_path() {
  requested=${PANEL_BASE_PATH:-auto}
  if [ "$requested" = "auto" ] || [ -z "$requested" ]; then
    if [ -s "$BASE_PATH_FILE" ]; then
      cat "$BASE_PATH_FILE"
      return 0
    fi
    generated=$(generate_secret 20)
    [ -n "$generated" ] || generated="panel$(date +%s)"
    printf '/%s/\n' "$generated" > "$BASE_PATH_FILE"
    chmod 600 "$BASE_PATH_FILE" 2>/dev/null
    cat "$BASE_PATH_FILE"
    return 0
  fi

  case "$requested" in
    /*) printf '%s\n' "$requested" ;;
    *) printf '/%s/\n' "$requested" ;;
  esac
}

xui_env() {
  export XUI_MAIN_FOLDER=/data/xui
  export XUI_BIN_FOLDER=/data/xui/bin
  export XUI_DB_FOLDER="$STATE_DIR/db"
  export XUI_LOG_FOLDER="$LOG_DIR/x-ui"
  export XUI_NODE_TOKEN_KEY_FILE="$STATE_DIR/db/node-token-key.json"
  export XUI_PORT="$PANEL_PORT"
  if [ -r "$CA_CERT_FILE" ]; then
    export SSL_CERT_FILE="$CA_CERT_FILE"
  fi
}

panel_signature() {
  printf 'port=%s;listen=%s;base=%s' "$PANEL_PORT" "$PANEL_LISTEN" "$(resolve_panel_base_path)"
}

configure_panel() {
  [ -x "$XUI_BIN" ] || return 1
  is_valid_port "$PANEL_PORT" || {
    log_line "invalid PANEL_PORT: $PANEL_PORT"
    return 1
  }

  base_path=$(resolve_panel_base_path)
  desired="port=$PANEL_PORT;listen=$PANEL_LISTEN;base=$base_path"
  previous=$(cat "$PANEL_SETTINGS_FILE" 2>/dev/null)

  xui_env
  if [ ! -f "$CREDENTIALS_FILE" ]; then
    password=$(generate_secret 28)
    [ -n "$password" ] || {
      log_line "could not generate panel password"
      return 1
    }
    "$XUI_BIN" setting -port "$PANEL_PORT" -listenIP "$PANEL_LISTEN" -webBasePath "$base_path" -username "$PANEL_USERNAME" -password "$password" >> "$PANEL_LOG" 2>&1 || return 1
    umask 077
    {
      printf 'username=%s\n' "$PANEL_USERNAME"
      printf 'password=%s\n' "$password"
      printf 'base_path=%s\n' "$base_path"
      printf 'local_url=http://%s:%s%s\n' "$PANEL_LISTEN" "$PANEL_PORT" "$base_path"
    } > "$CREDENTIALS_FILE"
    chmod 600 "$CREDENTIALS_FILE" 2>/dev/null
    log_line "initialized local panel credentials"
  elif [ "$desired" != "$previous" ]; then
    "$XUI_BIN" setting -port "$PANEL_PORT" -listenIP "$PANEL_LISTEN" -webBasePath "$base_path" >> "$PANEL_LOG" 2>&1 || return 1
  fi

  printf '%s\n' "$desired" > "$PANEL_SETTINGS_FILE"
  chmod 600 "$PANEL_SETTINGS_FILE" 2>/dev/null
}

run_panel() {
  cd /data/xui || exit 1
  xui_env
  exec "$XUI_BIN"
}

run_tunnel() {
  cd "$MODDIR" || exit 1
  if [ -r "$CA_CERT_FILE" ]; then
    export SSL_CERT_FILE="$CA_CERT_FILE"
  fi
  if [ "$TUNNEL_MODE" = "quick" ]; then
    : > "$TUNNEL_LOG"
    exec "$CLOUDFLARED_BIN" tunnel --no-autoupdate --protocol http2 --url "$QUICK_TUNNEL_TARGET"
  fi
  [ -s "$TOKEN_FILE" ] || return 1
  exec "$CLOUDFLARED_BIN" tunnel --no-autoupdate run --token-file "$TOKEN_FILE"
}

run_panel_tunnel() {
  cd "$MODDIR" || exit 1
  if [ -r "$CA_CERT_FILE" ]; then
    export SSL_CERT_FILE="$CA_CERT_FILE"
  fi
  if [ "$PANEL_TUNNEL_MODE" = "quick" ]; then
    : > "$PANEL_TUNNEL_LOG"
    exec "$CLOUDFLARED_BIN" tunnel --no-autoupdate --protocol http2 --url "$PANEL_TUNNEL_TARGET"
  fi
  [ -s "$PANEL_TUNNEL_TOKEN_FILE" ] || return 1
  exec "$CLOUDFLARED_BIN" tunnel --no-autoupdate run --token-file "$PANEL_TUNNEL_TOKEN_FILE"
}

run_quick_server() {
  if [ -r "$CA_CERT_FILE" ]; then
    export SSL_CERT_FILE="$CA_CERT_FILE"
  fi
  exec "$QUICK_XRAY_BIN" run -c "$QUICK_XRAY_CONFIG"
}

run_native_demux() {
  exec "$MANAGER_BIN" --demux \
    --demux-listen "127.0.0.1:$NATIVE_DEMUX_PORT" \
    --demux-ws "127.0.0.1:$NATIVE_DEMUX_WS_PORT" \
    --demux-xhttp "127.0.0.1:$NATIVE_DEMUX_XHTTP_PORT"
}

run_panel_demux() {
  exec "$MANAGER_BIN" --demux \
    --demux-listen "127.0.0.1:$PANEL_DEMUX_PORT" \
    --demux-ws "127.0.0.1:$PANEL_DEMUX_WS_PORT" \
    --demux-xhttp "127.0.0.1:$PANEL_DEMUX_XHTTP_PORT"
}

run_manager() {
  exec "$MANAGER_BIN" \
    --module "$MODDIR" \
    --state "$STATE_DIR" \
    --listen "127.0.0.1:$MANAGER_PORT"
}

start_background() {
  name=$1
  pid_file=$2
  log_file=$3
  shift 3

  remove_stale_pid "$pid_file" "$name"
  if pid_is_running "$pid_file" "$name"; then
    return 0
  fi

  umask 077
  "$@" >> "$log_file" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pid_file"
  sleep 1
  if ! pid_is_running "$pid_file" "$name"; then
    log_line "$name exited during startup; inspect $log_file"
    rm -f "$pid_file"
    return 1
  fi
  log_line "started $name pid=$pid"
}

start_panel() {
  if ! is_true "$PANEL_ENABLED"; then
    stop_pid_file "$PANEL_PID_FILE" "x-ui"
    return 0
  fi
  [ -x "$XUI_BIN" ] || {
    log_line "x-ui missing: $XUI_BIN"
    return 1
  }
  configure_panel || {
    log_line "panel configuration failed"
    return 1
  }
  start_background "x-ui" "$PANEL_PID_FILE" "$PANEL_LOG" run_panel
}

start_manager() {
  if ! is_true "$MANAGER_ENABLED"; then
    stop_pid_file "$MANAGER_PID_FILE" "android-mini-server-manager"
    return 0
  fi
  [ -x "$MANAGER_BIN" ] || {
    log_line "manager missing: $MANAGER_BIN"
    return 1
  }
  is_valid_port "$MANAGER_PORT" || {
    log_line "invalid MANAGER_PORT: $MANAGER_PORT"
    return 1
  }
  start_background "android-mini-server-manager" "$MANAGER_PID_FILE" "$MANAGER_LOG" run_manager
}

start_tunnel() {
  if ! is_true "$TUNNEL_ENABLED"; then
    stop_pid_file "$TUNNEL_PID_FILE" "cloudflared"
    return 0
  fi
  [ -x "$CLOUDFLARED_BIN" ] || {
    log_line "cloudflared missing: $CLOUDFLARED_BIN"
    return 1
  }
  if [ "$TUNNEL_MODE" != "quick" ] && [ ! -s "$TOKEN_FILE" ]; then
    log_line "cloudflared enabled but token.txt is empty"
    return 1
  fi
  start_background "cloudflared" "$TUNNEL_PID_FILE" "$TUNNEL_LOG" run_tunnel
}

start_panel_tunnel() {
  if ! is_true "$PANEL_TUNNEL_ENABLED"; then
    stop_pid_file "$PANEL_TUNNEL_PID_FILE" "cloudflared"
    return 0
  fi
  [ -x "$CLOUDFLARED_BIN" ] || {
    log_line "cloudflared missing: $CLOUDFLARED_BIN"
    return 1
  }
  if [ "$PANEL_TUNNEL_MODE" != "quick" ] && [ ! -s "$PANEL_TUNNEL_TOKEN_FILE" ]; then
    log_line "panel cloudflared enabled but panel-tunnel-token.txt is empty"
    return 1
  fi
  start_background "cloudflared" "$PANEL_TUNNEL_PID_FILE" "$PANEL_TUNNEL_LOG" run_panel_tunnel
}

start_quick_server() {
  if ! is_true "$QUICK_SERVER_ENABLED"; then
    stop_pid_file "$QUICK_SERVER_PID_FILE" "xray-linux-arm64"
    return 0
  fi
  [ -x "$QUICK_XRAY_BIN" ] || {
    log_line "Quick Tunnel Xray missing: $QUICK_XRAY_BIN"
    return 1
  }
  is_valid_port "$QUICK_SERVER_PORT" || {
    log_line "invalid QUICK_SERVER_PORT: $QUICK_SERVER_PORT"
    return 1
  }
  [ -s "$QUICK_XRAY_CONFIG" ] || {
    log_line "Quick Tunnel Xray config is missing: $QUICK_XRAY_CONFIG"
    return 1
  }
  start_background "xray-linux-arm64" "$QUICK_SERVER_PID_FILE" "$QUICK_SERVER_LOG" run_quick_server
}

start_native_demux() {
  if ! is_true "$NATIVE_DEMUX_ENABLED"; then
    stop_native_demux
    return 0
  fi
  [ -x "$MANAGER_BIN" ] || {
    log_line "native transport demux manager binary missing: $MANAGER_BIN"
    return 1
  }
  is_valid_port "$NATIVE_DEMUX_PORT" && is_valid_port "$NATIVE_DEMUX_WS_PORT" && is_valid_port "$NATIVE_DEMUX_XHTTP_PORT" || {
    log_line "invalid native transport demux port"
    return 1
  }
  start_background "demux-listen" "$NATIVE_DEMUX_PID_FILE" "$NATIVE_DEMUX_LOG" run_native_demux
}

stop_native_demux() {
  stop_pid_file "$NATIVE_DEMUX_PID_FILE" "demux-listen"
  # Older releases used a grep pattern beginning with --demux, which could
  # lose the pid file while leaving this process alive. Clean that case once.
  for proc in /proc/[0-9]*; do
    pid=${proc#/proc/}
    [ -r "$proc/cmdline" ] || continue
    command=$(tr '\000' ' ' < "$proc/cmdline" 2>/dev/null)
    case "$command" in
      *"$MANAGER_BIN"*" --demux "*" --demux-listen 127.0.0.1:$NATIVE_DEMUX_PORT "*)
        kill -TERM "$pid" 2>/dev/null
        attempt=0
        while kill -0 "$pid" 2>/dev/null && [ "$attempt" -lt 5 ]; do
          sleep 1
          attempt=$((attempt + 1))
        done
        kill -KILL "$pid" 2>/dev/null
        ;;
    esac
  done
  rm -f "$NATIVE_DEMUX_PID_FILE"
}

start_panel_demux() {
  if ! is_true "$PANEL_DEMUX_ENABLED"; then
    stop_panel_demux
    return 0
  fi
  [ -x "$MANAGER_BIN" ] || {
    log_line "panel transport demux manager binary missing: $MANAGER_BIN"
    return 1
  }
  is_valid_port "$PANEL_DEMUX_PORT" && is_valid_port "$PANEL_DEMUX_WS_PORT" && is_valid_port "$PANEL_DEMUX_XHTTP_PORT" || {
    log_line "invalid panel transport demux port"
    return 1
  }
  start_background "demux-listen" "$PANEL_DEMUX_PID_FILE" "$PANEL_DEMUX_LOG" run_panel_demux
}

stop_panel_demux() {
  stop_pid_file "$PANEL_DEMUX_PID_FILE" "demux-listen"
  rm -f "$PANEL_DEMUX_PID_FILE"
}
