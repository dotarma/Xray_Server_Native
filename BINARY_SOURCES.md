# Binary Payload Manifest

This repository contains source code for the Android module and may contain
release binaries in `module/`. Those binaries are upstream components, not
authored by Xray Server Native.

| Payload | Recorded source | License / notice | Local modification |
| --- | --- | --- | --- |
| `module/x-ui` | MHSanaei/3x-ui `v3.7.0`, `x-ui-linux-arm64.tar.gz` | GPL-3.0 | `tools/patch-xui-android-path.ps1` replaces equal-length runtime paths only. |
| `module/bin/xray-linux-arm64` | Bundled in the recorded 3x-ui release | MPL-2.0 | None. Its license is retained at `module/bin/LICENSE`. |
| `module/bin/cloudflared` | cloudflare/cloudflared `2026.9.0`, `cloudflared-linux-arm64` | Apache-2.0 | `tools/patch-cloudflared-android-dns.ps1` replaces one equal-length resolver path. |
| `module/bin/geoip.dat`, `geosite.dat` | Bundled in the recorded 3x-ui release | See upstream data sources and notices | None. |
| `module/cacert.pem` | curl CA Extract | MPL-2.0 | None. |

The download and patch process is implemented in
`tools/prepare-upstream-binaries.ps1`. It pins release versions and requires
their recorded SHA-256 values to match both GitHub release metadata and the
downloaded asset before applying an equal-length binary patch. The mutable curl
CA Extract requires an explicit, operator-verified SHA-256 argument; record it
in release notes before publishing a ZIP.

Recorded upstream release-asset SHA-256 values:

- 3x-ui `v3.7.0` `x-ui-linux-arm64.tar.gz`:
  `3caf1db1e8b10bb1fa1324c945522690bcf01c533ee75b377268f1c01a3ce896`
- cloudflared `2026.9.0` `cloudflared-linux-arm64`:
  `98aca3173f73248fad6180fc75dade2d186a6e54fa807e088108cb4345de8efe`

Current patched payload SHA-256 values:

- `module/x-ui`:
  `0aaa90ab9781af72e9b24f52b60f617e6a3f26aac23997f4a59fd2efb76e7e81`
- `module/bin/cloudflared`:
  `246cc3f1ce966a669a089960774afcb96f96bcf4979ab4e66884a271ff19a525`

Do not replace a payload from an unverified mirror. When updating a binary,
update this manifest, `THIRD_PARTY_NOTICES.md`, and the corresponding source
information before publishing a new flashable ZIP.
