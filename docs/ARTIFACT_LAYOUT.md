# Artifact Layout

`artifacts/` is deliberately ignored by Git. It is for local material that
must not be pushed with the source repository.

```text
artifacts/
  packages/xray-server-native/<version>/  Flashable ZIPs in chronological order
  upstream/                      Downloaded upstream release archives
  toolchains/                    Local compiler archives
  work/staging/                  Unpacked package staging trees
  work/verification/             Extracted package verification trees
  work/screenshots/              Local UI captures
  private/                       Device inspection data and other sensitive files
```

All historical packages, including the earlier `android-mini-server-native`
names, belong under the canonical `xray-server-native/<version>/` tree. Keep
variant subdirectories when a version has more than one local build so that no
archive is overwritten. Use `tools/package-module.ps1` without
`-OutputDirectory` to place a newly created Xray Server Native ZIP in its
version directory. Do not commit anything under `artifacts/`.
