# Third-Party Notices

Xray Server Native orchestrates upstream software. It is not affiliated with
the projects below. Trademarks remain the property of their respective owners.

## 3x-ui

- Project: [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui)
- Recorded release: `v3.7.0`
- License: GNU General Public License v3.0
- Included component: `module/x-ui`
- Local change: `tools/patch-xui-android-path.ps1` changes three equal-length
  path constants so the executable uses `/data/xui` on Android.

The flashable ZIP includes the GPL text and identifies the corresponding
upstream source and local patch. Anyone redistributing a ZIP containing the
patched `x-ui` executable must also provide its corresponding source under the
GPL-3.0 terms.

## Xray-core

- Project: [XTLS/Xray-core](https://github.com/XTLS/Xray-core)
- License: Mozilla Public License 2.0
- Included component: `module/bin/xray-linux-arm64`
- Local change: none

The MPL-2.0 text from the upstream release remains at `module/bin/LICENSE`.

## cloudflared

- Project: [cloudflare/cloudflared](https://github.com/cloudflare/cloudflared)
- Recorded release: `2026.9.0`
- License: Apache License 2.0
- Included component: `module/bin/cloudflared`
- Local change: `tools/patch-cloudflared-android-dns.ps1` replaces the
  equal-length resolver path `/etc/resolv.conf` with `/data/xui/resolv`.

## GeoIP and geosite data

`geoip.dat` and `geosite.dat` are received as part of the recorded 3x-ui
release archive. Their individual upstream provenance is not asserted by this
repository. Preserve upstream notices when redistributing them. Before
publishing a refreshed bundle, verify the exact upstream source and license for
each data asset.

Regional geo data and MTG are intentionally not included by the source build.
