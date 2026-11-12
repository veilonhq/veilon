# Veilon Client (Android)

The official Android client for the [Veilon](https://github.com/veilonhq/veilon) panel.

🖥 [Veilon panel: install and docs](https://github.com/veilonhq/veilon#readme) · [Latest panel release](https://github.com/veilonhq/veilon/releases/latest) · [Download this client](https://github.com/veilonhq/veilon/releases/tag/client-v1.0.6)

**توسعه داده شده توسط &lrm;[@lxhosein](https://t.me/lxhosein)**

کلاینت اندروید Veilon: رابط Flutter و هسته Go، سازگار با لینک اشتراک پنل Veilon.

- **UI:** Flutter (Persian and English, RTL aware), dark design with the Veilon palette.
- **Core:** Go library (`core/veiloncore`) bound with gomobile, running Xray and sing-box
  (VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard, SOCKS) plus AmneziaWG.
- **Tunnel:** Android `VpnService` with a gVisor TUN stack (system/mixed stacks optional),
  per-app split tunnelling, kill switch, quick-settings tile.
- **Configs:** standard share links (`vless://`, `vmess://`, `trojan://`, `ss://`,
  `hysteria2://`, `tuic://`, `wireguard://`, WireGuard/AmneziaWG `.conf`), QR codes,
  and subscription links, with a full editor and host override for every profile.

Android only for now (minSdk 24, Android 7.0+).

## Using it with the Veilon panel

1. In the panel, open a user and copy their **subscription link**.
2. In the app, tap **+** and paste the subscription URL (or scan its QR code).
3. Pick a server and tap the connect button. Use the refresh button to update the subscription.

## Layout

```
client/
  app/    Flutter app + Android (Kotlin) VpnService
  core/   Go core (gomobile -> veiloncore.aar)
```

## Building

Requirements: Go (see `core/go.mod`), gomobile, Android SDK + NDK, Flutter (Dart SDK per `app/pubspec.yaml`), JDK 17.

**1. Build the Go core AAR** (arm64 and armv7):

```sh
cd client/core
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android/arm64,android/arm -androidapi 24 \
  -tags with_quic,with_utls,with_wireguard,with_gvisor -trimpath \
  -ldflags="-s -w -buildid= -checklinkname=0" \
  -o ../app/android/app/libs/veiloncore.aar ./veiloncore
```

The AAR is a build output and is not committed.

**2. Build the APKs:**

```sh
cd client/app
flutter pub get
flutter build apk --release --split-per-abi --target-platform android-arm,android-arm64
```

Output: `app/build/app/outputs/flutter-apk/` (`app-arm64-v8a-release.apk`, `app-armeabi-v7a-release.apk`).

**Signing:** no signing key is included. Create your own keystore and an
`app/android/key.properties` (ignored by git):

```properties
storeFile=../my-release.jks
storePassword=...
keyAlias=...
keyPassword=...
```

`storeFile` is relative to `app/android/`. Without it, release builds are signed with the debug key.

**Tests:** `cd core && go test ./veiloncore/` and `cd app && flutter test`.

## Releases

Client releases use their own tag prefix, `client-v` (for example `client-v1.0.6`),
separate from the panel's `v*` tags.

## License

All rights reserved. No one may copy, modify, redistribute or sell this code. Using the official released builds is allowed. See [LICENSE](../LICENSE).

همه حقوق محفوظ است. هیچ کس حق کپی، ویرایش، انتشار دوباره یا فروش این کد را ندارد. استفاده از نسخه های رسمی منتشر شده آزاد است.

---

**توسعه داده شده توسط &lrm;[@lxhosein](https://t.me/lxhosein)**

**Developed by &lrm;[@lxhosein](https://t.me/lxhosein)**
