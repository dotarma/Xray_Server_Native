# Architecture

## Runtime model

```text
Android boot
  -> Magisk late-start service.sh or KernelSU initrc service
  -> supervisor.sh waits for sys.boot_completed=1
  -> android-mini-server-manager on 127.0.0.1:2036
  -> x-ui process
       -> Xray child process managed by 3x-ui
  -> optional cloudflared process
       -> outbound Cloudflare Tunnel connector
```

The module has no `system/` overlay and includes `skip_mount`. It does not need
systemd, a BusyBox installation, or a permissive SELinux rule. Magisk uses
`service.sh`; KernelSU also receives an `initrc` service with the documented
`u:r:ksu:s0` label, started only after `sys.boot_completed=1`. The supervisor
PID lock prevents the two compatible launch paths from duplicating work.

## Storage separation

| Location | Purpose | Survives a module update |
| --- | --- | --- |
| `/data/adb/modules/android-mini-server-native` | Module scripts, binaries, and mutable state | Preserved by `customize.sh` |
| `/data/xui` | Runtime bind mount used only by 3x-ui | Recreated at install/boot |
| Module `db/`, `log/`, `run/` | 3x-ui data, logs, and service PID files | Preserved during module update |
| Module `token.txt`, `deployment.json`, `quick-xray.json` | Root-only connector and deployment state | Preserved during module update |
| Module `resolv`, `panel-credentials.txt` | Resolver and panel credentials | Preserved during module update |

3x-ui uses `/data/xui` as its Android runtime root. Module services keep their
mutable state under the stable module directory, avoiding Magisk mount namespace
differences. During an update, `customize.sh` copies mutable state from the
active module or the legacy runtime directory before replacing the bind mount.

## Panel bootstrap

On its first start the supervisor:

1. creates the state folders with mode `0700`;
2. generates a 28-character panel password and uses `/` as the initial base path;
3. initializes 3x-ui with `XUI_DB_FOLDER`, `XUI_BIN_FOLDER`, and
   `XUI_LOG_FOLDER` pointed at module-controlled paths;
4. binds the panel to `127.0.0.1` unless the root owner explicitly changes the
   persistent configuration; and
5. writes the credentials to the root-only state directory.

The panel is not given a fixed default password. A user can rotate credentials
with `control.sh set-admin`.

## Manager model

`android-mini-server-manager` is a statically-linked Go ARM64 executable that
serves its original web UI only on `127.0.0.1:2036`. Every route requires HTTP
Basic authentication against the root-only 3x-ui credential file; mutating
requests also require a matching loopback Origin header. It calls `control.sh`
for lifecycle actions without returning credentials in its status response.
Native Modes 1 and 2 use a separate Xray configuration and never modify
3x-ui inbounds. Mode 3 identifies only its own VLESS inbounds by tag and
persisted UUID/path; existing user-created inbounds are not modified.

For a temporary functional test it starts an independent Xray VLESS-WebSocket
listener on `127.0.0.1:8888`, then starts cloudflared in Quick Tunnel mode and
waits for the generated `trycloudflare.com` hostname. It does not call the
3x-ui API in this mode. For a private domain it
accepts the connector token for a remotely-managed tunnel whose routes were
created by the user in Cloudflare Zero Trust, then persists only that connector
token. No Cloudflare API credential is accepted or stored by the module.

On KernelSU, `webroot/index.html` is opened by the Manager as the module's
embedded WebUI. The UI calls `scripts/webui-bridge.sh` through the official
KernelSU JavaScript bridge. The root bridge forwards JSON to the loopback
manager, so sensitive files remain outside page JavaScript. The same backend
remains available through
`127.0.0.1:2036` for Magisk, whose manager has no native module WebUI surface.

## Tunnel model

The module accepts only a remotely-managed tunnel token file. Cloudflare keeps
ingress and public-hostname configuration in its dashboard. This avoids
embedding hostname routing or account credentials in a flashable ZIP.

`cloudflared` is run with `--token-file`, not `--token`, to avoid exposing the
token via process listings. Treat `token.txt` like a password. Revoke and issue
a new tunnel token in Cloudflare if the phone, backup, or module ZIP is exposed.
The official binary's single `/etc/resolv.conf` path is replaced byte-for-byte
with `/data/xui/resolv`, allowing edge discovery on Android without modifying
the system partition.

## Supervision

The supervisor checks desired services every 20 seconds by default. Atomic
directory locks prevent concurrent supervisors and duplicate service starts;
it creates PID files for 3x-ui, cloudflared, and the manager, removes stale PID
files, and restarts an exited process on the next pass. Service logs rotate at
1 MiB by default and retain one `.1` copy. Module disable is honored without a
reboot by stopping all managed processes when the `disable` marker appears.

The 3x-ui process owns Xray. The module should not independently kill a child
Xray process during normal shutdown because that would bypass 3x-ui cleanup.
