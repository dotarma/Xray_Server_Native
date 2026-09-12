# Third-Party Source Map

This file records the corresponding source location and local modification for
each modified executable distributed by the module. It is not legal advice.

## 3x-ui source required for the patched x-ui binary

The packaged `module/x-ui` originates from:

```text
https://github.com/MHSanaei/3x-ui/tree/v3.7.0
```

The only local binary transformation is recorded in:

```text
tools/patch-xui-android-path.ps1
```

Before conveying a flashable ZIP publicly, make the `v3.7.0` 3x-ui source and
that patch script available alongside the ZIP at no charge, as required by the
GPL-3.0. Use `tools/download-3x-ui-source.ps1` to retrieve the exact tag into
an ignored local directory.

## cloudflared

The recorded binary originates from:

```text
https://github.com/cloudflare/cloudflared/tree/2026.9.0
```

Its local transformation is:

```text
tools/patch-cloudflared-android-dns.ps1
```

## Xray-core

The Xray executable is received unmodified from the recorded 3x-ui release.
Its upstream source is available from:

```text
https://github.com/XTLS/Xray-core
```

The precise release asset hashes and source records are in
`BINARY_SOURCES.md`.
