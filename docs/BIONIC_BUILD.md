# Android Bionic Build Fallback

Use this only when the checked upstream Linux ARM64 executable fails on the
target Android device. The standard packaging workflow first uses the official
3x-ui ARM64 release because it is static. A source build is more work and must
be tested on the exact Android version and root implementation that will run it.

## Requirements

- Linux or WSL2 build host.
- Android NDK r26 or newer.
- Go version required by the selected 3x-ui and Xray releases.
- Node.js and npm for the 3x-ui frontend bundle.
- Git, curl, unzip, and a working ARM64 Android device for validation.

Set `ANDROID_NDK_HOME` and choose an API level no higher than the device API.
API 26 is a conservative baseline for this module.

## Build 3x-ui for Android ARM64

```sh
export API=26
export TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin"
export GOOS=android GOARCH=arm64 CGO_ENABLED=1
export CC="$TOOLCHAIN/aarch64-linux-android${API}-clang"
export CXX="$TOOLCHAIN/aarch64-linux-android${API}-clang++"

git clone --depth 1 --branch v3.7.0 https://github.com/MHSanaei/3x-ui.git
cd 3x-ui/frontend
npm ci
npm run build
cd ..
go build -trimpath -ldflags '-s -w' -o x-ui .
```

Place the result at `module/x-ui` and ensure its Linux path constants target
`/data/xui`, as performed by `tools/patch-xui-android-path.ps1`.

Because this executable reports `runtime.GOOS=android`, 3x-ui will look for
`module/bin/xray-android-arm64`, not `xray-linux-arm64`.

## Build Xray and cloudflared for Android ARM64

The exact Go tags must be selected and audited from their upstream projects.
Keep the Android NDK compiler variables from the 3x-ui build above. Xray's own
Android release workflow uses CGO with the Android compiler:

```sh
export GOOS=android GOARCH=arm64 CGO_ENABLED=1
export CC="$TOOLCHAIN/aarch64-linux-android${API}-clang"
export CXX="$TOOLCHAIN/aarch64-linux-android${API}-clang++"

# From a checked-out XTLS/Xray-core source tree.
go build -o xray-android-arm64 -trimpath -buildvcs=false \
  -gcflags='all=-l=4' \
  -ldflags='-s -w -buildid= -checklinkname=0' -v ./main

# From a checked-out cloudflare/cloudflared source tree.
go build -mod=readonly -trimpath -ldflags='-s -w' \
  -o cloudflared github.com/cloudflare/cloudflared/cmd/cloudflared
```

Do not assume every release compiles unchanged for Android. Build logs and the
target-device `control.sh check` result are the source of truth. Keep a record
of source tag, Go version, NDK version, SHA-256 hash, and target device/API for
each binary that is put in a release ZIP.
