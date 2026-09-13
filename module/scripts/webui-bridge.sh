#!/system/bin/sh

# This bridge is invoked only by KernelSU's module WebUI. It forwards small
# JSON requests to the loopback manager as root; the WebUI never reads
# token.txt or panel credentials.

MODDIR=${0%/*}
MODDIR=${MODDIR%/scripts}
STATE_DIR=$MODDIR
CONFIG_FILE=$STATE_DIR/service.env
RUN_DIR=$STATE_DIR/run
KSU_BUSYBOX=/data/adb/ksu/bin/busybox
if [ ! -x "$KSU_BUSYBOX" ]; then
  # Magisk's module WebUI host still executes this root bridge, but stores
  # BusyBox separately from KernelSU.
  KSU_BUSYBOX=/data/adb/magisk/busybox
fi

json_error() {
  printf '{"error":"%s"}\n' "$1"
  exit 0
}

[ -x "$KSU_BUSYBOX" ] || json_error "A root-manager BusyBox is not available."

get_config_value() {
  key=$1
  fallback=$2
  value=$($KSU_BUSYBOX sed -n "s/^${key}=//p" "$CONFIG_FILE" 2>/dev/null | $KSU_BUSYBOX tail -n 1)
  [ -n "$value" ] && printf '%s\n' "$value" || printf '%s\n' "$fallback"
}

decode_payload() {
  encoded=$1
  [ -n "$encoded" ] || return 1
  printf '%s' "$encoded" | $KSU_BUSYBOX base64 -d 2>/dev/null
}

request() {
  method=$1
  endpoint=$2
  payload=$3
  manager_port=$(get_config_value MANAGER_PORT 2036)
  url="http://127.0.0.1:${manager_port}${endpoint}"
  if [ "$method" = GET ]; then
    $KSU_BUSYBOX wget -q -O - "$url" 2>/dev/null
    return 0
  fi

  mkdir -p "$RUN_DIR" || json_error "Could not prepare the local request file."
  request_file=$RUN_DIR/webui-request.$$.json
  umask 077
  printf '%s' "$payload" > "$request_file" || json_error "Could not write the local request file."
  $KSU_BUSYBOX wget -q -O - \
    --header "Content-Type: application/json" \
    --post-file "$request_file" \
    "$url" 2>/dev/null
  rm -f "$request_file"
  return 0
}

action=${1:-}
payload=$(decode_payload "${2:-}")
case "$action" in
  status)
    request GET /api/status ''
    ;;
  logs)
    target=$payload
    case "$target" in panel|panel-tunnel|tunnel|quick|all) ;; *) target=all ;; esac
    request GET "/api/logs?target=$target" ''
    ;;
  service)
    request POST /api/services "$payload"
    ;;
  quick)
    request POST /api/deploy/quick "$payload"
    ;;
  mode2)
    request POST /api/deploy/mode2 "$payload"
    ;;
  mode3)
    request POST /api/deploy/mode3 "$payload"
    ;;
  token)
    request POST /api/tunnel/token "$payload"
    ;;
  *)
    json_error "Unsupported WebUI action."
    ;;
esac
