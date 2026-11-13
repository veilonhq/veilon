<p align="center">
  <a href="../LICENSE"><img src="https://img.shields.io/badge/license-All%20rights%20reserved-8B7CFF?style=flat-square&labelColor=0B0D12" alt="License: All rights reserved"></a>
</p>

<p align="center">
  <a href="#fa"><b>فارسی</b></a>
  &nbsp;·&nbsp;
  <a href="#english"><b>English</b></a>
</p>

---

<a id="fa"></a>

<div dir="rtl">

<div align="center">

# Veilon Client

**توسعه داده شده توسط &lrm;[@lxhosein](https://t.me/lxhosein)**

کلاینت رسمی اندروید برای پنل Veilon

📱 [دانلود آخرین نسخه](https://github.com/veilonhq/veilon/releases/tag/client-latest)
&nbsp;·&nbsp;
🖥 [نصب پنل](https://github.com/veilonhq/veilon)
&nbsp;·&nbsp;
🔍 [اسکنر DNS و IP (ScanON)](https://github.com/skylarsob/ScanON)
&nbsp;·&nbsp;
📢 [کانال تلگرام](https://t.me/veilon2)

</div>

کلاینت اندروید Veilon با رابط Flutter و هسته Go ساخته شده و با لینک اشتراک پنل Veilon سازگار است. رابط برنامه فارسی و انگلیسی است.

- **هسته:** Xray و sing-box (VLESS، VMess، Trojan، Shadowsocks، Hysteria2، TUIC، WireGuard، SOCKS) به همراه AmneziaWG
- **تونل:** VpnService اندروید با پشته gVisor، تقسیم تونل برای هر برنامه، کیل سوییچ و کاشی تنظیمات سریع
- **کانفیگ ها:** لینک های اشتراک گذاری استاندارد، فایل های `.conf` برای WireGuard و AmneziaWG، کد QR و لینک اشتراک، با ویرایشگر کامل
- **بروزرسانی:** بررسی نسخه جدید از داخل تنظیمات برنامه

فقط اندروید (اندروید 7.0 به بالا).

## دانلود

جدیدترین نسخه همیشه در این صفحه است:
https://github.com/veilonhq/veilon/releases/tag/client-latest

لینک مستقیم دانلود (روی همه گوشی ها):
https://github.com/veilonhq/veilon/releases/download/client-latest/veilon-client-universal.apk

**کدام فایل را نصب کنم؟**

- `veilon-client-arm64-v8a.apk`: برای بیشتر گوشی ها از حدود سال 2017 به بعد (64 بیتی). اگر مطمئن نیستید، همین را نصب کنید.
- `veilon-client-armeabi-v7a.apk`: برای گوشی های قدیمی تر یا 32 بیتی.
- `veilon-client-universal.apk`: روی همه گوشی ها کار می کند اما حجمش بیشتر است.

اگر نسخه قبلی را نصب کرده اید، همان نوع را نصب کنید تا روی آن بروزرسانی شود و اطلاعات حفظ شود.

## استفاده با پنل Veilon

1. در پنل، کاربر را باز کنید و **لینک اشتراک** او را کپی کنید.
2. در برنامه دکمه **+** را بزنید و لینک اشتراک را وارد کنید (یا کد QR آن را اسکن کنید).
3. یک سرور انتخاب کنید و دکمه اتصال را بزنید. برای بروزرسانی اشتراک از دکمه بروزرسانی استفاده کنید.

## ابزار پیشنهادی: ScanON

اگر دنبال DNS یا IP تمیز هستید، [ScanON](https://github.com/skylarsob/ScanON) یک اپ اندرویدی برای کاربرهای ایران است که IP، DNS و دامنه را اسکن می کند و نتیجه واقعی و تایید شده می دهد.

- اسکن DNS با UDP، TCP، DoT و DoH
- تست TCP، HTTP، HTTPS، WebSocket، ICMP و سرعت دانلود
- موقعیت و ASN هر IP

راهنما و دانلود: https://skylarsob.github.io/ScanON/

</div>

---

<a id="english"></a>

<div align="center">

# Veilon Client

**Developed by &lrm;[@lxhosein](https://t.me/lxhosein)**

The official Android client for the Veilon panel

📱 [Download the latest version](https://github.com/veilonhq/veilon/releases/tag/client-latest)
&nbsp;·&nbsp;
🖥 [Install the panel](https://github.com/veilonhq/veilon)
&nbsp;·&nbsp;
🔍 [DNS & IP scanner (ScanON)](https://github.com/skylarsob/ScanON)
&nbsp;·&nbsp;
📢 [Telegram channel](https://t.me/veilon2)

</div>

The Veilon Android client has a Flutter UI and a Go core, and works with Veilon panel subscription links. The app is in Persian and English.

- **Core:** Go library (`core/veiloncore`) bound with gomobile, running Xray and sing-box
  (VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard, SOCKS) plus AmneziaWG.
- **Tunnel:** Android `VpnService` with a gVisor TUN stack, per-app split tunnelling, kill switch, quick-settings tile.
- **Configs:** standard share links (`vless://`, `vmess://`, `trojan://`, `ss://`,
  `hysteria2://`, `tuic://`, `wireguard://`, WireGuard/AmneziaWG `.conf`), QR codes,
  and subscription links, with a full editor and host override for every profile.
- **Updates:** in-app update check from Settings.

Android only for now (minSdk 24, Android 7.0+).

## Download

The newest client is always on the fixed release page:
https://github.com/veilonhq/veilon/releases/tag/client-latest

Direct download (all phones):
https://github.com/veilonhq/veilon/releases/download/client-latest/veilon-client-universal.apk

**Which APK?**

- `veilon-client-arm64-v8a.apk`: most phones from about 2017 onward (64-bit). Pick this one if unsure.
- `veilon-client-armeabi-v7a.apk`: older or 32-bit phones.
- `veilon-client-universal.apk`: works on all phones, but larger.

If you already have an earlier version, install the same build so it updates in place and keeps your data.

Versioned client releases use the tag prefix `client-v` (for example `client-v1.0.7`), separate from the panel's `v*` tags.

## Using it with the Veilon panel

1. In the panel, open a user and copy their **subscription link**.
2. In the app, tap **+** and paste the subscription URL (or scan its QR code).
3. Pick a server and tap the connect button. Use the refresh button to update the subscription.

## Recommended tool: ScanON

Looking for a clean DNS or IP? [ScanON](https://github.com/skylarsob/ScanON) is an Android app for users in Iran that scans IPs, DNS servers and domains with verified, honest measurements.

- DNS scanning over UDP, TCP, DoT and DoH
- TCP, HTTP, HTTPS, WebSocket, ICMP and download-speed tests
- Location and ASN for every IP

Guide and download: https://skylarsob.github.io/ScanON/

## Layout

```
client/
  app/    Flutter app + Android (Kotlin) VpnService
  core/   Go core (gomobile -> veiloncore.aar)
```

## License

All rights reserved. No one may copy, modify, redistribute or sell this code. Using the official released builds is allowed. See [LICENSE](../LICENSE).

<div dir="rtl">

همه حقوق محفوظ است. هیچ کس حق کپی، ویرایش، انتشار دوباره یا فروش این کد را ندارد. استفاده از نسخه های رسمی منتشر شده آزاد است.

</div>

---

<div align="center" dir="rtl">

**حق کپی و ویرایش وجود ندارد**

</div>

<p align="center"><b>Copying and modification are not permitted.</b></p>
