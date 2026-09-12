# Xray Server Native

Xray Server Native is a Magisk and KernelSU module that runs 3x-ui, Xray and
Cloudflare Tunnel directly on a rooted ARM64 Android device. It keeps services
on loopback and provides a local manager at `http://127.0.0.1:2036`.

This project is intended for devices you own or administer. You are
responsible for network access, Cloudflare account configuration, and
compliance with applicable law and provider terms.

## Features

- Native ARM64 3x-ui panel with a local-only listener at `127.0.0.1:2053`.
- Magisk-compatible local manager and embedded KernelSU WebUI.
- Cloudflare Named Tunnel support without router port forwarding.
- Mode 1 Quick Tunnel and Mode 2 Named Tunnel for native Xray on
  `127.0.0.1:8888`.
- Mode 3 manages a 3x-ui client and inbound for Cloudflare Tunnel publishing.
- Mode 3 can create WebSocket, XHTTP, or paired WebSocket + XHTTP endpoints.
  The paired mode uses a local demultiplexer on `127.0.0.1:8080`.
- Per-client subscription, traffic and expiry controls remain managed by
  3x-ui in Mode 3.

## Requirements

- ARM64 Android device (`arm64-v8a`), Android 8 / API 26 or newer.
- Magisk or KernelSU with root access.
- A browser on the device for the local manager and panel.
- A Cloudflare account and a domain delegated to Cloudflare for Named Tunnel
  modes.

The module creates no public listener. Cloudflare Tunnel requires an outbound
connection from the device. It publishes HTTP-compatible WebSocket or XHTTP
traffic; it is not a general raw-TCP tunnel for arbitrary VLESS transports.

## Architecture

```text
browser -> 127.0.0.1:2036 -> local manager -> 3x-ui / native Xray / cloudflared
browser -> 127.0.0.1:2053 -> 3x-ui panel

Cloudflare public hostname -> cloudflared -> loopback origin
```

| Local endpoint | Purpose |
| --- | --- |
| `127.0.0.1:2036` | Manager on Magisk; KernelSU exposes the same interface as WebUI. |
| `127.0.0.1:2053` | 3x-ui panel. |
| `127.0.0.1:2096` | 3x-ui subscription endpoint. |
| `127.0.0.1:8888` | Native Xray origin used by Modes 1 and 2. |
| `127.0.0.1:8080` | Mode 3 VPN origin and paired WS/XHTTP demultiplexer. |

## Install

1. Flash a release ZIP in Magisk or KernelSU Manager.
2. Reboot the device once.
3. Open `http://127.0.0.1:2036` on Magisk, or open the module WebUI in
   KernelSU Manager.
4. Use the displayed 3x-ui username and copy the displayed password to sign
   in at `http://127.0.0.1:2053`.
5. Configure only the mode you need. A fresh installation runs 3x-ui and the
   local manager but does not start a Cloudflare Tunnel or native Xray mode.

The manager and 3x-ui panel intentionally bind to loopback. Do not expose
their ports directly to Wi-Fi or the Internet.

## Deployment Modes

### Mode 1: Quick Tunnel

Creates a temporary `trycloudflare.com` hostname for the native Xray origin.
The hostname changes when the connector restarts, so this mode is for testing.

### Mode 2: Named Tunnel

Uses a connector token for a user-created Cloudflare Named Tunnel. Create a
Public Hostname route in Cloudflare that points to:

```text
http://127.0.0.1:8888
```

The manager configures native Xray and returns VLESS links. Starting Mode 2
replaces a running Mode 1 deployment; they share the same native Xray origin.

### Mode 3: 3x-ui Tunnel

Uses 3x-ui for client lifecycle, traffic and expiry management. Create these
Cloudflare Public Hostname routes on the tunnel before starting the mode:

| Route | Local service | Required |
| --- | --- | --- |
| VPN hostname | `http://127.0.0.1:8080` | Yes |
| Subscription hostname | `http://127.0.0.1:2096` | When sharing subscriptions |
| Panel hostname | `http://127.0.0.1:2053` | Optional; protect it with Cloudflare Access if used |

The manager creates the selected inbound and client. With two fake SNI choices,
paired WebSocket + XHTTP and both port-link variants, it can generate eight
client links. The 3x-ui subscription keeps the primary inbound link; the
manager registers the other generated links as client External Links. Manual
External Links are preserved.

Each new Mode 3 deployment adds inbounds in 3x-ui instead of deleting existing
ones. The manager displays only the latest Mode 3 deployment to keep the
interface focused.

## Security

- Treat a Cloudflare connector token as a password. Do not paste it in public
  chats, screenshots, issues or logs.
- Keep the 3x-ui panel local unless you explicitly protect a public panel route
  with Cloudflare Access or an equivalent access control.
- Use a unique 3x-ui password and review active inbounds before sharing a
  subscription.
- Update upstream binaries deliberately and verify hashes. A working tunnel is
  not evidence that a panel or client configuration is secure.

## Build

From PowerShell on Windows:

```powershell
Set-Location D:\project2026\Xray_Server_Native
.\tools\prepare-upstream-binaries.ps1
.\tools\package-module.ps1
```

The package is written to `artifacts/packages/xray-server-native/<version>/`.
The manager is Go source in
`manager/`; build it for Android/Linux ARM64 before packaging when its source
changes. On a WSL host:

```bash
cd /mnt/d/project2026/Xray_Server_Native/manager
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' \
  -o ../module/manager/android-mini-server-manager .
go test ./...
```

## Project Layout

```text
module/       Flashable Magisk/KernelSU payload
manager/      Go manager source and tests
tools/        Upstream preparation, packaging and diagnostics
docs/         Design and native-build notes
artifacts/    Ignored packages, upstream inputs and local verification data
```

Only source, build scripts, documentation and required license files belong in
the Git repository. Generated binaries, downloaded upstream archives, package
ZIPs, staging trees, screenshots and device data stay under the ignored
`artifacts/` tree or the generated paths listed in `.gitignore`.

## License and Third-Party Components

The Xray Server Native source authored in this repository is licensed under
GPL-3.0-or-later. This conservative project license is appropriate because the
flashable package includes a modified 3x-ui executable licensed under GPL-3.0.

The package also contains independently licensed upstream components:

- 3x-ui: GPL-3.0.
- Xray-core: MPL-2.0.
- cloudflared: Apache-2.0.
- GeoIP and geosite data: retain their upstream data licenses and attribution.

Read [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and
[THIRD_PARTY_SOURCES.md](THIRD_PARTY_SOURCES.md) before redistributing a ZIP.
This repository is not affiliated with 3x-ui, Xray, Cloudflare, Magisk or
KernelSU.

## Contributing

Keep changes small, run `go test ./...`, run shell syntax checks, and do not
commit tokens, passwords, device databases or generated binaries without
recording their provenance and licenses.
