SKIPUNZIP=0

ui_print " "
ui_print "- Android Mini Server Native"
ui_print "- Target: rooted Android arm64-v8a"

if [ "$ARCH" != "arm64" ]; then
  abort "! This module supports ARM64 devices only. Detected: $ARCH"
fi

if [ "${API:-0}" -lt 26 ]; then
  abort "! Android 8.0 (API 26) or newer is required."
fi

STATE_DIR=$MODPATH
LEGACY_STATE_DIR=/data/local/tmp/android-mini-server-native
ACTIVE_MODULE_DIR=/data/adb/modules/android-mini-server-native

# Keep the KernelSU WebUI sources packaged as webroot.disabled for now. KernelSU
# only exposes its module shortcut when a root-level webroot/index.html exists.
rm -rf "$MODPATH/webroot"

# Preserve mutable state when updating from an earlier build. The active module
# is still available while KernelSU extracts this ZIP into modules_update.
for previous in "$ACTIVE_MODULE_DIR" "$LEGACY_STATE_DIR"; do
  [ "$previous" = "$MODPATH" ] && continue
  [ -d "$previous" ] || continue
  for entry in db log service.env token.txt panel-tunnel-token.txt resolv panel-credentials.txt panel-base-path.txt panel-settings.sig deployment.json deployments.json quick-xray.json mode2-preferences.json mode3-preferences.json; do
    [ -e "$MODPATH/$entry" ] || cp -af "$previous/$entry" "$MODPATH/" 2>/dev/null
  done
done

mkdir -p "$STATE_DIR/db" "$STATE_DIR/log" "$STATE_DIR/run"
chmod 700 "$STATE_DIR/db" "$STATE_DIR/log" "$STATE_DIR/run"

if [ ! -f "$STATE_DIR/service.env" ]; then
  cp -f "$MODPATH/config/service.env.example" "$STATE_DIR/service.env"
  chmod 600 "$STATE_DIR/service.env"
  ui_print "- Created persistent service.env"
fi

if [ ! -f "$STATE_DIR/token.txt" ]; then
  : > "$STATE_DIR/token.txt"
  chmod 600 "$STATE_DIR/token.txt"
fi

if [ ! -f "$STATE_DIR/panel-tunnel-token.txt" ]; then
  : > "$STATE_DIR/panel-tunnel-token.txt"
  chmod 600 "$STATE_DIR/panel-tunnel-token.txt"
fi

# The manager is loopback-only and no longer has an HTTP login. Move previous
# installations to its new dedicated port without touching 3x-ui's port.
sed -i 's/^MANAGER_PORT=2081$/MANAGER_PORT=2036/' "$STATE_DIR/service.env" 2>/dev/null
sed -i 's/^QUICK_SERVER_PORT=18080$/QUICK_SERVER_PORT=8888/' "$STATE_DIR/service.env" 2>/dev/null
sed -i 's#^QUICK_TUNNEL_TARGET=http://127.0.0.1:\(10080\|18080\)$#QUICK_TUNNEL_TARGET=http://127.0.0.1:8888#' "$STATE_DIR/service.env" 2>/dev/null

set_perm "$MODPATH" 0 0 0755
for runtime_dir in bin config initrc manager scripts; do
  if [ -d "$MODPATH/$runtime_dir" ]; then
    set_perm_recursive "$MODPATH/$runtime_dir" 0 0 0755 0644
  fi
done
set_perm "$MODPATH/service.sh" 0 0 0755
set_perm "$MODPATH/boot-completed.sh" 0 0 0755
set_perm "$MODPATH/action.sh" 0 0 0755
set_perm "$MODPATH/uninstall.sh" 0 0 0755
set_perm_recursive "$MODPATH/scripts" 0 0 0755 0755
set_perm_recursive "$MODPATH/initrc" 0 0 0755 0644

if [ -f "$MODPATH/x-ui" ]; then
  set_perm "$MODPATH/x-ui" 0 0 0755
fi

if [ -f "$MODPATH/bin/cloudflared" ]; then
  set_perm "$MODPATH/bin/cloudflared" 0 0 0755
fi

if [ -f "$MODPATH/manager/android-mini-server-manager" ]; then
  set_perm "$MODPATH/manager/android-mini-server-manager" 0 0 0755
fi

if [ -f "$MODPATH/bin/xray-linux-arm64" ]; then
  set_perm "$MODPATH/bin/xray-linux-arm64" 0 0 0755
fi

chmod 700 "$MODPATH/db" "$MODPATH/log" "$MODPATH/run" 2>/dev/null
chmod 600 "$MODPATH/service.env" "$MODPATH/token.txt" "$MODPATH/panel-tunnel-token.txt" "$MODPATH/panel-credentials.txt" "$MODPATH/deployment.json" "$MODPATH/deployments.json" "$MODPATH/quick-xray.json" "$MODPATH/mode2-preferences.json" "$MODPATH/mode3-preferences.json" 2>/dev/null
chmod 644 "$MODPATH/resolv" 2>/dev/null

if mkdir -p /data/xui 2>/dev/null && mount -o bind "$MODPATH" /data/xui 2>/dev/null; then
  sh "$MODPATH/service.sh" >/dev/null 2>&1 &
  ui_print "- Native x-ui bootstrap started"
else
  ui_print "- Runtime mount will be created after reboot by KernelSU policy"
fi

ui_print "- Runtime mount: /data/xui"
ui_print "- Reboot once after installation."
